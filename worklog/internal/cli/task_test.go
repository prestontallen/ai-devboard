package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/projection"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
)

// runTask executes `worklog task <args...>` against a fresh root command,
// capturing stdout/stderr and the exit-code error.
func runTask(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := newRoot()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(append([]string{"task"}, args...))
	err = cmd.Execute()
	return out.String(), errBuf.String(), err
}

// taskStoreFixture builds a real worklog ticket "tkt" and points
// DEVBOARD_DATA/WORKLOG_DIR/WORKLOG_MIGRATION_DATA at a real migrated
// store — task<sub> resolves --id against the store (resolveStoreTarget),
// not a bare devboard file. tracked controls whether the ticket starts
// BoardTracked: false is the common case (a task<sub> mutation
// board-tracks and creates the file on first use, same as any other
// ticket); true is for tests that need the file to already exist before
// any mutation runs (untrack, which only ever finds an existing file,
// never creates one).
func taskStoreFixture(t *testing.T, tracked bool) (dir string) {
	t.Helper()
	s := memstore.New()
	if err := s.PutTicket(&store.Ticket{
		Slug: "tkt", Title: "T", Type: store.TypeTicket,
		State: store.StatePending, Section: store.SectionNext,
		Repo: devboard.RepoName(), BoardTracked: tracked,
	}); err != nil {
		t.Fatal(err)
	}
	dir = t.TempDir()
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
	return dir
}

// taskFilePath is where taskStoreFixture's "tkt" ticket's devboard file
// lands, whether or not it exists yet.
func taskFilePath(dir string) string {
	return filepath.Join(dir, "devboard", devboard.RepoName(), "tkt.yaml")
}

func loadTask(t *testing.T, p string) devboard.Task {
	t.Helper()
	var task devboard.Task
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &task); err != nil {
		t.Fatal(err)
	}
	return task
}

func TestTaskNoopWhenDisabled(t *testing.T) {
	t.Setenv("DEVBOARD_DATA", filepath.Join(t.TempDir(), "absent"))
	_, stderr, err := runTask(t, "phase", "verify", "--id", "x")
	if err != nil {
		t.Fatalf("expected exit 0 no-op, got %v", err)
	}
	if !strings.Contains(stderr, "no-op") {
		t.Fatalf("expected stderr notice, got %q", stderr)
	}
}

