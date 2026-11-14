package backup

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteFileCreatesParentsAndContent(t *testing.T) {
	st := newTestStore(t)
	path := filepath.Join(t.TempDir(), "a", "b", "SKILL.md")

	if err := WriteFile(st.Home, path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "hello" {
		t.Fatalf("content = %q, err = %v", got, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
		}
	}
}

func TestWriteFileReplacesAndLeavesNoTempFile(t *testing.T) {
	st := newTestStore(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	writeFile(t, path, "old", 0o600)

	if err := WriteFile(st.Home, path, []byte("new"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Fatalf("content = %q, want new", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory has %d entries, want only the target", len(entries))
	}
}

func TestWriteFileRejectsPathOutsideHome(t *testing.T) {
	st := newTestStore(t)
	outside := filepath.Join(filepath.Dir(st.Home), "outside-home", "x.md")

	err := WriteFile(st.Home, outside, []byte("x"), 0o600)
	if err == nil {
		t.Fatal("expected an error for a path outside home")
	}
	if strings.Contains(err.Error(), "restore") {
		t.Fatalf("error text is restore-specific: %v", err)
	}
	if _, statErr := os.Stat(outside); statErr == nil {
		t.Fatal("file was written outside home")
	}
}
