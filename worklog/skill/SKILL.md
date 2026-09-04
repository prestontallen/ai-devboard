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

The agent's interface to Preston's persistent worklog, the cure for
cross-session amnesia: one `WORK.md` every session reads, a searchable
monthly archive, a notes file per epic. It is **agent-maintained** — Preston
should rarely have to edit a worklog file by hand.

## Hard rules

1. **Mutate worklog data only through `worklog` subcommands** — never via
   Read/Edit/Write on worklog files. A store-backed write refuses and names
   the file if it finds a hand-edited projection; `note --editor` is the
   one sanctioned way to hand-write prose. Correcting a live ticket
   is `worklog edit <id>`; a task file's plan or scorecard is
   `worklog task plan|scorecard edit|remove <n>`. For an operation with no
   command, surface the limit and stop. What the CLI won't do is a
   deliberate limit, not a gap to work around. A never-adopted machine is
   told so: run `worklog adopt`.
2. **Never auto-delete** an archive entry, a notes file, or anything else in
   the worklog. Move-then-delete during archival is allowed; standalone
   deletion is not.
3. **Never let `## Now` exceed 5 tickets**, and never put an epic there —
   only child tickets or standalone non-epic tickets.
4. **Never leave a `[x]` item in `WORK.md`.** `[x]` exists only between "this
   is complete" and "this is archived"; persisting it defeats the cap.
5. **Never duplicate a repo's plan into a notes file.** Notes reference the
   in-repo plan and carry only the lean index the worklog needs.
6. **`INDEX.md` stays current on its own** — every store-backed write
   rebuilds it in the same transaction. Nothing to run by hand.
