package migrate

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMigrateFullCorpusIntegration runs the actual worklog migrate
// mechanism — staging, copy-forward seed, convert.Load, id-set diff,
// swap — against the same hazard-covering fixture corpus the round-trip
// and rerun-carries-every-kind tests are proven against
// (internal/convert/testdata/corpus), rather than migrate's own minimal
// synthetic fixtures. Unchanged then changed, per the contract's plan.
func TestMigrateFullCorpusIntegration(t *testing.T) {
	live := t.TempDir()
	copyTreeForTest(t, "../convert/testdata/corpus", live)
	src := Sources{WorklogDir: live, DevboardDir: filepath.Join(live, "devboard")}
	o := Options{Sources: src, DataDir: t.TempDir()}

	// Run 1: baseline.
	res1, err := Run(o)
	if err != nil {
		t.Fatal(err)
	}
	if res1.Report.Tickets != 7 {
		t.Errorf("run 1: tickets = %d, want 7 (matches internal/convert's TestRoundTrip fixture)", res1.Report.Tickets)
	}
	if res1.Report.Feedback != 2 {
		t.Errorf("run 1: feedback = %d, want 2", res1.Report.Feedback)
	}
	if res1.BackedUp {
		t.Error("run 1: should not report a backup")
	}
	if len(res1.Diff.Removed) != 0 || len(res1.Diff.Changed) != 0 {
		t.Errorf("run 1: baseline diff should be add-only: %+v", res1.Diff)
	}
	if len(res1.StaleRows) != 0 {
		t.Errorf("run 1: no stale rows on a baseline run, got %v", res1.StaleRows)
	}

	// Run 2: unchanged live data — full determinism proof end to end
	// through the CLI mechanism, not just convert.Load directly.
	res2, err := Run(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Diff.Added) != 0 || len(res2.Diff.Removed) != 0 || len(res2.Diff.Changed) != 0 {
		t.Errorf("run 2 (unchanged): diff should be empty: +%v -%v ~%v", res2.Diff.Added, res2.Diff.Removed, res2.Diff.Changed)
	}
	if res2.Diff.Unchanged == 0 {
		t.Error("run 2 (unchanged): expected a non-zero unchanged count")
	}
	if !res2.BackedUp {
		t.Error("run 2: should report a backup of run 1's db")
	}
	if res2.Report.Feedback != res1.Report.Feedback {
		t.Errorf("run 2: feedback count drifted on unchanged input: %d -> %d", res1.Report.Feedback, res2.Report.Feedback)
	}

	// Run 3: a real change — solo leaves live data, kid-live's title changes.
	workMD, err := os.ReadFile(filepath.Join(live, "WORK.md"))
	if err != nil {
		t.Fatal(err)
	}
	soloBlock := "- [ ] **SOLO** — A standalone ticket\n" +
		"  - **ID**: solo\n" +
		"  - **Repo**: nole\n" +
		"  - **Tags**: worklog cli release\n" +
		"  - **Acceptance**: does the thing\n\n"
	changed := strings.Replace(string(workMD), soloBlock, "", 1)
	changed = strings.Replace(changed, "Active child", "Active child, renamed", 1)
	if changed == string(workMD) {
		t.Fatal("test fixture assumptions about WORK.md's content are stale")
	}
	if err := os.WriteFile(filepath.Join(live, "WORK.md"), []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}

	res3, err := Run(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(res3.Diff.Changed) != 0 {
		t.Errorf("run 3: a title change must not change any ULID: %+v", res3.Diff.Changed)
	}
	found := false
	for _, s := range res3.StaleRows {
		if s == "solo" {
			found = true
		}
	}
	if !found {
		t.Errorf("run 3: expected 'solo' among stale rows (it left WORK.md), got %v", res3.StaleRows)
	}
}

func copyTreeForTest(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
