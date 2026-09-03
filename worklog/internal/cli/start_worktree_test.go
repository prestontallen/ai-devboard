package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
)

// End-to-end cover for the worktree grouping bug: task files created from a
// linked worktree were filed under a directory named after the worktree, so
// the dashboard (which groups by repo) never showed them.
//
// TestStartFromWorktreeGroupsUnderRepo and TestStartWarnsOnNewGroup are
// deliberately gone (not just fixed): they covered start's legacy
// cwd-based grouping (devboard.RepoName() detecting the git repo start
// happened to run from). The store-backed start has no such bug class —
// group is always the ticket's own canonical **Repo**: field, never cwd
// (adb-cutover's "repo attribution heals here rather than following
// cwd" decision) — so a ticket started from a worktree without a Repo
// field renders to "unknown", the same as running from anywhere else.
//
// TestTaskFromWorktreeNeedsNoForce is gone for the same reason
// (adb-cutover M4 legacy retirement): it pinned resolveTaskPath's
// cross-repo-group --force escape hatch from a worktree cwd, but
// ordinary task<sub> mutations now resolve against the store
// (resolveStoreTarget), which has no repo-group concept — see
// task_test.go's TestTaskUntrackRefusesCrossRepoID and its neighboring
// comment for the fuller version of this same point.

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

func TestPendingNewGroupQuietWhenDisabled(t *testing.T) {
	worktreeFixture(t)
	// Point at a path that doesn't exist: devboard is opt-in by dir presence,
	// so nothing should be reported.
	t.Setenv("DEVBOARD_DATA", filepath.Join(t.TempDir(), "absent"))
	if got := devboard.PendingNewGroup(); got != "" {
		t.Errorf("PendingNewGroup() = %q, want empty when devboard is disabled", got)
	}
}
