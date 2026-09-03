package pr

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
)

func fixtureWorkdir(t *testing.T, work string) model.Workdir {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "WORK.md"), []byte(work), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := model.NewWorkdir(root)
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

const fixtureWithPR = `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **Repo**: api
  - **Tags**: refactor
  - **PR**: https://example.com/pull/1
  - **Started**: 2026-05-15

## Next

## Someday
`

const fixtureNoPRLine = `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **Repo**: api
  - **Tags**: refactor
  - **Started**: 2026-05-15

## Next

## Someday
`

func TestGetReturnsCurrentValue(t *testing.T) {
	wd := fixtureWorkdir(t, fixtureWithPR)
	res, err := Get(wd, "auth-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if res.PR != "https://example.com/pull/1" {
		t.Errorf("PR = %q", res.PR)
	}
	if res.ID != "auth-1" {
		t.Errorf("ID = %q", res.ID)
	}
}

func TestGetMissingPRLine(t *testing.T) {
	wd := fixtureWorkdir(t, fixtureNoPRLine)
	res, err := Get(wd, "auth-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if res.PR != "" {
		t.Errorf("PR = %q, want empty", res.PR)
	}
}

func TestGetUnknownID(t *testing.T) {
	wd := fixtureWorkdir(t, fixtureWithPR)
	_, err := Get(wd, "nope")
	if !errors.Is(err, ErrIDNotFound) {
		t.Errorf("expected ErrIDNotFound, got %v", err)
	}
}
