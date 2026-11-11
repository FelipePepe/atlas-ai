package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/FelipePepe/atlas-ai/internal/backup"
	"github.com/FelipePepe/atlas-ai/internal/platform"
)

func testEnv(vars map[string]string) Env {
	return Env{
		Version: "1.2.3",
		GOOS:    "linux",
		GOARCH:  "amd64",
		Getenv:  func(k string) string { return vars[k] },
	}
}

func run(t *testing.T, env Env, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(args, &out, &errb, env)
	return code, out.String(), errb.String()
}

func TestHelp(t *testing.T) {
	cases := [][]string{nil, {"help"}, {"-h"}, {"--help"}}
	for _, args := range cases {
		code, out, errs := run(t, testEnv(nil), args...)
		if code != 0 {
			t.Errorf("%v: exit %d, want 0", args, code)
		}
		if !strings.Contains(out, "Usage:") || !strings.Contains(out, "version") || !strings.Contains(out, "paths") {
			t.Errorf("%v: usage missing from stdout: %q", args, out)
		}
		if errs != "" {
			t.Errorf("%v: unexpected stderr: %q", args, errs)
		}
	}
}

func TestVersion(t *testing.T) {
	code, out, errs := run(t, testEnv(nil), "version")
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if out != "atlas-ai 1.2.3 linux/amd64\n" {
		t.Errorf("stdout = %q", out)
	}
	if errs != "" {
		t.Errorf("unexpected stderr: %q", errs)
	}
}

func TestPaths(t *testing.T) {
	env := testEnv(map[string]string{"HOME": "/home/ana"})
	code, out, errs := run(t, env, "paths")
	if code != 0 {
		t.Fatalf("exit %d, want 0 (stderr %q)", code, errs)
	}
	for _, want := range []string{"home: /home/ana\n", "state: /home/ana/.local/state/atlas-ai\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout %q missing %q", out, want)
		}
	}
}

func TestPathsResolveError(t *testing.T) {
	code, out, errs := run(t, testEnv(nil), "paths")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if out != "" {
		t.Errorf("unexpected stdout: %q", out)
	}
	if !strings.Contains(errs, "HOME") {
		t.Errorf("stderr %q should mention HOME", errs)
	}
}

func TestUnknownCommand(t *testing.T) {
	code, out, errs := run(t, testEnv(nil), "frobnicate")
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if out != "" {
		t.Errorf("unexpected stdout: %q", out)
	}
	if !strings.Contains(errs, "frobnicate") || !strings.Contains(errs, "Usage:") {
		t.Errorf("stderr %q should name the command and show usage", errs)
	}
}

func TestBadFlag(t *testing.T) {
	for _, cmd := range []string{"version", "paths"} {
		code, out, errs := run(t, testEnv(map[string]string{"HOME": "/h"}), cmd, "--nope")
		if code != 2 {
			t.Errorf("%s: exit %d, want 2", cmd, code)
		}
		if out != "" {
			t.Errorf("%s: unexpected stdout: %q", cmd, out)
		}
		if !strings.Contains(errs, "nope") {
			t.Errorf("%s: stderr %q should mention the bad flag", cmd, errs)
		}
	}
}

func TestCommandHelpFlag(t *testing.T) {
	code, out, _ := run(t, testEnv(nil), "version", "-h")
	if code != 0 {
		t.Errorf("exit %d, want 0", code)
	}
	if out != "" {
		t.Errorf("unexpected stdout: %q", out)
	}
}

func TestUnexpectedArgument(t *testing.T) {
	code, _, errs := run(t, testEnv(nil), "version", "extra")
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(errs, "extra") {
		t.Errorf("stderr %q should mention the argument", errs)
	}
}

