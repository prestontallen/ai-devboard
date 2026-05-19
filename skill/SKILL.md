---
name: worklog
description: |
  Maintains Preston's personal daily worklog at ~/.local/share/worklog/ — a markdown-based
  task system with an active front page (WORK.md, capped at 5 in-flight tickets),
  epic containers in `## Next` with auto-maintained Active-children pointers, monthly
  archives, per-ticket notes, and a searchable INDEX.md spine. Use at the start of
  EVERY session to read WORK.md and orient on active work; use whenever tickets are
  added, started, completed, or referenced; use when the user asks "what am I working
  on", "have we hit X before", "log this", or anything that should persist across
  sessions. Triggers on session start, on ticket state transitions, and on questions
  about prior work.
tool: workflow
concern: process
---

# Worklog

This skill is the agent's interface to Preston's persistent worklog. The worklog
solves the cross-session amnesia problem: every session reads the same `WORK.md`,
every completed item is preserved in a searchable monthly archive, and every epic
gets a notes file.

The system is **agent-maintained**. Preston should rarely have to edit any worklog
file by hand — the agent reads, updates, and archives as work happens.

## Layout

```
~/.local/share/worklog/
├── WORK.md           ← front page: ## Now (tickets only, ≤5) / ## Next / ## Someday
├── archive/
│   └── YYYY-MM.md    ← completed items, full metadata + Summary + Feedback/Notes
├── notes/
│   └── <id>.md       ← long-form notes for an epic (canonical child list) or ticket
└── INDEX.md          ← search spine: by ticket, by tag, by repo, by archive month
```

Everything is plain markdown, no frontmatter on data files. `<id>` is lowercase-kebab
matching the ticket key (e.g. `ent-3794`) or another stable slug.

## CLI command map for agents

The `worklog` Go binary is the canonical mutation surface. For every
intent below, use the CLI where available; fall back to manual edits
(documented per-section below) only when CLI is not yet implemented.

| Intent | CLI command | Status |
|---|---|---|
| Read current state | `worklog status` (`--json` for machines) | available |
| Validate invariants | `worklog validate` (`--json` for machines) | available |
| Add standalone ticket | `worklog add --title X --id Y --section [Next\|Someday] --json` | available |
| Add an epic | `worklog add --type epic --id <id> --title "..."` (creates `notes/<id>.md` scaffold) | available |
| Add a child of an epic | `worklog add --parent <epic-id> --id <child-id> --title "..."` | available |
| Start a ticket | `worklog start <id>` (standalone or child-of-epic) | available |
| Complete a ticket | `worklog done <id> --summary "..."` (sets `epicCompletable: true` when last child) | available |
| Set PR on ticket | `worklog pr <id> <url>` (read with `worklog pr <id>`; `--clear` empties; `--edit` opens Huh prompt; `--json` for machines) | available |
| Search prior work | `worklog search <term>` (`--deep`, `--limit`, `--json`, `--plain` modes; INDEX-first with full-text fallback) | available |
| Open / append notes | manual: edit `notes/<id>.md` | manual-only |
| Deploy skill files | `worklog sync` (`--dry-run` / `--check` modes) | available |
| Detect rule drift | `worklog lint-specs` (`--print` mode) | available |
| Rebuild `INDEX.md` | `worklog reindex` (`--dry-run` / `--json` modes) | available |

### Conventions

- `--json` mode: every read command emits a single JSON document to
  stdout (success or error). Parse it as the single source of truth;
  ignore stderr in `--json` mode unless it's a system-level error.
- Errors in `--json` mode have the shape `{"error": "<message>"}`.
- `worklog add` requires `--title` and `--id` when stdin is not a TTY,
  else it falls into the human form (which agents must not use).
- `worklog tui` is for humans only; never invoke from an agent context.
- `worklog --dir <path>` overrides the data directory (default
  `~/.local/share/worklog/`). Honors `WORKLOG_DIR` env var as fallback;
  when both are set, the `--dir` flag wins.
