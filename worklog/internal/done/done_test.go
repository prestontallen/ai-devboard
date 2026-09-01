package done

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/day2day/internal/model"
)

const today = "2026-05-19"

// fixtureWorkdir creates a tempdir with the given files and returns a
// Workdir pointing at it.
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

const standaloneFixture = `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **Repo**: api
  - **Tags**: refactor, auth
  - **Started**: 2026-05-15

## Next

## Someday
`

func TestRunStandaloneFirstOfMonth(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{"WORK.md": standaloneFixture})
	out, err := Run(wd, Inputs{
		ID:      "auth-1",
		Summary: "Shipped the middleware refactor.",
	}, today)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Status != "archived" || out.ID != "auth-1" {
		t.Errorf("output = %+v", out)
	}
	if out.Completed != today {
		t.Errorf("Completed = %q, want %q", out.Completed, today)
	}

	// Archive file created with header + day section + entry
	data, err := os.ReadFile(out.ArchivePath)
	if err != nil {
		t.Fatalf("archive read: %v", err)
	}
	s := string(data)
	for _, want := range []string{
		"# Archive — 2026-05",
		"## 2026-05-19",
		"### auth-1 — Refactor auth",
		"- **Started → Completed**: 2026-05-15 → 2026-05-19",
		"- **Summary**: Shipped the middleware refactor.",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("archive missing %q:\n%s", want, s)
		}
	}

	// Ticket removed from WORK.md
	workData, _ := os.ReadFile(wd.WorkMD())
	if strings.Contains(string(workData), "**AUTH-1**") {
		t.Errorf("WORK.md still contains the archived ticket:\n%s", string(workData))
	}
}

func TestRunStandaloneSecondOfDay(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md": standaloneFixture,
		"archive/2026-05.md": `# Archive — 2026-05

## 2026-05-19

### older-entry — first today
- **Summary**: Older one.
`,
	})
	out, err := Run(wd, Inputs{
		ID:      "auth-1",
		Summary: "Newest entry of the day.",
	}, today)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out.ArchivePath)
	s := string(data)
	iNew := strings.Index(s, "auth-1")
	iOlder := strings.Index(s, "older-entry")
	if iNew < 0 || iOlder < 0 || iNew >= iOlder {
		t.Errorf("expected auth-1 above older-entry in archive:\n%s", s)
	}
}

func TestRunStandaloneNewDay(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md": standaloneFixture,
		"archive/2026-05.md": `# Archive — 2026-05

## 2026-05-18

### yesterday-entry — older
- **Summary**: Yesterday.
`,
	})
	if _, err := Run(wd, Inputs{
		ID:      "auth-1",
		Summary: "First entry today.",
	}, today); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(wd.Root, "archive", "2026-05.md"))
	s := string(data)
	iToday := strings.Index(s, "## 2026-05-19")
	iYday := strings.Index(s, "## 2026-05-18")
	if iToday < 0 || iYday < 0 || iToday >= iYday {
		t.Errorf("expected today's day section above yesterday's:\n%s", s)
	}
}

const childFixture = `## Now
- [~] **CHILD-1** — Do the thing
  - **ID**: child-1
  - **Parent**: epic-a
  - **Started**: 2026-05-15

## Next
- [ ] **EPIC-A** — Cross-cutting epic
  - **ID**: epic-a
  - **Type**: epic
  - **Notes**: notes/epic-a.md
  - **Active children**: child-1

## Someday
`

const childFixtureNotes = `# Epic A

Children:
- [ ] child-1: first child task
- [ ] child-2: second child task
`

func TestRunChildArchivesParentNotesCheckbox(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md":         childFixture,
		"notes/epic-a.md": childFixtureNotes,
	})
	out, err := Run(wd, Inputs{
		ID:      "child-1",
		Summary: "Child shipped.",
	}, today)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Parent != "epic-a" {
		t.Errorf("Parent = %q", out.Parent)
	}
	if out.EpicCompletable {
		t.Errorf("epic still has child-2 open; EpicCompletable should be false")
	}

	// Notes file flipped
	notes, _ := os.ReadFile(wd.NotesFile("epic-a"))
	if !strings.Contains(string(notes), "- [x] child-1: first child task") {
		t.Errorf("expected child-1 flipped to [x]:\n%s", string(notes))
	}
	if !strings.Contains(string(notes), "- [ ] child-2") {
		t.Errorf("expected child-2 untouched:\n%s", string(notes))
	}

	// Active children updated
	work, _ := os.ReadFile(wd.WorkMD())
	if !strings.Contains(string(work), "**Active children**: <none>") {
		t.Errorf("expected Active children = <none> after removing child-1:\n%s", string(work))
	}
	// Ticket removed
	if strings.Contains(string(work), "**CHILD-1**") {
		t.Errorf("ticket should be removed from WORK.md:\n%s", string(work))
	}
}

