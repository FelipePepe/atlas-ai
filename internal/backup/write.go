package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// WriteFile replaces path with data through a temporary file in the same
// directory, so a failure never leaves a half-written target. The path must
// lie under home and is confined like a tracked path; missing parent
// directories are created with mode 0700. mode is ignored on Windows.
func WriteFile(home, path string, data []byte, mode os.FileMode) error {
	if err := confine(home, path); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	tmp, err := os.CreateTemp(dir, ".atlas-ai-write-*.tmp")
	if err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	tmpName := tmp.Name()
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write %q: %w", path, err)
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
		return fmt.Errorf("write %q: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("write %q: %w", path, err)
	}
	return nil
}
