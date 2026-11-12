package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeSet writes a manifest for id directly, bypassing the writer API.
func writeSet(t *testing.T, s *Store, id string, m Manifest) {
	t.Helper()
	dir := filepath.Join(s.Root, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func validManifest(home, id string) Manifest {
	abs := filepath.Join(home, "target.json")
	return Manifest{
		Version: 1, ID: id, Label: "test", Created: "2026-11-12T10:30:45Z", Status: StatusComplete,
		Entries: []Entry{{
			Path: abs, Existed: true, Blob: "0", Mode: 0o600,
			BeforeSHA256: sum("a"), AfterSHA256: sum("b"), AfterExists: true,
		}},
	}
}

const (
	idOld = "20261110T080000Z-aaaa"
	idMid = "20261111T080000Z-bbbb"
	idNew = "20261112T080000Z-cccc"
)

func TestLoadRoundTrip(t *testing.T) {
	s := newTestStore(t)
	tx, _ := s.Begin("install")
	target := filepath.Join(t.TempDir(), "a")
	writeFile(t, target, "x", 0o600)
	if err := tx.Track(target); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	m, err := s.Load(tx.ID())
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != tx.ID() || m.Status != StatusComplete || len(m.Entries) != 1 {
		t.Errorf("unexpected manifest: %+v", m)
	}
}

func TestLoadAcceptsEntryWithoutBlob(t *testing.T) {
	s := newTestStore(t)
	m := validManifest(s.Home, idNew)
	m.Entries[0].Existed = false
	m.Entries[0].Blob = ""
	m.Entries[0].BeforeSHA256 = ""
	writeSet(t, s, idNew, m)
	if _, err := s.Load(idNew); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejectsBadIDs(t *testing.T) {
	// The root does not exist: a rejected id must fail before any
	// filesystem access, so the error must be about the id.
	s := &Store{Root: filepath.Join(t.TempDir(), "missing"), Home: t.TempDir()}
	for _, id := range []string{
		"", "abc", "../../x", "..", ".", idNew + "/..", "20261112T080000Z-CCCC",
		"20261112T080000Z-ccc", "20261112T080000Z-cccc ", "x" + idNew,
	} {
		_, err := s.Load(id)
		if err == nil || !strings.Contains(err.Error(), "invalid backup id") {
			t.Errorf("Load(%q) err = %v, want invalid backup id", id, err)
		}
	}
}

func TestLoadMissingManifest(t *testing.T) {
	s := newTestStore(t)
	if err := os.MkdirAll(filepath.Join(s.Root, idNew), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(idNew); err == nil {
		t.Fatal("expected error for missing manifest")
	}
}

func TestLoadRejectsInvalidJSON(t *testing.T) {
	s := newTestStore(t)
	dir := filepath.Join(s.Root, idNew)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(idNew); err == nil {
		t.Fatal("expected error for invalid json")
	}
}

func TestLoadValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Manifest)
		want   string
	}{
		{"wrong version", func(m *Manifest) { m.Version = 2 }, "version"},
		{"id mismatch", func(m *Manifest) { m.ID = idOld }, "id"},
		{"bad status", func(m *Manifest) { m.Status = "weird" }, "status"},
		{"relative path", func(m *Manifest) { m.Entries[0].Path = "rel/file" }, "absolute"},
		{"blob traversal", func(m *Manifest) { m.Entries[0].Blob = "../0" }, "blob"},
		{"blob separator", func(m *Manifest) { m.Entries[0].Blob = "a/0" }, "blob"},
		{"blob backslash", func(m *Manifest) { m.Entries[0].Blob = `a\0` }, "blob"},
		{"blob dotdot", func(m *Manifest) { m.Entries[0].Blob = ".." }, "blob"},
		{"blob letters", func(m *Manifest) { m.Entries[0].Blob = "abc" }, "blob"},
		{"blob on missing file", func(m *Manifest) { m.Entries[0].Existed = false }, "blob"},
		{"missing blob for existing file", func(m *Manifest) { m.Entries[0].Blob = "" }, "blob"},
		{"bad before hash", func(m *Manifest) { m.Entries[0].BeforeSHA256 = "zz" }, "sha256"},
		{"bad after hash", func(m *Manifest) { m.Entries[0].AfterSHA256 = strings.Repeat("A", 64) }, "sha256"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			m := validManifest(s.Home, idNew)
			tt.mutate(&m)
			writeSet(t, s, idNew, m)
			_, err := s.Load(idNew)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

func TestListNewestFirst(t *testing.T) {
	s := newTestStore(t)
	writeSet(t, s, idMid, validManifest(s.Home, idMid))
	writeSet(t, s, idNew, validManifest(s.Home, idNew))
	writeSet(t, s, idOld, validManifest(s.Home, idOld))

	got, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	for i, want := range []string{idNew, idMid, idOld} {
		if got[i].ID != want {
			t.Errorf("got[%d].ID = %q, want %q", i, got[i].ID, want)
		}
	}
	first := got[0]
	wantTime := time.Date(2026, 11, 12, 10, 30, 45, 0, time.UTC)
	if first.Label != "test" || first.Status != StatusComplete || first.Files != 1 || !first.Created.Equal(wantTime) {
		t.Errorf("unexpected summary: %+v", first)
	}
}

func TestListReportsMalformedSets(t *testing.T) {
	s := newTestStore(t)
	writeSet(t, s, idNew, validManifest(s.Home, idNew))
	// Bad directory name.
	if err := os.MkdirAll(filepath.Join(s.Root, "not-an-id"), 0o700); err != nil {
		t.Fatal(err)
	}
	// Valid name, unreadable (missing) manifest.
	if err := os.MkdirAll(filepath.Join(s.Root, idOld), 0o700); err != nil {
		t.Fatal(err)
	}
	// Valid name, invalid manifest content.
	bad := validManifest(s.Home, idMid)
	bad.Version = 9
	writeSet(t, s, idMid, bad)

	got, err := s.List()
	if err == nil {
		t.Fatal("expected a joined error for malformed sets")
	}
	if len(got) != 1 || got[0].ID != idNew {
		t.Fatalf("valid summaries = %+v, want only %s", got, idNew)
	}
	for _, name := range []string{"not-an-id", idOld, idMid} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error does not mention %q: %v", name, err)
		}
	}
}

func TestListReportsStrayFile(t *testing.T) {
	s := newTestStore(t)
	if err := os.MkdirAll(s.Root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Root, "stray.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := s.List()
	if err == nil || !strings.Contains(err.Error(), "stray.txt") {
		t.Errorf("err = %v, want stray.txt reported", err)
	}
}

func TestListEmptyAndMissingRoot(t *testing.T) {
	s := newTestStore(t)
	got, err := s.List()
	if err != nil || len(got) != 0 {
		t.Errorf("missing root: got %v, err %v", got, err)
	}
	if err := os.MkdirAll(s.Root, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err = s.List()
	if err != nil || len(got) != 0 {
		t.Errorf("empty root: got %v, err %v", got, err)
	}
}
