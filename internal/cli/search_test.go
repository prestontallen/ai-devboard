package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const searchCLIWorkMD = `## Now
- [~] **AUTH-1** — Refactor auth middleware
  - **ID**: auth-1
  - **Tags**: auth, refactor
  - **Started**: 2026-05-15

## Next

## Someday
`

func searchFixtureDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "WORK.md"), []byte(searchCLIWorkMD), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func invokeSearch(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	prev := flagDir
	flagDir = dir
	t.Cleanup(func() { flagDir = prev })

	cmd := newSearchCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestSearchCLIAllOfPositionalConflict(t *testing.T) {
	dir := searchFixtureDir(t)
	_, err := invokeSearch(t, dir, "auth", "--all-of", "refactor")
	if err == nil {
		t.Fatal("expected error for positional + --all-of")
	}
	ec, ok := err.(exitCoder)
	if !ok {
		t.Fatalf("expected exitCoder, got %T: %v", err, err)
	}
	if ec.ExitCode() != 64 {
		t.Errorf("exit = %d, want 64", ec.ExitCode())
	}
}

func TestSearchCLIAllOfAnyOfConflict(t *testing.T) {
	dir := searchFixtureDir(t)
	_, err := invokeSearch(t, dir, "--all-of", "auth", "--any-of", "refactor")
	if err == nil {
		t.Fatal("expected error for --all-of + --any-of")
	}
	ec, ok := err.(exitCoder)
	if !ok {
		t.Fatalf("expected exitCoder, got %T: %v", err, err)
	}
	if ec.ExitCode() != 64 {
		t.Errorf("exit = %d, want 64", ec.ExitCode())
	}
}

func TestSearchCLIAllOfEmptyAfterTrim(t *testing.T) {
	dir := searchFixtureDir(t)
	_, err := invokeSearch(t, dir, "--all-of", " , , ")
	if err == nil {
		t.Fatal("expected error for --all-of with only whitespace/commas")
	}
	ec, ok := err.(exitCoder)
	if !ok {
		t.Fatalf("expected exitCoder, got %T: %v", err, err)
	}
	if ec.ExitCode() != 64 {
		t.Errorf("exit = %d, want 64", ec.ExitCode())
	}
}

func TestSearchCLIJSONShapeMultiTerm(t *testing.T) {
	dir := searchFixtureDir(t)
	out, err := invokeSearch(t, dir, "--all-of", "auth,refactor", "--json")
	if err != nil {
		t.Fatalf("invokeSearch: %v\nout: %s", err, out)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	query, ok := res["query"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'query' object, got %T", res["query"])
	}
	if query["mode"] != "all-of" {
		t.Errorf("mode = %q, want 'all-of'", query["mode"])
	}
	terms, ok := query["terms"].([]any)
	if !ok || len(terms) != 2 {
		t.Errorf("terms = %v, want [auth, refactor]", query["terms"])
	}
}
