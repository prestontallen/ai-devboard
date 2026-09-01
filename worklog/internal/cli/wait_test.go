package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/day2day/internal/model"
	"github.com/prestontallen/day2day/internal/parse"
)

const waitCLIFixture = `## Now
- [~] **AUTH-1** — Refactor auth middleware
  - **ID**: auth-1
  - **Repo**: api
  - **PR**: https://example.com/pull/42
  - **Started**: 2026-05-10

## Next
- [ ] **DASH-1** — Dashboard redesign
  - **ID**: dash-1
  - **PR**:

## Someday
`

func waitFixtureDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "WORK.md"), []byte(waitCLIFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func invokeWait(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	prev := flagDir
	flagDir = dir
	t.Cleanup(func() { flagDir = prev })

	cmd := newWaitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func invokeStartInDir(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	prev := flagDir
	flagDir = dir
	t.Cleanup(func() { flagDir = prev })

	cmd := newStartCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestWaitCLIBasic(t *testing.T) {
	dir := waitFixtureDir(t)
	out, err := invokeWait(t, dir, "--json", "auth-1")
	if err != nil {
		t.Fatalf("wait: %v (output: %s)", err, out)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("parse JSON: %v\noutput: %s", err, out)
	}
	if result["status"] != "waiting" {
		t.Errorf("status = %v, want waiting", result["status"])
	}
	if result["id"] != "auth-1" {
		t.Errorf("id = %v, want auth-1", result["id"])
	}

	wd, _ := model.NewWorkdir(dir)
	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		t.Fatalf("parse WORK.md: %v", err)
	}
	waiting := doc.Section(model.SectionWaiting)
	if waiting == nil || len(waiting.Blocks) == 0 {
		t.Fatal("## Waiting section not created or empty")
	}
	if waiting.Blocks[0].ID != "auth-1" {
		t.Errorf("first Waiting block = %q, want auth-1", waiting.Blocks[0].ID)
	}
	if waiting.Blocks[0].WaitingSince == "" {
		t.Error("WaitingSince not stamped")
	}
}

func TestWaitCLIUnknownExits1(t *testing.T) {
	dir := waitFixtureDir(t)
	_, err := invokeWait(t, dir, "nope")
	if err == nil {
		t.Error("expected error for unknown ID")
	}
}

func TestStartResumesFromWaiting(t *testing.T) {
	// Set up: park auth-1 into Waiting first.
	dir := waitFixtureDir(t)
	if _, err := invokeWait(t, dir, "auth-1"); err != nil {
		t.Fatalf("setup wait: %v", err)
	}

	wd, _ := model.NewWorkdir(dir)
	doc, _ := parse.File(wd.WorkMD())
	if doc.Section(model.SectionWaiting) == nil {
		t.Fatal("setup: Waiting section not created")
	}

	// Now resume via worklog start.
	out, err := invokeStartInDir(t, dir, "--json", "auth-1")
	if err != nil {
		t.Fatalf("start: %v (output: %s)", err, out)
	}

	var result map[string]any
	if jsonErr := json.Unmarshal([]byte(out), &result); jsonErr != nil {
		t.Fatalf("parse JSON: %v\noutput: %s", jsonErr, out)
	}
	if result["status"] != "resumed" {
		t.Errorf("status = %v, want resumed", result["status"])
	}

	doc2, _ := parse.File(wd.WorkMD())
	now := doc2.Section(model.SectionNow)
	if now == nil || len(now.Blocks) == 0 {
		t.Fatal("auth-1 not back in ## Now")
	}
	found := false
	for _, b := range now.Blocks {
		if b.ID == "auth-1" {
			found = true
			if b.WaitingSince != "" {
				t.Errorf("WaitingSince not cleared after resume: %q", b.WaitingSince)
			}
		}
	}
	if !found {
		t.Error("auth-1 not found in ## Now after resume")
	}

	// Verify it's gone from Waiting.
	w := doc2.Section(model.SectionWaiting)
	if w != nil {
		for _, b := range w.Blocks {
			if b.ID == "auth-1" {
				t.Error("auth-1 still in ## Waiting after resume")
			}
		}
	}
}

func TestStatusTextShowsWaiting(t *testing.T) {
	fixture := `## Now

## Waiting
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **PR**:
  - **Started**: 2026-05-10
  - **Waiting since**: 2026-05-15

## Next

## Someday
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "WORK.md"), []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}

	prev := flagDir
	flagDir = dir
	t.Cleanup(func() { flagDir = prev })

	cmd := newStatusCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Waiting") {
		t.Errorf("output missing Waiting section:\n%s", out)
	}
	if !strings.Contains(out, "AUTH-1") {
		t.Errorf("output missing AUTH-1:\n%s", out)
	}
	if !strings.Contains(out, "days") {
		t.Errorf("output missing age annotation:\n%s", out)
	}
}
