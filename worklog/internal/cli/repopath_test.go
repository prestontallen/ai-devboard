package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/migrate"
	"github.com/prestontallen/ai-devboard/worklog/internal/projection"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/sqlitestore"
)

// This file ports internal/devboard/repopath_test.go's coverage
// (adb-cutover M4 legacy retirement): those tests drove OnStart/Mutate
// directly to pin RepoPath self-healing/refresh rules that moved into
// ensureBoardTracked (store_common.go) during the cutover — same logic
// (record when missing, refresh when the recorded root has gone away,
// never touched by an unrelated mutation), just against a store ticket
// instead of a devboard.Task. These tests drive real git, like the
// originals: the bug class they exist for is a wrong assumption about
// what `git rev-parse` reports, which a stub would encode rather than
// catch.

// repoPathFixture builds a real worklog ticket "tkt" with the given
// declared Repo, migrated into a real store, WITHOUT changing the
// process's cwd — callers chdir separately into whatever git repo the
// scenario needs, since ensureBoardTracked's git detection reads cwd.
func repoPathFixture(t *testing.T, declaredRepo string) (dir string) {
	t.Helper()
	s := memstore.New()
	if err := s.PutTicket(&store.Ticket{
		Slug: "tkt", Title: "T", Type: store.TypeTicket,
		State: store.StatePending, Section: store.SectionNext,
		Repo: declaredRepo,
	}); err != nil {
		t.Fatal(err)
	}
	dir = t.TempDir()
	if err := projection.RenderAll(s, dir); err != nil {
		t.Fatal(err)
	}
	devDir := filepath.Join(dir, "devboard")
	// The fixture ticket isn't BoardTracked yet, so RenderAll never creates
	// devboard/ — but ensureBoardTracked (which these tests exercise) is
	// gated on devboard.Enabled(), which checks the directory exists.
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

// startInRepo chdirs into a fresh repo named repoName, runs `worklog
// start tkt`, and returns the resulting devboard task plus the repo root
// git actually reports.
func startInRepo(t *testing.T, repoName, declaredRepo string) (devboard.Task, string) {
	t.Helper()
	root := newGitRepo(t, repoName)
	chdirTest(t, root)
	worklogDir := repoPathFixture(t, declaredRepo)

	if _, err := invokeStartInDir(t, worklogDir, "tkt"); err != nil {
		t.Fatalf("start: %v", err)
	}
	return loadTask(t, findTaskFile(t)), resolvedPath(t, root)
}

func newGitRepo(t *testing.T, name string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, root, "init", "-q", "-b", "main")
	gitIn(t, root, "commit", "-q", "--allow-empty", "-m", "init")
	return root
}

