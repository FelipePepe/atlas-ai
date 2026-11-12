package backup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// Action is what a restore would do with one tracked file.
type Action string

const (
	// ActionRestore puts the original content back.
	ActionRestore Action = "restore"
	// ActionDelete removes a file that did not exist before the change.
	ActionDelete Action = "delete"
	// ActionSkipUnchanged means the file already equals its original state.
	ActionSkipUnchanged Action = "skip-unchanged"
	// ActionConflict means the file is not in the state the set recorded, so
	// restoring it could discard changes made by someone else.
	ActionConflict Action = "conflict"
)

// PlanEntry is the planned action for one tracked file.
type PlanEntry struct {
	Path   string
	Action Action
	Reason string
}

// Plan lists what restoring a backup set would do, one entry per tracked
// file, in manifest order.
type Plan struct {
	ID      string
	Status  string
	Entries []PlanEntry
}

// Result reports the outcome of Restore. Every slice holds file paths.
type Result struct {
	// BackupID is the id of the set that backed up the state replaced by this
	// restore; empty when nothing had to change.
	BackupID  string
	Restored  []string
	Deleted   []string
	Skipped   []string
	Conflicts []string
}

// fileState is the current state of a path.
type fileState struct {
	exists bool
	hash   string
}

// inspect reads the current state of path without following symlinks. Anything
// that is not a regular file is an error.
func inspect(path string) (fileState, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return fileState{}, nil
	}
	if err != nil {
		return fileState{}, err
	}
	if !info.Mode().IsRegular() {
		return fileState{}, errors.New("not a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fileState{}, err
	}
	return fileState{exists: true, hash: hashBytes(data)}, nil
}

// Plan computes what restoring set id would do, by comparing the current
// state of every tracked file with the states recorded in the manifest.
func (s *Store) Plan(id string) (*Plan, error) {
	m, err := s.Load(id)
	if err != nil {
		return nil, err
	}
	for i, e := range m.Entries {
		if err := confine(s.Home, e.Path); err != nil {
			return nil, fmt.Errorf("entry %d: %w", i, err)
		}
	}
	p := &Plan{ID: m.ID, Status: m.Status, Entries: make([]PlanEntry, 0, len(m.Entries))}
	for _, e := range m.Entries {
		p.Entries = append(p.Entries, planEntry(m.Status, e))
	}
	return p, nil
}

func planEntry(status string, e Entry) PlanEntry {
	pe := PlanEntry{Path: e.Path}
	cur, err := inspect(e.Path)
	if err != nil {
		pe.Action = ActionConflict
		pe.Reason = fmt.Sprintf("cannot inspect current state: %v", err)
		return pe
	}

	atOriginal := !cur.exists
	if e.Existed {
		atOriginal = cur.exists && cur.hash == e.BeforeSHA256
	}
	if atOriginal {
		pe.Action = ActionSkipUnchanged
		pe.Reason = "already equal to the original"
		return pe
	}
	if status == StatusOpen {
		pe.Action = ActionConflict
		pe.Reason = "the set was never committed, so the state it produced is unknown"
		return pe
	}

	atAfter := cur.exists == e.AfterExists && (!cur.exists || cur.hash == e.AfterSHA256)
	if !atAfter {
		pe.Action = ActionConflict
		pe.Reason = "changed since the backup set was committed"
		return pe
	}
	if e.Existed {
		pe.Action = ActionRestore
		pe.Reason = "put the original content back"
	} else {
		pe.Action = ActionDelete
		pe.Reason = "the file did not exist before"
	}
	return pe
}

// op is one validated change Restore is about to apply.
type op struct {
	entry  Entry
	delete bool
	data   []byte // original content; nil for deletes
}