func TestRunLastChildEpicCompletable(t *testing.T) {
	// Notes file has only one child; archiving it should set EpicCompletable.
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md":         childFixture,
		"notes/epic-a.md": "- [ ] child-1: only child\n",
	})
	out, err := Run(wd, Inputs{
		ID:      "child-1",
		Summary: "Last child done.",
	}, today)
	if err != nil {
		t.Fatal(err)
	}
	if !out.EpicCompletable {
		t.Errorf("expected EpicCompletable=true; output=%+v", out)
	}
}

func TestRunFeedbackBecomesBullets(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{"WORK.md": standaloneFixture})
	out, err := Run(wd, Inputs{
		ID:       "auth-1",
		Summary:  "Done.",
		Feedback: []string{"reviewer asked about backfill", "CockroachDB FK quirk"},
	}, today)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out.ArchivePath)
	s := string(data)
	for _, want := range []string{
		"- **Feedback / Notes**:",
		"  - reviewer asked about backfill",
		"  - CockroachDB FK quirk",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q:\n%s", want, s)
		}
	}
}

func TestRunCompletedDateOverride(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{"WORK.md": standaloneFixture})
	out, err := Run(wd, Inputs{
		ID:        "auth-1",
		Summary:   "Backdated archive.",
		Completed: "2026-04-30",
	}, today)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out.ArchivePath, "/archive/2026-04.md") {
		t.Errorf("expected archive in 2026-04.md, got %s", out.ArchivePath)
	}
	if _, err := os.Stat(filepath.Join(wd.Root, "archive", "2026-04.md")); err != nil {
		t.Errorf("archive file should exist: %v", err)
	}
}

func TestRunNotFound(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{"WORK.md": standaloneFixture})
	_, err := Run(wd, Inputs{ID: "bogus", Summary: "x"}, today)
	if !errors.Is(err, ErrIDNotFound) {
		t.Errorf("expected ErrIDNotFound, got %v", err)
	}
}

func TestRunEpicRefusedWithoutNotesFile(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{"WORK.md": childFixture})
	_, err := Run(wd, Inputs{ID: "epic-a", Summary: "x"}, today)
	if !errors.Is(err, ErrEpicNotesMissing) {
		t.Errorf("expected ErrEpicNotesMissing, got %v", err)
	}
}

func TestRunRequiresSummary(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{"WORK.md": standaloneFixture})
	_, err := Run(wd, Inputs{ID: "auth-1"}, today)
	if !errors.Is(err, ErrSummaryRequired) {
		t.Errorf("expected ErrSummaryRequired, got %v", err)
	}
}

const prFixture = `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **Repo**: api
  - **Tags**: refactor
  - **PR**: https://example.com/pull/77
  - **Started**: 2026-05-15

## Next

## Someday
`

func TestRunArchiveInheritsBlockPR(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{"WORK.md": prFixture})
	out, err := Run(wd, Inputs{
		ID:      "auth-1",
		Summary: "shipped.",
	}, today)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, _ := os.ReadFile(out.ArchivePath)
	s := string(data)
	if !strings.Contains(s, "- **PR**: https://example.com/pull/77") {
		t.Errorf("archive should inherit block PR:\n%s", s)
	}
}

func TestRunArchivePRFlagOverridesBlock(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{"WORK.md": prFixture})
	out, err := Run(wd, Inputs{
		ID:      "auth-1",
		Summary: "shipped.",
		PR:      "https://example.com/pull/override",
	}, today)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, _ := os.ReadFile(out.ArchivePath)
	s := string(data)
	if !strings.Contains(s, "- **PR**: https://example.com/pull/override") {
		t.Errorf("archive should use --pr override:\n%s", s)
	}
	if strings.Contains(s, "pull/77") {
		t.Errorf("override should beat block PR:\n%s", s)
	}
}

func TestRunInvalidDate(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{"WORK.md": standaloneFixture})
	_, err := Run(wd, Inputs{ID: "auth-1", Summary: "x", Completed: "yesterday"}, today)
	if !errors.Is(err, ErrInvalidDate) {
		t.Errorf("expected ErrInvalidDate, got %v", err)
	}
}
