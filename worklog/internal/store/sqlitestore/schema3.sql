-- Migration 3: board render freshness as data (adb-store-serve-shadow).
-- board_rendered_at is when this ticket's rendered board file bytes last
-- changed, unix nanoseconds (0 = never rendered). It exists so a
-- store-served /api/tasks entry can report the same mtime the file-served
-- payload stats off disk; TouchBoardRendered is its sole writer, stamped
-- from the projection layer's writeIfChanged decision — a byte-identical
-- re-render advances neither the file mtime nor this column.
--
-- The DEFAULT is load-bearing: the migration loop is additive and
-- version-guarded, so an older binary still opens a migrated db — but
-- only because its column-less INSERT stays valid. PutTicket deliberately
-- never writes this column (see store.Ticket.BoardRenderedAt).

ALTER TABLE tickets ADD COLUMN board_rendered_at INTEGER NOT NULL DEFAULT 0;
