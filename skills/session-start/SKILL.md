---
name: session-start
description: >
  Activates the session workflow: detects the active project, checks git state,
  reads the last journal and state docs, detects the active spec with pending tasks,
  queries context in Engram and Atlas, and optionally runs quick tests/benchmarks.
  Returns a resumption briefing with next-step suggestions. Trigger: user says
  "start session", "session start", "resuming", "where did we leave off",
  "project state", "bring me up to speed".
license: Apache-2.0
metadata:
  author: Felipe Perez
  version: "1.0"
---

<!-- Canonical agent-neutral source, shipped unchanged to every supported agent by atlas-ai. Do not add agent-specific tool names, files or machine paths: skills/skills_test.go enforces this. -->

## When to Use This Skill

- The user opens a new session on a project and wants to resume context
- The user says "where did we leave off", "bring me up to speed", "project state"
- After several days away from a project, when returning to it
- Before starting to code in a non-trivial repo

Do NOT use if:
- The user asks something specific that does NOT require reloading project context
- session-start has already been run in this same session

---

## Startup Protocol

Execute steps in this order. **Parallelize** independent calls in the same turn (git + reads + mem_context + Atlas search are all independent).

**Runtime notes** (this text is shipped unchanged to every supported agent):

- **Tool names**: MCP tools are named here by their base name. Call them under whatever prefix this runtime exposes: the Engram memory tools (`mem_current_project`, `mem_context`, `mem_search`, …), the Atlas tools (search / read note), if available, and the codebase-memory tools (`list_projects`, `index_status`, `index_repository`, `search_graph`, …).
- **Global instructions**: "this agent's global instructions file" is the user-level rules file of the agent running this skill (e.g. `CLAUDE.md`, `AGENTS.md`, `copilot-instructions.md`).
- **`$ATLAS_VAULT`**: the Atlas vault root, set by atlas-ai at install time.

### Step 0 — Open the Logbook

This agent's global instructions file makes the logbook mandatory. Run, in parallel with Steps 1-2:

```
logbook new --tool <agent-name> --model <model-id> --repo <pwd> --task "session-start: <project> — resumption briefing"
```

`logbook new` prints `logbook: session=<id>`. **Keep that ID** and pass `--session <id>` in every later `logbook act` / `logbook finish` (shell variables may not persist between tool calls, so repeat the ID literally). Without it the command fails and writes nothing.

- `<agent-name>` is the value this agent already uses in the logbook (the shared day file). If this agent has a Stop hook that calls `logbook hook --tool ...`, use exactly the hook's value so the automatic `(auto)` entry is deduplicated; if it has no such hook there is nothing to dedupe.
- Skip if a logbook block was already opened in this turn or conversation (the agent may open it before the first work step).
- If `logbook` fails, report it in the briefing and continue (fail gracefully). Never edit the logbook `.md` by hand.
- Paths, actions and decisions only — never secrets, keys or tokens.

### Step 1 — Detect the Active Project

- `pwd` and look for project markers: `.git`, `package.json`, `pyproject.toml`, `Cargo.toml`, `go.mod`, `composer.json`
- If there is a `CLAUDE.md`, `AGENTS.md` or `.github/copilot-instructions.md` in the directory: read it (policies, conventions, venv, commands)
- This agent's global instructions file: already loaded automatically — do not re-read
- Canonical project name = directory name or `package.json::name`/`pyproject.toml::project.name`

### Step 2 — Git State

Run in parallel in a single turn:

- `git status --short` (uncommitted changes)
- `git branch --show-current` + `git log --oneline -5` (branch + last 5 commits)
- `git stash list` (pending stashes)

If there are uncommitted changes or stashes → flag them in the briefing.

### Step 2.5 — Reconnaissance (delegate when heavy)

Not every runtime has a subagent / delegation tool. If this one has a read-only subagent (isolated context, reports back to the parent), use it for the heavy reconnaissance.

