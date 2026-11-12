package backup

import (
	"os"
	"path/filepath"
	"testing"
)

// symlinkOrSkip creates link -> target, or skips the test when the platform
// does not allow it (for example Windows without privileges).
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

// confineLayout builds <base>/home and <base>/outside and returns them with
// symlinks resolved, so lexical and real paths agree.
func confineLayout(t *testing.T) (home, outside string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home = filepath.Join(base, "home")
	outside = filepath.Join(base, "outside")
	for _, d := range []string{home, outside} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return home, outside
}

func TestConfineLexical(t *testing.T) {
	home, outside := confineLayout(t)
	tests := []struct {
		name string
		path string
		ok   bool
	}{
		{"file directly in home", filepath.Join(home, "f.txt"), true},
		{"nested file, missing parents", filepath.Join(home, "a", "b", "f.txt"), true},
		{"dotdot escape", filepath.Join(home, "..", "outside", "f.txt"), false},
		{"dotdot inside the path", home + string(filepath.Separator) + "a" + string(filepath.Separator) + ".." + string(filepath.Separator) + "f.txt", false},
		{"not clean: double separator", home + string(filepath.Separator) + string(filepath.Separator) + "f.txt", false},
		{"not clean: trailing separator", filepath.Join(home, "f.txt") + string(filepath.Separator), false},
		{"not clean: dot component", home + string(filepath.Separator) + "." + string(filepath.Separator) + "f.txt", false},
		{"relative path", filepath.Join("rel", "f.txt"), false},
		{"empty path", "", false},
		{"home itself", home, false},
		{"parent of home", filepath.Dir(home), false},
		{"outside dir", filepath.Join(outside, "f.txt"), false},
		{"sibling with common prefix", home + "-evil" + string(filepath.Separator) + "f.txt", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := confine(home, tc.path)
			if tc.ok && err != nil {
				t.Errorf("confine(%q) = %v, want nil", tc.path, err)
			}
			if !tc.ok && err == nil {
				t.Errorf("confine(%q) = nil, want an error", tc.path)
			}
		})
	}
}

func TestConfineSymlinkedAncestors(t *testing.T) {
	t.Run("ancestor pointing outside is rejected", func(t *testing.T) {
		home, outside := confineLayout(t)
		symlinkOrSkip(t, outside, filepath.Join(home, "link"))
		if err := confine(home, filepath.Join(home, "link", "f.txt")); err == nil {
			t.Error("expected an error for a file below a symlink to outside")
		}
	})
	t.Run("ancestor pointing inside is accepted", func(t *testing.T) {
		home, _ := confineLayout(t)
		real := filepath.Join(home, "real")
		if err := os.MkdirAll(real, 0o700); err != nil {
			t.Fatal(err)
		}
		symlinkOrSkip(t, real, filepath.Join(home, "alias"))
		if err := confine(home, filepath.Join(home, "alias", "f.txt")); err != nil {
			t.Errorf("confine = %v, want nil", err)
		}
	})
	t.Run("missing parent below an outside symlink is rejected", func(t *testing.T) {
		// The deepest existing ancestor (home/link) is what must be
		// resolved, even though the target's own parent does not exist.
		home, outside := confineLayout(t)
		symlinkOrSkip(t, outside, filepath.Join(home, "link"))
		deep := filepath.Join(home, "link", "sub", "deeper", "f.txt")
		if err := confine(home, deep); err == nil {
			t.Error("expected an error when the deepest existing ancestor escapes home")
		}
	})
	t.Run("missing parent below a real directory is accepted", func(t *testing.T) {
		home, _ := confineLayout(t)
		if err := confine(home, filepath.Join(home, "new", "deeper", "f.txt")); err != nil {
			t.Errorf("confine = %v, want nil", err)
		}
	})
	t.Run("home that is itself a symlink", func(t *testing.T) {
		home, _ := confineLayout(t)
		alias := filepath.Join(filepath.Dir(home), "home-alias")
		symlinkOrSkip(t, home, alias)
		if err := confine(alias, filepath.Join(alias, "f.txt")); err != nil {
			t.Errorf("confine = %v, want nil", err)
		}
	})
	t.Run("a final symlink is left to the caller", func(t *testing.T) {
		// The last component keeps its own treatment (Lstat in the callers),
		// so a symlink there is not a confinement failure.
		home, outside := confineLayout(t)
		symlinkOrSkip(t, filepath.Join(outside, "target"), filepath.Join(home, "final"))
		if err := confine(home, filepath.Join(home, "final")); err != nil {
			t.Errorf("confine = %v, want nil", err)
		}
	})
}
