package projection

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
)

// The board's archive write used to rename the task file itself and then ask
// the store to record it, so a failed store write left disk and store
// disagreeing and every later write refused (adb-archive-store-desync). The
// store owns placement now, which puts two obligations on render: it must
// clear the path a ticket just left, and it must never touch a file the store
// has no ticket for.

// boardTicket is the smallest ticket that renders to a board file.
func boardTicket(slug, repo string, archived bool) *store.Ticket {
	return &store.Ticket{
		ID: store.ID(slug), Slug: slug, Title: "A task", Repo: repo,
		Type: store.TypeTicket, State: store.StatePending,
		BoardTracked: true, BoardArchived: archived,
	}
}

func liveP(root, repo, slug string) string {
	return filepath.Join(root, "devboard", repo, slug+".yaml")
}
func archivedP(root, repo, slug string) string {
	return filepath.Join(root, "devboard", repo, "_archive", slug+".yaml")
}

func exists(t *testing.T, p string) bool {
	t.Helper()
	_, err := os.Stat(p)
	return err == nil
}

// TestRenderClearsTheStalePath: flipping BoardArchived moves the file, and
// leaves exactly one copy. Two copies is how the board shows one task twice.
func TestRenderClearsTheStalePath(t *testing.T) {
	s := memstore.New()
	tk := boardTicket("kid", "r", false)
	if err := s.PutTicket(tk); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := RenderAll(s, dir); err != nil {
		t.Fatal(err)
	}
	if !exists(t, liveP(dir, "r", "kid")) {
		t.Fatal("first render did not write the live file")
	}

	tk.BoardArchived = true
	if err := s.PutTicket(tk); err != nil {
		t.Fatal(err)
	}
	if err := RenderAll(s, dir); err != nil {
		t.Fatal(err)
	}
	if !exists(t, archivedP(dir, "r", "kid")) {
		t.Error("archived render did not write into _archive/")
	}
	if exists(t, liveP(dir, "r", "kid")) {
		t.Error("the live file survived the move; the board would show it twice")
	}

	// And back again, so un-archiving is symmetric.
	tk.BoardArchived = false
	if err := s.PutTicket(tk); err != nil {
		t.Fatal(err)
	}
	if err := RenderAll(s, dir); err != nil {
		t.Fatal(err)
	}
	if !exists(t, liveP(dir, "r", "kid")) || exists(t, archivedP(dir, "r", "kid")) {
		t.Error("un-archiving left the file in the wrong place, or in both")
	}
}

// TestRenderNeverDeletesAnUnownedFile is the blast-radius guard, and the
// reason the prune is "the stale sibling of what I just wrote" rather than
// "anything I did not write". Hand-dropped producer files are a supported
// input (devboard/README.md) and the store knows nothing about them, so the
// broad rule would delete every one of them.
func TestRenderNeverDeletesAnUnownedFile(t *testing.T) {
	s := memstore.New()
	if err := s.PutTicket(boardTicket("owned", "r", true)); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()

	// A bare file at each path, in the same repo dir the render touches, and
	// one whose name matches a ticket the store has but does not board-track.
	untracked := boardTicket("shadow", "r", false)
	untracked.BoardTracked = false
	if err := s.PutTicket(untracked); err != nil {
		t.Fatal(err)
	}
	strays := []string{
		liveP(dir, "r", "hand-dropped"),
		archivedP(dir, "r", "hand-archived"),
		liveP(dir, "r", "shadow"),
	}
	for _, p := range strays {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("schema: 1\ntitle: hand written\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	for i := 0; i < 2; i++ { // twice: a second render must be a fixpoint too
		if err := RenderAll(s, dir); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range strays {
		if !exists(t, p) {
			t.Errorf("render deleted a file the store does not own: %s", p)
		}
		body, err := os.ReadFile(p)
		if err != nil || string(body) != "schema: 1\ntitle: hand written\n" {
			t.Errorf("render rewrote an unowned file: %s (%v)", p, err)
		}
	}
}

// TestEditedInReconcilesAMissedMove: a file byte-identical to its render but
// sitting at the other path is a move the store missed, not a hand edit.
// Reporting it as an edit is what made one board click refuse every
// subsequent write.
func TestEditedInReconcilesAMissedMove(t *testing.T) {
	s := memstore.New()
	if err := s.PutTicket(boardTicket("kid", "r", false)); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := RenderAll(s, dir); err != nil {
		t.Fatal(err)
	}

	// Exactly the state the old handler produced: file renamed into _archive/,
	// store still saying live.
	body, err := os.ReadFile(liveP(dir, "r", "kid"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(archivedP(dir, "r", "kid")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivedP(dir, "r", "kid"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(liveP(dir, "r", "kid")); err != nil {
		t.Fatal(err)
	}

	edited, err := EditedFiles(s, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(edited) != 0 {
		t.Errorf("a missed move was reported as a hand edit: %v", edited)
	}
}

// The self-healing must not blunt the guard: same displacement, different
// bytes, is still someone's edit and still has to refuse.
func TestEditedInStillRefusesARealEdit(t *testing.T) {
	s := memstore.New()
	if err := s.PutTicket(boardTicket("kid", "r", false)); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := RenderAll(s, dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(archivedP(dir, "r", "kid")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivedP(dir, "r", "kid"), []byte("title: edited by hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(liveP(dir, "r", "kid")); err != nil {
		t.Fatal(err)
	}

	edited, err := EditedFiles(s, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(edited) != 1 {
		t.Fatalf("a real edit at the sibling path was not caught: %v", edited)
	}

	// A plain in-place edit is unchanged too.
	dir2 := t.TempDir()
	if err := RenderAll(s, dir2); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(liveP(dir2, "r", "kid"), []byte("title: nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if edited, err = EditedFiles(s, dir2); err != nil || len(edited) != 1 {
		t.Fatalf("in-place hand edit no longer flagged: %v (err %v)", edited, err)
	}
}

// A rendered file that is simply gone is still a deletion, not a move.
func TestEditedInStillCatchesADeletion(t *testing.T) {
	s := memstore.New()
	if err := s.PutTicket(boardTicket("kid", "r", false)); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := RenderAll(s, dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(liveP(dir, "r", "kid")); err != nil {
		t.Fatal(err)
	}
	edited, err := EditedFiles(s, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(edited) != 1 {
		t.Errorf("a deleted projection was not reported: %v", edited)
	}
}
