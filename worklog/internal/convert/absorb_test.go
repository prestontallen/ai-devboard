package convert

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/boardmap"
	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
	"gopkg.in/yaml.v3"
)

// TestAbsorbRoundTripLosesNothing is the fidelity probe for absorbing a bare
// devboard file into its store ticket. The fixture is a verbatim copy of the
// one real orphan in live data: 9 plan steps, 10 scorecard rows and 3
// decisions that exist ONLY in that file. If a field cannot survive the trip
// through the store, absorbing is data loss and the design is wrong, so this
// runs before any absorb code exists.
func TestAbsorbRoundTripLosesNothing(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "orphan_absorb.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	// What the file says, parsed independently of the conversion path.
	var want devboard.Task
	if err := yaml.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if len(want.Plan) != 9 || len(want.Score) != 10 || len(want.Decision) != 3 {
		t.Fatalf("fixture drifted: plan=%d score=%d decisions=%d, want 9/10/3",
			len(want.Plan), len(want.Score), len(want.Decision))
	}

	// Into the store's shape, then back out to board YAML.
	bf, err := DevboardYAML("adb-skill-deploy-path.yaml", raw, false)
	if err != nil {
		t.Fatal(err)
	}
	if bf.Join != "" {
		t.Fatalf("fixture is supposed to be bare, got join %q", bf.Join)
	}
	var got devboard.Task
	if err := yaml.Unmarshal(boardmap.BoardYAML(bf.Fragment, nil), &got); err != nil {
		t.Fatal(err)
	}

	if got.Complexity != want.Complexity {
		t.Errorf("complexity: got %q want %q", got.Complexity, want.Complexity)
	}
	if got.Phase != want.Phase {
		t.Errorf("phase: got %q want %q", got.Phase, want.Phase)
	}
	if len(got.Plan) != len(want.Plan) {
		t.Fatalf("plan: got %d steps want %d", len(got.Plan), len(want.Plan))
	}
	for i := range want.Plan {
		if got.Plan[i].Text != want.Plan[i].Text || got.Plan[i].State != want.Plan[i].State {
			t.Errorf("plan[%d]: got %+v want %+v", i, got.Plan[i], want.Plan[i])
		}
	}
	if len(got.Score) != len(want.Score) {
		t.Fatalf("scorecard: got %d rows want %d", len(got.Score), len(want.Score))
	}
	for i := range want.Score {
		g, w := got.Score[i], want.Score[i]
		if g.Text != w.Text || g.Verify != w.Verify || g.Status != w.Status {
			t.Errorf("scorecard[%d]: got %+v want %+v", i, g, w)
		}
	}
	if len(got.Decision) != len(want.Decision) {
		t.Fatalf("decisions: got %d want %d", len(got.Decision), len(want.Decision))
	}
	for i := range want.Decision {
		g, w := got.Decision[i], want.Decision[i]
		if g.What != w.What || g.Why != w.Why || g.When != w.When {
			t.Errorf("decision[%d]: got %+v want %+v", i, g, w)
		}
	}
}

// bareCorpus is a minimal corpus with one WORK.md ticket and one bare board
// file, so each absorb rule can be exercised on its own.
func bareCorpus(t *testing.T, slug, filename string, board []byte) Corpus {
	t.Helper()
	work := "# Work\n\n## Now\n\n- [ ] **" + strings.ToUpper(slug) + "** — A ticket\n" +
		"  - **ID**: " + slug + "\n  - **Repo**: demo\n\n## Waiting\n\n## Next\n\n## Someday\n"
	return Corpus{
		WorkMD:   []byte(work),
		Archives: map[string][]byte{},
		Notes:    map[string][]byte{},
		Board:    []BoardInput{{Repo: "demo", Name: filename, Data: board}},
	}
}

const bareBody = "schema: 1\nphase: implementing\nplan:\n  - text: only step\n    state: done\n"

// A bare file whose stem names a ticket in the corpus is absorbed into it.
func TestAbsorbJoinsByFilename(t *testing.T) {
	c := bareCorpus(t, "demo-one", "demo-one.yaml", []byte(bareBody))
	s := memstore.New()
	rep, err := Load(s, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Skipped) != 0 {
		t.Fatalf("file was skipped, not absorbed: %v", rep.Skipped)
	}
	found, err := s.TicketBySlug("demo-one")
	if err != nil {
		t.Fatalf("ticket demo-one missing after load: %v", err)
	}
	if found.Phase != "implementing" {
		t.Errorf("phase not absorbed: %q", found.Phase)
	}
	if len(found.PlanSteps) != 1 || found.PlanSteps[0].Text != "only step" {
		t.Errorf("plan not absorbed: %+v", found.PlanSteps)
	}
	if !found.BoardTracked {
		t.Error("absorbed ticket should be board-tracked")
	}
}

// A bare file whose stem names nothing is left skipped, never absorbed into
// some near neighbour and never silently dropped.
func TestAbsorbLeavesUnmatchedFileSkipped(t *testing.T) {
	c := bareCorpus(t, "demo-one", "not-a-ticket.yaml", []byte(bareBody))
	rep, err := Load(memstore.New(), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Skipped) != 1 || rep.Skipped[0] != "demo/not-a-ticket.yaml" {
		t.Fatalf("want the file reported skipped, got %v", rep.Skipped)
	}
}

// Two files claiming one ticket refuse. mergeBoard replaces sub-items
// wholesale, so without this the later file wins on directory read order.
func TestAbsorbRefusesDuplicateClaim(t *testing.T) {
	c := bareCorpus(t, "demo-one", "demo-one.yaml", []byte(bareBody))
	c.Board = append(c.Board, BoardInput{
		Repo: "demo", Name: "other.yaml",
		Data: []byte("worklog: demo-one\nschema: 1\nphase: done\n"),
	})
	_, err := Load(memstore.New(), c)
	if err == nil {
		t.Fatal("want a refusal, got none")
	}
	if !strings.Contains(err.Error(), "both claim ticket") {
		t.Fatalf("refusal should name the collision, got: %v", err)
	}
}

// The join key still wins over the filename, so a file deliberately stored
// under a different name keeps pointing where it says.
func TestExplicitJoinBeatsFilename(t *testing.T) {
	c := bareCorpus(t, "demo-one", "demo-two.yaml",
		[]byte("worklog: demo-one\nschema: 1\nphase: verify\n"))
	s := memstore.New()
	rep, err := Load(s, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Skipped) != 0 {
		t.Fatalf("unexpected skip: %v", rep.Skipped)
	}
	tk, err := s.TicketBySlug("demo-one")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Phase != "verify" {
		t.Errorf("explicit join lost: phase %q", tk.Phase)
	}
}
