package projection

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/convert"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/sqlitestore"
)

// TestLiveSnapshot runs the full round-trip against a pinned snapshot of
// the real corpus. The snapshot lives OUTSIDE the repo (personal data is
// never committed); the test skips when it is absent, so CI runs on the
// synthetic corpus alone while the real proof runs locally.
//
// Prepare a snapshot (layout matches ReadCorpusDir):
//
//	d=$WORKLOG_SNAPSHOT   # e.g. ~/.local/share/worklog-snapshots/2026-09-02
//	mkdir -p $d && cp -r ~/.local/share/worklog/{WORK.md,FEEDBACK.md,archive,notes} $d/
//	cp -r ~/.local/share/devboard $d/devboard
func TestLiveSnapshot(t *testing.T) {
	root := os.Getenv("WORKLOG_SNAPSHOT")
	if root == "" {
		t.Skip("WORKLOG_SNAPSHOT not set; live round-trip runs locally only")
	}
	c, err := convert.ReadCorpusDir(root)
	if err != nil {
		t.Fatal(err)
	}

	sq, err := sqlitestore.Open(filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sq.Close()
	rep, err := convert.Load(sq, c)
	if err != nil {
		t.Fatalf("live corpus refused: %v", err)
	}
	t.Logf("converted %d tickets, %d feedback; skipped producer files: %v; warnings: %v",
		rep.Tickets, rep.Feedback, rep.Skipped, rep.Warnings)

	dir := t.TempDir()
	if err := RenderAll(sq, dir); err != nil {
		t.Fatal(err)
	}
	s2 := memstore.New()
	c2, err := convert.ReadCorpusDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := convert.Load(s2, c2); err != nil {
		t.Fatalf("re-parse of rendered projections refused: %v", err)
	}

	want, got := normalized(t, sq), normalized(t, s2)
	if string(want) != string(got) {
		wf := filepath.Join(root, "..", "direct.json")
		gf := filepath.Join(root, "..", "projected.json")
		os.WriteFile(wf, want, 0o644)
		os.WriteFile(gf, got, 0o644)
		t.Errorf("live round-trip drift; diff %s %s", wf, gf)
	}

	// Determinism over the live corpus: second conversion, same ids.
	rep2, err := convert.Load(sq, c)
	if err != nil {
		t.Fatal(err)
	}
	if rep2.Tickets != rep.Tickets {
		t.Errorf("re-run ticket count drifted: %d vs %d", rep.Tickets, rep2.Tickets)
	}
}
