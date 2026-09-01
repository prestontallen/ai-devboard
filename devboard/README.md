# Devboard

A read-only browser dashboard over a directory of task files, so the human
can follow agent work without living in terminal scrollback. Agents (or
anyone) drop one YAML/JSON file per task into `<data-root>/<repo>/`; the
page renders plan todos, the contract scorecard, decisions, code-to-know,
and a "needs you" attention queue — and hot-reloads within ~2s of any file
change, no refresh needed (SSE, auto-reconnecting).

## Run

```sh
cd devboard
mkdir -p ~/.local/share/devboard
docker compose up --build -d
# open http://localhost:8484
```

Defaults: data dir `~/.local/share/devboard`, port `8484`. Override with
`DEVBOARD_DATA=/path DEVBOARD_PORT=9000 docker compose up -d`. The data dir
is mounted read-only — the dashboard can never modify workflow state.

Without Docker: `DEVBOARD_DATA=~/.local/share/devboard python3 server.py`
(needs `pyyaml`).

## Directory layout

```
<data-root>/
  <repo-name>/          # grouping = directory name
    <task-slug>.yaml    # one file per task (.yml/.json also fine)
```

See [schema.md](schema.md) for the task file format (`schema: 1`), and
[examples/](examples/) for ready-made files — try it out with:

```sh
cp -r examples/* ~/.local/share/devboard/
```

## Behavior notes

- Malformed files render as an error card (filename + parse error); they
  never take down the rest of the page.
- Unknown top-level fields render in an "Other" section — extend freely.
- Tasks untouched for >2h render dimmed (likely-stale signal).
- Server: Python stdlib + PyYAML; a 1s mtime scan drives the SSE stream
  (no inotify — works identically under Docker volume mounts).
