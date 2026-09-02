# Contract — worklog edit verbs

- **Date:** 2026-09-01
- **Tier:** 2 Feature
- **Status:** in-progress
- **Worklog:** adb-worklog-edit

## Intent

The skills mandate operations the CLI cannot perform, so agents are pushed
into hand-editing `WORK.md` — the exact thing the worklog hard rules forbid.
Three concrete holes: (1) no writer for metadata on an *existing* block, so
`**Acceptance**` can only be set at `worklog start` time and `**Status**`
can be read by `summarize` but never written; (2) `worklog task
scorecard|plan` are add-and-set-state only, so a contract amendment cannot
reword or drop an item; (3) the contract skill's "Worklog integration"
section instructs the agent to mirror the acceptance summary into the
ticket with no command to do it with.

The point is to close the gap between what the skills *require* and what
the CLI *offers*, then delete the "fall back to manual edits" escape hatch
from the skill text. A documented fallback to hand-editing is a standing
invitation to corrupt the file; it should stop being documented because it
stops being necessary.

## Scope

**In:**
- New `worklog edit <id>` command: sets/clears `Title`, `Repo`, `Tags`,
  `Files`, `Acceptance`, `Status`, `Notes` on an existing block in any
  section.
- New generic `render.SetBlockField` splicer, with canonical field-order
  insertion; existing `SetBlockNotesRef` / `SetBlockWaitingSince` refactored
  to delegate to it (their exported signatures and tests are unchanged).
- `worklog task scorecard edit|remove <n>` and `worklog task plan
  edit|remove <n>`.
- `skill/SKILL.md`: command-map preamble loses the manual-edit fallback
  sentence; new rows for `edit`; hard-rules block updated.
- `contract/SKILL.md`: "Worklog integration" section gets the CLI path.

**Out (explicitly not doing):**
- `worklog move` (Next↔Someday, demote Now→Next). It is in the ticket
  notes as "optionally" but not in the ticket's Acceptance line. Deferred
  to its own ticket.
- Editing `ID`, `Type`, `Parent`, `Started`, `Waiting since`, `Source`,
  `Active children`. These are structural/lifecycle-owned — `ID` is the
  join key, `Started`/`Waiting since` are stamped by `start`/`wait`,
  `Active children` is maintained by the epic machinery. Renaming an ID
  is a separate, much larger job (INDEX, notes, archive, devboard).
- `**PR**`, which already has a dedicated writer (`worklog pr`). `edit`
  will not offer `--pr`; it points at `worklog pr` instead.
- Editing archived entries or notes files.
- `worklog task decision|code|needs-you` edit/remove verbs. Only
  `scorecard` and `plan` are named in the acceptance line; `needs-you`
  already has `resolve`.
- Fixing `devboard.RepoName()`'s git-worktree bug (found during intake,
  captured via `worklog feedback append`). Real, adjacent, not this ticket.
- Renumbering protection for `scorecard|plan remove` (index shift). Warned
  in help text, not enforced.
- Fixing `worklog lint-specs`. It compares three audience-specific rule
  blocks byte-for-byte and so reports drift unconditionally, on pristine
  HEAD as much as here. Filed as `wl-lint-specs-premise`. See amendment 1.

## Deliverables

- `worklog/internal/render/field.go` — `SetBlockField`, `SetBlockTitle`,
  canonical field order table.
- `worklog/internal/render/field_test.go`
- `worklog/internal/render/work.go` — `SetBlockNotesRef` /
  `SetBlockWaitingSince` reduced to delegating wrappers.
- `worklog/internal/cli/edit.go` + `edit_test.go` — the `edit` command.
- `worklog/internal/cli/root.go` — one line registering `newEditCmd()`.
- `worklog/internal/cli/task.go` — `edit`/`remove` verbs on the two
  subcommands.
- `worklog/internal/cli/task_test.go` — coverage for the new verbs.
- `skill/SKILL.md`, `contract/SKILL.md` — doc updates.

## Acceptance criteria