- **Flag ordering is flexible.** Cobra accepts global flags
  (`--dir`) either before or after the subcommand and its own flags.
  Canonical (for readability):
  `worklog --dir <path> <subcommand> [--subcommand-flags]`.
- `cap: 0` on a section in `status --json` output means **uncapped**,
  not zero-limit. Only `## Now` has a cap (5). `## Next` and
  `## Someday` are uncapped (`cap: 0`).

### Exit codes

| Code | Meaning |
|---|---|
| `0` | success, no violations |
| `1` | domain error (duplicate ID, invalid section, WORK.md missing) |
| `2` | `validate` found violations |
| `64` | usage error (missing required flags, conflicting modes) |

Without `--json`, errors print as plain text on stderr and the same
exit codes apply. With `--json`, errors are `{"error": "<message>"}`
on stdout. Exit codes are identical between text and JSON modes.

### What `worklog validate` actually checks

Run after any mutation as a sanity check. The validator emits the
following check IDs (each may appear 0+ times in `violations`):

| Check ID | Catches |
|---|---|
| `work-md-exists` | WORK.md is missing (hard fail, exit 1) |
| `now-cap` | More than 5 tickets in `## Now` |
| `no-top-level-x` | A `[x]` block lingering in WORK.md instead of archive |
| `started-on-active` | A `[~]` ticket in Now without a `**Started**:` date |
| `notes-file-exists` | A block references `notes/<id>.md` but the file is missing |
| `index-refs-exist` | INDEX.md references an archive or notes file that's missing |
| `three-place-consistency` | A child in Now whose epic / notes / parent linkage disagrees |

The `--json` output shape is:

    {
      "dir": "/home/.../worklog",
      "workMDMissing": false,
      "violations": [
        {"check": "now-cap", "message": "## Now has 6 tickets, cap is 5"}
      ],
      "infos": ["INDEX.md not present at ...; skipping index-refs-exist"],
      "violationCount": 1
    }

`infos` are non-fatal notices (e.g. a missing INDEX.md skips the
`index-refs-exist` check rather than failing). Heuristic for surfacing:

- **Surface** when the missing thing is something the user might want
  to create (e.g. "no INDEX.md yet — want me to bootstrap one?").
- **Suppress** when the info is steady-state and not actionable
  (e.g. a long-existing worklog with no INDEX.md by design).

When in doubt, surface on the first validation against a given dir
and suppress on subsequent ones in the same session.

## Required behavior

### 1. On every session start

Run `worklog status --json` and use the JSON document to orient. The
fields you care about for the opening response:

- `sections[0].count` and `sections[0].cap` — how full `## Now` is.
- `sections[0].blocks[*]` — the in-flight tickets themselves.
- For each block in `sections[1]` (`## Next`) where `type == "epic"`:
  its `activeChildren` array, so we surface epics that already have
  work in flight.

Example invocation and shape (one block sampled to show the full set of
metadata fields a block exposes):

    $ worklog status --json
    {
      "dir": "/home/.../worklog",
      "sections": [
        {
          "name": "Now",
          "count": 1,
          "cap": 5,
          "blocks": [
            {
              "id": "ent-3794",
              "state": "active",
              "title": "Migrate test cases",
              "type": "ticket",
              "parent": "ent-3634",
              "repo": "assessments-api",
              "tags": ["migration", "coding-questions"],
              "started": "2026-05-15",
              "pr": "",
              "files": [],
              "acceptance": "",
              "notesRef": "",
              "status": "",
              "activeChildren": []
            }
          ]
        },
        {"name": "Next",    "count": 2, "cap": 0, "blocks": [...]},
        {"name": "Someday", "count": 1, "cap": 0, "blocks": [...]}
      ]
    }

The `state` field is one of `"pending"`, `"active"`, `"done"`. Epic
blocks have `type == "epic"` and a non-empty `activeChildren` array when
children have been promoted to `## Now`.

**Bare items.** A trivial entry like `- [ ] Reorganize CI pipeline` in
`## Someday` (no `**ID**:` metadata) emits `"id": ""` in JSON. When
counting items, count by block presence, not by ID truthiness — bare
items have an empty ID but still count.

