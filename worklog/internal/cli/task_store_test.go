package cli

import (
	"github.com/prestontallen/ai-devboard/worklog/internal/convert"
	"github.com/prestontallen/ai-devboard/worklog/internal/projection"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/sqlitestore"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/storepath"
)

// storeWriteFixture stands up a canonical corpus and migrates it into a
// real store the CLI's unconditionally store-backed write path can open.
func storeWriteFixture(t *testing.T) (live, board, dataDir string) {
	t.Helper()
	// The corpus is loaded straight into the real store. It used to be
	// rendered to files and adopted back, which carried board-tracking and
	// in-flight detail through the rendered devboard YAML; with that
	// projection retired a file round trip drops both
	// (adb-retire-devboard-dir-2).
	live = t.TempDir()
	board = filepath.Join(live, "nowhere")
	dataDir = storepath.Dir(live)
	t.Setenv("DEVBOARD_DATA", board)
	t.Setenv("WORKLOG_DIR", live)

	c, err := convert.ReadCorpusDir("../convert/testdata/corpus")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sq, err := sqlitestore.Open(storepath.DB(live))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := convert.Load(sq, c); err != nil {
		t.Fatal(err)
	}
	if err := projection.RenderTo(sq, projection.Layout{WorklogDir: live}); err != nil {
		t.Fatal(err)
	}
	if err := sq.Close(); err != nil {
		t.Fatal(err)
	}
	return live, board, dataDir
}

// TestStoreWriteRendersThrough is M3c's core claim: with the store as the
// system of record, a task subcommand's mutation is committed to the
// store AND on disk in the devboard projection before the command
// returns — no async gap, so the dashboard and the session-start hook
// keep reading live files with nothing else changed.
func TestStoreWriteRendersThrough(t *testing.T) {
	live, _, _ := storeWriteFixture(t)

	_, stderr := runCLI(t, "task", "scorecard", "add", "a new criterion",
		"--verify", "go test ./...", "--id", "solo", "--dir", live)
	if strings.Contains(stderr, "refus") || strings.Contains(stderr, "error") {
		t.Fatalf("scorecard add failed: %s", stderr)
	}

	task := boardOf(t, live, "solo")
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

// TestStoreWriteWarnsOverHandEditedProjection replaces the old refusal.
//
// The store is the source and markdown is render output, so a write no
// longer refuses when it finds a hand-edited projection — it overwrites,
// which is the whole point. But it says so first and names the file,
// because the overwrite is otherwise silent and notes files carry prose a
// human actually wrote.
func TestStoreWriteWarnsOverHandEditedProjection(t *testing.T) {
	live, _, _ := storeWriteFixture(t)

	work := filepath.Join(live, "WORK.md")
	data, err := os.ReadFile(work)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(work, append(data, []byte("\n  - **Status**: typed by hand\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr := runCLI(t, "task", "phase", "implementing", "--id", "solo", "--dir", live)

	if !strings.Contains(stderr, "hand-edited") || !strings.Contains(stderr, "WORK.md") {
		t.Errorf("want a warning naming WORK.md, got stderr: %q", stderr)
	}
	if strings.Contains(stderr, "refusing to write") {
		t.Errorf("the write still refused: %q", stderr)
	}

	// The write went through, so the hand edit is gone. That is the
	// bargain: the human is told, not protected.
	after, err := os.ReadFile(work)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(after), "typed by hand") {
		t.Error("the render did not overwrite the hand edit")
	}
}

// TestStoreWriteKeepsSubItemIdentity is adb-task-item-ids through the
// chokepoint: removing an item must not disturb the identity of the ones
// that survive, which is what round-tripping the store's ULIDs through
// devboard.Task's Ident buys.
func TestStoreWriteKeepsSubItemIdentity(t *testing.T) {
	live, _, _ := storeWriteFixture(t)

	for _, text := range []string{"first", "second", "third"} {
		if _, stderr := runCLI(t, "task", "plan", "add", text, "--id", "solo", "--dir", live); strings.Contains(stderr, "error") {
			t.Fatalf("plan add %q: %s", text, stderr)
		}
	}
	before := boardOf(t, live, "solo")
	if len(before.Plan) < 3 {
		t.Fatalf("want 3 plan steps, got %d", len(before.Plan))
	}
	n := len(before.Plan)

	if _, stderr := runCLI(t, "task", "plan", "remove", "1", "--id", "solo", "--dir", live); strings.Contains(stderr, "error") {
		t.Fatalf("plan remove: %s", stderr)
	}
	after := boardOf(t, live, "solo")
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

// TestHandEditWarningIgnoresAnAbsentBoard: a machine with no devboard
// directory has every board file "missing", which EditedIn correctly
// reports as edits. Warning about all of them on every write buries the
// one line that matters. Under the old refusal this state was a hard stop;
// as a warning it has to be filtered or it is permanent noise.
func TestHandEditWarningIgnoresAnAbsentBoard(t *testing.T) {
	live, _, _ := storeWriteFixture(t)
	t.Setenv("DEVBOARD_DATA", filepath.Join(t.TempDir(), "no-board-here"))

	work := filepath.Join(live, "WORK.md")
	data, err := os.ReadFile(work)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(work, append(data, []byte("\n  - **Status**: typed by hand\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	// `task` no-ops without a board dir, so use a verb that writes anyway.
	_, stderr := runCLI(t, "note", "solo", "a write with no board set up", "--dir", live)

	if !strings.Contains(stderr, "WORK.md") {
		t.Errorf("the real hand edit was not reported: %q", stderr)
	}
	if strings.Contains(stderr, "devboard/") {
		t.Errorf("warned about board files on a machine with no board dir: %q", stderr)
	}
}
