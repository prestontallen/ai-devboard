package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
)

// End-to-end cover for the worktree grouping bug: task files created from a
// linked worktree were filed under a directory named after the worktree, so
// the dashboard (which groups by repo) never showed them.

const worktreeWorkMD = `# Work

## Now

## Next

- [ ] **TKT-1** — A ticket
  - **ID**: tkt-1
  - **PR**:

- [ ] **TKT-2** — Another ticket
  - **ID**: tkt-2
  - **PR**:

## Someday
`

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// worktreeFixture builds a repo named "acme-api" with a linked worktree at
// .claude/worktrees/feature-x, chdirs into the worktree, and returns both
// paths plus a fresh devboard data dir.
func worktreeFixture(t *testing.T) (repoName, worktree, dataDir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := filepath.Join(t.TempDir(), "acme-api")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, root, "init", "-q", "-b", "main")
	gitIn(t, root, "commit", "-q", "--allow-empty", "-m", "init")

	wt := filepath.Join(root, ".claude", "worktrees", "feature-x")
	gitIn(t, root, "worktree", "add", "-q", "-b", "feature-x", wt)

	dataDir = t.TempDir()
	t.Setenv("DEVBOARD_DATA", dataDir)

	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(wt); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	return "acme-api", wt, dataDir
}

// worklogDir writes a WORK.md fixture into a fresh dir and returns it.
func worklogDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "WORK.md"), []byte(worktreeWorkMD), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runStartCmd(t *testing.T, worklog string, args ...string) (string, error) {
	t.Helper()
	prev := flagDir
	t.Cleanup(func() { flagDir = prev })

	root := newRoot()
	var stdout, stderr strings.Builder
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"start", "--dir", worklog}, args...))
	err := root.Execute()
	return stdout.String(), err
}

func TestStartFromWorktreeGroupsUnderRepo(t *testing.T) {
	repo, _, data := worktreeFixture(t)
	wl := worklogDir(t)

	if out, err := runStartCmd(t, wl, "tkt-1", "--json"); err != nil {
		t.Fatalf("start: %v\nout: %s", err, out)
	}

	want := filepath.Join(data, repo, "tkt-1.yaml")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("task file not at %s: %v", want, err)
	}
	// The phantom directory is the actual symptom: a group named after the
	// worktree, holding the file the dashboard never finds.
	if _, err := os.Stat(filepath.Join(data, "feature-x")); err == nil {
		t.Error("a repo group named after the worktree was created")
	}

	entries, err := os.ReadDir(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != repo {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("devboard groups = %v, want exactly [%s]", names, repo)
	}
}

func TestTaskFromWorktreeNeedsNoForce(t *testing.T) {
	repo, _, data := worktreeFixture(t)

	// A task file as the main checkout would have written it.
	group := filepath.Join(data, repo)
	if err := os.MkdirAll(group, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(group, "tkt-1.yaml")
	if err := os.WriteFile(p, []byte("schema: 1\ntitle: T\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// No --force: this is what used to fail with "different repo".
	if _, stderr, err := runTask(t, "phase", "verify", "--id", "tkt-1"); err != nil {
		t.Fatalf("task from worktree needed --force: %v\nstderr: %s", err, stderr)
	}

	task := loadTask(t, p)
	if task.Phase != "verify" {
		t.Errorf("phase = %q, want the write to have landed", task.Phase)
	}
}

func TestStartWarnsOnNewGroup(t *testing.T) {
	repo, _, data := worktreeFixture(t)
	wl := worklogDir(t)

	// First start: the group doesn't exist, so the notice fires.
	out, err := runStartCmd(t, wl, "tkt-1", "--json")
	if err != nil {
		t.Fatalf("start: %v\nout: %s", err, out)
	}
	var res struct {
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	var found string
	for _, w := range res.Warnings {
		if strings.Contains(w, "repo group") {
			found = w
		}
	}
	if found == "" {
		t.Fatalf("no new-group warning in %q", res.Warnings)
	}
	if !strings.Contains(found, repo) {
		t.Errorf("warning %q does not name the group %q", found, repo)
	}

	// Second start, group now present: no notice. Uses a different ticket so
	// the command actually reaches the warning code instead of bailing out on
	// an already-started one.
	if _, err := os.Stat(filepath.Join(data, repo)); err != nil {
		t.Fatalf("group should exist by now: %v", err)
	}
	out2, err := runStartCmd(t, wl, "tkt-2", "--json")
	if err != nil {
		t.Fatalf("second start: %v\nout: %s", err, out2)
	}
	var res2 struct {
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out2), &res2); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out2)
	}
	for _, w := range res2.Warnings {
		if strings.Contains(w, "repo group") {
			t.Errorf("new-group warning fired for an existing group: %q", w)
		}
	}
}

func TestPendingNewGroupQuietWhenDisabled(t *testing.T) {
	worktreeFixture(t)
	// Point at a path that doesn't exist: devboard is opt-in by dir presence,
	// so nothing should be reported.
	t.Setenv("DEVBOARD_DATA", filepath.Join(t.TempDir(), "absent"))
	if got := devboard.PendingNewGroup(); got != "" {
		t.Errorf("PendingNewGroup() = %q, want empty when devboard is disabled", got)
	}
}
