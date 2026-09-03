// Package storetest is the Store conformance suite. Every implementation
// runs the same battery — this is the mechanism behind the swappability
// criterion: an implementation passes all of it or it isn't a Store.
package storetest

import (
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// Run executes the conformance suite against a fresh Store per subtest.
func Run(t *testing.T, open func(t *testing.T) store.Store) {
	t.Helper()
	t.Run("SubItemIdentityStableAcrossRemove", func(t *testing.T) { testItemIdentity(t, open(t)) })
	t.Run("SlugReservedAcrossHistory", func(t *testing.T) { testSlugReserved(t, open(t)) })
	t.Run("SlugCaseInsensitive", func(t *testing.T) { testSlugNoCase(t, open(t)) })
	t.Run("DecisionsDedupe", func(t *testing.T) { testDecisionDedupe(t, open(t)) })
	t.Run("JournalRecordsDiffs", func(t *testing.T) { testJournal(t, open(t)) })
	t.Run("ValidationRefusals", func(t *testing.T) { testValidation(t, open(t)) })
	t.Run("PRAbsentVsEmpty", func(t *testing.T) { testPRNullable(t, open(t)) })
	t.Run("FeedbackSameSecondDistinct", func(t *testing.T) { testFeedback(t, open(t)) })
	t.Run("NotFoundSentinel", func(t *testing.T) { testNotFound(t, open(t)) })
	t.Run("ChildrenSingleRelation", func(t *testing.T) { testChildren(t, open(t)) })
}

func base(slug string) *store.Ticket {
	return &store.Ticket{
		Slug: slug, Title: "T " + slug, Type: store.TypeTicket,
		State: store.StatePending, Section: store.SectionNext,
	}
}

func put(t *testing.T, s store.Store, tk *store.Ticket) *store.Ticket {
	t.Helper()
	if err := s.PutTicket(tk); err != nil {
		t.Fatalf("PutTicket(%s): %v", tk.Slug, err)
	}
	got, err := s.TicketBySlug(tk.Slug)
	if err != nil {
		t.Fatalf("TicketBySlug(%s): %v", tk.Slug, err)
	}
	return got
}

func testItemIdentity(t *testing.T, s store.Store) {
	tk := base("items")
	tk.Scorecard = []store.ScoreItem{
		{Text: "one", Status: "pending"},
		{Text: "two", Status: "pending"},
		{Text: "three", Status: "pending"},
	}
	got := put(t, s, tk)
	if len(got.Scorecard) != 3 {
		t.Fatalf("want 3 items, got %d", len(got.Scorecard))
	}
	for i, it := range got.Scorecard {
		if it.ID == "" {
			t.Fatalf("item %d has no ULID", i)
		}
	}
	idTwo, idThree := got.Scorecard[1].ID, got.Scorecard[2].ID

	// Remove the first item; survivors keep their IDs (the adb-task-item-ids bug).
	got.Scorecard = got.Scorecard[1:]
	got = put(t, s, got)
	if len(got.Scorecard) != 2 {
		t.Fatalf("want 2 items after remove, got %d", len(got.Scorecard))
	}
	if got.Scorecard[0].ID != idTwo || got.Scorecard[1].ID != idThree {
		t.Errorf("survivor identity changed: %s/%s vs %s/%s",
			got.Scorecard[0].ID, got.Scorecard[1].ID, idTwo, idThree)
	}
}

func testSlugReserved(t *testing.T, s store.Store) {
	dead := base("reused")
	dead.State = store.StateDone
	dead.Archived = true
	dead.Section = ""
	dead.Completed = "2026-09-01"
	dead.ArchiveMonth = "2026-09"
	put(t, s, dead)

	if err := s.PutTicket(base("reused")); err == nil {
		t.Error("archived slug was reusable; slugs are reserved across history")
	}
}

func testSlugNoCase(t *testing.T, s store.Store) {
	put(t, s, base("mixed-case"))
	got, err := s.TicketBySlug("MIXED-Case")
	if err != nil || got.Slug != "mixed-case" {
		t.Errorf("case-insensitive lookup failed: %v", err)
	}
	up := base("Mixed-CASE")
	if err := s.PutTicket(up); err == nil {
		t.Error("case-variant slug accepted as a distinct ticket")
	}
}

func testDecisionDedupe(t *testing.T, s store.Store) {
	tk := base("decides")
	tk.Decisions = []store.Decision{
		{What: "use X", Why: "simpler"},
		{What: "use X", Why: "simpler"},
		{What: "use X", Why: "different why"},
	}
	got := put(t, s, tk)
	if len(got.Decisions) != 2 {
		t.Errorf("want 2 decisions after dedupe, got %d", len(got.Decisions))
	}
}

func testJournal(t *testing.T, s store.Store) {
	tk := base("journaled")
	got := put(t, s, tk)
	got.Phase = "contract"
	got.State = store.StateActive
	got = put(t, s, got)

	j, err := s.Journal(got.ID)
	if err != nil {
		t.Fatal(err)
	}
	var phases, states int
	for _, ch := range j {
		switch ch.Field {
		case "phase":
			phases++
			if ch.New != "contract" {
				t.Errorf("phase journal: %+v", ch)
			}
		case "state":
			states++
		}
	}
	if phases != 1 || states < 1 {
		t.Errorf("journal missing rows: %+v", j)
	}
}

func testValidation(t *testing.T, s store.Store) {
	bad := base("bad-phase")
	bad.Phase = "implement" // the retired alias — canonical vocabulary only
	if err := s.PutTicket(bad); err == nil {
		t.Error("retired phase alias accepted")
	}
	twoPR := base("two-prs")
	twoPR.Links = []store.Link{
		{Kind: store.LinkPR, URL: "https://x/1"},
		{Kind: store.LinkPR, URL: "https://x/2"},
	}
	if err := s.PutTicket(twoPR); err == nil || !strings.Contains(err.Error(), "pr") {
		t.Errorf("second pr-kind link accepted: %v", err)
	}
	// Slug-less quick-capture entities are legal: identity is the ULID,
	// and two of them coexist without colliding.
	a, b := base(""), base("")
	a.Title, b.Title = "bare one", "bare two"
	if err := s.PutTicket(a); err != nil {
		t.Errorf("slug-less ticket refused: %v", err)
	}
	if err := s.PutTicket(b); err != nil {
		t.Errorf("second slug-less ticket refused: %v", err)
	}
	if got, err := s.Ticket(a.ID); err != nil || got.Title != "bare one" {
		t.Errorf("slug-less ticket not ID-addressable: %v", err)
	}
	if _, err := s.TicketBySlug(""); !store.IsNotFound(err) {
		t.Errorf("empty-slug lookup must miss, got %v", err)
	}
}

func testPRNullable(t *testing.T, s store.Store) {
	absent := put(t, s, base("pr-absent"))
	if absent.PR != nil {
		t.Error("PR should round-trip as absent (nil)")
	}
	empty := base("pr-empty")
	v := ""
	empty.PR = &v
	got := put(t, s, empty)
	if got.PR == nil || *got.PR != "" {
		t.Error("present-but-empty PR line lost its distinction from absent")
	}
}

func testFeedback(t *testing.T, s store.Store) {
	a := &store.FeedbackEntry{Seconds: 1000, Signal: "tui-error", Trigger: "first"}
	b := &store.FeedbackEntry{Seconds: 1000, Signal: "profanity", Trigger: "second"}
	if err := s.PutFeedback(a); err != nil {
		t.Fatal(err)
	}
	if err := s.PutFeedback(b); err != nil {
		t.Fatal(err)
	}
	all, err := s.Feedback()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].ID == all[1].ID {
		t.Errorf("same-second entries must be distinct by ULID: %+v", all)
	}
}

func testNotFound(t *testing.T, s store.Store) {
	_, err := s.TicketBySlug("ghost")
	if !store.IsNotFound(err) {
		t.Errorf("want NotFound sentinel, got %v", err)
	}
	_, err = s.Ticket(store.ID("01INVALIDULIDVALUE0000000X"))
	if !store.IsNotFound(err) {
		t.Errorf("want NotFound sentinel, got %v", err)
	}
}

func testChildren(t *testing.T, s store.Store) {
	epic := base("an-epic")
	epic.Type = store.TypeEpic
	epic = put(t, s, epic)

	for _, slug := range []string{"kid-b", "kid-a"} {
		kid := base(slug)
		kid.ParentID = epic.ID
		put(t, s, kid)
	}
	kids, err := s.Children(epic.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != 2 {
		t.Fatalf("want 2 children, got %d", len(kids))
	}
	for _, k := range kids {
		if k.ParentID != epic.ID {
			t.Errorf("child %s parent mismatch", k.Slug)
		}
	}
}
