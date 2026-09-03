-- Migration 2: ticket ordering as data (adb-cutover M3, 2026-09-03).
-- Migration 1's own header states the rule -- "rank column for order, never
-- position-as-identity" -- and applies it to all nine sub-item tables but
-- not to tickets themselves, so ticket order could only come from
-- ORDER BY slug. That alphabetized ## Next, whose order is the human's
-- priority queue. Rank restores it as data: position in WORK.md while
-- live, position in the archive month file once archived.
--
-- Added as a migration rather than folded into migration 1 because
-- migrate seeds its working copy copy-forward from any existing db to
-- preserve ULID identity across re-runs; rewriting v1 would strand those
-- databases at user_version 1 without the column.

ALTER TABLE tickets ADD COLUMN rank INTEGER NOT NULL DEFAULT 0;

-- roster_rank is the same rule applied to the parent/child relation: a
-- child's position in its epic's roster. Distinct from rank, which is the
-- child's position in WORK.md or an archive month and says nothing about
-- its place in the roster. Without it Children() could only ORDER BY
-- slug, which alphabetized an archived epic's Children: list.
ALTER TABLE tickets ADD COLUMN roster_rank INTEGER NOT NULL DEFAULT 0;
