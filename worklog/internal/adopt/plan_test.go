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
	return s, Roots{Worklog: live, Devboard: filepath.Join(live, "devboard")}, rep.Skipped
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
			if c.Op != OpKeep && c.Op != OpProduce && c.Op != OpDerived {
				t.Errorf("unexpected %s", c)
			}
		}
	}
	if len(p.Changes) == 0 {
		t.Fatal("plan is empty; it should account for every rendered file")
	}
}

// TestPlanDeletesAnOrphanBoardFile is the RenderTo-never-prunes gap.
// Without a delete class, a misfiled or orphaned board file survives beside
// its canonical twin and the dashboard renders both.
func TestPlanDeletesAnOrphanBoardFile(t *testing.T) {
	s, r, skipped := canonical(t)
	orphan := filepath.Join(r.Devboard, "some-repo", "gone-away.yaml")
	if err := os.MkdirAll(filepath.Dir(orphan), 0o755); err != nil {
		t.Fatal(err)
	}
	// Carries a worklog join, so it is store-shaped rather than producer-owned.
	if err := os.WriteFile(orphan, []byte("schema: 1\nworklog: gone-away\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := BuildPlan(s, r, skipped)
	if err != nil {
		t.Fatal(err)
	}
	if got := ops(p)["devboard/some-repo/gone-away.yaml"]; got != OpDelete {
		t.Errorf("orphan board file = %q, want %q", got, OpDelete)
	}
}

// TestPlanKeepsBareProducerFiles: a devboard file with no worklog join is
// producer-owned. adb-cutover's criterion 8 made keeping it an explicit
// promise, so it must be reported as considered, not silently deleted.
func TestPlanKeepsBareProducerFiles(t *testing.T) {
	s, r, _ := canonical(t)
	bare := filepath.Join(r.Devboard, "some-repo", "producer.yaml")
	if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bare, []byte("schema: 1\ntitle: hand written\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := BuildPlan(s, r, []string{"some-repo/producer.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if got := ops(p)["devboard/some-repo/producer.yaml"]; got != OpProduce {
		t.Errorf("bare producer file = %q, want %q", got, OpProduce)
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
	p, err := BuildPlan(s, Roots{Worklog: staged, Devboard: filepath.Join(staged, "devboard")}, rep.Skipped)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("plan: %v", p.Counts())
	for _, ch := range p.Changes {
		if ch.Op == OpDelete || ch.Op == OpCreate || ch.Op == OpProduce {
			t.Logf("%s", ch)
		}
	}
}

// TestPlanKeepsArchivedProducerFiles pins the bug the real-corpus preview
// caught. convert.Load reports a skipped file as <repo>/<slug>.yaml even
// when it lives under <repo>/_archive/, so matching on the full relative
// path missed every archived producer and planned to DELETE it. Two of the
// three bare producer files on the real corpus were archived.
func TestPlanKeepsArchivedProducerFiles(t *testing.T) {
	s, r, _ := canonical(t)
	bare := filepath.Join(r.Devboard, "nole", "_archive", "embed-retry.yaml")
	if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bare, []byte("schema: 1\ntitle: hand written\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Skipped carries no _archive/ segment, exactly as convert.Load reports it.
	p, err := BuildPlan(s, r, []string{"nole/embed-retry.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if got := ops(p)["devboard/nole/_archive/embed-retry.yaml"]; got != OpProduce {
		t.Errorf("archived bare producer = %q, want %q (criterion 8 promises it is kept)", got, OpProduce)
	}
}
