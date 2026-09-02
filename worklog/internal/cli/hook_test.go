package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const hookWorkMD = `## Now
- [~] **ADB-FRICTION-PANEL** — Devboard friction panel: render FEEDBACK.md
  - **ID**: adb-friction-panel
  - **Started**: 2026-09-01

## Waiting
- [ ] **WL-BLOCKED** — Blocked on someone else
  - **ID**: wl-blocked
  - **Waiting since**: 2026-08-26

## Next
- [ ] **SKILL-SLIM** — Slim the skills library
  - **ID**: skill-slim
  - **Type**: epic
  - **Active children**: slim-session-hook
- [ ] **NOLE-DOCKER-NET** — Fix Docker container internet access
  - **ID**: nole-docker-net

## Someday
`

const hookEmptyWorkMD = `## Now

## Next

## Someday
`

func hookFixtureDir(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "WORK.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// invokeHookSessionStart runs the command against dir and returns the parsed
// output plus the error the RunE returned (which must always be nil — a
// SessionStart hook that fails blocks session init).
func invokeHookSessionStart(t *testing.T, dir string) (hookSessionStartOutput, error) {
	t.Helper()
	prev := flagDir
	flagDir = dir
	t.Cleanup(func() { flagDir = prev })

	cmd := newHookSessionStartCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(nil)
	err := cmd.Execute()

	var out hookSessionStartOutput
	if jerr := json.Unmarshal(buf.Bytes(), &out); jerr != nil {
		t.Fatalf("output is not valid JSON: %v\nout: %s", jerr, buf.String())
	}
	return out, err
}

func TestHookSessionStart(t *testing.T) {
	out, err := invokeHookSessionStart(t, hookFixtureDir(t, hookWorkMD))
	if err != nil {
		t.Fatalf("RunE returned %v; a SessionStart hook must not fail", err)
	}
	if out.HookSpecificOutput.HookEventName != "SessionStart" {
		t.Errorf("hookEventName = %q, want SessionStart", out.HookSpecificOutput.HookEventName)
	}
	ctx := out.HookSpecificOutput.AdditionalContext
	if ctx != out.AdditionalContext {
		t.Errorf("nested and top-level additionalContext differ:\n%q\n%q", ctx, out.AdditionalContext)
	}
	for _, want := range []string{
		"worklog: 1 in Now (cap 5)",
		"[~] adb-friction-panel — Devboard friction panel: render FEEDBACK.md",
		"epic skill-slim — active children: slim-session-hook",
		"waiting: 1",
	} {
		if !strings.Contains(ctx, want) {
			t.Errorf("context missing %q\ngot:\n%s", want, ctx)
		}
	}
	// A non-epic Next ticket must not be listed — Next is noise at session
	// start, the epic line is there only to surface in-flight children.
	if strings.Contains(ctx, "nole-docker-net") {
		t.Errorf("context should not list plain Next tickets\ngot:\n%s", ctx)
	}
}

func TestHookSessionStartEmpty(t *testing.T) {
	out, err := invokeHookSessionStart(t, hookFixtureDir(t, hookEmptyWorkMD))
	if err != nil {
		t.Fatalf("RunE returned %v; a SessionStart hook must not fail", err)
	}
	ctx := out.HookSpecificOutput.AdditionalContext
	if !strings.Contains(ctx, "nothing in Now") {
		t.Errorf("empty Now should still report itself, got: %q", ctx)
	}
	if strings.Contains(ctx, "waiting:") {
		t.Errorf("no Waiting section should mean no waiting line, got: %q", ctx)
	}
}

func TestHookSessionStartMissing(t *testing.T) {
	// An empty dir: WORK.md does not exist.
	out, err := invokeHookSessionStart(t, t.TempDir())
	if err != nil {
		t.Fatalf("RunE returned %v; missing WORK.md must not fail the hook", err)
	}
	ctx := out.HookSpecificOutput.AdditionalContext
	if !strings.Contains(ctx, "WORK.md not found") {
		t.Errorf("missing WORK.md should say so, got: %q", ctx)
	}
}

func TestHookSessionStartUnparseable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "WORK.md"), []byte("not a worklog\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := invokeHookSessionStart(t, dir)
	if err != nil {
		t.Fatalf("RunE returned %v; an unreadable WORK.md must not fail the hook", err)
	}
	if out.HookSpecificOutput.HookEventName != "SessionStart" {
		t.Errorf("still expected a well-formed payload, got %+v", out)
	}
}

// TestHookSessionStartNoStdin covers criterion 4: the command is usable by
// hand, with nothing on stdin. drainHookStdin must not block or error.
func TestHookSessionStartNoStdin(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.Close() // immediate EOF — the `echo -n '' |` case
	prev := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = prev; r.Close() })

	if _, err := invokeHookSessionStart(t, hookFixtureDir(t, hookWorkMD)); err != nil {
		t.Fatalf("RunE returned %v with empty stdin", err)
	}
}
