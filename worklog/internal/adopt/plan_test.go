package adopt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/convert"
	"github.com/prestontallen/ai-devboard/worklog/internal/projection"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
)

// canonical renders the shared fixture corpus into a fresh pair of roots,
// so the corpus on disk is already a render fixpoint and any plan operation
// other than keep is attributable to what the test then does to it.
func canonical(t *testing.T) (store.Store, Roots, []string) {
	t.Helper()
	s := memstore.New()
	c, err := convert.ReadCorpusDir("../convert/testdata/corpus")
	if err != nil {
		t.Fatal(err)
	}
	rep, err := convert.Load(s, c)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	live := filepath.Join(root, "worklog")
	if err := projection.RenderAll(s, live); err != nil {
		t.Fatal(err)
	}
	return s, Roots{Worklog: live}, rep.Skipped
}

func ops(p *Plan) map[string]Op {
	out := map[string]Op{}
	for _, c := range p.Changes {
		out[c.Path] = c.Op
	}
	return out
}

// TestPlanOnACanonicalCorpusIsAllKeep: adoption must converge, so running
// it against a corpus already equal to the render must plan no writes.
func TestPlanOnACanonicalCorpusIsAllKeep(t *testing.T) {
	s, r, skipped := canonical(t)
	p, err := BuildPlan(s, r, skipped)
	if err != nil {
		t.Fatal(err)
	}
	if p.Writes() {
		for _, c := range p.Changes {
			if c.Op != OpKeep && c.Op != OpDerived {
				t.Errorf("unexpected %s", c)
			}
		}
	}
	if len(p.Changes) == 0 {
		t.Fatal("plan is empty; it should account for every rendered file")
	}
}

// TestPlanNeverDeletesIndexMD: INDEX.md is not a projection, it is rebuilt
// by reindex over the rendered output. A plan that treats "the store does
// not render it" as "delete it" would destroy the index every run.
func TestPlanNeverDeletesIndexMD(t *testing.T) {
	s, r, skipped := canonical(t)
	if err := os.WriteFile(filepath.Join(r.Worklog, "INDEX.md"), []byte("# Index\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := BuildPlan(s, r, skipped)
	if err != nil {
		t.Fatal(err)
	}
	if got := ops(p)["INDEX.md"]; got != OpDerived {
		t.Errorf("INDEX.md = %q, want %q", got, OpDerived)
	}
}

// TestPlanRewritesAndCreates covers the two write classes.
func TestPlanRewritesAndCreates(t *testing.T) {
	s, r, skipped := canonical(t)
	if err := os.WriteFile(filepath.Join(r.Worklog, "WORK.md"), []byte("# Work\n\n## Next\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(r.Worklog, "FEEDBACK.md")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	p, err := BuildPlan(s, r, skipped)
	if err != nil {
		t.Fatal(err)
	}
	got := ops(p)
	if got["WORK.md"] != OpRewrite {
		t.Errorf("WORK.md = %q, want %q", got["WORK.md"], OpRewrite)
	}
	if got["FEEDBACK.md"] != OpCreate {
		t.Errorf("FEEDBACK.md = %q, want %q", got["FEEDBACK.md"], OpCreate)
	}
	if !p.Writes() {
		t.Error("Writes() = false with a rewrite and a create planned")
	}
}

// TestPlanAgainstSnapshot previews a real adoption: it converts the corpus
// named by WORKLOG_SNAPSHOT and prints what adoption would do to it. Purely
// read-only.
func TestPlanAgainstSnapshot(t *testing.T) {
	live := os.Getenv("WORKLOG_SNAPSHOT")
	if live == "" {
		t.Skip("set WORKLOG_SNAPSHOT to preview a real adoption")
	}
	board := os.Getenv("DEVBOARD_SNAPSHOT")

	// ReadCorpusDir expects one root with devboard/ nested inside, the
	// shape migrate.Stage produces, so stage a copy the same way.
	staged := t.TempDir()
	if err := copyTree(live, staged, board, map[string]string{}); err != nil {
		t.Fatal(err)
	}
	if board != "" {
		if err := copyTree(board, filepath.Join(staged, "devboard"), "", map[string]string{}); err != nil {
			t.Fatal(err)
		}
	}

	s := memstore.New()
	c, err := convert.ReadCorpusDir(staged)
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	rep, err := convert.Load(s, c)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	p, err := BuildPlan(s, Roots{Worklog: staged}, rep.Skipped)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("plan: %v", p.Counts())
	for _, ch := range p.Changes {
		if ch.Op == OpDelete || ch.Op == OpCreate || ch.Op == OpOrphan {
			t.Logf("%s", ch)
		}
	}
}
