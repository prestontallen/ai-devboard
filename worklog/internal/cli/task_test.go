package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
// DEVBOARD_DATA and WORKLOG_DIR at a real migrated
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
	t.Setenv("DEVBOARD_DATA", devDir)
	t.Setenv("WORKLOG_DIR", dir)
	if _, stderr := runCLI(t, "adopt", "--commit", "--dir", dir); strings.Contains(stderr, "error") {
		t.Fatalf("migrate: %s", stderr)
	}
	return dir
}

// loadTask reads taskStoreFixture's "tkt" ticket's board shape. It read
// the rendered devboard YAML until that projection was retired
// (adb-retire-devboard-dir-2).
func loadTask(t *testing.T, dir string) devboard.Task {
	t.Helper()
	return boardOf(t, dir, "tkt")
}

func TestTaskPhaseSetAndValidate(t *testing.T) {
	dir := taskStoreFixture(t, false)

	if _, _, err := runTask(t, "phase", "verify", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	if got := loadTask(t, dir).Phase; got != "verify" {
		t.Fatalf("phase = %q", got)
	}
	_, _, err := runTask(t, "phase", "bogus", "--id", "tkt")
	if err == nil || !strings.Contains(err.Error(), "unknown phase") {
		t.Fatalf("expected unknown-phase error, got %v", err)
	}
}

func TestTaskPlanLifecycle(t *testing.T) {
	dir := taskStoreFixture(t, false)

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
	task := loadTask(t, dir)
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

func TestTaskScorecardAndDecisionAndCode(t *testing.T) {
	dir := taskStoreFixture(t, false)

	mustRun := func(args ...string) {
		t.Helper()
		if _, _, err := runTask(t, append(args, "--id", "tkt")...); err != nil {
			t.Fatal(err)
		}
	}
	mustRun("scorecard", "add", "it works", "--verify", "go test ./...")
	mustRun("scorecard", "pass", "1")
	mustRun("decision", "chose flock", "--why", "atomic rename alone races")
	mustRun("code", "internal/devboard/devboard.go", "--lines", "1-20", "--lang", "go", "--note", "core")

	task := loadTask(t, dir)
	if task.Score[0].Status != "pass" || task.Score[0].Verify != "go test ./..." {
		t.Fatalf("scorecard = %+v", task.Score)
	}
	if task.Decision[0].Why == "" || task.Decision[0].When == "" {
		t.Fatalf("decision = %+v", task.Decision)
	}
	if task.Code[0].Note != "core" {
		t.Fatalf("code = %+v", task.Code)
	}
}

func TestTaskJSONOutput(t *testing.T) {
	taskStoreFixture(t, false)
	stdout, _, err := runTask(t, "phase", "plan", "--id", "tkt", "--json")
	if err != nil {
		t.Fatal(err)
	}
	// `file` is the ticket slug. It named the rendered devboard YAML until
	// that projection was retired (adb-retire-devboard-dir-2).
	if !strings.Contains(stdout, `"action"`) || !strings.Contains(stdout, `"file": "tkt"`) {
		t.Fatalf("json = %q", stdout)
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

// research joins the phase enum between clarify and contract; the error
// message is derived from the same list, so it can never go stale.
func TestTaskPhaseResearch(t *testing.T) {
	dir := taskStoreFixture(t, false)

	if _, _, err := runTask(t, "phase", "research", "--id", "tkt"); err != nil {
		t.Fatalf("phase research: %v", err)
	}
	if got := loadTask(t, dir).Phase; got != "research" {
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

// TestRemovedTaskSubcommandsFail: a deleted subcommand must not exit 0.
//
// Cobra's default is to treat an unknown word as an argument to the parent,
// print the parent's help and exit 0 — so a script or an agent still calling
// `worklog task needs-you add "..."` would read a deleted command as
// success. That is the same class of failure as printing success without
// writing, which this project has already been bitten by once.
func TestRemovedTaskSubcommandsFail(t *testing.T) {
	taskStoreFixture(t, false)
	for _, sub := range []string{"needs-you", "waiting-on"} {
		if _, _, err := runTask(t, sub, "--id", "tkt"); err == nil {
			t.Errorf("`worklog task %s` succeeded; a removed command must fail", sub)
		}
	}
}
