# Devboard task file schema (v1)

This is the shape of a task on the wire, not of a file on disk. There is
no task file any more: the board is built from worklog tickets, and this
document describes what one looks like once it reaches the frontend
(`devboard/API.md` freezes the envelope around it). Unknown top-level
fields are rendered generically in an "Other" section — additive
extensions don't break the renderer.

Repo grouping is the ticket's own repo attribution, and a ticket with none
groups under `unknown`. Archiving is a flag on the ticket rather than a
move: an archived task leaves the Board lens and appears under the
Archived chip. Nothing is deleted either way (adb-retire-devboard-dir-2).

## Ownership (one author per field)

Worklog is the system of record; the board is a disposable live view of
it. Where both systems describe the same task, **each field has
exactly one author**, and mirroring flows worklog→devboard only. The
`worklog` binary is the privileged writer of these files — but never a
required one: a bare schema-valid file with no worklog ticket is fully
supported.

| Field | Author | Notes |
|-------|--------|-------|
| `worklog`, `title` | worklog (when ticket exists) | mirrored from the ticket; agent-authored on bare tasks |
| `type` | worklog | `epic` marks this file as an epic container; `spike` marks investigation-first work (short phase track); absent for an ordinary ticket |
| `children` | worklog (roster identity), agent (per-child in-flight detail) | see "Epic files" below |
| `links` (PR) | worklog (`worklog pr`) | other entries worklog too (`worklog link <id> <name> [url]`, e.g. Jira/Slack/docs — any name, not just PR) |
| `phase` | agent (dev-context phases) | worklog `done` sets `done` |
| `tier`, `complexity`, `branch`, `session`, `repo_path`, `scout` | agent | identity/telemetry |
| `plan`, `scorecard`, `decisions`, `code` | agent | in-flight detail; deliberately NOT stored in worklog |
| `needs_you`, `waiting_on` | nobody, since 2026-09 | the commands that wrote them were removed after neither ever carried an entry on a real board. The keys stay readable so pre-existing data still parses; nothing new writes them |
| notes (rendered section) | worklog (`notes/<id>.md`) | rendered live from the ticket, never copied into this shape |

```yaml
schema: 1                 # required; schema version
title: Add retry to embedding client
branch: feat/embed-retry  # optional
repo_path: /home/you/nole # optional; the repo's working-tree root, recorded
                          # by `worklog start`. Absent when it could not be
                          # established with confidence — notably when the
                          # ticket's **Repo**: disagrees with the repo the
                          # command ran in, since a wrong path is worse than
                          # none. Consumers must check it still exists.
session: 5cc41a6e-9f2c-4c11-b0a6-2f4e7f1d8a33
                          # optional; Claude Code session id of the agent
                          # working this task — the UI shows a button that
                          # copies `claude --resume <session>`
worklog: embed-retry      # optional; worklog ticket id (join key). Shown
                          # as a badge; when the worklog notes are
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
  - what: Retargeted to a generic link mechanism
    why: The storage layer had already generalised
    when: 2026-09-02
    complexity: low -> high
                          # optional; written only by `worklog task amend`,
                          # which requires a re-rate. "low (unchanged)" when
                          # the rating was reconsidered and kept. A plain
                          # `worklog task decision` entry omits it.

scout:                    # optional; risk-scout attestation, written by
  mode: ran               # `worklog task scout ran|inline|skipped --why`.
  why: 4 lenses over the draft scope
  when: 2026-09-02        # Absence on medium/high work is the state the
                          # gate reports on phase/complexity/amend.

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
                          # `Waiting since` stamp. RETIRED 2026-09: the
                          # commands that wrote this queue were removed
                          # after it never carried an entry. Existing data
                          # still parses and `worklog done` still converts
                          # any leftover open question into a dated
                          # decision. Note that this was also the one
                          # sanctioned devboard->worklog write; with it
                          # gone, mirroring is one-way without exception.

needs_you:                # attention queue — questions & pending checkpoints
  - type: checkpoint      # question|checkpoint
    text: Commit approval pending
    detail: |
      Summary: retry decorator in indexer.py, 2 new tests. Message:
      "indexer: retry embedding calls with backoff"
  - type: question
    text: Is 30s max total wait acceptable for batch jobs?

links:                    # `worklog pr` writes the PR entry; `worklog link`
                          # writes any other named entry (Jira, Slack, a
                          # design doc — the name is free-form)
  - label: PR #42
    url: https://github.com/prestontallen/nole/pull/42
  - label: Jira
    url: https://company.atlassian.net/browse/AUTH-1234
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

### Per-child board routing

A child's board-grid presence stays inside its epic's single nested card —
a child never gets its own top-level card. Both the roster chips and the
child cards on the epic's own detail page link to a dedicated detail hash,
distinct from an epic's own and from a standalone ticket's, so a child's
plan/scorecard/decisions/code can be read on its own without the rest of
the epic's children in view. This is a UI routing convention only; no
schema field carries it, and no per-child archive endpoint exists —
archiving stays a whole-file action from the epic's own view.

The routes, all served from `/`:

| | route |
|---|---|
| standalone | `#/task/<repo>/<id>` |
| epic | `#/task/<repo>/<epic-id>` |
| child | `#/task/<repo>/<epic-id>/<child-id>` |

A second grammar without the leading slash — `#<repo>/<id>` and
`#<repo>/<epic-id>/<child-id>` — was the outgoing board's, and links in that
shape exist in archives and bookmarks. They are translated to the above on
arrival and the URL is rewritten in place. The leading slash is what makes
that safe: the two shapes cannot collide, so an old hash is unambiguous
rather than a guess.

A child is addressed *through* its epic, because the epic file is what the
route has to resolve. That is also why the payload attaches a child's
`notes/<child-id>.md` to its `children[]` entry (see `API.md`): the child
has no file of its own to carry a `worklog` key.
