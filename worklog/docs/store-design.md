# Worklog store design — schema, identity, projections

Deliverable of adb-schema-design (epic adb-worklog-rewrite). Status: the
design is implemented and proven — the converter round-trips the full
live corpus (72 tickets, 2026-09-02 snapshot) through both Store
implementations with semantic equality and deterministic IDs.

## The boundary

Everything programs against `internal/store.Store` (store.go). Two
implementations exist and pass one shared conformance suite
(`store/storetest`): `sqlitestore` (durable; modernc, WAL, user_version
migrations) and `memstore` (proves the boundary). A boundary test refuses
any consumer importing an implementation. The eventual CLI verbs and the
server wire an implementation at their composition root and nowhere else.

## Entities

One aggregate root — `Ticket` — covers live tickets, epics, spikes,
chores, archived entries, and epic children (the four legacy epic↔child
places collapse into `ParentID`). Sub-items all carry ULIDs and ranks:
plan steps, scorecard items, decisions, code refs, needs-you, waiting-on,
typed links, phase transitions, note entries. `FeedbackEntry` and the
`FieldChange` journal complete the model. Field-by-field coverage of the
legacy vocabularies (devboard.Task, ChildEntry, model.Block, archive
format, reindex records) is enforced mechanically: the round-trip test
fails on any dropped field, and the live corpus is the checklist.

Notable modeling decisions:

- **Slug is an alias, not identity.** Mutable, unique case-insensitively
  across all history (`slug_aliases` keeps every slug ever worn resolving
  and reserved — ratified OQ3). Slug-less title-only quick-capture blocks
  are legal; their identity is the ULID alone.
- **PR is `*string`**: a `**PR**: ` line with no value is not the same as
  no line, and the corpus distinguishes them. Elsewhere absent == empty.
- **`PlanText` vs `PlanSteps`**: WORK.md's `**Plan**` string and the
  devboard plan list are different things sharing a name; both have homes.
- **Links are typed** (`kind: pr|ref`); a partial unique index makes a
  second pr-link unconstructable — the old label-collision class is gone.
- **Decisions are a set** on (what, why), deduped before rank minting so
  ranks stay contiguous (the live corpus contains real duplicates).
- **Extras everywhere**: unknown YAML keys (`Extra`) and unknown WORK.md
  field bullets (`ExtraFields`) round-trip on the ticket and on every
  sub-item, serving the frozen additive-keys promise (devboard/API.md).
- **Board state is entity state**: `BoardTracked` / `BoardArchived`
  replace "has a file" / "sits in _archive/" — visibility stops being an
  artifact of file existence (unblocks adb-devboard-backlog-visibility).
