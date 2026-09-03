package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/prestontallen/ai-devboard/worklog/internal/convert"
	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/projection"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
)

// epicChildStoreFixture builds on storeWriteFixture with a fresh epic
// ("epic-a") and two not-yet-started children ("child-1", "child-2"),
// created through the real store-backed `add` command — the store model
// has no notes-file-checkbox child resolution to hand-author around
// (adb-cutover M3d/M4: a child is a full store.Ticket row with ParentID
// from the moment add --parent creates it).
//
// TestChildOfEpicStartBackfillsExistingTitlelessFile is deliberately gone
// along with it: it covered a legacy-only scenario (a devboard file
// created by task<sub> before the epic's title/type were known, later
// "backfilled" once start resolved the real block). Under the store the
// epic's title/type are the same store.Ticket WORK.md parsing already
// captured, present from the moment the file first renders — there is no
// titleless placeholder state to backfill.
func epicChildStoreFixture(t *testing.T) (live, board string) {
	t.Helper()
	live, board, _ = storeWriteFixture(t)
	mustAdd := func(args ...string) {
		t.Helper()
		runCLI(t, append(args, "--dir", live)...)
	}
	mustAdd("add", "--type", "epic", "--title", "Cross-cutting epic", "--id", "epic-a")
	mustAdd("add", "--parent", "epic-a", "--title", "first child task", "--id", "child-1")
	mustAdd("add", "--parent", "epic-a", "--title", "second child task", "--id", "child-2")
	return live, board
}

