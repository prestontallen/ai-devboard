package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/parse"
)

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
	live, _, _ := storeWriteFixture(t)
	out, err := invokeWait(t, live, "--json", "kid-live")
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
	if result["id"] != "kid-live" {
		t.Errorf("id = %v, want kid-live", result["id"])
	}

	wd, _ := model.NewWorkdir(live)
	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		t.Fatalf("parse WORK.md: %v", err)
	}
	waiting := doc.Section(model.SectionWaiting)
	if waiting == nil || len(waiting.Blocks) == 0 {
		t.Fatal("## Waiting section not created or empty")
	}
	if waiting.Blocks[0].ID != "kid-live" {
		t.Errorf("first Waiting block = %q, want kid-live", waiting.Blocks[0].ID)
	}
	if waiting.Blocks[0].WaitingSince == "" {
		t.Error("WaitingSince not stamped")
	}
}

func TestWaitCLIUnknownExits1(t *testing.T) {
	live, _, _ := storeWriteFixture(t)
	_, err := invokeWait(t, live, "nope")
	if err == nil {
		t.Error("expected error for unknown ID")
	}
}

func TestStartResumesFromWaiting(t *testing.T) {
	// Set up: park kid-live into Waiting first.
	live, _, _ := storeWriteFixture(t)
	if _, err := invokeWait(t, live, "kid-live"); err != nil {
		t.Fatalf("setup wait: %v", err)
	}

	wd, _ := model.NewWorkdir(live)
	doc, _ := parse.File(wd.WorkMD())
	if doc.Section(model.SectionWaiting) == nil {
		t.Fatal("setup: Waiting section not created")
	}

	// Now resume via worklog start.
	out, err := invokeStartInDir(t, live, "--json", "kid-live")
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
		t.Fatal("kid-live not back in ## Now")
	}
	found := false
	for _, b := range now.Blocks {
		if b.ID == "kid-live" {
			found = true
			if b.WaitingSince != "" {
				t.Errorf("WaitingSince not cleared after resume: %q", b.WaitingSince)
			}
		}
	}
	if !found {
		t.Error("kid-live not found in ## Now after resume")
	}

	// Verify it's gone from Waiting.
	w := doc2.Section(model.SectionWaiting)
	if w != nil {
		for _, b := range w.Blocks {
			if b.ID == "kid-live" {
				t.Error("kid-live still in ## Waiting after resume")
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