// backupEnv returns an Env whose state directory lives in a temp dir for the
// host OS, plus the backup store the CLI will resolve.
func backupEnv(t *testing.T) (Env, *backup.Store) {
	t.Helper()
	vars := map[string]string{}
	if runtime.GOOS == "windows" {
		vars["USERPROFILE"] = t.TempDir()
		vars["LOCALAPPDATA"] = t.TempDir()
	} else {
		vars["HOME"] = t.TempDir()
		vars["XDG_STATE_HOME"] = t.TempDir()
	}
	env := Env{Version: "1.2.3", GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Getenv: func(k string) string { return vars[k] }}
	p, err := platform.Resolve(env.GOOS, env.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	return env, &backup.Store{Root: filepath.Join(p.StateDir, "backups")}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
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

// commitSet records a committed set that changed target from "orig" to "new"
// and created extra (absent before). It returns the set id and both paths.
func commitSet(t *testing.T, st *backup.Store, at time.Time, label string) (id, target, extra string) {
	t.Helper()
	dir := t.TempDir()
	target = filepath.Join(dir, "target.txt")
	extra = filepath.Join(dir, "extra.txt")
	writeFile(t, target, "orig")
	st.Now = func() time.Time { return at }
	tx, err := st.Begin(label)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Track(target); err != nil {
		t.Fatal(err)
	}
	if err := tx.Track(extra); err != nil {
		t.Fatal(err)
	}
	writeFile(t, target, "new")
	writeFile(t, extra, "created")
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return tx.ID(), target, extra
}

func runIn(t *testing.T, env Env, stdin io.Reader, args ...string) (int, string, string) {
	t.Helper()
	env.Stdin = stdin
	return run(t, env, args...)
}

func TestHelpListsBackupCommands(t *testing.T) {
	_, out, _ := run(t, testEnv(nil), "help")
	if !strings.Contains(out, "backups") || !strings.Contains(out, "restore") {
		t.Errorf("usage missing backup commands: %q", out)
	}
}

func TestBackupsEmpty(t *testing.T) {
	env, _ := backupEnv(t)
	code, out, errs := run(t, env, "backups")
	if code != 0 || out != "no backups\n" || errs != "" {
		t.Errorf("code %d out %q err %q", code, out, errs)
	}
}

func TestBackupsBadArgs(t *testing.T) {
	env, _ := backupEnv(t)
	if code, _, _ := run(t, env, "backups", "extra"); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
}

func TestBackupsResolveError(t *testing.T) {
	env := testEnv(nil)
	if runtime.GOOS == "windows" {
		env.GOOS = "windows"
	}
	code, _, errs := run(t, env, "backups")
	if code != 1 || errs == "" {
		t.Errorf("code %d err %q", code, errs)
	}
}

func TestBackupsNewestFirst(t *testing.T) {
	env, st := backupEnv(t)
	old, _, _ := commitSet(t, st, time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC), "first")
	recent, _, _ := commitSet(t, st, time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC), "second")
	code, out, errs := run(t, env, "backups")
	if code != 0 || errs != "" {
		t.Fatalf("code %d err %q", code, errs)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %q", lines)
	}
	if !strings.HasPrefix(lines[0], recent) || !strings.HasPrefix(lines[1], old) {
		t.Errorf("not newest first: %q", lines)
	}
	for _, want := range []string{"2026-02-01T10:00:00Z", "complete", "2 files", "second"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("line %q missing %q", lines[0], want)
		}
	}
}

func TestBackupsMalformedSet(t *testing.T) {
	env, st := backupEnv(t)
	good, _, _ := commitSet(t, st, time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC), "good")
	bad := filepath.Join(st.Root, "20260102T100000Z-abcd")
	if err := os.MkdirAll(bad, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(bad, "manifest.json"), "{not json")
	code, out, errs := run(t, env, "backups")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if !strings.Contains(out, good) {
		t.Errorf("valid set not printed: %q", out)
	}
	if !strings.Contains(errs, "20260102T100000Z-abcd") {
		t.Errorf("stderr should describe the bad set: %q", errs)
	}
}

func TestRestoreUsageErrors(t *testing.T) {
	env, st := backupEnv(t)
	id, target, extra := commitSet(t, st, time.Now(), "x")
	cases := map[string][]string{
		"no id":        {"restore"},
		"two ids":      {"restore", id, id},
		"unknown flag": {"restore", "--nope", id},
		"traversal":    {"restore", "../../x"},
		"flags only":   {"restore", "--yes", "--force"},
	}
	for name, args := range cases {
		code, out, errs := runIn(t, env, strings.NewReader("y\n"), args...)
		if code != 2 {
			t.Errorf("%s: exit %d, want 2 (out %q err %q)", name, code, out, errs)
		}
		if errs == "" {
			t.Errorf("%s: expected a message on stderr", name)
		}
	}
	if readFile(t, target) != "new" || readFile(t, extra) != "created" {
		t.Error("usage errors must not touch files")
	}
}

func TestRestoreHelpFlag(t *testing.T) {
	env, _ := backupEnv(t)
	if code, _, _ := run(t, env, "restore", "-h"); code != 0 {
		t.Errorf("exit %d, want 0", code)
	}
}

