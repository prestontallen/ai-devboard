-- Migration 4: created_at and updated_at (adb-retire-devboard-dir-2).
--
-- The board's sense of time used to come from the rendered YAML file's
-- mtime, stamped into board_rendered_at by the projection layer. That was
-- never a design: writeIfChanged skipped a byte-identical render, the file
-- mtime therefore did not move, and the stamp followed it. With the file
-- retired, the row itself carries the answer.
--
-- updated_at is set by PutTicket on every write, so it moves for the
-- ticket that was written and for no other. That is the property the old
-- mechanism did NOT have on its own: the renderer walked every ticket, and
-- only the file comparison stopped it marking the whole board fresh
-- whenever one card changed.
--
-- The rename carries the existing values across, so no card loses its age
-- on upgrade. A row written before this migration keeps whatever the file
-- mtime last said, which is the same instant it would have recorded.
--
-- What is given up, deliberately: a write that changes nothing still
-- counts as an update. Setting a phase to the value it already held will
-- reset that card's age badge. The precision was an artifact of comparing
-- file bytes, not something the board asked for.

ALTER TABLE tickets RENAME COLUMN board_rendered_at TO updated_at;
ALTER TABLE tickets ADD COLUMN created_at INTEGER NOT NULL DEFAULT 0;
