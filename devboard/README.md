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

![Devboard grid showing two task cards, one in-flight with a needs-you badge and one in planning](docs/board.png)

The intended writer is the `worklog` CLI (`start`/`done`/`pr` side
effects plus the `worklog task` family, including `untrack` to stop
tracking a task by deleting only its YAML) — but a hand-dropped
schema-valid file is fully supported.

## Run

```sh
cd devboard
mkdir -p ~/.local/share/devboard
docker compose up --build -d
# open http://localhost:8484
```

Defaults: data dir `~/.local/share/devboard`, port `8484`, worklog dir
`~/.local/share/worklog` (for notes rendering). Override with
`DEVBOARD_DATA=/path WORKLOG_DATA=/path DEVBOARD_PORT=9000 docker compose
up -d`. The worklog mount is read-only — the dashboard can never modify
worklog state. The data mount is writable for exactly one operation:
archiving (see below).

Without Docker: `DEVBOARD_DATA=~/.local/share/devboard python3 server.py`
(needs `pyyaml`).

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
archived tasks leave the grid, stats, and attention band and collapse into
an "Archived · N" fold with un-archive buttons that move them back. Nothing
is ever deleted or rewritten — both endpoints (`POST /api/archive`,
`POST /api/unarchive`, JSON body `{"repo", "id"}`) are a single validated
rename, and the worklog dir is never touched. There is no auth: anyone who
can reach the port can archive/un-archive (reversible by design; the server
rejects non-JSON content types and never answers CORS preflights, so a
browsing session on another site can't trigger it cross-origin).

## Friction panel

`FEEDBACK.md` at the root of the worklog mount is the friction log the
worklog skill's capture subagent appends to (`worklog feedback append`).
The board renders it as a single global band — it is not per-task, so no
task-file field is involved:

- an unreviewed count in the topbar stats, absent when nothing is outstanding
- a collapsed `Friction · N` fold with a count per signal, then the
  unresolved entries newest-first (signal, local time, trigger, and a fold
  for the excerpt and context)
- resolved entries in a `Resolved · N` sub-fold, dimmed

Reviewing happens in the CLI, never here: the worklog mount is read-only, so
each entry offers a button that copies `worklog feedback resolve <timestamp>`
rather than writing anything. Running it adds a `**Resolved**: <unix-ts>`
line to that entry, and the board updates over SSE within ~2s.

The entry format is owned by the Go side
(`worklog/internal/feedback/feedback.go`); `server.py` parses it a second
time because the container ships no `worklog` binary. The reader skips
unknown `**Field**:` lines rather than failing, so the two can be extended
independently. `devboard/test_server.py` (stdlib `unittest`, no extra
dependency) pins the reader:

```sh
cd devboard && python3 -m unittest test_server
```

## Behavior notes

- Malformed files render as an error card (filename + parse error); they
  never take down the rest of the page.
- Unknown top-level fields render in an "Other" section — extend freely.
- Tasks untouched for >2h render dimmed (likely-stale signal).
- A missing or malformed `FEEDBACK.md` simply means no friction panel —
  it never takes down the page.
- Server: Python stdlib + PyYAML; a 1s mtime scan drives the SSE stream
  (no inotify — works identically under Docker volume mounts).
