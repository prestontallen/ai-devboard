# Contract — Worklog ID robustness (ticketless path) + tone coverage for worklog/devboard text

- **Date:** 2026-09-01
- **Tier:** 2 Feature
- **Status:** draft
- **Worklog:** wl-id-robustness

## Intent

Investigation (this session, ticketless `worklog task` path) found that the devboard
join key — the task file's `id` — is validated for uniqueness on the *ticketed*
path (`worklog add` checks the whole global `WORK.md` + open notes files) but
**not** on the *ticketless* `worklog task ... --id <slug>` path dev-context uses
for tier-1+ work with no worklog ticket. `resolveTaskPath()` →
`devboard.Find(id)` searches every repo directory in the devboard data dir by
filename alone; if `--id` collides with a file created under a *different*
repo, the command silently mutates that other project's task file. No error,
no warning.

Human reviewed three options (uniqueness check only / secondary UUID join key
/ primary-id UUID) and chose the uniqueness check: cheap, no schema change,
and doesn't degrade the human-facing surfaces (CLI args, devboard URLs,
WORK.md scanning, notes filenames) the way a UUID-primary-id would — directly
undercutting today's separate devboard-glanceability work.

Separately: the "Tone hook" in `dev-context/SKILL.md` only governs Phase 8
(Ship) text — commit messages, PR text, replies. Every other human-facing
string an agent writes into worklog/devboard (`worklog note`, `task decision
--why`, `task needs-you add`, `task waiting-on add`, `done --summary
/--feedback` — all rendered straight onto the devboard and into
WORK.md/archive) has no tone governance today. Human chose to extend it.

Bundled into one contract per human's choice: both are "which inputs/text
get governed" fixes discovered in the same investigation pass.

## Scope

**In:**
- `worklog/internal/cli/task.go`: `resolveTaskPath` gains a cross-repo
  collision guard — if `devboard.Find(id)` resolves to a file whose parent
  directory name differs from `devboard.RepoName()`, refuse (exit 1) unless
  a new `--force` flag is set. Applies regardless of `allowCreate` (mutating
  an existing cross-repo file via e.g. `plan done` is equally dangerous as
  creating one).
- `worklog/internal/cli/task_waiting.go`: same guard (it calls
  `resolveTaskPath` directly for `waiting-on resolve`).
- New `--force` persistent flag on the `task` command, alongside the
  existing `--id`/`--json`.
- Test coverage in `task_test.go` / `task_waiting_test.go` for: cross-repo
  refusal, `--force` bypass, same-repo re-entry unaffected, `--json` error
  shape, guard fires for both `allowCreate` values.
- `dev-context/SKILL.md`: broaden the Tone hook's stated scope beyond
  Phase 8, and add a cross-reference from the Devboard sync section's
  mandatory sync points (note/decision/needs-you/waiting-on/done-summary).
- `worklog/skill/SKILL.md`: matching pointer, so tone applies even when an
  agent writes worklog notes outside a dev-context-driven task.
- Deploy both edited skill docs to `~/.claude/skills/` (worklog via
  `worklog sync`; dev-context via direct copy, since it has no sync command)
  so this running session picks up the change.

**Out (explicitly not doing):**
- No change to `devboard.OnStart`/`OnDone`/`OnPR` (ticketed lifecycle) — id
  uniqueness there is already guaranteed upstream by `add`'s WORK.md/notes
  checks, and cross-repo re-discovery of the *same ticket* (e.g. resuming
  from a different checkout) is intended behavior, not a bug.
- No UUID scheme change, primary or secondary — explicitly declined.
- No change to `worklog add`'s existing uniqueness logic (already correct).
- No machine-enforced tone linting on the CLI (rejecting verbose input,
  length limits). Tone stays an authoring-time convention for the agent,
  exactly like the existing Ship-phase hook — advisory, not validated.
- No retroactive audit/fix of any ids that may have already collided under
  the old behavior (none known; a real hit would be a separate cleanup).