func TestTaskPhaseSetAndValidate(t *testing.T) {
	dir := taskStoreFixture(t, false)
	p := taskFilePath(dir)

	if _, _, err := runTask(t, "phase", "verify", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	if got := loadTask(t, p).Phase; got != "verify" {
		t.Fatalf("phase = %q", got)
	}
	_, _, err := runTask(t, "phase", "bogus", "--id", "tkt")
	if err == nil || !strings.Contains(err.Error(), "unknown phase") {
		t.Fatalf("expected unknown-phase error, got %v", err)
	}
}

func TestTaskPlanLifecycle(t *testing.T) {
	dir := taskStoreFixture(t, false)
	p := taskFilePath(dir)

	for _, item := range []string{"first step", "second step"} {
		if _, _, err := runTask(t, "plan", "add", item, "--id", "tkt"); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := runTask(t, "plan", "start", "1", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runTask(t, "plan", "done", "1", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	task := loadTask(t, p)
	if len(task.Plan) != 2 || task.Plan[0].State != "done" || task.Plan[1].State != "pending" {
		t.Fatalf("plan = %+v", task.Plan)
	}
	// out-of-range index → exit 64
	_, _, err := runTask(t, "plan", "done", "9", "--id", "tkt")
	ec, ok := err.(exitCoder)
	if !ok || ec.ExitCode() != 64 {
		t.Fatalf("expected exit 64, got %v", err)
	}
}

func TestTaskScorecardAndNeedsYouAndDecisionAndCode(t *testing.T) {
	dir := taskStoreFixture(t, false)
	p := taskFilePath(dir)

	mustRun := func(args ...string) {
		t.Helper()
		if _, _, err := runTask(t, append(args, "--id", "tkt")...); err != nil {
			t.Fatal(err)
		}
	}
	mustRun("scorecard", "add", "it works", "--verify", "go test ./...")
	mustRun("scorecard", "pass", "1")
	mustRun("needs-you", "add", "approve the thing", "--type", "checkpoint", "--detail", "body")
	mustRun("decision", "chose flock", "--why", "atomic rename alone races")
	mustRun("code", "internal/devboard/devboard.go", "--lines", "1-20", "--lang", "go", "--note", "core")

	task := loadTask(t, p)
	if task.Score[0].Status != "pass" || task.Score[0].Verify != "go test ./..." {
		t.Fatalf("scorecard = %+v", task.Score)
	}
	if task.NeedsYou[0].Type != "checkpoint" || task.NeedsYou[0].Detail != "body" {
		t.Fatalf("needs_you = %+v", task.NeedsYou)
	}
	if task.Decision[0].Why == "" || task.Decision[0].When == "" {
		t.Fatalf("decision = %+v", task.Decision)
	}
	if task.Code[0].Note != "core" {
		t.Fatalf("code = %+v", task.Code)
	}

	mustRun("needs-you", "resolve", "all")
	if task = loadTask(t, p); len(task.NeedsYou) != 0 {
		t.Fatalf("needs_you not cleared: %+v", task.NeedsYou)
	}
}

func TestTaskJSONOutput(t *testing.T) {
	taskStoreFixture(t, false)
	stdout, _, err := runTask(t, "phase", "plan", "--id", "tkt", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `"action"`) || !strings.Contains(stdout, "tkt.yaml") {
		t.Fatalf("json = %q", stdout)
	}
}

func TestTaskUntrackRemovesOnlyTaskFile(t *testing.T) {
	dir := taskStoreFixture(t, true)
	p := taskFilePath(dir)
	os.WriteFile(p+".lock", nil, 0o644)

	if _, _, err := runTask(t, "untrack", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{p, p + ".lock"} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Fatalf("%s still exists", gone)
		}
	}
	// untracking again: clear error, exit 1
	_, _, err := runTask(t, "untrack", "--id", "tkt")
	if err == nil || !strings.Contains(err.Error(), "no task file") {
		t.Fatalf("expected no-task-file error, got %v", err)
	}
}

// otherRepoTaskFile creates a task file under a repo-group directory
// guaranteed to differ from devboard.RepoName() — simulating a task file
// that belongs to a different, unrelated repo checkout. Still meaningful
// for untrack (TestTaskUntrackRefusesCrossRepoID below): untrack alone
// still resolves via resolveTaskPath's filesystem scan, cross-repo check
// included. Every other task<sub> mutation now resolves against the
// store (resolveStoreTarget) instead, which has no such concept — see
// that test's neighbors, deliberately gone, for why.
func otherRepoTaskFile(t *testing.T, dir, id string) string {
	t.Helper()
	other := "other-repo"
	if other == devboard.RepoName() {
		other = "other-repo-2" // paranoia: never collide with the real repo name
	}
	p := filepath.Join(dir, other, id+".yaml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("schema: 1\ntitle: Other repo's task\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestTaskCrossRepoIDCollisionRefused, TestTaskCrossRepoIDCollisionJSONShape,
// TestTaskForceBypassesCrossRepoCollision, TestTaskSameRepoReEntryNeedsNoForce,
// and TestTaskMalformedFileFailsCleanly are deliberately gone (adb-cutover
// M4 legacy retirement). All five drove an ordinary mutation (phase, not
// untrack) against a bare devboard file with no backing worklog ticket,
// to pin resolveTaskPath's filesystem-scan behavior: cross-repo-group
// collision detection (with its --force escape hatch) and a malformed-YAML
// parse error. Ordinary task<sub> mutations no longer resolve through
// resolveTaskPath at all — they resolve against the store
// (resolveStoreTarget, task_store.go), which has no repo-group concept
// ("Repo attribution heals here rather than following cwd") and no file to
// parse in the first place; a bare id with no matching ticket is just "no
// ticket found". untrack is the one command that still resolves via
// resolveTaskPath, and keeps its own cross-repo coverage in
// TestTaskUntrackRefusesCrossRepoID below.
func TestTaskUntrackRefusesCrossRepoID(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DEVBOARD_DATA", dir)
	otherPath := otherRepoTaskFile(t, dir, "shared-id") // allowCreate=false path

	_, _, err := runTask(t, "untrack", "--id", "shared-id")
	if err == nil || !strings.Contains(err.Error(), "different repo") {
		t.Fatalf("expected cross-repo refusal on untrack (allowCreate=false), got %v", err)
	}
	if _, statErr := os.Stat(otherPath); statErr != nil {
		t.Fatalf("other repo's file must survive a refused untrack: %v", statErr)
	}
}

// research joins the phase enum between clarify and contract; the error
// message is derived from the same list, so it can never go stale.
func TestTaskPhaseResearch(t *testing.T) {
	dir := taskStoreFixture(t, false)
	p := taskFilePath(dir)

	if _, _, err := runTask(t, "phase", "research", "--id", "tkt"); err != nil {
		t.Fatalf("phase research: %v", err)
	}
	if got := loadTask(t, p).Phase; got != "research" {
		t.Fatalf("phase = %q, want research", got)
	}

	_, _, err := runTask(t, "phase", "bogus", "--id", "tkt")
	if err == nil {
		t.Fatal("expected unknown-phase error")
	}
	if !strings.Contains(err.Error(), "research") {
		t.Fatalf("enum in error message is stale, got %v", err)
	}
}
