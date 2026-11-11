---
name: checked-loop
description: >
  Implements a task end to end as a graph of small units, each verified by a
  frozen, user-approved check (Karpathy loop: one change, run check, keep or
  revert). Trigger: user says "checked loop", "karpathy loop", "implement with
  frozen checks", "run this as units with checks", or pastes a <goal> block with
  TASK, WORKSPACE, HARNESS and BUDGET.
license: Apache-2.0
---

## When to Use

- The task has an objective pass condition (tests, lint, schema validator, path allowlist).
- The user wants autonomous implementation with bounded retries.

Do NOT use for ambiguous or design-heavy tasks with no executable check. Ask for the missing inputs first: TASK, workspace, harness, budget. One question at a time.

## Inputs

TASK, workspace path, harness, budget (attempts or time, whichever comes first).

## Method

For every unit: make one change, run the fixed check, keep it if it passes, revert it if it fails. The check is the only judge. Confidence is not evidence.

## Setup

1. Create branch `agent/<short-slug>` from the current HEAD. Never commit to main, develop or any shared branch.
2. Create `results.tsv` with the header `unit<TAB>attempt<TAB>commit<TAB>verdict<TAB>reason`.
3. Create an empty `RULES.md`. If a `RULES.md` or `results*.tsv` from another run exists, rotate it first (`mv RULES.md RULES.<old-slug>.md`): rules and logs are per run, and a leftover `RULES.md` from another task misleads a resumed run.
4. Add `results.tsv` and `RULES.md` to `.git/info/exclude`. They are the run log and must never be tracked, so `git reset --hard` cannot erase them.
5. Commit `PLAN.md` and the frozen check files once, before the first unit, and only after the user approved the checks. `PLAN.md` states `Checks approved by the user: <date>` and the base branch, so the approval is on disk and a resumed run does not depend on someone remembering it. Note the commit as `<T>`.
6. No git repo yet: `git init -b main`, one empty root commit on `main`, create `develop`, then branch `agent/<slug>` from `develop`.
7. A global check that is a script (`check_global.sh`) stays untracked and is added to `.git/info/exclude`: if committed, it shows up in the diff the allowlist inspects and fails it.
8. Path-allowlist checks diff against the branch the agent branch was created from (`git diff --name-only <base>...`), never a hardcoded `main`. Commits inherited from the base (for example a `.gitignore` already on `develop`) would otherwise count as violations. Record `<base>` in `PLAN.md`.
9. Language artifacts (`__pycache__/`, `node_modules/`...) need a `.gitignore`. It is not a unit output, so it falls outside a path allowlist: ask the user which branch gets it, never add it to the agent branch silently.
10. Under the global plan-before-coding rule, present the setup and unit plan as the JSON plan and wait for approval before the first file write.

## Step 1: Check first (user gate)

- Write the pass condition for the whole task as a command that exits 0 on pass and non-zero on fail.
- Write one check command per unit (see Step 2).
- Show the checks and STOP until the user approves them.
- After approval the check files are frozen: never edit, skip, weaken or disable them, never mark tests as skipped.
- If a check seems wrong, stop and explain why. Do not fix it.
- "Looks good" and "I'm confident" never count as passing.
- Checks must not run a build unless the user explicitly approves it (global rule: never build after changes).
- A check that cannot fail is worthless. For every check, show the negative control before asking approval: run it against the empty or known-bad state and show it exits non-zero. For the global check, simulate a failing test run and show the whole script exits non-zero.
- Never chain a compound check with `;` or `A && B | C ; test $?`: `;` starts a new command and `$?` then reports the wrong status (a failing `A` can read as a pass). Put multi-command checks in a script with `set -e` and explicit `if ... ; then exit 1; fi` blocks.
- Write checks by mirroring, never from memory: before each new `pytest.raises`, `assert`, or event/JSON comparison, grep the existing suite for that surface (exception type, event fields, schema keys, parameter defaults, argv shape) and copy the pinned contract. Authoring from memory is exactly how one real run produced five check amendments.
- Tautology lint before freeze: scan every new test file for `or True`, `if False`, `if True`, `and False`, `assert True` (use a project lint script if the repository has one; otherwise an equivalent grep). A hit is deleted, not justified.
- Byte-level adjudication of string diffs: when a string assertion fails and both sides look nearly identical, stop reading pytest output and compare position-by-position with codepoints (use a project diff script if the repository has one; otherwise a short script that prints the first differing position and both codepoints). Eyeballing expected/actual is where a five-turn `=`/`:` misread happened.
- Check-command runner: run the test runner standalone, or through a wrapper that trims output but returns the runner's real exit code (use a project wrapper script if the repository has one). `cmd | tail` must never be the last word on the exit code.
- Fixture contract: a test that drives the SUT end-to-end (runner, agent) states what the SUT needs (approver present? allowlist populated? closed argv?) in a `# CONTRACT:` header. An un-contracted fixture fails for reasons that look like SUT bugs.
- Whole-block rewrites: when changing a module section, replace the entire block in one edit, then verify with `py_compile` and `grep -c "def <name>"` for duplicates. Splices leave dead definitions invisible on a second read.
- Self-review each check against these pitfalls and say so in the proposal, instead of waiting for the user to find them.

