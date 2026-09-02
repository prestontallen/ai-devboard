package reindex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
)

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

func TestReindexEmpty(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md": "## Now\n## Next\n## Someday\n",
	})
	out, err := Run(wd, Inputs{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "regenerated" {
		t.Errorf("Status = %q", out.Status)
	}
	data, _ := os.ReadFile(out.IndexPath)
	s := string(data)
	for _, want := range []string{
		"# Worklog Index",
		"## By ticket",
		"## By tag",
		"## By repo",
		"## By archive month",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q:\n%s", want, s)
		}
	}
	// All four sections should report "(empty)".
	if strings.Count(s, "(empty)") != 4 {
		t.Errorf("expected 4 (empty) sections:\n%s", s)
	}
}

func TestReindexWorkMDOnly(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md": `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **Repo**: api
  - **Tags**: refactor, auth
  - **Started**: 2026-05-15

## Next

## Someday
`,
	})
	out, err := Run(wd, Inputs{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Entries.ByTicket != 1 || out.Entries.ByTag != 2 || out.Entries.ByRepo != 1 {
		t.Errorf("counts wrong: %+v", out.Entries)
	}
	data, _ := os.ReadFile(out.IndexPath)
	s := string(data)
	for _, want := range []string{
		"- auth-1 → WORK.md (Now)",
		"- auth: auth-1",
		"- refactor: auth-1",
		"- api: auth-1",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q:\n%s", want, s)
		}
	}
}

func TestReindexEpicShowsInTicketSection(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md": `## Now
## Next
- [ ] **EPIC-A** — Big epic
  - **ID**: epic-a
  - **Type**: epic
  - **Notes**: notes/epic-a.md
  - **Active children**: <none>
## Someday
`,
	})
	out, err := Run(wd, Inputs{})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out.IndexPath)
	s := string(data)
	if !strings.Contains(s, "- epic-a → WORK.md (Next, epic)") {
		t.Errorf("expected epic to show with status `Next, epic`:\n%s", s)
	}
}

func TestReindexArchiveEntries(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md": "## Now\n## Next\n## Someday\n",
		"archive/2026-05.md": `# Archive — 2026-05

## 2026-05-19

### ent-3794 — Migrate test cases
- **Repo**: assessments-api
- **Tags**: migration, coding-questions
- **Parent**: ent-3634
- **Started → Completed**: 2026-05-15 → 2026-05-19
- **Summary**: Done.
`,
		"archive/2026-04.md": `# Archive — 2026-04

## 2026-04-30

### old-1 — Old ticket
- **Repo**: api
- **Started → Completed**: 2026-04-25 → 2026-04-30
- **Summary**: Old.
`,
	})
	out, err := Run(wd, Inputs{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Entries.ByArchiveMonth != 2 {
		t.Errorf("ByArchiveMonth = %d, want 2", out.Entries.ByArchiveMonth)
	}
	data, _ := os.ReadFile(out.IndexPath)
	s := string(data)
	for _, want := range []string{
		"- ent-3794 → archive/2026-05.md (archived 2026-05-19)",
		"- old-1 → archive/2026-04.md (archived 2026-04-30)",
		"- 2026-05 → archive/2026-05.md (1 entry)",
		"- 2026-04 → archive/2026-04.md (1 entry)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q:\n%s", want, s)
		}
	}
	// Archive months should be reverse-chronological.
	i05 := strings.Index(s, "2026-05 → archive/2026-05.md")
	i04 := strings.Index(s, "2026-04 → archive/2026-04.md")
	if i05 < 0 || i04 < 0 || i05 > i04 {
		t.Errorf("expected 2026-05 above 2026-04 in By archive month:\n%s", s)
	}
}

func TestReindexOpenChildren(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md": `## Now
## Next
- [ ] **EPIC-A** — Epic A
  - **ID**: epic-a
  - **Type**: epic
  - **Notes**: notes/epic-a.md
  - **Active children**: <none>
## Someday
`,
		"notes/epic-a.md": `# Epic A
- [ ] child-1: first child
- [x] child-2: done child (should NOT appear)
- [ ] child-3: third child
`,
	})
	out, err := Run(wd, Inputs{})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out.IndexPath)
	s := string(data)
	for _, want := range []string{
		"- child-1 → notes/epic-a.md (open child of epic-a)",
		"- child-3 → notes/epic-a.md (open child of epic-a)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q:\n%s", want, s)
		}
	}
	// child-2 is [x] in notes — should not appear (it'd be in archive if real)
	if strings.Contains(s, "child-2") {
		t.Errorf("child-2 ([x] in notes) should not appear in INDEX:\n%s", s)
	}
}

func TestReindexDeduplicatesPromotedChild(t *testing.T) {
	// A child that's already in WORK.md (because it was started) should
	// appear once — as the WORK.md record, not as the notes line.
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md": `## Now
- [~] **CHILD-1** — promoted
  - **ID**: child-1
  - **Parent**: epic-a
  - **Started**: 2026-05-15

## Next
- [ ] **EPIC-A** — Epic
  - **ID**: epic-a
  - **Type**: epic
  - **Notes**: notes/epic-a.md
  - **Active children**: child-1

## Someday
`,
		"notes/epic-a.md": "- [ ] child-1: still listed here per spec\n",
	})
	out, err := Run(wd, Inputs{})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out.IndexPath)
	s := string(data)
	if strings.Count(s, "child-1 →") != 1 {
		t.Errorf("expected child-1 to appear once:\n%s", s)
	}
	if !strings.Contains(s, "- child-1 → WORK.md (Now)") {
		t.Errorf("expected child-1 from WORK.md, not notes:\n%s", s)
	}
}

func TestReindexDryRunPreservesFile(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md":  "## Now\n## Next\n## Someday\n",
		"INDEX.md": "ORIGINAL CONTENT — should not be replaced",
	})
	out, err := Run(wd, Inputs{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "would-regenerate" {
		t.Errorf("Status = %q, want would-regenerate", out.Status)
	}
	if out.Content == "" {
		t.Errorf("DryRun should populate Content")
	}
	if !strings.Contains(out.Content, "# Worklog Index") {
		t.Errorf("Content should look like an INDEX.md:\n%s", out.Content)
	}
	// Original INDEX.md untouched.
	data, _ := os.ReadFile(wd.IndexMD())
	if string(data) != "ORIGINAL CONTENT — should not be replaced" {
		t.Errorf("INDEX.md was modified during DryRun")
	}
}

func TestReindexReplacesExistingIndex(t *testing.T) {
	wd := fixtureWorkdir(t, map[string]string{
		"WORK.md":  "## Now\n## Next\n## Someday\n",
		"INDEX.md": "stale content — should be replaced",
	})
	if _, err := Run(wd, Inputs{}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(wd.IndexMD())
	s := string(data)
	if strings.Contains(s, "stale content") {
		t.Errorf("stale content should have been replaced:\n%s", s)
	}
	if !strings.Contains(s, "# Worklog Index") {
		t.Errorf("missing INDEX.md header:\n%s", s)
	}
}

func TestParseArchiveExtractsFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-05.md")
	body := `# Archive — 2026-05

## 2026-05-19

### auth-1 — Refactor auth middleware
- **Repo**: api
- **Tags**: auth, refactor
- **PR**: https://example.com/pr/1
- **Parent**: epic-a
- **Started → Completed**: 2026-05-15 → 2026-05-19
- **Summary**: Done.

### tiny-1 — Tiny one
- **Started → Completed**: 2026-05-18 → 2026-05-19
- **Summary**: Tiny.
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := ParseArchive(path, "archive/2026-05.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	a := entries[0]
	if a.ID != "auth-1" || a.Title != "Refactor auth middleware" {
		t.Errorf("first entry mismatch: %+v", a)
	}
	if a.Repo != "api" || a.Parent != "epic-a" || a.Completed != "2026-05-19" {
		t.Errorf("metadata wrong: %+v", a)
	}
	if len(a.Tags) != 2 || a.Tags[0] != "auth" {
		t.Errorf("tags wrong: %+v", a.Tags)
	}
}
