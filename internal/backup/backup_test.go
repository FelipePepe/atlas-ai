package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

var fixedTime = time.Date(2026, 11, 12, 10, 30, 45, 0, time.UTC)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return &Store{
		Root: filepath.Join(t.TempDir(), "backups"),
		Now:  func() time.Time { return fixedTime },
	}
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func readManifest(t *testing.T, s *Store, id string) Manifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(s.Root, id, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestBeginCreatesSet(t *testing.T) {
	s := newTestStore(t)
	tx, err := s.Begin("install")
	if err != nil {
		t.Fatal(err)
	}
	id := tx.ID()
	if !ValidID(id) {
		t.Fatalf("id %q does not match the id pattern", id)
	}
	if got := id[:16]; got != "20261112T103045Z" {
		t.Errorf("id timestamp = %q, want 20261112T103045Z", got)
	}
	dir := filepath.Join(s.Root, id)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("set directory missing: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "files")); err != nil || !fi.IsDir() {
		t.Fatalf("files directory missing: %v", err)
	}
	if runtime.GOOS != "windows" {
		if info.Mode().Perm() != 0o700 {
			t.Errorf("set dir mode = %v, want 0700", info.Mode().Perm())
		}
		mi, err := os.Stat(filepath.Join(dir, "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		if mi.Mode().Perm() != 0o600 {
			t.Errorf("manifest mode = %v, want 0600", mi.Mode().Perm())
		}
	}
	m := readManifest(t, s, id)
	if m.Version != 1 || m.ID != id || m.Label != "install" || m.Status != StatusOpen {
		t.Errorf("unexpected initial manifest: %+v", m)
	}
	if m.Created != "2026-11-12T10:30:45Z" {
		t.Errorf("created = %q", m.Created)
	}
	if len(m.Entries) != 0 {
		t.Errorf("entries = %d, want 0", len(m.Entries))
	}
}

func TestBeginIDsAreUnique(t *testing.T) {
	s := newTestStore(t)
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		tx, err := s.Begin("x")
		if err != nil {
			t.Fatal(err)
		}
		if seen[tx.ID()] {
			t.Fatalf("duplicate id %q", tx.ID())
		}
		seen[tx.ID()] = true
	}
}

func TestTrackCopiesExistingFile(t *testing.T) {
	s := newTestStore(t)
	target := filepath.Join(t.TempDir(), "settings.json")
	writeFile(t, target, "original", 0o640)

	tx, err := s.Begin("install")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Track(target); err != nil {
		t.Fatal(err)
	}
	m := readManifest(t, s, tx.ID())
	if len(m.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(m.Entries))
	}
	e := m.Entries[0]
	if e.Path != target || !e.Existed || e.Blob != "0" {
		t.Errorf("unexpected entry: %+v", e)
	}
	if e.BeforeSHA256 != sum("original") {
		t.Errorf("before hash = %q", e.BeforeSHA256)
	}
	if runtime.GOOS != "windows" && e.Mode != 0o640 {
		t.Errorf("mode = %o, want 640", e.Mode)
	}
	blob := filepath.Join(s.Root, tx.ID(), "files", "0")
	data, err := os.ReadFile(blob)
	if err != nil || string(data) != "original" {
		t.Fatalf("blob content = %q, err %v", data, err)
	}
	if runtime.GOOS != "windows" {
		bi, _ := os.Stat(blob)
		if bi.Mode().Perm() != 0o600 {
			t.Errorf("blob mode = %v, want 0600", bi.Mode().Perm())
		}
	}
}

func TestTrackMissingFile(t *testing.T) {
	s := newTestStore(t)
	target := filepath.Join(t.TempDir(), "absent.json")
	tx, _ := s.Begin("install")
	if err := tx.Track(target); err != nil {
		t.Fatal(err)
	}
	e := readManifest(t, s, tx.ID()).Entries[0]
	if e.Existed || e.Blob != "" || e.BeforeSHA256 != "" {
		t.Errorf("missing file entry should be empty: %+v", e)
	}
}

