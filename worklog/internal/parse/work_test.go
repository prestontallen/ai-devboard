package parse

import (
	"reflect"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
)

func TestEmpty(t *testing.T) {
	doc, err := Bytes("WORK.md", []byte(""))
	if err != nil {
		t.Fatalf("parse empty: %v", err)
	}
	if len(doc.Sections) != 0 {
		t.Fatalf("expected 0 sections, got %d", len(doc.Sections))
	}
}

func TestThreeEmptySections(t *testing.T) {
	src := "# Worklog\n\n## Now\n\n## Next\n\n## Someday\n"
	doc, err := Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []model.SectionName{model.SectionNow, model.SectionNext, model.SectionSomeday}
	got := make([]model.SectionName, 0, len(doc.Sections))
	for _, s := range doc.Sections {
		got = append(got, s.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sections = %v, want %v", got, want)
	}
	for _, s := range doc.Sections {
		if len(s.Blocks) != 0 {
			t.Errorf("section %s has %d blocks, want 0", s.Name, len(s.Blocks))
		}
	}
}

func TestSingleTicketInNow(t *testing.T) {
	src := `# Worklog
## Now
- [~] **ENT-3794** — Migrate test cases
  - **ID**: ent-3794
  - **Repo**: assessments-api
  - **Tags**: migration, coding-questions
  - **Started**: 2026-05-15
  - **Parent**: ent-3634
## Next
## Someday
`
	doc, err := Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	now := doc.Section(model.SectionNow)
	if now == nil || len(now.Blocks) != 1 {
		t.Fatalf("expected 1 block in Now, got section=%v", now)
	}
	b := now.Blocks[0]
	if b.ID != "ent-3794" {
		t.Errorf("ID = %q, want ent-3794", b.ID)
	}
	if b.State != model.StateActive {
		t.Errorf("State = %q, want %q", b.State, model.StateActive)
	}
	if b.Parent != "ent-3634" {
		t.Errorf("Parent = %q, want ent-3634", b.Parent)
	}
	if b.Started != "2026-05-15" {
		t.Errorf("Started = %q, want 2026-05-15", b.Started)
	}
	if !reflect.DeepEqual(b.Tags, []string{"migration", "coding-questions"}) {
		t.Errorf("Tags = %v", b.Tags)
	}
	if b.Title != "Migrate test cases" {
		t.Errorf("Title = %q", b.Title)
	}
}

func TestEpicWithActiveChildren(t *testing.T) {
	src := `## Next
- [ ] **ENT-3634** — Coding questions overhaul (epic)
  - **ID**: ent-3634
  - **Type**: epic
  - **Notes**: notes/ent-3634.md
  - **Active children**: ent-3794, ent-3795
`
	doc, err := Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	next := doc.Section(model.SectionNext)
	if next == nil || len(next.Blocks) != 1 {
		t.Fatalf("expected 1 block in Next")
	}
	b := next.Blocks[0]
	if !b.IsEpic() {
		t.Errorf("expected epic, got Type=%q", b.Type)
	}
	if !reflect.DeepEqual(b.ActiveChildren, []string{"ent-3794", "ent-3795"}) {
		t.Errorf("ActiveChildren = %v", b.ActiveChildren)
	}
	if b.NotesRef != "notes/ent-3634.md" {
		t.Errorf("NotesRef = %q", b.NotesRef)
	}
}

func TestBlockByID(t *testing.T) {
	src := `## Now
- [~] **A** — first
  - **ID**: a
## Next
- [ ] **B** — second
  - **ID**: b
`
	doc, err := Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if b := doc.BlockByID("A"); b == nil || b.ID != "a" {
		t.Errorf("BlockByID(A) = %+v", b)
	}
	if b := doc.BlockByID("b"); b == nil || b.Section != model.SectionNext {
		t.Errorf("BlockByID(b) section = %+v", b)
	}
}

func TestLineRanges(t *testing.T) {
	src := `## Now
- [ ] **A** — title
  - **ID**: a
- [ ] **B** — title
  - **ID**: b
## Next
`
	doc, err := Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	now := doc.Section(model.SectionNow)
	if got, want := now.Blocks[0].StartLine, 2; got != want {
		t.Errorf("blocks[0].StartLine = %d, want %d", got, want)
	}
	if got, want := now.Blocks[0].EndLine, 3; got != want {
		t.Errorf("blocks[0].EndLine = %d, want %d", got, want)
	}
	if got, want := now.Blocks[1].StartLine, 4; got != want {
		t.Errorf("blocks[1].StartLine = %d, want %d", got, want)
	}
	if got, want := now.Blocks[1].EndLine, 5; got != want {
		t.Errorf("blocks[1].EndLine = %d, want %d", got, want)
	}
}

func TestParsePRNonEmpty(t *testing.T) {
	src := `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **PR**: https://github.com/example/pull/42
  - **Started**: 2026-05-15
`
	doc, err := Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	b := doc.BlockByID("auth-1")
	if b == nil {
		t.Fatal("block auth-1 not found")
	}
	if b.PR != "https://github.com/example/pull/42" {
		t.Errorf("PR = %q", b.PR)
	}
}

func TestParsePREmpty(t *testing.T) {
	src := `## Now
- [ ] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **PR**:
  - **Started**: 2026-05-15
`
	doc, err := Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	b := doc.BlockByID("auth-1")
	if b == nil {
		t.Fatal("block auth-1 not found")
	}
	if b.PR != "" {
		t.Errorf("PR = %q, want empty", b.PR)
	}
}

func TestParsePRMissing(t *testing.T) {
	src := `## Now
- [ ] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **Started**: 2026-05-15
`
	doc, err := Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	b := doc.BlockByID("auth-1")
	if b == nil {
		t.Fatal("block auth-1 not found")
	}
	if b.PR != "" {
		t.Errorf("PR = %q, want empty (backward compat)", b.PR)
	}
}

func TestNonEpicNotesRefRoundTrip(t *testing.T) {
	src := `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **Repo**: api
  - **PR**:
  - **Notes**: notes/auth-1.md
  - **Started**: 2026-05-15

## Next

## Someday
`
	doc, err := Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	b := doc.BlockByID("auth-1")
	if b == nil {
		t.Fatal("block auth-1 not found")
	}
	if b.NotesRef != "notes/auth-1.md" {
		t.Errorf("NotesRef = %q, want notes/auth-1.md", b.NotesRef)
	}
}

func TestTopLevelXTransient(t *testing.T) {
	src := `## Now
- [x] **DONE** — should not linger
  - **ID**: done
`
	doc, err := Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	now := doc.Section(model.SectionNow)
	if now.Blocks[0].State != model.StateDone {
		t.Errorf("State = %q, want %q", now.Blocks[0].State, model.StateDone)
	}
}

func TestWaitingSinceParses(t *testing.T) {
	src := `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **PR**: https://example.com/pull/1
  - **Started**: 2026-05-10
  - **Waiting since**: 2026-05-18

## Next

## Someday
`
	doc, err := Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	now := doc.Section(model.SectionNow)
	if now == nil || len(now.Blocks) == 0 {
		t.Fatal("no blocks in ## Now")
	}
	b := now.Blocks[0]
	if b.WaitingSince != "2026-05-18" {
		t.Errorf("WaitingSince = %q, want 2026-05-18", b.WaitingSince)
	}
}

func TestParseSource(t *testing.T) {
	src := `## Now
- [~] **JIRA-1** — Import from Jira
  - **ID**: jira-1
  - **Source**: https://company.atlassian.net/browse/JIRA-1
## Next
## Someday
`
	doc, err := Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	now := doc.Section(model.SectionNow)
	if now == nil || len(now.Blocks) == 0 {
		t.Fatal("expected block in ## Now")
	}
	b := now.Blocks[0]
	if b.Source != "https://company.atlassian.net/browse/JIRA-1" {
		t.Errorf("Source = %q, want https://company.atlassian.net/browse/JIRA-1", b.Source)
	}
}
