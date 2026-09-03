package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// seedItems builds a task file with three scorecard criteria and three plan
// steps, each carrying a distinct status/state so edit and remove can be shown
// not to disturb them.
func seedItems(t *testing.T) string {
	t.Helper()
	dir := taskStoreFixture(t, false)
	p := taskFilePath(dir)

	mustRun := func(args ...string) {
		t.Helper()
		if _, _, err := runTask(t, append(args, "--id", "tkt")...); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []string{"first criterion", "second criterion", "third criterion"} {
		mustRun("scorecard", "add", c, "--verify", "check "+c)
	}
	mustRun("scorecard", "pass", "1")
	mustRun("scorecard", "fail", "2")

	for _, s := range []string{"first step", "second step", "third step"} {
		mustRun("plan", "add", s)
	}
	mustRun("plan", "done", "1")
	mustRun("plan", "start", "2")
	return p
}

func TestTaskItemEdit(t *testing.T) {
	p := seedItems(t)

	// Rewording a criterion keeps its status, and leaves verify alone when
	// --verify isn't passed.
	if _, _, err := runTask(t, "scorecard", "edit", "2", "reworded criterion", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	task := loadTask(t, p)
	if task.Score[1].Text != "reworded criterion" {
		t.Errorf("text = %q", task.Score[1].Text)
	}
	if task.Score[1].Status != "fail" {
		t.Errorf("status = %q, want it preserved", task.Score[1].Status)
	}
	if task.Score[1].Verify != "check second criterion" {
		t.Errorf("verify = %q, want it preserved", task.Score[1].Verify)
	}
	if task.Score[0].Text != "first criterion" || task.Score[2].Text != "third criterion" {
		t.Errorf("neighbours disturbed: %+v", task.Score)
	}

	// --verify on edit rewrites the check.
	if _, _, err := runTask(t, "scorecard", "edit", "2", "reworded again",
		"--verify", "go test ./new", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	task = loadTask(t, p)
	if task.Score[1].Verify != "go test ./new" {
		t.Errorf("verify = %q, want it rewritten", task.Score[1].Verify)
	}
	if task.Score[1].Status != "fail" {
		t.Errorf("status = %q, want it still preserved", task.Score[1].Status)
	}

	// Plan edit keeps the item's state.
	if _, _, err := runTask(t, "plan", "edit", "2", "reworded step", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	task = loadTask(t, p)
	if task.Plan[1].Text != "reworded step" {
		t.Errorf("text = %q", task.Plan[1].Text)
	}
	if task.Plan[1].State != "in_progress" {
		t.Errorf("state = %q, want it preserved", task.Plan[1].State)
	}
}

func TestTaskItemRemove(t *testing.T) {
	p := seedItems(t)

	if _, _, err := runTask(t, "scorecard", "remove", "2", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	task := loadTask(t, p)
	if len(task.Score) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(task.Score), task.Score)
	}
	if task.Score[0].Text != "first criterion" || task.Score[1].Text != "third criterion" {
		t.Errorf("remaining order wrong: %+v", task.Score)
	}
	if task.Score[0].Status != "pass" {
		t.Errorf("survivor lost its status: %+v", task.Score[0])
	}

	if _, _, err := runTask(t, "plan", "remove", "1", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	task = loadTask(t, p)
	if len(task.Plan) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(task.Plan), task.Plan)
	}
	if task.Plan[0].Text != "second step" || task.Plan[1].Text != "third step" {
		t.Errorf("remaining order wrong: %+v", task.Plan)
	}

	// Removing the last remaining items empties the list without error.
	for i := 0; i < 2; i++ {
		if _, _, err := runTask(t, "plan", "remove", "1", "--id", "tkt"); err != nil {
			t.Fatal(err)
		}
	}
	if task = loadTask(t, p); len(task.Plan) != 0 {
		t.Errorf("plan = %+v, want empty", task.Plan)
	}
}

func TestTaskItemBadIndex(t *testing.T) {
	p := seedItems(t)
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		args []string
	}{
		{"scorecard edit out of range", []string{"scorecard", "edit", "9", "x"}},
		{"scorecard edit zero", []string{"scorecard", "edit", "0", "x"}},
		{"scorecard remove out of range", []string{"scorecard", "remove", "9"}},
		{"scorecard remove non-numeric", []string{"scorecard", "remove", "two"}},
		{"plan edit out of range", []string{"plan", "edit", "9", "x"}},
		{"plan remove non-numeric", []string{"plan", "remove", "two"}},
		{"scorecard edit missing text", []string{"scorecard", "edit", "1"}},
		{"plan edit missing text", []string{"plan", "edit", "1"}},
		{"scorecard remove extra arg", []string{"scorecard", "remove", "1", "extra"}},
		{"plan add extra arg", []string{"plan", "add", "text", "extra"}},
		{"scorecard unknown verb", []string{"scorecard", "nope", "1"}},
		{"plan unknown verb", []string{"plan", "nope", "1"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := runTask(t, append(tc.args, "--id", "tkt")...)
			ec, ok := err.(exitCoder)
			if !ok {
				t.Fatalf("err = %v, want an exitCoder", err)
			}
			if ec.ExitCode() != 64 {
				t.Errorf("exit code = %d, want 64 (%v)", ec.ExitCode(), err)
			}
			after, rerr := os.ReadFile(p)
			if rerr != nil {
				t.Fatal(rerr)
			}
			if string(after) != string(before) {
				t.Errorf("task file changed on the failure path:\n%s", string(after))
			}
		})
	}
}

func TestTaskItemEditRejectsUnknownTaskFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DEVBOARD_DATA", dir)
	if err := os.MkdirAll(filepath.Join(dir, "somerepo"), 0o755); err != nil {
		t.Fatal(err)
	}
	// edit/remove must not create a task file the way `add` does.
	_, _, err := runTask(t, "scorecard", "edit", "1", "x", "--id", "missing")
	if err == nil {
		t.Fatal("expected an error for a task file that doesn't exist")
	}
	if _, serr := os.Stat(filepath.Join(dir, "somerepo", "missing.yaml")); serr == nil {
		t.Error("edit created a task file; only add should")
	}
}