func TestRestoreAborts(t *testing.T) {
	answers := map[string]io.Reader{
		"n":     strings.NewReader("n\n"),
		"empty": strings.NewReader("\n"),
		"eof":   strings.NewReader(""),
		"nil":   nil,
	}
	for name, in := range answers {
		env, st := backupEnv(t)
		id, target, extra := commitSet(t, st, time.Now(), "x")
		code, out, _ := runIn(t, env, in, "restore", id)
		if code != 1 {
			t.Errorf("%s: exit %d, want 1", name, code)
		}
		for _, want := range []string{"restore", "delete", target, extra, "Apply? [y/N] ", "aborted"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: stdout %q missing %q", name, out, want)
			}
		}
		if readFile(t, target) != "new" || readFile(t, extra) != "created" {
			t.Errorf("%s: files were touched", name)
		}
	}
}

func TestRestoreApplies(t *testing.T) {
	for name, args := range map[string]struct {
		in   string
		flag bool
	}{"yes flag": {"", true}, "y": {"y\n", false}, "YES": {" YES \n", false}} {
		env, st := backupEnv(t)
		id, target, extra := commitSet(t, st, time.Now(), "x")
		a := []string{"restore", id}
		if args.flag {
			a = []string{"restore", "--yes", id}
		}
		code, out, errs := runIn(t, env, strings.NewReader(args.in), a...)
		if code != 0 {
			t.Fatalf("%s: exit %d err %q out %q", name, code, errs, out)
		}
		if readFile(t, target) != "orig" {
			t.Errorf("%s: target not restored", name)
		}
		if _, err := os.Stat(extra); !os.IsNotExist(err) {
			t.Errorf("%s: extra should be deleted (err %v)", name, err)
		}
		if !strings.Contains(out, "restored: 1, deleted: 1, skipped: 0, conflicts: 0") ||
			!strings.Contains(out, "undo with: atlas-ai restore ") {
			t.Errorf("%s: summary missing: %q", name, out)
		}
	}
}

func TestRestoreFlagsAfterID(t *testing.T) {
	env, st := backupEnv(t)
	id, target, _ := commitSet(t, st, time.Now(), "x")
	// The documented form puts flags first; flags after the id are not parsed
	// as flags, so the extra token is a second positional argument.
	if code, _, _ := runIn(t, env, nil, "restore", id, "--yes"); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if readFile(t, target) != "new" {
		t.Error("file touched")
	}
}

func TestRestoreConflict(t *testing.T) {
	env, st := backupEnv(t)
	id, target, extra := commitSet(t, st, time.Now(), "x")
	writeFile(t, target, "edited by someone")
	code, out, _ := runIn(t, env, nil, "restore", "--yes", id)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	for _, want := range []string{"conflict", target, "--force", "conflicts: 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout %q missing %q", out, want)
		}
	}
	if readFile(t, target) != "edited by someone" {
		t.Error("conflicting file must stay untouched")
	}
	if _, err := os.Stat(extra); !os.IsNotExist(err) {
		t.Error("non-conflicting entries are still restored")
	}
}

func TestRestoreForce(t *testing.T) {
	env, st := backupEnv(t)
	id, target, _ := commitSet(t, st, time.Now(), "x")
	writeFile(t, target, "edited by someone")
	code, out, errs := runIn(t, env, nil, "restore", "--yes", "--force", id)
	if code != 0 {
		t.Fatalf("exit %d err %q", code, errs)
	}
	if readFile(t, target) != "orig" {
		t.Error("force should overwrite")
	}
	if !strings.Contains(out, "undo with: atlas-ai restore ") {
		t.Errorf("undo id missing: %q", out)
	}
}

func TestRestoreNothingToDo(t *testing.T) {
	env, st := backupEnv(t)
	id, target, extra := commitSet(t, st, time.Now(), "x")
	writeFile(t, target, "orig")
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runIn(t, env, nil, "restore", id)
	if code != 0 || !strings.Contains(out, "nothing to restore") {
		t.Errorf("code %d out %q", code, out)
	}
	if strings.Contains(out, "Apply?") {
		t.Error("must not prompt when there is nothing to do")
	}
}

func TestRestoreUnknownSet(t *testing.T) {
	env, _ := backupEnv(t)
	code, _, errs := runIn(t, env, nil, "restore", "20260101T000000Z-abcd")
	if code != 1 || errs == "" {
		t.Errorf("code %d err %q", code, errs)
	}
}
