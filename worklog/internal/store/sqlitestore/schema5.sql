-- Migration 5: clear finished work off the board (adb-done-leaves-board-card).
--
-- Two things had accumulated, for one reason: board_archived was only ever
-- written by the board's own archive button, never by `worklog done`.
--
-- Every epic closed from the CLI stayed on the in-flight lens. A finished
-- TICKET leaves that lens because closeOut sets phase=done, but an epic has
-- no phase — deliberately, its queues live on its children — so nothing
-- could remove one. The code fix that accompanies this migration makes the
-- epic path set the flag; this clears the ones already stuck.
--
-- The Done lens had also collected every ticket ever closed. That lens is
-- the review-then-dismiss queue and it is working as designed, so this is a
-- one-off sweep of a backlog the human asked to clear, NOT a rule. New
-- closes still land there to be dismissed by hand.
--
-- Deliberately narrow:
--
--   parent_id IS NULL — children are inert here. storeSnapshot skips any
--   ticket with a parent, and the child shape carries no archived key, so
--   setting it on the 51 child rows would be meaningless writes.
--
--   No touch of updated_at. That column is what the board reports as a
--   card's mtime, and its age badge, stale dot, recency sort and stale
--   count all read off it. Doing this through PutTicket would stamp all 50
--   rows and make every archived card report "just now" forever, which is
--   the reason this is a migration and not a loop over the store API.
--
-- Not journaled. board_archived is a journaled scalar everywhere else, so
-- this sweep is a gap in that trail on purpose: migrations have no journal
-- path, and inventing one for a single backfill is worse than saying so
-- here. The journal still shows every hand archive before and after.
--
-- Idempotent by construction: the predicate excludes rows already set.

UPDATE tickets
   SET board_archived = 1
 WHERE archived = 1
   AND board_tracked = 1
   AND board_archived = 0
   AND parent_id IS NULL;
