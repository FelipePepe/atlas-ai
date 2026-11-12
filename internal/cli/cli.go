// Package cli implements the atlas-ai command dispatcher.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/FelipePepe/atlas-ai/internal/platform"
)

// Env carries everything Run needs from the process, injected for testing.
type Env struct {
	Version string
	GOOS    string
	GOARCH  string
	Getenv  func(string) string
	// Stdin is read for confirmations; nil behaves like empty input.
	Stdin io.Reader
}

const usage = `Usage: atlas-ai <command> [flags]

Commands:
  help      Show this help
  version   Print the version and platform
  paths     Print the resolved home and state directories
  backups   List backup sets, newest first
  restore   Restore a backup set: restore [--yes] [--force] <id>
`

// Run executes the command in args and returns the process exit code:
// 0 on success, 1 on a runtime error, 2 on a usage error.
func Run(args []string, stdout, stderr io.Writer, env Env) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return 0
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	case "version":
		return runVersion(rest, stdout, stderr, env)
	case "paths":
		return runPaths(rest, stdout, stderr, env)
	case "backups":
		return runBackups(rest, stdout, stderr, env)
	case "restore":
		return runRestore(rest, stdout, stderr, env)
	default:
		fmt.Fprintf(stderr, "atlas-ai: unknown command %q\n\n%s", cmd, usage)
		return 2
	}
}

// parse parses the flags of one command. It returns the exit code to use and
// whether the command should stop (parse error, help request or stray args).
func parse(name string, args []string, stderr io.Writer) (int, bool) {
	fs := flag.NewFlagSet("atlas-ai "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, true
		}
		return 2, true
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "atlas-ai %s: unexpected argument %q\n", name, fs.Arg(0))
		return 2, true
	}
	return 0, false
}

func runVersion(args []string, stdout, stderr io.Writer, env Env) int {
	if code, stop := parse("version", args, stderr); stop {
		return code
	}
	fmt.Fprintf(stdout, "atlas-ai %s %s/%s\n", env.Version, env.GOOS, env.GOARCH)
	return 0
}

func runPaths(args []string, stdout, stderr io.Writer, env Env) int {
	if code, stop := parse("paths", args, stderr); stop {
		return code
	}
	p, err := platform.Resolve(env.GOOS, env.Getenv)
	if err != nil {
		fmt.Fprintf(stderr, "atlas-ai paths: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "home: %s\nstate: %s\n", p.Home, p.StateDir)
	return 0
}
