package backup

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func readString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func actionOf(t *testing.T, p *Plan, path string) PlanEntry {
	t.Helper()
	for _, e := range p.Entries {
		if e.Path == path {
			return e
		}
	}
	t.Fatalf("plan has no entry for %s: %+v", path, p.Entries)
	return PlanEntry{}
}

// committedSet tracks the given paths, applies change, and commits.
func committedSet(t *testing.T, s *Store, paths []string, change func()) string {
	t.Helper()
	tx, err := s.Begin("test")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		if err := tx.Track(p); err != nil {
			t.Fatal(err)
		}
	}
	change()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return tx.ID()
}

func TestPlanActions(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	modified := filepath.Join(dir, "modified")
	created := filepath.Join(dir, "created")
	same := filepath.Join(dir, "same")
	stillAbsent := filepath.Join(dir, "absent")
	edited := filepath.Join(dir, "edited")
	writeFile(t, modified, "old", 0o600)
	writeFile(t, same, "same", 0o600)
	writeFile(t, edited, "old", 0o600)

	id := committedSet(t, s, []string{modified, created, same, stillAbsent, edited}, func() {
		writeFile(t, modified, "new", 0o600)
		writeFile(t, created, "new", 0o600)
		writeFile(t, edited, "new", 0o600)
	})
	writeFile(t, edited, "user edit", 0o600)

	p, err := s.Plan(id)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != id || p.Status != StatusComplete || len(p.Entries) != 5 {
		t.Fatalf("unexpected plan header: %+v", p)
	}
	want := map[string]Action{
		modified:    ActionRestore,
		created:     ActionDelete,
		same:        ActionSkipUnchanged,
		stillAbsent: ActionSkipUnchanged,
		edited:      ActionConflict,
	}
	for path, a := range want {
		if got := actionOf(t, p, path); got.Action != a {
			t.Errorf("%s: action = %q, want %q (%s)", path, got.Action, a, got.Reason)
		}
	}
	if actionOf(t, p, edited).Reason == "" {
		t.Error("conflict must carry a reason")
	}
}

func TestPlanConflictWhenNewFileEdited(t *testing.T) {
	s := newTestStore(t)
	created := filepath.Join(t.TempDir(), "created")
	id := committedSet(t, s, []string{created}, func() { writeFile(t, created, "v1", 0o600) })
	writeFile(t, created, "v2", 0o600)
	p, err := s.Plan(id)
	if err != nil {
		t.Fatal(err)
	}
	if got := actionOf(t, p, created).Action; got != ActionConflict {
		t.Errorf("action = %q, want conflict", got)
	}
}

func TestPlanConflictWhenFileRemovedAfterInstall(t *testing.T) {
	s := newTestStore(t)
	f := filepath.Join(t.TempDir(), "f")
	writeFile(t, f, "old", 0o600)
	id := committedSet(t, s, []string{f}, func() { writeFile(t, f, "new", 0o600) })
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}
	p, err := s.Plan(id)
	if err != nil {
		t.Fatal(err)
	}
	if got := actionOf(t, p, f).Action; got != ActionConflict {
		t.Errorf("action = %q, want conflict", got)
	}
}

func TestPlanOpenSetConflicts(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	changed := filepath.Join(dir, "changed")
	untouched := filepath.Join(dir, "untouched")
	writeFile(t, changed, "old", 0o600)
	writeFile(t, untouched, "u", 0o600)
	tx, err := s.Begin("open")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{changed, untouched} {
		if err := tx.Track(p); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, changed, "new", 0o600)

	p, err := s.Plan(tx.ID())
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != StatusOpen {
		t.Errorf("status = %q", p.Status)
	}
	if got := actionOf(t, p, changed); got.Action != ActionConflict || got.Reason == "" {
		t.Errorf("changed: %+v", got)
	}
	if got := actionOf(t, p, untouched).Action; got != ActionSkipUnchanged {
		t.Errorf("untouched: action = %q", got)
	}
}

