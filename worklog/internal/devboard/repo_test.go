package devboard

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// These tests drive real git rather than a stub. The bug they exist for was a
// wrong assumption about what `git rev-parse` returns inside a linked
// worktree, so a stub would have encoded the same wrong assumption and passed.

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// Keep the fixture independent of the developer's git config and of any
	// GIT_* vars the surrounding test run may have set.
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

// newRepo creates a repo with one commit at <tmp>/<name> and returns its path.
func newRepo(t *testing.T, name string) string {
	t.Helper()
	requireGit(t)
	root := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "-q", "-b", "main")
	git(t, root, "commit", "-q", "--allow-empty", "-m", "init")
	return root
}

// chdir moves into dir for the duration of the test. RepoName reads the
// process working directory, so these can't run in parallel.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

func TestRepoNameInMainCheckout(t *testing.T) {
	root := newRepo(t, "acme-api")
	chdir(t, root)
	if got := RepoName(); got != "acme-api" {
		t.Errorf("RepoName() = %q, want %q", got, "acme-api")
	}
}

func TestRepoNameInWorktree(t *testing.T) {
	root := newRepo(t, "acme-api")
	// Mirror the real layout: .claude/worktrees/<name> inside the repo.
	wt := filepath.Join(root, ".claude", "worktrees", "feature-x")
	git(t, root, "worktree", "add", "-q", "-b", "feature-x", wt)

	chdir(t, wt)
	got := RepoName()
	if got == "feature-x" {
		t.Fatalf("RepoName() = %q — that's the worktree's name, the bug this test exists for", got)
	}
	if got != "acme-api" {
		t.Errorf("RepoName() = %q, want %q", got, "acme-api")
	}
}

func TestRepoNameFromSubdir(t *testing.T) {
	root := newRepo(t, "acme-api")
	wt := filepath.Join(root, ".claude", "worktrees", "feature-x")
	git(t, root, "worktree", "add", "-q", "-b", "feature-x", wt)

	sub := filepath.Join(wt, "internal", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, sub)
	if got := RepoName(); got != "acme-api" {
		t.Errorf("RepoName() = %q, want %q", got, "acme-api")
	}
}

func TestRepoNameBareRepo(t *testing.T) {
	requireGit(t)
	root := filepath.Join(t.TempDir(), "acme-api.git")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "-q", "--bare", "-b", "main")

	chdir(t, root)
	// The common dir IS the repo here, so neither the parent directory nor
	// the raw basename is the answer.
	if got := RepoName(); got != "acme-api" {
		t.Errorf("RepoName() = %q, want %q", got, "acme-api")
	}
}

func TestRepoNameOutsideGit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-a-repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)

	// Nothing above t.TempDir() should be a repo; if the temp root happens to
	// sit inside one, the fallback isn't what's under test.
	if _, err := exec.Command("git", "-C", dir, "rev-parse", "--git-dir").Output(); err == nil {
		t.Skip("temp dir is inside a git repo; fallback path not exercised")
	}
	if got := RepoName(); got != "not-a-repo" {
		t.Errorf("RepoName() = %q, want the cwd basename %q", got, "not-a-repo")
	}
}

func TestRepoNameFromCommonDirEmptyOutsideGit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-a-repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)
	if _, err := exec.Command("git", "-C", dir, "rev-parse", "--git-dir").Output(); err == nil {
		t.Skip("temp dir is inside a git repo")
	}
	// The helper must report "no answer" rather than a bogus name, so
	// RepoName knows to fall through.
	if got := repoNameFromCommonDir(); got != "" {
		t.Errorf("repoNameFromCommonDir() = %q, want empty", got)
	}
}
