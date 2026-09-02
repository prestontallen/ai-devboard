# Devboard task file schema (v1)

Location: `<data-root>/<repo>/<task-slug>.yaml` (or `.yml` / `.json` — same
structure). The repo grouping comes from the directory name, never from file
content. Producers should write atomically (temp file + rename). Unknown
top-level fields are rendered generically in an "Other" section — additive
extensions don't break the renderer.

Archived tasks live in `<data-root>/<repo>/_archive/` — the devboard UI
moves files there (and back) via its archive/un-archive buttons; the file
content is untouched and no schema field is involved. Producers should
write only to the repo dir, never into `_archive/`.

## Ownership (one author per field)

Worklog is the system of record; a devboard task file is disposable live
telemetry. Where both systems describe the same task, **each field has
exactly one author**, and mirroring flows worklog→devboard only. The
`worklog` binary is the privileged writer of these files — but never a
required one: a bare schema-valid file with no worklog ticket is fully
supported.

| Field | Author | Notes |
|-------|--------|-------|
| `worklog`, `title` | worklog (when ticket exists) | mirrored from the ticket; agent-authored on bare tasks |
| `type` | worklog | `epic` marks this file as an epic container; `spike` marks investigation-first work (short phase track); absent for an ordinary ticket |
| `children` | worklog (roster identity), agent (per-child in-flight detail) | see "Epic files" below |
| `links` (PR) | worklog (`worklog pr`) | other links agent-authored |
| `phase` | agent (dev-context phases) | worklog `done` sets `done` |
| `tier`, `complexity`, `branch`, `session` | agent | identity/telemetry |
| `plan`, `scorecard`, `decisions`, `code`, `needs_you`, `waiting_on` | agent | in-flight detail; deliberately NOT stored in worklog |
| notes (rendered section) | worklog (`notes/<id>.md`) | rendered live from the worklog data dir, never copied into this file |

```yaml
schema: 1                 # required; schema version
title: Add retry to embedding client
branch: feat/embed-retry  # optional
session: 5cc41a6e-9f2c-4c11-b0a6-2f4e7f1d8a33
                          # optional; Claude Code session id of the agent
                          # working this task — the UI shows a button that
                          # copies `claude --resume <session>`
worklog: embed-retry      # optional; worklog ticket id (join key). Shown
                          # as a badge; when the worklog data dir is
                          # mounted, notes/<id>.md renders in a Notes
                          # section (render, never copied)
tier: 2                   # optional; dev-context tier 0-3
complexity: medium        # optional; low|medium|high — uncertainty/blast
                          # radius, throttles fan-out depth (fan-out skill)
type: spike               # optional; epic|spike (mirrored by `worklog
                          # start` from the ticket's Type). `epic` makes
                          # this an epic container — see "Epic files";
                          # `spike` marks investigation-first work, which
                          # the UI renders on the short track
                          # intake|research|present|done. Absent for an
                          # ordinary ticket.
phase: implementing       # optional; intake|clarify|research|contract|plan|
                          # implementing|verify|present|ship|done. A spike
                          # uses the subset intake|research|present|done.

plan:                     # todo list
  - text: Wrap indexer calls in retry decorator
    state: done           # pending|in_progress|done|blocked
  - text: Add backoff tests
    state: in_progress

scorecard:                # contract acceptance criteria, live status
  - text: Retries on connection error, max 3 attempts
    verify: pytest tests/test_retry.py
    status: pass          # pending|pass|fail

decisions:                # implementation decisions + amendments
  - what: Retry lives in indexer, not shared client
    why: Sync path can't tolerate blocking
    when: 2026-09-01

code:                     # code the human should be aware of
  - file: nole/indexer.py
    lines: 88-104
    lang: python
    note: The load-bearing change — exponential backoff, jittered
    snippet: |
      @retry(attempts=3, backoff=exponential(0.5))
      def embed_batch(texts): ...

waiting_on:               # external-answer queue: blocked on OTHER people/teams
  - text: Can platform raise the rate limit for the batch endpoint?
    who: platform-team    # required; who owes the answer
    asked: 2026-09-01     # YYYY-MM-DD; the UI renders age from this
    link: https://slack.example/thread   # where it was asked (optional)
    detail: |
      context the answerer needs
                          # distinct from needs_you (blocked on the task's
                          # own human). Age is independent of any worklog
                          # `Waiting since` stamp. Resolve via
                          # `worklog task waiting-on resolve` — answers are
                          # recorded as decisions and appended to worklog
                          # notes (the ONE sanctioned devboard->worklog
                          # write; mirroring is otherwise one-way).

needs_you:                # attention queue — questions & pending checkpoints
  - type: checkpoint      # question|checkpoint
    text: Commit approval pending
    detail: |
      Summary: retry decorator in indexer.py, 2 new tests. Message:
      "indexer: retry embedding calls with backoff"
  - type: question
    text: Is 30s max total wait acceptable for batch jobs?

links:
  - label: PR #42
    url: https://github.com/prestontallen/nole/pull/42
```

## Epic files

An epic ticket's file is the single dashboard surface for all of its
children's work — no child of an epic gets its own task file. `type: epic`
marks it; `children` carries one entry per child, in notes-file document
order. An epic file's own `branch`/`session`/`tier`/`complexity`/`phase`/
`plan`/`scorecard`/`decisions`/`code`/`needs_you`/`waiting_on`/`links`
fields are unused — every child can be independently active, so that
in-flight detail lives per child under `children[]` instead of shared at
the top level, using the exact same shape (`plan`, `scorecard`, etc.) a
standalone ticket file uses.

`children[].state` is `pending` | `active` | `done`. `id`/`title`/`state`
are worklog-authored (mirrored from `notes/<epic-id>.md`'s checkbox roster
and WORK.md's `**Active children**:`); everything else on a child entry is
agent-authored via `worklog task <subcommand> --id <epic-id> --child
<child-id>`.

```yaml
schema: 1
type: epic
title: Slim the skills library
worklog: skill-slim
children:
  - id: slim-session-hook
    title: SessionStart hook injects worklog status
    state: active
    branch: feat/slim-hook
    phase: implementing
    plan:
      - text: emit compact orient block as SessionStart JSON
        state: done
    scorecard:
      - text: hook never exits 2
        verify: go test ./internal/hook/...
        status: pass
  - id: slim-eval-harness
    title: Three-way skill eval harness
    state: active
    phase: plan
  - id: slim-dedupe
    title: Cut duplicated devboard-sync and tone content
    state: done
```
