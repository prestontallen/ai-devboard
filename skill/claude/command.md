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
| Search prior work | `worklog search <term>` (`--deep`, `--limit`, `--json`, `--plain` modes; INDEX-first with full-text fallback) | available |
| Open / append notes | manual: edit `notes/<id>.md` | manual-only |
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

**CLI not yet implemented.** Slash command and skill fall through to the
manual procedure below.

1. If `notes/<id>.md` exists, open it and offer to append a dated section.
2. If it doesn't exist, create it with the standard scaffold (Jira link, repo,
   plan reference, child-ticket hierarchy, open questions) and link it from the
   matching task block in `WORK.md` via `**Notes**: notes/<id>.md`.

### search `<term>`

**CLI (available, new in Phase 2B-5):** `worklog search <term> [--limit N]
[--deep] [--json] [--plain]`

INDEX-first scan with full-text fallback across `WORK.md`,
`archive/*.md`, and `notes/*.md`. Glamour-rendered output for TTYs;
plain markdown for pipes or `--plain`; structured JSON for agents via
`--json`.

See `skill/SKILL.md` §5 for the full JSON shape, flag table, citation
requirement, and stale-index advice.

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
