package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const noteCLIFixture = `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **Repo**: api
  - **Tags**: refactor
  - **PR**:
  - **Started**: 2026-05-15

## Next

## Someday
`

func noteFixtureDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "WORK.md"), []byte(noteCLIFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func invokeNote(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	prev := flagDir
	flagDir = dir
	t.Cleanup(func() { flagDir = prev })

	cmd := newNoteCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestNoteAppendBasic(t *testing.T) {
	dir := noteFixtureDir(t)
	out, err := invokeNote(t, dir, "auth-1", "hello")
	if err != nil {
		t.Fatalf("invokeNote: %v\nout: %s", err, out)
	}
	if !strings.Contains(out, "appended:") {
		t.Errorf("expected 'appended:' in output, got %q", out)
	}
	notesData, err := os.ReadFile(filepath.Join(dir, "notes", "auth-1.md"))
	if err != nil {
		t.Fatalf("notes file missing: %v", err)
	}
	if !strings.Contains(string(notesData), "hello") {
		t.Errorf("notes file missing body:\n%s", string(notesData))
	}
	workData, _ := os.ReadFile(filepath.Join(dir, "WORK.md"))
	if !strings.Contains(string(workData), "  - **Notes**: notes/auth-1.md") {
		t.Errorf("WORK.md missing Notes link:\n%s", string(workData))
	}
}

func TestNoteAppendEmptyBodyExits64(t *testing.T) {
	dir := noteFixtureDir(t)
	_, err := invokeNote(t, dir, "auth-1", "")
	if err == nil {
		t.Fatal("expected error for empty body")
	}
	ec, ok := err.(exitCoder)
	if !ok {
		t.Fatalf("expected exitCoder, got %T: %v", err, err)
	}
	if ec.ExitCode() != 64 {
		t.Errorf("exit = %d, want 64", ec.ExitCode())
	}
}

func TestNoteAppendUnknownIDExits1(t *testing.T) {
	dir := noteFixtureDir(t)
	_, err := invokeNote(t, dir, "nope", "x")
	if err == nil {
		t.Fatal("expected error for unknown ID")
	}
	ec, ok := err.(exitCoder)
	if !ok {
		t.Fatalf("expected exitCoder, got %T: %v", err, err)
	}
	if ec.ExitCode() != 1 {
		t.Errorf("exit = %d, want 1", ec.ExitCode())
	}
}

func TestNoteEditConflictsWithPositional(t *testing.T) {
	dir := noteFixtureDir(t)
	_, err := invokeNote(t, dir, "auth-1", "x", "--edit")
	if err == nil {
		t.Fatal("expected error from positional text + --edit")
	}
	ec, ok := err.(exitCoder)
	if !ok {
		t.Fatalf("expected exitCoder, got %T: %v", err, err)
	}
	if ec.ExitCode() != 64 {
		t.Errorf("exit = %d, want 64", ec.ExitCode())
	}
}

func TestNoteReadEmptyJSON(t *testing.T) {
	dir := noteFixtureDir(t)
	out, err := invokeNote(t, dir, "auth-1", "--json")
	if err != nil {
		t.Fatalf("invokeNote: %v\nout: %s", err, out)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	if res["exists"] != false {
		t.Errorf("exists = %v, want false", res["exists"])
	}
	entries, ok := res["entries"].([]any)
	if !ok || len(entries) != 0 {
		t.Errorf("entries = %v, want []", res["entries"])
	}
	if res["count"] != float64(0) {
		t.Errorf("count = %v, want 0", res["count"])
	}
}

func TestNoteReadAfterAppendJSON(t *testing.T) {
	dir := noteFixtureDir(t)
	if _, err := invokeNote(t, dir, "auth-1", "my note text"); err != nil {
		t.Fatalf("append: %v", err)
	}
	out, err := invokeNote(t, dir, "auth-1", "--json")
	if err != nil {
		t.Fatalf("read: %v\nout: %s", err, out)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	if res["exists"] != true {
		t.Errorf("exists = %v, want true", res["exists"])
	}
	if res["count"] != float64(1) {
		t.Errorf("count = %v, want 1", res["count"])
	}
	entries, ok := res["entries"].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("entries = %v, want 1 entry", res["entries"])
	}
	entry, ok := entries[0].(map[string]any)
	if !ok {
		t.Fatalf("entry not a map: %T", entries[0])
	}
	if entry["body"] != "my note text" {
		t.Errorf("body = %q, want 'my note text'", entry["body"])
	}
}
