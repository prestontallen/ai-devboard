package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
)

// writeWorkdir builds a temporary worklog dir with the given files.
// files keys are paths relative to the dir; values are file contents.
func writeWorkdir(t *testing.T, files map[string]string) model.Workdir {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", full, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	wd, err := model.NewWorkdir(root)
	if err != nil {
		t.Fatalf("workdir: %v", err)
	}
	return wd
}

func hasViolation(res *Result, check CheckID) bool {
	for _, v := range res.Violations {
		if v.Check == check {
			return true
		}
	}
	return false
}

func TestWorkMDMissing(t *testing.T) {
	wd := writeWorkdir(t, nil)
	res, err := Run(wd)
	if err != nil {
		t.Fatal(err)
	}
	if !res.WorkMDMissing {
		t.Errorf("WorkMDMissing should be true")
	}
	if !hasViolation(res, CheckWorkMDExists) {
		t.Errorf("expected work-md-exists violation")
	}
}

func TestMinimalValid(t *testing.T) {
	wd := writeWorkdir(t, map[string]string{
		"WORK.md": "# Worklog\n## Now\n## Next\n## Someday\n",
	})
	res, err := Run(wd)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 0 {
		t.Errorf("expected 0 violations, got %d: %+v", len(res.Violations), res.Violations)
	}
}

func TestNowCapViolation(t *testing.T) {
	work := "## Now\n"
	for i := 1; i <= 6; i++ {
		work += "- [ ] **T-" + string(rune('0'+i)) + "** — t\n"
	}
	work += "## Next\n## Someday\n"
	wd := writeWorkdir(t, map[string]string{"WORK.md": work})
	res, err := Run(wd)
	if err != nil {
		t.Fatal(err)
	}
	if !hasViolation(res, CheckNowCap) {
		t.Errorf("expected now-cap violation; got %+v", res.Violations)
	}
}

func TestNoTopLevelX(t *testing.T) {
	wd := writeWorkdir(t, map[string]string{
		"WORK.md": "## Now\n- [x] **DONE** — bad\n## Next\n## Someday\n",
	})
	res, err := Run(wd)
	if err != nil {
		t.Fatal(err)
	}
	if !hasViolation(res, CheckNoTopLevelX) {
		t.Errorf("expected no-top-level-x violation; got %+v", res.Violations)
	}
}

func TestStartedOnActive(t *testing.T) {
	wd := writeWorkdir(t, map[string]string{
		"WORK.md": `## Now
- [~] **A** — missing started
  - **ID**: a
## Next
## Someday
`,
	})
	res, err := Run(wd)
	if err != nil {
		t.Fatal(err)
	}
	if !hasViolation(res, CheckStartedOnActive) {
		t.Errorf("expected started-on-active violation; got %+v", res.Violations)
	}
}

func TestNotesFileMissing(t *testing.T) {
	wd := writeWorkdir(t, map[string]string{
		"WORK.md": `## Now
## Next
- [ ] **EPIC** — e (epic)
  - **ID**: epic-1
  - **Type**: epic
  - **Notes**: notes/epic-1.md
## Someday
`,
	})
	res, err := Run(wd)
	if err != nil {
		t.Fatal(err)
	}
	if !hasViolation(res, CheckNotesFileExists) {
		t.Errorf("expected notes-file-exists violation; got %+v", res.Violations)
	}
}

func TestThreePlaceConsistencyAllGood(t *testing.T) {
	wd := writeWorkdir(t, map[string]string{
		"WORK.md": `## Now
- [~] **CHILD-1** — first
  - **ID**: child-1
  - **Parent**: epic-1
  - **Started**: 2026-05-15
## Next
- [ ] **EPIC** — e (epic)
  - **ID**: epic-1
  - **Type**: epic
  - **Notes**: notes/epic-1.md
  - **Active children**: child-1
## Someday
`,
		"notes/epic-1.md": "# Epic\n\n<!-- Notes for epic epic-1. Children are tracked automatically (add --parent\n     epic-1, then worklog start <child-id>); no roster list to maintain here. -->\n\n## Background\n",
	})
	res, err := Run(wd)
	if err != nil {
		t.Fatal(err)
	}
	if hasViolation(res, CheckThreePlaceConsistency) {
		t.Errorf("did not expect 3-place violation; got %+v", res.Violations)
	}
}

