package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/FelipePepe/atlas-ai/internal/platform"
)

var skillNames = []string{"session-start", "session-end", "checked-loop"}

func homeOf(t *testing.T, env Env) string {
	t.Helper()
	p, err := platform.Resolve(env.GOOS, env.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	return p.Home
}

func skillPath(home, agentDir, name string) string {
	return filepath.Join(home, agentDir, "skills", name, "SKILL.md")
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestInstallSkillsAbortsOnEmptyStdin(t *testing.T) {
	env, _ := backupEnv(t)
	code, out, _ := runIn(t, env, strings.NewReader(""), "install-skills")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(out, "create  ") || !strings.Contains(out, "aborted") {
		t.Errorf("stdout %q should show plan and aborted", out)
	}
	if exists(skillPath(homeOf(t, env), ".claude", "session-start")) {
		t.Error("nothing must be written when aborted")
	}
}

func TestInstallSkillsYes(t *testing.T) {
	env, _ := backupEnv(t)
	code, out, errs := run(t, env, "install-skills", "--yes")
	if code != 0 {
		t.Fatalf("exit %d (stderr %q)", code, errs)
	}
	home := homeOf(t, env)
	for _, dir := range []string{".claude", ".copilot"} {
		for _, n := range skillNames {
			if !exists(skillPath(home, dir, n)) {
				t.Errorf("missing %s/%s", dir, n)
			}
		}
	}
	for _, want := range []string{"installed 6 files", "undo with: atlas-ai restore "} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout %q missing %q", out, want)
		}
	}
}

func TestInstallSkillsSingleAgent(t *testing.T) {
	env, _ := backupEnv(t)
	code, _, errs := run(t, env, "install-skills", "--yes", "--agent", "claude-code")
	if code != 0 {
		t.Fatalf("exit %d (stderr %q)", code, errs)
	}
	home := homeOf(t, env)
	if !exists(skillPath(home, ".claude", "checked-loop")) {
		t.Error("claude skills missing")
	}
	if exists(filepath.Join(home, ".copilot")) {
		t.Error(".copilot must not exist")
	}
}

func TestInstallSkillsSharedDir(t *testing.T) {
	for _, args := range [][]string{
		{"install-skills", "--yes", "--agent", "copilot-cli,copilot-vscode"},
		{"install-skills", "--yes", "--agent", "copilot-cli", "--agent", "copilot-vscode"},
	} {
		env, _ := backupEnv(t)
		code, out, errs := run(t, env, args...)
		if code != 0 || !strings.Contains(out, "installed 3 files") {
			t.Errorf("%v: exit %d out %q err %q", args, code, out, errs)
		}
	}
}

func TestInstallSkillsRerunIsNoop(t *testing.T) {
	env, _ := backupEnv(t)
	if code, _, errs := run(t, env, "install-skills", "--yes"); code != 0 {
		t.Fatalf("first run exit %d: %s", code, errs)
	}
	code, out, _ := run(t, env, "install-skills")
	if code != 0 || !strings.Contains(out, "nothing to install") {
		t.Errorf("rerun: exit %d out %q", code, out)
	}
	_, list, _ := run(t, env, "backups")
	if n := len(strings.Split(strings.TrimSpace(list), "\n")); n != 1 {
		t.Errorf("backups lists %d sets, want 1: %q", n, list)
	}
}

func TestInstallSkillsUnknownAgent(t *testing.T) {
	env, _ := backupEnv(t)
	code, _, errs := run(t, env, "install-skills", "--yes", "--agent", "nope")
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	for _, id := range []string{"claude-code", "copilot-cli", "copilot-vscode"} {
		if !strings.Contains(errs, id) {
			t.Errorf("stderr %q should list %q", errs, id)
		}
	}
}

func TestInstallSkillsUsageErrors(t *testing.T) {
	env, _ := backupEnv(t)
	code, _, errs := run(t, env, "install-skills", "stray")
	if code != 2 || !strings.Contains(errs, `unexpected argument "stray"`) {
		t.Errorf("stray: exit %d stderr %q", code, errs)
	}
	if code, _, _ := run(t, env, "install-skills", "--bogus"); code != 2 {
		t.Errorf("bad flag: exit %d, want 2", code)
	}
	if code, _, _ := run(t, env, "install-skills", "-h"); code != 0 {
		t.Errorf("-h: exit %d, want 0", code)
	}
}

func TestInstallSkillsConfirmYes(t *testing.T) {
	env, _ := backupEnv(t)
	code, _, errs := runIn(t, env, strings.NewReader("y\n"), "install-skills", "--agent", "claude-code")
	if code != 0 {
		t.Fatalf("exit %d (stderr %q)", code, errs)
	}
	if !exists(skillPath(homeOf(t, env), ".claude", "session-end")) {
		t.Error("skill not installed after confirming")
	}
}

func TestInstallSkillsRestoreRoundTrip(t *testing.T) {
	env, _ := backupEnv(t)
	_, out, _ := run(t, env, "install-skills", "--yes")
	m := regexp.MustCompile(`undo with: atlas-ai restore (\S+)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no undo line in %q", out)
	}
	if code, o, e := run(t, env, "restore", "--yes", m[1]); code != 0 {
		t.Fatalf("restore exit %d out %q err %q", code, o, e)
	}
	home := homeOf(t, env)
	for _, dir := range []string{".claude", ".copilot"} {
		for _, n := range skillNames {
			if exists(skillPath(home, dir, n)) {
				t.Errorf("%s/%s should be gone after restore", dir, n)
			}
		}
	}
}
