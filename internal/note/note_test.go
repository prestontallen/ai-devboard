package note

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prestontallen/day2day/internal/model"
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

var (
	t1 = time.Date(2026, 5, 19, 14, 23, 0, 0, time.Local)
	t2 = time.Date(2026, 5, 19, 16, 1, 0, 0, time.Local)
	t3 = time.Date(2026, 5, 19, 17, 45, 0, 0, time.Local)
)

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

func TestAppendCreatesFileAndPreamble(t *testing.T) {
	wd := newWD(t)
	res, err := Append(wd, "auth-1", "Started auth refactor.", t1)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if !res.CreatedFile {
		t.Error("expected CreatedFile=true")
	}
	if !res.LinkedInWorkMD {
		t.Error("expected LinkedInWorkMD=true")
	}
	if res.TotalEntries != 1 {
		t.Errorf("TotalEntries = %d, want 1", res.TotalEntries)
	}

	data, err := os.ReadFile(wd.NotesFile("auth-1"))
	if err != nil {
		t.Fatalf("notes file missing: %v", err)
	}
	s := string(data)
	if !strings.HasPrefix(s, "# Notes — auth-1\n") {
		t.Errorf("missing preamble header:\n%q", s)
	}
	if !strings.Contains(s, "## 2026-05-19 14:23") {
		t.Errorf("missing timestamp heading:\n%q", s)
	}
	if !strings.Contains(s, "Started auth refactor.") {
		t.Errorf("missing body:\n%q", s)
	}

	workData, _ := os.ReadFile(wd.WorkMD())
	if !strings.Contains(string(workData), "  - **Notes**: notes/auth-1.md") {
		t.Errorf("WORK.md missing Notes link:\n%s", string(workData))
	}
}

func TestAppendIdempotency(t *testing.T) {
	wd := newWD(t)

	r1, err := Append(wd, "auth-1", "First note", t1)
	if err != nil {
		t.Fatalf("Append 1: %v", err)
	}
	if !r1.CreatedFile || !r1.LinkedInWorkMD {
		t.Errorf("first append: CreatedFile=%v LinkedInWorkMD=%v (both should be true)", r1.CreatedFile, r1.LinkedInWorkMD)
	}

	r2, err := Append(wd, "auth-1", "Second note", t2)
	if err != nil {
		t.Fatalf("Append 2: %v", err)
	}
	if r2.CreatedFile || r2.LinkedInWorkMD {
		t.Errorf("second append: CreatedFile=%v LinkedInWorkMD=%v (both should be false)", r2.CreatedFile, r2.LinkedInWorkMD)
	}

	r3, err := Append(wd, "auth-1", "Third note", t3)
	if err != nil {
		t.Fatalf("Append 3: %v", err)
	}
	if r3.CreatedFile || r3.LinkedInWorkMD {
		t.Errorf("third append: CreatedFile=%v LinkedInWorkMD=%v (both should be false)", r3.CreatedFile, r3.LinkedInWorkMD)
	}
	if r3.TotalEntries != 3 {
		t.Errorf("TotalEntries = %d, want 3", r3.TotalEntries)
	}

	parsed, err := Read(wd, "auth-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(parsed.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(parsed.Entries))
	}
	if parsed.Entries[0].Body != "First note" || parsed.Entries[1].Body != "Second note" || parsed.Entries[2].Body != "Third note" {
		t.Errorf("wrong entry order: %+v", parsed.Entries)
	}
}

func TestAppendEmptyBody(t *testing.T) {
	wd := newWD(t)
	_, err := Append(wd, "auth-1", "  ", t1)
	if !errors.Is(err, ErrEmptyBody) {
		t.Errorf("expected ErrEmptyBody, got %v", err)
	}
}

func TestAppendUnknownID(t *testing.T) {
	wd := newWD(t)
	_, err := Append(wd, "nope", "body", t1)
	if !errors.Is(err, ErrUnknownID) {
		t.Errorf("expected ErrUnknownID, got %v", err)
	}
}

func TestAppendOnEpicPreservesScaffold(t *testing.T) {
	wd := newWD(t)
	res, err := Append(wd, "epic-a", "First epic-level note", t1)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if res.CreatedFile {
		t.Error("expected CreatedFile=false (file already existed)")
	}
	if res.LinkedInWorkMD {
		t.Error("expected LinkedInWorkMD=false (epic already had NotesRef)")
	}

	data, _ := os.ReadFile(wd.NotesFile("epic-a"))
	s := string(data)
	if !strings.Contains(s, "## Background") {
		t.Errorf("scaffold lost ## Background:\n%s", s)
	}
	if !strings.Contains(s, "## Children") {
		t.Errorf("scaffold lost ## Children:\n%s", s)
	}
	if !strings.Contains(s, "First epic-level note") {
		t.Errorf("missing note body:\n%s", s)
	}
	iChildren := strings.Index(s, "## Children")
	iNote := strings.Index(s, "First epic-level note")
	if iNote <= iChildren {
		t.Errorf("note should appear after ## Children:\n%s", s)
	}
}

func TestReadParsesEntries(t *testing.T) {
	wd := newWD(t)
	_, _ = Append(wd, "auth-1", "First body", t1)
	_, _ = Append(wd, "auth-1", "Second body", t2)

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
