package sqlitestore

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// The board's sense of time is the row's own. It used to be taken from the
// rendered YAML file's mtime by the projection layer, which meant the
// renderer decided freshness while walking every ticket on every write
// (adb-retire-devboard-dir-2).

func pinClock(t *testing.T) func(time.Duration) {
	t.Helper()
	at := time.Unix(1_700_000_000, 0)
	prev := timeNow
	timeNow = func() time.Time { return at }
	t.Cleanup(func() { timeNow = prev })
	return func(d time.Duration) { at = at.Add(d) }
}

func openDB(t *testing.T) *SQLite {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "worklog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func putTicket(t *testing.T, s *SQLite, slug string, mutate func(*store.Ticket)) *store.Ticket {
	t.Helper()
	tk, err := s.TicketBySlug(slug)
	if err != nil {
		tk = &store.Ticket{Slug: slug, Title: "T", Type: store.TypeTicket,
			State: store.StateActive, Section: store.SectionNow, Repo: "r",
			BoardTracked: true, Phase: "intake"}
	}
	if mutate != nil {
		mutate(tk)
	}
	if err := s.PutTicket(tk); err != nil {
		t.Fatal(err)
	}
	got, err := s.TicketBySlug(slug)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// A write moves the row it wrote, and the creation stamp never moves
// again. That second half is the one an upsert gets wrong by default:
// `excluded.created_at` would overwrite it on every update.
func TestUpdatedAtMovesAndCreatedAtDoesNot(t *testing.T) {
	advance := pinClock(t)
	s := openDB(t)
	first := putTicket(t, s, "tkt", nil)
	if first.CreatedAt != first.UpdatedAt {
		t.Fatalf("a first insert disagrees with itself: %d vs %d", first.CreatedAt, first.UpdatedAt)
	}

	advance(time.Hour)
	second := putTicket(t, s, "tkt", func(tk *store.Ticket) { tk.Phase = "verify" })
	if second.CreatedAt != first.CreatedAt {
		t.Errorf("CreatedAt moved on update: %d -> %d", first.CreatedAt, second.CreatedAt)
	}
	if second.UpdatedAt != first.UpdatedAt+int64(time.Hour) {
		t.Errorf("UpdatedAt = %d, want the clock %d", second.UpdatedAt, first.UpdatedAt+int64(time.Hour))
	}
}

// A write moves the row it wrote and no other. The old mechanism could not
// say this: the renderer stamped from files it had just walked for every
// ticket, and only a byte comparison kept the rest of the board still.
func TestAWriteMovesOnlyItsOwnRow(t *testing.T) {
	advance := pinClock(t)
	s := openDB(t)
	putTicket(t, s, "one", nil)
	putTicket(t, s, "two", nil)
	before, err := s.TicketBySlug("two")
	if err != nil {
		t.Fatal(err)
	}

	advance(time.Hour)
	putTicket(t, s, "one", func(tk *store.Ticket) { tk.Phase = "verify" })

	after, err := s.TicketBySlug("two")
	if err != nil {
		t.Fatal(err)
	}
	if after.UpdatedAt != before.UpdatedAt {
		t.Errorf("writing one ticket moved another's stamp: %d -> %d", before.UpdatedAt, after.UpdatedAt)
	}
}

// A sub-item edit is a real change and moves the stamp. This is the case
// the journal could never have supplied: PutTicket journals scalars only,
// so a plan, scorecard or decision edit leaves no field-change row at all.
func TestSubItemEditsMoveUpdatedAt(t *testing.T) {
	advance := pinClock(t)
	s := openDB(t)
	before := putTicket(t, s, "tkt", nil)

	advance(time.Hour)
	after := putTicket(t, s, "tkt", func(tk *store.Ticket) {
		tk.PlanSteps = append(tk.PlanSteps, store.PlanStep{Text: "do it", State: "pending", Rank: 1})
	})
	if after.UpdatedAt == before.UpdatedAt {
		t.Error("a plan step did not move UpdatedAt")
	}
}

// The stamps the caller passes in are ignored, so an aggregate read before
// another process's write and written back after cannot drag the row's
// time backwards with it.
func TestCallerStampsAreIgnored(t *testing.T) {
	advance := pinClock(t)
	s := openDB(t)
	first := putTicket(t, s, "tkt", nil)

	advance(time.Hour)
	got := putTicket(t, s, "tkt", func(tk *store.Ticket) {
		tk.CreatedAt, tk.UpdatedAt = 1, 1
		tk.Phase = "verify" // a real change, so the write happens at all
	})
	if got.CreatedAt != first.CreatedAt {
		t.Errorf("a caller's CreatedAt was honoured: %d", got.CreatedAt)
	}
	if got.UpdatedAt != first.UpdatedAt+int64(time.Hour) {
		t.Errorf("UpdatedAt = %d, want the clock, not the caller's 1", got.UpdatedAt)
	}
}

// A write that changes nothing does not happen. Stale timestamps alone are
// not a change, so this is also the case above with the real edit removed.
func TestANoOpWriteIsNotAWrite(t *testing.T) {
	advance := pinClock(t)
	s := openDB(t)
	first := putTicket(t, s, "tkt", nil)

	for _, c := range []struct {
		name  string
		apply func(*store.Ticket)
	}{
		{"identical aggregate", func(*store.Ticket) {}},
		{"same value set again", func(tk *store.Ticket) { tk.Phase = "intake" }},
		{"stale caller stamps", func(tk *store.Ticket) { tk.CreatedAt, tk.UpdatedAt = 1, 1 }},
		{"dropped sub-item ids", func(tk *store.Ticket) {
			for i := range tk.PlanSteps {
				tk.PlanSteps[i].ID = ""
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			advance(time.Hour)
			got := putTicket(t, s, "tkt", c.apply)
			if got.UpdatedAt != first.UpdatedAt {
				t.Errorf("UpdatedAt moved on a no-op write: %d -> %d", first.UpdatedAt, got.UpdatedAt)
			}
		})
	}
}

// And the guard is not so eager that it swallows a real change that
// happens to touch only a sub-item.
func TestTheGuardDoesNotSwallowARealChange(t *testing.T) {
	advance := pinClock(t)
	s := openDB(t)
	before := putTicket(t, s, "tkt", nil)
	advance(time.Hour)
	after := putTicket(t, s, "tkt", func(tk *store.Ticket) {
		tk.Scorecard = append(tk.Scorecard, store.ScoreItem{Text: "c1", Verify: "go test", Status: "pending"})
	})
	if after.UpdatedAt == before.UpdatedAt {
		t.Error("a new scorecard row was mistaken for a no-op")
	}
}

// The rename carries existing values across, so no card loses its age on
// upgrade: a database at the previous version opens, migrates, and its
// board_rendered_at values are readable as updated_at.
func TestMigrationCarriesTheOldStampAcross(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worklog.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	tk := &store.Ticket{Slug: "old", Title: "T", Type: store.TypeTicket,
		State: store.StateActive, Section: store.SectionNow, Repo: "r", BoardTracked: true}
	if err := s.PutTicket(tk); err != nil {
		t.Fatal(err)
	}
	// Stand in for a value written before the rename.
	if _, err := s.db.Exec("UPDATE tickets SET updated_at = ? WHERE slug = ?", 4242, "old"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.TicketBySlug("old")
	if err != nil {
		t.Fatal(err)
	}
	if got.UpdatedAt != 4242 {
		t.Errorf("UpdatedAt after reopen = %d, want the stored 4242", got.UpdatedAt)
	}
}
