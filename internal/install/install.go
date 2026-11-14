// Package install plans and applies the copy of embedded skills into agent
// skills directories, with every change recorded in a backup set.
package install

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/FelipePepe/atlas-ai/internal/backup"
)

const skillFile = "SKILL.md"

// Action is what installing an entry does to its target file.
type Action string

const (
	// Create writes a file that does not exist yet.
	Create Action = "create"
	// Update replaces a file whose content differs.
	Update Action = "update"
	// Unchanged leaves a file that already has the expected content.
	Unchanged Action = "unchanged"
)

// Entry is one planned SKILL.md file.
type Entry struct {
	Path   string
	Action Action
	Data   []byte
}

// Plan computes, without writing anything, the entries needed to install
// every <skill>/SKILL.md of skillsFS into each directory of dirs. Entries
// are ordered by directory, then skill name. Symlinks and non-regular files
// at any target location are rejected.
func Plan(skillsFS fs.FS, dirs []string) ([]Entry, error) {
	files, err := fs.Glob(skillsFS, "*/"+skillFile)
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	datas := make([][]byte, len(files))
	for i, f := range files {
		if datas[i], err = fs.ReadFile(skillsFS, f); err != nil {
			return nil, fmt.Errorf("read skill %q: %w", f, err)
		}
	}
	var entries []Entry
	for _, dir := range dirs {
		if err := checkDir(dir); err != nil {
			return nil, err
		}
		for i, f := range files {
			skillDir := filepath.Join(dir, filepath.Dir(filepath.FromSlash(f)))
			if err := checkDir(skillDir); err != nil {
				return nil, err
			}
			path := filepath.Join(skillDir, skillFile)
			action, err := classify(path, datas[i])
			if err != nil {
				return nil, err
			}
			entries = append(entries, Entry{Path: path, Action: action, Data: datas[i]})
		}
	}
	return entries, nil
}

// checkDir accepts a missing path or a real directory.
func checkDir(path string) error {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("inspect %q: %w", path, err)
	case !info.IsDir():
		return fmt.Errorf("%q is not a directory (symlinks are not followed)", path)
	}
	return nil
}

func classify(path string, want []byte) (Action, error) {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Create, nil
	case err != nil:
		return "", fmt.Errorf("inspect %q: %w", path, err)
	case !info.Mode().IsRegular():
		return "", fmt.Errorf("%q is not a regular file (symlinks are not followed)", path)
	}
	have, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %q: %w", path, err)
	}
	if bytes.Equal(have, want) {
		return Unchanged, nil
	}
	return Update, nil
}

// Apply writes every Create and Update entry inside one backup set and
// returns its id. It returns "" without creating a set when nothing changes.
// If a write fails after earlier files were written, the set is still
// committed so it can be restored, and its id is returned with the error.
func Apply(st *backup.Store, entries []Entry) (string, error) {
	var changed []Entry
	for _, e := range entries {
		if e.Action == Create || e.Action == Update {
			changed = append(changed, e)
		}
	}
	if len(changed) == 0 {
		return "", nil
	}
	tx, err := st.Begin("install skills")
	if err != nil {
		return "", err
	}
	for _, e := range changed {
		if err := tx.Track(e.Path); err != nil {
			if aerr := tx.Abort(); aerr != nil {
				return "", errors.Join(err, aerr)
			}
			return "", err
		}
	}
	written := 0
	for _, e := range changed {
		if err := backup.WriteFile(st.Home, e.Path, e.Data, 0o600); err != nil {
			if written == 0 {
				if aerr := tx.Abort(); aerr != nil {
					return "", errors.Join(err, aerr)
				}
				return "", err
			}
			if cerr := tx.Commit(); cerr != nil {
				return "", errors.Join(err, cerr)
			}
			return tx.ID(), err
		}
		written++
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return tx.ID(), nil
}
