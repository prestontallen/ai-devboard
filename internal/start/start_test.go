package start

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/day2day/internal/model"
	"github.com/prestontallen/day2day/internal/parse"
)

const today = "2026-05-19"

// fixtureWorkdir writes the given files (rel-path → content) into a tempdir
// and returns a Workdir pointing at it.
func fixtureWorkdir(t *testing.T, files map[string]string) model.Workdir {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wd, err := model.NewWorkdir(root)
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

const baseFixture = `## Now

## Next
- [ ] **AUTH-1** — Refactor auth middleware
  - **ID**: auth-1
  - **Repo**: api
  - **Tags**: refactor, auth

## Someday
- [ ] **CLEANUP-1** — Tidy logs
  - **ID**: cleanup-1
`

func TestRunStandaloneFromNext(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{"WORK.md": baseFixture})
	out, err := Run(wd, Inputs{ID: "auth-1"}, today)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Status != "started" || out.ID != "auth-1" || out.Section != "Now" {
		t.Errorf("output = %+v", out)
	}
	if out.Started != today {
		t.Errorf("Started = %q, want %q", out.Started, today)
	}

	data, _ := os.ReadFile(wd.WorkMD())
	got := string(data)
	if !strings.Contains(got, "- [~] **AUTH-1** — Refactor auth middleware") {
		t.Errorf("expected [~] block under ## Now:\n%s", got)
	}
	if !strings.Contains(got, "  - **Started**: "+today) {
		t.Errorf("expected Started date on the moved block:\n%s", got)
	}
	// The block should no longer be in ## Next.
	if strings.Contains(got, "## Next\n- [ ] **AUTH-1**") {
		t.Errorf("block still in ## Next after start:\n%s", got)
	}
}

func TestRunStandaloneFromSomeday(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{"WORK.md": baseFixture})
	out, err := Run(wd, Inputs{ID: "cleanup-1"}, today)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.ID != "cleanup-1" || out.Section != "Now" {
		t.Errorf("output = %+v", out)
	}
}

func TestRunStandaloneCapExceeded(t *testing.T) {
	fixture := `## Now
- [~] **T-1** — t1
  - **ID**: t-1
  - **Started**: 2026-05-19
- [~] **T-2** — t2
  - **ID**: t-2
  - **Started**: 2026-05-19
- [~] **T-3** — t3
  - **ID**: t-3
  - **Started**: 2026-05-19
- [~] **T-4** — t4
  - **ID**: t-4
  - **Started**: 2026-05-19
- [~] **T-5** — t5
  - **ID**: t-5
  - **Started**: 2026-05-19

## Next
- [ ] **BLOCKED** — Should not promote
  - **ID**: blocked-1

## Someday
`
	wd := fixtureWorkdir(t, map[string]string{"WORK.md": fixture})
	_, err := Run(wd, Inputs{ID: "blocked-1"}, today)
	if !errors.Is(err, ErrCapExceeded) {
		t.Errorf("expected ErrCapExceeded, got %v", err)
	}
	if !strings.Contains(err.Error(), "5/5") {
		t.Errorf("error should mention 5/5: %v", err)
	}
}

func TestRunChildOfEpic(t *testing.T) {
	work := `## Now

## Next
- [ ] **EPIC-A** — Cross-cutting epic
  - **ID**: epic-a
  - **Type**: epic
  - **Notes**: notes/epic-a.md
  - **Active children**: <none>

## Someday
`
	notes := `# Epic A

Children:
- [ ] child-1: first child task
- [ ] child-2: second child task
`
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md":          work,
		"notes/epic-a.md":  notes,
	})

	out, err := Run(wd, Inputs{
		ID:         "child-1",
		Repo:       "api",
		Tags:       []string{"backend", "urgent"},
		Acceptance: "PR merged + tests green",
	}, today)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Parent != "epic-a" {
		t.Errorf("Parent = %q, want epic-a", out.Parent)
	}
	if out.Title != "first child task" {
		t.Errorf("Title = %q, want 'first child task'", out.Title)
	}

	data, _ := os.ReadFile(wd.WorkMD())
	got := string(data)
	for _, want := range []string{
		"- [~] **CHILD-1** — first child task",
		"  - **ID**: child-1",
		"  - **Parent**: epic-a",
		"  - **Repo**: api",
		"  - **Tags**: backend, urgent",
		"  - **Acceptance**: PR merged + tests green",
		"  - **Started**: " + today,
		"  - **Active children**: child-1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRunChildOfEpicAppendsToExistingActiveChildren(t *testing.T) {
	work := `## Now

## Next
- [ ] **EPIC-A** — Epic A
  - **ID**: epic-a
  - **Type**: epic
  - **Notes**: notes/epic-a.md
  - **Active children**: pre-existing

## Someday
`
	notes := `- [ ] child-1: first
`
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md":         work,
		"notes/epic-a.md": notes,
	})

	if _, err := Run(wd, Inputs{ID: "child-1"}, today); err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, _ := os.ReadFile(wd.WorkMD())
	if !strings.Contains(string(data), "**Active children**: pre-existing, child-1") {
		t.Errorf("expected child appended to active children list:\n%s", string(data))
	}
}