## Step 2: Split

Each unit has: ID, one job, inputs (files read), outputs (files written), its own check command, and a named failure state.

- Edge A -> B only if B reads A's output.
- No shared file means no edge, so units may run in parallel.
- Two units writing the same file: merge them or order them with an edge.
- Write the graph to `PLAN.md` before running anything.

## Step 3: Run

- Use a fresh context per unit (subagent) when available. Otherwise, before each unit re-read only its inputs, `PLAN.md` and `RULES.md`.
- Units with no edge may run in parallel only in separate `git worktree`s. On one shared branch, run them sequentially: concurrent commits and `reset --hard` would race. Small units: sequential inline.
- Worktree procedure:
  1. Commit the frozen checks and `PLAN.md` on the integration branch `agent/<slug>`; note that commit as `<T>`.
  2. Per unit: `git worktree add -b agent/<slug>-<unit> <dir-outside-repo>/<unit> <T>`. Use a dash, not a slash: `agent/<slug>` and `agent/<slug>/<unit>` cannot coexist (git ref file/directory conflict).
  3. One subagent per worktree, launched in the background in the same turn. Each works only inside its worktree, writes only its unit's output file, never edits `tests/` or `PLAN.md`, and on failure runs `git reset --hard <T>` inside its own worktree only.
  4. Shared log: every subagent appends to the main checkout's `results.tsv` by absolute path, one `printf ... >>` per row (single short line, so appends do not interleave).
  5. Do not trust the hand-back reports: verify with commands. Check that every row has 5 fields (`awk -F'\t' 'NF!=5'`), that a failed commit is not an ancestor of any branch (`git merge-base --is-ancestor`), that `tests/` and `PLAN.md` are unchanged (`git diff --quiet <T> <branch> -- tests PLAN.md`), and that each branch only changed its own file (`git diff --name-only <T> <branch>`).
  6. Merge each unit branch into the integration branch with `git merge --no-ff -m '<conventional message>'` (no conflicts expected, since units share no files), run the global check on the integration branch, log it as `GLOBAL`.
  7. Clean up only after the global check: `git worktree remove <dir>` and `git branch -d <branch>` (`-d` refuses unmerged branches, which is the safety net).
- To prove parallelism, give each subagent `START=$(date +%s.%N)` at its first action and `END` at its last, and write both in the log row. Windows that only wrap commit and check (about 0.1 s) cannot show overlap: they prove interleaving at best. If the full-run windows do not overlap, report it and do not claim parallelism.
- Log the reason with an exact command, e.g. `python3 -m unittest <check> 2>&1 | grep -m1 '^AssertionError'`, not "first assertion line": a subagent may otherwise log a context line.
- `verdict` in `results.tsv` is exactly `pass` or `fail`, so totals can be computed with `awk` instead of judgment.
- Each attempt: produce, commit (conventional commit, no AI attribution), run the unit check, add a row to `results.tsv`.
- Pass: keep the commit.
- Fail: `git reset --hard` to the last passing commit (untracked and excluded files survive), log the reason in `results.tsv`, try again. Verify the revert with `git merge-base --is-ancestor <failed-commit> HEAD` (exit 1 = gone from the branch). Do not use `git branch --contains | wc -l`: the output is not conclusive.
- Max 3 attempts per unit. After the 3rd failure stop the whole run and tell the user the plan is wrong for that unit. Do not touch the check.

