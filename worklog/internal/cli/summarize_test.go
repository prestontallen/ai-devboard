package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func summarizeFixtureDir(t *testing.T, workMD string, notes map[string]string) string {
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
	return root
}

func invokeSummarize(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	prev := flagDir
	flagDir = dir
	t.Cleanup(func() { flagDir = prev })

	cmd := newSummarizeCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestSummarizeJSONEmpty(t *testing.T) {
	dir := summarizeFixtureDir(t, "## Now\n\n## Next\n\n## Someday\n", nil)
	out, err := invokeSummarize(t, dir, "--json")
	if err != nil {
		t.Fatalf("invokeSummarize: %v\nout: %s", err, out)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	groups, ok := res["groups"].([]any)
	if !ok {
		t.Fatalf("expected 'groups' array, got %T", res["groups"])
	}
	if len(groups) != 0 {
		t.Errorf("groups = %d, want 0", len(groups))
	}
}

func TestSummarizeJSONShape(t *testing.T) {
	workMD := `## Now
- [~] **AUTH-1** — JWT middleware
  - **ID**: auth-1
  - **Parent**: epic-a
  - **Repo**: api
  - **Tags**: auth
  - **Started**: 2026-05-10

## Next
- [ ] **EPIC-A** — Auth refactor
  - **ID**: epic-a
  - **Type**: epic
  - **Active children**: auth-1

- [ ] **AUTH-2** — Refresh tokens
  - **ID**: auth-2
  - **Parent**: epic-a

## Someday
`
	dir := summarizeFixtureDir(t, workMD, nil)
	out, err := invokeSummarize(t, dir, "--json")
	if err != nil {
		t.Fatalf("invokeSummarize: %v\nout: %s", err, out)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	groups, _ := res["groups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	g, _ := groups[0].(map[string]any)
	if g["kind"] != "epic" {
		t.Errorf("kind = %q, want epic", g["kind"])
	}
	if g["id"] != "epic-a" {
		t.Errorf("id = %q, want epic-a", g["id"])
	}
	rows, _ := g["rows"].([]any)
	if len(rows) != 2 {
		t.Errorf("rows = %d, want 2", len(rows))
	}
	agg, _ := g["aggregate"].(map[string]any)
	if agg["total"] != float64(2) {
		t.Errorf("total = %v, want 2", agg["total"])
	}
	if agg["active"] != float64(1) {
		t.Errorf("active = %v, want 1", agg["active"])
	}
	if agg["notStarted"] != float64(1) {
		t.Errorf("notStarted = %v, want 1", agg["notStarted"])
	}
	if agg["status"] != "On Track" {
		t.Errorf("status = %q, want On Track", agg["status"])
	}
}

func TestSummarizePlainText(t *testing.T) {
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
	dir := summarizeFixtureDir(t, workMD, nil)
	out, err := invokeSummarize(t, dir, "--plain")
	if err != nil {
		t.Fatalf("invokeSummarize: %v\nout: %s", err, out)
	}
	if !strings.Contains(out, "| Item |") {
		t.Errorf("expected markdown table header in output:\n%s", out)
	}
	if !strings.Contains(out, "auth-1") {
		t.Errorf("expected auth-1 in output:\n%s", out)
	}
	if !strings.Contains(out, "On Track") {
		t.Errorf("expected On Track status in output:\n%s", out)
	}
}

func TestSummarizeLimitTruncatesRows(t *testing.T) {
	workMD := `## Now
- [ ] **C-1** — Child one
  - **ID**: c-1
  - **Parent**: epic-a

- [ ] **C-2** — Child two
  - **ID**: c-2
  - **Parent**: epic-a

- [ ] **C-3** — Child three
  - **ID**: c-3
  - **Parent**: epic-a

- [ ] **C-4** — Child four
  - **ID**: c-4
  - **Parent**: epic-a

- [ ] **C-5** — Child five
  - **ID**: c-5
  - **Parent**: epic-a

## Next
- [ ] **EPIC-A** — Some epic
  - **ID**: epic-a
  - **Type**: epic

## Someday
`
	dir := summarizeFixtureDir(t, workMD, nil)
	out, err := invokeSummarize(t, dir, "--json", "--limit", "2")
	if err != nil {
		t.Fatalf("invokeSummarize: %v\nout: %s", err, out)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	groups, _ := res["groups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	g, _ := groups[0].(map[string]any)
	rows, _ := g["rows"].([]any)
	if len(rows) != 2 {
		t.Errorf("rows = %d after --limit 2, want 2", len(rows))
	}
	// Aggregate counts are computed from the full set before truncation.
	agg, _ := g["aggregate"].(map[string]any)
	if agg["total"] != float64(5) {
		t.Errorf("aggregate.total = %v, want 5 (full set)", agg["total"])
	}
}
