package storesync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/convert"
	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/projection"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
	"github.com/prestontallen/ai-devboard/worklog/internal/verify"
)

// TestDisabledIsNoop: without WORKLOG_STORE_SYNC=1, AfterWrite must not
// touch the filesystem at all — this is the property that makes the hook
// safe to call unconditionally from every write verb.
func TestDisabledIsNoop(t *testing.T) {
	t.Setenv("WORKLOG_STORE_SYNC", "")
	t.Setenv("WORKLOG_MIGRATION_DATA", filepath.Join(t.TempDir(), "should-not-be-created"))

	wd := model.Workdir{Root: t.TempDir()} // no WORK.md — would error if AfterWrite tried to read it
	rep, err := AfterWrite(wd)
	if err != nil {
		t.Fatalf("disabled AfterWrite returned an error: %v", err)
	}
	if rep != nil {
		t.Errorf("disabled AfterWrite returned a non-nil report: %+v", rep)
	}
	if _, statErr := os.Stat(os.Getenv("WORKLOG_MIGRATION_DATA")); !os.IsNotExist(statErr) {
		t.Error("disabled AfterWrite created the migration data dir — it must be a true no-op")
	}
}

// TestEnabledDerivesCleanAgainstCanonicalCorpus: proves the shadow-sync
// mechanism itself (migrate.Run + sqlitestore.Open + verify.Run wiring)
// round-trips correctly end to end, using a genuinely canonical directory
// as input. The hand-authored fixture corpus is NOT itself a render
// fixpoint (it deliberately contains things a canonical render normalizes
// away — see internal/verify's TestVerifyCleanCorpus for the same
// reasoning), so this test renders it once first to get one.
func TestEnabledDerivesCleanAgainstCanonicalCorpus(t *testing.T) {
	s := memstore.New()
	c, err := convert.ReadCorpusDir("../convert/testdata/corpus")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := convert.Load(s, c); err != nil {
		t.Fatal(err)
	}
	live := t.TempDir()
	if err := projection.RenderAll(s, live); err != nil {
		t.Fatal(err)
	}

	t.Setenv("WORKLOG_STORE_SYNC", "1")
	dataDir := t.TempDir()
	t.Setenv("WORKLOG_MIGRATION_DATA", dataDir)
	t.Setenv("DEVBOARD_DATA", filepath.Join(live, "devboard"))
	wd := model.Workdir{Root: live}
	beforeHash := hashTree(t, live)

	rep, err := AfterWrite(wd)
	if err != nil {
		t.Fatalf("AfterWrite: %v", err)
	}
	if rep == nil {
		t.Fatal("enabled AfterWrite returned a nil report")
	}
	if !rep.Clean() {
		t.Errorf("expected 0 drift against a canonical corpus, got %d: %+v", len(rep.Drifts), rep.Drifts)
	}
	if _, statErr := os.Stat(filepath.Join(dataDir, "worklog.db")); statErr != nil {
		t.Errorf("expected a derived store at %s: %v", filepath.Join(dataDir, "worklog.db"), statErr)
	}

	// The live directory itself must be untouched — shadow-sync only reads.
	if afterHash := hashTree(t, live); beforeHash != afterHash {
		t.Error("AfterWrite modified the live directory it was pointed at")
	}
}

func hashTree(t *testing.T, root string) string {
	t.Helper()
	var out string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		out += rel + ":" + string(b) + "\x00"
		return nil
	})
	if err != nil {
		t.Fatalf("hashTree(%s): %v", root, err)
	}
	return out
}

// TestBaselineDeltaHidesKnownDriftShowsNew: the property M2 shipped
// without. Live data carries a standing baseline of known drift, so a
// count alone can't distinguish "same 33 as always" from "34 — this write
// broke something". Only drift absent from the previous run is new.
func TestBaselineDeltaHidesKnownDriftShowsNew(t *testing.T) {
	known := []verify.Drift{
		{Surface: "board", File: "devboard/ai-devboard", Ticket: "skill-slim", Field: "presence", Live: "present", Rendered: "missing"},
		{Surface: "board", File: "devboard/ai-devboard", Ticket: "slim-dedupe", Field: "presence", Live: "present", Rendered: "missing"},
	}
	path := filepath.Join(t.TempDir(), "storesync-baseline.json")

	if _, had := loadBaseline(path); had {
		t.Fatal("no baseline file exists yet, but loadBaseline reported one")
	}
	if err := saveBaseline(path, known); err != nil {
		t.Fatalf("saveBaseline: %v", err)
	}
	prev, had := loadBaseline(path)
	if !had {
		t.Fatal("baseline was saved but loadBaseline did not find it")
	}

	// Same drift set as last run: nothing to report.
	if fresh := newDrifts(prev, known); len(fresh) != 0 {
		t.Errorf("known drift reported as new: %+v", fresh)
	}

	// One genuinely new entry among the known ones: exactly that one.
	regression := verify.Drift{Surface: "workmd", File: "WORK.md", Ticket: "adb-cutover", Field: "acceptance", Live: "x", Rendered: "y"}
	fresh := newDrifts(prev, append(append([]verify.Drift{}, known...), regression))
	if len(fresh) != 1 || fresh[0].Ticket != "adb-cutover" {
		t.Fatalf("want only the new drift, got %+v", fresh)
	}

	// A known drift whose values changed is a different disagreement, so
	// it must read as new rather than hiding behind its old entry.
	moved := known[0]
	moved.Rendered = "present-but-different"
	if fresh := newDrifts(prev, []verify.Drift{moved}); len(fresh) != 1 {
		t.Errorf("changed drift values should read as new, got %+v", fresh)
	}

	// A corrupt baseline degrades to "no baseline", never an error.
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, had := loadBaseline(path); had {
		t.Error("corrupt baseline should read as absent")
	}
}