- **Phase history** is `phase_transitions` (from, to, at, actor, note)
  per task and per child; `Phase` stays the authoritative current value.
  The converter does not fabricate history; it accrues from cutover
  (csk-devboard-timestamps' data half).
- **Bare devboard files are not canon** (ratified D7): producer-owned
  files with no `worklog:` join stay on disk untouched; renderers write
  only files derived from canon.

## Identity

ULIDs (oklog/ulid), minted exactly once per entity: the converter
resolves by slug (or exact title for slug-less entities) before minting,
so re-runs into the same store reuse every ID — the store IS the
persisted id-map (D4), which is what makes adb-worklog2-migrate's
id-set-diff check meaningful. Sub-item IDs carry across re-runs by exact
content match, for every sub-item kind (plan steps, scorecard, decisions,
note entries, links, code refs, needs-you, waiting-on) — verified by
adb-worklog2-migrate, which found and fixed four kinds the original
implementation missed. Removing an item never renumbers survivors
(adb-task-item-ids retired by construction).

## Concurrency contract

WAL + busy_timeout(5000) + foreign keys, single connection. Readers never
block on the writer; a second writer waits instead of erroring. The
SessionStart hook and `status` remain read-only. Cross-process WAL under
the pure-Go driver is the one caveat the cutover ticket must load-test
(modernc's VFS locking is its least battle-tested area).

## Migrations

`PRAGMA user_version`, numbered, one transaction each
(sqlitestore/schema.sql is migration 1). The devboard YAML's `schema: 1`
stays the projection format's version, independent of the DB's.

## Projections (internal/projection)

All five surfaces render from the store; every one is verified by the
real reader that consumes it (the oracle suite):

| Surface | Spec | Oracle |
|---|---|---|
| WORK.md | parse.File's grammar: section headings, state chars, `**ID** — Title` bullets, field bullets in FormatTicketBlock order (+ Link/Plan/extras), `**PR**: ` always-rendered-when-present, `<none>` sentinel, Active-children derived from the relation | parse.File field-equality vs original |
| notes/<slug>.md | verbatim preamble (human-owned; includes the roster region) + `## YYYY-MM-DD HH:MM` entries per the D6 segmentation rule — only timestamp-shaped headings split entries, duplicates legal | segmentation entry-count stability; serve `notes` join |
| archive/<month>.md | FormatArchiveEntry field order; day headings newest-first; Parent/Children derived from the relation; bare-Completed for epics; nested Feedback bullets; Time | standup.ParseFile entry-set equality |
| INDEX.md | reindex.Run over the rendered tree — the projection IS that code's output, not a reimplementation | reindex.Run counts |
| devboard feed | slug-named YAML in the canonical repo's group (misfiled files heal at cutover, ratified OQ2), `_archive/` from BoardArchived, full per-child nesting for board-tracked children, extras merged back at every level | the ported server's /api/tasks over rendered files: passthrough, children, notes join |

Freshness rule (criterion 13): `writeIfChanged` — byte-identical renders
never touch the file, so the server's mtime watcher sees no phantom
changes and the frozen SSE behavior cannot fire on no-op rebuilds.

FEEDBACK.md is canon (ratified OQ4): entries carry ULIDs; the epoch
stamp remains the stable alias (`feedback resolve` handle, frozen JSON
timestamp); same-second entries are distinct.

## The converter (internal/convert)

Full-fidelity parsers, deliberately not the CLI's lossy ones: WORK.md
(unknown bullets kept, unmodeled lines refused), archive (all ~14 fields;
the CLI's parser keeps 8), notes (D6 segmentation — the live corpus
contains duplicate stamps and body headings, so only a format rule
resolves the ambiguity). Refusals, never silent imports: unmodeled
content, duplicate slugs across live+archive, board joins to ghost
tickets, notes for unknown slugs, Active-children inconsistency. The
converter reads a COPY; the live dir is never written.

Verified conversions preserve the live corpus's oddities verbatim with a
lint warning (the space-separated tag), skip the three bare producer
files, and heal nothing silently.

## Corpus snapshot policy

The committed corpus (`internal/convert/testdata/corpus`) is synthetic —
it encodes every hazard class the scouts found, and CI runs on it alone.
The real proof runs locally: `WORKLOG_SNAPSHOT=<dir> go test -run
TestLiveSnapshot ./internal/projection/` against a copied snapshot
(layout documented in the test). Personal data never enters the repo.
Snapshot pinned for this ticket: 2026-09-02, repo at the adb-schema-design
branch head. Re-capture rule: when adb-research-mode or
adb-epic-per-child-cards land format changes, refresh the snapshot and
re-run; the synthetic corpus gains a fixture only if a new hazard class
appears.

## Migrate command (internal/migrate, internal/cli/migrate.go)

`worklog migrate` (adb-worklog2-migrate) is the first thing to actually
run this design end to end. It stages a read-only copy of the live
worklog dir + devboard dir (never writes them — verified by a live-dir
checksum test), converts the copy via `convert.ReadCorpusDir` +
`convert.Load`, and reports whether entity identity held steady: an
id-set diff of every ticket and sub-item ULID between the pre- and
post-run state of a persisted SQLite db, plus stale rows the diff alone
can't see (`convert.Load` only upserts — nothing here deletes).

Mechanism, in order: stage (with size/mtime tear detection and one retry —
`ErrTornSnapshot`), copy-forward seed the working copy from the existing
output db (a plain read of `OUTPUT_PATH`; it is never opened for writing —
`convert.Load` runs against the working copy only), checkpoint
(`PRAGMA wal_checkpoint(TRUNCATE)`) and close the working copy, then
atomically swap it into place — `OUTPUT_PATH` → `OUTPUT_PATH.bak` (one
generation, no rotation), working copy → `OUTPUT_PATH` — clearing any WAL
sidecar rather than carrying or orphaning it. All of it lives under one
directory: `--out`, `$WORKLOG_MIGRATION_DATA`, or
`~/.local/share/worklog-migration` by default.

Building this against the real mechanics (not just against `convert.Load`
in isolation) found and fixed two gaps in the identity guarantee the
design above describes: `carrySubItemIDs` didn't cover links, code refs,
needs-you or waiting-on (only plan steps, scorecard, decisions and note
entries), and feedback entries had no re-run identity at all, so
copy-forward would have duplicated every friction entry on every run. Both
are fixed in `internal/convert`; see the Identity section above.

Still not done here: the production cutover — freeze, rename binary, retire
old write paths (`adb-cutover`); skill text updates for the projection
world (`adb-skill-projection-update`); JSONL export (deferred, D9).

## Verify command (internal/verify, internal/cli/verify.go)

`worklog verify` (`adb-projection-render`) is the store/projection design's
first production caller: it stages a read-only snapshot of the live
worklog + devboard dirs (reusing `internal/migrate`'s `Stage`), converts it
into an in-memory store, renders that store's projections into a second
scratch dir via `RenderAll`, and reports field-level drift between the
staged snapshot and the render — surface by surface (WORK.md, notes,
archive, INDEX.md, FEEDBACK.md, devboard feed). It never writes to the
live worklog or devboard directories, under any outcome; that write-back
is deferred to `adb-cutover` (contract:
`contracts/ai-devboard/2026-09-03-adb-projection-render.md`).

`worklog verify` is unrelated to the pre-existing `worklog validate`:
`validate` checks structural invariants over live data as it stands today
(e.g. three-place epic/child consistency); `verify` checks live data
against what the rewrite's projections would render from it. The two can
disagree without either being wrong — `validate` was written against the
legacy format's own rules, `verify` against the store/projection design.

Composition roots that open a concrete `store.Store` implementation
directly are no longer unique to `internal/migrate`: `internal/cli/verify.go`
also constructs one (`memstore.New()`), by design (contract Decision #4) —
`internal/verify` itself stays interface-only, so the CLI layer is the
composition root for this command, consistent with "the eventual CLI verbs
... wire an implementation at their composition root and nowhere else"
above.
