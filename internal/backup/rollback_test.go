package backup

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// skipIfPermissionsIgnored skips tests that force a failure with a read-only
// directory: Windows does not enforce permission bits and root ignores them.
func skipIfPermissionsIgnored(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root; read-only directories do not block writes")
	}
}

// makeReadOnly makes dir read-only for the rest of the test, restoring its
// mode during cleanup.
func makeReadOnly(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, prev.Mode().Perm()) })
}

func TestRestoreRollsBackWhenALaterFileFails(t *testing.T) {
	skipIfPermissionsIgnored(t)
	s := newTestStore(t)
	base := t.TempDir()
	f1 := filepath.Join(base, "a", "f1")
	f2 := filepath.Join(base, "b", "f2")
	f3 := filepath.Join(base, "c", "f3")
	for _, f := range []string{f1, f2, f3} {
		if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, f1, "one-old", 0o600)
	writeFile(t, f2, "two-old", 0o600)
	writeFile(t, f3, "three-old", 0o600)
	id := committedSet(t, s, []string{f1, f2, f3}, func() {
		writeFile(t, f1, "one-new", 0o644)
		writeFile(t, f2, "two-new", 0o640)
		writeFile(t, f3, "three-new", 0o600)
	})
	// writeAtomic cannot create its temp file in f3's directory, so the
	// third op fails after the first two have been applied.
	makeReadOnly(t, filepath.Dir(f3))

	res, err := s.Restore(id, false)
	if err == nil {
		t.Fatal("restore must fail")
	}
	if !strings.Contains(err.Error(), "rolled back") {
		t.Errorf("error = %q, want it to mention the rollback", err)
	}
	if res == nil {
		t.Fatal("Restore must return the partial result")
	}
	if got := readString(t, f1); got != "one-new" {
		t.Errorf("f1 = %q, want the pre-restore content %q", got, "one-new")
	}
	if got := readString(t, f2); got != "two-new" {
		t.Errorf("f2 = %q, want the pre-restore content %q", got, "two-new")
	}
	for path, want := range map[string]os.FileMode{f1: 0o644, f2: 0o640} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s mode = %o, want the pre-restore mode %o", path, info.Mode().Perm(), want)
		}
	}
	if got := readString(t, f3); got != "three-new" {
		t.Errorf("f3 = %q, want it untouched at %q", got, "three-new")
	}
	if len(res.Restored) != 0 || len(res.Deleted) != 0 {
		t.Errorf("result = %+v, want no Restored or Deleted", res)
	}
	if res.BackupID != "" {
		t.Errorf("BackupID = %q, want empty: no set may be left in the store", res.BackupID)
	}
	sets, err := s.List()
	if err != nil || len(sets) != 1 {
		t.Errorf("sets = %d, err = %v, want only the original set", len(sets), err)
	}
}

func TestRestoreRollsBackADeletedFile(t *testing.T) {
	skipIfPermissionsIgnored(t)
	s := newTestStore(t)
	base := t.TempDir()
	created := filepath.Join(base, "created")
	f2 := filepath.Join(base, "locked", "f")
	if err := os.MkdirAll(filepath.Dir(f2), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, f2, "f2-old", 0o600)
	// The first op deletes `created` (absent before the change) and the
	// second one fails, so the rollback must bring the file back.
	id := committedSet(t, s, []string{created, f2}, func() {
		writeFile(t, created, "made during the change", 0o600)
		writeFile(t, f2, "f2-new", 0o600)
	})
	makeReadOnly(t, filepath.Dir(f2))

	res, err := s.Restore(id, false)
	if err == nil {
		t.Fatal("restore must fail")
	}
	if !strings.Contains(err.Error(), "rolled back") {
		t.Errorf("error = %q, want it to mention the rollback", err)
	}
	if got := readString(t, created); got != "made during the change" {
		t.Errorf("created = %q, want it back with its pre-restore content", got)
	}
	if res != nil && (len(res.Restored) != 0 || len(res.Deleted) != 0 || res.BackupID != "") {
		t.Errorf("result = %+v, want empty lists and BackupID", res)
	}
	sets, err := s.List()
	if err != nil || len(sets) != 1 {
		t.Errorf("sets = %d, err = %v, want only the original set", len(sets), err)
	}
}

func TestRestoreTrackFailureLeavesNoOpenSet(t *testing.T) {
	s := newTestStore(t)
	base := t.TempDir()
	f := filepath.Join(base, "f")
	writeFile(t, f, "old", 0o600)
	id := committedSet(t, s, []string{f}, func() { writeFile(t, f, "installed", 0o600) })
	// A tracked file that became a symlink is a conflict; forcing the
	// restore makes Track reject it after the open set was created.
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "target"), f); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	sets, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	before := len(sets)

	if _, err := s.Restore(id, true); err == nil {
		t.Fatal("restoring over a symlink must fail")
	}

	sets, err = s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != before {
		t.Fatalf("sets = %d, want %d: the abandoned set must be removed", len(sets), before)
	}
	for _, sum := range sets {
		if sum.Status == StatusOpen {
			t.Errorf("set %s left open", sum.ID)
		}
	}
}