| # | Criterion (given/when/then) | Verify | Status |
|---|-----------------------------|--------|--------|
| 1 | Given a ticket with no `**Acceptance**` line, when `worklog edit <id> --acceptance "X"` runs, then the line is inserted in canonical position (after `Files`, before `Status`) and every other line in `WORK.md` is byte-identical. | `go test ./internal/cli -run TestEditInsert`; manual `worklog edit --dir <tmp>` + `git diff` | ☐ |
| 2 | Given a ticket that already has the field, when `edit` sets it again, then the line is rewritten in place (no duplicate, no move). | `go test ./internal/render -run TestSetBlockFieldRewrite` | ☐ |
| 3 | Given a flag passed with an empty value (`--acceptance ""`), then the metadata line is removed entirely; given the flag not passed at all, then the field is untouched. | `go test ./internal/cli -run TestEditClearVsAbsent` | ☐ |
| 4 | When `worklog edit <id> --title "New"` runs, then only the text after the em dash on the bullet line changes — checkbox state and the bold ID are preserved. | `go test ./internal/render -run TestSetBlockTitle` | ☐ |
| 5 | `--tags`/`--files` accept a comma-separated list and round-trip through the parser to the same slice. | `go test ./internal/cli -run TestEditCSVRoundTrip` | ☐ |
| 6 | Sad path: `worklog edit nope --status x` exits non-zero with a "block not found" message; in `--json` mode the sole stdout document is `{"error": ...}` and `WORK.md` is unmodified. | `go test ./internal/cli -run TestEditUnknownID` | ☐ |
| 7 | Sad path: `worklog edit <id>` with no field flags exits 64 with usage guidance rather than silently rewriting the file. | `go test ./internal/cli -run TestEditNoFlags` | ☐ |
| 8 | `worklog task scorecard edit <n> "text"` replaces the criterion text (and `--verify` when passed) leaving `status` untouched; `worklog task plan edit <n> "text"` does the same leaving `state` untouched. | `go test ./internal/cli -run TestTaskItemEdit` | ☐ |
| 9 | `worklog task scorecard remove <n>` / `plan remove <n>` delete the item; a subsequent `worklog task ... --json` read shows the list one shorter with remaining items in order. | `go test ./internal/cli -run TestTaskItemRemove` | ☐ |
| 10 | Sad path: `edit`/`remove` with an out-of-range or non-numeric index exits 64 with `index must be 1..N`, and the task YAML is byte-identical. | `go test ./internal/cli -run TestTaskItemBadIndex` | ☐ |
| 11 | Existing `SetBlockNotesRef` / `SetBlockWaitingSince` behavior is unchanged after the refactor. | `go test ./internal/render` (pre-existing tests, unmodified) | ☐ |
| 12 | `skill/SKILL.md` no longer tells agents to fall back to manual edits, and `contract/SKILL.md`'s Worklog integration section names the concrete command. | `grep -c "fall back to manual edits" skill/SKILL.md` → 0; read `contract/SKILL.md` | ☐ |

## Definition of done (standing bar)

- [ ] All existing tests pass (`make test` in `worklog/`)
- [ ] New behavior covered by tests
- [ ] `make vet` clean
- [ ] No unrelated changes in the diff
- [ ] User-facing behavior changes documented (`skill/SKILL.md` command map,
      `--help` text)

## Constraints & assumptions

- `WORK.md` is mutated by **line splice** (`render` package doc: "preserving
  every untouched line byte-for-byte"). All new writers must follow that —
  no re-render of a whole block.
- Canonical field order is whatever `render.FormatTicketBlock` emits:
  `ID · Type · Parent · Repo · Tags · PR · Source · Notes · Started ·
  Waiting since · Files · Acceptance · Status`. `SetBlockField` derives its
  insertion point from that single table, so new fields stay consistent.
- **Assumption:** the "flag present but empty clears the field" convention
  (detected via cobra's `Flags().Changed`) is the right ergonomics, chosen
  to match `SetBlockWaitingSince`'s existing empty→remove semantics rather
  than adding a separate `--clear <field>` flag.
- **Assumption:** `scorecard edit` takes the new text positionally
  (`edit <n> <text>`) for symmetry with `add <text>`; this widens the
  subcommands' `Args` from `ExactArgs(2)` to `RangeArgs(2,3)` with
  per-verb validation.
- Work happens in the git worktree `.claude/worktrees/adb-worklog-edit` on
  branch `worktree-adb-worklog-edit`, per the isolation decision at intake.
- **Deviation from the contract skill:** step 6's fan-out risk scout was
  not run — this session is under a standing "no subagents unless
  requested" instruction. The risks below come from reading the code
  directly instead.

## Risks & open questions

- **Blast radius is the live `WORK.md`.** A bad splice corrupts real data.
  — Mitigation: every test drives the CLI against a `--dir <t.TempDir()>`
  fixture; no test touches `~/.local/share/worklog`. Manual verification
  also runs against a temp dir.
- **Insertion-point regression in the refactor.** Folding two hand-rolled
  insertion heuristics into one table-driven one could silently move lines.
  — Mitigation: criterion 11 keeps the existing render tests unmodified as
  the equivalence proof. (Both current heuristics already agree with
  canonical order — Notes lands after PR/before Started, Waiting since
  after Started — so the fold should be behavior-preserving.)
- **Index shift on `remove`.** `dev-context` has agents addressing scorecard
  items by number; removing item 2 renumbers 3→2, so a later
  `scorecard pass 3` hits the wrong criterion. — Mitigation: documented in
  `--help` and in the skill command map. Not enforced; flagged as a known
  sharp edge.
- **`edit` overlaps `worklog pr`/`wait`/`start` as a writer.** — Mitigation:
  the excluded-field list above is enforced in code (no flags exist for
  them), so there is exactly one writer per field.
- **Open question:** should `worklog edit` also accept `--section` to move
  a ticket between Next/Someday? Answered at drafting: **no** — that's the
  deferred `worklog move` ticket, kept out so `edit` means "change this
  block's fields" and nothing else.

## Amendments

| Date | Change | Why | Approved |
|------|--------|-----|----------|
| 2026-09-01 | Dropped criterion 13 and the matching definition-of-done line; removed `README.md` from scope and deliverables. | `worklog lint-specs` compares three deliberately audience-specific rule blocks byte-for-byte, so it exits 1 on pristine HEAD and the criterion could never pass. The tool's premise is the bug; filed as `wl-lint-specs-premise`. | Preston |
