---
name: worklog
description: |
  Inspect and update Preston's personal worklog at ~/.local/share/worklog/. Subcommands
  cover the full lifecycle: status (default), start, done, add, note, search.
  Always-on behavior (read WORK.md every session, archive on completion, etc.) is
  handled by the worklog skill — this command is for explicit, targeted operations.
tool: workflow
concern: process
---

# Worklog Command

Explicit driver for the worklog system. The day-to-day always-on behavior is
already handled by the `worklog` skill at `~/.cursor/skills/worklog/SKILL.md` and
its mirror at `~/.claude/skills/worklog/SKILL.md`. Read that skill for the full
data model, file layout, and required behavior before running any subcommand
below.

## Invocation

```
/worklog                  → status (default)
/worklog status
/worklog start <id>
/worklog done <id>
/worklog add <free-form description, optionally with #tags>
/worklog note <id>        → open / create notes/<id>.md
/worklog search <term>
```

`<id>` is the lowercase-kebab ID of the task (e.g. `ent-3794`). It matches the
`**ID**:` field in the task block.

## CLI command map for agents

The `/worklog` slash command is a Claude Code-only convenience. Agents
should prefer direct `worklog` binary invocations — they're cross-tool,
testable, and have stable JSON output. The slash command is documented
below; in every case it falls through to the same underlying behavior.

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
| Search prior work | `worklog search [term]` (`--all-of`, `--any-of`, `--deep`, `--limit`, `--json`, `--plain` modes; INDEX-first with full-text fallback) | available |
| Capture user friction | (spawned by agent; see Feedback capture in SKILL.md) | available |
| List captured feedback | `worklog feedback` (`--signal`, `--since`, `--json`, `--plain`) | available |
| Add / read notes | `worklog note <id> [text]` (`--edit`, `--editor`, `--json`; positional text appends, no text reads) | available |
| Park to waiting | `worklog wait <id>` (`--json`; moves Now→Waiting, stamps **Waiting since**:; resume via `worklog start`) | available |
| Deploy skill files | `worklog sync` (`--dry-run` / `--check` modes) | available |
| Detect rule drift | `worklog lint-specs` (`--print` mode) | available |
| Rebuild `INDEX.md` | `worklog reindex` (`--dry-run` / `--json` modes) | available |

### Conventions

- `--json` mode: every read command emits a single JSON document to
  stdout (success or error). Parse it as the single source of truth.
- Errors in `--json` mode have the shape `{"error": "<message>"}`.
- `worklog add` requires `--title` and `--id` when stdin is not a TTY,
  else it falls into the human form (which agents must not use).
- `worklog tui` is for humans only; never invoke from an agent context.
- `worklog --dir <path>` overrides the data directory (default
  `~/.local/share/worklog/`). Honors `WORKLOG_DIR` env var as fallback.

## Subcommands

### status (default)

**CLI:** `worklog status` (or `worklog status --json` for machines). The
slash command form is a Claude Code-only convenience.

1. Read `~/.local/share/worklog/WORK.md`.
2. Print a compact summary:
   - `## Now`: each ticket with its `[~]` / `[ ]` state, ID, parent epic (if
     any), and one-line title. Show `N of 5` cap usage.
   - `## Next`: top items by file order. For epic blocks, show
     `**Active children**:` inline so it's clear which epics already have work
     in flight.
   - `## Someday`: count only.
3. If `## Now` has < 5 tickets and `## Next` is non-empty, suggest one
   promotion candidate (preferring an open child of an epic that already has
   active children, to keep WIP focused) but do NOT promote without
   confirmation.

### start `<id>`

**CLI (available):** `worklog start <id>` does everything below. The
slash-command form is a Claude Code-only convenience; agents should
invoke the binary directly.

`<id>` may be a standalone ticket ID, a child-ticket ID (resolved by
scanning `notes/*.md` for a matching `- [ ]` line), or an epic ID
(refused — epics never occupy `## Now`).

Flags: `--repo`, `--tags`, `--acceptance` populate / override the
ticket block. `--json` emits the structured output.

Refusal cases (each exit 1 with a descriptive error):

- `<id>` not found anywhere
- `<id>` is an epic — error lists open children from `notes/<id>.md`
- `<id>` is already `[~]` in `## Now`
- `## Now` is at cap (5) — error lists current Now IDs

See `skill/SKILL.md` §3 for the full JSON shape and worked examples.

### done `<id>`

**CLI (available):** `worklog done <id> --summary "..." [--feedback "..."]
[--time "..."] [--pr <url>] [--completed YYYY-MM-DD] [--json]` does
everything below. Slash-command form is a Claude Code-only convenience;
agents should invoke the binary directly.

