# Devboard server API — the frozen frontend contract

Served by `worklog serve` (Go, `worklog/internal/serve`), which replaced
`devboard/server.py` in 2026-09. This document freezes what the frontend
may rely on; the golden fixture
(`worklog/internal/serve/testdata/golden_tasks.json`, captured from the
Python server before retirement) is the mechanical record.

The point of the freeze: the storage layer under this server will change
(adb-worklog-rewrite). The JSON below is the boundary — storage swaps must
be invisible on this surface.

## Surface

Exactly four endpoints. Anything else is 404 `{"error": "not found"}`.

| Endpoint | Method | Behavior |
|---|---|---|
| `/`, `/index.html` | GET | the embedded board page, `text/html; charset=utf-8` |
| `/api/tasks` | GET | full payload, see below |
| `/events` | GET | SSE change stream |
| `/api/archive`, `/api/unarchive` | POST | move a task file into/out of `<repo>/_archive/` |

All responses carry `Cache-Control: no-store`. GET on the POST endpoints
is 405 `{"error": "POST only"}`.

## /api/tasks payload

```
{
  "version":   <int, change counter since server start>,
  "generated": <float, unix seconds>,
  "repos": [
    { "repo": "<dir name>", "tasks": [ <entry>... ] }
  ],
  "feedback": [ <feedback entry>... ]
}
```

- Repo grouping is the directory name under the data dir, sorted; hidden
  dirs skipped; repos with zero task files omitted.
- Task files: `*.yaml|*.yml|*.json`, sorted by filename, live files first,
  then `_archive/` files (each with `"archived": true`).

Task entry:

```
{
  "file":     "<path relative to data dir>",
  "id":       "<filename without extension>",
  "archived": true,            // archived entries only
  "task":     { ...raw parsed file... },
  "mtime":    <float, unix seconds>,
  "notes":    "<full notes file text>",   // when task.worklog names one
  "error":    "<message>"      // parse failure: no task/mtime, entry stays
}
```

- **`task` is the raw parsed file, passed through generically.** Unknown
  keys at any level reach the frontend verbatim (the detail view renders
  unrecognized top-level keys in its "Other" table). The server never
  decodes into the schema structs. **Additive policy:** new keys may appear
  at any time; consumers must ignore what they don't know. This is how
  schema growth (new phases, new fields) ships without a contract rev.
- `notes` appears when `task.worklog` is a plain name (no `/`, no `..`)
  and `<worklog>/notes/<name>.md` is readable.
- A file that fails to parse yields an error card: `error` present,
  `task`/`mtime` absent, the board renders it as a card — never a 500.

Feedback entry (parsed from `<worklog>/FEEDBACK.md` by the same package
the CLI writes it with):

```
{ "timestamp": <int>, "signal": "<slug>", "trigger": "...",
  "excerpt": "...", "context": "...", "resolved": <int, 0 = open> }
```

`resolved` is always present. Unknown `**Field**:` lines are skipped and
end any excerpt in progress. Any read/parse problem yields `[]` — friction
is a side panel and must never take down the page.

## Deliberately NOT frozen

- **Key order** inside JSON objects (the frontend reads by name).
- **Error message text** (`error` values) — presence and placement are
  frozen, wording is whatever the parser produces.
- **YAML 1.1 bool coercion** — accepted divergence, ratified 2026-09-02:
  bare `yes/no/on/off` are strings (YAML 1.2), where the Python server
  made them booleans. Write `true`/`false` for booleans.
- NaN/Inf float scalars are sanitized to their raw text (the Python
  server emitted invalid JSON for these — a bug, not reproduced).

## /events (SSE)

- Unnamed `message` events, body `data: {"version": N}`.
- One event immediately on connect; one whenever watched files change
  (task files live+archived, worklog `notes/*.md`, `FEEDBACK.md` —
  polled at `DEVBOARD_SCAN_INTERVAL`, default 1s); one synchronously
  after a successful archive/unarchive POST.
- `: keepalive` comment after 15s idle. No `retry:`/`id:` fields;
  clients rely on EventSource auto-reconnect.

## Write endpoints

POST body `{"repo": "<name>", "id": "<task id>"}`. Requires
`Content-Type: application/json` — this is the CSRF guard (cross-origin
JSON forces a preflight this server never answers).

| Code | When | Body |
|---|---|---|
| 415 | Content-Type not application/json | `{"error": "Content-Type must be application/json"}` |
| 400 | invalid JSON | `{"error": "invalid JSON body"}` |
| 400 | repo/id empty, dot-prefixed, containing `..`, `/` or `\` | `{"error": "invalid repo or id"}` |
| 404 | no matching task file on the source side | `{"error": "task not found"}` |
| 409 | destination file already exists | `{"error": "destination already exists"}` |
| 500 | rename failed | `{"error": "move failed: ..."}` |
| 200 | moved | `{"status": "archived"\|"restored", "repo": ..., "id": ...}` |

The rename runs under `<repo>/<file>.lock` — the same flock
`devboard.Mutate` takes — so a concurrent CLI mutation cannot race the
move (an internal upgrade over the Python server, which renamed bare).

## Configuration

Env vars, unchanged from the Python server, with native defaults:

| Var | Default |
|---|---|
| `DEVBOARD_DATA` | `~/.local/share/devboard` |
| `DEVBOARD_WORKLOG` | `~/.local/share/worklog` (honors `XDG_DATA_HOME`) |
| `DEVBOARD_PORT` | `8484` |
| `DEVBOARD_SCAN_INTERVAL` | `1.0` (seconds) |

Binds `0.0.0.0` — the board is used over LAN. The server reads the
worklog dir and never writes under it.