- If resuming requires **4+ files** of exploration (large STATE/doc tree, several specs, journal + summary + README + code) → do not inline it in the parent: run the scoped read-only subagent first, e.g. "Map [repo]: key files + state + risks, and return a compact briefing (≤30 lines)".
- Use the returned briefing as input to the Steps 3-5 briefing; parent context stays thin. The subagent is read-only and reports back.
- Lightweight resume (≤3 files: last journal + one STATE) stays inline — delegation for the obvious is over-orchestration.
- If no subagent / delegation tool is available in this runtime, fall back to inline reads and note "subagents unavailable" in the briefing.

### Step 3 — State Documentation

Read if they exist (in order, without failing if any is missing):

1. `docs/journal/sessions/` — most recent entry by date
2. `docs/STATE.md` — executive snapshot
3. `IMPLEMENTATION_SUMMARY.md` — capabilities and spec state
4. `README.md` — only if the above do not exist

### Step 4 — Active Spec (if the project follows SDD)

- Look for `specs/NNN-*/tasks.md` (descending numeric order — the last is usually the active one)
- For each spec, count `- [ ]` checkboxes (pending) vs `- [x]` (closed)
- Active spec = the last one whose pending ratio > 0 AND whose last edit is recent

CAUTION: unchecked checkboxes may be stale. Cross-reference with `IMPLEMENTATION_SUMMARY.md` or the journal before stating "spec X has Y pending tasks". If the summary says "closed" and checkboxes are unchecked → report the conflict, do not assume it is open.

### Step 4b — Open ODD Features (read-only)

