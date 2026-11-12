# atlas-ai

An installer that sets up a complete AI-assisted development workflow on Linux and Windows:
an Atlas knowledge vault, persistent memory, a session logbook, a code knowledge graph,
token-saving tools, skills, hooks and MCP server configuration for your coding agents.

## Status

Early development. The repository contains the agent-neutral skills with the contract test
that guards them, and the skeleton of the `atlas-ai` command. The installer itself does not
exist yet: the command reports its version and paths, and lists and restores backup sets.

## Usage

```text
atlas-ai help       Show the usage
atlas-ai version    Print the version and platform
atlas-ai paths      Print the home and state directories atlas-ai uses
atlas-ai backups    List the backup sets, newest first
atlas-ai restore [--yes] [--force] <id>
                    Restore the files of a backup set
```

`paths` resolves the directories from the environment of the current platform:

| Platform | Home | State directory |
|---|---|---|
| Linux | `$HOME` | `$XDG_STATE_HOME/atlas-ai`, or `$HOME/.local/state/atlas-ai` |
| Windows | `%USERPROFILE%` | `%LOCALAPPDATA%\atlas-ai` |

Empty or relative values are rejected; atlas-ai never falls back to the current directory.

### Backups and restore

Before atlas-ai changes a file it records a backup set under `<state directory>/backups/<id>/`:
a `manifest.json` plus a copy of every file as it was. Directories are created with mode `0700`
and files with `0600`. Symlinks and non-regular files are never backed up or followed.

`restore` prints what it will do and asks `Apply? [y/N]`. Anything other than `y` or `yes`,
including end of input, aborts without changing anything; `--yes` skips the question.
For each file the plan is one of:

| Action | Meaning |
|---|---|
| `restore` | Put the original content back |
| `delete` | The file did not exist before the install; remove it |
| `skip-unchanged` | The file already has the original content |
| `conflict` | The file changed after atlas-ai wrote it; it is left untouched |

Flags go before the id. `--force` overwrites conflicting files. Every restore first records
its own backup set, labelled `restore <id>`, so a restore can be undone the same way.

Exit codes: `0` everything restored, `1` runtime error, abort or conflicts left in place,
`2` usage error.

## Layout

| Path | Contents |
|---|---|
| `cmd/atlas-ai` | Entry point of the binary |
| `internal/cli` | Command dispatcher and commands |
| `internal/platform` | Platform paths for Linux and Windows |
| `internal/backup` | Backup sets, listing and restore with drift detection |
| `skills` | Agent-neutral skills, embedded in the binary later |
| `docs` | Scope and roadmap |
| `odd` | Task documents of the features under development |

## Documentation

[`docs/scope-and-roadmap.md`](docs/scope-and-roadmap.md) holds the project scope, the decisions taken and the roadmap.

## Skills

`skills/` holds one neutral `SKILL.md` per skill. The same text is meant to be copied unchanged
to every supported agent (Claude Code, GitHub Copilot CLI, GitHub Copilot in VS Code), so it must
not name agent-specific tools, files or machine paths.

| Skill | Purpose |
|---|---|
| `session-start` | Resume a project: git state, journal, open work, memory, briefing |
| `session-end` | Close a session: classify and save what was learned, close the logbook |
| `checked-loop` | Implement a task as small units, each verified by a frozen check |

The neutrality rules are enforced by `skills/skills_test.go`:

```bash
go test ./...
```

The test checks a public list of generic tokens. A maintainer can add private names (project
names, hosts, paths) to an optional `skills/forbidden.local.txt`, one per line; that file is
ignored by git and never published.

## Development

Gitflow: `feature/*` branches from `develop`; `main` only receives releases.
Commits follow Conventional Commits.