## Step 4: Merge

Merge, dedupe, sort and compare with code (scripts, diff, jq), not judgment. Spend model reasoning only on steps that judge or decide. After merging, run the global check.

## Step 5: Return

If the global check fails, reopen only the responsible unit with:

- UNIT, VERDICT, REASON
- EVIDENCE (command and relevant output lines)
- SCOPE (files it may touch)

Never redo units that already passed unless their own check now fails.

- Attribute the failure with evidence before reopening: run a one-liner or the failing assertion against each suspect unit's output, and name the one responsible.
- Log each global run in `results.tsv` as unit `GLOBAL` (its own attempt number, verdict `pass`/`fail`).
- If the unit's own check passed on its previous attempt, the fix is a new commit on top, not a `git reset --hard`. It counts as that unit's next attempt, so the 3-attempt cap still applies.
- After the fix, rerun the check of every unit that reads the reopened unit's output (edges in `PLAN.md`) and log a row for each, even if its code did not change. Confirm with `git diff --quiet <commit> -- <file>` that the units not reopened are untouched.

## Step 6: Gate by cost to undo

- Cheap and contained (inside the branch, scoped files): ship it.
- Reversible but wide (many files, shared config, public API, dependencies): ship only if the global check passes, and cite the evidence.
- Irreversible (DB migrations, deletes outside the branch, prod or real data, external calls with side effects, money, pushes to shared branches): stop and wait for approval, whatever the confidence.
- When unsure of the class, pick the stricter one.

## Step 7: Learn

When a fix passes after a failed attempt, add one line to `RULES.md`:

`- <rule> (from <unit>, attempt <n>: <evidence>)`

Apply `RULES.md` to every unit started afterwards. Keep at most 15 rules: merge duplicates, drop rules that never applied again.

## Stop

Stop when the global check passes, or 2 full rounds add no new passing unit, or the budget runs out, or a unit fails 3 times. Then show one summary:

1. What changed (files and commits on the branch)
2. What was checked (commands, results, `results.tsv` totals: rows, passes, fails). Report reverts separately: a revert is a failed attempt undone with `git reset --hard`; a failed global run or a failed reopened unit that was fixed forward is a fail without a revert.
3. What needs approval (irreversible items, doubts about the check)
4. `RULES.md` as it stands

Do not merge the branch. Merging belongs to the user. Open a PR only if asked (`gh pr create --base develop`).

## Resume after an interruption

State lives on disk: the agent branch, `PLAN.md`, `results.tsv`, `RULES.md`. Rebuild it from there, never from memory, and never ask the user to approve the checks again if `PLAN.md` records the approval.

