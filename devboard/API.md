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

Six endpoint groups. Anything else is 404 `{"error": "not found"}` — including
every directory path under `/assets/`, which never lists its contents.

| Endpoint | Method | Behavior |
|---|---|---|
| `/`, `/index.html` | GET | the board page, `text/html; charset=utf-8` |
| `/next` | GET | `308` to `/` — the board's former address, see below |
| `/assets/<path>` | GET | one embedded front-end module, typed by extension |
| `/api/tasks` | GET | full payload, see below |
| `/events` | GET | SSE change stream |
| `/api/archive`, `/api/unarchive` | POST | move a task file into/out of `<repo>/_archive/` |

All responses carry `Cache-Control: no-store`, `/assets/*` included. GET on
the POST endpoints is 405 `{"error": "POST only"}`. Any other method is 501
`{"error": "unsupported method"}`. `/next` is the only route that redirects.

### `/`, `/next` and `/assets/*`

`/` serves the Lens Board. It was built at `/next` over the course of the
Lens Board epic and moved here by `adb-lens-cutover`, which deleted the
board it replaced.

`/next` is kept as a `308` to `/` rather than 404ed. A fragment never
reaches the server, so a bookmarked `/next#/backlog` can only keep working
by the browser reapplying the fragment to a target that carries none —
which is exactly what a redirect to a bare `/` gets you. The route is a
compatibility shim and can be dropped once nothing points at it.

The board consumes this document's `/api/tasks` payload like any other
client — the freeze binds it, it does not bend for it. The archive and
un-archive controls on `#/done` and `#/archived` POST to the same two write
endpoints documented below, with the same body and the same `Content-Type`
requirement.

Routing is through the URL fragment — `#/board`, `#/needs-you`, `#/waiting`,
`#/friction`, `#/backlog`, `#/done`, `#/archived`, task detail at
`#/task/<repo>/<id>` and a child of an epic at
`#/task/<repo>/<epic-id>/<child-id>` (every segment percent-encoded). Those
are **unfrozen internals**, not API: they may be renamed without notice.

The outgoing board's grammar — `#<repo>/<id>` and `#<repo>/<epic>/<child>`,
with no leading slash — is translated to the above on arrival and the URL
rewritten in place, so links saved before the cutover still resolve. The
leading slash is what makes that unambiguous: the two grammars cannot
collide. Path-style deep links do not exist — `/<lens>` is a 404, by test.

`/assets/<path>` serves files embedded from
`worklog/internal/serve/static/assets/`, and only those: there is no disk
fallback and no user-supplied path reaches a filesystem. Paths are refused
unless already canonical, so traversal attempts 404 rather than being
normalized into a hit. Content types are assigned from an explicit extension
table (`.js`, `.map`, `.json`, `.css`, `.html`, `.md`, `.svg`), defaulting to
`application/octet-stream`.

The asset root is `static/assets/`, not `static/`, so the board page is
reachable at exactly one URL: `/assets/index.html` has nothing to resolve to
and 404s, as `/static/index.html` always has.

## /api/tasks payload

```
{
  "version":   <int, change counter since server start>,
  "generated": <float, unix seconds>,
  "repos": [
    { "repo": "<dir name>", "tasks": [ <entry>... ] }
  ],
  "feedback": [ <feedback entry>... ],
  "backlog":  [ <backlog section>... ]
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
- `notes` appears when `task.worklog` is a plain name (no separator, no
  `..`) and `<worklog>/notes/<name>.md` is readable.
- **`task.children[].notes`** appears on the same terms, keyed by the child's
  `id` instead: a child of an epic has no task file and so no `worklog` key of
  its own, but it is a worklog ticket whose id IS the notes filename (see
  `schema.md`, "Epic files"). The id runs through the same plain-name guard,
  because it comes out of the same hand-editable file. A child with no
  readable notes file carries no `notes` key at all, never an empty string.
- A file that fails to parse yields an error card: `error` present,
  `task`/`mtime` absent, the board renders it as a card — never a 500.

Backlog section (parsed from `<worklog>/WORK.md` by the same package the
CLI parses it with) — the not-yet-started work the task files cannot carry,
since a task file exists only once a ticket is started:

```
{ "name": "Next", "items": [ { ...model.Block... } ] }
```

- `backlog` is a list of sections in bar order: `Next`, then `Someday`.
  `Now` and `Waiting` are deliberately absent — that work is already on the
  board as task files, and carrying it twice would double it against the
  Board and Waiting chips.
- Each section is **always present**, empty when it has no items, so "no
  tickets queued" and "the server never reported this section" stay
  distinguishable.
- An item is a `model.Block`: `id`, `title`, `type`, `repo`, `tags`,
  `acceptance`, `parent`, `links` and the rest of the WORK.md metadata.
- A missing, unreadable or malformed `WORK.md` yields empty sections, never
  an error — the backlog is one lens of seven and must not take the payload
  down. Unstarted children of an epic never appear, because `WORK.md`
  carries only an epic's *active* children.

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
| 409 | destination file already exists (rename path only) | `{"error": "destination already exists"}` |
| 500 | the move failed, and nothing was moved | `{"error": "move failed: ..."}` |
| 200 | moved | `{"status": "archived"\|"restored", "repo": ..., "id": ...}` |

The move runs under `<repo>/<file>.lock` — the same flock
`devboard.Mutate` takes — so a concurrent CLI mutation cannot race it.

**Who moves the file.** For a task the worklog store board-tracks, the
store does: archiving sets a field, and re-rendering writes the YAML at
the archived path and clears the live one. The endpoint never renames
those, because two writers deciding where one file lives is what made a
failed sync leave disk and store disagreeing — after which every
store-backed CLI write refused (`adb-archive-store-desync`).

A file the store does not board-track — a hand-dropped producer file has
no ticket behind it — is renamed by the endpoint, which is the only
archive mechanism it has.

**A failed move is a failure.** If the store write fails the response is
`500`, the cause is logged, and the file has not moved. There is no
partial outcome: either the task is archived in both places or in
neither.

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
