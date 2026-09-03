package sqlitestore

import (
	"path/filepath"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/storetest"
)

func TestConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store {
		s, err := Open(filepath.Join(t.TempDir(), "worklog.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	})
}

// TestReopen: the aggregate survives a close/reopen cycle byte-for-byte —
// memstore can't prove durability, so this pin lives here.
func TestReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worklog.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	pr := "https://example.com/pr/9"
	tk := &store.Ticket{
		Slug: "durable", Title: "Durable", Type: store.TypeSpike,
		State: store.StateActive, Section: store.SectionNow,
		PR: &pr, Tags: []string{"a", "b"}, Phase: "research",
		Extra: map[string]any{"custom": "kept"},
		PlanSteps: []store.PlanStep{{Text: "step", State: "pending",
			Extra: map[string]any{"note": "x"}}},
		NoteEntries: []store.NoteEntry{{Stamp: "2026-09-02 19:19",
			Body: "## a content heading inside the body\nsurvives"}},
	}
	if err := s.PutTicket(tk); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, err := s2.TicketBySlug("durable")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != tk.ID || *got.PR != pr || got.Extra["custom"] != "kept" ||
		len(got.PlanSteps) != 1 || got.PlanSteps[0].ID != tk.PlanSteps[0].ID ||
		got.NoteEntries[0].Body != tk.NoteEntries[0].Body {
		t.Errorf("reopen lost data: %+v", got)
	}
	j, err := s2.Journal(got.ID)
	if err != nil || len(j) == 0 {
		t.Errorf("journal lost across reopen: %v %v", j, err)
	}
}

// TestMigrationVersion: user_version lands at the migration count.
func TestMigrationVersion(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "worklog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != len(migrations) {
		t.Errorf("user_version = %d, want %d", v, len(migrations))
	}
}
