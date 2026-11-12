package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// secLayout holds a store whose Home is <base>/home, plus a directory outside
// it. The backup root is outside Home too, as in real use.
type secLayout struct {
	store   *Store
	home    string
	outside string
}

func newSecLayout(t *testing.T) secLayout {
	t.Helper()
	l := secLayout{}
	l.home, l.outside = confineLayout(t)
	base := filepath.Dir(l.home)
	l.store = &Store{
		Root: filepath.Join(base, "backups"),
		Home: l.home,
		Now:  newTestStore(t).Now,
	}
	return l
}

// plant writes a self-consistent set whose only entry points at path. When
// existed is true the blob holds "orig" and the file is expected to be "new";
// otherwise the entry says the file did not exist before and was created.
func plant(t *testing.T, s *Store, id, path string, existed bool) {
	t.Helper()
	e := Entry{Path: path, Mode: 0o600, AfterExists: true, AfterSHA256: sum("new")}
	if existed {
		e.Existed = true
		e.Blob = "0"
		e.BeforeSHA256 = sum("orig")
		blobs := filepath.Join(s.Root, id, "files")
		if err := os.MkdirAll(blobs, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(blobs, "0"), []byte("orig"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeSet(t, s, id, Manifest{
		Version: 1, ID: id, Label: "planted", Created: "2026-11-12T10:30:45Z",
		Status: StatusComplete, Entries: []Entry{e},
	})
}

// setNames lists the entries of the backup root; a missing root is empty.
func setNames(t *testing.T, s *Store) []string {
	t.Helper()
	items, err := os.ReadDir(s.Root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, it := range items {
		names = append(names, it.Name())
	}
	return names
}

func TestManifestPathOutsideHome(t *testing.T) {
	tests := []struct {
		name    string
		existed bool
		// build prepares the outside path and returns it plus a check that
		// the outside world is unchanged.
		build func(t *testing.T, outside string) (string, func())
	}{
		{"restore over an existing outside file", true, func(t *testing.T, outside string) (string, func()) {
			f := filepath.Join(outside, "victim.txt")
			writeFile(t, f, "new", 0o600)
			return f, func() {
				if got := readString(t, f); got != "new" {
					t.Errorf("outside file = %q, want it untouched (new)", got)
				}
			}
		}},
		{"delete of an existing outside file", false, func(t *testing.T, outside string) (string, func()) {
			f := filepath.Join(outside, "victim.txt")
			writeFile(t, f, "new", 0o600)
			return f, func() {
				if !exists(f) {
					t.Error("outside file was deleted")
				}
				if got := readString(t, f); got != "new" {
					t.Errorf("outside file = %q, want it untouched (new)", got)
				}
			}
		}},
		{"restore creating an outside directory", true, func(t *testing.T, outside string) (string, func()) {
			dir := filepath.Join(outside, "newdir")
			return filepath.Join(dir, "f.txt"), func() {
				if exists(dir) {
					t.Error("outside directory was created")
				}
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := newSecLayout(t)
			path, unchanged := tc.build(t, l.outside)
			plant(t, l.store, idNew, path, tc.existed)
			before := setNames(t, l.store)

			if _, err := l.store.Load(idNew); err == nil {
				t.Error("Load accepted a manifest path outside home")
			}
			if _, err := l.store.Plan(idNew); err == nil {
				t.Error("Plan accepted a manifest path outside home")
			}
			for _, force := range []bool{true, false} {
				if _, err := l.store.Restore(idNew, force); err == nil {
					t.Errorf("Restore(force=%v) accepted a manifest path outside home", force)
				}
			}
			unchanged()
			after := setNames(t, l.store)
			if len(after) != len(before) {
				t.Errorf("backup root entries = %v, want %v: a failed restore must not create a set", after, before)
			}
		})
	}
}

func TestManifestWithOneBadEntryIsRejectedWhole(t *testing.T) {
	l := newSecLayout(t)
	good := filepath.Join(l.home, "good.txt")
	writeFile(t, good, "new", 0o600)
	bad := filepath.Join(l.outside, "bad.txt")
	writeFile(t, bad, "new", 0o600)
	writeSet(t, l.store, idNew, Manifest{
		Version: 1, ID: idNew, Label: "planted", Created: "2026-11-12T10:30:45Z", Status: StatusComplete,
		Entries: []Entry{
			{Path: good, AfterExists: true, AfterSHA256: sum("new")},
			{Path: bad, AfterExists: true, AfterSHA256: sum("new")},
		},
	})
	if _, err := l.store.Restore(idNew, true); err == nil {
		t.Fatal("expected an error")
	}
	if !exists(good) || !exists(bad) {
		t.Error("no file may be touched when any entry is rejected")
	}
}

func TestManifestPathWithDotDotIsRejected(t *testing.T) {
	l := newSecLayout(t)
	victim := filepath.Join(l.outside, "victim.txt")
	writeFile(t, victim, "new", 0o600)
	sneaky := l.home + string(filepath.Separator) + ".." + string(filepath.Separator) + "outside" + string(filepath.Separator) + "victim.txt"
	plant(t, l.store, idNew, sneaky, false)
	if _, err := l.store.Load(idNew); err == nil {
		t.Error("Load accepted a non-clean path")
	}
	if _, err := l.store.Restore(idNew, true); err == nil {
		t.Error("Restore accepted a non-clean path")
	}
	if !exists(victim) {
		t.Error("outside file was deleted")
	}
}

func TestRestoreThroughSymlinkedParent(t *testing.T) {
	for _, existed := range []bool{true, false} {
		name := "delete entry"
		if existed {
			name = "restore entry"
		}
		for _, force := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s force=%v", name, force), func(t *testing.T) {
				l := newSecLayout(t)
				symlinkOrSkip(t, l.outside, filepath.Join(l.home, "link"))
				victim := filepath.Join(l.outside, "f.txt")
				writeFile(t, victim, "new", 0o600)
				plant(t, l.store, idNew, filepath.Join(l.home, "link", "f.txt"), existed)
				before := setNames(t, l.store)

				if _, err := l.store.Restore(idNew, force); err == nil {
					t.Errorf("Restore(force=%v) followed a symlinked parent out of home", force)
				}
				if !exists(victim) {
					t.Fatal("outside file was deleted through the symlink")
				}
				if got := readString(t, victim); got != "new" {
					t.Errorf("outside file = %q, want it untouched (new)", got)
				}
				if after := setNames(t, l.store); len(after) != len(before) {
					t.Errorf("backup root entries = %v, want %v", after, before)
				}
			})
		}
	}
}

func TestTrackThroughSymlinkedParent(t *testing.T) {
	l := newSecLayout(t)
	symlinkOrSkip(t, l.outside, filepath.Join(l.home, "link"))
	writeFile(t, filepath.Join(l.outside, "f.txt"), "secret", 0o600)

	tx, err := l.store.Begin("install")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		filepath.Join(l.home, "link", "f.txt"),
		filepath.Join(l.home, "link", "missing", "g.txt"),
		filepath.Join(l.outside, "f.txt"),
	} {
		if err := tx.Track(p); err == nil {
			t.Errorf("Track(%q) = nil, want an error", p)
		}
	}
	blobs, err := os.ReadDir(filepath.Join(l.store.Root, tx.ID(), "files"))
	if err != nil {
		t.Fatal(err)
	}
	if len(blobs) != 0 {
		t.Errorf("%d blob(s) copied into the set, want none", len(blobs))
	}
	if n := len(readManifest(t, l.store, tx.ID()).Entries); n != 0 {
		t.Errorf("entries = %d, want 0", n)
	}
}

func TestHomeIsRequired(t *testing.T) {
	good := newSecLayout(t)
	target := filepath.Join(good.home, "f.txt")
	writeFile(t, target, "orig", 0o600)
	id := committedSet(t, good.store, []string{target}, func() { writeFile(t, target, "new", 0o600) })

	for name, home := range map[string]string{
		"empty":    "",
		"relative": filepath.Join("some", "home"),
		"dot":      ".",
	} {
		t.Run(name, func(t *testing.T) {
			writeFile(t, target, "new", 0o600) // each case starts from the post-change state
			s := &Store{Root: good.store.Root, Home: home, Now: good.store.Now}
			before := setNames(t, s)
			if tx, err := s.Begin("x"); err == nil {
				t.Errorf("Begin with Home %q = %v, want an error", home, tx)
			}
			if _, err := s.Plan(id); err == nil {
				t.Errorf("Plan with Home %q = nil, want an error", home)
			}
			if _, err := s.Restore(id, true); err == nil {
				t.Errorf("Restore with Home %q = nil, want an error", home)
			}
			if got := readString(t, target); got != "new" {
				t.Errorf("file = %q, want it untouched (new)", got)
			}
			if after := setNames(t, s); len(after) != len(before) {
				t.Errorf("backup root entries = %v, want %v", after, before)
			}
		})
	}
}