Flags (see skill §4 for full detail):

- `--summary` required (or interactive Huh form on TTY)
- `--feedback` repeatable, becomes bullets in archive entry
- `--time`, `--pr`, `--completed` optional
- `--json` for machine-parseable output

The JSON success object includes `epicCompletable: true` when the
just-archived ticket was the last open child of its epic. The CLI does
**not** auto-archive the epic; the agent decides whether to follow up.

Refusals (each exit 1 except summary-missing-no-TTY which is exit 64):

- `<id>` not in WORK.md
- `<id>` is an epic — epic archival is not yet supported
- `--summary` empty and stdin is not a TTY
- `--completed` not a valid YYYY-MM-DD

### pr `<id>`

**CLI (available):** `worklog pr <id> [url] [--clear|--edit] [--json]`

Reads or updates the `**PR**:` field on a live `WORK.md` ticket block.
The field is always rendered on every ticket block (even when empty) so
the slot stays visibly available for the TUI / human to fill in. Allowed
on tickets in any section (`## Now`, `## Next`, `## Someday`). Any string
is accepted — no URL validation.

Forms:

- `worklog pr <id>` — print current value (or `(empty)`); JSON shape
  `{"id": "...", "pr": "...", "previous": "..."}`.
- `worklog pr <id> <url>` — set the value.
- `worklog pr <id> --clear` — empty the value (line stays rendered).
- `worklog pr <id> --edit` — open an interactive Huh input pre-populated
  with the current value (TTY only).

In the TUI (`worklog tui`), pressing `p` on a selected item opens the
same Huh prompt; submit writes via the same code path, Esc cancels.

Refusals (exit 64): combining a positional URL with `--clear`,
combining a positional URL with `--edit`, combining `--clear` with
`--edit`, or `--edit` without a TTY.
Exit 1: `<id>` not found in `WORK.md`.

On archive (`worklog done`), the block's stored PR is carried into the
archive entry's `**PR**:` line; `worklog done --pr <url>` overrides.

### add `<description>`

**CLI (available, three paths):** all three add flows are now supported
by `worklog add`. Slash-command form is a Claude Code-only convenience;
agents should invoke the binary directly.

- **Standalone ticket** (default; form-fallback on TTY when no flags):
  `worklog add --title X --id Y --section [Next|Someday] --json`
- **Epic** (flag-only; creates `notes/<id>.md` scaffold):
  `worklog add --type epic --id epic-a --title "..." --json`
