package backup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
// written.
//
// If the restore fails midway it is rolled back: the files it already
// changed are put back to their pre-restore content and mode, in reverse
// order, the backup set is discarded and the Result reports nothing. A
// machine left half old and half new is worse than the state the restore
// found, so a restore either completes fully or does not happen at all.
// Only when the rollback itself cannot complete is the set committed, so it
// records the state that really remains; the error then names the files
// that could not be put back.
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
			// Nothing was written yet, and the open set must not outlive the
			// failed restore.
			_ = tx.Abort()
			res.BackupID = ""
			return res, fmt.Errorf("back up current state: %w", err)
		}
	}

	// The state each op will roll back to is read and verified before a
	// single file is touched: that way nothing is changed if the set turned
	// out to be unreadable, and the rollback works from these snapshots.
	type state struct {
		data    []byte
		mode    os.FileMode
		existed bool
	}
	states := make([]state, len(ops))
	for i, o := range ops {
		data, mode, existed, err := tx.prior(o.entry.Path)
		if err != nil {
			_ = tx.Abort()
			res.BackupID = ""
			return res, err
		}
		states[i] = state{data: data, mode: mode, existed: existed}
	}

	n, applyErr := applyOps(s.Home, ops, res)
	if applyErr == nil {
		// Commit the set so this restore itself can be undone through
		// Result.BackupID.
		if err := tx.Commit(); err != nil {
			return res, fmt.Errorf("finish restore backup set: %w", err)
		}
		return res, nil
	}

	// The restore failed midway: put back, in reverse order, every file it
	// already changed, so the machine ends where the restore found it.
	var (
		rollbackErrs []error
		badPaths     []string
	)
	for i := n - 1; i >= 0; i-- {
		st := states[i]
		path := ops[i].entry.Path
		var rerr error
		if st.existed {
			rerr = writeAtomic(s.Home, path, st.data, st.mode)
		} else {
			rerr = removeFile(s.Home, path)
		}
		if rerr == nil {
			continue
		}
		rollbackErrs = append(rollbackErrs, rerr)
		badPaths = append(badPaths, path)
	}

	if len(rollbackErrs) == 0 {
		abortErr := tx.Abort()
		if abortErr == nil {
			// The restore is fully undone and its set discarded: to the
			// outside world it never happened.
			res.Restored = nil
			res.Deleted = nil
			res.BackupID = ""
			return res, fmt.Errorf("restore failed and was rolled back: %w", applyErr)
		}
		// The set could not be discarded: fall through and commit it below,
		// so the state it holds is not lost.
		rollbackErrs = append(rollbackErrs, abortErr)
	}

	// The rollback is incomplete, so commit the set: it records the state
	// that really remains and stays undoable, instead of being thrown away.
	if commitErr := tx.Commit(); commitErr != nil {
		rollbackErrs = append(rollbackErrs, commitErr)
	}
	return res, fmt.Errorf("restore failed and could not be fully rolled back (files: %s; set %s records the current state): %w",
		strings.Join(badPaths, ", "), res.BackupID, errors.Join(append([]error{applyErr}, rollbackErrs...)...))
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

// prior returns the state of path as tx recorded it when tracking: the
// original content, its mode, and whether the file existed at all. The
// content is read from the set's blob and verified against its recorded
// hash, so a rollback can never restore corrupted data.
func (t *Tx) prior(path string) (data []byte, mode os.FileMode, existed bool, err error) {
	path = filepath.Clean(path)
	for _, e := range t.manifest.Entries {
		if e.Path != path {
			continue
		}
		if !e.Existed {
			return nil, 0, false, nil
		}
		data, err = os.ReadFile(filepath.Join(t.dir, blobsDir, e.Blob))
		if err != nil {
			return nil, 0, false, err
		}
		if hashBytes(data) != e.BeforeSHA256 {
			return nil, 0, false, fmt.Errorf("backup of %q is corrupted: hash mismatch", path)
		}
		return data, os.FileMode(e.Mode), true, nil
	}
	return nil, 0, false, fmt.Errorf("no backup entry for %q", path)
}

// applyOps applies ops in order and stops at the first failure. It returns
// how many ops were fully applied, and the op that failed is not counted,
// so the caller can roll exactly those back in reverse order. It keeps
// appending to res.Restored and res.Deleted as it goes.
func applyOps(home string, ops []op, res *Result) (int, error) {
	applied := 0
	for _, o := range ops {
		if o.delete {
			if err := removeFile(home, o.entry.Path); err != nil {
				return applied, err
			}
			res.Deleted = append(res.Deleted, o.entry.Path)
			applied++
			continue
		}
		if err := writeAtomic(home, o.entry.Path, o.data, os.FileMode(o.entry.Mode)); err != nil {
			return applied, err
		}
		res.Restored = append(res.Restored, o.entry.Path)
		applied++
	}
	return applied, nil
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
