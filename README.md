# atlas-ai

An installer that sets up a complete AI-assisted development workflow on Linux and Windows:
an Atlas knowledge vault, persistent memory, a session logbook, a code knowledge graph,
token-saving tools, skills, hooks and MCP server configuration for your coding agents.

## Status

Early development. The repository currently contains only the agent-neutral skills and the
contract test that guards them. The installer itself does not exist yet.

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