func TestPlanRejectsBadIDs(t *testing.T) {
	s := newTestStore(t)
	for _, id := range []string{"", "..", "../x", "20261112T103045Z-zzzz", "/etc"} {
		if _, err := s.Plan(id); err == nil {
			t.Errorf("Plan(%q) succeeded", id)
		}
		if _, err := s.Restore(id, true); err == nil {
			t.Errorf("Restore(%q) succeeded", id)
		}
	}
	if exists(s.Root) {
		t.Error("invalid ids must not touch the disk")
	}
}

func TestPlanSymlinkTargetIsConflict(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	f := filepath.Join(dir, "f")
	other := filepath.Join(dir, "other")
	writeFile(t, other, "secret", 0o600)
	id := committedSet(t, s, []string{f}, func() { writeFile(t, f, "new", 0o600) })
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, f); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	p, err := s.Plan(id)
	if err != nil {
		t.Fatal(err)
	}
	if got := actionOf(t, p, f); got.Action != ActionConflict || got.Reason == "" {
		t.Errorf("symlink: %+v", got)
	}
	res, err := s.Restore(id, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(res.Conflicts, f) {
		t.Errorf("conflicts = %v", res.Conflicts)
	}
	if readString(t, other) != "secret" {
		t.Error("symlink target was modified")
	}
}

func TestRestoreEndToEnd(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing")
	created := filepath.Join(dir, "sub", "created")
	writeFile(t, existing, "original", 0o600)

	id := committedSet(t, s, []string{existing, created}, func() {
		writeFile(t, existing, "modified", 0o600)
		if err := os.MkdirAll(filepath.Dir(created), 0o700); err != nil {
			t.Fatal(err)
		}
		writeFile(t, created, "brand new", 0o600)
	})

	res, err := s.Restore(id, false)
	if err != nil {
		t.Fatal(err)
	}
	if readString(t, existing) != "original" {
		t.Error("existing file not restored")
	}
	if exists(created) {
		t.Error("created file not deleted")
	}
	if !slices.Equal(res.Restored, []string{existing}) || !slices.Equal(res.Deleted, []string{created}) {
		t.Errorf("result = %+v", res)
	}
	if len(res.Conflicts) != 0 || !ValidID(res.BackupID) {
		t.Errorf("result = %+v", res)
	}
}

