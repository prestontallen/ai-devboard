package devboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Like repo_test.go, these drive real git: the value being recorded is
// whatever `git rev-parse` reports, so a stub would encode the assumption
// under test rather than checking it.

// startInRepo runs OnStart from inside a fresh repo named repoName and returns
// the resulting task plus the repo root git actually reports.
func startInRepo(t *testing.T, repoName, id, declaredRepo string) (Task, string) {
	t.Helper()
	root := newRepo(t, repoName)
	chdir(t, root)
	t.Setenv("DEVBOARD_DATA", t.TempDir())

	if err := OnStart(id, "T", "", declaredRepo); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	return loadTaskByID(t, id), resolved(t, root)
}

// resolved matches git, which reports the symlink-resolved path (/var vs
// /private/var on macOS).
func resolved(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func loadTaskByID(t *testing.T, id string) Task {
	t.Helper()
	p, err := Find(id)
	if err != nil || p == "" {
		t.Fatalf("Find(%q) = %q, %v", id, p, err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var task Task
	if err := yaml.Unmarshal(raw, &task); err != nil {
		t.Fatal(err)
	}
	return task
}

func TestRepoPathRecordedOnStart(t *testing.T) {
	task, root := startInRepo(t, "ai-devboard", "tkt-1", "ai-devboard")
	if task.RepoPath != root {
		t.Errorf("repo_path = %q, want %q", task.RepoPath, root)
	}
}

// The ticket says its code is in ai-devboard while cwd resolves to
// claude-skills. Recording cwd would point tooling at the wrong tree, so
// nothing is recorded.
func TestRepoPathSkippedOnRepoMismatch(t *testing.T) {
	task, _ := startInRepo(t, "claude-skills", "tkt-1", "ai-devboard")
	if task.RepoPath != "" {
		t.Errorf("repo_path = %q, want empty on repo mismatch", task.RepoPath)
	}
}

func TestRepoPathRecordedWhenRepoUndeclared(t *testing.T) {
	task, root := startInRepo(t, "somerepo", "tkt-1", "")
	if task.RepoPath != root {
		t.Errorf("repo_path = %q, want %q", task.RepoPath, root)
	}
}

// An owner-qualified **Repo**: value still matches the repo named by its final
// element — WORK.md carries both "ai-devboard" and "prestontallen/nole".
func TestRepoPathMatchesOwnerQualifiedRepo(t *testing.T) {
	task, root := startInRepo(t, "nole", "tkt-1", "prestontallen/nole")
	if task.RepoPath != root {
		t.Errorf("repo_path = %q, want %q", task.RepoPath, root)
	}
}

func TestRepoPathAbsentOutsideAGitRepo(t *testing.T) {
	requireGit(t)
	chdir(t, t.TempDir())
	t.Setenv("DEVBOARD_DATA", t.TempDir())

	if err := OnStart("tkt-1", "T", "", ""); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	if got := loadTaskByID(t, "tkt-1").RepoPath; got != "" {
		t.Errorf("repo_path = %q, want empty outside a repo", got)
	}
}

// A bare repo has no working tree, so any path recorded there is unusable.
func TestRepoRootEmptyForBareRepo(t *testing.T) {
	requireGit(t)
	bare := filepath.Join(t.TempDir(), "foo.git")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, bare, "init", "-q", "--bare")
	chdir(t, bare)

	if got := RepoRoot(); got != "" {
		t.Errorf("RepoRoot() = %q, want empty for a bare repo", got)
	}
}

func TestRepoPathRefreshRules(t *testing.T) {
	task, root := startInRepo(t, "ai-devboard", "tkt-1", "ai-devboard")
	if task.RepoPath != root {
		t.Fatalf("setup: repo_path = %q, want %q", task.RepoPath, root)
	}
	p, err := Find("tkt-1")
	if err != nil {
		t.Fatal(err)
	}

	// Still on disk: the recorded answer stands.
	stable := t.TempDir()
	if err := Mutate(p, func(tk *Task) error { tk.RepoPath = stable; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := OnStart("tkt-1", "T", "", "ai-devboard"); err != nil {
		t.Fatal(err)
	}
	if got := loadTaskByID(t, "tkt-1").RepoPath; got != stable {
		t.Errorf("repo_path = %q, want the existing %q left untouched", got, stable)
	}

	// Gone: refreshed, because a stale path silently disables every consumer.
	gone := filepath.Join(t.TempDir(), "deleted")
	if err := Mutate(p, func(tk *Task) error { tk.RepoPath = gone; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := OnStart("tkt-1", "T", "", "ai-devboard"); err != nil {
		t.Fatal(err)
	}
	if got := loadTaskByID(t, "tkt-1").RepoPath; got != root {
		t.Errorf("repo_path = %q, want refreshed to %q", got, root)
	}
}

// Only start records the field; no other mutation may introduce it.
func TestRepoPathNotAddedByOtherMutations(t *testing.T) {
	requireGit(t)
	root := newRepo(t, "ai-devboard")
	chdir(t, root)
	data := t.TempDir()
	t.Setenv("DEVBOARD_DATA", data)

	dir := filepath.Join(data, RepoName())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "tkt-1.yaml")
	if err := os.WriteFile(p, []byte("schema: 1\ntitle: T\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Mutate(p, func(tk *Task) error { tk.Phase = "verify"; return nil }); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "repo_path") {
		t.Errorf("repo_path must not appear from an unrelated mutation:\n%s", raw)
	}
}
