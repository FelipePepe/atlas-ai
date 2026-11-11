package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

var (
	blobPattern = regexp.MustCompile(`^[0-9]+$`)
	hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Summary is the listing view of one backup set.
type Summary struct {
	ID      string
	Label   string
	Created time.Time
	Status  string
	Files   int
}

// Load reads and validates the manifest of set id. The id is checked before
// any filesystem access, and nothing read from disk is trusted until it
// passes validation.
func (s *Store) Load(id string) (*Manifest, error) {
	if !ValidID(id) {
		return nil, fmt.Errorf("invalid backup id %q", id)
	}
	data, err := os.ReadFile(filepath.Join(s.Root, id, manifestName))
	if err != nil {
		return nil, fmt.Errorf("read manifest of %s: %w", id, err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest of %s: %w", id, err)
	}
	if err := validate(&m, id); err != nil {
		return nil, fmt.Errorf("invalid manifest of %s: %w", id, err)
	}
	return &m, nil
}

func validate(m *Manifest, id string) error {
	if m.Version != ManifestVersion {
		return fmt.Errorf("unsupported version %d", m.Version)
	}
	if m.ID != id {
		return fmt.Errorf("id %q does not match the set directory", m.ID)
	}
	if m.Status != StatusOpen && m.Status != StatusComplete {
		return fmt.Errorf("unknown status %q", m.Status)
	}
	if _, err := time.Parse(time.RFC3339, m.Created); err != nil {
		return fmt.Errorf("bad created time: %w", err)
	}
	for i, e := range m.Entries {
		if !filepath.IsAbs(e.Path) {
			return fmt.Errorf("entry %d: path %q is not absolute", i, e.Path)
		}
		if e.Existed {
			if !blobPattern.MatchString(e.Blob) {
				return fmt.Errorf("entry %d: bad blob name %q", i, e.Blob)
			}
		} else if e.Blob != "" {
			return fmt.Errorf("entry %d: blob %q set for a file that did not exist", i, e.Blob)
		}
		if e.BeforeSHA256 != "" && !hashPattern.MatchString(e.BeforeSHA256) {
			return fmt.Errorf("entry %d: bad before_sha256", i)
		}
		if e.AfterSHA256 != "" && !hashPattern.MatchString(e.AfterSHA256) {
			return fmt.Errorf("entry %d: bad after_sha256", i)
		}
	}
	return nil
}

// List returns the backup sets, newest first. A set that cannot be read or
// validated is never skipped silently: the valid summaries are returned
// together with an error that describes every bad entry. A missing root
// yields an empty list and no error.
func (s *Store) List() ([]Summary, error) {
	items, err := os.ReadDir(s.Root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read backup root: %w", err)
	}
	var (
		out  []Summary
		errs []error
	)
	for _, it := range items {
		name := it.Name()
		if !it.IsDir() || !ValidID(name) {
			errs = append(errs, fmt.Errorf("unexpected entry %q in backup root", name))
			continue
		}
		m, err := s.Load(name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		created, _ := time.Parse(time.RFC3339, m.Created) // validated by Load
		out = append(out, Summary{
			ID:      m.ID,
			Label:   m.Label,
			Created: created,
			Status:  m.Status,
			Files:   len(m.Entries),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, errors.Join(errs...)
}
