package done

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const epicOnlyFixture = `## Now

## Next
- [ ] **EPIC-A** — Cross-cutting epic
  - **ID**: epic-a
  - **Type**: epic
  - **Notes**: notes/epic-a.md
  - **Plan**: two-phase rollout, flag-gated
  - **Active children**: <none>
  - **Status**: winding down

## Someday
`

const allDoneNotes = `# Epic A

## Children
- [x] child-1: first child task
- [x] child-2: second child task
`

func TestRunEpicArchivesWhenChildrenComplete(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md":         epicOnlyFixture,
		"notes/epic-a.md": allDoneNotes,
	})
	out, err := Run(wd, Inputs{ID: "epic-a", Summary: "Epic wrapped."}, today)
	if err != nil {
		t.Fatal(err)
	}
	if out.Type != "epic" || out.Status != "archived" {
		t.Fatalf("out = %+v", out)
	}

	entry, _ := os.ReadFile(out.ArchivePath)
	for _, want := range []string{
		"### epic-a — Cross-cutting epic",
		"- **Type**: epic",
		"- **Completed**: " + today,
		"- **Notes**: notes/epic-a.md",
		"- **Plan**: two-phase rollout, flag-gated",
		"- **Children**: child-1, child-2",
		"- **Summary**: Epic wrapped.",
	} {
		if !strings.Contains(string(entry), want) {
			t.Errorf("archive entry missing %q:\n%s", want, entry)
		}
	}
	if strings.Contains(string(entry), "Started → Completed") {
		t.Error("epic entry must use the Completed-only date form")
	}

	work, _ := os.ReadFile(wd.WorkMD())
	if strings.Contains(string(work), "epic-a") {
		t.Error("epic block still present in WORK.md")
	}
	if _, err := os.Stat(filepath.Join(wd.Root, "notes", "epic-a.md")); err != nil {
		t.Errorf("notes file must remain on disk: %v", err)
	}
}

func TestRunEpicRefusesOpenNotesChildren(t *testing.T) {
	for _, mark := range []string{" ", "~"} {
		notes := "# Epic A\n\n- [x] child-1: done\n- [" + mark + "] child-2: open\n"
		wd := fixtureWorkdir(t, map[string]string{
			"WORK.md":         epicOnlyFixture,
			"notes/epic-a.md": notes,
		})
		before, _ := os.ReadFile(wd.WorkMD())
		_, err := Run(wd, Inputs{ID: "epic-a", Summary: "x"}, today)
		if !errors.Is(err, ErrEpicHasOpenChildren) {
			t.Fatalf("mark %q: expected ErrEpicHasOpenChildren, got %v", mark, err)
		}
		if !strings.Contains(err.Error(), "child-2 (notes") {
			t.Errorf("error must name the open child and source: %v", err)
		}
		after, _ := os.ReadFile(wd.WorkMD())
		if string(before) != string(after) {
			t.Error("refusal must not modify WORK.md")
		}
		if _, err := os.Stat(filepath.Join(wd.Root, "archive")); !os.IsNotExist(err) {
			t.Error("refusal must not create archive files")
		}
	}
}

func TestRunEpicRefusesWorkMDChildren(t *testing.T) {
	// Child recorded ONLY as a WORK.md block (the `import` path) — notes
	// checkboxes alone would say "complete".
	work := `## Now

## Next
- [ ] **EPIC-A** — Cross-cutting epic
  - **ID**: epic-a
  - **Type**: epic
  - **Notes**: notes/epic-a.md
  - **Active children**: <none>
- [ ] **ORPHAN** — Imported child
  - **ID**: orphan-1
  - **Parent**: epic-a

## Someday
`
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md":         work,
		"notes/epic-a.md": allDoneNotes,
	})
	_, err := Run(wd, Inputs{ID: "epic-a", Summary: "x"}, today)
	if !errors.Is(err, ErrEpicHasOpenChildren) {
		t.Fatalf("expected ErrEpicHasOpenChildren, got %v", err)
	}
	if !strings.Contains(err.Error(), "orphan-1 (WORK.md ## Next)") {
		t.Errorf("error must name the WORK.md child and section: %v", err)
	}
}

func TestRunEpicOpenChildrenBeatsSummaryRequired(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md":         epicOnlyFixture,
		"notes/epic-a.md": "# Epic A\n\n- [ ] child-9: still open\n",
	})
	_, err := Run(wd, Inputs{ID: "epic-a"}, today) // no summary AND open child
	if !errors.Is(err, ErrEpicHasOpenChildren) {
		t.Fatalf("open-children refusal must win over summary-required, got %v", err)
	}
}

func TestRunEpicRequiresSummaryWhenCompletable(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md":         epicOnlyFixture,
		"notes/epic-a.md": allDoneNotes,
	})
	_, err := Run(wd, Inputs{ID: "epic-a"}, today)
	if !errors.Is(err, ErrSummaryRequired) {
		t.Fatalf("expected ErrSummaryRequired, got %v", err)
	}
}
