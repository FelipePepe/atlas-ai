# ADR 0001: Agent-neutral skills catalog in Go

## Status

Accepted. Retrospective: it documents the state of the repository at the time of writing.

## Context

The repository is a small Go module (`go.mod`, one external dependency: `gopkg.in/yaml.v3`) that holds a catalog of agent skills under `skills/`. Each skill is a directory containing a `SKILL.md` file with YAML frontmatter. The skills must work on more than one agent platform, so they cannot depend on a single runtime's vocabulary, tool names, or machine-specific paths.

## Decision

- Skills live as plain Markdown in `skills/<name>/SKILL.md`. The Go code parses their frontmatter.
- `skills/embed.go` embeds the catalog into the Go binary, so consumers read it from the module instead of from loose paths.
- `skills/skills_test.go` is the contract test. It checks that:
  - every skill in `expectedSkills` is shipped (`TestExpectedSkillsAreShipped`),
  - frontmatter is valid for every agent (`TestFrontmatterIsValidForEveryAgent`),
  - no skill contains a forbidden token (`TestForbiddenTokenHits`, `TestSkillsAreAgentNeutral`).
- The public forbidden-token list is committed in the test file. Tokens that identify a person or machine (private project names, hosts, personal paths) are never committed. They go in `skills/forbidden.local.txt`, which is gitignored. The test adds that file's tokens when it exists, so the local check runs only on maintainer machines.
- Feature planning artifacts follow the ODD override in CLAUDE.md and use `odd/tasks/<feature>.json`.

## Consequences

- Neutrality is enforced by tests, not by convention. A skill that leaks a platform-specific token fails `go test`.
- The private token list is not versioned. Contributors without the local file get only the public checks, and the full check runs only on maintainer machines.
- Adding a skill requires updating `expectedSkills` in the test. This is deliberate: a missing skill is a packaging error.
- The embedded catalog currently has no consumer inside this repository. Consumers live outside it.

## Evidence

- Code index: 160 nodes, 211 edges, Go only, 1 external package, 4 clusters (`get_architecture`).
- `skills/skills_test.go:19`: `expectedSkills = {"checked-loop", "session-end", "session-start"}`, which matches the three skill directories in the repository.
- `skills/skills_test.go:21-27`: the public-list and local-tokens policy described above.
- Commits: `49ed4b0` (bootstrap), `a271c19` (agent-neutral skills with contract test), `a1c7384` (keep local forbidden tokens out of test output).

## Open questions

- Which external consumer imports the embedded catalog, and whether it needs a stable API. Not visible from this repository.
