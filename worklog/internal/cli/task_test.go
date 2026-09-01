package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/prestontallen/day2day/internal/devboard"
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

func taskFile(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "repo-x", "tkt.yaml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("schema: 1\ntitle: T\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
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
	dir := t.TempDir()
	t.Setenv("DEVBOARD_DATA", dir)
	p := taskFile(t, dir)

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
	dir := t.TempDir()
	t.Setenv("DEVBOARD_DATA", dir)
	p := taskFile(t, dir)

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
	dir := t.TempDir()
	t.Setenv("DEVBOARD_DATA", dir)
	p := taskFile(t, dir)

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
	dir := t.TempDir()
	t.Setenv("DEVBOARD_DATA", dir)
	taskFile(t, dir)
	stdout, _, err := runTask(t, "phase", "plan", "--id", "tkt", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `"action"`) || !strings.Contains(stdout, "tkt.yaml") {
		t.Fatalf("json = %q", stdout)
	}
}

func TestTaskMalformedFileFailsCleanly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DEVBOARD_DATA", dir)
	p := filepath.Join(dir, "repo-x", "bad.yaml")
	os.MkdirAll(filepath.Dir(p), 0o755)
	garbage := []byte("title: broken\n  bad: [unclosed\n")
	os.WriteFile(p, garbage, 0o644)

	_, _, err := runTask(t, "phase", "verify", "--id", "bad")
	if err == nil || !strings.Contains(err.Error(), "not valid YAML") {
		t.Fatalf("expected YAML error, got %v", err)
	}
	raw, _ := os.ReadFile(p)
	if !bytes.Equal(raw, garbage) {
		t.Fatal("malformed file modified")
	}
}
