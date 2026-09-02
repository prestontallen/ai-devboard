# CLI reference — JSON shapes, exit codes, error strings

Read this when you need a JSON field name, an exit code, or the exact error
a command emits. Flags and usage are not repeated here — run
`worklog <command> --help` for those.

- [Conventions](#conventions)
- [Exit codes](#exit-codes)
- [status](#status)
- [add](#add)
- [start](#start)
- [done](#done)
- [search](#search)
- [reindex](#reindex)
- [summarize](#summarize)
- [validate](#validate)

## Conventions

- `--json` mode: every command emits a single JSON document to stdout,
  success or error. Parse it as the single source of truth; ignore stderr
  unless it's a system-level error.
- Errors in `--json` mode are `{"error": "<message>"}`. Exit codes are
  identical between text and JSON modes.
- `worklog add` requires `--title` and `--id` when stdin is not a TTY,
  else it falls into the human form (which agents must not use).
- `worklog --dir <path>` overrides the data directory. Honors `WORKLOG_DIR`
  as fallback; when both are set, `--dir` wins.
- Cobra accepts `--dir` before or after the subcommand. Canonical form for
  readability: `worklog --dir <path> <subcommand> [--flags]`.
- `cap: 0` in `status --json` means **uncapped**, not zero-limit. Only
  `## Now` has a cap (5).

## Exit codes

| Code | Meaning |
|---|---|
| `0` | success, no violations |
| `1` | domain error (duplicate ID, invalid section, WORK.md missing) |
| `2` | `validate` found violations |
| `64` | usage error (missing required flags, conflicting modes) |

## status

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

`state` is `"pending"` | `"active"` | `"done"`. Epic blocks have
`type == "epic"` and a non-empty `activeChildren` once children are in Now.

**Bare items.** A trivial entry like `- [ ] Reorganize CI pipeline` with no
`**ID**:` metadata emits `"id": ""`. Count by block presence, not by ID
truthiness — bare items still count.

Missing data dir: `{"error": "WORK.md not found ..."}`.

## add

`warnings` is load-bearing on every mutation — read it and surface anything
that matters. The INDEX.md warning is expected; clear it with `worklog
reindex`.

Standalone:

    {
      "status": "added",
      "id": "auth-1",
      "title": "Refactor auth middleware",
      "section": "Next",
      "workMD": "/home/.../WORK.md",
      "warnings": ["INDEX.md not updated; run `worklog reindex` when convenient"]
    }

Epic — adds `"kind": "epic"` and `"notesPath"`. Creates `notes/<id>.md`
with a title heading, a `## Background` placeholder, and an empty
`## Children` section.

Child — adds `"kind": "child"`, `"parent"`, and `"notesPath"` (the
*parent's* notes file). Appends `- [ ] <child-id>: <title>` at: end of the
parent's `## Children` section; else after the last checkbox line; else at
end of file. **No WORK.md mutation occurs** — children live in the notes
file until `worklog start` promotes them.

Errors:

- `{"error": "ID \"auth-1\" already exists in WORK.md"}` — exit 1
- `{"error": "add requires --title and --id when stdin is not a TTY"}` — exit 64
- `{"error": "section \"Foo\" is invalid (use Next or Someday)"}` — exit 1
- `--type epic` with `--parent` → exit 1, `ErrEpicHasNoParent`
- pre-existing `notes/<id>.md` → exit 1, `ErrNotesAlreadyExists`
- `--parent` doesn't resolve to an epic → exit 1, `ErrParentEpicNotFound`
- ID collision with an open child in any notes file → exit 1, `ErrIDCollisionInNotes`

Epic and child creation are flag-only — no form fallback.

## start

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

`parent` is the epic's ID for a child promotion, empty otherwise.

| Refusal | Error |
|---|---|
| `<id>` not in WORK.md or any notes file | exit 1, `ErrIDNotFound` |
| `<id>` already `[~]` in `## Now` | exit 1, `ErrAlreadyStarted` |
| `## Now` would exceed 5 | exit 1, `ErrCapExceeded` (lists current Now IDs) |
| `<id>` is an epic | exit 1, `ErrEpicCannotStart` (lists startable children) |

    {"error": "ticket ID not found: \"foo\""}
    {"error": "## Now is at cap (5/5); current Now: a-1, b-2, c-3, d-4, e-5"}
    {"error": "ticket is already in ## Now: \"x-1\" is already in ## Now"}
    {"error": "epics do not occupy ## Now; epic \"epic-a\" has startable children: child-2"}
    {"error": "epics do not occupy ## Now; epic \"epic-a\" has no startable children (all in progress)"}

"Startable" filters out children already in `## Now`.

Child promotion looks the child up in any `notes/*.md` with a `- [ ]` line
whose first token is `<child-id>`, creates the Now block with `**Parent**:`,
and appends the ID to the epic's `**Active children**:`. Use `--repo`,
`--tags`, `--acceptance` to populate the block — the notes line carries only
ID and title.

## done

Writes three files in order: archive → notes → WORK.md.

1. Appends to `archive/YYYY-MM.md`, creating it with a `# Archive — YYYY-MM`
   header on the month's first archive.
2. For a child: flips its checkbox in `notes/<epic-id>.md` to `[x]` and
   removes it from the epic's `**Active children**:`.
3. Removes the block from `WORK.md`.

Days within a month-file are reverse-chronological, newest first; entries
within a day likewise.

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

| Refusal | Error | Exit |
|---|---|---|
| `<id>` not in WORK.md | `ErrIDNotFound` | 1 |
| `<id>` is an epic with open children | `ErrEpicHasOpenChildren` (names each open child and its source) | 1 |
| `<id>` is an epic with no notes file | `ErrEpicNotesMissing` (absence is "cannot determine", never "complete") | 1 |
| `--summary` empty + no TTY | `ErrSummaryRequired` | 64 |
| `--completed` not YYYY-MM-DD | `ErrInvalidDate` | 1 |

    {"error": "ticket ID not found in WORK.md: \"foo\""}
    {"error": "epic has open children: child-2 (notes, [ ]), orphan-1 (WORK.md ## Next)"}
    {"error": "epic notes file missing; cannot determine child completeness: notes/epic-a.md"}
    {"error": "summary is required"}
    {"error": "done requires --summary when stdin is not a TTY"}
    {"error": "invalid date (expected YYYY-MM-DD): \"yesterday\""}

**Epic archival.** `worklog done <epic-id> --summary "..."` works once no
`[ ]`/`[~]` lines remain in `notes/<id>.md` and no WORK.md block names it as
Parent. The entry is epic-shaped (`Type: epic`, Completed-only date, Notes
ref, Plan, child roster); `notes/<id>.md` stays on disk as history. Archival
is **terminal** — `add --parent`, `import`, and `start` all refuse against an
archived epic with an "archived on `<date>`" error. Archived epics show in
standup as an "epic closed" line, not a completed ticket.

**Failure recovery.** Each individual write is atomic (tempfile + rename),
but the three-file sequence is not transactional. A crash between the archive
write and the WORK.md remove leaves a duplicate. Re-running `worklog done
<id>` converges: the archive write is idempotent if the file is intact, and
the WORK.md remove succeeds on the second pass. Better a duplicate than a
lost record.

## search

Positional (single term), `--all-of` (AND), and `--any-of` (OR) are mutually
exclusive; combining any two exits 64. Terms are comma-split,
whitespace-trimmed, lowercased; an empty list after trim exits 64. Matching
is case-insensitive substring.

Algorithm: scan `INDEX.md`'s "By ticket" / "By tag" / "By repo" sections and
follow the pointers; if that returns zero hits, fall back to full-text over
`WORK.md`, every `archive/*.md`, and every `notes/*.md`. `--deep` skips
straight to full-text.

Output modes: TTY → Glamour-styled markdown; pipe → raw markdown; `--plain`
→ raw markdown regardless; `--json` → structured.

    {
      "query": {"terms": ["auth"], "mode": "single"},
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

`query.mode` is `single` | `all-of` | `any-of`. `kind` is `live` | `epic` |
`archived` | `child` | `notesFile`. `source` is `index` | `fulltext`.
`truncated` is true when `--limit` (default 50) fired.

Empty query → exit 64, `{"error": "search term required"}`. No hits is exit
0 with an empty `hits[]` — searching for nonexistent terms is normal.

Multi-term `--all-of` queries usually fall back to full-text (INDEX lines
rarely contain all terms at once), so stale-index risk is lower for those.

## reindex

Scans `WORK.md`, every `archive/YYYY-MM.md`, and every `notes/<id>.md` to
produce four sections: alphabetical "By ticket", "By tag", "By repo", and
reverse-chronological "By archive month". Regeneration is **destructive** —
INDEX.md is fully replaced.

    {
      "status": "regenerated",
      "indexPath": "/home/.../INDEX.md",
      "entries": {"byTicket": 12, "byTag": 8, "byRepo": 3, "byArchiveMonth": 2}
    }

`--dry-run` emits `"would-regenerate"` plus a `content` field with the
would-be text, and writes nothing.

## summarize

Groups in-progress tickets by epic; standalone tickets land in a final
"Standalone" group. `## Someday` and archived tickets are excluded. Each row
carries Status (On Track `[~]` / Not Started `[ ]` / DONE `[x]` / Waiting),
Started, Updated (most recent note timestamp, else Started), and Note
(`**Status**:` if set, else the first non-empty line of the latest note,
truncated to 80 chars).

    { "groups": [{ "kind", "id", "title", "rows": [...],
      "aggregate": { "total", "done", "active", "notStarted", "waiting",
      "percentDone", "status" } }] }

## validate

Run after any mutation as a sanity check.

| Check ID | Catches |
|---|---|
| `work-md-exists` | WORK.md is missing (hard fail, exit 1) |
| `now-cap` | More than 5 tickets in `## Now` |
| `no-top-level-x` | A `[x]` block lingering in WORK.md instead of archive |
| `started-on-active` | A `[~]` ticket in Now without a `**Started**:` date |
| `notes-file-exists` | A block references `notes/<id>.md` but the file is missing |
| `index-refs-exist` | INDEX.md references an archive or notes file that's missing |
| `three-place-consistency` | A child in Now whose epic / notes / parent linkage disagrees |

    {
      "dir": "/home/.../worklog",
      "workMDMissing": false,
      "violations": [
        {"check": "now-cap", "message": "## Now has 6 tickets, cap is 5"}
      ],
      "infos": ["INDEX.md not present at ...; skipping index-refs-exist"],
      "violationCount": 1
    }

`infos` are non-fatal notices. Surface one when the missing thing is
something the user might want to create ("no INDEX.md yet — want me to
bootstrap one?"); suppress it when it's steady-state and not actionable.
When in doubt, surface on the first validation against a given dir and
suppress on later ones in the same session.
