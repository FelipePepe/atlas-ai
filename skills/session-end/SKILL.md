---
name: session-end
description: >
  Structured session close: reviews work done, separates operational memory (Engram)
  from stable architectural knowledge (Atlas), checks for duplicates before saving,
  persists each observation in canonical format, and optionally adds an entry to the
  project journal. Trigger: user says "close the session", "end of session", "save to
  engram", "save context", "summarize what we did", "what do we save from this session",
  or at the end of a long working session.
license: Apache-2.0
metadata:
  author: Felipe Perez
  version: "1.0"
---

<!-- Canonical agent-neutral source, shipped unchanged to every supported agent by atlas-ai. Do not add agent-specific tool names, files or machine paths: skills/skills_test.go enforces this. -->

## When to Use This Skill

- The user explicitly asks to close/save at the end of a session
- Significant work was done (deploy, feature, bugfix, architectural decision)
- The user says "let's wrap up", "save what matters", "summarize the session"
- Decisions/learnings are detected that are not in persistent memory

Do NOT use if:
- The session was trivial (a typo, a question without code)
- session-end was already run in this same turn

---

## Closing Protocol

**Runtime notes** (this text is shipped unchanged to every supported agent):

- **Tool names**: MCP tools are named here by their base name. Call them under whatever prefix this runtime exposes: the Engram memory tools (`mem_current_project`, `mem_search`, `mem_save`, `mem_update`, …), the Atlas tools (search / read note), if available, and the codebase-memory tools (`list_projects`, `index_status`, `index_repository`, …).
- **Global instructions**: "this agent's global instructions file" is the user-level rules file of the agent running this skill (e.g. `CLAUDE.md`, `AGENTS.md`, `copilot-instructions.md`).
- **`$ATLAS_VAULT`**: the Atlas vault root, set by atlas-ai at install time.

### Step 1 — Review the Session History

Go through the conversation and identify candidates. For each one, decide the type:

| Type | Save if... |
|------|-----------|
| `decision` | Non-obvious architecture/technology/approach decision |
| `bugfix` | Resolved error — symptom, root cause, fix |
| `architecture` | Designed/documented structure (module, network, system) |
| `pattern` | Repeatable convention or way of working |
| `config` | Environment configuration (nginx, systemd, DNS, env vars) |
| `discovery` | Undocumented finding (occupied port, limitation, unexpected behavior) |

**Do NOT save**:
- Obvious steps or officially documented behavior
- Code already in the repo (the repo is the source of truth)
- Temporary state (in-progress tasks, TODOs without context)
- Things already in Engram/Atlas — verify first

### Step 2 — Classify Destination: Engram vs Atlas

Each candidate goes to ONE destination (not both unless there is an explicit reason).

**Atlas — persistent and architectural knowledge** (useful life >6 months, cross-session):
- Lives in `$ATLAS_VAULT` (Obsidian vault). Primary access via filesystem (Read/Write/Edit) — the Atlas tools (search / read note) may not be available in every session.
- Structure: `Proyectos/<project>.md` (entity page per project), `Stack/<category>/<tech>.md` (tech reference with `_INDEX.md` master), `Setup/` (infra/operational), `AI/`, `Temp/`.
- "Load-bearing" architecture decisions: stack choice, data model, stable API contracts.
- Organizational conventions that apply across multiple projects.
- Canonical mappings (e.g. "billing-api↔my-app", OWASP taxonomies).
- Established personal/team working patterns.

**Most typical at session close**: if there was a spec/phase close with architectural substance, **update `Proyectos/<project>.md`** with the delta (current state, metrics, new Stack links). This is usually not a new page — it is an upsert on the existing entity page. Mandatory convention: refresh the frontmatter `updatedAt: YYYY-MM-DD`. If the page only has a legacy body field (`**Last updated:**` / `**Última actualización:**`), still set `updatedAt` in the frontmatter and leave the legacy field untouched (no mass migration).

**Engram — operational memory** (useful life weeks-months, day-to-day):
- Resolved bugs (symptom+cause+fix)
- Spec state and metrics (temporal snapshots)
- Specific environment configuration (venv, ports, paths)
- Prioritized technical debt
- Tactical decisions within a spec

**Quick rule**: if the answer to "will this still be true in 6 months?" is YES → atlas. If "it depends on the current state" → engram.

### Step 2.5 — Codebase Graph Freshness (codebase-memory-mcp)

Only if the codebase-memory tools are available this session AND source files (not just docs/memory) were created, edited, or committed:

- `list_projects` — check whether this repo has ever been indexed.
  - Never indexed → skip this step entirely. Do not suggest indexing a project that has never used the graph.
