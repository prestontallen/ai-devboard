package projection

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
)

func stampOf(t *testing.T, s store.Store, slug string) int64 {
	t.Helper()
	got, err := s.TicketBySlug(slug)
	if err != nil {
		t.Fatalf("TicketBySlug(%s): %v", slug, err)
	}
	return got.BoardRenderedAt
}

// TestRenderToStampsBoardMtime: after RenderTo, a board-tracked ticket's
// BoardRenderedAt equals its rendered file's mtime — the invariant that
// makes a store-served /api/tasks mtime equal the file-served one. A
// byte-identical re-render moves neither; a content change moves both
// together (criterion 4's flow half: a sub-item write advances the stamp).
func TestRenderToStampsBoardMtime(t *testing.T) {
	for name, s := range impls(t) {
		t.Run(name, func(t *testing.T) {
			if err := s.PutTicket(boardTicket("s1", "r1", false)); err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			l := SingleRoot(root)
			file := filepath.Join(root, "devboard", "r1", "s1.yaml")

			if err := RenderTo(s, l); err != nil {
				t.Fatal(err)
			}
			st, err := os.Stat(file)
			if err != nil {
				t.Fatal(err)
			}
			if got := stampOf(t, s, "s1"); got != st.ModTime().UnixNano() {
				t.Fatalf("stamp = %d, want file mtime %d", got, st.ModTime().UnixNano())
			}

			// Byte-identical re-render: neither file nor stamp moves.
			before := stampOf(t, s, "s1")
			if err := RenderTo(s, l); err != nil {
				t.Fatal(err)
			}
			st2, _ := os.Stat(file)
			if !st2.ModTime().Equal(st.ModTime()) {
				t.Fatalf("no-op re-render touched the file: %v -> %v", st.ModTime(), st2.ModTime())
			}
			if got := stampOf(t, s, "s1"); got != before {
				t.Fatalf("no-op re-render moved the stamp: %d -> %d", before, got)
			}

			// A sub-item write changes the rendered bytes; the stamp follows
			// the new mtime.
			tk, err := s.TicketBySlug("s1")
			if err != nil {
				t.Fatal(err)
			}
			tk.PlanSteps = append(tk.PlanSteps, store.PlanStep{Text: "one", State: "pending", Rank: 1})
			if err := s.PutTicket(tk); err != nil {
				t.Fatal(err)
			}
			if err := RenderTo(s, l); err != nil {
				t.Fatal(err)
			}
			st3, err := os.Stat(file)
			if err != nil {
				t.Fatal(err)
			}
			if got := stampOf(t, s, "s1"); got != st3.ModTime().UnixNano() {
				t.Fatalf("stamp after change = %d, want file mtime %d", got, st3.ModTime().UnixNano())
			}
		})
	}
}

// TestRenderToBackfillsZeroStamp: a ticket from before the column existed
// (stamp 0, file already on disk and byte-identical) picks up the file's
// mtime on its next write-through render, without the file being rewritten.
func TestRenderToBackfillsZeroStamp(t *testing.T) {
	s := memstore.New()
	if err := s.PutTicket(boardTicket("s1", "r1", false)); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	l := SingleRoot(root)
	if err := RenderTo(s, l); err != nil {
		t.Fatal(err)
	}
	tk, err := s.TicketBySlug("s1")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TouchBoardRendered(tk.ID, 0); err != nil { // pre-column state
		t.Fatal(err)
	}
	if err := RenderTo(s, l); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(root, "devboard", "r1", "s1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := stampOf(t, s, "s1"); got != st.ModTime().UnixNano() {
		t.Fatalf("backfilled stamp = %d, want file mtime %d", got, st.ModTime().UnixNano())
	}
}

// TestRenderAllDoesNotStamp: RenderAll is the scratch-dir variant — verify
// renders into a temp dir for comparison, and a read-only command must not
// write the store or adopt stamps from scratch-file mtimes.
func TestRenderAllDoesNotStamp(t *testing.T) {
	s := memstore.New()
	if err := s.PutTicket(boardTicket("s1", "r1", false)); err != nil {
		t.Fatal(err)
	}
	if err := RenderAll(s, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if got := stampOf(t, s, "s1"); got != 0 {
		t.Fatalf("RenderAll stamped BoardRenderedAt = %d, want 0", got)
	}
}

// TestBoardRenderedAtInvisibleInRender: the stamp reaches no projection
// output (criterion 5) — otherwise every stamp would dirty a projection,
// EditedIn would flag it, and every write verb would refuse.
func TestBoardRenderedAtInvisibleInRender(t *testing.T) {
	s := memstore.New()
	if err := s.PutTicket(boardTicket("s1", "r1", false)); err != nil {
		t.Fatal(err)
	}
	before, err := Render(s)
	if err != nil {
		t.Fatal(err)
	}
	tk, err := s.TicketBySlug("s1")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TouchBoardRendered(tk.ID, 12345); err != nil {
		t.Fatal(err)
	}
	after, err := Render(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatalf("stamp changed the projection set: %d files -> %d", len(before), len(after))
	}
	for rel, want := range before {
		if !bytes.Equal(after[rel], want) {
			t.Fatalf("stamp leaked into projection %s", rel)
		}
	}

	// And the write-through form: a RenderTo (which stamps) leaves nothing
	// EditedIn would refuse on.
	root := t.TempDir()
	if err := RenderTo(s, SingleRoot(root)); err != nil {
		t.Fatal(err)
	}
	edited, err := EditedFiles(s, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(edited) != 0 {
		t.Fatalf("RenderTo left self-inflicted drift: %v", edited)
	}
}
