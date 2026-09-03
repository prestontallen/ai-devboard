package migrate

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/store/sqlitestore"
)

func TestMigrateFreshRun(t *testing.T) {
	src, _, _ := newLiveFixture(t)
	o := Options{Sources: src, DataDir: t.TempDir()}

	res, err := Run(o)
	if err != nil {
		t.Fatal(err)
	}
	if res.Report.Tickets != 2 {
		t.Errorf("tickets = %d, want 2", res.Report.Tickets)
	}
	if res.BackedUp {
		t.Error("first run should not report a backup")
	}
	if len(res.Diff.Added) == 0 {
		t.Error("fresh run should report every ID as added")
	}
	if len(res.Diff.Removed) != 0 || len(res.Diff.Changed) != 0 || res.Diff.Unchanged != 0 {
		t.Errorf("fresh run diff should be add-only: %+v", res.Diff)
	}
	if !fileExists(o.outputPath()) {
		t.Fatal("output db was not created")
	}
	if fileExists(o.backupPath()) {
		t.Error("no backup should exist after the first run")
	}
}

func TestMigrateIdempotentRerun(t *testing.T) {
	src, _, _ := newLiveFixture(t)
	o := Options{Sources: src, DataDir: t.TempDir()}

	if _, err := Run(o); err != nil {
		t.Fatal(err)
	}
	res, err := Run(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Diff.Added) != 0 || len(res.Diff.Removed) != 0 || len(res.Diff.Changed) != 0 {
		t.Errorf("unchanged re-run should have an empty diff: +%d added -%d removed ~%d changed",
			len(res.Diff.Added), len(res.Diff.Removed), len(res.Diff.Changed))
	}
	if res.Diff.Unchanged == 0 {
		t.Error("unchanged re-run should report every id as unchanged")
	}
	if !res.BackedUp {
		t.Error("second run should report a backup was made")
	}
}

func TestMigratePreservesIDOnTitleChange(t *testing.T) {
	src, worklogDir, _ := newLiveFixture(t)
	o := Options{Sources: src, DataDir: t.TempDir()}

	if _, err := Run(o); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(worklogDir, "WORK.md"), workMDFixture("Renamed first ticket", "Second ticket"))

	res, err := Run(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Diff.Changed) != 0 {
		t.Errorf("a title change (same slug) must not change any ULID: %+v", res.Diff.Changed)
	}
	if len(res.Diff.Added) != 0 || len(res.Diff.Removed) != 0 {
		t.Errorf("a title change should not add or remove ids: +%v -%v", res.Diff.Added, res.Diff.Removed)
	}
}

func TestMigrateRefusalLeavesOutputUnchanged(t *testing.T) {
	src, worklogDir, _ := newLiveFixture(t)
	o := Options{Sources: src, DataDir: t.TempDir()}

	if _, err := Run(o); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(o.outputPath())
	if err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(o.outputPath())
	if err != nil {
		t.Fatal(err)
	}

	mustWrite(t, filepath.Join(worklogDir, "WORK.md"), duplicateSlugFixture())

	if _, err := Run(o); err == nil {
		t.Fatal("expected a refusal from the duplicate-slug corpus")
	}

	after, err := os.ReadFile(o.outputPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("OUTPUT_PATH changed after a refused run")
	}
	afterInfo, err := os.Stat(o.outputPath())
	if err != nil {
		t.Fatal(err)
	}
	if !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
		t.Error("OUTPUT_PATH's mtime changed after a refused run — it must never be opened for writing")
	}
	for _, sidecar := range []string{o.outputPath() + "-wal", o.outputPath() + "-shm"} {
		if fileExists(sidecar) {
			t.Errorf("refusal left a WAL sidecar next to OUTPUT_PATH: %s", sidecar)
		}
	}
}

