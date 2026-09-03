package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
)

// storeWriteFixture stands up a canonical corpus and migrates it into a
// real store the CLI's unconditionally store-backed write path can open.
func storeWriteFixture(t *testing.T) (live, board, dataDir string) {
	t.Helper()
	live, board = canonicalWorklogFixture(t)
	dataDir = filepath.Join(t.TempDir(), "migration")
	t.Setenv("DEVBOARD_DATA", board)
	t.Setenv("WORKLOG_MIGRATION_DATA", dataDir)
	t.Setenv("WORKLOG_STORE_SYNC", "")
	if _, stderr := runCLI(t, "migrate", "--dir", live, "--out", dataDir); strings.Contains(stderr, "error") {
		t.Fatalf("migrate: %s", stderr)
	}
	return live, board, dataDir
}

func readBoard(t *testing.T, board, repo, slug string) devboard.Task {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(board, repo, slug+".yaml"))
	if err != nil {
		t.Fatalf("reading rendered board file: %v", err)
	}
	var task devboard.Task
	if err := yaml.Unmarshal(data, &task); err != nil {
		t.Fatal(err)
	}
	return task
}

// TestStoreWriteRendersThrough is M3c's core claim: with the store as the
// system of record, a task subcommand's mutation is committed to the
// store AND on disk in the devboard projection before the command
// returns — no async gap, so the dashboard and the session-start hook
// keep reading live files with nothing else changed.
func TestStoreWriteRendersThrough(t *testing.T) {
	live, board, _ := storeWriteFixture(t)

	_, stderr := runCLI(t, "task", "scorecard", "add", "a new criterion",
		"--verify", "go test ./...", "--id", "solo", "--dir", live)
	if strings.Contains(stderr, "refus") || strings.Contains(stderr, "error") {
		t.Fatalf("scorecard add failed: %s", stderr)
	}

	task := readBoard(t, board, "nole", "solo")
	var found bool
	for _, c := range task.Score {
		if c.Text == "a new criterion" {
			found = true
			if c.Verify != "go test ./..." {
				t.Errorf("verify not written through: %+v", c)
			}
		}
	}
	if !found {
		t.Fatalf("the write never reached the rendered file: %+v", task.Score)
	}
}

// TestStoreWriteRefusesHandEditedProjection is M3b's guard, wired: the
// store is the source, so re-rendering over a hand-edited projection
// would destroy it. The write refuses and names the file instead.
func TestStoreWriteRefusesHandEditedProjection(t *testing.T) {
	live, _, _ := storeWriteFixture(t)

	work := filepath.Join(live, "WORK.md")
	data, err := os.ReadFile(work)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(work, append(data, []byte("\n  - **Status**: typed by hand\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	err = runCLIExpectingFailure(t, "task", "phase", "implementing", "--id", "solo", "--dir", live)
	if err == nil {
		t.Fatal("the write was allowed over a hand-edited projection")
	}
	if !strings.Contains(err.Error(), "refusing to write") || !strings.Contains(err.Error(), "WORK.md") {
		t.Fatalf("want a refusal naming WORK.md, got: %v", err)
	}

	// And the hand-edit is still there — refusing means not writing.
	after, err := os.ReadFile(work)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "typed by hand") {
		t.Error("the refusal did not protect the edit")
	}
}

// TestStoreWriteKeepsSubItemIdentity is adb-task-item-ids through the
// chokepoint: removing an item must not disturb the identity of the ones
// that survive, which is what round-tripping the store's ULIDs through
// devboard.Task's Ident buys.
func TestStoreWriteKeepsSubItemIdentity(t *testing.T) {
	live, board, _ := storeWriteFixture(t)

	for _, text := range []string{"first", "second", "third"} {
		if _, stderr := runCLI(t, "task", "plan", "add", text, "--id", "solo", "--dir", live); strings.Contains(stderr, "error") {
			t.Fatalf("plan add %q: %s", text, stderr)
		}
	}
	before := readBoard(t, board, "nole", "solo")
	if len(before.Plan) < 3 {
		t.Fatalf("want 3 plan steps, got %d", len(before.Plan))
	}
	n := len(before.Plan)

	if _, stderr := runCLI(t, "task", "plan", "remove", "1", "--id", "solo", "--dir", live); strings.Contains(stderr, "error") {
		t.Fatalf("plan remove: %s", stderr)
	}
	after := readBoard(t, board, "nole", "solo")
	if len(after.Plan) != n-1 {
		t.Fatalf("want %d steps after remove, got %d", n-1, len(after.Plan))
	}
	for i, p := range after.Plan {
		if p.Text != before.Plan[i+1].Text {
			t.Errorf("step %d shifted: want %q, got %q", i, before.Plan[i+1].Text, p.Text)
		}
	}
}

// runCLIExpectingFailure is runCLI for a command that is supposed to fail:
// it returns the error instead of failing the test on it.
func runCLIExpectingFailure(t *testing.T, args ...string) error {
	t.Helper()
	prev := flagDir
	t.Cleanup(func() { flagDir = prev })

	root := newRoot()
	var out, errOut strings.Builder
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	return root.Execute()
}