func TestThreePlaceConsistencyMissingActive(t *testing.T) {
	wd := writeWorkdir(t, map[string]string{
		"WORK.md": `## Now
- [~] **CHILD-1** — first
  - **ID**: child-1
  - **Parent**: epic-1
  - **Started**: 2026-05-15
## Next
- [ ] **EPIC** — e (epic)
  - **ID**: epic-1
  - **Type**: epic
  - **Notes**: notes/epic-1.md
  - **Active children**: <none>
## Someday
`,
		"notes/epic-1.md": "# Epic\n\n<!-- Notes for epic epic-1. Children are tracked automatically (add --parent\n     epic-1, then worklog start <child-id>); no roster list to maintain here. -->\n\n## Background\n",
	})
	res, err := Run(wd)
	if err != nil {
		t.Fatal(err)
	}
	if !hasViolation(res, CheckThreePlaceConsistency) {
		t.Errorf("expected 3-place violation; got %+v", res.Violations)
	}
}

// An epic's notes file is not a roster. A file naming a DIFFERENT child, or
// naming none at all, says nothing about whether child-1 is parented here —
// the ParentID relation does, and **Active children** renders it. This is the
// case that used to fail for every epic the current scaffold builds.
func TestThreePlaceConsistencyNotesNeedNoRoster(t *testing.T) {
	for name, notes := range map[string]string{
		"scaffold":               "# Epic\n\n<!-- Notes for epic epic-1. Children are tracked automatically (add --parent\n     epic-1, then worklog start <child-id>); no roster list to maintain here. -->\n\n## Background\n",
		"names some other child": "- [ ] child-9\n",
		"empty":                  "",
	} {
		t.Run(name, func(t *testing.T) {
			wd := writeWorkdir(t, map[string]string{
				"WORK.md": `## Now
- [~] **CHILD-1** — first
  - **ID**: child-1
  - **Parent**: epic-1
  - **Started**: 2026-05-15
## Next
- [ ] **EPIC** — e (epic)
  - **ID**: epic-1
  - **Type**: epic
  - **Notes**: notes/epic-1.md
  - **Active children**: child-1
## Someday
`,
				"notes/epic-1.md": notes,
			})
			res, err := Run(wd)
			if err != nil {
				t.Fatal(err)
			}
			if hasViolation(res, CheckThreePlaceConsistency) {
				t.Errorf("did not expect 3-place violation; got %+v", res.Violations)
			}
		})
	}
}

// The file's existence is still the third place: an epic with no notes file
// at all is a real inconsistency, and dropping the roster rule must not take
// this with it.
func TestThreePlaceConsistencyMissingNotesFile(t *testing.T) {
	wd := writeWorkdir(t, map[string]string{
		"WORK.md": `## Now
- [~] **CHILD-1** — first
  - **ID**: child-1
  - **Parent**: epic-1
  - **Started**: 2026-05-15
## Next
- [ ] **EPIC** — e (epic)
  - **ID**: epic-1
  - **Type**: epic
  - **Active children**: child-1
## Someday
`,
	})
	res, err := Run(wd)
	if err != nil {
		t.Fatal(err)
	}
	if !hasViolation(res, CheckThreePlaceConsistency) {
		t.Errorf("expected 3-place violation; got %+v", res.Violations)
	}
}

func TestIndexRefsExist(t *testing.T) {
	wd := writeWorkdir(t, map[string]string{
		"WORK.md":  "## Now\n## Next\n## Someday\n",
		"INDEX.md": "Things\n\n- See archive/2026-05.md\n- See notes/missing.md\n",
		// archive file present:
		"archive/2026-05.md": "# Archive\n",
		// notes file deliberately not created
	})
	res, err := Run(wd)
	if err != nil {
		t.Fatal(err)
	}
	if !hasViolation(res, CheckIndexRefsExist) {
		t.Errorf("expected index-refs-exist violation for notes/missing.md; got %+v", res.Violations)
	}
	// And it should NOT flag the existing archive file:
	for _, v := range res.Violations {
		if v.Check == CheckIndexRefsExist && strings.Contains(v.Message, "2026-05.md") {
			t.Errorf("did not expect violation for existing 2026-05.md; got %v", v)
		}
	}
}
