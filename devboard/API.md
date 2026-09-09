# Devboard server API — the frozen frontend contract

Served by `worklog serve` (Go, `worklog/internal/serve`), which replaced
`devboard/server.py` in 2026-09. This document freezes what the frontend
may rely on; the golden fixture
(`worklog/internal/serve/testdata/golden_tasks.json`, captured from the
Python server before retirement) is the mechanical record.

The point of the freeze: the storage layer under this server will change
(adb-worklog-rewrite). The JSON below is the boundary — storage swaps must
be invisible on this surface.

That change has landed for reads. The payload is built from the worklog
store's tickets, not by walking rendered files
(`worklog/internal/serve/store_tasks.go`, adb-serve-store-direct). The
shape below did not move: it is pinned by a fixture captured from the
file-walking builder before it was deleted.

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
| `/api/archive`, `/api/unarchive` | POST | archive or restore a ticket |

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

- Repo grouping is the ticket's own repo attribution, sorted. A ticket
  with no repo groups under `unknown`. A group with no tickets is omitted.
- Entries within a group: live first, then archived (each with
  `"archived": true`), sorted by ticket id inside each half.
- Only board-tracked tickets appear, and a child of an epic never appears
  on its own — it is carried inside its epic's `children`.

Task entry:

```
{
  "file":     "<repo>/[_archive/]<id>.yaml",
  "id":       "<ticket id>",
  "archived": true,            // archived entries only
  "task":     { ...the ticket's board shape... },
  "mtime":    <float, unix seconds>,
  "notes":    "<full notes text>"         // when the ticket has notes
}
```

- **`task` is the ticket's board shape, passed through generically.**
  Unknown keys at any level reach the frontend verbatim (the detail view
  renders unrecognized top-level keys in its "Other" table). The server
  never decodes into the schema structs. **Additive policy:** new keys may
  appear at any time; consumers must ignore what they don't know. This is
  how schema growth (new phases, new fields) ships without a contract rev.
- `file` is composed from the ticket's repo, archived state and id. It
  names no file the server reads; the frontend uses it as a card-title
  fallback and it is kept because the shape is frozen.
- `mtime` is the ticket row's `updated_at`: when the ticket was last
  written. It moves for the ticket that was written and for no other, and
  a write that changes nothing does not move it, because the store
  recognises an unchanged aggregate and skips the write outright.
- `notes` appears when the ticket has a notes file. **`task.children[].notes`**
  appears on the same terms, keyed by the child's id: a child of an epic
  has no entry of its own but is a worklog ticket with its own notes. A
  ticket or child with no notes carries no `notes` key at all, never an
  empty string.
- There is no `error` entry. Error cards existed because a hand-dropped
  file could fail to parse; a ticket cannot, so the key is gone. A store
  that cannot be read is a `500`, not a card.

Backlog section — the not-yet-started work the board's cards cannot carry,
since a ticket reaches the board only once it is started:

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
- Items come from the tickets in each section, in the human's own order,
  through the one ticket-to-block correspondence `internal/blockmap` holds.
  An empty section yields an empty list, never an error — the backlog is
  one lens of seven and must not take the payload down. Unstarted children
  of an epic never appear; only an epic's *active* children do.

Feedback entry (read with the same parser the CLI writes it with):

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
- One event immediately on connect; one whenever the store changes
  (polled at `DEVBOARD_SCAN_INTERVAL`, default 1s); one synchronously
  after a successful archive/unarchive POST.
- **A write that changes nothing produces no event.** The signal is a
  content fingerprint of what the board draws, not a timestamp: setting a
  field to the value it already held rewrites rows and moves the database
  file, and neither is a change here. Notes and friction entries are part
  of that content, so appending either does fire.
- A read failure is not a change, so a briefly busy database does not make
  every open board refetch.
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
| 404 | the store board-tracks no ticket under that id | `{"error": "task not found"}` |
| 503 | the worklog is frozen for adoption | `{"error": "the worklog is frozen for adoption; try again when it finishes"}` |
| 500 | the store write failed, or no store hook is wired | `{"error": "move failed: ..."}` |
| 200 | recorded | `{"status": "archived"\|"restored", "repo": ..., "id": ...}` |

**The write is a store write and nothing else.** Archiving sets a field on
the ticket; the re-render is what places the file. The endpoint reads no
directory, takes no lock and renames nothing. It used to rename and then
tell the store, and a failed sync answered `200` with an empty log and
left disk and store disagreeing, after which every store-backed CLI write
refused (`adb-archive-store-desync`). Two writers deciding where one file
lives is the fault; there is now one.

**`404` covers two shapes**: an id with no ticket at all, and a ticket that
exists but is not board-tracked. The second resolves, so setting a field on
it would look like success while no card ever existed to move. From the
board's side neither has a card, so both answer the same.

**`503` is the adoption freeze.** `worklog adopt --commit` takes a sentinel
that CLI processes check at start-up. The server is exempt as a process and
this handler runs per request, long after, so it checks for itself — until
this landed, adopting on live data meant stopping the service by hand. A
sentinel that cannot be read counts as frozen.

**A failed write is a failure.** The response is `500`, the cause is logged,
and nothing changed. There is no partial outcome.

## Configuration

Env vars, unchanged from the Python server, with native defaults:

| Var | Default |
|---|---|
| `DEVBOARD_WORKLOG` | `~/.local/share/worklog` (honors `XDG_DATA_HOME`) |
| `DEVBOARD_PORT` | `8484` |
| `DEVBOARD_SCAN_INTERVAL` | `1.0` (seconds) |

`DEVBOARD_DATA` is gone: the server has no data directory to point at.

Binds `0.0.0.0` — the board is used over LAN. The store beside the worklog
dir is the only thing the server reads, and archiving is the only thing it
writes.