ODD is the default workflow (this agent's global instructions file), so unfinished features must show up in the briefing like SDD specs do.

- List `odd/tasks/*.json` and `odd/tasks/*.md` at the repo root. Skip this step if the directory does not exist.
- `.json`: count `tasks[].status`; the feature is open if any task is not `done` or `skipped`. Read `next_step`.
- `.md` (legacy): count `- [ ]` vs `- [x]` task lines; read `**Status:**`, `**Updated:**` and the `## Next step` section. Open if tasks are pending OR the status says something like "PR pending".
- Engram mirror (read-only, in parallel with Step 5): `mem_search("odd/<feature>/tasks", project=<resolved>)` per open feature, only to report `mirror: ok | missing | differs`. Do NOT call `mem_get_observation` to reconcile.

CAUTION: checkboxes may be stale. Cross-check with `git log --oneline` on the feature branch before calling a task pending; if the commits say otherwise, report the conflict instead of assuming.

**session-start never resumes an ODD feature**: no reconciliation, no task status change, no writes to the file or the mirror. Resuming follows the ODD protocol (`mem_context` → `mem_search` → `mem_get_observation` → task file) only when the user asks to continue that feature.

### Step 5 — Persistent Memory (Atlas-first)

Fetch in parallel, but treat **Atlas as the primary source of truth** — it's where `session-end` now saves by default:

- Direct read of the **Atlas (Obsidian)** vault at `$ATLAS_VAULT`:
  - `Proyectos/<name>.md` — project entity page, if it exists (architectural state).
  - If the Atlas tools (search / read note) are available, also use them. If not, use `grep`/`find` in the vault.
- `mem_current_project` first, then `mem_context(project=<resolved project>, compact=true)` (Engram memory tools, see Runtime notes) — recent observations in Engram, for temporal/tactical detail Atlas doesn't carry (bugfixes, spec snapshots, in-progress state). `mem_context` has no `limit` parameter. On `ambiguous_project`, follow the protocol in this agent's global instructions file (resolve the repo root and retry); never guess the project.

If Atlas and Engram disagree on the same fact, **Atlas wins** — it's the later, more deliberate write. Use Engram to fill gaps (what happened recently) rather than to override Atlas's architectural state.

Cross-check dates:
- Atlas page date: read the frontmatter `updatedAt:` (canonical; formats `YYYY-MM-DD`, `YYYY-MM-DD HH:MM` or epoch ms). Fall back to the legacy body fields `**Last updated:**` / `**Última actualización:**`. The epoch `1754467200000` (2025-08-06) is an import placeholder shared by many pages → treat it as "date unknown". No field at all → report "no date".
- If that date is earlier than the project's last journal → flag Atlas drift in the briefing (reconciliation is `session-end` work). Do not flag drift when the date is unknown.
- If the last Engram observation is older than the last journal → flag "Engram outdated, N days of drift" (lower priority than Atlas drift).

### Step 6 — Codebase Graph (codebase-memory-mcp)

Run in parallel with Step 5 when the codebase-memory tools are available in this session.

- `list_projects` — check whether the current repo is indexed.
- If indexed: `index_status` — compare the indexed commit against the HEAD commit from Step 2.
  - Index behind HEAD → flag "Codebase graph stale, N commits behind" in the briefing. Do **not** re-index automatically (that is `session-end` work, and can take minutes on large repos).
- If NOT indexed → note "Codebase graph not indexed for this project" in the briefing. Do not offer to index unless the user's request in this turn is explicitly structural.
- If the user's opening request is structural ("what changed", "who calls X", "show me the architecture") → use `search_graph`, `search_code`, `get_architecture` or `detect_changes` to enrich the briefing instead of grep.

CAUTION: if the codebase-memory tools are not connected this session, skip this step silently — do not block the briefing on it.

### Step 7 — Tests/Benchmark (optional, ask first)

Do NOT run automatically. Ask the user:

> "Shall I run tests + benchmark as a sanity check? (may take ~30s-2min)"

If they say yes:
- Detect the test command from the project's `CLAUDE.md` / `AGENTS.md` or conventions (`pytest`, `pnpm test`, `go test ./...`, `cargo test`, `unittest discover -s tests`)
- Respect venvs and paths documented there (e.g. a venv python path declared in the project's `CLAUDE.md` / `AGENTS.md`)
- Report green/red + number of tests; if red, do NOT attempt to fix — only flag it

### Step 8 — Briefing to User

Return a structured summary:

```
## Resumption point — <project>

**Branch:** <branch> · **Last commit:** <hash> <msg>
**Uncommitted changes:** <N files> | <none>
**Last session:** <journal date> — <title or first line>
**Codebase graph:** indexed as of <commit/date> | stale (N behind) | not indexed | unavailable
**Logbook:** session opened in <path> | failed: <reason>
**Atlas:** <page> (updatedAt <date>) | no entity page | date unknown
**Security:** secure by default and by design apply to all implementation · pending findings: <list> | none

### Project state
- <bullet with key metrics from STATE/IMPLEMENTATION_SUMMARY>
- <bullet with active spec if any>

### Open ODD features
- `<feature>` (.json|.md) · N/M done · blocked: <Tk or none> · next: <next_step> · mirror: ok | missing | differs
(omit this block if there are none)

### Prioritized debt (top 3)
- <from STATE.md §debt or IMPLEMENTATION_SUMMARY>

### Suggested next step
<1-2 concrete sentences based on the last journal, pending tasks and the first open ODD feature. If the next step is heavy recon, propose it as a delegated read-only reconnaissance task (see Step 2.5)>
```

If conflicts are detected (outdated engram, checkboxes vs IMPLEMENTATION_SUMMARY, red tests), add an **⚠ Attention** section with the detail.

---

## Operational Rules

- **Write nothing to disk** during session-start. Read only — sole exception: the `logbook new` call from Step 0 (mandated by this agent's global instructions file).
- **Do not modify memories** (engram/atlas) in start — that is `session-end` work.
- **Never resume ODD features** here: show them (Step 4b), do not reconcile or update their files or Engram mirrors.
- **Parallelize reads** whenever possible. Bash + Read + mem_context + Atlas search can go in the same turn.
- **Delegation** (Step 2.5): broad context exploration (4+ files) → read-only subagent, if this runtime has one; small reads (1–3 files) stay inline. session-start itself never writes code — the subagent is read-only and reports back.
- **Fail gracefully**: if engram, atlas, codebase-memory-mcp, or any file does not respond/exist → omit that section from the briefing, do not break the flow.
- **Respect project language**: if the project's CLAUDE.md / AGENTS.md specifies Spanish, the briefing goes in Spanish. English by default.
- **Do not suggest code changes** in the briefing. The next-step suggestion is "which task to tackle", not "which line to edit".
- **Security by default and by design**: every implementation that follows this session must take security into account from the start (secure defaults, least privilege, validated input, no secrets in code), not as a later review step. Surface any pending security finding found in STATE/journal/IMPLEMENTATION_SUMMARY in the briefing.

---

## Heuristics

**Is there an active spec?**
- `specs/NNN-*/tasks.md` exists with `- [ ]` pending AND the journal/STATE does not explicitly say "closed" → yes
- If `IMPLEMENTATION_SUMMARY.md` says "closed on <date>" and checkboxes are unchecked → stale checkboxes, NO active spec

**Is there an open ODD feature?**
- `odd/tasks/<feature>.json|.md` with tasks not `done`/`skipped` (or `- [ ]` lines), or a status such as "PR pending" → yes, list it
- Everything done and no pending delivery step → closed, do not list it
- Never resume it from session-start (see Step 4b)

**Is Engram outdated?**
- Last observation with `project=<X>` > 7 days before the last journal → outdated
- Suggest at the end of the briefing: "Consider running `/session-end` at the end of today to sync Engram"

**Is Atlas relevant?**
- The Atlas vault lives in `$ATLAS_VAULT`. Structure: `Proyectos/<project>.md` (entity page), `Stack/<category>/<tech>.md` (tech catalog with `_INDEX.md`), `Setup/` (infra), `AI/`, `Temp/`.
- If `Proyectos/<project>.md` exists → always read it: it has stable architectural state + backlinks to relevant Stack technologies.
- If there is NO entity page → say "Atlas: no entity page" in the briefing and read the parent page if one exists (e.g. `Proyectos/my-app.md` for `my-app-NN`). Creating the page is `session-end`'s decision, never start's.

**Is the codebase graph stale?**
- `index_status` commit older than the HEAD commit from Step 2 → stale, report the gap in commits.
- Not indexed at all is normal for a first session on a project — do not treat it as an error, just report it. (This agent's global instructions file may suggest `index_repository` when not indexed; during session-start that stays an explicit user request, see below.)
- Never call `index_repository` during session-start — indexing is a write/compute action reserved for `session-end` (with confirmation) or an explicit user request.

---

## Example Output (my-app, simulated)

```
## Resumption point — my-app

**Branch:** main · **Last commit:** abc1234 chore: close spec 006
**Uncommitted changes:** 12 untracked files (.artifacts/, .claude/, CLAUDE.md, docs/, specs/, src/, tests/)
**Last session:** 2026-05-21 — Closed spec 006 (invoice export). p95 latency 180 ms, 317 tests.
**Codebase graph:** stale (3 commits behind HEAD)
**Security:** secure by default and by design apply to all implementation · pending findings: none

### Project state
- 6 specs closed (001→006). 6-stage pipeline: ingest→validate→normalize→dedup→enrich→report.
- p95 latency 180 ms, 0 failed jobs, 7 export formats covered. Latest metric is a record.
- No active spec with pending phases.

### Prioritized debt (top 3)
- Calibrate the retry policy of the payment client (times out under load; success rate with retries drops to 0.76)
- Real-data fixtures for the export tests (large invoices, multi-currency) — spec 004 phase 10.1
- Webhook handlers for the second provider (provider detected, handlers not implemented)

### Suggested next step
If you want to continue: open spec 007 extending the export to the remaining formats, or an iterative retry-policy calibration session (1-2h). If the goal is to consolidate: commit current state (there are docs/, specs/, src/ untracked).

⚠ Engram was outdated as of 2026-05-18 — synced in the previous session. OK.
```