7. **The epic↔child link is one stored relation** (the child's `ParentID`).
   WORK.md's `**Active children**:` is a computed view of it. An older
   epic's `notes/<id>.md` may still carry a frozen `## Children` checklist
   from before the cutover — it no longer updates and isn't the source of
   truth.

## Layout

Under `~/.local/share/worklog/`:

- `WORK.md` — front page: `## Now` (tickets only, ≤5) / `## Waiting` /
  `## Next` / `## Someday`
- `archive/YYYY-MM.md` — completed items, metadata + Summary + Feedback
- `notes/<id>.md` — long-form notes; for an epic, the canonical child list
- `INDEX.md` — search spine: by ticket, tag, repo, archive month

Plain markdown, no frontmatter. `<id>` is lowercase-kebab: the ticket key
(`ent-3794`) or a stable slug.

## Reference files

Load one only when you need it.

- [references/cli.md](references/cli.md) — JSON output shapes, exit codes,
  and the exact error strings each command emits. Read when parsing `--json`
  or interpreting a refusal.
- [references/formats.md](references/formats.md) — ticket, epic, archive, and
  notes file formats. Read when *reading* the raw markdown.
- [references/import.md](references/import.md) — the import JSON shape and
  the Jira / Linear / GitHub / Asana field mapping. Read when turning a
  tracker ticket into a worklog block.
- [references/feedback-capture.md](references/feedback-capture.md) — the
  verbatim subagent prompt. Read when a friction signal has fired.

## Using the CLI

The `worklog` binary is the canonical mutation surface; every intent has a
command. `worklog --help` lists them and `worklog <cmd> --help` documents
every flag — this skill duplicates neither, and covers only what help can't
tell you.

- `## Now` is reached only through `start` — `add` writes to `Next` or
  `Someday`. A blocked ticket goes to `## Waiting` via `wait`, which is
  cap-exempt, and comes back with `start`.
- `--json` is the agent contract: one JSON document on stdout, success or
  error. Ignore stderr unless it's a system-level failure.
- `worklog tui` is for humans only. Never invoke it from an agent context.
- `cap: 0` in `status --json` means **uncapped**, not zero-limit. Only
  `## Now` is capped.
- `worklog --dir <path>` overrides the data directory (`WORKLOG_DIR` is the
  env fallback).

## Required behavior

**1. On every session start.** Orient from the injected `worklog:` block — or
run `worklog status --json` yourself if none appeared, since only Claude Code
runs the SessionStart hook that injects it.

**2. Adding work.** Prefer the user's own ticket key as the ID, lowercased
(`ENT-3794` → `ent-3794`); failing that, a 2–4 word lowercase-kebab slug from
the title (*"rewrite the auth middleware"* → `auth-middleware-rewrite`). Keep
it short and stable — the ID is referenced from notes, archives, and the
index. `--type` takes `ticket` (default), `epic`, `spike`, or `chore` and
is validated. `--type epic` and `--parent` never combine; a spike is always
standalone. `--type spike` is what puts dev-context on the collapsed
research track, and Type is fixed at creation — `edit` refuses it as
structural. The `warnings` array is load-bearing: read it and surface
anything that matters.

**3. Starting.** `## Now` holds tickets only; starting an epic is always a
refusal that names its startable children. Promoting a child sets its
`ParentID`; WORK.md's Active-children view updates automatically — nothing
to flip by hand. The cap is enforced by the CLI; don't count by hand.

**4. Completing.** `--summary` and `--feedback` land permanently in the
archive and are human-facing — draft them in the tone convention below.
`epicCompletable: true` means the ticket was its epic's last open child:
surface that and let the user decide when to archive the epic. Epic archival
is terminal — an archived epic refuses `add --parent`, `import`, and `start`
from then on.

**5. Searching.** Always cite the `file` field when relaying a hit, so the
user can verify where it came from. `--deep` bypasses the index if you
suspect data written outside the CLI (a restore, an import script).

**6. Notes.** Any ticket can carry a timestamped journal in `notes/<id>.md`,
lazy-created on first use. Use `--editor` to revise past entries rather than
appending a new section. **Never put a literal `## YYYY-MM-DD HH:MM` line
inside a note body** — it will be misparsed as a new entry; indent it or use
`###`. Every epic needs a notes file; a standalone ticket only needs one once
it accrues more than ~5 bullets of context.

**7. INDEX.md.** Kept current automatically — see Hard rule 6. `worklog
reindex` still exists for a manual rebuild (e.g. after restoring from
backup) but isn't required in normal use.

### Tone

Draft it in Preston's installed tone convention — see the dev-context
skill's Tone hook (§9 Ship) for the mechanics and the fallback voice. This
applies even for a plain "log this" outside a dev-context task.

## Devboard side effects

The binary is the privileged writer of devboard task files (the YAML the
dashboard renders — `devboard/schema.md` in ai-devboard), including the
automatic mirroring on `start`/`done`/`pr`. The full sync mechanics — which
`worklog task` calls to make and when — are documented in the dev-context
skill's "Devboard sync" section, since that's the skill that actually
drives them during a task's phases.

## Feedback capture

Watch for four friction signals during worklog interactions:

- `missing-feature` — the user wants something the skill or TUI can't do.
- `tui-error` — a TUI interaction errors (any error, not just repeats).
- `profanity` — the user swears.
- `agent-frustration` — frustration aimed at the agent (insults, "you keep
  getting this wrong"), with or without profanity.

The vocabulary is closed — use exactly one. When a signal fires, spawn a
Sonnet subagent (`subagent_type="general-purpose"`, `model="sonnet"`) with
the verbatim prompt in
[references/feedback-capture.md](references/feedback-capture.md). Don't
pause, don't acknowledge it in chat, keep working. Resolving an entry is the
human's call, never the capture subagent's.

## Fallback

If the worklog directory is missing or `WORK.md` is corrupt:

1. Tell the user immediately — do not silently recreate.
2. Offer to restore from `archive/` and `notes/`, or to start fresh.
3. Never overwrite an existing file without explicit confirmation.

Explicit "show me status / start / done" requests are covered by
`~/.claude/commands/worklog.md`; this skill is the always-on handling.