// Restore puts the files of set id back to their recorded original state.
//
// Files in conflict are left untouched and listed in Result.Conflicts unless
// force is true, in which case they are overwritten (or deleted). Before any
// file is changed, the current state of every file about to change is saved
// in a new backup set (Result.BackupID), so a restore can itself be undone.
// Every blob is verified against its recorded hash before anything is
// written. On failure it returns the partial Result together with the error.
func (s *Store) Restore(id string, force bool) (*Result, error) {
	m, err := s.Load(id)
	if err != nil {
		return nil, err
	}
	res := &Result{}
	var ops []op
	for _, e := range m.Entries {
		pe := planEntry(m.Status, e)
		action := pe.Action
		if action == ActionConflict {
			if !force {
				res.Conflicts = append(res.Conflicts, e.Path)
				continue
			}
			if e.Existed {
				action = ActionRestore
			} else {
				action = ActionDelete
			}
		}
		switch action {
		case ActionSkipUnchanged:
			res.Skipped = append(res.Skipped, e.Path)
		case ActionDelete:
			ops = append(ops, op{entry: e, delete: true})
		case ActionRestore:
			data, err := s.readBlob(m.ID, e)
			if err != nil {
				return res, err
			}
			ops = append(ops, op{entry: e, data: data})
		}
	}
	if len(ops) == 0 {
		return res, nil
	}

	// Confine every target before anything is written, so a bad entry
	// aborts the whole restore with no new backup set.
	for _, o := range ops {
		if err := confine(s.Home, o.entry.Path); err != nil {
			return res, err
		}
	}

	// Save the state about to be replaced so this restore can be undone.
	tx, err := s.Begin("restore " + id)
	if err != nil {
		return res, fmt.Errorf("back up current state: %w", err)
	}
	res.BackupID = tx.ID()
	for _, o := range ops {
		if err := tx.Track(o.entry.Path); err != nil {
			return res, fmt.Errorf("back up current state: %w", err)
		}
	}

	applyErr := applyOps(s.Home, ops, res)
	// Commit even after a failed apply so the set records what really
	// happened, which keeps the partial restore undoable.
	if err := tx.Commit(); err != nil && applyErr == nil {
		applyErr = fmt.Errorf("finish restore backup set: %w", err)
	}
	return res, applyErr
}

// readBlob returns the original content of e, verified against its recorded
// hash.
func (s *Store) readBlob(id string, e Entry) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(s.Root, id, blobsDir, e.Blob))
	if err != nil {
		return nil, fmt.Errorf("read backup of %q: %w", e.Path, err)
	}
	if hashBytes(data) != e.BeforeSHA256 {
		return nil, fmt.Errorf("backup of %q is corrupted: hash mismatch", e.Path)
	}
	return data, nil
}

func applyOps(home string, ops []op, res *Result) error {
	for _, o := range ops {
		if o.delete {
			if err := removeFile(home, o.entry.Path); err != nil {
				return err
			}
			res.Deleted = append(res.Deleted, o.entry.Path)
			continue
		}
		if err := writeAtomic(home, o.entry.Path, o.data, os.FileMode(o.entry.Mode)); err != nil {
			return err
		}
		res.Restored = append(res.Restored, o.entry.Path)
	}
	return nil
}

func removeFile(home, path string) error {
	if err := confine(home, path); err != nil {
		return fmt.Errorf("delete %q: %w", path, err)
	}
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete %q: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("delete %q: is a directory", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete %q: %w", path, err)
	}
	return nil
}

// writeAtomic replaces path with data through a temporary file in the same
// directory, so a failure never leaves a half-written target.
func writeAtomic(home, path string, data []byte, mode os.FileMode) error {
	if err := confine(home, path); err != nil {
		return fmt.Errorf("restore %q: %w", path, err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("restore %q: %w", path, err)
	}
	tmp, err := os.CreateTemp(dir, ".atlas-ai-restore-*.tmp")
	if err != nil {
		return fmt.Errorf("restore %q: %w", path, err)
	}
	tmpName := tmp.Name()
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("restore %q: %w", path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if runtime.GOOS != "windows" {
		if err := tmp.Chmod(mode.Perm()); err != nil {
			return fail(err)
		}
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("restore %q: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("restore %q: %w", path, err)
	}
	return nil
}
