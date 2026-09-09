# Devboard

A browser dashboard over a directory of task files, so the human
can follow agent work without living in terminal scrollback. Agents (or
anyone) drop one YAML/JSON file per task into `<data-root>/<repo>/`; the
page renders plan todos, the contract scorecard, decisions, code-to-know,
a "needs you" attention queue, tier/complexity/worklog badges, and a
copy-`claude --resume` button per task (from the `session` field) — and
hot-reloads within ~2s of any file change, no refresh needed (SSE,
auto-reconnecting).

Tasks with a `worklog:` join key also render the ticket's
`notes/<id>.md` live from the worklog data dir (second read-only mount) —
rendered, never copied, and note edits hot-reload too. That mount also
carries `FEEDBACK.md`, rendered as the global Friction panel (see below).

![The devboard Board lens: a row of chips for each lens with counts, an attention line reading "3 items need you", and a grid of task cards including two epics showing their child rosters](docs/board.png)

The intended writer is the `worklog` CLI (`start`/`done`/`pr` side
effects plus the `worklog task` family, including `untrack` to stop
tracking a task by deleting only its YAML). Every file here is derived
from the store. A file with no `worklog:` key used to be a supported
producer path; it is not any more (adb-retire-devboard-dir). Adoption
absorbs one whose filename names a ticket, and refuses over one it
cannot place rather than deleting it.

## Run

The server lives in the `worklog` binary — frontend embedded, no runtime
dependencies. The vendored Preact and HTM modules the newer front-end loads
are committed and embedded alongside the pages, so npm is never needed to
build, release, or run this:

```sh
worklog serve
# open http://localhost:8484
```

For supervision (start on boot, restart on crash), `worklog install`
offers a systemd user unit running exactly that; accept it, or manage it
directly:

```sh
systemctl --user status devboard   # the unit installed by `worklog install`
```

Defaults: data dir `~/.local/share/devboard`, port `8484`, worklog dir
`~/.local/share/worklog` (for notes rendering). Override with the
environment: `DEVBOARD_DATA`, `DEVBOARD_WORKLOG`, `DEVBOARD_PORT`,
`DEVBOARD_SCAN_INTERVAL`. The server reads the worklog dir and never
writes under it; the data dir is written for exactly one operation:
archiving (see below). The response shape is frozen as the frontend
contract — see [API.md](API.md).

Fallback: a Docker deployment (multi-stage build of the same binary) for
setups that prefer container supervision:

```sh
docker compose -f devboard/compose.yaml up --build -d   # from the repo root
```

## Directory layout

```
<data-root>/
  <repo-name>/          # grouping = directory name
    <task-slug>.yaml    # one file per task (.yml/.json also fine)
    _archive/           # archived tasks (moved here by the UI; kept on disk)
      <task-slug>.yaml
```

See [schema.md](schema.md) for the task file format (`schema: 1`), and
[examples/](examples/) for ready-made files — try it out with:

```sh
cp -r examples/* ~/.local/share/devboard/
```

## Archive / un-archive

The board's only write action. The archive button (on done cards and in
every task's detail view) moves the task's file into `<repo>/_archive/`;
archived tasks leave the Board lens and appear under the Archived chip,
with un-archive buttons that move them back. Nothing is ever deleted, and
the worklog dir is never touched.

For a task the worklog store knows, the **store** performs the move: the
endpoint records it and the re-render writes the file at its new path and
clears the old one. A file the store board-tracks is the only kind that
exists now, so the store always performs the move. The move is
all-or-nothing —
a failure answers 500 with the file where it started, rather than leaving
the file and the store disagreeing.

There is no auth: anyone who can reach the port can archive/un-archive
(reversible by design; the server rejects non-JSON content types and never
answers CORS preflights, so a browsing session on another site can't
trigger it cross-origin).

## Friction panel

`FEEDBACK.md` at the root of the worklog mount is the friction log the
worklog skill's capture subagent appends to (`worklog feedback append`).
It gets its own lens rather than a per-task field, since it is a global log
and no task file is involved:

- the Friction chip counts unresolved entries, and dims at zero
- a count per signal across the top, most serious signal first
- the unresolved entries newest-first: signal, relative time, trigger, then
  the excerpt and context inline
- resolved entries below under a `resolved · N` heading, dimmed

Reviewing happens in the CLI, never here: the worklog mount is read-only, so
each entry offers a button that copies `worklog feedback resolve <timestamp>`
rather than writing anything. Running it adds a `**Resolved**: <unix-ts>`
line to that entry, and the board updates over SSE within ~2s.

The entry format is owned by `worklog/internal/feedback/feedback.go`, and
the server reads it through that same package — one parser, so board and
CLI cannot drift. The reader skips unknown `**Field**:` lines rather than
failing. Its parity pins (migrated from the retired Python suite) run
with the normal Go tests:

```sh
cd worklog && go test ./internal/feedback/ ./internal/serve/
```

Front-end behavior has its own harness — vitest and happy-dom rendering the
same untranspiled ESM the browser loads. It is dev-only: nothing about
building, releasing, or running the binary needs npm.

```sh
npm ci && npm test   # from the repo root
```

## The board

The status bar is the router: each chip is a lens, and the number on a chip
is exactly what that lens draws.

| Chip | Shows | Counts |
|---|---|---|
| Board | in-flight work, default | tasks |
| Needs you | one panel per queue item | items |
| Waiting | tasks blocked on someone else | tasks |
| Friction | unresolved `FEEDBACK.md` entries | entries |
| Backlog | `WORK.md`'s Next and Someday | tickets |
| Done | finished, not archived | tasks |
| Archived | moved to `_archive/` | tasks |

Zero-count chips dim rather than vanish, so the bar never changes shape.
Stale is a per-card badge rather than a chip. There are no folds on the
board: an attention line sits above one card grid, ordered newest-first,
because attention has its own route and does not need hoisting into the
grid. Folds survive only inside detail views, where the material is
reference rather than status.

A task's detail lives at `#/task/<repo>/<id>`: a hero, then the **contract
ledger** — plan as a connected rail because its steps are ordered, the
scorecard as independent squares because its criteria are not, under one
header carrying the phase and both ratios — then the record beneath it
(queues, risk scout, decisions, code, links, unknown keys, worklog notes).
Outstanding and failed checks sort above passing ones, every criterion shows
its verify line, and an empty scorecard above tier 1 reads as a warning
rather than neutral emptiness.

An epic opens at the same shape of URL and shows an aggregate line over a
grid of child cards, since an epic has no phase or contract of its own. Each
child opens at `#/task/<repo>/<epic-id>/<child-id>` with its own ledger and
record. A backlog row is deliberately not a card and not a link: it has no
phase, plan or scorecard, and no task file to open.

## Behavior notes

- Malformed files render as an error card (filename + parse error); they
  never take down the rest of the page, and a malformed entry stays on the
  board rather than being filtered out of it.
- Unknown top-level fields render in an "Other" section — extend freely.
- Tasks untouched for >2h render dimmed (likely-stale signal).
- A missing or malformed `FEEDBACK.md` simply means no friction panel —
  it never takes down the page.
- Server: Go, in the `worklog` binary (`worklog/internal/serve`, stdlib
  net/http + yaml.v3); a 1s mtime scan drives the SSE stream (no inotify —
  works identically under container volume mounts). Bare `yes/no/on/off`
  in hand-written YAML are strings (YAML 1.2), not booleans — write
  `true`/`false`; see API.md for the full frozen contract.
