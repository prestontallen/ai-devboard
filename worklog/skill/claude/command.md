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
| Daily standup | `worklog standup` (`--since`, `--until`, `--days`, `--simple`, `--json`) | available |
| Import tickets | `worklog import` (`--file`, `--section`, `--dry-run`, `--json`) | available |
| Validate invariants | `worklog validate` (`--json` for machines) | available |
| Add standalone ticket | `worklog add --title X --id Y --section [Next\|Someday] --json` | available |
| Add an epic | `worklog add --type epic --id <id> --title "..."` (creates `notes/<id>.md` scaffold) | available |
| Add a child of an epic | `worklog add --parent <epic-id> --id <child-id> --title "..."` | available |
| Start a ticket | `worklog start <id>` (standalone or child-of-epic) | available |
| Complete a ticket | `worklog done <id> --summary "..."` (sets `epicCompletable: true` when last child) | available |
| Set PR on ticket | `worklog pr <id> <url>` (read with `worklog pr <id>`; `--clear` empties; `--edit` opens Huh prompt; `--json` for machines) | available |
| Search prior work | `worklog search [term]` (`--all-of`, `--any-of`, `--deep`, `--limit`, `--json`, `--plain` modes; INDEX-first with full-text fallback) | available |
| Status-report view | `worklog summarize` (`--json`, `--plain`, `--limit N`; grouped by epic, in-progress scope) | available |
| Capture user friction | (spawned by agent; see Feedback capture in SKILL.md) | available |
| List captured feedback | `worklog feedback` (`--signal`, `--since`, `--json`, `--plain`) | available |
| Add / read notes | `worklog note <id> [text]` (`--edit`, `--editor`, `--json`; positional text appends, no text reads) | available |
| Park to waiting | `worklog wait <id>` (`--json`; moves Now→Waiting, stamps **Waiting since**:; resume via `worklog start`) | available |
| Deploy skill files | `worklog install` (`--dry-run` / `--check` modes; deploys every skill, not just this one) | available |
| Rebuild `INDEX.md` | `worklog reindex` (`--dry-run` / `--json` modes) | available |

Full per-command detail (JSON shapes, flags, refusal cases, exit codes) is
not repeated here — run `worklog <cmd> --help`, or see
`worklog/skill/references/cli.md` in the repo.

## Hard rules (inherited from the skill)

- `## Now` is capped at 5 tickets. Epics never occupy `## Now` — only child
  tickets (or standalone non-epic tickets) do.
- Completion is move-then-delete, atomic.
- Never auto-delete a notes file or archive entry.
- Keep the epic <-> child relationship consistent across three places: the
  child's checkbox in `notes/<epic>.md`, the parent's `**Active children**:`
  in `WORK.md`, and the child's `**Parent**:` field in its `## Now` block.
- Always update `INDEX.md` on add / archive / relocate.

## Fallback

If `~/.local/share/worklog/WORK.md` is missing or malformed, stop and report. Do not
recreate without explicit confirmation. The skill's Fallback section covers the
recovery flow.
