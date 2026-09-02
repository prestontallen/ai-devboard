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

<!-- eval-harness variant: authored for the "trimmed-EARS-lite" arm of the
     slim-eval-harness pilot. Same content as trimmed-prose (snapshot of
     worklog/skill/SKILL.md at commit 7cc76e3), except the Hard rules and
     Required behavior sections are rewritten as WHEN -> action one-liners
     with rule IDs. Everything else is untouched prose. Do not edit in
     place outside the harness. -->

# Worklog

The agent's interface to Preston's persistent worklog, the cure for
cross-session amnesia: one `WORK.md` every session reads, a searchable
monthly archive, a notes file per epic. It is **agent-maintained** — Preston
should rarely have to edit a worklog file by hand.

## Hard rules

- **HR-1.** WHEN mutating worklog data → use `worklog` subcommands only,
  never Read/Edit/Write on worklog files. WHEN no subcommand covers the
  operation (moving a ticket between sections, rewriting an archive entry,
  deleting anything) → surface the limit and stop, or ask permission before
  touching the file.
- **HR-2.** WHEN anything would be removed from the worklog → never
  auto-delete an archive entry, a notes file, or anything else. Move-then-
  delete during archival is the only allowed deletion.
- **HR-3.** WHEN `## Now` would exceed 5 tickets, or an epic would enter
  `## Now` → refuse. Only child tickets or standalone non-epic tickets
  belong in `## Now`.
- **HR-4.** WHEN a ticket completes → `[x]` in `WORK.md` is transient,
  valid only between completion and archival. Never leave it persisted.
- **HR-5.** WHEN writing a notes file → never duplicate a repo's plan into
  it. Carry only the lean index the worklog needs.
- **HR-6.** WHEN entries are added, archived, or relocated → rebuild
  `INDEX.md`.
- **HR-7.** WHEN promoting or archiving a child → keep the epic↔child link
  consistent in all three places: the child's checkbox in
  `notes/<epic-id>.md`, the parent's `**Active children**:` in `WORK.md`,
  and the child's `**Parent**:` field.

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

- **RB-1.** WHEN a session starts → orient from the injected `worklog:`
  block, or run `worklog status --json` yourself if none appeared (only
  Claude Code runs the SessionStart hook that injects it).
- **RB-2.** WHEN adding work → use the user's own ticket key lowercased, or
  a 2–4 word lowercase-kebab slug from the title. Never combine `--type
  epic` with `--parent`. WHEN the response has a `warnings` array → read it
  and surface anything that matters.
- **RB-3.** WHEN starting a ticket → only tickets reach `## Now`, never
  epics (refusal names startable children instead). WHEN a child is
  promoted → its checkbox in `notes/<epic-id>.md` stays unticked until
  archival. Don't count the `## Now` cap by hand.
- **RB-4.** WHEN completing a ticket → draft `--summary`/`--feedback` in
  the tone convention (below). WHEN the result has `epicCompletable: true`
  → surface that and let the user decide when to archive the epic (epic
  archival is terminal).
- **RB-5.** WHEN relaying a search hit → always cite the `file` field. WHEN
  the index might be stale → use `--deep` or run `reindex` first.
- **RB-6.** WHEN writing a note body → never place a literal
  `## YYYY-MM-DD HH:MM` line inside it — indent it or use `###` instead.
  Every epic needs a notes file; a standalone ticket only needs one once it
  accrues more than ~5 bullets of context.
- **RB-7.** WHEN entries are added, started, or completed → `INDEX.md` is
  not auto-updated. Run `worklog reindex` periodically.

### Tone

Draft it in Preston's installed tone convention — see the dev-context
skill's Tone hook (§8 Ship) for the mechanics and the fallback voice. This
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
