package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
)

func amendFixture(t *testing.T, task devboard.Task, slug string) (data, path string) {
	t.Helper()
	data = t.TempDir()
	t.Setenv("DEVBOARD_DATA", data)
	group := filepath.Join(data, devboard.RepoName())
	if err := os.MkdirAll(group, 0o755); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(group, slug+".yaml")
	raw, err := yaml.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return data, path
}

func loadAmendTask(t *testing.T, path string) devboard.Task {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got devboard.Task
	if err := yaml.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

// ---- criterion 1 ----

func TestTaskAmendRequiresComplexity(t *testing.T) {
	amendFixture(t, devboard.Task{Schema: 1, Title: "T", Complexity: "low"}, "tkt")

	_, _, err := runTask(t, "amend", "scope grew", "--why", "w", "--id", "tkt")
	ec, ok := err.(exitCoder)
	if !ok || ec.ExitCode() != 64 {
		t.Fatalf("exit = %v, want 64", ec)
	}
	if !strings.Contains(err.Error(), "--complexity") {
		t.Errorf("error should name the flag: %v", err)
	}

	// Under --json the refusal must still be one parseable document, which
	// cobra's own required-flag machinery would not produce.
	out, _, _ := runTask(t, "amend", "scope grew", "--why", "w", "--id", "tkt", "--json")
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, out)
	}
	if _, has := doc["error"]; !has {
		t.Errorf("want an error key, got %s", out)
	}
}

// ---- criterion 2 ----

func TestTaskAmendRejectionLeavesFileUnchanged(t *testing.T) {
	_, path := amendFixture(t, devboard.Task{Schema: 1, Title: "T", Complexity: "low"}, "tkt")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"amend", "x", "--why", "w", "--id", "tkt"},                          // no --complexity
		{"amend", "x", "--why", "w", "--complexity", "bogus", "--id", "tkt"}, // bad value
	} {
		if _, _, err := runTask(t, args...); err == nil {
			t.Fatalf("%v: expected refusal", args)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Errorf("%v changed the file:\n%s", args, after)
		}
	}
}

// ---- criterion 3 ----

func TestTaskAmendRecordsEntry(t *testing.T) {
	_, path := amendFixture(t, devboard.Task{Schema: 1, Title: "T", Complexity: "low"}, "tkt")

	if _, _, err := runTask(t, "amend", "retargeted to a generic link",
		"--why", "the storage layer had already generalised",
		"--complexity", "high", "--id", "tkt"); err != nil {
		t.Fatalf("amend: %v", err)
	}
	got := loadAmendTask(t, path)
	if len(got.Decision) != 1 {
		t.Fatalf("decisions = %d, want 1", len(got.Decision))
	}
	d := got.Decision[0]
	if d.What != "retargeted to a generic link" {
		t.Errorf("what = %q", d.What)
	}
	if d.Why == "" {
		t.Errorf("why is empty")
	}
	// The cli package has no injectable clock, so assert the shape.
	if len(d.When) != 10 || strings.Count(d.When, "-") != 2 {
		t.Errorf("when = %q, want yyyy-mm-dd", d.When)
	}
	if d.Complexity != "low → high" {
		t.Errorf("complexity transition = %q, want %q", d.Complexity, "low → high")
	}
}

// ---- criterion 4 ----

func TestTaskAmendUpdatesRating(t *testing.T) {
	_, path := amendFixture(t, devboard.Task{Schema: 1, Title: "T", Complexity: "low"}, "tkt")

	if _, _, err := runTask(t, "amend", "scope doubled", "--why", "w",
		"--complexity", "high", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	got := loadAmendTask(t, path)
	if got.Complexity != "high" {
		t.Errorf("complexity = %q, want high", got.Complexity)
	}
	if got.Decision[0].Complexity != "low → high" {
		t.Errorf("transition = %q", got.Decision[0].Complexity)
	}
}

// ---- criterion 5 ----

func TestTaskAmendUnchangedNeverPersisted(t *testing.T) {
	_, path := amendFixture(t, devboard.Task{Schema: 1, Title: "T", Complexity: "medium"}, "tkt")

	if _, _, err := runTask(t, "amend", "wording only", "--why", "w",
		"--complexity", "unchanged", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	got := loadAmendTask(t, path)
	if got.Complexity != "medium" {
		t.Errorf("complexity = %q, want medium preserved", got.Complexity)
	}
	// The sentinel must never reach the field that gates the risk scout.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "complexity:") &&
			strings.Contains(line, "unchanged") && !strings.Contains(line, "(unchanged)") {
			t.Errorf("sentinel leaked into a complexity field: %q", line)
		}
	}
	if got.Decision[0].Complexity != "medium (unchanged)" {
		t.Errorf("transition = %q", got.Decision[0].Complexity)
	}
}

// ---- criterion 6 ----

// Only 5 of 23 contracts carried a rating at all, so "unchanged from nothing"
// is the common case and is exactly the skip this verb exists to prevent.
func TestTaskAmendUnchangedWithoutPriorRating(t *testing.T) {
	_, path := amendFixture(t, devboard.Task{Schema: 1, Title: "T"}, "tkt")
	before, _ := os.ReadFile(path)

	_, _, err := runTask(t, "amend", "x", "--why", "w", "--complexity", "unchanged", "--id", "tkt")
	ec, ok := err.(exitCoder)
	if !ok || ec.ExitCode() != 64 {
		t.Fatalf("exit = %v, want 64", ec)
	}
	if !strings.Contains(err.Error(), "low|medium|high") {
		t.Errorf("error should say what to state instead: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Errorf("refusal changed the file:\n%s", after)
	}
}

// ---- criterion 7 ----

func TestTaskAmendPrintsResyncChecklist(t *testing.T) {
	amendFixture(t, devboard.Task{Schema: 1, Title: "T", Complexity: "low"}, "tkt")

	out, _, err := runTask(t, "amend", "x", "--why", "w", "--complexity", "medium", "--id", "tkt")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"contract file", "worklog edit title", "acceptance",
		"scorecard", "plan", "slug",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("checklist missing %q:\n%s", want, out)
		}
	}
}