func chdirTest(t *testing.T, dir string) {
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

// resolvedPath matches git, which reports the symlink-resolved path (/var
// vs /private/var on macOS).
func resolvedPath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// findTaskFile locates "tkt"'s rendered devboard file. Unlike
// taskFilePath (which assumes the file's group is devboard.RepoName()),
// this walks the whole data dir — the render path is keyed off the
// ticket's own declared Repo, which several tests here deliberately
// mismatch against cwd's repo name, and an owner-qualified Repo
// ("prestontallen/nole") nests the file a level deeper than
// devboard.Find's single-level group scan expects.
func findTaskFile(t *testing.T) string {
	t.Helper()
	var found string
	err := filepath.WalkDir(devboard.DataDir(), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if d.Name() == "tkt.yaml" {
			found = p
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == "" {
		t.Fatal("tkt task file not found under the devboard data dir")
	}
	return found
}

func TestRepoPathRecordedOnStart(t *testing.T) {
	task, root := startInRepo(t, "ai-devboard", "ai-devboard")
	if task.RepoPath != root {
		t.Errorf("repo_path = %q, want %q", task.RepoPath, root)
	}
}

// The ticket says its code is in ai-devboard while cwd resolves to
// claude-skills. Recording cwd would point tooling at the wrong tree, so
// nothing is recorded.
func TestRepoPathSkippedOnRepoMismatch(t *testing.T) {
	task, _ := startInRepo(t, "claude-skills", "ai-devboard")
	if task.RepoPath != "" {
		t.Errorf("repo_path = %q, want empty on repo mismatch", task.RepoPath)
	}
}

func TestRepoPathRecordedWhenRepoUndeclared(t *testing.T) {
	task, root := startInRepo(t, "somerepo", "")
	if task.RepoPath != root {
		t.Errorf("repo_path = %q, want %q", task.RepoPath, root)
	}
}

// An owner-qualified **Repo**: value still matches the repo named by its
// final element — WORK.md carries both "ai-devboard" and
// "prestontallen/nole".
func TestRepoPathMatchesOwnerQualifiedRepo(t *testing.T) {
	task, root := startInRepo(t, "nole", "prestontallen/nole")
	if task.RepoPath != root {
		t.Errorf("repo_path = %q, want %q", task.RepoPath, root)
	}
}

func TestRepoPathAbsentOutsideAGitRepo(t *testing.T) {
	chdirTest(t, t.TempDir())
	worklogDir := repoPathFixture(t, "")

	if _, err := invokeStartInDir(t, worklogDir, "tkt"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := loadTask(t, findTaskFile(t)).RepoPath; got != "" {
		t.Errorf("repo_path = %q, want empty outside a repo", got)
	}
}

// setStoreRepoPath opens the real migrated store directly (bypassing the
// CLI) and overwrites ticket slug's RepoPath, then re-renders — the
// direct-store equivalent of the legacy test's
// Mutate(p, func(tk *Task) {...}), since there is no longer a file-level
// Mutate to splice through. The re-render is required: PutTicket without
// it leaves the file on disk stale, which the next real write's hand-edit
// guard would then (correctly) refuse to touch.
func setStoreRepoPath(t *testing.T, worklogDir, slug, repoPath string) {
	t.Helper()
	dataDir, err := storeDataDir()
	if err != nil {
		t.Fatal(err)
	}
	s, err := sqlitestore.Open(migrate.OutputPath(dataDir))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tk, err := s.TicketBySlug(slug)
	if err != nil {
		t.Fatal(err)
	}
	tk.RepoPath = repoPath
	if err := s.PutTicket(tk); err != nil {
		t.Fatal(err)
	}
	layout := projection.Layout{WorklogDir: worklogDir, DevboardDir: devboard.DataDir()}
	if err := projection.RenderTo(s, layout); err != nil {
		t.Fatal(err)
	}
}

func TestRepoPathRefreshRules(t *testing.T) {
	root := newGitRepo(t, "ai-devboard")
	chdirTest(t, root)
	worklogDir := repoPathFixture(t, "ai-devboard")

	if _, err := invokeStartInDir(t, worklogDir, "tkt"); err != nil {
		t.Fatalf("start: %v", err)
	}
	task := loadTask(t, findTaskFile(t))
	if task.RepoPath != resolvedPath(t, root) {
		t.Fatalf("setup: repo_path = %q, want %q", task.RepoPath, root)
	}

	// Still on disk: the recorded answer stands. Re-entry (start on an
	// already-Active ticket refuses, so park it in Waiting and resume,
	// exercising runStoreStart's other ensureBoardTracked call site) must
	// not disturb a RepoPath that still resolves.
	stable := t.TempDir()
	setStoreRepoPath(t, worklogDir, "tkt", stable)
	if _, err := invokeWait(t, worklogDir, "tkt"); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if _, err := invokeStartInDir(t, worklogDir, "tkt"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got := loadTask(t, findTaskFile(t)).RepoPath; got != stable {
		t.Errorf("repo_path = %q, want the existing %q left untouched", got, stable)
	}

	// Gone: refreshed, because a stale path silently disables every consumer.
	gone := filepath.Join(t.TempDir(), "deleted")
	setStoreRepoPath(t, worklogDir, "tkt", gone)
	if _, err := invokeWait(t, worklogDir, "tkt"); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if _, err := invokeStartInDir(t, worklogDir, "tkt"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got := loadTask(t, findTaskFile(t)).RepoPath; got != resolvedPath(t, root) {
		t.Errorf("repo_path = %q, want refreshed to %q", got, root)
	}
}

// Only start records the field; an unrelated mutation may not introduce
// it.
func TestRepoPathNotAddedByOtherMutations(t *testing.T) {
	root := newGitRepo(t, "ai-devboard")
	chdirTest(t, root)
	repoPathFixture(t, "ai-devboard")

	if _, _, err := runTask(t, "phase", "verify", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(findTaskFile(t))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "repo_path") {
		t.Errorf("repo_path must not appear from an unrelated mutation:\n%s", raw)
	}
}
