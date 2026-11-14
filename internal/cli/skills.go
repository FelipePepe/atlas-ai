package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/FelipePepe/atlas-ai/internal/agents"
	"github.com/FelipePepe/atlas-ai/internal/install"
	"github.com/FelipePepe/atlas-ai/internal/platform"
	"github.com/FelipePepe/atlas-ai/skills"
)

// agentList collects --agent values; each may hold comma-separated ids.
type agentList []string

func (l *agentList) String() string { return strings.Join(*l, ",") }

func (l *agentList) Set(v string) error {
	for _, id := range strings.Split(v, ",") {
		if id = strings.TrimSpace(id); id != "" {
			*l = append(*l, id)
		}
	}
	return nil
}

func runInstallSkills(args []string, stdout, stderr io.Writer, env Env) int {
	fs := flag.NewFlagSet("atlas-ai install-skills", flag.ContinueOnError)
	fs.SetOutput(stderr)
	yes := fs.Bool("yes", false, "apply without asking for confirmation")
	var ids agentList
	fs.Var(&ids, "agent", "agent id(s) to install for, comma-separated or repeated (default: all)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "atlas-ai install-skills: unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	p, err := platform.Resolve(env.GOOS, env.Getenv)
	if err != nil {
		fmt.Fprintf(stderr, "atlas-ai install-skills: %v\n", err)
		return 1
	}
	dirs, err := agents.Targets(env.GOOS, p.Home, ids)
	if err != nil {
		fmt.Fprintf(stderr, "atlas-ai install-skills: %v\n", err)
		return 2
	}
	entries, err := install.Plan(skills.FS, dirs)
	if err != nil {
		fmt.Fprintf(stderr, "atlas-ai install-skills: %v\n", err)
		return 1
	}

	todo := 0
	for _, e := range entries {
		fmt.Fprintf(stdout, "%s  %s\n", e.Action, e.Path)
		if e.Action == install.Create || e.Action == install.Update {
			todo++
		}
	}
	if todo == 0 {
		fmt.Fprintln(stdout, "nothing to install")
		return 0
	}
	if !*yes && !confirm(stdout, env.Stdin) {
		fmt.Fprintln(stdout, "aborted")
		return 1
	}

	st, err := backupStore(env)
	if err != nil {
		fmt.Fprintf(stderr, "atlas-ai install-skills: %v\n", err)
		return 1
	}
	id, err := install.Apply(st, entries)
	if err != nil {
		fmt.Fprintf(stderr, "atlas-ai install-skills: %v\n", err)
		if id != "" {
			fmt.Fprintf(stdout, "partial install; undo with: atlas-ai restore %s\n", id)
		}
		return 1
	}
	fmt.Fprintf(stdout, "installed %d files\n", todo)
	fmt.Fprintf(stdout, "undo with: atlas-ai restore %s\n", id)
	return 0
}