- **Child of an epic** (flag-only; appends to parent's notes file):
  `worklog add --parent epic-a --id child-1 --title "..." --json`

See `skill/SKILL.md` §2 for the full JSON shapes and error conditions.
INDEX.md is **not** auto-updated; run `worklog reindex` periodically.

### reindex

**CLI (available, new in Phase 2B-4):** `worklog reindex [--dry-run] [--json]`

Rebuilds `INDEX.md` from a scan of `WORK.md`, `archive/*.md`, and
`notes/*.md`. Destructive — fully replaces the existing INDEX.md. Run
periodically to clear the "INDEX.md not updated" warnings emitted by
`add`/`start`/`done`. See SKILL.md §7 for details.

### note `<id>`

**CLI (available):** `worklog note <id> [text] [--edit] [--editor] [--json]`

Appends a timestamped entry to `notes/<id>.md`, or reads existing notes.
The file is lazy-created on first use. For non-epic tickets, `**Notes**:
notes/<id>.md` is auto-added to the WORK.md block.

Forms:

- `worklog note <id>` — print all notes (Glamour-rendered on TTY, plain
  markdown when piped).
- `worklog note <id> "text"` — append a one-shot note.
- `worklog note <id> --edit` — open a Huh multi-line input (TTY only).
- `worklog note <id> --editor` — open `notes/<id>.md` directly in `$EDITOR`
  (falls back to `vi`). Unlike `--edit`, edits the whole file freely —
  useful for longer notes or revising past entries. TTY only.
- `worklog note <id> --json` — structured JSON in either mode.

JSON shape on append: `{"id":"...","file":"...","appended":{"timestamp":"...","body":"..."},
"totalEntries":N,"createdFile":bool,"linkedInWorkMD":bool}`.
JSON shape on read: `{"path":"...","exists":bool,"entries":[...],"count":N}`.
JSON shape on `--editor`: `{"id":"...","file":"...","createdFile":bool,"linkedInWorkMD":bool,"editorExitCode":0}`.

In the TUI (`worklog tui`), pressing `n` on a selected item opens the
same Huh multi-line input; submit appends, Esc cancels.

Refusals (exit 64): `--edit`, `--editor`, and positional text are mutually
exclusive (any two combined → exit 64); empty body; `--edit`/`--editor`
without a TTY.
Exit 1: `<id>` not found in `WORK.md`.

### wait `<id>`

**CLI (available):** `worklog wait <id> [--json]`

Moves a ticket from `## Now` → `## Waiting`, stamping `**Waiting since**:`
with today's date. Creates the `## Waiting` section before `## Next` if
absent. The section is cap-exempt (Waiting tickets do not count against the
5-ticket Now limit).

To resume: `worklog start <id>` detects the Waiting section and calls the
resume path (cap-checked, clears `**Waiting since**:`).
To archive directly: `worklog done <id> --summary "..."` works from any section.

In the TUI, press `w` on a selected ticket to invoke the same operation.

Refusals (exit 1):
- `<id>` not found in `WORK.md`
- `<id>` is not in `## Now`
- `<id>` is already in `## Waiting`

### search `[term]`

**CLI (available):** `worklog search [term] [--all-of "a,b"] [--any-of "a,b"]
[--limit N] [--deep] [--json] [--plain]`

INDEX-first scan with full-text fallback across `WORK.md`,
`archive/*.md`, and `notes/*.md`. Glamour-rendered output for TTYs;
plain markdown for pipes or `--plain`; structured JSON for agents via
`--json`.

- `worklog search "auth"` — single-term (positional).
- `worklog search --all-of "auth,refactor"` — AND: all terms must appear.
- `worklog search --any-of "auth,security"` — OR: at least one term.

Positional, `--all-of`, and `--any-of` are mutually exclusive → exit 64
if combined. JSON output uses `"query": {"terms":[...], "mode":"..."}`;
the old `"term"` field is gone (breaking change).

See `skill/SKILL.md` §5 for the full JSON shape, flag table, citation
requirement, and stale-index advice.

### feedback

**CLI (available):** `worklog feedback [--signal S] [--since YYYY-MM-DD] [--json] [--plain]`

Lists friction-signal entries captured by the worklog agent, stored in
`FEEDBACK.md` in the worklog data directory.

Forms:

- `worklog feedback` — list all entries (Glamour-rendered on TTY, plain
  markdown when piped).
- `worklog feedback --signal missing-feature` — filter to one signal type.
- `worklog feedback --since 2026-05-15` — entries on or after that date.
- `worklog feedback --json` — structured output (see JSON shape below).
- `worklog feedback --plain` — raw markdown regardless of TTY.

JSON list shape:
```json
{
  "entries": [
    {
      "timestamp": 1716148991,
      "signal": "missing-feature",
      "trigger": "User asked to set a due-date on a ticket.",
      "excerpt": "User: can we add due dates to these tickets",
      "context": "Triaging tickets in ## Next; wanted to surface deadlines."
    }
  ],
  "count": 1
}
```

**Append subcommand (for capture subagent):**
`worklog feedback append --signal S --trigger T [--excerpt E] [--context C] [--json]`

Appends a new entry to `FEEDBACK.md`, creating the file with a header if
absent. Intended for use by the capture subagent (see `## Feedback capture`
in `SKILL.md`) — not for direct user invocation.

Required flags: `--signal` (one of `missing-feature`, `tui-error`,
`profanity`, `agent-frustration`), `--trigger` (non-empty one-line summary).
Optional: `--excerpt`, `--context`, `--json`.

JSON append shape (on `--json`): the stamped `Entry` object:
```json
{
  "timestamp": 1716148991,
  "signal": "missing-feature",
  "trigger": "User asked to set a due-date on a ticket.",
  "excerpt": "User: can we add due dates to these tickets",
  "context": "Triaging tickets in ## Next; wanted to surface deadlines."
}
```

Valid signals: `missing-feature`, `tui-error`, `profanity`, `agent-frustration`.
Bad signal or empty trigger → exit 64.

## Hard rules (inherited from the skill)

<!-- rules:start -->
- `## Now` is capped at 5 tickets. Epics never occupy `## Now` — only child
  tickets (or standalone non-epic tickets) do.
- Completion is move-then-delete, atomic.
- Never auto-delete a notes file or archive entry.
- Keep the epic <-> child relationship consistent across three places: the
  child's checkbox in `notes/<epic>.md`, the parent's `**Active children**:`
  in `WORK.md`, and the child's `**Parent**:` field in its `## Now` block.
- Always update `INDEX.md` on add / archive / relocate.
<!-- rules:end -->

## Fallback

If `~/.local/share/worklog/WORK.md` is missing or malformed, stop and report. Do not
recreate without explicit confirmation. The skill's Fallback section covers the
recovery flow.
