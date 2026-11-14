// Package agents describes the AI coding agents atlas-ai installs skills
// into and where each one keeps them.
//
// Paths are built for the target OS name, never the host's, so both
// supported platforms can be tested on any host.
package agents

import (
	"fmt"
	"path"
	"strings"
)

// Agent is a supported coding agent.
type Agent struct {
	// ID is the stable identifier used on the command line.
	ID string
	// Name is the human-readable name.
	Name string
	// dir is the skills directory relative to the user's home.
	dir []string
}

var all = []Agent{
	{ID: "claude-code", Name: "Claude Code", dir: []string{".claude", "skills"}},
	{ID: "copilot-cli", Name: "GitHub Copilot CLI", dir: []string{".copilot", "skills"}},
	{ID: "copilot-vscode", Name: "GitHub Copilot in VS Code", dir: []string{".copilot", "skills"}},
}

// All returns every supported agent in a stable order.
func All() []Agent {
	out := make([]Agent, len(all))
	copy(out, all)
	return out
}

// Lookup returns the agent with the given id.
func Lookup(id string) (Agent, error) {
	for _, a := range all {
		if a.ID == id {
			return a, nil
		}
	}
	ids := make([]string, len(all))
	for i, a := range all {
		ids[i] = a.ID
	}
	return Agent{}, fmt.Errorf("unknown agent %q (valid: %s)", id, strings.Join(ids, ", "))
}

// SkillsDir returns the agent's skills directory under home using the path
// rules of goos. home must already have no trailing separator.
func (a Agent) SkillsDir(goos, home string) string {
	if goos == "windows" {
		if strings.HasSuffix(home, `\`) || strings.HasSuffix(home, "/") {
			return home + strings.Join(a.dir, `\`)
		}
		return home + `\` + strings.Join(a.dir, `\`)
	}
	return path.Join(append([]string{home}, a.dir...)...)
}

// Targets resolves ids (all agents when empty) to their unique skills
// directories in first-seen order, so agents sharing a directory collapse.
func Targets(goos, home string, ids []string) ([]string, error) {
	var list []Agent
	if len(ids) == 0 {
		list = all
	} else {
		for _, id := range ids {
			a, err := Lookup(id)
			if err != nil {
				return nil, err
			}
			list = append(list, a)
		}
	}
	seen := map[string]bool{}
	var dirs []string
	for _, a := range list {
		d := a.SkillsDir(goos, home)
		if !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	return dirs, nil
}