func TestRunEpicRefused(t *testing.T) {
	work := `## Now
## Next
- [ ] **EPIC-A** — Epic A
  - **ID**: epic-a
  - **Type**: epic
  - **Notes**: notes/epic-a.md
  - **Active children**: <none>
## Someday
`
	notes := `- [ ] child-1: one
- [ ] child-2: two
`
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md":         work,
		"notes/epic-a.md": notes,
	})
	_, err := Run(wd, Inputs{ID: "epic-a"}, today)
	if !errors.Is(err, ErrEpicCannotStart) {
		t.Errorf("expected ErrEpicCannotStart, got %v", err)
	}
	// In this fixture, no children are in Now, so both should appear under "startable children".
	if !strings.Contains(err.Error(), "startable children") {
		t.Errorf("epic-refuse should mention startable children, got %v", err)
	}
	if !strings.Contains(err.Error(), "child-1") || !strings.Contains(err.Error(), "child-2") {
		t.Errorf("epic-refuse should list both children, got %v", err)
	}
}

func TestRunAlreadyActive(t *testing.T) {
	work := `## Now
- [~] **AUTH-1** — In progress
  - **ID**: auth-1
  - **Started**: 2026-05-15
## Next
## Someday
`
	wd := fixtureWorkdir(t, map[string]string{"WORK.md": work})
	_, err := Run(wd, Inputs{ID: "auth-1"}, today)
	if !errors.Is(err, ErrAlreadyStarted) {
		t.Errorf("expected ErrAlreadyStarted, got %v", err)
	}
}

func TestRunEpicRefusedWithInflightChildren(t *testing.T) {
	work := `## Now
- [~] **CHILD-1** — already started
  - **ID**: child-1
  - **Parent**: epic-a
  - **Started**: 2026-05-15
## Next
- [ ] **EPIC-A** — Epic A
  - **ID**: epic-a
  - **Type**: epic
  - **Notes**: notes/epic-a.md
  - **Active children**: child-1
## Someday
`
	notes := `- [ ] child-1: in flight
- [ ] child-2: still open
`
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md":         work,
		"notes/epic-a.md": notes,
	})
	_, err := Run(wd, Inputs{ID: "epic-a"}, today)
	if !errors.Is(err, ErrEpicCannotStart) {
		t.Fatalf("expected ErrEpicCannotStart, got %v", err)
	}
	// child-1 should be filtered out (it's in Now); child-2 should remain.
	if !strings.Contains(err.Error(), "child-2") {
		t.Errorf("expected startable list to include child-2, got %v", err)
	}
	if strings.Contains(err.Error(), "startable children: child-1") ||
		strings.Contains(err.Error(), "child-1, child-2") {
		t.Errorf("expected child-1 (in-flight) to be filtered from startable list, got %v", err)
	}
}

func TestRunEpicRefusedAllInFlight(t *testing.T) {
	work := `## Now
- [~] **CHILD-1** — c1
  - **ID**: child-1
  - **Parent**: epic-a
  - **Started**: 2026-05-15
- [~] **CHILD-2** — c2
  - **ID**: child-2
  - **Parent**: epic-a
  - **Started**: 2026-05-15
## Next
- [ ] **EPIC-A** — Epic A
  - **ID**: epic-a
  - **Type**: epic
  - **Notes**: notes/epic-a.md
  - **Active children**: child-1, child-2
## Someday
`
	notes := `- [ ] child-1: c1
- [ ] child-2: c2
`
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md":         work,
		"notes/epic-a.md": notes,
	})
	_, err := Run(wd, Inputs{ID: "epic-a"}, today)
	if !errors.Is(err, ErrEpicCannotStart) {
		t.Fatal(err)
	}
	if !strings.Contains(err.Error(), "all in progress") {
		t.Errorf("expected 'all in progress' suffix, got %v", err)
	}
}

func TestRunNotFound(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{"WORK.md": baseFixture})
	_, err := Run(wd, Inputs{ID: "bogus-99"}, today)
	if !errors.Is(err, ErrIDNotFound) {
		t.Errorf("expected ErrIDNotFound, got %v", err)
	}
}

func TestResolveStandalone(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{"WORK.md": baseFixture})
	doc, err := parseFile(wd)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Resolve(wd, doc, "auth-1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Resolution != ResStandalone {
		t.Errorf("Resolution = %v, want ResStandalone", res.Resolution)
	}
}

func TestResolveChildIsCaseInsensitive(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md": `## Now
## Next
- [ ] **EPIC-A** — Epic
  - **ID**: epic-a
  - **Type**: epic
  - **Notes**: notes/epic-a.md
  - **Active children**: <none>
## Someday
`,
		"notes/epic-a.md": "- [ ] CHILD-1: upper case\n",
	})
	doc, err := parseFile(wd)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Resolve(wd, doc, "child-1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Resolution != ResChildOfEpic {
		t.Errorf("Resolution = %v, want ResChildOfEpic", res.Resolution)
	}
	if res.EpicID != "epic-a" {
		t.Errorf("EpicID = %q", res.EpicID)
	}
}

func parseFile(wd model.Workdir) (*model.WorkDoc, error) {
	return parse.File(wd.WorkMD())
}
