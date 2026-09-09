package cli

import (
	"github.com/prestontallen/ai-devboard/worklog/internal/projection"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/sqlitestore"
	"github.com/prestontallen/ai-devboard/worklog/internal/storepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// The store-backed done path had no test at all, which is how it lost its
// close-out when adb-cutover M4 retired devboard.OnDone: nothing asserted
// that closing a ticket finishes it. These are that assertion.

func TestCloseOutFinishesPhaseAndQueues(t *testing.T) {
	tk := &store.Ticket{
		Slug:  "tkt",
		Phase: "verify",
		NeedsYou: []store.NeedsItem{
			{Text: "commit approval", Type: "checkpoint"},
		},
		WaitingOn: []store.WaitingItem{
			{Text: "can platform raise the limit?", Who: "platform", Asked: "2026-09-01"},
			{Text: "second question", Who: "someone"},
		},
		Decisions: []store.Decision{{What: "an earlier decision", When: "2026-09-01"}},
	}

	closeOut(tk, "2026-09-05")

	if tk.Phase != "done" {
		t.Errorf("phase = %q, want done — a closed ticket that keeps its phase sits on the board's in-flight grid forever", tk.Phase)
	}
	if len(tk.NeedsYou) != 0 {
		t.Errorf("needs_you = %+v, want empty", tk.NeedsYou)
	}
	if len(tk.WaitingOn) != 0 {
		t.Errorf("waiting_on = %+v, want empty", tk.WaitingOn)
	}

	// The open questions must survive as a record, not vanish.
	if len(tk.Decisions) != 3 {
		t.Fatalf("decisions = %d, want 3 (1 pre-existing + 2 closed-out)", len(tk.Decisions))
	}
	if tk.Decisions[0].What != "an earlier decision" {
		t.Errorf("existing decision was disturbed: %+v", tk.Decisions[0])
	}
	for _, d := range tk.Decisions[1:] {
		if !strings.HasPrefix(d.What, "unanswered at close: ") {
			t.Errorf("decision = %q, want an unanswered-at-close record", d.What)
		}
		if d.When != "2026-09-05" {
			t.Errorf("decision date = %q, want the completion date", d.When)
		}
	}
	if !strings.Contains(tk.Decisions[1].What, "platform") {
		t.Errorf("decision must name who owed the answer: %q", tk.Decisions[1].What)
	}
}

func TestCloseOutOnAnEmptyQueueAddsNothing(t *testing.T) {
	tk := &store.Ticket{Slug: "tkt", Phase: "ship"}
	closeOut(tk, "2026-09-05")
	if tk.Phase != "done" {
		t.Errorf("phase = %q, want done", tk.Phase)
	}
	if len(tk.Decisions) != 0 {
		t.Errorf("decisions = %+v, want none invented", tk.Decisions)
	}
}

// Both close-out paths must phrase the record identically, or the same event
// reads two ways depending on which one ran.
func TestCloseOutSharesItsPhrasingWithTheFilePath(t *testing.T) {
	tk := &store.Ticket{WaitingOn: []store.WaitingItem{{Text: "q", Who: "them"}}}
	closeOut(tk, "2026-09-05")

	ft := &devboard.Task{WaitingOn: []devboard.WaitingItem{{Text: "q", Who: "them"}}}
	devboard.CloseWaitingOn(ft, "2026-09-05")

	if tk.Decisions[0].What != ft.Decision[0].What {
		t.Errorf("store path %q != file path %q", tk.Decisions[0].What, ft.Decision[0].What)
	}
}

// End to end through the real command: the guard that would have caught the
// original bug, since it asserts the rendered board file rather than the
// in-memory ticket.
func TestDoneClosesOutTheBoardFile(t *testing.T) {
	dir := taskStoreFixture(t, true)

	// The commands that used to create these entries are gone (they had
	// never carried an entry on any real board), so the queues are seeded
	// directly. The close-out behavior still has to work: a store or an
	// archived board file written before the removal can carry them, and
	// silently dropping an unanswered question is the information loss
	// close-out exists to prevent.
	seedQueues(t, dir, "tkt")

	if _, _, err := runTask(t, "phase", "verify", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}

	if _, stderr := runCLI(t, "done", "tkt", "--summary", "shipped it"); strings.Contains(stderr, "error") {
		t.Fatalf("done: %s", stderr)
	}

	task := loadTask(t, dir)
	if task.Phase != "done" {
		t.Errorf("board file phase = %q, want done", task.Phase)
	}
	if len(task.NeedsYou) != 0 {
		t.Errorf("board file still flags needs-you: %+v", task.NeedsYou)
	}
	if len(task.WaitingOn) != 0 {
		t.Errorf("board file still shows waiting-on: %+v", task.WaitingOn)
	}
	var closed bool
	for _, d := range task.Decision {
		if strings.Contains(d.What, "unanswered at close: rate limit? (platform)") {
			closed = true
		}
	}
	if !closed {
		t.Errorf("the open question was dropped instead of recorded: %+v", task.Decision)
	}
}

// seedQueues writes an attention-queue and an external-question entry
// straight into the store, standing in for the removed commands.
func seedQueues(t *testing.T, worklogDir, slug string) {
	t.Helper()
	s, err := sqlitestore.Open(storepath.DB(worklogDir))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tk, err := s.TicketBySlug(slug)
	if err != nil {
		t.Fatal(err)
	}
	tk.NeedsYou = []store.NeedsItem{{Rank: 0, Type: "checkpoint", Text: "approve the commit"}}
	tk.WaitingOn = []store.WaitingItem{{Rank: 0, Text: "rate limit?", Who: "platform", Asked: "2026-09-08"}}
	if err := s.PutTicket(tk); err != nil {
		t.Fatal(err)
	}
	// Re-render so the board file on disk matches what was just stored;
	// the assertions below read the file, not the store.
	if err := projection.RenderTo(s, projection.Layout{
		WorklogDir: worklogDir,
	}); err != nil {
		t.Fatal(err)
	}
}