// The child form names the notes checkbox line instead, because the roster
// sync rewrites a child's YAML title from it on every sibling start or done.
func TestTaskAmendChecklistNamesNotesLineForChild(t *testing.T) {
	if got := strings.Join(resyncChecklist("kid"), "\n"); !strings.Contains(got, "notes/<epic>.md") {
		t.Errorf("child checklist should name the notes line:\n%s", got)
	}
	if got := strings.Join(resyncChecklist(""), "\n"); strings.Contains(got, "notes/<epic>.md") {
		t.Errorf("plain-ticket checklist should not mention the epic notes line:\n%s", got)
	}
}

// ---- criterion 8 ----

func TestTaskAmendChildPathPersists(t *testing.T) {
	_, path := amendFixture(t, devboard.Task{
		Schema: 1, Title: "E", Type: "epic",
		Children: []devboard.ChildEntry{
			{ID: "kid", Title: "K", State: "active", Complexity: "low"},
			{ID: "sib", Title: "S", State: "active"},
		},
	}, "epic")

	if _, _, err := runTask(t, "amend", "child scope grew", "--why", "w",
		"--complexity", "high", "--id", "epic", "--child", "kid"); err != nil {
		t.Fatalf("amend on child: %v", err)
	}

	child := func() devboard.ChildEntry {
		t.Helper()
		for _, c := range loadAmendTask(t, path).Children {
			if c.ID == "kid" {
				return c
			}
		}
		t.Fatal("child kid missing")
		return devboard.ChildEntry{}
	}

	got := child()
	if len(got.Decision) != 1 || got.Decision[0].Complexity != "low → high" {
		t.Fatalf("child decisions = %+v, want one entry with the transition", got.Decision)
	}
	if got.Complexity != "high" {
		t.Errorf("child complexity = %q, want high", got.Complexity)
	}

	// An unrelated write to a sibling must not clobber it: childWorkView and
	// applyChildWorkView enumerate fields by hand, so a field they forget is
	// silently dropped on the next write-back.
	if _, _, err := runTask(t, "phase", "verify", "--id", "epic", "--child", "sib"); err != nil {
		t.Fatal(err)
	}
	if after := child(); len(after.Decision) != 1 {
		t.Errorf("sibling write dropped the child's amendment: %+v", after.Decision)
	} else if after.Decision[0].Complexity != "low → high" {
		t.Errorf("sibling write lost the transition: %q", after.Decision[0].Complexity)
	}
}

// ---- criterion 9 ----

// task complexity stays the initial-rating path and must not learn the
// amend-only sentinel: it has no old value to keep.
func TestTaskComplexityRejectsUnchanged(t *testing.T) {
	amendFixture(t, devboard.Task{Schema: 1, Title: "T"}, "tkt")

	_, _, err := runTask(t, "complexity", "unchanged", "--id", "tkt")
	ec, ok := err.(exitCoder)
	if !ok || ec.ExitCode() != 64 {
		t.Fatalf("exit = %v, want 64", ec)
	}
	for _, level := range []string{"low", "medium", "high"} {
		if _, _, err := runTask(t, "complexity", level, "--id", "tkt"); err != nil {
			t.Errorf("complexity %s: %v", level, err)
		}
	}
}

// ---- criterion 10 ----

// adb-scout-gate will add an attestation key to this file. Amend must not
// eat a key it does not know about before that ticket lands.
func TestTaskAmendPreservesUnknownKeys(t *testing.T) {
	data := t.TempDir()
	t.Setenv("DEVBOARD_DATA", data)
	group := filepath.Join(data, devboard.RepoName())
	if err := os.MkdirAll(group, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(group, "tkt.yaml")
	if err := os.WriteFile(path, []byte(
		"schema: 1\ntitle: T\ncomplexity: low\nscout: {mode: ran, why: because}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := runTask(t, "amend", "x", "--why", "w",
		"--complexity", "medium", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "scout:") {
		t.Errorf("amend dropped an unknown key:\n%s", raw)
	}
}
