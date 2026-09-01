# Contract — Devboard archive / un-archive (first write path)

- **Date:** 2026-09-01
- **Tier:** 2 Feature
- **Status:** in-progress
- **Worklog:** csk-devboard-archive

## Intent

Completed (or abandoned) task files pile up in the data dir forever; the Done
fold hides them visually but the human currently has to shell out to actually
clear the board. Add an **archive** action to the GUI: move the task's YAML
into `<repo>/_archive/` — kept on disk, ignored by the main board — and an
**un-archive** action to bring it back. Reversibility is the reason there is
no delete: nothing in the GUI ever destroys a file.

This is deliberately the devboard's first write path. The server has been
read-only until now; the design below keeps the writable surface as small as
possible (one move operation, two directions, no content writes).

## Scope

**In:**
- `devboard/server.py`: `POST /api/archive` and `POST /api/unarchive`
  (JSON body `{repo, id}`); scanner additionally lists `<repo>/_archive/*`
  tasks flagged `archived: true`; watcher covers `_archive/` so moves
  hot-reload.
- `devboard/static/index.html`: archive button on Done-fold cards and in the
  detail view (any task); "Archived · N" fold below the Done fold with
  un-archive buttons; archived tasks excluded from stats, attention band,
  main grid, and Done fold; error feedback when a move fails.
- `devboard/compose.yaml`: data mount `:ro` → rw (worklog mount stays `:ro`).
- Docs: `devboard/schema.md` layout note + `devboard/README.md` for the new
  endpoints and `_archive/` convention.

**Out (explicitly not doing):**
- No delete, ever — archive is the only removal and it is reversible.
- No auth. The server binds 0.0.0.0, so anyone on the LAN can archive/
  un-archive. Accepted because the action is reversible and content-free;
  see Risks — this is the one line the human must consciously accept.
- No bulk operations, no undo history, no confirmation dialogs (reversible
  actions don't need them).
- No worklog CLI involvement (`worklog task untrack` remains the CLI path;
  archive is a devboard-only concept, invisible to worklog).
- No changes to task file content or the field schema; `_archive/` is pure
  filesystem convention.
- No CORS headers, no OPTIONS handling — their absence is the CSRF guard.

## Deliverables

- Updated `server.py`, `static/index.html`, `compose.yaml`, `schema.md`,
  `README.md`; this contract.

## Acceptance criteria

| # | Criterion | Verify | Status |
|---|-----------|--------|--------|
| 1 | Clicking archive moves `<repo>/<id>.yaml` to `<repo>/_archive/<id>.yaml`; the card leaves the board/Done fold and appears under "Archived · N" | browser click + `ls` on disk | ☐ |
| 2 | Clicking un-archive moves the file back; the task reappears in its normal board position | browser click + `ls` | ☐ |
| 3 | Archived tasks are excluded from stats, attention band, main grid, and Done fold; their detail view still opens via deep link and offers un-archive | fixtures | ☐ |
| 4 | Endpoint safety: non-POST → 405; body not `application/json` → 415; `repo`/`id` containing `/`, `\`, `..`, or not matching an existing scanned file → 4xx; disk untouched in every rejection | curl matrix | ☐ |
| 5 | Cross-origin guard: a "simple" CSRF-style request (form-encoded or text/plain body) is rejected by the content-type check; JSON from another origin dies on the unanswered preflight | curl with forged Content-Type/Origin | ☐ |
| 6 | A move that would overwrite an existing `_archive/` file (or vice versa on un-archive) is refused with an error the UI surfaces; no overwrite | collision fixture | ☐ |
| 7 | Archiving hot-reloads every open tab (watcher sees `_archive/`) | two tabs, one click | ☐ |
| 8 | Works in the rebuilt container with the rw data mount (root renames preston-owned file; ownership preserved) | live container click test | ☐ |
| 9 | Sad path: when the data dir is not writable, the endpoint returns an error, the UI shows failure feedback, and the board keeps rendering | read-only dir test | ☐ |
| 10 | Worklog untouched: archive/un-archive never reads from or writes to the worklog data dir — it is byte-identical before and after, and the worklog ticket/notes/archive are unaffected | checksum `~/.local/share/worklog` before/after | ☐ |

## Definition of done (standing bar)

- [ ] No unrelated changes in the diff
- [ ] Docs updated (schema.md layout, README endpoints)
- [ ] Manual verification is the accepted bar (no UI test harness exists)
- [ ] Final verification against the rebuilt container

## Constraints & assumptions

- Single-file frontend constraint still holds (server serves only
  `/index.html`); the button/fold UI lands in the same file.
- The scanner already ignores subdirectories of a repo dir, so `_archive/`
  is invisible to the existing board loop by construction; only the new
  flagged listing exposes it.
- Container runs as root; `os.rename` within one filesystem preserves file
  ownership. The `_archive/` dir itself may be created root-owned from the
  container — cosmetic, noted, accepted.
- Risk-scout reuse: both lenses (blockers, downstream) ran against this repo
  earlier today for the UI redesign; their findings (docs promises, scanner
  shape, `:ro` mounts, no test harness) are folded in here, and the two
  deploy files were re-read directly. No fresh fan-out was run.

## Risks & open questions

- **LAN-open write endpoint (accepted?):** anyone who can reach port 8484
  can archive/un-archive (not delete). Mitigations in scope: POST-only,
  strict JSON content-type (blocks CSRF simple requests), no CORS/OPTIONS
  (blocks cross-origin JSON via preflight), path-validated ids. Out of
  scope: auth. **The human accepts this residual risk by approving this
  contract.**
- Archive/worklog divergence: archiving on the devboard does not touch the
  worklog ticket. A live (non-done) task can be archived and silently drop
  off the board while its worklog ticket stays open — mitigated by keeping
  the archive button prominent only on done cards (detail view covers the
  rest deliberately).
- Compose change means the dashboard process gains write access to the data
  dir permanently — the blast-radius cost of any future server bug goes up.
  Mitigation: the write code path is two `os.rename` calls behind
  validation; no content writes exist.

## Amendments

| Date | Change | Why | Approved |
|------|--------|-----|----------|
| 2026-09-01 | draft → agreed; LAN-open write endpoint risk accepted ("local only tool, address auth later if need be") | Contract review | Preston |
