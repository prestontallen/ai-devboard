package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func standupFixtureDir(t *testing.T, workMD string, archiveFiles map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "WORK.md"), []byte(workMD), 0o644); err != nil {
		t.Fatal(err)
	}
	if len(archiveFiles) > 0 {
		if err := os.MkdirAll(filepath.Join(root, "archive"), 0o755); err != nil {
			t.Fatal(err)
		}
		for name, content := range archiveFiles {
			if err := os.WriteFile(filepath.Join(root, "archive", name), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

func invokeStandup(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	prev := flagDir
	flagDir = dir
	t.Cleanup(func() { flagDir = prev })

	cmd := newStandupCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestStandupBasicJSON(t *testing.T) {
	workMD := `## Now
- [~] **AUTH-1** — Refactor auth middleware
  - **ID**: auth-1
  - **Repo**: api
  - **PR**: #42
  - **Started**: 2026-05-15

- [~] **PAY-5** — Stripe webhook fixes
  - **ID**: pay-5
  - **Repo**: api
  - **Started**: 2026-05-18

## Waiting
- [ ] **PAY-7** — Provider docs review
  - **ID**: pay-7
  - **Repo**: api
  - **Waiting since**: 2026-05-16

## Next

## Someday
`
	archiveMD := `## Archive

### DOCS-3 — Clean up README
- **Repo**: docs
- **Started → Completed**: 2026-05-18 → 2026-05-19
- **Summary**: Updated the install steps.
`
	dir := standupFixtureDir(t, workMD, map[string]string{"2026-05.md": archiveMD})
	out, err := invokeStandup(t, dir, "--since", "2026-05-19", "--until", "2026-05-19", "--json")
	if err != nil {
		t.Fatalf("invokeStandup: %v\nout: %s", err, out)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	completed, _ := res["completed"].([]any)
	if len(completed) != 1 {
		t.Errorf("completed = %d, want 1", len(completed))
	}
	active, _ := res["active"].([]any)
	if len(active) != 2 {
		t.Errorf("active = %d, want 2", len(active))
	}
	waiting, _ := res["waiting"].([]any)
	if len(waiting) != 1 {
		t.Errorf("waiting = %d, want 1", len(waiting))
	}
	// Verify dates are present.
	if res["today"] == nil {
		t.Error("missing 'today' field")
	}
	if res["since"] == nil {
		t.Error("missing 'since' field")
	}
}

func TestStandupInvalidSince(t *testing.T) {
	dir := standupFixtureDir(t, "## Now\n## Waiting\n## Next\n## Someday\n", nil)
	out, err := invokeStandup(t, dir, "--since", "not-a-date")
	if err == nil {
		t.Fatalf("expected error, got nil\nout: %s", out)
	}
	if ec, ok := err.(exitCoder); !ok || ec.ExitCode() != 64 {
		t.Errorf("expected exit 64, got %v", err)
	}
}

func TestStandupDaysAndSinceConflict(t *testing.T) {
	dir := standupFixtureDir(t, "## Now\n## Waiting\n## Next\n## Someday\n", nil)
	out, err := invokeStandup(t, dir, "--days", "3", "--since", "2026-05-15")
	if err == nil {
		t.Fatalf("expected error, got nil\nout: %s", out)
	}
	if ec, ok := err.(exitCoder); !ok || ec.ExitCode() != 64 {
		t.Errorf("expected exit 64, got %v", err)
	}
}

func TestStandupDaysZero(t *testing.T) {
	dir := standupFixtureDir(t, "## Now\n## Waiting\n## Next\n## Someday\n", nil)
	out, err := invokeStandup(t, dir, "--days", "0")
	if err == nil {
		t.Fatalf("expected error, got nil\nout: %s", out)
	}
	if ec, ok := err.(exitCoder); !ok || ec.ExitCode() != 64 {
		t.Errorf("expected exit 64, got %v", err)
	}
}

func TestStandupSimpleText(t *testing.T) {
	workMD := `## Now
- [~] **AUTH-1** — Refactor auth middleware
  - **ID**: auth-1
  - **Started**: 2026-05-15

## Waiting
- [ ] **PAY-7** — Provider docs review
  - **ID**: pay-7
  - **Waiting since**: 2026-05-16

## Next

## Someday
`
	archiveMD := `## Archive

### DOCS-3 — Clean up README
- **Repo**: docs
- **Started → Completed**: 2026-05-18 → 2026-05-19
- **Summary**: Updated.
`
	dir := standupFixtureDir(t, workMD, map[string]string{"2026-05.md": archiveMD})
	out, err := invokeStandup(t, dir, "--simple", "--since", "2026-05-19", "--until", "2026-05-19")
	if err != nil {
		t.Fatalf("invokeStandup: %v\nout: %s", err, out)
	}
	// No section headings.
	if strings.Contains(out, "## Yesterday") || strings.Contains(out, "## Today") {
		t.Errorf("simple mode should not have section headings:\n%s", out)
	}
	// Prefixes present.
	if !strings.Contains(out, "done:") {
		t.Errorf("expected 'done:' prefix in simple mode:\n%s", out)
	}
	if !strings.Contains(out, "active:") {
		t.Errorf("expected 'active:' prefix in simple mode:\n%s", out)
	}
	if !strings.Contains(out, "waiting:") {
		t.Errorf("expected 'waiting:' prefix in simple mode:\n%s", out)
	}
	// Title line present.
	if !strings.Contains(out, "# Standup") {
		t.Errorf("expected '# Standup' heading:\n%s", out)
	}
}

func TestStandupEmptyJSON(t *testing.T) {
	dir := standupFixtureDir(t, "## Now\n\n## Waiting\n\n## Next\n\n## Someday\n", nil)
	out, err := invokeStandup(t, dir, "--json")
	if err != nil {
		t.Fatalf("invokeStandup: %v\nout: %s", err, out)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	if res["today"] == nil {
		t.Error("missing 'today' field")
	}
	completed, _ := res["completed"].([]any)
	if len(completed) != 0 {
		t.Errorf("completed = %d, want 0", len(completed))
	}
	active, _ := res["active"].([]any)
	if len(active) != 0 {
		t.Errorf("active = %d, want 0", len(active))
	}
	waiting, _ := res["waiting"].([]any)
	if len(waiting) != 0 {
		t.Errorf("waiting = %d, want 0", len(waiting))
	}
}
