package cli

import (
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/storepath"
)

// Closing work has to remove it from the board, and the two shapes do it by
// different means. A ticket leaves because closeOut sets phase=done and the
// frontend reads that. An epic has no phase at all — deliberately, since its
// queues live on its children — so phase can never remove one, and
// BoardArchived is the only flag that can.
//
// Nothing set it. Every epic ever closed from the CLI stayed on the
// in-flight lens permanently; adb-one-store sat there until this was found.
// The frontend had already promised otherwise: counts.js says "an epic
// closes when the human archives it (`worklog done`)".
//
// These assert on the store rather than through rendersOwnCard, which
// reports "has a card" from BoardTracked and ParentID alone and would call
// an archived epic a live card.

// ticketAfterDone reads a ticket straight back out of the store.
func ticketAfterDone(t *testing.T, dir, slug string) *store.Ticket {
	t.Helper()
	wd, err := model.NewWorkdir(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, _, err := openStore(storeExisting, storepath.DB(wd.Root))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tk, err := s.TicketBySlug(slug)
	if err != nil {
		t.Fatalf("reading %s back: %v", slug, err)
	}
	return tk
}

// inFlight mirrors the frontend's Board-lens rule (counts.js): a card is
// in flight when it is neither archived nor phase=done. Duplicated here on
// purpose — the point is to assert against the rule the board actually
// applies, not against our own idea of it.
func inFlight(tk *store.Ticket) bool {
	return !tk.BoardArchived && tk.Phase != "done"
}

func TestDoneEpicLeavesTheBoard(t *testing.T) {
	dir := seedStore(t, &store.Ticket{
		Slug: "epic-a", Title: "An epic", Type: store.TypeEpic,
		State: store.StateActive, Section: store.SectionNow, BoardTracked: true,
	})

	if _, stderr := runCLI(t, "done", "epic-a", "--summary", "all four moves landed"); strings.Contains(stderr, "error") {
		t.Fatalf("done: %s", stderr)
	}

	tk := ticketAfterDone(t, dir, "epic-a")
	if !tk.Archived {
		t.Fatal("the epic was not archived in the corpus")
	}
	if !tk.BoardArchived {
		t.Error("a closed epic must leave the board; BoardArchived is the only flag that can remove one")
	}
	if inFlight(tk) {
		t.Error("the closed epic is still on the board's in-flight lens")
	}
}

// The epic must NOT pick up a phase on the way out. Giving it one would make
// it read as done on the wire, which counts.js explicitly does not want.
func TestDoneEpicHasNoPhase(t *testing.T) {
	dir := seedStore(t, &store.Ticket{
		Slug: "epic-b", Title: "Another epic", Type: store.TypeEpic,
		State: store.StateActive, Section: store.SectionNow, BoardTracked: true,
	})

	if _, stderr := runCLI(t, "done", "epic-b", "--summary", "done"); strings.Contains(stderr, "error") {
		t.Fatalf("done: %s", stderr)
	}

	if got := ticketAfterDone(t, dir, "epic-b").Phase; got != "" {
		t.Errorf("epic phase = %q, want empty — an epic has no phase of its own", got)
	}
}

// The counterpart, and the more important of the two: a closed TICKET must
// still land in the Done lens, not be archived out from under the human.
// That lens is the review-then-dismiss queue and the archive button is its
// gesture; archiving on close would empty it structurally and delete the
// gesture's only list surface.
func TestDoneTicketStaysInTheDoneLens(t *testing.T) {
	dir := seedStore(t, &store.Ticket{
		Slug: "tkt-a", Title: "A ticket", Type: store.TypeTicket,
		State: store.StateActive, Section: store.SectionNow, BoardTracked: true,
		Phase: "verify",
	})

	if _, stderr := runCLI(t, "done", "tkt-a", "--summary", "shipped"); strings.Contains(stderr, "error") {
		t.Fatalf("done: %s", stderr)
	}

	tk := ticketAfterDone(t, dir, "tkt-a")
	if tk.BoardArchived {
		t.Error("closing a ticket must not archive its card; the Done lens is where the human dismisses it")
	}
	if tk.Phase != "done" {
		t.Errorf("phase = %q, want done — that is what takes a ticket off the in-flight lens", tk.Phase)
	}
	if inFlight(tk) {
		t.Error("the closed ticket is still in flight")
	}
}
