package summarize

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
)

func makeWD(t *testing.T, workMD string, notes map[string]string) model.Workdir {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "WORK.md"), []byte(workMD), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range notes {
		if err := os.WriteFile(filepath.Join(root, "notes", name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wd, err := model.NewWorkdir(root)
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

func TestBuildEmptyWorkdir(t *testing.T) {
	wd := makeWD(t, "## Now\n\n## Next\n\n## Someday\n", nil)
	s, err := Build(wd)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(s.Groups) != 0 {
		t.Errorf("expected 0 groups, got %d", len(s.Groups))
	}
}

func TestBuildStandaloneOnly(t *testing.T) {
	workMD := `## Now
- [~] **DASH-1** — Dashboard cleanup
  - **ID**: dash-1
  - **Repo**: web
  - **Started**: 2026-05-10

## Next

## Someday
`
	wd := makeWD(t, workMD, nil)
	s, err := Build(wd)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(s.Groups) != 1 {
		t.Fatalf("expected 1 group (Standalone), got %d", len(s.Groups))
	}
	g := s.Groups[0]
	if g.Kind != "standalone" {
		t.Errorf("kind = %q, want standalone", g.Kind)
	}
	if len(g.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(g.Rows))
	}
	if g.Rows[0].ID != "dash-1" {
		t.Errorf("row ID = %q, want dash-1", g.Rows[0].ID)
	}
	if g.Rows[0].Status != "On Track" {
		t.Errorf("status = %q, want On Track", g.Rows[0].Status)
	}
}

func TestBuildEpicWithChildren(t *testing.T) {
	workMD := `## Now
- [~] **AUTH-1** — JWT middleware
  - **ID**: auth-1
  - **Parent**: epic-a
  - **Repo**: api
  - **Started**: 2026-05-10

## Next
- [ ] **EPIC-A** — Auth refactor
  - **ID**: epic-a
  - **Type**: epic
  - **Active children**: auth-1

- [ ] **AUTH-2** — Refresh tokens
  - **ID**: auth-2
  - **Parent**: epic-a
  - **Repo**: api

## Someday
`
	wd := makeWD(t, workMD, nil)
	s, err := Build(wd)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(s.Groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(s.Groups))
	}
	g := s.Groups[0]
	if g.Kind != "epic" || g.ID != "epic-a" {
		t.Errorf("group = {kind:%q id:%q}", g.Kind, g.ID)
	}
	if len(g.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(g.Rows))
	}
	agg := g.Aggregate
	if agg.Total != 2 || agg.Active != 1 || agg.NotStarted != 1 {
		t.Errorf("aggregate = %+v", agg)
	}
}

func TestBuildEpicMixedStates(t *testing.T) {
	workMD := `## Now
- [~] **C-1** — Active child
  - **ID**: c-1
  - **Parent**: epic-b

- [x] **C-2** — Done child
  - **ID**: c-2
  - **Parent**: epic-b

- [ ] **C-3** — Pending child
  - **ID**: c-3
  - **Parent**: epic-b

## Next
- [ ] **EPIC-B** — Some epic
  - **ID**: epic-b
  - **Type**: epic

## Someday
`
	wd := makeWD(t, workMD, nil)
	s, err := Build(wd)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(s.Groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(s.Groups))
	}
	agg := s.Groups[0].Aggregate
	if agg.Total != 3 {
		t.Errorf("total = %d, want 3", agg.Total)
	}
	if agg.Done != 1 {
		t.Errorf("done = %d, want 1", agg.Done)
	}
	if agg.Active != 1 {
		t.Errorf("active = %d, want 1", agg.Active)
	}
	if agg.NotStarted != 1 {
		t.Errorf("notStarted = %d, want 1", agg.NotStarted)
	}
	if agg.PercentDone != 33 {
		t.Errorf("percentDone = %d, want 33", agg.PercentDone)
	}
	if agg.Status != "On Track" {
		t.Errorf("status = %q, want On Track", agg.Status)
	}
}

func TestBuildLastUpdateFromNotes(t *testing.T) {
	workMD := `## Now
- [~] **AUTH-1** — JWT middleware
  - **ID**: auth-1
  - **Parent**: epic-a
  - **Started**: 2026-05-10

## Next
- [ ] **EPIC-A** — Auth refactor
  - **ID**: epic-a
  - **Type**: epic

## Someday
`
	notesContent := "# Notes — auth-1\n\n## 2026-05-19 14:23\nSchema migration applied.\n"
	wd := makeWD(t, workMD, map[string]string{"auth-1.md": notesContent})
	s, err := Build(wd)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(s.Groups) != 1 || len(s.Groups[0].Rows) != 1 {
		t.Fatalf("unexpected groups/rows: %+v", s.Groups)
	}
	row := s.Groups[0].Rows[0]
	if row.LastUpdate != "2026-05-19" {
		t.Errorf("lastUpdate = %q, want 2026-05-19", row.LastUpdate)
	}
}

func TestBuildProgressNoteFromStatus(t *testing.T) {
	workMD := `## Now
- [~] **AUTH-1** — JWT middleware
  - **ID**: auth-1
  - **Parent**: epic-a
  - **Status**: Stage 1 nearly done

## Next
- [ ] **EPIC-A** — Auth refactor
  - **ID**: epic-a
  - **Type**: epic

## Someday
`
	wd := makeWD(t, workMD, nil)
	s, err := Build(wd)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	row := s.Groups[0].Rows[0]
	if row.Note != "Stage 1 nearly done" {
		t.Errorf("note = %q, want 'Stage 1 nearly done'", row.Note)
	}
}

func TestBuildProgressNoteFromLatestNoteBody(t *testing.T) {
	workMD := `## Now
- [~] **AUTH-1** — JWT middleware
  - **ID**: auth-1
  - **Parent**: epic-a

## Next
- [ ] **EPIC-A** — Auth refactor
  - **ID**: epic-a
  - **Type**: epic

## Someday
`
	notesContent := "# Notes — auth-1\n\n## 2026-05-19 14:23\nTests passing; ready for review.\n"
	wd := makeWD(t, workMD, map[string]string{"auth-1.md": notesContent})
	s, err := Build(wd)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	row := s.Groups[0].Rows[0]
	if row.Note != "Tests passing; ready for review." {
		t.Errorf("note = %q", row.Note)
	}
}