func TestRestoreSkipsUnchangedWithoutPreRestoreSet(t *testing.T) {
	s := newTestStore(t)
	f := filepath.Join(t.TempDir(), "f")
	writeFile(t, f, "same", 0o600)
	id := committedSet(t, s, []string{f}, func() {})
	res, err := s.Restore(id, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.BackupID != "" || !slices.Equal(res.Skipped, []string{f}) {
		t.Errorf("result = %+v", res)
	}
	sets, err := s.List()
	if err != nil || len(sets) != 1 {
		t.Errorf("sets = %v, err = %v", sets, err)
	}
}

func TestRestoreConflictNeedsForce(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	edited := filepath.Join(dir, "edited")
	editedNew := filepath.Join(dir, "editedNew")
	writeFile(t, edited, "old", 0o600)
	id := committedSet(t, s, []string{edited, editedNew}, func() {
		writeFile(t, edited, "installed", 0o600)
		writeFile(t, editedNew, "installed", 0o600)
	})
	writeFile(t, edited, "user edit", 0o600)
	writeFile(t, editedNew, "user edit", 0o600)

	res, err := s.Restore(id, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Conflicts) != 2 || len(res.Restored) != 0 || len(res.Deleted) != 0 {
		t.Errorf("result = %+v", res)
	}
	if readString(t, edited) != "user edit" || readString(t, editedNew) != "user edit" {
		t.Error("conflicting files must be left untouched")
	}

	res, err = s.Restore(id, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Conflicts) != 0 || len(res.Restored) != 1 || len(res.Deleted) != 1 {
		t.Errorf("forced result = %+v", res)
	}
	if readString(t, edited) != "old" {
		t.Error("force did not overwrite")
	}
	if exists(editedNew) {
		t.Error("force did not delete")
	}
}

func TestRestoreCanBeUndone(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing")
	created := filepath.Join(dir, "created")
	writeFile(t, existing, "original", 0o600)
	id := committedSet(t, s, []string{existing, created}, func() {
		writeFile(t, existing, "modified", 0o600)
		writeFile(t, created, "new", 0o600)
	})

	res, err := s.Restore(id, false)
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.Load(res.BackupID)
	if err != nil {
		t.Fatal(err)
	}
	if m.Label != "restore "+id || m.Status != StatusComplete || len(m.Entries) != 2 {
		t.Errorf("pre-restore manifest = %+v", m)
	}

	undo, err := s.Restore(res.BackupID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(undo.Conflicts) != 0 {
		t.Fatalf("undo conflicts: %v", undo.Conflicts)
	}
	if readString(t, existing) != "modified" || readString(t, created) != "new" {
		t.Error("undo did not return files to their pre-restore content")
	}
}

func TestRestoreRestoresModeAndCreatesParents(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	parent := filepath.Join(dir, "gone")
	f := filepath.Join(parent, "f")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, f, "secret", 0o640)
	id := committedSet(t, s, []string{f}, func() {
		if err := os.RemoveAll(parent); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := s.Restore(id, false); err != nil {
		t.Fatal(err)
	}
	if readString(t, f) != "secret" {
		t.Error("content not restored")
	}
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Errorf("mode = %o, want 640", info.Mode().Perm())
	}
	pinfo, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if pinfo.Mode().Perm() != 0o700 {
		t.Errorf("parent mode = %o, want 700", pinfo.Mode().Perm())
	}
}

func TestRestoreRestoresModeOverExistingFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits")
	}
	s := newTestStore(t)
	f := filepath.Join(t.TempDir(), "f")
	writeFile(t, f, "old", 0o600)
	id := committedSet(t, s, []string{f}, func() {
		writeFile(t, f, "new", 0o644)
	})
	if _, err := s.Restore(id, false); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600", info.Mode().Perm())
	}
}

func TestRestoreRefusesTamperedBlob(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	writeFile(t, a, "a-old", 0o600)
	writeFile(t, b, "b-old", 0o600)
	id := committedSet(t, s, []string{a, b}, func() {
		writeFile(t, a, "a-new", 0o600)
		writeFile(t, b, "b-new", 0o600)
	})
	blob := filepath.Join(s.Root, id, "files", "1")
	if err := os.WriteFile(blob, []byte("evil"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := s.Restore(id, false)
	if err == nil {
		t.Fatal("tampered blob must fail")
	}
	if res != nil && len(res.Restored) != 0 {
		t.Errorf("nothing may be restored: %+v", res)
	}
	if readString(t, a) != "a-new" || readString(t, b) != "b-new" {
		t.Error("targets must be untouched")
	}
	sets, _ := s.List()
	if len(sets) != 1 {
		t.Errorf("no pre-restore set expected, got %d sets", len(sets))
	}
}

func TestRestoreLeavesNoTempFiles(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	f := filepath.Join(dir, "f")
	writeFile(t, f, "old", 0o600)
	id := committedSet(t, s, []string{f}, func() { writeFile(t, f, "new", 0o600) })
	if _, err := s.Restore(id, false); err != nil {
		t.Fatal(err)
	}
	items, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Errorf("unexpected files left in target directory: %v", items)
	}
}

func TestRestoreOpenSetNeedsForce(t *testing.T) {
	s := newTestStore(t)
	f := filepath.Join(t.TempDir(), "f")
	writeFile(t, f, "old", 0o600)
	tx, err := s.Begin("interrupted")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Track(f); err != nil {
		t.Fatal(err)
	}
	writeFile(t, f, "half-installed", 0o600)

	res, err := s.Restore(tx.ID(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Conflicts) != 1 || readString(t, f) != "half-installed" {
		t.Errorf("result = %+v", res)
	}
	if _, err := s.Restore(tx.ID(), true); err != nil {
		t.Fatal(err)
	}
	if readString(t, f) != "old" {
		t.Error("forced restore of an open set failed")
	}
}