Summarize in one or two lines what's in Now, and call out any epics
in Next whose `activeChildren` is non-empty. Example response:
*"Picking up where we left off — `## Now` is empty. `## Next` has the
ENT-3634 and ENT-3846 epics; neither has active children yet."*

Do this **before** asking the user what they want, unless they've
already started talking about something specific.

If `worklog status --json` returns `{"error": "WORK.md not found ..."}`,
the worklog data dir is missing or uninitialized — tell the user and
ask whether to recreate or initialize.

**Manual fallback** (if the binary is unavailable): read
`~/.local/share/worklog/WORK.md` directly and parse the same shape by
hand. The CLI path is strongly preferred so the cap is consistent and
the schema can't drift.

### 2. Adding a new ticket or epic

When the user mentions new work that should be tracked (a Jira ticket
they were just assigned, a follow-up they want to remember, an epic
they're scoping), the path branches by what kind of thing it is.

#### Standalone ticket (CLI ✓)

    worklog add --title "Refactor auth middleware" \
                --id auth-1 \
                --section Next \
                --tags refactor,auth \
                --json

Required flags: `--title`, `--id`. Optional: `--repo`, `--tags`
(comma-separated), `--acceptance`, `--section` (default `Next`; only
`Next` and `Someday` are valid — `Now` is reserved for in-progress
work, promoted via the `start` operation after the ticket exists).

**Choosing the ID.** Prefer the user's ticket key when they mention one
(`ENT-3794`, `AUTH-1`, etc., lowercased). If the user has no ticket
key, derive a 2–4 word lowercase-kebab slug from the title — e.g.
*"rewrite the auth middleware"* → `auth-middleware-rewrite`. Keep it
short and stable; the ID is referenced from notes, archives, and INDEX.

For an epic, use `worklog add --type epic` instead (see the Epic
subsection below). For a child of an existing epic, use
`worklog add --parent <epic-id>`. These two flags must NOT be combined
— refusing that combination is enforced at the CLI level.

The command emits a JSON success object:

    {
      "status": "added",
      "id": "auth-1",
      "title": "Refactor auth middleware",
      "section": "Next",
      "workMD": "/home/.../WORK.md",
      "warnings": ["INDEX.md not updated; run `worklog reindex` when convenient"]
    }

The `warnings` array is load-bearing — read it and surface anything
load-bearing to the user. The INDEX.md warning is expected on every
mutation; clear it by running `worklog reindex` periodically.

Error shapes:

- Duplicate ID: `{"error": "ID \"auth-1\" already exists in WORK.md"}`,
  exit 1.
- Missing required flag outside a TTY:
  `{"error": "add requires --title and --id when stdin is not a TTY"}`,
  exit 64.
- Invalid section: `{"error": "section \"Foo\" is invalid (use Next or Someday)"}`,
  exit 1.

#### Epic (CLI ✓)

    worklog add --type epic \
                --id epic-a \
                --title "Cross-cutting refactor" \
                --repo api \
                --tags refactor,epic \
                --section Next \
                --json

Required: `--type epic`, `--id`, `--title`. Optional: `--repo`,
`--tags`, `--section` (default `Next`; only `Next` and `Someday` are
valid — epics never occupy `## Now`).

The command:

1. Appends an epic block to the chosen section with
   `**Type**: epic`, `**Notes**: notes/<id>.md`, and
   `**Active children**: <none>`.
2. Creates `notes/<id>.md` with a scaffold: title heading, a
   `## Background` section (placeholder for Jira link / plan ref /
   open questions), and a `## Children` section (initially empty).

JSON success shape:

    {
      "status": "added",
      "kind": "epic",
      "id": "epic-a",
      "title": "Cross-cutting refactor",
      "section": "Next",
      "workMD": "/home/.../WORK.md",
      "notesPath": "/home/.../notes/epic-a.md",
      "warnings": ["INDEX.md not updated; run `worklog reindex` when convenient"]
    }

Errors:

- `--type epic` AND `--parent <id>` → exit 1, `ErrEpicHasNoParent`
- Pre-existing `notes/<id>.md` on disk → exit 1, `ErrNotesAlreadyExists`
- ID collision with WORK.md or any open child in notes → exit 1
- Invalid section (anything other than Next / Someday) → exit 1

There is **no Huh form fallback** for epic creation — flags only.

#### Child of an epic (CLI ✓)

    worklog add --parent epic-a \
                --id child-1 \
                --title "Set up the schema migration" \
                --json

Required: `--parent` (must resolve to an existing `Type: epic` block
in WORK.md), `--id`, `--title`.

The command appends a `- [ ] <child-id>: <title>` line to the parent
epic's `notes/<epic-id>.md`. The insertion point is:

1. End of the parent's `## Children` section, if one exists.
2. Else, immediately after the last existing checkbox line in the file.
3. Else, at end of file with a leading blank line.

**No WORK.md mutation occurs.** Children live in the notes file until
promoted to `## Now` via `worklog start <child-id>`.

JSON success shape:

    {
      "status": "added",
      "kind": "child",
      "id": "child-1",
      "title": "Set up the schema migration",
      "parent": "epic-a",
      "notesPath": "/home/.../notes/epic-a.md",
      "warnings": ["INDEX.md not updated; run `worklog reindex` when convenient"]
    }

Errors:

- `--parent` doesn't resolve to an epic in WORK.md → exit 1, `ErrParentEpicNotFound`
- ID collision (WORK.md or any notes file) → exit 1, `ErrIDCollisionInNotes`

No form fallback — flags only.

### 3. Starting a ticket (Pending → In progress)

#### Standalone ticket (CLI ✓)

    worklog start <id> --json

Moves the ticket from its current section (`## Next` or `## Someday`) into
`## Now`, flips state to `[~]`, stamps today's date.

Optional `--repo`, `--tags`, `--acceptance` flags override the source
block's metadata on the way through. Useful when promotion is a good
moment to update those.

#### Child of an epic (CLI ✓)

    worklog start <child-id> --repo api --tags backend --json

Looks up the child in the appropriate `notes/<epic-id>.md` (any notes
file that has a `- [ ]` line whose first token is `<child-id>`), creates
a new ticket block in `## Now` with `**Parent**: <epic-id>`, and appends
the child's ID to the epic's `**Active children**:` field. The child's
checkbox in `notes/<epic-id>.md` stays `[ ]` — that's intentional; it
flips to `[x]` only on `done`.

Use `--repo`, `--tags`, `--acceptance` to populate the newly-created
block (the notes-file line only carries ID + title).

#### Refusals

| Case | Error |
|---|---|
| `<id>` not found in WORK.md or any notes file | exit 1, `ErrIDNotFound` |
| `<id>` is already `[~]` in `## Now` | exit 1, `ErrAlreadyStarted` |
| `## Now` would exceed cap of 5 | exit 1, `ErrCapExceeded` (lists current Now IDs) |
| `<id>` is an epic | exit 1, `ErrEpicCannotStart` (lists startable children) |

#### JSON success shape

    {
      "status": "started",
      "id": "auth-1",
      "title": "Refactor auth middleware",
      "section": "Now",
      "parent": "",
      "started": "2026-05-19",
      "workMD": "/home/.../WORK.md",
      "warnings": ["INDEX.md not updated; run `worklog reindex` when convenient"]
    }

For child-of-epic promotions, `parent` is the epic's ID.

#### JSON error shapes

    {"error": "ticket ID not found: \"foo\""}
    {"error": "## Now is at cap (5/5); current Now: a-1, b-2, c-3, d-4, e-5"}
    {"error": "ticket is already in ## Now: \"x-1\" is already in ## Now"}
    {"error": "epics do not occupy ## Now; epic \"epic-a\" has startable children: child-2"}
    {"error": "epics do not occupy ## Now; epic \"epic-a\" has no startable children (all in progress)"}

The epic-refusal error filters out children that are already in `## Now`,
so "startable" means "open in notes AND not already in flight."

#### Hard rule reminders

- `## Now` holds **tickets only**, never epics. Starting an epic is
  always a refusal.
- Children of an epic live in `notes/<epic-id>.md` until promoted to
  `## Now`. Promotion creates a new block in WORK.md but does NOT tick
  the notes-file checkbox.
- The cap (5) is enforced by the CLI; you don't need to count by hand.

### 4. Completing a ticket (In progress → Archived)

#### Standalone or child ticket (CLI ✓)

    worklog done <id> --summary "..." --feedback "bullet 1" --feedback "bullet 2" --json

Performs the full archive operation:

1. Appends an archive entry to `archive/YYYY-MM.md` (creates the file
   with a `# Archive — YYYY-MM` header if it's the first archive of
   the month).
2. If the ticket was a child of an epic: flips its checkbox in
   `notes/<epic-id>.md` from `[ ]` to `[x]`, and removes its ID from
   the epic's `**Active children**:` field.
3. Removes the ticket block from `WORK.md`.

Days within an archive month-file are reverse-chronological (newest
first). Entries within a day are also reverse-chronological — the new
entry always lands at the top.

#### Flags

| Flag | Purpose |
|---|---|
| `--summary` | Required. One or two sentences describing the outcome. |
| `--feedback "..."` | Optional, repeatable. Each value becomes a bullet under `Feedback / Notes`. |
| `--time "~3h"` | Optional, free-form effort estimate. |
| `--pr <url>` | Optional. Overrides any `**PR**:` field that was on the live block. |
| `--completed YYYY-MM-DD` | Optional. Defaults to today. Useful for back-dating an archive. |
| `--json` | Emit JSON instead of styled text. |

When `--summary` is missing AND stdin is a TTY, a Huh form opens to
collect Summary, Feedback (multi-line, one bullet per line), and Time.
When `--summary` is missing AND stdin is NOT a TTY (e.g. piped), `done`
exits 64 with `done requires --summary when stdin is not a TTY`.

#### JSON success shape

    {
      "status": "archived",
      "id": "ent-3794",
      "title": "Migrate test cases",
      "archivePath": "/home/.../archive/2026-05.md",
      "completed": "2026-05-19",
      "parent": "ent-3634",
      "epicCompletable": false,
      "warnings": ["INDEX.md not updated; run `worklog reindex` when convenient"]
    }

`parent` is empty for a standalone ticket. **`epicCompletable: true`**
means the just-archived ticket was the last `- [ ]` child in
`notes/<parent>.md` — the epic is ready to archive too. The current CLI
does not yet auto-archive epics; surface this signal to the user so
they can decide whether to follow up.

#### JSON error shapes

    {"error": "ticket ID not found in WORK.md: \"foo\""}
    {"error": "cannot done an epic (epic archival not yet supported): \"epic-a\" is an epic"}
    {"error": "summary is required"}
    {"error": "done requires --summary when stdin is not a TTY"}
    {"error": "invalid date (expected YYYY-MM-DD): \"yesterday\""}

#### Refusal cases

| Case | Error | Exit |
|---|---|---|
| `<id>` not in WORK.md | `ErrIDNotFound` | 1 |
| `<id>` is an epic | `ErrCannotDoneEpic` (epic archival not yet supported) | 1 |
| `--summary` empty + no TTY | `ErrSummaryRequired` | 64 |
| `--completed` not a valid YYYY-MM-DD | `ErrInvalidDate` | 1 |

#### Hard rule reminders

- The archive is the source of truth for completed work. `[x]` should
  never linger in `WORK.md`; the binary removes the block atomically.
- When archiving a child, the notes-file checkbox flips `[ ]` → `[x]`
  AND the epic's `**Active children**:` is updated, in that order.
- `INDEX.md` is **not** updated by `done`. Run `worklog reindex`
  periodically (or after a batch of mutations) to rebuild it. The
  validator's `index-refs-exist` check stays clean as long as
  INDEX.md doesn't reference an archive/notes file that doesn't
  exist on disk.
- Epic archival (`worklog done <epic-id>`) is **not yet supported**.
  When all children of an epic are complete, the JSON output flags
  `epicCompletable: true` but the epic block stays in `## Next` until
  manually archived.

#### Failure recovery

The operation writes three files in order: archive → notes → WORK.md.
Each individual write is atomic (tempfile + rename). Across files it
is NOT transactional — a crash between archive write and WORK.md
remove leaves a duplicate (entry in archive AND block in WORK.md).
Re-running `worklog done <id>` after a partial failure converges: the
archive write is idempotent if the file is intact, and the
WORK.md remove will succeed on the second pass.

The skill's hard rule "better a duplicate than a lost record" holds.

### 5. Discovery / search ("have I worked on X before?")

#### CLI ✓

    worklog search "auth" --json

Finds every place "auth" appears in the worklog. Algorithm:

1. **INDEX-first**: scans `INDEX.md` for matching lines in the "By
   ticket", "By tag", and "By repo" sections and follows the pointers
   to extract content from the referenced files.
2. **Full-text fallback**: if INDEX returns zero hits, scans
   `WORK.md`, every `archive/*.md`, and every `notes/*.md` directly
   for the term.

Case-insensitive substring match. Single term only this slice (no
boolean / regex / fuzzy yet).

#### Flags

| Flag | Purpose |
|---|---|
| `--limit N` | Cap on hits (default 50). When the cap fires, JSON includes `"truncated": true`. |
| `--deep` | Skip the INDEX-first pass and run full-text directly. Useful when INDEX may be stale. |
| `--json` | Emit structured Output instead of styled text. |
| `--plain` | Emit raw markdown (no ANSI), even when stdout is a TTY. |

#### Output modes

- Default + TTY → Glamour-rendered styled markdown for each hit.
- Default + pipe → raw markdown (Glamour skipped; clean for piping).
- `--plain` → raw markdown, regardless of TTY.
- `--json` → structured Output with `hits[]`, `indexUsed`,
  `fellBackToFullText`, `truncated`.

#### JSON shape

    {
      "term": "auth",
      "hits": [
        {
          "id": "auth-1",
          "file": "archive/2026-05.md",
          "source": "index",
          "kind": "archived",
          "title": "Refactor auth middleware",
          "snippet": "### auth-1 — Refactor auth middleware\n- **Repo**: api\n..."
        }
      ],
      "indexUsed": true,
      "fellBackToFullText": false,
      "truncated": false
    }

`kind` is one of `live`, `epic`, `archived`, `child`, or `notesFile`.
`source` is `index` or `fulltext`.

#### Citation requirement

Always cite the `file` field when relaying a hit to the user. They
should be able to verify where the information came from.

#### When the index is stale

If INDEX.md hasn't been regenerated since the last mutation, an
INDEX-first search may miss valid hits. The full-text fallback catches
what INDEX missed. To eliminate the stale-index risk entirely, run
`worklog reindex` before search (or use `--deep` on a one-off query).

#### Error shape

Empty term returns exit 64 with `{"error": "search term required"}`.
Anything else (no hits, all sources empty) returns exit 0 with an
empty `hits[]` array — searching for nonexistent terms is normal.

### 6. Notes for epics or long-running work

**CLI not yet implemented.** Manual procedure follows.

Every epic gets a `notes/<id>.md` file and a `**Notes**: notes/<id>.md` line in
its container block. The notes file is freeform but should at minimum include
Jira link, repo, full plan reference (if any), child-ticket hierarchy as a
`[ ]` checkbox list, and open questions. The checkbox list is the canonical
backlog for the epic — promote children from there into `## Now` when starting
work; tick `[x]` only when the child archives.

A standalone ticket only needs a notes file if it accrues more than ~5 bullets
of context.

### 7. Maintaining INDEX.md

`INDEX.md` is a derived artifact. `add`, `start`, and `done` all emit
"INDEX.md not updated" warnings — they intentionally don't mutate the
index on every operation. Rebuild it with:

    worklog reindex          # writes INDEX.md
    worklog reindex --json   # also emits structured EntryCounts
    worklog reindex --dry-run # prints the would-be content; no write

Reindex scans `WORK.md`, every `archive/YYYY-MM.md`, and every
`notes/<id>.md` to produce four sections (alphabetical "By ticket",
"By tag", "By repo"; reverse-chronological "By archive month").
The regeneration is **destructive** — INDEX.md is fully replaced.

Run periodically (e.g. once per session) or after a batch of mutations.
Discovery / search workflows are smoother when INDEX.md is current.

JSON output shape:

    {
      "status": "regenerated",       // or "would-regenerate" with --dry-run
      "indexPath": "/home/.../INDEX.md",
      "entries": {
        "byTicket": 12,
        "byTag": 8,
        "byRepo": 3,
        "byArchiveMonth": 2
      }
    }

With `--dry-run`, an additional `content` field contains the would-be
INDEX.md text.

## Ticket block format (in `## Now` / `## Next` / `## Someday`)

```markdown
- [ ] **<TICKET-OR-TITLE>** — Short description
  - **ID**: <id>                          # lowercase-kebab, e.g. ent-3794
  - **Type**: ticket | spike | chore      # optional; epics use the epic format below
  - **Parent**: <epic-id>                 # only if this is a child of an epic
  - **Repo**: <repo-name>                 # optional, omit if cross-repo
  - **Tags**: tag1, tag2                  # comma-separated, lowercase
  - **PR**: <url>                         # always rendered; value may be empty
  - **Started**: YYYY-MM-DD               # only when [~]
  - **Files**: `path/one.go`, `path/two.go`  # optional
  - **Acceptance**: one-line definition of done  # optional but recommended
  - **Notes**: notes/<id>.md              # if a notes file exists
  - **Status**: free-form one-liner       # optional
```

Bare `- [ ] Fix the typo` is also valid for trivial items — full metadata is
for non-trivial work.

## Epic block format (in `## Next` / `## Someday` only — never `## Now`)

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
(visible in `**Active children**`) or they don't.

## Archive entry format (in archive/YYYY-MM.md)

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

Multiple archive entries on the same day live under the same `## YYYY-MM-DD`
heading. Most recent day at the top of the file.

## Hard rules

<!-- rules:start -->
- **Never auto-delete an archive entry, notes file, or anything in the worklog.**
  Move-then-delete is allowed (during archival from `WORK.md`); standalone
  deletion is not.
- **Never let `## Now` exceed 5 tickets.** Epics do not occupy `## Now` — only
  individual child tickets (or standalone non-epic tickets) do.
- **Never mark a top-level item `[x]` and leave it in `WORK.md`.** `[x]` is a
  transient state that exists only between "I've completed this" and "I've moved
  it to archive". Persisting `[x]` on the front page defeats the purpose of the
  cap.
- **Never duplicate the full repo plan into a notes file.** Notes files reference
  the in-repo plan and only carry the lean index needed for the worklog.
- **Update `INDEX.md` whenever an entry is added, archived, or relocated.**
  A stale index makes the discovery loop unreliable.
- **Keep the epic <-> child relationship consistent in three places** every time
  a child is promoted or archived: the child's checkbox in `notes/<epic-id>.md`,
  the parent's `**Active children**:` field in `WORK.md`, and the child's
  `**Parent**: <epic-id>` field in its `## Now` ticket block.
<!-- rules:end -->

## Fallback

If the worklog directory is missing or `WORK.md` is corrupt:
1. Tell the user immediately — do not silently recreate.
2. Offer to restore from the most recent state visible in `archive/` and
   `notes/`, or to start fresh.
3. Never overwrite an existing file without explicit confirmation.

## Slash command

For explicit invocation, see `~/.claude/commands/worklog.md`. The skill above
covers the always-on auto-handling; the slash command covers explicit "show me
status / start / done / add" requests.
