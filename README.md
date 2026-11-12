# atlas-ai

An installer that sets up a complete AI-assisted development workflow on Linux and Windows:
an Atlas knowledge vault, persistent memory, a session logbook, a code knowledge graph,
token-saving tools, skills, hooks and MCP server configuration for your coding agents.

## Status

Early development. The repository contains the agent-neutral skills with the contract test
that guards them, and the skeleton of the `atlas-ai` command. The installer itself does not
exist yet: the command only reports its version and the paths it will use.

## Usage

```text
atlas-ai help       Show the usage
atlas-ai version    Print the version and platform
atlas-ai paths      Print the home and state directories atlas-ai uses
```

`paths` resolves the directories from the environment of the current platform:

| Platform | Home | State directory |
|---|---|---|
| Linux | `$HOME` | `$XDG_STATE_HOME/atlas-ai`, or `$HOME/.local/state/atlas-ai` |
| Windows | `%USERPROFILE%` | `%LOCALAPPDATA%\atlas-ai` |

Empty or relative values are rejected; atlas-ai never falls back to the current directory.

## Layout

| Path | Contents |
|---|---|
| `cmd/atlas-ai` | Entry point of the binary |
| `internal/cli` | Command dispatcher and commands |
| `internal/platform` | Platform paths for Linux and Windows |
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