- If previously indexed: `index_status` — compare the indexed commit against the current HEAD (post-commit, if you committed this session).
  - Behind HEAD → ask the user: "Codebase graph is N commits behind — re-index with `index_repository`? (may take a while on large repos)"
  - Only run `index_repository` if the user confirms. Never re-index automatically — same caution as the Tests/Benchmark step in `session-start`.
  - Up to date → nothing to do, no need to mention it.

### Step 3 — Check for Duplicates Before Saving

For each candidate:

- Engram: resolve the project with `mem_current_project` first. On `ambiguous_project`, never guess: ask the user to pick one of `available_projects` and retry with `project`, `project_choice_reason=user_selected_after_ambiguous_project` and the returned `recovery_token`. Then `mem_search("<title or keywords>", project=<X>)`. If something similar exists: use `topic_key` to upsert with `mem_save` or `mem_update`.
- Atlas: check if `Proyectos/<project>.md` exists in `$ATLAS_VAULT`. If yes → edit the relevant section (do not create a new page). If the observation spans several projects or concerns a technology, consider `Stack/<category>/<tech>.md`.
- Atlas, project WITHOUT an entity page: if the session has architectural substance (Step 2 Atlas criteria), **ask the user** before creating `Proyectos/<project>.md`. On yes, create a minimal page following the `atlas-docs` skill if it is installed in this runtime, otherwise the frontmatter of sibling pages in `Proyectos/` (frontmatter `title`, `tags`, `createdAt`, `updatedAt`; bidirectional wikilink with the parent page if any, e.g. `my-app.md` for `my-app-NN`). On no, or without substance → save to Engram only and say so in the Step 6 summary. Never create the page silently.

`topic_key` values (Engram) must be stable and compound: `my-app/spec-006-export`, `billing-api/nginx-config`, `personal/git-workflow`.

### Step 4 — Save with Canonical Structure

Mandatory format for `content` (engram and atlas):

```
**What**: [what was done or decided, in one line]
**Why**: [why it matters, what problem it solves]
**Where**: [paths, files, commands, affected services]
**Learned**: [gotchas, edge cases, decisions made — omit if not applicable]
```

**Titles**: short, searchable, prefixed by project when applicable.
- ✅ `my-app: dedup duplicate-invoice check (spec 006 phase 8)`
- ✅ `billing-api: nginx on staging - reload without restart`
- ❌ `fixed a problem with nginx`

**Project**: use the project name Engram resolves (`mem_current_project`, see Step 3) — do not invent variants of it.
**Personal scope** (preferred for infra and patterns that apply across projects): `scope: personal`.

Run saves in parallel when they are independent (multiple `mem_save` in a single turn).

### Step 5 — Project Journal (if applicable)

If the project has `docs/journal/sessions/` and the session was substantial (not trivial):

- Check if an entry for today already exists: `docs/journal/sessions/YYYY-MM-DD.md`
- If it does NOT exist and the session warrants an entry: **ask the user** before creating it
  - Template at `docs/journal/sessions/_template.md` if it exists
  - Minimum content: objective, work done, decisions, bugs found, quantified result, state at close
- If it already exists: ask whether to append the advances from this turn
- Do NOT create the journal automatically — always confirm

If the project does NOT have `docs/journal/` → skip this step.

### Step 5.1 — ODD Features Check (verify and warn)

In ODD the feature file and its Engram mirror `odd/<feature>/tasks` are updated task by task during the work, so this step is only a safety net.

- **Detect** (same definition as `session-start` Step 4b): list `odd/tasks/*.json` and `odd/tasks/*.md` at the repo root. Open = tasks not `done`/`skipped`, `- [ ]` lines, or a status such as "PR pending". Skip the step if `odd/tasks/` does not exist.
- **Compare** (read-only): `mem_search("odd/<feature>/tasks", project=<resolved>)`, then `mem_get_observation` for the full mirror. Compare per-task status, `next_step` and the updated date. Result per feature: `ok` | `missing` | `differs` (list the concrete differences).
- **Act only with confirmation**: `missing` → ask whether to create the mirror from the file. `differs` → show the differences and ask which side wins. On an answer, write the losing side (mirror via `mem_save` with `topic_key=odd/<feature>/tasks` and the FULL document; file via Edit), then read back BOTH. If irreconcilable or unanswered → change nothing, preserve both versions and report.
- **Never** mark tasks `done`/`failed`, change task scope or add tasks from this skill. That belongs to the ODD flow during the work.
- Pass one line per open feature to the logbook `--followup` in Step 5.5: `<feature>: N/M done, next: <next_step>, mirror: ok|missing|differs`.

### Step 5.2 — Git State (report only)

If the project is a git repo, run `git status --short`, `git branch --show-current`, unpushed commits (`git log @{u}..` only if an upstream exists) and, if `gh` is available, `gh pr view` for the branch.

- Report pending items (uncommitted files, unpushed commits, branch without PR) in the Step 6 summary and pass them to the logbook `--followup` in Step 5.5.
- **Report only**: never commit, push or open a PR from this skill. Those stay with the user (gitflow: feature → PR to `develop`).

