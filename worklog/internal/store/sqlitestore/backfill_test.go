package sqlitestore

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// Migration 5 sweeps finished work off the board. The properties worth
// pinning are the ones a careless sweep would break: it must not move
// updated_at (the board's mtime, which the age badge, stale dot, recency
// sort and stale count all read off), it must leave children alone, and it
// must be safe to run twice.

// openAt opens a store at a fresh path, running every migration.
func openAt(t *testing.T) (*SQLite, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "worklog.db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, p
}

// rawSet writes board state straight through SQL, bypassing PutTicket so
// the fixture can hold an updated_at that the test controls.
func rawSet(t *testing.T, p, slug string, archived, tracked, boardArchived int, updated int64) {
	t.Helper()
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(
		`UPDATE tickets SET archived=?, board_tracked=?, board_archived=?, updated_at=? WHERE slug=?`,
		archived, tracked, boardArchived, updated, slug); err != nil {
		t.Fatal(err)
	}
}

func boardState(t *testing.T, p, slug string) (boardArchived int, updated int64) {
	t.Helper()
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.QueryRow(
		`SELECT board_archived, updated_at FROM tickets WHERE slug=?`, slug,
	).Scan(&boardArchived, &updated); err != nil {
		t.Fatal(err)
	}
	return
}

// applyBackfill runs migration 5's statement against an existing file, the
// way re-opening an already-migrated store would if the version allowed it.
func applyBackfill(t *testing.T, p string) int64 {
	t.Helper()
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	res, err := db.Exec(migration5)
	if err != nil {
		t.Fatal(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestBackfillClearsArchivedTopLevelWithoutMovingUpdatedAt(t *testing.T) {
	s, p := openAt(t)
	for _, slug := range []string{"stuck", "live"} {
		if err := s.PutTicket(&store.Ticket{
			Slug: slug, Title: slug, Type: store.TypeTicket, State: store.StatePending,
		}); err != nil {
			t.Fatal(err)
		}
	}
	const stamp int64 = 1_600_000_000
	rawSet(t, p, "stuck", 1, 1, 0, stamp) // archived + board-tracked: swept
	rawSet(t, p, "live", 0, 1, 0, stamp)  // not archived: left alone

	if n := applyBackfill(t, p); n != 1 {
		t.Fatalf("swept %d rows, want exactly the archived one", n)
	}

	got, updated := boardState(t, p, "stuck")
	if got != 1 {
		t.Error("an archived, board-tracked, top-level ticket was not swept off the board")
	}
	if updated != stamp {
		t.Errorf("updated_at moved: %d, want %d — that is the board's mtime, and moving it "+
			"makes every archived card report \"just now\"", updated, stamp)
	}

	if got, _ := boardState(t, p, "live"); got != 0 {
		t.Error("a ticket that is not archived was swept off the board")
	}
}

func TestBackfillLeavesChildrenAlone(t *testing.T) {
	s, p := openAt(t)
	epic := &store.Ticket{Slug: "ep", Title: "ep", Type: store.TypeEpic, State: store.StateActive}
	if err := s.PutTicket(epic); err != nil {
		t.Fatal(err)
	}
	if err := s.PutTicket(&store.Ticket{
		Slug: "kid", Title: "kid", Type: store.TypeTicket,
		State: store.StateDone, ParentID: epic.ID,
	}); err != nil {
		t.Fatal(err)
	}
	rawSet(t, p, "kid", 1, 1, 0, 1_600_000_000)

	applyBackfill(t, p)

	if got, _ := boardState(t, p, "kid"); got != 0 {
		t.Error("a child was swept; children render inside their epic and carry no archived flag, " +
			"so writing it is meaningless work")
	}
}

func TestBackfillIsIdempotent(t *testing.T) {
	s, p := openAt(t)
	if err := s.PutTicket(&store.Ticket{
		Slug: "one", Title: "one", Type: store.TypeTicket, State: store.StatePending,
	}); err != nil {
		t.Fatal(err)
	}
	rawSet(t, p, "one", 1, 1, 0, 1_600_000_000)

	if n := applyBackfill(t, p); n != 1 {
		t.Fatalf("first run swept %d rows, want 1", n)
	}
	if n := applyBackfill(t, p); n != 0 {
		t.Errorf("second run swept %d rows, want 0 — the predicate must exclude rows already set", n)
	}
}

// A hand archive that predates the sweep must survive it untouched, since
// the flag it set means the same thing.
func TestBackfillPreservesAnExistingArchive(t *testing.T) {
	s, p := openAt(t)
	if err := s.PutTicket(&store.Ticket{
		Slug: "dismissed", Title: "dismissed", Type: store.TypeTicket, State: store.StateDone,
	}); err != nil {
		t.Fatal(err)
	}
	const stamp int64 = 1_600_000_123
	rawSet(t, p, "dismissed", 1, 1, 1, stamp)

	applyBackfill(t, p)

	got, updated := boardState(t, p, "dismissed")
	if got != 1 || updated != stamp {
		t.Errorf("an already-archived row changed: board_archived=%d updated_at=%d", got, updated)
	}
}