- No changes to `contract`/`fan-out` skill docs.

## Deliverables

- Updated `task.go`, `task_waiting.go`, their tests.
- Updated `dev-context/SKILL.md`, `worklog/skill/SKILL.md`.
- Deployed copies in `~/.claude/skills/{worklog,dev-context}/SKILL.md`.
- This contract.

## Acceptance criteria

| # | Criterion | Verify | Status |
|---|-----------|--------|--------|
| 1 | `--id X` created in repo A; later run with the same `--id X` from repo B, no `--force` → refused, exit 1, names the existing file/repo, repo B's dir untouched | new test: two temp DEVBOARD_DATA repo dirs | ☐ |
| 2 | Same scenario with `--force` → succeeds, adopts/mutates repo A's file | new test | ☐ |
| 3 | Same-repo re-entry (today's normal dev-context workflow: re-running `task phase`/`task plan` etc. against a file already created in the current repo) needs no `--force` and is unaffected | new test + existing suite green | ☐ |
| 4 | Guard fires for both `allowCreate=true` (e.g. `decision`) and `allowCreate=false` (e.g. `plan done`, `needs-you resolve`, `waiting-on resolve`) subcommands | test hits `resolveTaskPath` with both flag values | ☐ |
| 5 | `worklog start`/`done`/`pr` (ticketed lifecycle) unchanged: no `--force` flag added, no behavior change | existing tests green; no new flag on those commands | ☐ |
| 6 | `--json` mode surfaces the refusal as `{"error": "..."}` at the existing exit-code convention, not a panic or bare-text fallback | test | ☐ |
| 7 | `dev-context/SKILL.md`'s tone guidance explicitly covers `worklog note`, `task decision --why`, `task needs-you add`, `task waiting-on add`, `done --summary/--feedback`, not just Ship-phase text | grep + human review of wording | ☐ |
| 8 | `worklog/skill/SKILL.md` carries a matching pointer so tone applies even outside a dev-context-driven task | human review | ☐ |
| 9 | Deployed `~/.claude/skills/{worklog,dev-context}/SKILL.md` match the edited repo sources | diff after ship | ☐ |

## Definition of done (standing bar)

- [ ] `go test ./...` passes in `worklog/`
- [ ] `gofmt -l` clean on touched files
- [ ] No unrelated diff
- [ ] Both skill docs deployed (criterion 9)

## Constraints & assumptions

- `resolveTaskPath` is the single choke point for every `task` subcommand
  (11+ call sites via `mutateTask`, plus one direct call in
  `task_waiting.go`) — the fix lives there once, not per-subcommand, so the
  diff is additive/behavior-preserving in the common (same-repo) case
  rather than a rewrite.
- `--force` is a deliberate, rare escape hatch (e.g. the same conceptual
  repo checked out under two different directory names) — not documented
  as a routine flag.
- Tone extension is a documentation/instruction change only; no code
  validates it. Consistent with how the existing Ship-phase hook already
  works — an instruction the agent follows, not a constraint the CLI
  enforces.

## Risks & open questions

- **Blast radius:** `resolveTaskPath` is called from every `task`
  subcommand; a bug in the new guard could break normal same-repo usage
  worklog/dev-context relies on constantly. Mitigated by criterion 3's
  explicit regression test plus the full existing suite staying green —
  no fan-out risk scout run given the change is additive and the package
  has direct test coverage already (complexity downgraded medium after
  scope was pinned down in the Clarify conversation).
- **Skill-doc drift:** `~/.claude/skills/dev-context` has no automated sync
  command (unlike worklog's `sync`); deployment there is a manual copy at
  ship time (criterion 9) — a future edit to the repo source without a
  matching deploy would silently drift again. Not fixed here (out of
  scope: building a sync mechanism for non-worklog skills) — flagged for
  awareness.

## Amendments

| Date | Change | Why | Approved |
|------|--------|-----|----------|
