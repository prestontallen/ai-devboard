package note

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
)

const testWorkMD = `## Now
- [~] **AUTH-1** — Refactor auth middleware
  - **ID**: auth-1
  - **Repo**: api
  - **Tags**: auth, refactor
  - **PR**:
  - **Started**: 2026-05-15

## Next
- [ ] **EPIC-A** — Cross-cutting refactor
  - **ID**: epic-a
  - **Type**: epic
  - **Notes**: notes/epic-a.md
  - **Active children**: <none>

## Someday
`

const epicScaffold = `# epic-a

## Background

## Children
`

func newWD(t *testing.T) model.Workdir {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "WORK.md"), []byte(testWorkMD), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes", "epic-a.md"), []byte(epicScaffold), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := model.NewWorkdir(root)
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

func TestReadMissingFile(t *testing.T) {
	wd := newWD(t)
	res, err := Read(wd, "auth-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if res.Exists {
		t.Error("expected Exists=false for missing file")
	}
	if len(res.Entries) != 0 {
		t.Errorf("expected empty entries, got %d", len(res.Entries))
	}
}

func TestReadParsesEntries(t *testing.T) {
	wd := newWD(t)
	content := "# Notes — auth-1\n\n## 2026-05-19 14:23\nFirst body\n\n## 2026-05-19 16:01\nSecond body\n"
	if err := os.WriteFile(wd.NotesFile("auth-1"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Read(wd, "auth-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !res.Exists {
		t.Error("expected Exists=true")
	}
	if len(res.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(res.Entries))
	}
	if res.Entries[0].Timestamp != "2026-05-19 14:23" {
		t.Errorf("entry[0].Timestamp = %q", res.Entries[0].Timestamp)
	}
	if res.Entries[0].Body != "First body" {
		t.Errorf("entry[0].Body = %q", res.Entries[0].Body)
	}
	if res.Entries[1].Body != "Second body" {
		t.Errorf("entry[1].Body = %q", res.Entries[1].Body)
	}
}

func TestReadPreservesPreamble(t *testing.T) {
	wd := newWD(t)
	content := "# Notes — auth-1\n\n## 2026-05-19 14:23\nSome note.\n"
	if err := os.WriteFile(wd.NotesFile("auth-1"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Read(wd, "auth-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !strings.Contains(res.Preamble, "# Notes — auth-1") {
		t.Errorf("preamble = %q", res.Preamble)
	}
	if len(res.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(res.Entries))
	}
	if res.Entries[0].Body != "Some note." {
		t.Errorf("entry body = %q", res.Entries[0].Body)
	}
}