// epicChildFreshStoreFixture is epicChildStoreFixture's twin for the
// devboard-disabled sad path: it cannot reuse storeWriteFixture's shared
// corpus, which already carries "an-epic"/"kid-live" as board-tracked
// tickets rendered under that fixture's own DEVBOARD_DATA — pointing
// DEVBOARD_DATA at a second, unrelated absent directory afterward would
// make those unrelated, already-tracked files look "missing" to the
// hand-edit guard (correctly refused: a relocated devboard dir is not a
// hand edit, but the guard can't tell them apart). Starting from an empty
// store sidesteps that: nothing is board-tracked yet, so the guard has
// nothing to miss, and DEVBOARD_DATA can stay pointed at a directory that
// never exists for the whole test.
func epicChildFreshStoreFixture(t *testing.T) (live string) {
	t.Helper()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "WORK.md"), []byte("## Now\n\n## Next\n\n## Someday\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := memstore.New()
	c, err := convert.ReadCorpusDir(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := convert.Load(s, c); err != nil {
		t.Fatal(err)
	}
	live = t.TempDir()
	if err := projection.RenderAll(s, live); err != nil {
		t.Fatal(err)
	}

	t.Setenv("DEVBOARD_DATA", filepath.Join(t.TempDir(), "absent"))
	dataDir := filepath.Join(t.TempDir(), "migration")
	t.Setenv("WORKLOG_MIGRATION_DATA", dataDir)
	if _, stderr := runCLI(t, "migrate", "--dir", live, "--out", dataDir); strings.Contains(stderr, "error") {
		t.Fatalf("migrate: %s", stderr)
	}

	mustAdd := func(args ...string) {
		t.Helper()
		runCLI(t, append(args, "--dir", live)...)
	}
	mustAdd("add", "--type", "epic", "--title", "Cross-cutting epic", "--id", "epic-a")
	mustAdd("add", "--parent", "epic-a", "--title", "first child task", "--id", "child-1")
	return live
}

func invokeDoneInDir(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	prev := flagDir
	flagDir = dir
	t.Cleanup(func() { flagDir = prev })

	cmd := newDoneCmd()
	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func loadEpicTask(t *testing.T, devDir, epicID string) devboard.Task {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(devDir, "*", epicID+".yaml"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected one %s.yaml, got %v (err %v)", epicID, matches, err)
	}
	var task devboard.Task
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &task); err != nil {
		t.Fatal(err)
	}
	return task
}

func childByID(t *testing.T, task devboard.Task, id string) devboard.ChildEntry {
	t.Helper()
	for _, c := range task.Children {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("child %q not found in %+v", id, task.Children)
	return devboard.ChildEntry{}
}

// Criterion 1.
func TestChildOfEpicStartCreatesNoStandaloneFile(t *testing.T) {
	live, devDir := epicChildStoreFixture(t)

	if _, err := invokeStartInDir(t, live, "child-1"); err != nil {
		t.Fatalf("start: %v", err)
	}

	if matches, _ := filepath.Glob(filepath.Join(devDir, "*", "child-1.yaml")); len(matches) != 0 {
		t.Fatalf("stray per-child file created: %v", matches)
	}
	task := loadEpicTask(t, devDir, "epic-a")
	if task.Type != "epic" || task.Title != "Cross-cutting epic" || task.Worklog != "epic-a" {
		t.Fatalf("bad epic identity: %+v", task)
	}
	c1 := childByID(t, task, "child-1")
	if c1.State != devboard.ChildActive || c1.Title != "first child task" {
		t.Fatalf("child-1 not active: %+v", c1)
	}
	c2 := childByID(t, task, "child-2")
	if c2.State != devboard.ChildPending {
		t.Fatalf("child-2 should be pending (not started): %+v", c2)
	}
}

// Criterion 3.
func TestChildOfEpicResumeSyncsEpic(t *testing.T) {
	live, devDir := epicChildStoreFixture(t)

	if _, err := invokeStartInDir(t, live, "child-1"); err != nil {
		t.Fatalf("setup start: %v", err)
	}
	if _, err := invokeWait(t, live, "child-1"); err != nil {
		t.Fatalf("setup wait: %v", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(devDir, "*", "child-1.yaml")); len(matches) != 0 {
		t.Fatalf("stray per-child file present after wait: %v", matches)
	}

	if _, err := invokeStartInDir(t, live, "child-1"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(devDir, "*", "child-1.yaml")); len(matches) != 0 {
		t.Fatalf("stray per-child file created on resume: %v", matches)
	}
	task := loadEpicTask(t, devDir, "epic-a")
	if childByID(t, task, "child-1").State != devboard.ChildActive {
		t.Fatalf("child-1 not active after resume: %+v", task.Children)
	}
}

// Criterion 4.
func TestChildOfEpicDoneMarksChildDone(t *testing.T) {
	live, devDir := epicChildStoreFixture(t)

	if _, err := invokeStartInDir(t, live, "child-1"); err != nil {
		t.Fatalf("setup start: %v", err)
	}
	if _, err := invokeDoneInDir(t, live, "child-1", "--summary", "done"); err != nil {
		t.Fatalf("done: %v", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(devDir, "*", "child-1.yaml")); len(matches) != 0 {
		t.Fatalf("stray per-child file present after done: %v", matches)
	}
	task := loadEpicTask(t, devDir, "epic-a")
	if childByID(t, task, "child-1").State != devboard.ChildDone {
		t.Fatalf("child-1 not done: %+v", task.Children)
	}
}

// Criterion 5.
func TestTwoActiveChildrenIndependentState(t *testing.T) {
	live, devDir := epicChildStoreFixture(t)

	if _, err := invokeStartInDir(t, live, "child-1"); err != nil {
		t.Fatalf("start child-1: %v", err)
	}
	if _, err := invokeStartInDir(t, live, "child-2"); err != nil {
		t.Fatalf("start child-2: %v", err)
	}

	if _, _, err := runTask(t, "phase", "implementing", "--id", "epic-a", "--child", "child-1", "--dir", live); err != nil {
		t.Fatalf("mutate child-1: %v", err)
	}
	if _, _, err := runTask(t, "plan", "add", "child-2 step", "--id", "epic-a", "--child", "child-2", "--dir", live); err != nil {
		t.Fatalf("mutate child-2: %v", err)
	}

	task := loadEpicTask(t, devDir, "epic-a")
	c1, c2 := childByID(t, task, "child-1"), childByID(t, task, "child-2")
	if c1.State != devboard.ChildActive || c2.State != devboard.ChildActive {
		t.Fatalf("both children should be active: c1=%+v c2=%+v", c1, c2)
	}
	if c1.Phase != "implementing" || len(c1.Plan) != 0 {
		t.Fatalf("child-1 mutation wrong or leaked: %+v", c1)
	}
	if c2.Phase != "" || len(c2.Plan) != 1 {
		t.Fatalf("child-2 mutation wrong or leaked: %+v", c2)
	}
}

// Criterion 6.
func TestTaskEpicWithoutChildRefuses(t *testing.T) {
	// resolveStoreTarget's "--id is an epic, pass --child" refusal names
	// known children via s.Children(epicID) — a pure store relation query,
	// with no rendering or BoardTracked involved — so a WORK.md-only
	// fixture (via migrate's WORK.md-derived pass) is enough.
	s := memstore.New()
	epic := &store.Ticket{
		Slug: "epic-x", Title: "X", Type: store.TypeEpic,
		State: store.StatePending, Section: store.SectionNext,
	}
	if err := s.PutTicket(epic); err != nil {
		t.Fatal(err)
	}
	if err := s.PutTicket(&store.Ticket{
		Slug: "kid-1", Title: "Kid one", Type: store.TypeTicket,
		State: store.StateActive, Section: store.SectionNow, ParentID: epic.ID,
	}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := projection.RenderAll(s, dir); err != nil {
		t.Fatal(err)
	}
	devDir := filepath.Join(dir, "devboard")
	if err := os.MkdirAll(devDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(t.TempDir(), "migration")
	t.Setenv("DEVBOARD_DATA", devDir)
	t.Setenv("WORKLOG_DIR", dir)
	t.Setenv("WORKLOG_MIGRATION_DATA", dataDir)
	if _, stderr := runCLI(t, "migrate", "--dir", dir, "--out", dataDir); strings.Contains(stderr, "error") {
		t.Fatalf("migrate: %s", stderr)
	}

	_, _, err := runTask(t, "phase", "implementing", "--id", "epic-x")
	if err == nil || !strings.Contains(err.Error(), "kid-1") {
		t.Fatalf("expected refusal naming known children, got %v", err)
	}
}

// Criterion 7.
func TestTaskChildRejectedOnPlainTicket(t *testing.T) {
	taskStoreFixture(t, false)

	_, _, err := runTask(t, "phase", "implementing", "--id", "tkt", "--child", "whatever")
	if err == nil || !strings.Contains(err.Error(), "--child is only valid") {
		t.Fatalf("expected --child rejection, got %v", err)
	}
}

// Criterion 8.
func TestTaskChildSlugRefuses(t *testing.T) {
	live, devDir := epicChildStoreFixture(t)

	if _, err := invokeStartInDir(t, live, "child-1"); err != nil {
		t.Fatalf("setup start: %v", err)
	}

	_, _, err := runTask(t, "plan", "add", "x", "--id", "child-1", "--dir", live)
	if err == nil || !strings.Contains(err.Error(), "epic-a") || !strings.Contains(err.Error(), "--child") {
		t.Fatalf("expected refusal pointing at --id epic-a --child child-1, got %v", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(devDir, "*", "child-1.yaml")); len(matches) != 0 {
		t.Fatalf("stray per-child file created despite refusal: %v", matches)
	}
}

// Criterion 9.
func TestChildOfEpicPRMirrorsToEpicFile(t *testing.T) {
	live, devDir := epicChildStoreFixture(t)

	if _, err := invokeStartInDir(t, live, "child-1"); err != nil {
		t.Fatalf("setup start: %v", err)
	}
	if _, err := invokePR(t, live, "child-1", "https://example.com/pull/9"); err != nil {
		t.Fatalf("pr: %v", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(devDir, "*", "child-1.yaml")); len(matches) != 0 {
		t.Fatalf("stray per-child file created by pr: %v", matches)
	}
	task := loadEpicTask(t, devDir, "epic-a")
	c1 := childByID(t, task, "child-1")
	if len(c1.Links) != 1 || c1.Links[0].Label != "PR" || c1.Links[0].URL != "https://example.com/pull/9" {
		t.Fatalf("PR not mirrored onto child entry: %+v", c1)
	}

	// Clearing follows the same replace-not-append logic as a plain ticket.
	if _, err := invokePR(t, live, "child-1", "--clear"); err != nil {
		t.Fatalf("pr --clear: %v", err)
	}
	task = loadEpicTask(t, devDir, "epic-a")
	if len(childByID(t, task, "child-1").Links) != 0 {
		t.Fatalf("PR link not cleared: %+v", childByID(t, task, "child-1"))
	}
}

// Sad path: devboard disabled, child-of-epic start/done remain no-ops.
func TestChildOfEpicNoopWhenDevboardDisabled(t *testing.T) {
	live := epicChildFreshStoreFixture(t)

	if _, err := invokeStartInDir(t, live, "child-1"); err != nil {
		t.Fatalf("start should succeed even with devboard disabled: %v", err)
	}
	if _, err := invokeDoneInDir(t, live, "child-1", "--summary", "done"); err != nil {
		t.Fatalf("done should succeed even with devboard disabled: %v", err)
	}
	if _, err := os.Stat(devboard.DataDir()); !os.IsNotExist(err) {
		t.Fatalf("devboard data dir should stay absent (opt-in by presence), got err=%v", err)
	}
}

// TestEpicRosterMissingNotesFile (criterion 12: an epic with no children
// degrades gracefully rather than erroring) is deliberately gone along
// with epicRoster/devboardSyncEpic (adb-cutover M4 legacy retirement):
// those existed to scrape a roster out of notes/<epic>.md by regex, with
// "missing notes file" as a special case worth pinning. Under the store
// model, an epic's roster is s.Children(epicID) against the DB — an epic
// with zero children returns an empty slice like any other query, with
// no notes file to be missing and nothing special to degrade.
