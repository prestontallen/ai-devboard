package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prestontallen/day2day/internal/model"
)

func makeTestWD(t *testing.T, workMD string) model.Workdir {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "WORK.md"), []byte(workMD), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	return model.Workdir{Root: root}
}

func readWorkMD(t *testing.T, wd model.Workdir) string {
	t.Helper()
	data, err := os.ReadFile(wd.WorkMD())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

var emptyWorkMD = "## Now\n\n## Next\n\n## Someday\n"

func TestDecodeSingleObject(t *testing.T) {
	tickets, err := Decode(strings.NewReader(`{"id":"x","title":"t"}`))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(tickets) != 1 {
		t.Fatalf("want 1 ticket, got %d", len(tickets))
	}
	if tickets[0].ID != "x" {
		t.Errorf("ID = %q, want x", tickets[0].ID)
	}
}

func TestDecodeArray(t *testing.T) {
	tickets, err := Decode(strings.NewReader(`[{"id":"a","title":"First"},{"id":"b","title":"Second"}]`))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(tickets) != 2 {
		t.Fatalf("want 2 tickets, got %d", len(tickets))
	}
	if tickets[0].ID != "a" || tickets[1].ID != "b" {
		t.Errorf("IDs = %q, %q", tickets[0].ID, tickets[1].ID)
	}
}

func TestDecodeEmpty(t *testing.T) {
	tickets, err := Decode(strings.NewReader(""))
	if err != nil {
		t.Fatalf("Decode empty: %v", err)
	}
	if len(tickets) != 0 {
		t.Errorf("want 0 tickets, got %d", len(tickets))
	}
}

func TestDecodeInvalidJSON(t *testing.T) {
	_, err := Decode(strings.NewReader("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestImportStandaloneNext(t *testing.T) {
	wd := makeTestWD(t, emptyWorkMD)
	tickets := []Ticket{{ID: "foo-1", Title: "Standalone"}}
	result, err := Import(wd, tickets, Options{})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(result.Imported) != 1 {
		t.Fatalf("Imported = %d, want 1", len(result.Imported))
	}
	if result.Imported[0].ID != "foo-1" {
		t.Errorf("ID = %q", result.Imported[0].ID)
	}
	if result.Imported[0].Section != "next" {
		t.Errorf("Section = %q, want next", result.Imported[0].Section)
	}
	wmd := readWorkMD(t, wd)
	if !strings.Contains(wmd, "foo-1") {
		t.Errorf("expected foo-1 in WORK.md:\n%s", wmd)
	}
	if !strings.Contains(wmd, "## Next") {
		t.Errorf("expected ## Next in WORK.md")
	}
}

func TestImportWithExistingParentEpic(t *testing.T) {
	workMD := `## Now

## Next
- [ ] **EPIC-AUTH** — Auth refactor
  - **ID**: epic-auth
  - **Type**: epic
  - **Active children**: <none>

## Someday
`
	wd := makeTestWD(t, workMD)
	tickets := []Ticket{{ID: "child-1", Title: "Child ticket", Parent: "epic-auth"}}
	result, err := Import(wd, tickets, Options{})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(result.Imported) != 1 {
		t.Fatalf("Imported = %d, want 1", len(result.Imported))
	}
	wmd := readWorkMD(t, wd)
	if !strings.Contains(wmd, "child-1") {
		t.Errorf("expected child-1 in WORK.md:\n%s", wmd)
	}
	if !strings.Contains(wmd, "**Active children**: child-1") {
		t.Errorf("expected epic-auth Active children updated to include child-1:\n%s", wmd)
	}
}

func TestImportRefusesParentMissing(t *testing.T) {
	wd := makeTestWD(t, emptyWorkMD)
	before := readWorkMD(t, wd)
	tickets := []Ticket{{ID: "orphan", Title: "Orphan", Parent: "epic-nope"}}
	result, err := Import(wd, tickets, Options{})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(result.Failed) != 1 {
		t.Fatalf("Failed = %d, want 1", len(result.Failed))
	}
	if !strings.Contains(result.Failed[0].Reason, "not found") {
		t.Errorf("reason = %q, expected 'not found'", result.Failed[0].Reason)
	}
	if readWorkMD(t, wd) != before {
		t.Error("WORK.md was mutated on failure")
	}
}

func TestImportRefusesParentNotEpic(t *testing.T) {
	workMD := `## Now

## Next
- [ ] **NOT-EPIC** — A regular ticket
  - **ID**: not-epic

## Someday
`
	wd := makeTestWD(t, workMD)
	tickets := []Ticket{{ID: "child-x", Title: "Child", Parent: "not-epic"}}
	result, err := Import(wd, tickets, Options{})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(result.Failed) != 1 {
		t.Fatalf("Failed = %d, want 1", len(result.Failed))
	}
	if !strings.Contains(result.Failed[0].Reason, "not an epic") {
		t.Errorf("reason = %q, expected 'not an epic'", result.Failed[0].Reason)
	}
}

func TestImportRefusesDuplicateID(t *testing.T) {
	workMD := `## Now

## Next
- [ ] **FOO-1** — Existing
  - **ID**: foo-1

## Someday
`
	wd := makeTestWD(t, workMD)
	tickets := []Ticket{{ID: "foo-1", Title: "Dup"}}
	result, err := Import(wd, tickets, Options{})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(result.Failed) != 1 {
		t.Fatalf("Failed = %d, want 1", len(result.Failed))
	}
	if !strings.Contains(result.Failed[0].Reason, "already exists") {
		t.Errorf("reason = %q, expected 'already exists'", result.Failed[0].Reason)
	}
}

func TestImportNowSetsStartedAndActive(t *testing.T) {
	wd := makeTestWD(t, emptyWorkMD)
	fixedNow, _ := time.ParseInLocation("2006-01-02", "2026-05-20", time.Local)
	tickets := []Ticket{{ID: "now-1", Title: "Active now", Section: "now"}}
	result, err := Import(wd, tickets, Options{Now: fixedNow})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(result.Imported) != 1 {
		t.Fatalf("Imported = %d, want 1", len(result.Imported))
	}
	if result.Imported[0].Section != "now" {
		t.Errorf("Section = %q, want now", result.Imported[0].Section)
	}
	wmd := readWorkMD(t, wd)
	if !strings.Contains(wmd, "[~] **NOW-1**") {
		t.Errorf("expected [~] state in WORK.md:\n%s", wmd)
	}
	if !strings.Contains(wmd, "**Started**: 2026-05-20") {
		t.Errorf("expected Started=2026-05-20 in WORK.md:\n%s", wmd)
	}
}
