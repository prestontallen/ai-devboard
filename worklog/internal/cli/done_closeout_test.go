package cli

import (
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

	if _, _, err := runTask(t, "needs-you", "add", "approve the commit", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runTask(t, "waiting-on", "add", "rate limit?", "--who", "platform", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runTask(t, "phase", "verify", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}

	if _, stderr := runCLI(t, "done", "tkt", "--summary", "shipped it"); strings.Contains(stderr, "error") {
		t.Fatalf("done: %s", stderr)
	}

	task := loadTask(t, taskFilePath(dir))
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
