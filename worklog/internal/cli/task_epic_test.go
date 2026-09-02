package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/model"
)

const epicCLIFixture = `## Now

## Next
- [ ] **EPIC-A** — Cross-cutting epic
  - **ID**: epic-a
  - **Type**: epic
  - **Notes**: notes/epic-a.md
  - **Active children**: <none>

## Someday
`

const epicCLINotes = `# Epic A

Children:
- [ ] child-1: first child task
- [ ] child-2: second child task
`

// epicFixtureDir builds a WORK.md + notes fixture with one epic and two
// not-yet-started children, and returns the worklog dir.
func epicFixtureDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "WORK.md"), []byte(epicCLIFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes", "epic-a.md"), []byte(epicCLINotes), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func invokeDoneInDir(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	prev := flagDir
	flagDir = dir
	t.Cleanup(func() { flagDir = prev })

	cmd := newDoneCmd()
	var buf bytes.Buffer
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
	wlDir := epicFixtureDir(t)
	devDir := t.TempDir()
	t.Setenv("DEVBOARD_DATA", devDir)

	if _, err := invokeStartInDir(t, wlDir, "child-1"); err != nil {
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

// Criterion 2.
func TestChildOfEpicStartBackfillsExistingTitlelessFile(t *testing.T) {
	wlDir := epicFixtureDir(t)
	devDir := t.TempDir()
	t.Setenv("DEVBOARD_DATA", devDir)

	repo := devboard.RepoName()
	seedPath := filepath.Join(devDir, repo, "epic-a.yaml")
	if err := os.MkdirAll(filepath.Dir(seedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seedPath, []byte("schema: 1\nworklog: epic-a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := invokeStartInDir(t, wlDir, "child-1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	task := loadEpicTask(t, devDir, "epic-a")
	if task.Type != "epic" || task.Title != "Cross-cutting epic" {
		t.Fatalf("title/type not backfilled: %+v", task)
	}
}

// Criterion 3.
func TestChildOfEpicResumeSyncsEpic(t *testing.T) {
	wlDir := epicFixtureDir(t)
	devDir := t.TempDir()
	t.Setenv("DEVBOARD_DATA", devDir)

	if _, err := invokeStartInDir(t, wlDir, "child-1"); err != nil {
		t.Fatalf("setup start: %v", err)
	}
	if _, err := invokeWait(t, wlDir, "child-1"); err != nil {
		t.Fatalf("setup wait: %v", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(devDir, "*", "child-1.yaml")); len(matches) != 0 {
		t.Fatalf("stray per-child file present after wait: %v", matches)
	}

	if _, err := invokeStartInDir(t, wlDir, "child-1"); err != nil {
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
	wlDir := epicFixtureDir(t)
	devDir := t.TempDir()
	t.Setenv("DEVBOARD_DATA", devDir)

	if _, err := invokeStartInDir(t, wlDir, "child-1"); err != nil {
		t.Fatalf("setup start: %v", err)
	}
	if _, err := invokeDoneInDir(t, wlDir, "child-1", "--summary", "done"); err != nil {
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
	wlDir := epicFixtureDir(t)
	devDir := t.TempDir()
	t.Setenv("DEVBOARD_DATA", devDir)

	if _, err := invokeStartInDir(t, wlDir, "child-1"); err != nil {
		t.Fatalf("start child-1: %v", err)
	}
	if _, err := invokeStartInDir(t, wlDir, "child-2"); err != nil {
		t.Fatalf("start child-2: %v", err)
	}

	if _, _, err := runTask(t, "phase", "implementing", "--id", "epic-a", "--child", "child-1"); err != nil {
		t.Fatalf("mutate child-1: %v", err)
	}
	if _, _, err := runTask(t, "plan", "add", "child-2 step", "--id", "epic-a", "--child", "child-2"); err != nil {
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
	devDir := t.TempDir()
	t.Setenv("DEVBOARD_DATA", devDir)
	repo := devboard.RepoName()
	p := filepath.Join(devDir, repo, "epic-x.yaml")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("schema: 1\ntype: epic\ntitle: X\nchildren:\n  - id: kid-1\n    state: active\n"), 0o644)

	_, _, err := runTask(t, "phase", "implementing", "--id", "epic-x")
	if err == nil || !strings.Contains(err.Error(), "kid-1") {
		t.Fatalf("expected refusal naming known children, got %v", err)
	}
}

// Criterion 7.
func TestTaskChildRejectedOnPlainTicket(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DEVBOARD_DATA", dir)
	taskFile(t, dir)

	_, _, err := runTask(t, "phase", "implementing", "--id", "tkt", "--child", "whatever")
	if err == nil || !strings.Contains(err.Error(), "--child is only valid") {
		t.Fatalf("expected --child rejection, got %v", err)
	}
}

// Criterion 8.
func TestTaskChildSlugRefuses(t *testing.T) {
	wlDir := epicFixtureDir(t)
	devDir := t.TempDir()
	t.Setenv("DEVBOARD_DATA", devDir)
	// runTask below goes through newRoot(), whose --dir flag registration
	// resets the package-level flagDir to "" as a side effect of
	// construction (pflag.StringVar sets the default immediately) — so
	// WORKLOG_DIR, not flagDir, is what resolveWorkdir() falls back to here.
	t.Setenv("WORKLOG_DIR", wlDir)

	if _, err := invokeStartInDir(t, wlDir, "child-1"); err != nil {
		t.Fatalf("setup start: %v", err)
	}

	_, _, err := runTask(t, "plan", "add", "x", "--id", "child-1")
	if err == nil || !strings.Contains(err.Error(), "epic-a") || !strings.Contains(err.Error(), "--child") {
		t.Fatalf("expected refusal pointing at --id epic-a --child child-1, got %v", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(devDir, "*", "child-1.yaml")); len(matches) != 0 {
		t.Fatalf("stray per-child file created despite refusal: %v", matches)
	}
}

// Criterion 9.
func TestChildOfEpicPRMirrorsToEpicFile(t *testing.T) {
	wlDir := epicFixtureDir(t)
	devDir := t.TempDir()
	t.Setenv("DEVBOARD_DATA", devDir)

	if _, err := invokeStartInDir(t, wlDir, "child-1"); err != nil {
		t.Fatalf("setup start: %v", err)
	}
	if _, err := invokePR(t, wlDir, "child-1", "https://example.com/pull/9"); err != nil {
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
	if _, err := invokePR(t, wlDir, "child-1", "--clear"); err != nil {
		t.Fatalf("pr --clear: %v", err)
	}
	task = loadEpicTask(t, devDir, "epic-a")
	if len(childByID(t, task, "child-1").Links) != 0 {
		t.Fatalf("PR link not cleared: %+v", childByID(t, task, "child-1"))
	}
}

// Sad path: devboard disabled, child-of-epic start/done remain no-ops.
func TestChildOfEpicNoopWhenDevboardDisabled(t *testing.T) {
	wlDir := epicFixtureDir(t)
	t.Setenv("DEVBOARD_DATA", filepath.Join(t.TempDir(), "absent"))

	if _, err := invokeStartInDir(t, wlDir, "child-1"); err != nil {
		t.Fatalf("start should succeed even with devboard disabled: %v", err)
	}
	if _, err := invokeDoneInDir(t, wlDir, "child-1", "--summary", "done"); err != nil {
		t.Fatalf("done should succeed even with devboard disabled: %v", err)
	}
}

// Sad path (criterion 12): epicRoster degrades gracefully with no notes file.
func TestEpicRosterMissingNotesFile(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "notes"), 0o755)
	os.WriteFile(filepath.Join(root, "WORK.md"), []byte(`## Now

## Next
- [ ] **EPIC-B** — Epic B
  - **ID**: epic-b
  - **Type**: epic

## Someday
`), 0o644)
	wd, err := model.NewWorkdir(root)
	if err != nil {
		t.Fatal(err)
	}
	roster, err := epicRoster(wd, "epic-b")
	if err != nil {
		t.Fatalf("expected graceful nil, got error: %v", err)
	}
	if roster != nil {
		t.Fatalf("expected empty roster, got %v", roster)
	}

	devDir := t.TempDir()
	t.Setenv("DEVBOARD_DATA", devDir)
	devboardSyncEpic(wd, "epic-b")
	task := loadEpicTask(t, devDir, "epic-b")
	if task.Type != "epic" || task.Title != "Epic B" || len(task.Children) != 0 {
		t.Fatalf("sync did not degrade gracefully: %+v", task)
	}
}
