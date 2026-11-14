package agents

import (
	"reflect"
	"strings"
	"testing"
)

func TestAllOrder(t *testing.T) {
	var ids []string
	for _, a := range All() {
		ids = append(ids, a.ID)
	}
	want := []string{"claude-code", "copilot-cli", "copilot-vscode"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("All ids = %v, want %v", ids, want)
	}
}

func TestLookup(t *testing.T) {
	a, err := Lookup("copilot-cli")
	if err != nil || a.Name != "GitHub Copilot CLI" {
		t.Fatalf("Lookup = %+v, %v", a, err)
	}
	_, err = Lookup("vim")
	if err == nil {
		t.Fatal("unknown id: want error")
	}
	for _, id := range []string{"claude-code", "copilot-cli", "copilot-vscode"} {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("error %q should name valid id %q", err, id)
		}
	}
}

func TestSkillsDir(t *testing.T) {
	tests := []struct {
		id, goos, home, want string
	}{
		{"claude-code", "linux", "/home/u", "/home/u/.claude/skills"},
		{"copilot-cli", "linux", "/home/u", "/home/u/.copilot/skills"},
		{"copilot-vscode", "linux", "/home/u", "/home/u/.copilot/skills"},
		{"claude-code", "windows", `C:\Users\u`, `C:\Users\u\.claude\skills`},
		{"copilot-vscode", "windows", `C:\Users\u`, `C:\Users\u\.copilot\skills`},
		{"claude-code", "windows", `C:\`, `C:\.claude\skills`},
	}
	for _, tt := range tests {
		a, err := Lookup(tt.id)
		if err != nil {
			t.Fatal(err)
		}
		if got := a.SkillsDir(tt.goos, tt.home); got != tt.want {
			t.Errorf("%s/%s: got %q, want %q", tt.id, tt.goos, got, tt.want)
		}
	}
}

func TestTargets(t *testing.T) {
	tests := []struct {
		name string
		ids  []string
		want []string
		err  bool
	}{
		{"empty is all, deduped", nil, []string{"/h/.claude/skills", "/h/.copilot/skills"}, false},
		{"copilot pair collapses", []string{"copilot-cli", "copilot-vscode"}, []string{"/h/.copilot/skills"}, false},
		{"first-seen order", []string{"copilot-vscode", "claude-code", "copilot-cli"}, []string{"/h/.copilot/skills", "/h/.claude/skills"}, false},
		{"repeated id", []string{"claude-code", "claude-code"}, []string{"/h/.claude/skills"}, false},
		{"unknown id", []string{"claude-code", "nope"}, nil, true},
	}
	for _, tt := range tests {
		got, err := Targets("linux", "/h", tt.ids)
		if (err != nil) != tt.err {
			t.Fatalf("%s: err = %v", tt.name, err)
		}
		if !tt.err && !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
	got, err := Targets("windows", `C:\Users\u`, nil)
	want := []string{`C:\Users\u\.claude\skills`, `C:\Users\u\.copilot\skills`}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("windows: got %v, %v", got, err)
	}
}