1. Read `PLAN.md` (units, edges, base, budget), `RULES.md`, `results.tsv`, `git log <base>..`, `git status --short`. Confirm you are on the agent branch and not on a shared one.
2. Confirm the frozen files are intact: `git diff --quiet <T> -- tests PLAN.md` must exit 0 (also check `git status` shows them unmodified). If not, stop and report: do not repair a check.
3. Classify each unit by comparing the log with git:
   - **Done:** a `pass` row whose commit is on the branch. Do not touch it.
   - **Committed, not logged:** a unit commit with no row. Re-run its check; log the result with reason `logged on resume`. It counts as an attempt. On `fail`, treat it as a failed attempt: `git reset --hard` to the last passing commit.
   - **Partial:** untracked or modified output files with no commit. This is not an attempt (it was never committed or checked). Discard it (`rm`/`git checkout -- <file>`, scoped to that unit's output files) and run the unit normally. Never discard anything outside the unit's declared outputs.
   - **Not started:** run normally.
4. Count attempts from `results.tsv`, not from your own context: the 3-attempt cap and the budget include the attempts before the interruption. Count per unit; if rows appear out of `PLAN.md` order, do not try to reconcile the order. `GLOBAL` rows do not count against a unit.
5. Continue with Step 3 for the remaining units in `PLAN.md` order, then Steps 4 to 7 and the Stop summary. A subagent resuming does not call `logbook`; the parent does. A resuming agent that is itself a subagent may run the remaining small units inline instead of spawning one subagent per unit.
6. When the parent delegates the resume to a subagent, the prompt must say that the subagent's final reply IS the report (state found, decisions the skill did not cover, actions, result). Do not trust it blindly: the parent verifies the end state with commands (`results.tsv` rows with 5 fields, failed commits not ancestors of `HEAD`, frozen files intact, global check). A hand-back can be empty or useless.

## Known gaps

- Exercised (a real multi-phase run): five check amendments (all "memory vs pinned contract"), one red check committed behind a masked pipeline exit code, a five-turn byte misread fixed the wrong way three times, splice-left dead definitions, a near-frozen tautological assert, a fixture missing its approver, and a live-harness verdict hole: `model_error` read as a pass (fixed: the run is INDETERMINATE, not passing). Preventives were applied: three helper scripts (a codepoint diff, a test-runner wrapper that keeps the real exit code and a tautology lint, each demonstrated against the run's own failure case) + the six authoring rules above + `RULES.md` lines.
- Exercised (sizefmt run): revert, retry, `RULES.md` learning, and a global-check failure caused by the base-branch allowlist (now fixed above). The failing first attempt was a staged drill, since the trap was known beforehand.
- Exercised (bytesparse run): the 3-strike stop. A unit with a contradictory check failed 3 times; the run stopped with no 4th attempt, the check and `PLAN.md` untouched (`git diff --quiet`), later units not run, and the global check not run. Also a staged drill: the contradiction was approved on purpose.
- Exercised (wordstats run): the Step 5 return. Unit checks passed, the global check failed on an integration test no unit check covered, the failure was attributed to one unit with evidence, only that unit was reopened (fix-forward commit, no reset), dependents were rechecked and untouched files verified. Also a staged drill: the Unicode trap was known beforehand.
- Exercised (geom run): two units in separate `git worktree`s with two background subagents. Branch isolation, the revert inside one worktree not affecting the other, a shared append-only log with no corrupted rows, `--no-ff` merges without conflicts and worktree cleanup all verified with commands. Caveat: the timestamp windows only covered commit and check, so real overlap was not demonstrated (the log shows interleaving, with U1's attempt between U2's two attempts). Re-test with full-run START/END timestamps. Also a staged drill: U2's first-attempt failure was prepared.
- Exercised (strutil run): resume after an interruption by a fresh subagent with no procedural hints. The interruption was simulated: one unit done, one committed but not logged, one partial and uncommitted. Verified with commands: done commits kept (same hashes, one commit per unit), the missing row logged after re-running the check, the partial file replaced, frozen files intact, global check green. The Resume section above was written from the improvisations this subagent reported.
- Exercised (listutil runs): the Resume section re-tested with a harder interrupted state: a committed-but-unlogged unit whose check FAILS, a unit with two failed attempts already logged, and a partial uncommitted file. Two fresh subagents with no hints. Both reached the right end state, verified with commands: failed commits off the branch, done commit kept, one commit per unit, attempts counted from `results.tsv` (the last U3 row is attempt 3), frozen files intact, global green, no re-approval requested (the approval line in `PLAN.md` worked). The first subagent's hand-back was the literal word "placeholder", so its decisions are unknown; the second, told that its final reply IS the report, gave a full one. Staged drill: I built the interruption.
- Caveat from that report: my staged log had U3 attempts logged before U2, which cannot happen in a sequential run. The resuming agent ignored the order and counted per unit, which is now written into Resume step 4.
- Gaps the resume exposed (now fixed in the text): the approval of the checks was not on disk (the user had to state it), a leftover `RULES.md` from other runs polluted the resumed run, and nothing said how to classify committed-unlogged versus partial work or how to count attempts.
- Not yet exercised: real overlap measured with full-run timestamps, resuming mid-way through parallel worktrees (leftover `git worktree` entries), and resuming a run whose check files were tampered with.

## Notes

- `results.tsv` and `RULES.md` are not in the repo. Mention in the summary that they must be copied elsewhere if the user wants to keep them.
- Log the session with `logbook` (new, act per unit, finish) as the global rules require.
- Each unit's change is secure by default (least privilege, validated input, no secrets in code). When the task touches input, auth or secrets, the frozen check should cover it.
