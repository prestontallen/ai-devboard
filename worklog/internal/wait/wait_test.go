package wait

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/parse"
)

const baseFixture = `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **Repo**: api
  - **PR**: https://example.com/pull/1
  - **Started**: 2026-05-10

## Next
- [ ] **DASH-1** — Dashboard redesign
  - **ID**: dash-1
  - **PR**:

## Someday
`

func buildWorkdir(t *testing.T, content string) model.Workdir {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "WORK.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := model.NewWorkdir(root)
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

func readWorkMD(t *testing.T, wd model.Workdir) string {
	t.Helper()
	data, err := os.ReadFile(wd.WorkMD())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func parseWorkMD(t *testing.T, wd model.Workdir) *model.WorkDoc {
	t.Helper()
	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestWaitMovesNowToWaiting(t *testing.T) {
	wd := buildWorkdir(t, baseFixture)
	out, err := Wait(wd, "auth-1", "2026-05-20")
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if out.Status != "waiting" || out.ID != "auth-1" || out.WaitingSince != "2026-05-20" {
		t.Errorf("output = %+v", out)
	}

	doc := parseWorkMD(t, wd)

	if now := doc.Section(model.SectionNow); now != nil {
		for _, b := range now.Blocks {
			if b.ID == "auth-1" {
				t.Error("auth-1 still in ## Now after Wait")
			}
		}
	}

	waiting := doc.Section(model.SectionWaiting)
	if waiting == nil {
		t.Fatal("## Waiting section not created")
	}
	found := false
	for _, b := range waiting.Blocks {
		if b.ID == "auth-1" {
			found = true
			if b.WaitingSince != "2026-05-20" {
				t.Errorf("WaitingSince = %q, want 2026-05-20", b.WaitingSince)
			}
		}
	}
	if !found {
		t.Error("auth-1 not found in ## Waiting")
	}
}

func TestWaitCreatesSection(t *testing.T) {
	wd := buildWorkdir(t, baseFixture)
	if _, err := Wait(wd, "auth-1", "2026-05-20"); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	raw := readWorkMD(t, wd)
	waitIdx := strings.Index(raw, "## Waiting")
	nextIdx := strings.Index(raw, "## Next")
	if waitIdx < 0 {
		t.Fatal("## Waiting not in WORK.md")
	}
	if nextIdx < 0 {
		t.Fatal("## Next not in WORK.md")
	}
	if waitIdx > nextIdx {
		t.Errorf("## Waiting (%d) appears after ## Next (%d)", waitIdx, nextIdx)
	}
}

func TestWaitPreservesExistingSection(t *testing.T) {
	fixture := `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **PR**:
  - **Started**: 2026-05-10

- [~] **AUTH-2** — Write tests
  - **ID**: auth-2
  - **PR**:
  - **Started**: 2026-05-12

## Waiting
- [~] **DASH-1** — Dashboard
  - **ID**: dash-1
  - **PR**:
  - **Started**: 2026-05-01
  - **Waiting since**: 2026-05-18

## Next

## Someday
`
	wd := buildWorkdir(t, fixture)
	if _, err := Wait(wd, "auth-1", "2026-05-20"); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	doc := parseWorkMD(t, wd)
	waiting := doc.Section(model.SectionWaiting)
	if waiting == nil {
		t.Fatal("## Waiting section missing")
	}
	if len(waiting.Blocks) != 2 {
		t.Errorf("## Waiting has %d blocks, want 2", len(waiting.Blocks))
	}
}

func TestWaitUnknownID(t *testing.T) {
	wd := buildWorkdir(t, baseFixture)
	_, err := Wait(wd, "nope", "2026-05-20")
	if err == nil || !strings.Contains(err.Error(), ErrIDNotFound.Error()) {
		t.Errorf("expected ErrIDNotFound, got %v", err)
	}
}

func TestWaitNotInNow(t *testing.T) {
	wd := buildWorkdir(t, baseFixture)
	_, err := Wait(wd, "dash-1", "2026-05-20")
	if err == nil || !strings.Contains(err.Error(), ErrNotInNow.Error()) {
		t.Errorf("expected ErrNotInNow, got %v", err)
	}
}

func TestResumeMovesWaitingToNow(t *testing.T) {
	fixture := `## Now

## Waiting
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **PR**:
  - **Started**: 2026-05-10
  - **Waiting since**: 2026-05-18

## Next

## Someday
`
	wd := buildWorkdir(t, fixture)
	out, err := Resume(wd, "auth-1", "2026-05-20")
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if out.Status != "resumed" || out.ID != "auth-1" || out.Section != "Now" {
		t.Errorf("output = %+v", out)
	}

	doc := parseWorkMD(t, wd)

	now := doc.Section(model.SectionNow)
	if now == nil || len(now.Blocks) != 1 || now.Blocks[0].ID != "auth-1" {
		t.Error("auth-1 not back in ## Now")
	}
	if now.Blocks[0].WaitingSince != "" {
		t.Errorf("WaitingSince not cleared: %q", now.Blocks[0].WaitingSince)
	}

	waiting := doc.Section(model.SectionWaiting)
	if waiting != nil {
		for _, b := range waiting.Blocks {
			if b.ID == "auth-1" {
				t.Error("auth-1 still in ## Waiting after Resume")
			}
		}
	}
}

func TestResumeCapExceeded(t *testing.T) {
	fixture := `## Now
- [~] **T1** — one
  - **ID**: t1
  - **PR**:
  - **Started**: 2026-05-01

- [~] **T2** — two
  - **ID**: t2
  - **PR**:
  - **Started**: 2026-05-01

- [~] **T3** — three
  - **ID**: t3
  - **PR**:
  - **Started**: 2026-05-01

- [~] **T4** — four
  - **ID**: t4
  - **PR**:
  - **Started**: 2026-05-01

- [~] **T5** — five
  - **ID**: t5
  - **PR**:
  - **Started**: 2026-05-01

## Waiting
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **PR**:
  - **Started**: 2026-05-10
  - **Waiting since**: 2026-05-18

## Next

## Someday
`
	wd := buildWorkdir(t, fixture)
	_, err := Resume(wd, "auth-1", "2026-05-20")
	if err == nil || !strings.Contains(err.Error(), ErrCapExceeded.Error()) {
		t.Errorf("expected ErrCapExceeded, got %v", err)
	}
}

func TestResumeUnknownID(t *testing.T) {
	wd := buildWorkdir(t, baseFixture)
	_, err := Resume(wd, "nope", "2026-05-20")
	if err == nil || !strings.Contains(err.Error(), ErrIDNotFound.Error()) {
		t.Errorf("expected ErrIDNotFound, got %v", err)
	}
}

func TestResumeNotInWaiting(t *testing.T) {
	wd := buildWorkdir(t, baseFixture)
	_, err := Resume(wd, "dash-1", "2026-05-20")
	if err == nil || !strings.Contains(err.Error(), ErrNotInWaiting.Error()) {
		t.Errorf("expected ErrNotInWaiting, got %v", err)
	}
}