### Step 5.5 — Close the Logbook

This agent's global instructions file makes the logbook mandatory. Always via the `logbook` CLI, never by editing the `.md`:

1. Reuse the `session=<id>` printed by the `logbook new` of THIS turn or conversation (e.g. from session-start). Only if none was opened, run it first: `logbook new --tool <agent-name> --model <model-id> --repo <pwd> --task "<real task of the session>"` and keep the printed ID. `act`/`finish` require `--session <id>` and never guess the block; repeat the ID literally in every call (shell variables may not persist between tool calls). `<agent-name>` is the value this agent already uses in the logbook. If this agent has a Stop hook that calls `logbook hook --tool ...`, use exactly the hook's value so the automatic `(auto)` entry is deduplicated; if it has no such hook there is nothing to dedupe.
2. `logbook act "session-end: <N> saves (Engram/Atlas)" --obj "<main page or topic_key>" --estado ok|warn --session <id>`
3. `logbook finish --session <id> --status <status> --detail "<one-line result>" [--learn "..."] [--followup "..."]`

Status: `done` = goal met · `partial` = progress with pending items · `failed` = goal not met · `blocked` = waiting on user or external.

Paths, actions and decisions only — never secrets, keys or tokens.

### Step 5.7 — Engram Session Summary

After the saves and the logbook, call `mem_session_summary` (this agent's global instructions file requires it before declaring "done"), then `mem_session_end` if this runtime exposes it. Use only the session ID already registered by the runtime — never invent one. If either fails, report it as pending; do not block the Step 6 summary.

### Step 6 — Summary to User

Display a table of what was saved:

| Destination | Title / Page | Type | Action |
|-------------|-------------|------|--------|
| Engram | ... | bugfix | save |
| Engram | ... | decision | update (#1234) |
| Atlas | `Proyectos/my-app.md` | architecture | edit (section X) |
| Atlas | `Stack/Languages/Rust.md` | pattern | edit (section Y) |
| Logbook | `logbook-YYYY-MM-DD.md` | session | finish [done] |
| Engram | session summary | summary | save |
| Git (report only) | `<branch>` | state | N uncommitted / N unpushed / PR #N or none |
| ODD | `odd/tasks/<feature>.json` | feature | N/M done · mirror ok\|missing\|differs → synced\|pending |

**Security:** secure by default and by design applied to this session's implementation · pending findings: <list> | none

And briefly mention what was decided NOT to save and why (e.g. "trivial", "already in X").

If a journal was created/modified: include the file path.
If the codebase graph was re-indexed (Step 2.5): mention the new indexed commit; if it was flagged stale but the user declined to re-index, note that it's still pending.

---

## Operational Rules

- **Do not invent** observations — only save what actually happened in the conversation.
- **Do not duplicate** — always run `mem_search` / Atlas search first. Upsert with `topic_key`.
- **Parallelism**: independent saves go in the same turn.
- **Atlas-first**: when in doubt, default to Atlas. Only use Engram if the candidate is clearly temporal/tactical (a snapshot tied to current state, a resolved bug, a spec metric) per the Step 2 table.
- **Respect project language**: if the project's CLAUDE.md / AGENTS.md is in Spanish, titles and content go in Spanish too.
- **Do not silence errors**: if engram/atlas fails, report to the user what was left pending.
- **Logbook**: if `logbook` fails, report it as pending — never edit the logbook `.md` by hand.
- **ODD**: verify and warn. Write a feature file or its mirror only after explicit user confirmation, and read back both.
- **Security by default and by design**: at close, check that the implementation done this session took security into account from the start (secure defaults, least privilege, validated input, no secrets in code). Save security decisions and findings as normal candidates (Step 1), report any pending finding in the summary, and never persist secrets, keys or tokens in Engram, Atlas or the logbook.

---

## Quick Heuristics

**Is this worth saving?**
- Would you search for this in memory next time you do the same thing? → Yes → save
- Did it take >15 min to resolve or understand? → Yes → save
- Is it something you could forget in 2 weeks? → Yes → save
- Is it already in the code, CLAUDE.md / AGENTS.md, or docs/? → No → don't save

**Engram or Atlas?**
- "The my-app export catalog maps 19 invoice types to 7 formats" → atlas (stable taxonomy)
- "Spec 006 closed on May 21 with p95 latency 180 ms and 317 tests" → engram (temporal snapshot)
- "To avoid duplicates, the duplicate-invoice dedup omits the line number" → engram (tactical decision)
- "We use a 6-stage pipeline architecture inspired by billing-api" → atlas (architectural decision)

**Create a journal?**
- Session <30min with no substantial changes → no
- Session with new bug fix, decision, or closed phase → yes (ask first)
- Conversation/analysis only without code → no, but update memory
