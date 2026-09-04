# On-disk formats

Read this when you need to *read* WORK.md, an archive entry, or a notes file
and want to know what a field means. The CLI is the only writer — never
hand-edit these files to produce the shapes below. Every generated file
opens with an HTML-comment banner marking it as such; a write that finds a
hand-edited file refuses outright and names it.

- [Ticket block](#ticket-block)
- [Epic block](#epic-block)
- [Archive entry](#archive-entry)
- [Notes entry](#notes-entry)

## Ticket block

In `## Now` / `## Waiting` / `## Next` / `## Someday`:

```markdown
- [ ] **<TICKET-OR-TITLE>** — Short description
  - **ID**: <id>                          # lowercase-kebab, e.g. ent-3794
  - **Type**: ticket | spike | chore      # optional; epics use the epic format
  - **Parent**: <epic-id>                 # only if this is a child of an epic
  - **Repo**: <repo-name>                 # optional, omit if cross-repo
  - **Tags**: tag1, tag2                  # comma-separated, lowercase
  - **PR**: <url>                         # always rendered; value may be empty
  - **Started**: YYYY-MM-DD               # only when [~]
  - **Waiting since**: YYYY-MM-DD         # only while in ## Waiting
  - **Files**: `path/one.go`, `path/two.go`  # optional
  - **Acceptance**: one-line definition of done  # optional but recommended
  - **Notes**: notes/<id>.md              # if a notes file exists
  - **Status**: free-form one-liner       # optional
```

Bare `- [ ] Fix the typo` is also valid for trivial items — full metadata is
for non-trivial work.

## Epic block

In `## Next` / `## Someday` only — never `## Now`:

```markdown
- [ ] **<EPIC-KEY>** — Short description (epic)
  - **ID**: <id>                          # lowercase-kebab, e.g. ent-3634
  - **Type**: epic
  - **Repo**: <repo-name>
  - **Tags**: epic, <other-tags>
  - **Notes**: notes/<id>.md              # required for epics
  - **Plan**: <repo>/PLAN.md              # optional, points to the in-repo plan
  - **Active children**: <id-1>, <id-2>   # auto-maintained; "<none>" when empty
  - **Status**: free-form one-liner       # e.g. "Phase 1 next: ENT-3794 PR open"
```

Epics never receive `[~]`. Their state is implicit: they have active children
or they don't.

## Archive entry

In `archive/YYYY-MM.md`:

```markdown
# Archive — 2026-05

## 2026-05-18

### ent-3794 — Coding question test cases migration
- **Repo**: assessments-api
- **Tags**: migration, coding-questions
- **PR**: https://github.com/example/assessments-api/pull/4521
- **Files**: `migrations/035-create-coding-question-test-cases.sql`
- **Started → Completed**: 2026-05-15 → 2026-05-18
- **Summary**: One- or two-sentence outcome.
- **Feedback / Notes**:
  - Reviewer asked about backfill — none needed; documented in PR.
  - CockroachDB FK quirk: composite cascade unsupported, used trigger.
- **Time**: ~3h
```

Multiple entries on the same day share one `## YYYY-MM-DD` heading. Most
recent day at the top of the file.

When present, `Parent` and (on an epic's entry) `Children` render from the
store's parent/child relation, never from a value stored on the entry
itself — hand-editing either has no effect.

## Notes entry

In `notes/<id>.md`. Each note is a `## YYYY-MM-DD HH:MM` heading followed by
free-form markdown. New entries append at the bottom.

A pre-cutover epic's notes file may still carry a `## Children` checkbox
roster. It is legacy and frozen — nothing re-derives or flips it anymore.
The canonical backlog is the store's `ParentID` relation, rendered live as
`**Active children**:` in the epic's WORK.md block. New epics don't
scaffold a `## Children` section at all.
