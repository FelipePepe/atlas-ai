package cli

import (
	"bytes"
	"strings"
	"testing"
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
