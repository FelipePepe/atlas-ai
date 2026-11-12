package backup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// checkHome reports whether home can anchor confinement: it must be a
// non-empty absolute path. There is deliberately no fallback, so a missing
// or relative home fails closed instead of widening what may be touched.
func checkHome(home string) error {
	if home == "" {
		return errors.New("backup store has no home directory")
	}
	if !filepath.IsAbs(home) {
		return fmt.Errorf("backup store home %q is not absolute", home)
	}
	return nil
}

// within reports whether path is strictly below base, by lexical relation.
// filepath.Rel is used instead of a string prefix so that a sibling such as
// "/home/u-evil" is not mistaken for a child of "/home/u".
func within(base, path string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// confineLexical rejects paths that are not absolute, not clean, or not
// strictly below home, without touching the filesystem. Manifests are read
// from disk and must not be trusted: a planted entry such as "/etc/passwd"
// or "<home>/../x" would otherwise let a restore write or delete anything
// the user can reach.
func confineLexical(home, path string) error {
	if err := checkHome(home); err != nil {
		return err
	}
	if path == "" {
		return errors.New("path is empty: must be absolute")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path %q is not absolute", path)
	}
	if path != filepath.Clean(path) {
		return fmt.Errorf("path %q is not clean (absolute and normalized required)", path)
	}
	if !within(filepath.Clean(home), path) {
		return fmt.Errorf("path %q is outside the home directory %q", path, home)
	}
	return nil
}

// confine is confineLexical plus a check of the real location: the deepest
// existing ancestor of path, with symlinks resolved, must be inside the
// resolved home. This stops a symlinked parent directory from redirecting a
// write or read outside home, which checking only the last component (Lstat)
// cannot detect. The final component is not resolved; callers keep rejecting
// symlinks there with Lstat. A home that is itself a symlink is accepted
// because both sides are compared after resolution.
func confine(home, path string) error {
	if err := confineLexical(home, path); err != nil {
		return err
	}
	realHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		return fmt.Errorf("resolve home directory %q: %w", home, err)
	}
	anc := filepath.Dir(path)
	for {
		_, err := os.Lstat(anc)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("check parent of %q: %w", path, err)
		}
		next := filepath.Dir(anc)
		if next == anc {
			break
		}
		anc = next
	}
	realAnc, err := filepath.EvalSymlinks(anc)
	if err != nil {
		return fmt.Errorf("resolve parent of %q: %w", path, err)
	}
	if realAnc != realHome && !within(realHome, realAnc) {
		return fmt.Errorf("path %q is outside the home directory %q (parent resolves to %q)", path, home, realAnc)
	}
	return nil
}
