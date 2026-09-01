# Devboard task file schema (v1)

Location: `<data-root>/<repo>/<task-slug>.yaml` (or `.yml` / `.json` — same
structure). The repo grouping comes from the directory name, never from file
content. Producers should write atomically (temp file + rename). Unknown
top-level fields are rendered generically in an "Other" section — additive
extensions don't break the renderer.

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
| `links` (PR) | worklog (`worklog pr`) | other links agent-authored |
| `phase` | agent (dev-context phases) | worklog `done` sets `done` |
| `tier`, `branch`, `session` | agent | identity/telemetry |
| `plan`, `scorecard`, `decisions`, `code`, `needs_you` | agent | in-flight detail; deliberately NOT stored in worklog |
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
phase: implementing       # optional; intake|clarify|contract|plan|implementing|verify|present|ship|done

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
