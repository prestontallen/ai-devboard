package migrate

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// newLiveFixture builds a minimal but real live-dir-shaped corpus: a
// separate worklog dir and devboard dir (as they are on disk, unlike
// convert's single-root ReadCorpusDir test convention), two tickets, and
// one feedback entry. Callers mutate the returned paths directly to model
// a second live-data state.
func newLiveFixture(t *testing.T) (src Sources, worklogDir, devboardDir string) {
	t.Helper()
	worklogDir = t.TempDir()
	devboardDir = t.TempDir()

	mustWrite(t, filepath.Join(worklogDir, "WORK.md"), workMDFixture("First ticket", "Second ticket"))
	mustWrite(t, filepath.Join(worklogDir, "FEEDBACK.md"), feedbackFixture())
	mustMkdir(t, filepath.Join(devboardDir, "repo"))
	mustWrite(t, filepath.Join(devboardDir, "repo", "solo-a.yaml"), boardFixture("solo-a", "First ticket"))

	return Sources{WorklogDir: worklogDir, DevboardDir: devboardDir}, worklogDir, devboardDir
}

func workMDFixture(titleA, titleB string) string {
	return fmt.Sprintf(`# Work

## Now
- [ ] **SOLO-A** — %s
  - **ID**: solo-a
  - **Repo**: repo

- [ ] **SOLO-B** — %s
  - **ID**: solo-b
  - **Repo**: repo

## Next
## Someday
`, titleA, titleB)
}

// workMDFixtureOneTicket drops solo-b — models a ticket that left live
// data (archived or removed) between two migrate runs.
func workMDFixtureOneTicket(titleA string) string {
	return fmt.Sprintf(`# Work

## Now
- [ ] **SOLO-A** — %s
  - **ID**: solo-a
  - **Repo**: repo

## Next
## Someday
`, titleA)
}

func feedbackFixture() string {
	return "# Friction log\n\n## 1000 — missing-feature\n**Trigger**: wanted a verb\n"
}

func boardFixture(slug, title string) string {
	return fmt.Sprintf("schema: 1\nworklog: %s\ntitle: %s\ntype: ticket\nphase: intake\n", slug, title)
}

// duplicateSlugFixture is refused by convert.Load: two tickets under the
// same slug.
func duplicateSlugFixture() string {
	return `# Work

## Now
- [ ] **SOLO-A** — First
  - **ID**: solo-a
  - **Repo**: repo

- [ ] **SOLO-A AGAIN** — Duplicate
  - **ID**: solo-a
  - **Repo**: repo

## Next
## Someday
`
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
