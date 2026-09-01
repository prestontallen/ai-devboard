# Contract — waiting_on: the external-answer queue

- **Date:** 2026-09-01
- **Tier:** 2 (Feature)
- **Complexity:** medium
- **Status:** fulfilled (accepted 2026-09-01)
- **Worklog:** dvb-waiting-on

## Intent

`needs_you` covers "blocked on my human, resolvable in minutes." Nothing
covers "blocked on someone else, resolvable in days" — a question posted
to another team, a pending access request, a concern awaiting someone
else's decision. Those need to stay visible with their **age** and **who
owes the answer**, and when the answer arrives it must not evaporate:
it becomes part of the task's durable story. Question lifecycle is
telemetry (devboard); answers become system-of-record (worklog notes).

## Scope

**In:**
- Schema: `waiting_on` list — `{text, who, asked (YYYY-MM-DD), link,
  detail}`; documented with ownership row (agent-authored)
- CLI: `worklog task waiting-on add <text> --who <party> [--link]
  [--detail]` (stamps `asked` with today) and `waiting-on resolve <n>
  [--answer "..."]`; `--answer` appends a Decision ("<who> answered:
  ...") to the task file and, when the task carries a `worklog:` id,
  appends the answer to the ticket's notes file
- GUI: per-task "Waiting on" section in detail view; top-level
  aggregation band below Needs-You, calmer styling, per-entry who + age,
  age visually escalating past a threshold
- Skills: dev-context Devboard sync gains the sync point ("the moment a
  question goes to an external party…"); guidance on `worklog wait`
  (whole ticket blocked) vs `waiting_on` (one thread blocked)
- schema.md + examples updated

**Out (explicitly not doing):**
- A global/cross-task questions store (v-next if the need proves out;
  v1 attaches questions to the nearest task)
- Reminders/notifications or Slack integration — the link field points
  at where it was asked, nothing more
- Auto-nagging or escalation workflows
- Changes to `worklog wait` semantics; no `OnWait` devboard hook (a
  parked ticket's dashboard staleness is a known, intentional gap —
  schema.md notes that `waiting_on` age is independent of the ticket's
  `Waiting since`)
- CSK-DEVBOARD-TIMESTAMPS (separate ticket; coordinate schema additions
  if it lands first — additive fields, no conflict expected)

## Deliverables

- `worklog/internal/devboard/devboard.go` (WaitingItem type),
  `worklog/internal/cli/task.go` (+ tests)
- `devboard/static/index.html` (band + section, following the redesign's
  patterns), `devboard/schema.md`, `devboard/examples/`
- `dev-context/SKILL.md` sync-point edit; sync.sh redeploy if worklog
  skill text changes

## Acceptance criteria

| # | Criterion | Verify | Status |
|---|-----------|--------|--------|
| 1 | `waiting-on add "q" --who platform` creates an entry with `asked` = today; `--who` is required (exit 64 without); add/resolve mutate exactly their field, atomically, `--json` supported | CLI tests | ✅ |
| 2 | `resolve <n> --answer "..."` removes the entry AND appends a decision "<who> answered: <answer>"; with a `worklog:` id the answer also lands in `notes/<id>.md` | CLI test + notes file assert | ✅ |
| 3 | `resolve <n>` without `--answer` removes the entry and records a "closed unanswered" decision (nothing silently evaporates) | CLI test | ✅ |
| 4 | Detail view renders the Waiting-on section (who, age, link, detail); tasks without `waiting_on` render no section, no errors | browser | ✅ |
| 5 | Top-level band aggregates all waiting_on entries across tasks with who + age; absent when none exist | browser | ✅ |
| 6 | Age display is relative ("3d") and visually escalates past 7 days | browser with backdated entry | ✅ |
| 7 | `waiting_on` leaves the "Other" section (KNOWN updated) AND `custom_field` in canonize-scripts.yaml still lands in Other in the same render — the fallback stays proven; nole/embed-retry.yaml gains the waiting_on example | browser, both halves | ✅ |
| 8 | Ticket `done` converts unanswered waiting_on entries to "unanswered at close" decisions, then clears them | CLI test | ✅ |
| 9 | dev-context sync section cites only commands present in `worklog task --help` (criterion-12 discipline holds) | grep vs help | ✅ |
| 10 | Existing example task files still render fully (no regression); schema.md documents the field + ownership | browser + review | ✅ |
| 11 | Age renders via a new date-aware helper (`asked` is an ISO date string, not epoch seconds — `ago()` yields NaN on it); missing/unparseable `asked` renders as "?" without breaking the entry | unit-style browser check | ✅ |
| 12 | The band uses its own modifier class (`.band.wait`, never `.calm` — that class injects a ✓) and a non-amber token; amber `--hold` stays reserved for needs_you so the bands stay visually ranked | browser + code review | ✅ |
| 13 | Cards get a distinct waiting marker and a separate `waitCount()` stat tile; waiting_on does NOT set the `attn` class, the amber flag chip, or change the sort comparator's primary key (external blockage never outranks human blockage) | browser + code review | ✅ |
| 14 | The "nothing needs you" calm line stays honest: three-state copy — both empty, "nothing needs you · N waiting on others", suppressed when needs exist | browser, all three states | ✅ |
| 15 | waiting_on `detail` renders through the redesign's fold() path with its own fold-key prefix (`wait<i>`), never sharing state with needs_you folds | browser: fold both on one task | ✅ |
| 16 | Late answers reach the record even post-done: resolve `--answer` appends to `notes/<id>.md` when the file exists or the ticket is live; when the ticket is archived with no notes file, the answer lands as a task-file decision plus a stderr notice saying so — never a hard failure, never silent loss | CLI tests: live, archived-with-notes, archived-without | ✅ |
| 17 | Failure ordering is safe: the decision is written inside the same atomic Mutate that removes the entry (durable record first); the worklog-notes append is best-effort afterward and warns on failure — an entry is never deleted with no record anywhere | code review + fault-injection test | ✅ |
| 18 | `waiting-on resolve` exits 0 with a stderr notice when the worklog dir is absent or the id is unknown (devboard-only setups keep working); schema.md documents the one sanctioned devboard→worklog write (answers) as an explicit ownership carve-out | CLI test + schema review | ✅ |
| 19 | The Q1 close-out (unanswered → "unanswered at close" decisions) lives in ONE shared function used by both paths: ticketed `worklog done` (OnDone) and the ticketless flow via `waiting-on resolve all`; dev-context step 7 cites the ticketless command | package test ×2 paths + skill grep | ✅ |

## Definition of done (standing bar)

- [x] `go test ./...` green; new behavior tested; no new deps
- [x] Devboard stays python-alpine + PyYAML only (server untouched if
      pass-through holds)
- [x] No unrelated changes; redesign's UI patterns followed, not fought
- [x] schema.md/examples/skills updated

## Constraints & assumptions

- `asked` is stamped by the CLI (today), overridable via `--asked` for
  backfill (assumption)
- Age threshold for visual escalation: 7 days (assumption, cheap to tune)
- The server passes task YAML through generically — CONFIRMED by scout
  (server.py entry["task"] = data); zero server changes
- UI integration follows the redesign's own patterns (bands, folds,
  tokens) — criteria 12-15 pin the specifics so the two attention queues
  stay visually and behaviorally distinct

## Risks & open questions

- **Q1 (resolved, human):** `worklog done` converts each unanswered
  `waiting_on` entry to a decision "unanswered at close: <text> (<who>)"
  then clears — the file closes honestly without blocking done.
- **Q2 (resolved, human):** `--who` is required on add (exit 64 without
  it) — a question without an owner is the failure mode this feature
  exists to prevent.
- Risk (scout): `note.Append`/`EnsureFile` resolve ids via WORK.md and
  write a `**Notes**:` ref back — unusable for archived tickets and an
  ownership inversion; criteria 16-18 pin the resolution (direct notes
  append where safe, decision-always, sanctioned-write carve-out)
- Risk (scout): `mutateTask` is one-shot with no data out of the
  closure — `resolve --answer` needs its own flow capturing who/text
  before deletion; criterion 17 pins the ordering

## Amendments

| Date | Change | Why | Approved |
|------|--------|-----|----------|
