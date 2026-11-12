package backup

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Store is a directory holding backup sets.
type Store struct {
	// Root is the directory that contains one subdirectory per set.
	Root string
	// Home is the user's home directory; only files under it may be tracked
	// or restored. It must be absolute.
	Home string
	// Now returns the current time; time.Now is used when nil.
	Now func() time.Time
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Tx is an open backup transaction. It is not safe for concurrent use.
type Tx struct {
	dir      string
	home     string
	manifest Manifest
	tracked  map[string]bool
}

// ID returns the id of the backup set.
func (t *Tx) ID() string { return t.manifest.ID }

// Begin creates a new backup set labelled label and writes its initial
// manifest with status "open".
func (s *Store) Begin(label string) (*Tx, error) {
	if err := checkHome(s.Home); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	if err := os.MkdirAll(s.Root, 0o700); err != nil {
		return nil, fmt.Errorf("create backup root: %w", err)
	}
	var dir, id string
	for attempt := 0; attempt < 8; attempt++ {
		suffix, err := randomSuffix()
		if err != nil {
			return nil, err
		}
		id = now.Format("20060102T150405Z") + "-" + suffix
		dir = filepath.Join(s.Root, id)
		err = os.Mkdir(dir, 0o700)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("create backup set: %w", err)
		}
		dir = ""
	}
	if dir == "" {
		return nil, errors.New("create backup set: could not find a free id")
	}
	if err := os.Mkdir(filepath.Join(dir, blobsDir), 0o700); err != nil {
		// Begin must not leave a half-created set behind.
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("create backup files directory: %w", err)
	}
	t := &Tx{
		dir:  dir,
		home: filepath.Clean(s.Home),
		manifest: Manifest{
			Version: ManifestVersion,
			ID:      id,
			Label:   label,
			Created: now.Format(time.RFC3339),
			Status:  StatusOpen,
			Entries: []Entry{},
		},
		tracked: map[string]bool{},
	}
	if err := t.writeManifest(); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return t, nil
}

func randomSuffix() (string, error) {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate backup id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// Track records the current state of path so it can be restored later. It
// must be called before the file is modified. Tracking a path twice keeps the
// first snapshot. Symlinks and non-regular files are rejected.
func (t *Tx) Track(path string) error {
	if t.manifest.Status != StatusOpen {
		return errors.New("backup set is already committed")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("cannot track %q: path must be absolute", path)
	}
	path = filepath.Clean(path)
	// Confine before touching the filesystem so nothing outside home is
	// read, copied or recorded.
	if err := confine(t.home, path); err != nil {
		return fmt.Errorf("cannot track %q: %w", path, err)
	}
	if t.tracked[path] {
		return nil
	}

	entry := Entry{Path: path}
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// Recorded as not existing; restore will delete the file.
	case err != nil:
		return fmt.Errorf("cannot track %q: %w", path, err)
	case !info.Mode().IsRegular():
		return fmt.Errorf("cannot track %q: not a regular file", path)
	default:
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("cannot track %q: %w", path, err)
		}
		blob := strconv.Itoa(len(t.manifest.Entries))
		blobPath := filepath.Join(t.dir, blobsDir, blob)
		if err := os.WriteFile(blobPath, data, 0o600); err != nil {
			return fmt.Errorf("copy %q into backup: %w", path, err)
		}
		entry.Existed = true
		entry.Blob = blob
		entry.Mode = uint32(info.Mode().Perm())
		entry.BeforeSHA256 = hashBytes(data)
	}

	t.manifest.Entries = append(t.manifest.Entries, entry)
	t.tracked[path] = true
	return t.writeManifest()
}

// Abort discards an open set whose changes were never applied, or whose
// changes were fully undone. The set directory is removed entirely, so the
// set no longer appears in Store.List.
func (t *Tx) Abort() error {
	if t.manifest.Status != StatusOpen {
		return errors.New("backup set is already committed")
	}
	if err := os.RemoveAll(t.dir); err != nil {
		return fmt.Errorf("remove backup set: %w", err)
	}
	return nil
}

// Commit records the state of every tracked file after the change and marks
// the set complete.
func (t *Tx) Commit() error {
	if t.manifest.Status != StatusOpen {
		return errors.New("backup set is already committed")
	}
	entries := make([]Entry, len(t.manifest.Entries))
	copy(entries, t.manifest.Entries)
	for i := range entries {
		e := &entries[i]
		data, err := os.ReadFile(e.Path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			e.AfterExists = false
			e.AfterSHA256 = ""
		case err != nil:
			return fmt.Errorf("read %q at commit: %w", e.Path, err)
		default:
			e.AfterExists = true
			e.AfterSHA256 = hashBytes(data)
		}
	}
	prev := t.manifest
	t.manifest.Entries = entries
	t.manifest.Status = StatusComplete
	if err := t.writeManifest(); err != nil {
		t.manifest = prev
		return err
	}
	return nil
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// writeManifest replaces manifest.json atomically: it writes a temporary file
// in the same directory and renames it over the manifest.
func (t *Tx) writeManifest() error {
	data, err := json.MarshalIndent(t.manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(t.dir, ".manifest-*.tmp")
	if err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write manifest: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := os.Rename(tmpName, filepath.Join(t.dir, manifestName)); err != nil {
		cleanup()
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}
