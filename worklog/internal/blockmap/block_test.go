package blockmap

import (
	"reflect"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/parse"
	"github.com/prestontallen/ai-devboard/worklog/internal/projection"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

func ptr(s string) *string { return &s }

// corpus covers every field the renderer can emit, so the round trip below
// is a claim about the whole correspondence rather than about one ticket.
func corpus() []*store.Ticket {
	// The epic's slug is uppercase so that its children's Parent lines
	// exercise the same defensive lowering the parser applies. The store's
	// contract says slugs are lowercase; this pins both encodings to one
	// answer if that ever slips.
	epic := &store.Ticket{
		ID: "E1", Slug: "EPIC-ONE", Title: "An epic", Type: store.TypeEpic,
		State: store.StateActive, Section: store.SectionNext, Rank: 0,
		Repo: "ai-devboard", Tags: []string{"store", "architecture"},
		NotesPreamble: "# An epic\n",
	}
	return []*store.Ticket{
		epic,
		// Added to the roster second but ranked first in Now: roster order
		// and section order are different orderings of the same children.
		{ID: "C2", Slug: "child-two", Title: "Second child", Type: store.TypeTicket,
			State: store.StateActive, Section: store.SectionNow, Rank: 0,
			ParentID: "E1", RosterRank: 1, Started: "2026-05-15"},
		{ID: "C1", Slug: "child-one", Title: "First child", Type: store.TypeSpike,
			State: store.StateActive, Section: store.SectionNow, Rank: 1,
			ParentID: "E1", RosterRank: 0, Started: "2026-05-14"},
		// A pending child: on the roster's books but not an active child,
		// and it renders no block of its own.
		{ID: "C3", Slug: "child-three", Title: "Third child", Type: store.TypeTicket,
			State: store.StatePending, ParentID: "E1", RosterRank: 2},
		{ID: "T1", Slug: "full-ticket", Title: "Every field set", Type: store.TypeChore,
			State: store.StatePending, Section: store.SectionNext, Rank: 1,
			Repo: "ai-devboard", Tags: []string{"one", "two"},
			Started: "2026-05-01", WaitingSince: "2026-05-02", PR: ptr("https://example/pr/1"),
			Source: "jira", Files: []string{"a.go", "b.go"},
			Acceptance: "it works", Status: "in review", PlanText: "do the thing",
			NotesPreamble: "# Every field set\n",
			Links: []store.Link{
				{Kind: store.LinkRef, Label: "design", URL: "https://example/design"},
				{Kind: store.LinkRef, Label: "", URL: "https://example/unlabelled"},
			}},
		// Untidy list fields: the renderer comma-joins and the parser
		// splits, so a blank member or stray whitespace cannot survive
		// either way. Reachable, since tags and files are free-form CSV
		// from the command line.
		{ID: "T5", Slug: "untidy-lists", Title: "Untidy", Type: store.TypeTicket,
			State: store.StatePending, Section: store.SectionNext, Rank: 2,
			Tags: []string{"  spaced  ", "", "kept"}, Files: []string{"x.go", "   "}},
		// An uppercase slug on the block itself, as distinct from on its
		// parent above.
		{ID: "T6", Slug: "SHOUTY-SLUG", Title: "Shouty", Type: store.TypeTicket,
			State: store.StateActive, Section: store.SectionNow, Rank: 2,
			ParentID: "E1", RosterRank: 3, Started: "2026-05-20"},
		// Nothing but a title and an id: every optional line absent.
		{ID: "T2", Slug: "bare-ticket", Title: "Bare", Type: store.TypeTicket,
			State: store.StatePending, Section: store.SectionSomeday, Rank: 1},
		// An empty-but-present PR line, which the store distinguishes from
		// an absent one and the block cannot.
		{ID: "T3", Slug: "empty-pr", Title: "Empty PR", Type: store.TypeTicket,
			State: store.StatePending, Section: store.SectionSomeday, Rank: 2, PR: ptr("")},
		// No type at all. The renderer omits the line for anything equal to
		// "ticket" and `add` drops an empty value, so an unset type renders
		// nothing and parses back as a ticket; the mapping has to reach the
		// same answer without the round trip.
		{ID: "T7", Slug: "typeless", Title: "No type", Type: "",
			State: store.StatePending, Section: store.SectionSomeday, Rank: 0},
		// Archived: renders nowhere, so it must not reach a section either.
		{ID: "T4", Slug: "gone", Title: "Archived", Type: store.TypeTicket,
			State: store.StateDone, Archived: true, ArchiveMonth: "2026-05"},
	}
}

// The correspondence is defined by the WORK.md renderer, so this asserts
// it against the renderer rather than against a hand-written expectation:
// build a block from the ticket, render the same ticket to WORK.md, parse
// it back, and require the two to agree. Either encoding drifting fails
// here.
func TestBlockMatchesRenderedBlock(t *testing.T) {
	tickets := corpus()
	doc, err := parse.Bytes("WORK.md", projection.WorkMD(tickets))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[store.ID]*store.Ticket{}
	for _, tk := range tickets {
		byID[tk.ID] = tk
	}

	parsed := map[string]model.Block{}
	for _, sec := range doc.Sections {
		for _, b := range sec.Blocks {
			b.StartLine, b.EndLine = 0, 0 // positions in a file blockmap never writes
			parsed[b.ID] = b
		}
	}

	for _, tk := range tickets {
		// The parser lowercases every id, so the corpus's one uppercase
		// slug is looked up the way it landed, not the way it was written.
		slug := strings.ToLower(tk.Slug)
		got, want := Block(tk, byID, tickets), parsed[slug]
		if tk.Archived {
			if _, rendered := parsed[slug]; rendered {
				t.Errorf("%s is archived but rendered into WORK.md", tk.Slug)
			}
			continue
		}
		if tk.ParentID != "" && tk.State != store.StateActive {
			continue // a pending child renders on no page at all
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s:\n built  %#v\n parsed %#v", slug, got, want)
		}
	}
}

func TestBlocksAreRankOrderedAndSectionScoped(t *testing.T) {
	tickets := corpus()

	var next []string
	for _, b := range Blocks(tickets, store.SectionNext) {
		next = append(next, b.ID)
	}
	if want := []string{"epic-one", "full-ticket", "untidy-lists"}; !reflect.DeepEqual(next, want) {
		t.Errorf("Next = %v, want %v", next, want)
	}

	var someday []string
	for _, b := range Blocks(tickets, store.SectionSomeday) {
		someday = append(someday, b.ID)
	}
	if want := []string{"typeless", "bare-ticket", "empty-pr"}; !reflect.DeepEqual(someday, want) {
		t.Errorf("Someday = %v, want %v", someday, want)
	}

	// Blocks states its own ordering rather than inheriting the caller's,
	// so a shuffled input comes back in the same document order.
	shuffled := append([]*store.Ticket(nil), tickets...)
	for i, j := 0, len(shuffled)-1; i < j; i, j = i+1, j-1 {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}
	var reshuffled []string
	for _, b := range Blocks(shuffled, store.SectionNext) {
		reshuffled = append(reshuffled, b.ID)
	}
	if !reflect.DeepEqual(reshuffled, next) {
		t.Errorf("reversed input gave %v, want %v", reshuffled, next)
	}

	if got := Blocks(tickets, store.SectionBlocked); len(got) != 0 {
		t.Errorf("empty section = %v, want no blocks", got)
	}
	// An empty section must still marshal as [] rather than null: the
	// backlog lens distinguishes "no items" from "no backlog reported".
	if Blocks(tickets, store.SectionBlocked) == nil {
		t.Error("empty section returned a nil slice")
	}
}

// The roster is the order children were added to the epic, which is not
// the order they sit in Now.
func TestEpicRosterIsInAddOrder(t *testing.T) {
	tickets := corpus()
	byID := map[store.ID]*store.Ticket{}
	for _, tk := range tickets {
		byID[tk.ID] = tk
	}
	got := Block(tickets[0], byID, tickets).ActiveChildren
	if want := []string{"child-one", "child-two", "shouty-slug"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ActiveChildren = %v, want %v", got, want)
	}
}

// Section membership is not the only thing that keeps an archived ticket
// off the front page. Its section is cleared on archive, so this state
// should not occur; the filter is what makes that a guarantee rather than
// a consequence.
func TestArchivedNeverReachesASection(t *testing.T) {
	stale := &store.Ticket{ID: "A", Slug: "stale", Title: "Stale", Type: store.TypeTicket,
		State: store.StateDone, Archived: true, Section: store.SectionNext}
	if got := Blocks([]*store.Ticket{stale}, store.SectionNext); len(got) != 0 {
		t.Errorf("archived ticket with a stale section rendered as %v", got)
	}
}

// A non-epic carries no roster, and an epic whose children are all done
// carries an empty one rather than a list of ghosts.
func TestRosterAbsentWhereItHasNoMeaning(t *testing.T) {
	byID := map[store.ID]*store.Ticket{}
	epic := &store.Ticket{ID: "E", Slug: "e", Type: store.TypeEpic, State: store.StateActive}
	kid := &store.Ticket{ID: "K", Slug: "k", Type: store.TypeTicket, State: store.StateDone, ParentID: "E"}
	all := []*store.Ticket{epic, kid}
	for _, t := range all {
		byID[t.ID] = t
	}
	if got := Block(epic, byID, all).ActiveChildren; got != nil {
		t.Errorf("epic with only done children = %v, want nil", got)
	}
	if got := Block(kid, byID, all).ActiveChildren; got != nil {
		t.Errorf("non-epic = %v, want nil", got)
	}
}
