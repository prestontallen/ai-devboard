package standup

import (
	"os"
	"path/filepath"
	"testing"
)

func writeArchiveFile(t *testing.T, content string) (absPath, sourcePath string) {
	t.Helper()
	dir := t.TempDir()
	absPath = filepath.Join(dir, "2026-05.md")
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return absPath, "archive/2026-05.md"
}

func TestParseFileEmpty(t *testing.T) {
	abs, src := writeArchiveFile(t, "")
	entries, err := ParseFile(abs, src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("want 0 entries, got %d", len(entries))
	}
}

func TestParseFileSingleEntry(t *testing.T) {
	content := `## Archive

### DOCS-3 — Clean up README
- **ID**: docs-3
- **Repo**: docs
- **PR**: #42
- **Parent**: epic-a
- **Started → Completed**: 2026-05-18 → 2026-05-19
- **Summary**: Tightened the install steps.
`
	abs, src := writeArchiveFile(t, content)
	entries, err := ParseFile(abs, src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.ID != "docs-3" {
		t.Errorf("ID = %q, want docs-3", e.ID)
	}
	if e.Title != "Clean up README" {
		t.Errorf("Title = %q", e.Title)
	}
	if e.Repo != "docs" {
		t.Errorf("Repo = %q", e.Repo)
	}
	if e.PR != "#42" {
		t.Errorf("PR = %q", e.PR)
	}
	if e.Parent != "epic-a" {
		t.Errorf("Parent = %q", e.Parent)
	}
	if e.Started != "2026-05-18" {
		t.Errorf("Started = %q", e.Started)
	}
	if e.Completed != "2026-05-19" {
		t.Errorf("Completed = %q", e.Completed)
	}
	if e.Summary != "Tightened the install steps." {
		t.Errorf("Summary = %q", e.Summary)
	}
	if e.SourceFile != "archive/2026-05.md" {
		t.Errorf("SourceFile = %q", e.SourceFile)
	}
}

func TestParseFileMultipleEntriesPreservesOrder(t *testing.T) {
	content := `## Archive

### ALPHA-1 — First task
- **Repo**: api
- **Started → Completed**: 2026-05-10 → 2026-05-11
- **Summary**: First summary.

### BETA-2 — Second task
- **Repo**: web
- **Started → Completed**: 2026-05-12 → 2026-05-13
- **Summary**: Second summary.

### GAMMA-3 — Third task
- **Repo**: infra
- **Started → Completed**: 2026-05-14 → 2026-05-15
- **Summary**: Third summary.
`
	abs, src := writeArchiveFile(t, content)
	entries, err := ParseFile(abs, src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("want 3 entries, got %d", len(entries))
	}
	if entries[0].ID != "alpha-1" {
		t.Errorf("entries[0].ID = %q, want alpha-1", entries[0].ID)
	}
	if entries[1].ID != "beta-2" {
		t.Errorf("entries[1].ID = %q, want beta-2", entries[1].ID)
	}
	if entries[2].ID != "gamma-3" {
		t.Errorf("entries[2].ID = %q, want gamma-3", entries[2].ID)
	}
	// Verify metadata doesn't bleed between entries.
	if entries[1].Repo != "web" {
		t.Errorf("entries[1].Repo = %q, want web", entries[1].Repo)
	}
	if entries[2].Summary != "Third summary." {
		t.Errorf("entries[2].Summary = %q", entries[2].Summary)
	}
}
