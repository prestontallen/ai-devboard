package projection

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

func yamlUnmarshal(b []byte, v any) error { return yaml.Unmarshal(b, v) }

func richTicket() *store.Ticket {
	return &store.Ticket{
		ID: "01AAAAAAAAAAAAAAAAAAAAAAAA", Slug: "rich", Title: "Rich ticket",
		Type: store.TypeTicket, State: store.StateActive, Section: store.SectionNow,
		Phase: "implementing", Tier: 3, Complexity: "high",
		Branch: "b", Session: "s", RepoPath: "/repo", BoardTracked: true,
		Scout:     &store.Scout{Mode: "ran", Why: "w", When: "2026-09-03"},
		PlanSteps: []store.PlanStep{{ID: "01P", Rank: 1, Text: "one", State: "done", Extra: map[string]any{"x": "1"}}},
		Scorecard: []store.ScoreItem{{ID: "01S", Rank: 1, Text: "c", Verify: "v", Status: "pass"}},
		Decisions: []store.Decision{{ID: "01D", Rank: 1, What: "w", Why: "y", When: "2026-09-03", Complexity: "high"}},
		CodeRefs:  []store.CodeRef{{ID: "01C", Rank: 1, File: "f.go", Lines: "1-2", Lang: "go", Note: "n"}},
		NeedsYou:  []store.NeedsItem{{ID: "01N", Rank: 1, Type: "question", Text: "t", Detail: "d"}},
		WaitingOn: []store.WaitingItem{{ID: "01W", Rank: 1, Text: "t", Who: "them", Asked: "2026-09-03", Link: "l", Detail: "d"}},
		Links: []store.Link{
			{ID: "01L", Rank: 1, Kind: store.LinkPR, URL: "https://pr"},
			{ID: "01M", Rank: 2, Kind: store.LinkRef, Label: "spec", URL: "https://spec"},
		},
		Extra: map[string]any{"producer": "keep"},
	}
}

// TestBoardTaskRoundTrip: BoardTask then ApplyBoardTask is the identity on
// in-flight detail, IDs and ranks included. This is what lets the eleven
// task<sub> closures keep operating on *devboard.Task while the store
// stays the system of record underneath them.
func TestBoardTaskRoundTrip(t *testing.T) {
	orig := richTicket()
	got := richTicket()
	ApplyBoardTask(got, BoardTask(orig, nil))
	if !reflect.DeepEqual(orig, got) {
		t.Errorf("round trip lost detail:\nwant %+v\n got %+v", orig, got)
	}
}

// TestBoardTaskIdentitySurvivesEdit is adb-task-item-ids: editing an
// item's text, and removing an earlier one, must not disturb the identity
// of any survivor. Content-matching would fail the first; positional
// carry would fail the second.
func TestBoardTaskIdentitySurvivesEdit(t *testing.T) {
	tk := richTicket()
	tk.PlanSteps = []store.PlanStep{
		{ID: "01A", Rank: 1, Text: "first", State: "done"},
		{ID: "01B", Rank: 2, Text: "second", State: "pending"},
		{ID: "01C2", Rank: 3, Text: "third", State: "pending"},
	}
	task := BoardTask(tk, nil)

	// What `plan edit 3` and `plan remove 1` do to the closure's slice.
	task.Plan[2].Text = "third, reworded"
	task.Plan = task.Plan[1:]

	ApplyBoardTask(tk, task)
	if len(tk.PlanSteps) != 2 {
		t.Fatalf("want 2 steps, got %d", len(tk.PlanSteps))
	}
	if tk.PlanSteps[0].ID != "01B" || tk.PlanSteps[0].Rank != 2 {
		t.Errorf("survivor lost identity: %+v", tk.PlanSteps[0])
	}
	if tk.PlanSteps[1].ID != "01C2" || tk.PlanSteps[1].Text != "third, reworded" {
		t.Errorf("edited item lost identity: %+v", tk.PlanSteps[1])
	}
}

// TestIdentNeverSerialized: the store's ULIDs must not leak into the file.
func TestIdentNeverSerialized(t *testing.T) {
	out := string(BoardYAML(richTicket(), []*store.Ticket{
		{ID: "01K", Slug: "kid", Title: "Kid", State: store.StateActive, Type: store.TypeTicket,
			PlanSteps: []store.PlanStep{{ID: "01KP", Rank: 1, Text: "kid step", State: "pending"}}},
	}))
	for _, id := range []string{"01AAAAAAAAAAAAAAAAAAAAAAAA", "01P", "01S", "01D", "01L", "01KP"} {
		if strings.Contains(out, id) {
			t.Errorf("store id %q leaked into the rendered file:\n%s", id, out)
		}
	}
	if !strings.Contains(out, "kid step") {
		t.Errorf("child detail missing from render:\n%s", out)
	}
	var back devboard.Task
	if err := yamlUnmarshal([]byte(out), &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Plan) != 1 || back.Plan[0].ID != "" {
		t.Errorf("parsed item carried an identity it should not have: %+v", back.Plan)
	}
}
