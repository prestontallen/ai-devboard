package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/projection"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
)

// fakeLister replaces the toolchain seam. A real nested `go test -list` inside
// `go test ./...` would be slow, non-hermetic, and would depend on a `go`
// binary being on PATH — and it could not simulate a build failure at all.
type fakeLister struct {
	calls   int
	dirs    []string
	pkgs    [][]string
	pattern []string
	names   []string
	err     error
}

func (f *fakeLister) install(t *testing.T) {
	t.Helper()
	prev := listTests
	listTests = func(dir string, pkgs []string, pattern string) ([]string, error) {
		f.calls++
		f.dirs = append(f.dirs, dir)
		f.pkgs = append(f.pkgs, pkgs)
		f.pattern = append(f.pattern, pattern)
		return f.names, f.err
	}
	t.Cleanup(func() { listTests = prev })
}

// scorecardFixture makes a real worklog ticket "tkt" whose single
// scorecard criterion carries verify. Under the store model, task<sub>
// resolves --id against a real store ticket (resolveStoreTarget), not a
// bare devboard file — there's no devboard file yet at all; the first
// task<sub> mutation creates it (storeMutateTaskOrChild's
// create-on-first-use), matching what "worklog start" does for any other
// ticket.
func scorecardFixture(t *testing.T, verify string) (dir string) {
	t.Helper()
	// Repo matches devboard.RepoName() so the rendered file's group lines
	// up with the process's actual cwd, same as the legacy fixture's
	// explicit devboard.RepoName() group directory did.
	return scorecardFixtureWithRepoPath(t, verify, devboard.RepoName(), "")
}

// scorecardFixtureWithRepoPath is scorecardFixture with repo/repoPath
// overridable, for the working-directory-resolution tests that need the
// rendered file's group to mismatch cwd and/or carry a recorded RepoPath.
func scorecardFixtureWithRepoPath(t *testing.T, verify, repo, repoPath string) (dir string) {
	t.Helper()
	s := memstore.New()
	if err := s.PutTicket(&store.Ticket{
		Slug: "tkt", Title: "T", Type: store.TypeTicket,
		State: store.StatePending, Section: store.SectionNext,
		Repo: repo, RepoPath: repoPath,
		// Scorecard content lives only in the rendered devboard YAML, so
		// the ticket must be BoardTracked for RenderAll to write it —
		// migrate then re-derives the sqlite store purely from what's on
		// disk, with no visibility into this in-memory store at all.
		BoardTracked: true,
		Scorecard:    []store.ScoreItem{{Text: "c1", Verify: verify, Status: "pending"}},
	}); err != nil {
		t.Fatal(err)
	}
	dir = t.TempDir()
	if err := projection.RenderAll(s, dir); err != nil {
		t.Fatal(err)
	}
	devDir := filepath.Join(dir, "devboard")
	dataDir := filepath.Join(t.TempDir(), "migration")
	t.Setenv("DEVBOARD_DATA", devDir)
	t.Setenv("WORKLOG_DIR", dir)
	t.Setenv("WORKLOG_MIGRATION_DATA", dataDir)
	if _, stderr := runCLI(t, "migrate", "--dir", dir, "--out", dataDir); strings.Contains(stderr, "error") {
		t.Fatalf("migrate: %s", stderr)
	}
	return dir
}

// ---- criterion 1: wording rules, pure ----