func TestTrackRejectsRelativePath(t *testing.T) {
	s := newTestStore(t)
	tx, _ := s.Begin("install")
	if err := tx.Track("relative/file.json"); err == nil {
		t.Fatal("expected error for relative path")
	}
	if n := len(readManifest(t, s, tx.ID()).Entries); n != 0 {
		t.Errorf("entries = %d, want 0", n)
	}
}

func TestTrackRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	writeFile(t, real, "x", 0o600)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	s := newTestStore(t)
	tx, _ := s.Begin("install")
	if err := tx.Track(link); err == nil {
		t.Fatal("expected error for symlink")
	}
}

func TestTrackRejectsDirectory(t *testing.T) {
	s := newTestStore(t)
	tx, _ := s.Begin("install")
	if err := tx.Track(t.TempDir()); err == nil {
		t.Fatal("expected error for directory")
	}
}

func TestTrackDuplicateIsNoOp(t *testing.T) {
	s := newTestStore(t)
	target := filepath.Join(t.TempDir(), "a.json")
	writeFile(t, target, "v1", 0o600)
	tx, _ := s.Begin("install")
	if err := tx.Track(target); err != nil {
		t.Fatal(err)
	}
	// The file changes after the first Track; the second Track must keep
	// the original snapshot.
	writeFile(t, target, "v2", 0o600)
	if err := tx.Track(target); err != nil {
		t.Fatal(err)
	}
	m := readManifest(t, s, tx.ID())
	if len(m.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(m.Entries))
	}
	if m.Entries[0].BeforeSHA256 != sum("v1") {
		t.Errorf("snapshot was replaced by duplicate Track")
	}
}

func TestManifestRewrittenAfterEachTrack(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	writeFile(t, a, "a", 0o600)
	writeFile(t, b, "b", 0o600)
	tx, _ := s.Begin("install")

	for i, p := range []string{a, b} {
		if err := tx.Track(p); err != nil {
			t.Fatal(err)
		}
		m := readManifest(t, s, tx.ID())
		if m.Status != StatusOpen {
			t.Errorf("status = %q mid-transaction, want open", m.Status)
		}
		if len(m.Entries) != i+1 {
			t.Errorf("after track %d entries = %d", i+1, len(m.Entries))
		}
	}
	// No temporary files may be left behind.
	items, _ := os.ReadDir(filepath.Join(s.Root, tx.ID()))
	for _, it := range items {
		if it.Name() != "manifest.json" && it.Name() != "files" {
			t.Errorf("unexpected leftover %q", it.Name())
		}
	}
}

func TestCommitRecordsAfterState(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	changed := filepath.Join(dir, "changed")
	created := filepath.Join(dir, "created")
	removed := filepath.Join(dir, "removed")
	writeFile(t, changed, "before", 0o600)
	writeFile(t, removed, "gone", 0o600)

	tx, _ := s.Begin("install")
	for _, p := range []string{changed, created, removed} {
		if err := tx.Track(p); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, changed, "after", 0o600)
	writeFile(t, created, "new", 0o600)
	if err := os.Remove(removed); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	m := readManifest(t, s, tx.ID())
	if m.Status != StatusComplete {
		t.Errorf("status = %q, want complete", m.Status)
	}
	if e := m.Entries[0]; !e.AfterExists || e.AfterSHA256 != sum("after") {
		t.Errorf("changed entry: %+v", e)
	}
	if e := m.Entries[1]; !e.AfterExists || e.AfterSHA256 != sum("new") || e.Existed {
		t.Errorf("created entry: %+v", e)
	}
	if e := m.Entries[2]; e.AfterExists || e.AfterSHA256 != "" || !e.Existed {
		t.Errorf("removed entry: %+v", e)
	}
}

func TestTrackAfterCommitFails(t *testing.T) {
	s := newTestStore(t)
	tx, _ := s.Begin("install")
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Track(filepath.Join(t.TempDir(), "x")); err == nil {
		t.Fatal("expected error tracking after commit")
	}
}
