package install

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"testing/fstest"
	"time"

	"github.com/FelipePepe/atlas-ai/internal/backup"
	"github.com/FelipePepe/atlas-ai/skills"
)

var testFS = fstest.MapFS{
	"alpha/SKILL.md": {Data: []byte("alpha v1\n")},
	"beta/SKILL.md":  {Data: []byte("beta v1\n")},
	"README.md":      {Data: []byte("ignored")},
}

func newTestStore(t *testing.T) *backup.Store {
	t.Helper()
	base := t.TempDir()
	return &backup.Store{
		Root: filepath.Join(base, "backups"),
		Home: filepath.Dir(base),
		Now:  func() time.Time { return time.Date(2026, 11, 12, 10, 30, 45, 0, time.UTC) },
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustPlan(t *testing.T, fsys fstest.MapFS, dirs []string) []Entry {
	t.Helper()
	entries, err := Plan(fsys, dirs)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestPlanFreshAndOrder(t *testing.T) {
	d1, d2 := t.TempDir(), t.TempDir()
	entries := mustPlan(t, testFS, []string{d1, d2})
	want := []string{
		filepath.Join(d1, "alpha", "SKILL.md"), filepath.Join(d1, "beta", "SKILL.md"),
		filepath.Join(d2, "alpha", "SKILL.md"), filepath.Join(d2, "beta", "SKILL.md"),
	}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(entries), len(want))
	}
	for i, e := range entries {
		if e.Path != want[i] || e.Action != Create {
			t.Errorf("entry %d = %s %s, want %s create", i, e.Action, e.Path, want[i])
		}
	}
	if string(entries[0].Data) != "alpha v1\n" {
		t.Errorf("data = %q", entries[0].Data)
	}
}

func TestApplyFreshThenRerun(t *testing.T) {
	st := newTestStore(t)
	dir := t.TempDir()
	id, err := Apply(st, mustPlan(t, testFS, []string{dir}))
	if err != nil || id == "" {
		t.Fatalf("Apply = %q, %v", id, err)
	}
	p := filepath.Join(dir, "alpha", "SKILL.md")
	if got := readFile(t, p); got != "alpha v1\n" {
		t.Errorf("content = %q", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v, want 0600", info.Mode().Perm())
		}
	}
	sets, err := st.List()
	if err != nil || len(sets) != 1 {
		t.Fatalf("List = %v, %v", sets, err)
	}

	// Rerun: everything unchanged, no new backup set.
	entries := mustPlan(t, testFS, []string{dir})
	for _, e := range entries {
		if e.Action != Unchanged {
			t.Errorf("%s = %s, want unchanged", e.Path, e.Action)
		}
	}
	id2, err := Apply(st, entries)
	if err != nil || id2 != "" {
		t.Fatalf("rerun Apply = %q, %v", id2, err)
	}
	if sets, _ := st.List(); len(sets) != 1 {
		t.Errorf("rerun created a backup set: %d sets", len(sets))
	}
}

func TestApplyNoopCreatesNoSet(t *testing.T) {
	st := newTestStore(t)
	id, err := Apply(st, nil)
	if err != nil || id != "" {
		t.Fatalf("Apply = %q, %v", id, err)
	}
	if sets, _ := st.List(); len(sets) != 0 {
		t.Errorf("sets = %v", sets)
	}
}

func TestUpdateBacksUpAndRestores(t *testing.T) {
	st := newTestStore(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "alpha", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("old local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries := mustPlan(t, testFS, []string{dir})
	if entries[0].Action != Update || entries[1].Action != Create {
		t.Fatalf("actions = %s, %s", entries[0].Action, entries[1].Action)
	}
	id, err := Apply(st, entries)
	if err != nil || id == "" {
		t.Fatalf("Apply = %q, %v", id, err)
	}
	if got := readFile(t, p); got != "alpha v1\n" {
		t.Fatalf("after apply = %q", got)
	}
	if _, err := st.Restore(id, false); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, p); got != "old local\n" {
		t.Errorf("after restore = %q, want old content", got)
	}
	if _, err := os.Lstat(filepath.Join(dir, "beta", "SKILL.md")); !os.IsNotExist(err) {
		t.Errorf("created file should be deleted by restore, err = %v", err)
	}
}

func TestPlanRejectsSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "alpha"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "alpha", "SKILL.md")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Plan(testFS, []string{dir}); err == nil || !bytes.Contains([]byte(err.Error()), []byte(fmt.Sprintf("%q", link))) {
		t.Fatalf("Plan err = %v, want error naming %s", err, link)
	}
	if got := readFile(t, target); got != "x" {
		t.Errorf("target modified: %q", got)
	}
}

func TestPlanRejectsSymlinkDirs(t *testing.T) {
	real := t.TempDir()
	parent := t.TempDir()

	// skillDir is a symlink.
	dir := filepath.Join(parent, "skills")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(dir, "alpha")
	if err := os.Symlink(real, skillDir); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Plan(testFS, []string{dir}); err == nil {
		t.Error("symlinked skill dir: want error")
	}

	// dir itself is a symlink.
	linkDir := filepath.Join(parent, "linked")
	if err := os.Symlink(real, linkDir); err != nil {
		t.Fatal(err)
	}
	if _, err := Plan(testFS, []string{linkDir}); err == nil {
		t.Error("symlinked dir: want error")
	}

	// dir is a regular file.
	file := filepath.Join(parent, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Plan(testFS, []string{file}); err == nil {
		t.Error("file as dir: want error")
	}
}

func TestPlanRealSkills(t *testing.T) {
	dir := t.TempDir()
	entries, err := Plan(skills.FS, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range entries {
		got[e.Path] = e.Action == Create && len(e.Data) > 0
	}
	for _, name := range []string{"session-start", "session-end", "checked-loop"} {
		if !got[filepath.Join(dir, name, "SKILL.md")] {
			t.Errorf("skill %s not planned", name)
		}
	}
}