func TestLintVerifyRules(t *testing.T) {
	cases := []struct {
		name string
		cell string
		want string // substring, "" means no warning
	}{
		{"runnable command is clean", "go test ./internal/cli -run TestX", ""},
		{"explicit manual procedure is clean", "manual: open the board and click archive", ""},
		{"pipeline is clean", "grep -n foo x.go | wc -l", ""},
		{"relative script is clean", "./scripts/check.sh", ""},
		{"hedged with or manual", "go test ./x -run TestY or manual", "hedges"},
		{"hedged with plus manual", "unit test + manual", "hedges"},
		{"spot-check", "go test ./x -run TestY, spot-check the output", "hedges"},
		{"category cell", "CLI test", "not re-runnable"},
		{"one word category", "browser", "not re-runnable"},
		{"empty is not warned", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(lintVerify(tc.cell), " | ")
			if tc.want == "" {
				if got != "" {
					t.Errorf("lintVerify(%q) = %q, want no warning", tc.cell, got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("lintVerify(%q) = %q, want it to mention %q", tc.cell, got, tc.want)
			}
		})
	}
}

// ---- criterion 2: zero match warns on pass, naming the pattern ----

func TestVerifyLintZeroMatchWarnsOnPass(t *testing.T) {
	f := &fakeLister{names: nil}
	f.install(t)
	scorecardFixture(t, "go test ./internal/pr/... -run TestGetsNothing")

	out, _, err := runTask(t, "scorecard", "pass", "1", "--id", "tkt")
	if err != nil {
		t.Fatalf("pass: %v", err)
	}
	if !strings.Contains(out, "matches no test") {
		t.Errorf("want a zero-match warning, got:\n%s", out)
	}
	if !strings.Contains(out, "TestGetsNothing") {
		t.Errorf("warning should name the pattern, got:\n%s", out)
	}
	// The warning names the pattern, never a package: -list gives no package
	// attribution, and ./... legitimately expands to packages without the test.
	if strings.Contains(out, "./internal/pr/...") {
		t.Errorf("warning must not name a package, got:\n%s", out)
	}
}

// ---- criterion 3: matched names are printed ----

func TestVerifyLintPrintsMatchedNames(t *testing.T) {
	f := &fakeLister{names: []string{"TestAlpha", "TestBeta"}}
	f.install(t)
	scorecardFixture(t, "go test ./internal/cli -run 'TestAlpha|TestBeta'")

	out, _, err := runTask(t, "scorecard", "pass", "1", "--id", "tkt")
	if err != nil {
		t.Fatalf("pass: %v", err)
	}
	if !strings.Contains(out, "TestAlpha") || !strings.Contains(out, "TestBeta") {
		t.Errorf("want matched names printed, got:\n%s", out)
	}
	if strings.Contains(out, "matches no test") {
		t.Errorf("must not warn when the pattern matched, got:\n%s", out)
	}
}

// ---- criterion 4: add and edit never query the toolchain ----

func TestVerifyLintNoRunnerOnAddOrEdit(t *testing.T) {
	f := &fakeLister{names: []string{"TestAnything"}}
	f.install(t)
	scorecardFixture(t, "go test ./x -run TestX")

	if _, _, err := runTask(t, "scorecard", "add", "c2",
		"--verify", "go test ./x -run TestNope", "--id", "tkt"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, _, err := runTask(t, "scorecard", "edit", "1", "c1",
		"--verify", "go test ./x -run TestAlsoNope", "--id", "tkt"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if f.calls != 0 {
		t.Errorf("toolchain queried %d times on add/edit; the tests do not exist yet at contract time", f.calls)
	}
}

// ---- criterion 5: subtest patterns ----

func TestVerifyLintSubtestPattern(t *testing.T) {
	f := &fakeLister{names: []string{"TestFoo"}}
	f.install(t)
	scorecardFixture(t, "go test ./internal/cli -run 'TestFoo/case_b'")

	out, _, err := runTask(t, "scorecard", "pass", "1", "--id", "tkt")
	if err != nil {
		t.Fatalf("pass: %v", err)
	}
	if f.calls != 1 {
		t.Fatalf("toolchain calls = %d, want 1", f.calls)
	}
	if got := f.pattern[0]; got != "TestFoo" {
		t.Errorf("queried pattern = %q, want %q (-list cannot match subtests)", got, "TestFoo")
	}
	if strings.Contains(out, "matches no test") {
		t.Errorf("a valid subtest pattern must not be flagged, got:\n%s", out)
	}
}

func TestTopLevelPatternKeepsCharacterClasses(t *testing.T) {
	if got := topLevelPattern("TestA[/x]B"); got != "TestA[/x]B" {
		t.Errorf("topLevelPattern = %q, want the slash inside [] preserved", got)
	}
	if got := topLevelPattern("TestA|TestB"); got != "TestA|TestB" {
		t.Errorf("topLevelPattern = %q, want alternation untouched", got)
	}
}

// ---- criterion 6: cannot-evaluate is silent ----

func TestVerifyLintCannotEvaluateIsSilent(t *testing.T) {
	// A build failure, a timeout, a missing toolchain and any non-zero exit
	// all arrive as an error, and none of them means "zero tests matched".
	f := &fakeLister{err: errors.New("exit status 1: # pkg\nsyntax error")}
	f.install(t)
	scorecardFixture(t, "go test ./internal/cli -run TestSomething")

	out, _, err := runTask(t, "scorecard", "pass", "1", "--id", "tkt")
	if err != nil {
		t.Fatalf("pass: %v", err)
	}
	if strings.Contains(out, "matches no test") {
		t.Errorf("must not claim zero matches when it could not evaluate, got:\n%s", out)
	}
	if strings.Contains(out, "syntax error") {
		t.Errorf("compiler output must never reach the user, got:\n%s", out)
	}
}

func TestRunGoTestListSilentWithoutToolchain(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := runGoTestList(t.TempDir(), []string{"./..."}, "TestX"); err == nil {
		t.Error("want an error when go is absent, so the caller stays silent")
	}
}

// ---- criterion 7: working directory resolution ----

func TestVerifyLintResolvesWorkingDir(t *testing.T) {
	t.Run("cwd when its repo matches the task group", func(t *testing.T) {
		f := &fakeLister{names: []string{"TestX"}}
		f.install(t)
		scorecardFixture(t, "go test ./x -run TestX")

		if _, _, err := runTask(t, "scorecard", "pass", "1", "--id", "tkt"); err != nil {
			t.Fatal(err)
		}
		wd, _ := os.Getwd()
		if f.calls != 1 || f.dirs[0] != wd {
			t.Errorf("ran in %v, want cwd %q", f.dirs, wd)
		}
	})

	t.Run("recorded repo_path when the group does not match", func(t *testing.T) {
		f := &fakeLister{names: []string{"TestX"}}
		f.install(t)
		elsewhere := t.TempDir()
		scorecardFixtureWithRepoPath(t, "go test ./x -run TestX", "some-other-repo", elsewhere)
		if _, _, err := runTask(t, "scorecard", "pass", "1", "--id", "tkt", "--force"); err != nil {
			t.Fatal(err)
		}
		if f.calls != 1 || f.dirs[0] != elsewhere {
			t.Errorf("ran in %v, want the recorded repo_path %q", f.dirs, elsewhere)
		}
	})

	t.Run("silent when neither resolves", func(t *testing.T) {
		f := &fakeLister{names: []string{"TestX"}}
		f.install(t)
		gone := filepath.Join(t.TempDir(), "gone")
		scorecardFixtureWithRepoPath(t, "go test ./x -run TestX", "some-other-repo", gone)
		if _, _, err := runTask(t, "scorecard", "pass", "1", "--id", "tkt", "--force"); err != nil {
			t.Fatal(err)
		}
		if f.calls != 0 {
			t.Errorf("queried the toolchain %d times; must not guess a tree", f.calls)
		}
	})
}

// ---- criterion 8: devboard disabled ----

func TestVerifyLintDisabledDevboardIsNoOp(t *testing.T) {
	f := &fakeLister{names: nil}
	f.install(t)
	// TestMain points DEVBOARD_DATA at a nonexistent path; don't override it.
	out, errOut, err := runTask(t, "scorecard", "add", "c1",
		"--verify", "go test ./x -run TestNope", "--id", "tkt")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if f.calls != 0 {
		t.Errorf("spawned %d toolchain queries with devboard disabled", f.calls)
	}
	if !strings.Contains(errOut, "no-op") {
		t.Errorf("want the existing no-op notice, got stderr:\n%s", errOut)
	}
	if strings.Contains(out, "NOTE:") {
		t.Errorf("must not warn about a mutation that did not happen, got:\n%s", out)
	}
}

// ---- criterion 9: epic child path ----

func TestVerifyLintChildPath(t *testing.T) {
	f := &fakeLister{names: nil}
	f.install(t)

	s := memstore.New()
	// Only the epic needs BoardTracked: its own file is what carries the
	// rendered devboard YAML (Scorecard included) that migrate re-derives
	// the store from; a child never gets a file of its own, and every
	// child of a tracked epic nests into it regardless of its own flag.
	epic := &store.Ticket{
		Slug: "epic", Title: "E", Type: store.TypeEpic,
		State: store.StatePending, Section: store.SectionNext,
		Repo: devboard.RepoName(), BoardTracked: true,
	}
	if err := s.PutTicket(epic); err != nil {
		t.Fatal(err)
	}
	if err := s.PutTicket(&store.Ticket{
		Slug: "kid", Title: "K", Type: store.TypeTicket,
		State: store.StateActive, ParentID: epic.ID, Repo: devboard.RepoName(),
		Scorecard: []store.ScoreItem{{Text: "c1", Verify: "go test ./x -run TestNope", Status: "pending"}},
	}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := projection.RenderAll(s, dir); err != nil {
		t.Fatal(err)
	}
	devDir := filepath.Join(dir, "devboard")
	dataDir := filepath.Join(t.TempDir(), "migration")
	t.Setenv("DEVBOARD_DATA", devDir)
	t.Setenv("WORKLOG_DIR", dir)
	t.Setenv("WORKLOG_MIGRATION_DATA", dataDir)
	if _, stderr := runCLI(t, "migrate", "--dir", dir, "--out", dataDir); strings.Contains(stderr, "error") {
		t.Fatalf("migrate: %s", stderr)
	}

	out, _, err := runTask(t, "scorecard", "pass", "1", "--id", "epic", "--child", "kid")
	if err != nil {
		t.Fatalf("pass on child: %v", err)
	}
	if f.calls != 1 {
		t.Fatalf("toolchain calls = %d, want 1 on the child path", f.calls)
	}
	if !strings.Contains(out, "matches no test") {
		t.Errorf("child path must lint identically, got:\n%s", out)
	}
}

// ---- criterion 10: --json stays one document ----

func TestVerifyLintJSONSingleDocument(t *testing.T) {
	f := &fakeLister{names: nil}
	f.install(t)
	scorecardFixture(t, "go test ./x -run TestNope or manual")

	out, _, err := runTask(t, "scorecard", "pass", "1", "--id", "tkt", "--json")
	if err != nil {
		t.Fatalf("pass: %v", err)
	}
	var res struct {
		Action   string   `json:"action"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, out)
	}
	if res.Action == "" {
		t.Errorf("action missing from %s", out)
	}
	if len(res.Warnings) < 2 {
		t.Errorf("want the hedge and zero-match warnings in the array, got %v", res.Warnings)
	}
}

// ---- criterion 11: exit codes ----

func TestVerifyLintExitCodesUnchanged(t *testing.T) {
	f := &fakeLister{names: nil}
	f.install(t)

	t.Run("warning paths still exit 0", func(t *testing.T) {
		scorecardFixture(t, "go test ./x -run TestNope or manual")
		for _, args := range [][]string{
			{"scorecard", "add", "c2", "--verify", "browser", "--id", "tkt"},
			{"scorecard", "edit", "1", "c1", "--verify", "CLI test", "--id", "tkt"},
			{"scorecard", "pass", "1", "--id", "tkt"},
			{"scorecard", "fail", "1", "--id", "tkt"},
			{"scorecard", "pending", "1", "--id", "tkt"},
		} {
			if _, _, err := runTask(t, args...); err != nil {
				t.Errorf("%v: %v", args, err)
			}
		}
	})

	t.Run("usage errors keep exit 64", func(t *testing.T) {
		scorecardFixture(t, "browser")
		_, _, err := runTask(t, "scorecard", "pass", "99", "--id", "tkt")
		ec, ok := err.(exitCoder)
		if !ok || ec.ExitCode() != 64 {
			t.Errorf("out-of-range index exit = %v, want 64", ec)
		}
	})
}
