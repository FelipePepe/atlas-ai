package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/FelipePepe/atlas-ai/internal/backup"
	"github.com/FelipePepe/atlas-ai/internal/platform"
)

// backupStore resolves the backup store under the user's state directory.
func backupStore(env Env) (*backup.Store, error) {
	p, err := platform.Resolve(env.GOOS, env.Getenv)
	if err != nil {
		return nil, err
	}
	return &backup.Store{Root: filepath.Join(p.StateDir, "backups")}, nil
}

func runBackups(args []string, stdout, stderr io.Writer, env Env) int {
	if code, stop := parse("backups", args, stderr); stop {
		return code
	}
	st, err := backupStore(env)
	if err != nil {
		fmt.Fprintf(stderr, "atlas-ai backups: %v\n", err)
		return 1
	}
	sums, err := st.List()
	if len(sums) == 0 && err == nil {
		fmt.Fprintln(stdout, "no backups")
		return 0
	}
	for _, s := range sums {
		fmt.Fprintf(stdout, "%s  %s  %s  %d files  %s\n",
			s.ID, s.Created.UTC().Format("2006-01-02T15:04:05Z07:00"), s.Status, s.Files, s.Label)
	}
	if err != nil {
		fmt.Fprintf(stderr, "atlas-ai backups: %v\n", err)
		return 1
	}
	return 0
}

func runRestore(args []string, stdout, stderr io.Writer, env Env) int {
	fs := flag.NewFlagSet("atlas-ai restore", flag.ContinueOnError)
	fs.SetOutput(stderr)
	yes := fs.Bool("yes", false, "apply without asking for confirmation")
	force := fs.Bool("force", false, "overwrite files that changed since the backup")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "atlas-ai restore: expected exactly one backup id: atlas-ai restore [--yes] [--force] <id>")
		return 2
	}
	id := fs.Arg(0)
	if !backup.ValidID(id) {
		fmt.Fprintf(stderr, "atlas-ai restore: invalid backup id %q\n", id)
		return 2
	}
	st, err := backupStore(env)
	if err != nil {
		fmt.Fprintf(stderr, "atlas-ai restore: %v\n", err)
		return 1
	}
	plan, err := st.Plan(id)
	if err != nil {
		fmt.Fprintf(stderr, "atlas-ai restore: %v\n", err)
		return 1
	}

	conflicts := 0
	todo := 0
	for _, e := range plan.Entries {
		line := fmt.Sprintf("%s  %s", e.Action, e.Path)
		if e.Reason != "" {
			line += "  " + e.Reason
		}
		fmt.Fprintln(stdout, line)
		switch e.Action {
		case backup.ActionConflict:
			conflicts++
		case backup.ActionSkipUnchanged:
		default:
			todo++
		}
	}
	if conflicts == 0 && todo == 0 {
		fmt.Fprintln(stdout, "nothing to restore")
		return 0
	}
	if conflicts > 0 && !*force {
		fmt.Fprintf(stdout, "%d conflicting file(s) stay untouched; use --force to overwrite them\n", conflicts)
	}

	if !*yes && !confirm(stdout, env.Stdin) {
		fmt.Fprintln(stdout, "aborted")
		return 1
	}

	res, err := st.Restore(id, *force)
	if res != nil {
		fmt.Fprintf(stdout, "restored: %d, deleted: %d, skipped: %d, conflicts: %d\n",
			len(res.Restored), len(res.Deleted), len(res.Skipped), len(res.Conflicts))
		if res.BackupID != "" {
			fmt.Fprintf(stdout, "undo with: atlas-ai restore %s\n", res.BackupID)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "atlas-ai restore: %v\n", err)
		return 1
	}
	if len(res.Conflicts) > 0 {
		return 1
	}
	return 0
}

// confirm asks on stdout and reads one line from in; only y/yes continue.
func confirm(stdout io.Writer, in io.Reader) bool {
	fmt.Fprint(stdout, "Apply? [y/N] ")
	if in == nil {
		return false
	}
	line, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}