func TestMigrateBackupGeneration(t *testing.T) {
	src, worklogDir, _ := newLiveFixture(t)
	o := Options{Sources: src, DataDir: t.TempDir()}

	if _, err := Run(o); err != nil {
		t.Fatal(err)
	}
	firstGen, err := os.ReadFile(o.outputPath())
	if err != nil {
		t.Fatal(err)
	}

	mustWrite(t, filepath.Join(worklogDir, "WORK.md"), workMDFixture("Renamed first ticket", "Second ticket"))
	if _, err := Run(o); err != nil {
		t.Fatal(err)
	}

	backup, err := os.ReadFile(o.backupPath())
	if err != nil {
		t.Fatal("expected a .bak file:", err)
	}
	if string(backup) != string(firstGen) {
		t.Error(".bak does not hold the prior generation's exact bytes")
	}
	second, err := os.ReadFile(o.outputPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(second) == string(firstGen) {
		t.Error("OUTPUT_PATH did not change after a real conversion")
	}
}

func TestMigrateWALCheckpointBeforeSwap(t *testing.T) {
	src, _, _ := newLiveFixture(t)
	o := Options{Sources: src, DataDir: t.TempDir()}

	if _, err := Run(o); err != nil {
		t.Fatal(err)
	}
	if fileExists(o.workingPath()) {
		t.Error("working copy should be removed after a successful swap")
	}
	for _, p := range []string{o.outputPath(), o.workingPath()} {
		wal, shm := sidecarPaths(p)
		if fileExists(wal) || fileExists(shm) {
			t.Errorf("orphaned WAL sidecar for %s", p)
		}
	}
	// The swapped-in db must be a normal, directly-openable store — proof
	// the checkpoint actually ran (an un-checkpointed WAL would still be
	// openable via the driver, so this alone isn't sufficient, but it
	// guards against renaming into a broken state).
	s, err := sqlitestore.Open(o.outputPath())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tickets, err := s.Tickets()
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 2 {
		t.Errorf("swapped-in db has %d tickets, want 2", len(tickets))
	}
}

func TestMigrateDetectsTornSnapshot(t *testing.T) {
	// These two subtests call stageOnce directly (one attempt, no retry)
	// so the assertion is about detection, not about Stage's separate
	// retry behavior (covered by the third subtest below).
	t.Run("delete", func(t *testing.T) {
		src, worklogDir, _ := newLiveFixture(t)

		testHookAfterCopy = func(e copyEntry) {
			testHookAfterCopy = nil // fire exactly once
			os.Remove(filepath.Join(worklogDir, "FEEDBACK.md"))
		}
		t.Cleanup(func() { testHookAfterCopy = nil })

		err := stageOnce(src, t.TempDir())
		if err == nil {
			t.Fatal("expected a torn-snapshot error")
		}
		var torn *ErrTornSnapshot
		if !errors.As(err, &torn) {
			t.Fatalf("expected *ErrTornSnapshot, got %T: %v", err, err)
		}
	})

	t.Run("rewrite", func(t *testing.T) {
		// A single-file corpus (WORK.md only, no FEEDBACK.md/devboard)
		// removes any ambiguity about copy order: the one hook firing is
		// guaranteed to land strictly after WORK.md's own "before" stat
		// was captured, so only the final re-stat pass can catch it —
		// exactly the case a delete-only check would miss.
		worklogDir := t.TempDir()
		mustWrite(t, filepath.Join(worklogDir, "WORK.md"), workMDFixtureOneTicket("Original title"))
		src := Sources{WorklogDir: worklogDir, DevboardDir: t.TempDir()}

		testHookAfterCopy = func(e copyEntry) {
			testHookAfterCopy = nil // fire exactly once
			mustWrite(t, filepath.Join(worklogDir, "WORK.md"), workMDFixtureOneTicket("Rewritten mid-copy, much longer title so the size differs"))
		}
		t.Cleanup(func() { testHookAfterCopy = nil })

		err := stageOnce(src, t.TempDir())
		if err == nil {
			t.Fatal("expected a torn-snapshot error")
		}
		var torn *ErrTornSnapshot
		if !errors.As(err, &torn) {
			t.Fatalf("expected *ErrTornSnapshot, got %T: %v", err, err)
		}
	})

	t.Run("Run retries once and reports OUTPUT_PATH untouched on a persistent tear", func(t *testing.T) {
		src, worklogDir, _ := newLiveFixture(t)
		o := Options{Sources: src, DataDir: t.TempDir()}

		// Mutates WORK.md's size after every single copy event — since
		// this fires after WORK.md's own copy too, the final re-stat
		// pass always disagrees, on both the first attempt and the retry.
		count := 0
		testHookAfterCopy = func(e copyEntry) {
			count++
			mustWrite(t, filepath.Join(worklogDir, "WORK.md"),
				workMDFixtureOneTicket("Rewritten mid-copy "+strings.Repeat("x", count)))
		}
		t.Cleanup(func() { testHookAfterCopy = nil })

		_, err := Run(o)
		if err == nil {
			t.Fatal("expected a torn-snapshot error")
		}
		var torn *ErrTornSnapshot
		if !errors.As(err, &torn) {
			t.Fatalf("expected *ErrTornSnapshot, got %T: %v", err, err)
		}
		if fileExists(o.outputPath()) {
			t.Error("a persistently torn run must not leave a partial OUTPUT_PATH")
		}
	})

	t.Run("retry succeeds once the tear stops", func(t *testing.T) {
		src, worklogDir, _ := newLiveFixture(t)
		o := Options{Sources: src, DataDir: t.TempDir()}

		attempts := 0
		testHookAfterCopy = func(e copyEntry) {
			attempts++
			if attempts == 1 { // only the first attempt's first copy is torn
				os.Remove(filepath.Join(worklogDir, "FEEDBACK.md"))
			}
		}
		t.Cleanup(func() { testHookAfterCopy = nil })

		res, err := Run(o)
		if err != nil {
			t.Fatalf("expected the retry to succeed, got %v", err)
		}
		if res.Report.Tickets != 2 {
			t.Errorf("tickets = %d, want 2", res.Report.Tickets)
		}
	})
}

func TestMigrateFeedbackNotDuplicated(t *testing.T) {
	src, _, _ := newLiveFixture(t)
	o := Options{Sources: src, DataDir: t.TempDir()}

	res1, err := Run(o)
	if err != nil {
		t.Fatal(err)
	}
	res2, err := Run(o)
	if err != nil {
		t.Fatal(err)
	}
	if res1.Report.Feedback != res2.Report.Feedback {
		t.Errorf("feedback count changed on unchanged input: %d -> %d", res1.Report.Feedback, res2.Report.Feedback)
	}
	s, err := sqlitestore.Open(o.outputPath())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	entries, err := s.Feedback()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != res2.Report.Feedback {
		t.Errorf("db holds %d feedback rows, report says %d converted this run — copy-forward duplicated them", len(entries), res2.Report.Feedback)
	}
}

func TestMigrateReportsStaleRows(t *testing.T) {
	src, worklogDir, _ := newLiveFixture(t)
	o := Options{Sources: src, DataDir: t.TempDir()}

	if _, err := Run(o); err != nil {
		t.Fatal(err)
	}
	// solo-b leaves live data entirely (e.g. archived by hand outside this
	// corpus, or simply removed) between the two runs.
	mustWrite(t, filepath.Join(worklogDir, "WORK.md"), workMDFixtureOneTicket("First ticket"))

	res, err := Run(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.StaleRows) != 1 || res.StaleRows[0] != "solo-b" {
		t.Errorf("stale rows = %v, want [solo-b]", res.StaleRows)
	}
	s, err := sqlitestore.Open(o.outputPath())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tickets, err := s.Tickets()
	if err != nil {
		t.Fatal(err)
	}
	if res.Report.Tickets+len(res.StaleRows) != len(tickets) {
		t.Errorf("converted (%d) + stale (%d) != db ticket count (%d)", res.Report.Tickets, len(res.StaleRows), len(tickets))
	}
}

func TestMigrateNeverWritesLiveDirs(t *testing.T) {
	run := func(t *testing.T, corrupt bool) {
		src, worklogDir, devboardDir := newLiveFixture(t)
		if corrupt {
			mustWrite(t, filepath.Join(worklogDir, "WORK.md"), duplicateSlugFixture())
		}
		before := checksumTree(t, worklogDir) + "|" + checksumTree(t, devboardDir)

		o := Options{Sources: src, DataDir: t.TempDir()}
		_, err := Run(o)
		if corrupt && err == nil {
			t.Fatal("expected the duplicate-slug corpus to be refused")
		}
		if !corrupt && err != nil {
			t.Fatal(err)
		}

		after := checksumTree(t, worklogDir) + "|" + checksumTree(t, devboardDir)
		if before != after {
			t.Error("live dirs changed during migrate")
		}
	}
	t.Run("success", func(t *testing.T) { run(t, false) })
	t.Run("refusal", func(t *testing.T) { run(t, true) })
}

// checksumTree returns a stable digest of every file's relative path,
// size and content under root, order-independent.
func checksumTree(t *testing.T, root string) string {
	t.Helper()
	var entries []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		entries = append(entries, rel+":"+string(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(entries)
	out := ""
	for _, e := range entries {
		out += e + "\x00"
	}
	return out
}
