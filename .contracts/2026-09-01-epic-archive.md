# Contract — Epic archival in worklog done

- **Date:** 2026-09-01
- **Tier:** 2 (Feature)
- **Complexity:** medium
- **Status:** fulfilled (accepted 2026-09-01)
- **Worklog:** wl-epic-archive

## Intent

`worklog done` refuses epics ("epic archival not yet supported"), so an
epic whose children are all archived lingers in `## Next` forever — the
CLI even suggests archiving it, then can't. Extend `worklog done <id>` to
archive an epic once its children are complete, using the same archive
machinery tickets use, so the epic lifecycle actually terminates.

## Scope

**In:**
- `done.Run` accepts epics: replace the refusal with an open-children
  guard; archive entry via the existing render machinery (epic-shaped as
  needed); remove the epic block from WORK.md; leave `notes/<id>.md` in
  place as history
- Refusal path: open children remain → clear error naming them, distinct
  exit, no partial writes
- CLI text/help updated ("epic archival not yet supported" retired)
- Tests: happy path, open-children refusal, missing-notes-file behavior,
  archive entry golden shape

**Out (explicitly not doing):**
- Bulk archival (`done --all-completable` etc.)
- Un-archive / restore
- TUI changes
- Devboard UI work of any kind (owned by another agent; this task touches
  no files under `devboard/`)
- INDEX.md auto-update (stays deferred to reindex, as with tickets)

## Deliverables

- `worklog/internal/done/done.go` (+ tests), render/model touches as the
  scout findings require, `worklog/skill/SKILL.md` epic-lifecycle note
- Rebuilt binary via install.sh

## Acceptance criteria

| # | Criterion | Verify | Status |
|---|-----------|--------|--------|
| 1 | `worklog done <epic> --summary ...` on an epic with zero open children archives it: entry appended to the right month file, block gone from WORK.md, `notes/<id>.md` untouched | CLI test + live run on csk-integration | ✅ |
| 2 | With any open child (notes `- [ ]` line or a WORK.md block naming it as Parent), done refuses, names the open children, and changes no file | CLI test; byte-compare WORK.md/archive | ✅ |
| 3 | `worklog validate` passes after an epic archival (no dangling three-place or notes-file violations) | run validate in test + live | ✅ |
| 4 | Archived-epic entry is searchable the same way archived tickets are | `worklog search` after reindex | ✅ |
| 5 | Existing ticket `done` behavior is byte-identical (no regression) | existing done tests unchanged and green | ✅ |
| 6 | Epic archive entry carries a valid date line reindex can parse (epics have no Started; entry uses a Completed-only variant with a matching reindex regex) | golden test + reindex round-trip | ✅ |
| 7 | Epic-only metadata survives archival: entry carries Plan, Status, notes ref, and the child roster — `Plan` added to model.Block + parse | golden test; grep archive entry | ✅ |
| 8 | Completeness predicate is the union of notes checkboxes AND WORK.md `Parent:` blocks across ALL sections (Now/Next/Someday/Blocked/Waiting) — children exist in disjoint sources (`add --parent` writes notes only; `import` writes WORK.md only) | CLI tests seeded each way | ✅ |
| 9 | `[~]` (started) notes lines count as OPEN children; only `[x]` is complete | unit test on predicate | ✅ |
| 10 | Missing `notes/<id>.md` on an epic → hard refusal ("cannot determine completeness"), never vacuous success | CLI test | ✅ |
| 11 | Open-children refusal wins over summary-required (lookup precedes the summary gate), and the error names each open child with its source | CLI test asserting error text | ✅ |
| 12 | `start`/`add --parent`/`import` against an archived epic fail with a named error ("epic <id> archived on <date>, see archive/YYYY-MM.md") — never an internal render error or partial write | CLI tests for all three paths | ✅ |
| 13 | The archive entry marks the record as an epic (Type in ArchiveOpts + entry + reindex Record), so INDEX reads "archived epic", never a silent downgrade to ticket | golden + reindex test | ✅ |
| 14 | The epic's archive entry carries `**Notes**: notes/<id>.md` and reindex surfaces it — search on the epic id resolves both archive and notes history | search test after reindex | ✅ |
| 15 | `standup` does not double-count an archived epic as a completed ticket (epics reported as their own "epic closed" line or excluded — per Q4) | standup test with epic in window | ✅ |
| 16 | Agent-facing docs rewritten: SKILL.md and command.md no longer claim epic archival is unsupported; epic lifecycle section documents the terminal semantics | grep both files | ✅ |

## Definition of done (standing bar)

- [x] `go test ./...` green; new behavior tested; no new deps
- [x] No unrelated changes; no `devboard/` files touched
- [x] SKILL.md updated where agent-facing behavior changes

## Constraints & assumptions

- "All children complete" is determined from `notes/<id>.md` open-checkbox
  lines AND absence of WORK.md blocks naming the epic as Parent (both must
  agree — scout to confirm this is sufficient)
- Epics keep requiring `--summary`, same as tickets (assumption)

## Risks & open questions

Scout findings (blockers lens) driving criteria 6-11 above; verbatim
evidence in the scout transcript. Remaining open items:

- **Q1 (resolved by scout):** `devboardOnDone` fires for epics — exact
  slug match makes it a no-op unless the epic is dashboard-tracked, and
  for the documented epic-slug pattern flipping that task to done is
  exactly right
- **Q2:** are `--pr`/`--time` meaningful on epics? (proposed: accepted
  and archived if given, never prompted for)
- **Q3:** epics keep requiring `--summary` (assumption unchanged — the
  epic's closing summary is the archive's value)
- **Q4:** standup treatment of archived epics — proposed: an "epic
  closed: <title>" line, not a Completed count entry (children already
  counted)
- Risk: `FormatArchiveEntry` reuse is NOT viable as-is (date shape);
  entry variant must stay parseable by the same reindex pass
- Risk: archival is TERMINAL — `add --parent`/`import` can never target
  an archived epic (no un-archive in scope); error text must say
  "archived", not "not found", so agents don't retry blindly
- Risk: validate's three-place message would misdiagnose an archived
  parent as "missing epic"; message extended to distinguish (folded into
  criterion 12's error-naming work)

## Amendments

| Date | Change | Why | Approved |
|------|--------|-----|----------|
