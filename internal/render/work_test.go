package render

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/prestontallen/day2day/internal/model"
	"github.com/prestontallen/day2day/internal/parse"
)

func TestAppendIntoEmptySection(t *testing.T) {
	src := "## Now\n\n## Next\n\n## Someday\n"
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := AppendToSection(doc, model.SectionNext, []string{
		"- [ ] **NEW** — first",
		"  - **ID**: new",
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "- [ ] **NEW** — first") {
		t.Errorf("output missing new block:\n%s", joined)
	}
	// Order: ## Next must appear before the block; ## Someday must appear after.
	iNext := strings.Index(joined, "## Next")
	iBlock := strings.Index(joined, "- [ ] **NEW**")
	iSomeday := strings.Index(joined, "## Someday")
	if !(iNext < iBlock && iBlock < iSomeday) {
		t.Errorf("ordering wrong:\n%s", joined)
	}
}

func TestAppendIntoNonEmptySection(t *testing.T) {
	src := `## Now
## Next
- [ ] **EXISTING** — a
  - **ID**: existing
## Someday
`
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := AppendToSection(doc, model.SectionNext, []string{
		"- [ ] **NEW** — b",
		"  - **ID**: new",
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	joined := strings.Join(out, "\n")
	iExisting := strings.Index(joined, "- [ ] **EXISTING**")
	iNew := strings.Index(joined, "- [ ] **NEW**")
	iSomeday := strings.Index(joined, "## Someday")
	if !(iExisting < iNew && iNew < iSomeday) {
		t.Errorf("expected EXISTING < NEW < ## Someday, got:\n%s", joined)
	}
}

func TestAppendIntoLastSection(t *testing.T) {
	src := "## Someday\n"
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := AppendToSection(doc, model.SectionSomeday, []string{
		"- [ ] **NEW** — at-end",
		"  - **ID**: new",
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "- [ ] **NEW** — at-end") {
		t.Errorf("missing new block:\n%s", joined)
	}
}

func TestRoundTripIdempotent(t *testing.T) {
	src := `# Worklog

## Now
- [~] **A** — first
  - **ID**: a
  - **Repo**: foo

## Next
- [ ] **B** — second
  - **ID**: b

## Someday
`
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Write back the same lines we parsed; the file content should be identical
	// to the input (modulo trailing newline normalization).
	tmpdir := t.TempDir()
	dst := filepath.Join(tmpdir, "WORK.md")
	if err := WriteAtomic(dst, doc.Lines); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != src {
		t.Errorf("round-trip mismatch.\nwant:\n%q\n\ngot:\n%q", src, string(got))
	}
}

func TestFormatTicketBlockMinimal(t *testing.T) {
	got := FormatTicketBlock(BlockOptions{
		Title: "Refactor auth",
		ID:    "auth-1",
	})
	want := []string{
		"- [ ] **AUTH-1** — Refactor auth",
		"  - **ID**: auth-1",
		"  - **PR**: ",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestFormatTicketBlockFull(t *testing.T) {
	got := FormatTicketBlock(BlockOptions{
		Title:      "Migrate test cases",
		ID:         "ent-3794",
		Type:       "ticket",
		Parent:     "ent-3634",
		Repo:       "assessments-api",
		Tags:       []string{"migration", "coding-questions"},
		Started:    "2026-05-15",
		Acceptance: "PR merged, CI green",
		State:      model.StateActive,
	})
	joined := strings.Join(got, "\n")
	for _, want := range []string{
		"- [~] **ENT-3794** — Migrate test cases",
		"  - **ID**: ent-3794",
		"  - **Type**: ticket",
		"  - **Parent**: ent-3634",
		"  - **Repo**: assessments-api",
		"  - **Tags**: migration, coding-questions",
		"  - **Started**: 2026-05-15",
		"  - **Acceptance**: PR merged, CI green",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing line %q in:\n%s", want, joined)
		}
	}
}

func TestRemoveBlock(t *testing.T) {
	src := `## Now
## Next
- [ ] **A** — first
  - **ID**: a
- [ ] **B** — second
  - **ID**: b
## Someday
`
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out, removed, err := RemoveBlock(doc, "a")
	if err != nil {
		t.Fatalf("RemoveBlock: %v", err)
	}
	if removed.ID != "a" {
		t.Errorf("removed.ID = %q, want a", removed.ID)
	}
	joined := strings.Join(out, "\n")
	if strings.Contains(joined, "**A**") {
		t.Errorf("output still contains removed block:\n%s", joined)
	}
	if !strings.Contains(joined, "**B**") {
		t.Errorf("output missing surviving block:\n%s", joined)
	}
	// Section ordering preserved.
	iNext := strings.Index(joined, "## Next")
	iB := strings.Index(joined, "**B**")
	iSomeday := strings.Index(joined, "## Someday")
	if !(iNext < iB && iB < iSomeday) {
		t.Errorf("ordering wrong after remove:\n%s", joined)
	}
}

func TestRemoveBlockNotFound(t *testing.T) {
	doc, _ := parse.Bytes("WORK.md", []byte("## Now\n## Next\n## Someday\n"))
	_, _, err := RemoveBlock(doc, "nope")
	if !errors.Is(err, ErrBlockNotFound) {
		t.Errorf("expected ErrBlockNotFound, got %v", err)
	}
}

func TestUpdateEpicActiveChildrenFromNone(t *testing.T) {
	src := `## Now
## Next
- [ ] **EPIC-A** — Cross-cutting epic
  - **ID**: epic-a
  - **Type**: epic
  - **Notes**: notes/epic-a.md
  - **Active children**: <none>
## Someday
`
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out, err := UpdateEpicActiveChildren(doc, "epic-a", "child-1")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "**Active children**: child-1") {
		t.Errorf("expected `Active children: child-1` in output:\n%s", joined)
	}
	if strings.Contains(joined, "<none>") {
		t.Errorf("expected <none> to be replaced:\n%s", joined)
	}
}

func TestUpdateEpicActiveChildrenAppend(t *testing.T) {
	src := `## Next
- [ ] **EPIC-A** — Epic
  - **ID**: epic-a
  - **Type**: epic
  - **Notes**: notes/epic-a.md
  - **Active children**: child-1, child-2
`
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out, err := UpdateEpicActiveChildren(doc, "epic-a", "child-3")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "**Active children**: child-1, child-2, child-3") {
		t.Errorf("append wrong:\n%s", joined)
	}
}

func TestUpdateEpicActiveChildrenIdempotent(t *testing.T) {
	src := `## Next
- [ ] **EPIC-A** — Epic
  - **ID**: epic-a
  - **Type**: epic
  - **Notes**: notes/epic-a.md
  - **Active children**: child-1, child-2
`
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out, err := UpdateEpicActiveChildren(doc, "epic-a", "child-1")
	if err != nil {
		t.Fatal(err)
	}
	// Should be identical to the original.
	if !reflect.DeepEqual(out, doc.Lines) {
		t.Errorf("idempotent update changed the doc")
	}
}

func TestUpdateEpicActiveChildrenNotEpic(t *testing.T) {
	src := `## Next
- [ ] **NORMAL** — not an epic
  - **ID**: normal
`
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	_, err = UpdateEpicActiveChildren(doc, "normal", "x")
	if !errors.Is(err, ErrBlockNotEpic) {
		t.Errorf("expected ErrBlockNotEpic, got %v", err)
	}
}

func TestUpdateEpicActiveChildrenMissingLine(t *testing.T) {
	src := `## Next
- [ ] **EPIC-X** — Malformed epic without Active children
  - **ID**: epic-x
  - **Type**: epic
  - **Notes**: notes/epic-x.md
`
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	_, err = UpdateEpicActiveChildren(doc, "epic-x", "x")
	if !errors.Is(err, ErrActiveChildrenMissing) {
		t.Errorf("expected ErrActiveChildrenMissing, got %v", err)
	}
}

func TestRemoveFromEpicActiveChildrenLeavesNoneWhenEmpty(t *testing.T) {
	src := `## Next
- [ ] **EPIC-A** — Epic
  - **ID**: epic-a
  - **Type**: epic
  - **Notes**: notes/epic-a.md
  - **Active children**: child-1
`
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out, err := RemoveFromEpicActiveChildren(doc, "epic-a", "child-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(out, "\n"), "**Active children**: <none>") {
		t.Errorf("expected <none>, got:\n%s", strings.Join(out, "\n"))
	}
}

func TestRemoveFromEpicActiveChildrenRemovesMatching(t *testing.T) {
	src := `## Next
- [ ] **EPIC-A** — Epic
  - **ID**: epic-a
  - **Type**: epic
  - **Notes**: notes/epic-a.md
  - **Active children**: child-1, child-2, child-3
`
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out, err := RemoveFromEpicActiveChildren(doc, "epic-a", "child-2")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "**Active children**: child-1, child-3") {
		t.Errorf("expected child-2 removed:\n%s", joined)
	}
}

func TestRemoveFromEpicActiveChildrenIdempotent(t *testing.T) {
	src := `## Next
- [ ] **EPIC-A** — Epic
  - **ID**: epic-a
  - **Type**: epic
  - **Notes**: notes/epic-a.md
  - **Active children**: child-1
`
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	// Remove something not present
	out, err := RemoveFromEpicActiveChildren(doc, "epic-a", "child-99")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(out, "\n"), "**Active children**: child-1") {
		t.Errorf("expected unchanged content")
	}
}

func TestFlipChildCheckboxFound(t *testing.T) {
	in := []byte(`# Epic notes
- [ ] child-1: first
- [ ] child-2: second
`)
	out, found, err := FlipChildCheckbox(in, "child-2")
	if err != nil || !found {
		t.Fatalf("expected found, got found=%v err=%v", found, err)
	}
	s := string(out)
	if !strings.Contains(s, "- [x] child-2: second") {
		t.Errorf("expected child-2 flipped to [x]:\n%s", s)
	}
	if !strings.Contains(s, "- [ ] child-1: first") {
		t.Errorf("expected child-1 untouched:\n%s", s)
	}
}

func TestFlipChildCheckboxAlreadyDone(t *testing.T) {
	in := []byte("- [x] child-1: already done\n")
	out, found, err := FlipChildCheckbox(in, "child-1")
	if err != nil || !found {
		t.Fatalf("expected found+idempotent, got found=%v err=%v", found, err)
	}
	if string(out) != string(in) {
		t.Errorf("idempotent flip should return unchanged content")
	}
}

func TestFlipChildCheckboxNotFound(t *testing.T) {
	in := []byte("- [ ] other-id\n")
	_, found, err := FlipChildCheckbox(in, "missing")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Errorf("expected not found")
	}
}

func TestFlipChildCheckboxCaseInsensitive(t *testing.T) {
	in := []byte("- [ ] CHILD-1: upper\n")
	out, found, err := FlipChildCheckbox(in, "child-1")
	if err != nil || !found {
		t.Fatalf("expected case-insensitive match")
	}
	if !strings.Contains(string(out), "[x]") {
		t.Errorf("expected flip:\n%s", string(out))
	}
}

func TestFormatArchiveEntryAllFields(t *testing.T) {
	got := FormatArchiveEntry(ArchiveOpts{
		ID:        "ent-3794",
		Title:     "Migrate test cases",
		Repo:      "assessments-api",
		Tags:      []string{"migration", "coding-questions"},
		PR:        "https://github.com/foo/pull/1",
		Files:     []string{"a.sql", "b.go"},
		Parent:    "ent-3634",
		Started:   "2026-05-15",
		Completed: "2026-05-19",
		Summary:   "One-line outcome.",
		Feedback:  []string{"first bullet", "second bullet"},
		Time:      "~3h",
	})
	joined := strings.Join(got, "\n")
	for _, want := range []string{
		"### ent-3794 — Migrate test cases",
		"- **Repo**: assessments-api",
		"- **Tags**: migration, coding-questions",
		"- **PR**: https://github.com/foo/pull/1",
		"- **Files**: a.sql, b.go",
		"- **Parent**: ent-3634",
		"- **Started → Completed**: 2026-05-15 → 2026-05-19",
		"- **Summary**: One-line outcome.",
		"- **Feedback / Notes**:",
		"  - first bullet",
		"  - second bullet",
		"- **Time**: ~3h",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
}

func TestFormatArchiveEntryMinimumFields(t *testing.T) {
	got := FormatArchiveEntry(ArchiveOpts{
		ID:        "x-1",
		Title:     "small ticket",
		Started:   "2026-05-19",
		Completed: "2026-05-19",
		Summary:   "Done.",
	})
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "### x-1 — small ticket") {
		t.Errorf("missing heading")
	}
	if strings.Contains(joined, "Feedback / Notes") {
		t.Errorf("Feedback section should be omitted when empty:\n%s", joined)
	}
	if strings.Contains(joined, "**Repo**") || strings.Contains(joined, "**Tags**") {
		t.Errorf("optional fields should be omitted:\n%s", joined)
	}
}

func TestAppendToArchiveCreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-05.md")
	entry := FormatArchiveEntry(ArchiveOpts{
		ID:        "x-1",
		Title:     "First",
		Started:   "2026-05-19",
		Completed: "2026-05-19",
		Summary:   "Done.",
	})
	if err := AppendToArchive(path, entry, "2026-05-19", "2026-05"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{
		"# Archive — 2026-05",
		"## 2026-05-19",
		"### x-1 — First",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
}

func TestAppendToArchiveTopOfExistingDay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-05.md")
	existing := `# Archive — 2026-05

## 2026-05-19

### older-entry — first today
- **Summary**: Older one.
`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := FormatArchiveEntry(ArchiveOpts{
		ID: "newer-entry", Title: "Newer", Started: "2026-05-19", Completed: "2026-05-19", Summary: "Newer.",
	})
	if err := AppendToArchive(path, entry, "2026-05-19", "2026-05"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	s := string(data)
	iNewer := strings.Index(s, "newer-entry")
	iOlder := strings.Index(s, "older-entry")
	if iNewer < 0 || iOlder < 0 || iNewer >= iOlder {
		t.Errorf("expected newer-entry to appear ABOVE older-entry:\n%s", s)
	}
}

func TestAppendToArchiveNewDayPrependedToFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-05.md")
	existing := `# Archive — 2026-05

## 2026-05-18

### yesterday-entry — older
- **Summary**: Yesterday.
`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := FormatArchiveEntry(ArchiveOpts{
		ID: "today-entry", Title: "Today", Started: "2026-05-19", Completed: "2026-05-19", Summary: "Today.",
	})
	if err := AppendToArchive(path, entry, "2026-05-19", "2026-05"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	s := string(data)
	iToday := strings.Index(s, "## 2026-05-19")
	iYesterday := strings.Index(s, "## 2026-05-18")
	if iToday < 0 || iYesterday < 0 || iToday >= iYesterday {
		t.Errorf("expected today's day section ABOVE yesterday's:\n%s", s)
	}
}

func TestFormatEpicBlockAllFields(t *testing.T) {
	got := FormatEpicBlock(EpicBlockOptions{
		ID:             "epic-a",
		Title:          "Cross-cutting refactor",
		Repo:           "api",
		Tags:           []string{"epic", "refactor"},
		NotesRef:       "notes/epic-a.md",
		Plan:           "api/PLAN.md",
		ActiveChildren: []string{"child-1", "child-2"},
		Status:         "Phase 1: in progress",
	})
	joined := strings.Join(got, "\n")
	for _, want := range []string{
		"- [ ] **EPIC-A** — Cross-cutting refactor",
		"  - **ID**: epic-a",
		"  - **Type**: epic",
		"  - **Repo**: api",
		"  - **Tags**: epic, refactor",
		"  - **Notes**: notes/epic-a.md",
		"  - **Plan**: api/PLAN.md",
		"  - **Active children**: child-1, child-2",
		"  - **Status**: Phase 1: in progress",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
}

func TestFormatEpicBlockMinimum(t *testing.T) {
	got := FormatEpicBlock(EpicBlockOptions{
		ID:       "epic-b",
		Title:    "Tiny epic",
		NotesRef: "notes/epic-b.md",
	})
	joined := strings.Join(got, "\n")
	for _, want := range []string{
		"- [ ] **EPIC-B** — Tiny epic",
		"  - **Type**: epic",
		"  - **Notes**: notes/epic-b.md",
		"  - **Active children**: <none>",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "**Repo**") || strings.Contains(joined, "**Tags**") {
		t.Errorf("optional empty fields should be omitted:\n%s", joined)
	}
}

func TestAppendChildToNotesWithChildrenSection(t *testing.T) {
	in := []byte(`# Epic notes

## Background

Some context.

## Children

- [ ] child-1: first
- [ ] child-2: second
`)
	out := AppendChildToNotes(in, "child-3", "third child")
	s := string(out)
	if !strings.Contains(s, "- [ ] child-3: third child") {
		t.Errorf("expected new child line in output:\n%s", s)
	}
	// New entry must come AFTER existing ones.
	iC2 := strings.Index(s, "child-2")
	iC3 := strings.Index(s, "child-3")
	if iC2 < 0 || iC3 < 0 || iC2 > iC3 {
		t.Errorf("expected child-3 after child-2:\n%s", s)
	}
}

func TestAppendChildToNotesEmptyChildrenSection(t *testing.T) {
	in := []byte(`# Epic notes

## Children
`)
	out := AppendChildToNotes(in, "first-child", "the first")
	s := string(out)
	if !strings.Contains(s, "- [ ] first-child: the first") {
		t.Errorf("expected child appended under empty section:\n%s", s)
	}
}

func TestAppendChildToNotesNoChildrenSection(t *testing.T) {
	in := []byte(`# Epic notes

Random preamble.

- [ ] some-other: pre-existing checkbox
`)
	out := AppendChildToNotes(in, "new-child", "added")
	s := string(out)
	if !strings.Contains(s, "- [ ] new-child: added") {
		t.Errorf("expected child appended after last checkbox:\n%s", s)
	}
	iOther := strings.Index(s, "some-other")
	iNew := strings.Index(s, "new-child")
	if iOther < 0 || iNew < 0 || iOther > iNew {
		t.Errorf("expected new-child after some-other:\n%s", s)
	}
}

func TestAppendChildToNotesEmptyFile(t *testing.T) {
	out := AppendChildToNotes(nil, "lone-child", "first ever")
	s := string(out)
	if !strings.Contains(s, "- [ ] lone-child: first ever") {
		t.Errorf("expected child in empty file output: %q", s)
	}
}

func TestFormatTicketBlockRendersEmptyPR(t *testing.T) {
	got := FormatTicketBlock(BlockOptions{
		Title: "Refactor auth",
		ID:    "auth-1",
		Repo:  "api",
		Tags:  []string{"refactor"},
	})
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "\n  - **PR**: \n") && !strings.HasSuffix(joined, "  - **PR**: ") {
		t.Errorf("expected literal `  - **PR**: ` (trailing space) line in:\n%q", joined)
	}
}

func TestFormatTicketBlockFieldOrder(t *testing.T) {
	got := FormatTicketBlock(BlockOptions{
		Title:   "t",
		ID:      "x-1",
		Repo:    "api",
		Tags:    []string{"a"},
		PR:      "url",
		Started: "2026-05-19",
	})
	joined := strings.Join(got, "\n")
	iID := strings.Index(joined, "**ID**")
	iRepo := strings.Index(joined, "**Repo**")
	iTags := strings.Index(joined, "**Tags**")
	iPR := strings.Index(joined, "**PR**")
	iStarted := strings.Index(joined, "**Started**")
	if !(iID < iRepo && iRepo < iTags && iTags < iPR && iPR < iStarted) {
		t.Errorf("field order wrong (want ID < Repo < Tags < PR < Started):\n%s", joined)
	}
}

func TestSetBlockPRRoundTripPreservesValue(t *testing.T) {
	src := `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **Repo**: api
  - **Tags**: refactor
  - **PR**: https://example.com/pull/7
  - **Started**: 2026-05-15
`
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := SetBlockPR(doc, "auth-1", "https://example.com/pull/9")
	if err != nil {
		t.Fatalf("SetBlockPR: %v", err)
	}
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "  - **PR**: https://example.com/pull/9") {
		t.Errorf("expected rewritten PR line:\n%s", joined)
	}
	if strings.Contains(joined, "pull/7") {
		t.Errorf("old value should be replaced:\n%s", joined)
	}
}

func TestSetBlockPREmptyValueKeepsLine(t *testing.T) {
	src := `## Now
- [ ] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **Repo**: api
  - **Tags**: refactor
  - **PR**: https://example.com/pull/7
  - **Started**: 2026-05-15
`
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := SetBlockPR(doc, "auth-1", "")
	if err != nil {
		t.Fatalf("SetBlockPR: %v", err)
	}
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "  - **PR**: \n") {
		t.Errorf("expected `  - **PR**: ` (trailing space) line preserved:\n%q", joined)
	}
}

func TestSetBlockPRInsertsWhenMissing(t *testing.T) {
	src := `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **Repo**: api
  - **Tags**: refactor
  - **Started**: 2026-05-15
`
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := SetBlockPR(doc, "auth-1", "https://example.com/pull/9")
	if err != nil {
		t.Fatalf("SetBlockPR: %v", err)
	}
	joined := strings.Join(out, "\n")
	iTags := strings.Index(joined, "**Tags**")
	iPR := strings.Index(joined, "**PR**")
	iStarted := strings.Index(joined, "**Started**")
	if !(iTags < iPR && iPR < iStarted) {
		t.Errorf("expected PR between Tags and Started:\n%s", joined)
	}
}

func TestFormatTicketBlockNoNotesWhenEmpty(t *testing.T) {
	got := FormatTicketBlock(BlockOptions{
		Title: "Refactor auth",
		ID:    "auth-1",
		Repo:  "api",
	})
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "**Notes**") {
		t.Errorf("expected no **Notes** line when NotesRef is empty:\n%s", joined)
	}
}

func TestFormatTicketBlockNotesWhenSet(t *testing.T) {
	got := FormatTicketBlock(BlockOptions{
		Title:    "Refactor auth",
		ID:       "auth-1",
		PR:       "https://example.com/pr/1",
		NotesRef: "notes/auth-1.md",
		Started:  "2026-05-19",
	})
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "  - **Notes**: notes/auth-1.md") {
		t.Errorf("expected **Notes**: line:\n%s", joined)
	}
	iPR := strings.Index(joined, "**PR**")
	iNotes := strings.Index(joined, "**Notes**")
	iStarted := strings.Index(joined, "**Started**")
	if !(iPR < iNotes && iNotes < iStarted) {
		t.Errorf("expected PR < Notes < Started:\n%s", joined)
	}
}

func TestFormatTicketBlockWaitingSince(t *testing.T) {
	lines := FormatTicketBlock(BlockOptions{
		Title:        "Refactor auth",
		ID:           "auth-1",
		Started:      "2026-05-10",
		WaitingSince: "2026-05-18",
		State:        model.StateActive,
	})
	joined := strings.Join(lines, "\n")
	iStarted := strings.Index(joined, "**Started**")
	iWaiting := strings.Index(joined, "**Waiting since**")
	if iWaiting < 0 {
		t.Fatalf("**Waiting since** not emitted:\n%s", joined)
	}
	if iStarted < 0 || iWaiting < iStarted {
		t.Errorf("expected **Waiting since** after **Started**:\n%s", joined)
	}
	if !strings.Contains(joined, "**Waiting since**: 2026-05-18") {
		t.Errorf("wrong value in output:\n%s", joined)
	}
}

func TestFormatTicketBlockNoWaitingSinceWhenEmpty(t *testing.T) {
	lines := FormatTicketBlock(BlockOptions{
		Title:   "Refactor auth",
		ID:      "auth-1",
		Started: "2026-05-10",
		State:   model.StateActive,
	})
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "Waiting since") {
		t.Errorf("**Waiting since** emitted for empty value:\n%s", joined)
	}
}

func TestSetBlockWaitingSinceSetsValue(t *testing.T) {
	src := `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **PR**:
  - **Started**: 2026-05-10

## Next

## Someday
`
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := SetBlockWaitingSince(doc, "auth-1", "2026-05-20")
	if err != nil {
		t.Fatalf("SetBlockWaitingSince: %v", err)
	}
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "**Waiting since**: 2026-05-20") {
		t.Errorf("value not set:\n%s", joined)
	}
	iStarted := strings.Index(joined, "**Started**")
	iWaiting := strings.Index(joined, "**Waiting since**")
	if iWaiting < iStarted {
		t.Errorf("**Waiting since** should appear after **Started**:\n%s", joined)
	}
}

func TestSetBlockWaitingSinceClearsValue(t *testing.T) {
	src := `## Now
- [~] **AUTH-1** — Refactor auth
  - **ID**: auth-1
  - **PR**:
  - **Started**: 2026-05-10
  - **Waiting since**: 2026-05-18

## Next

## Someday
`
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := SetBlockWaitingSince(doc, "auth-1", "")
	if err != nil {
		t.Fatalf("SetBlockWaitingSince: %v", err)
	}
	joined := strings.Join(out, "\n")
	if strings.Contains(joined, "Waiting since") {
		t.Errorf("**Waiting since** still present after clear:\n%s", joined)
	}
}

func TestInsertSectionBefore(t *testing.T) {
	src := "## Now\n\n## Next\n\n## Someday\n"
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := InsertSectionBefore(doc, model.SectionWaiting, model.SectionNext)
	if err != nil {
		t.Fatalf("InsertSectionBefore: %v", err)
	}
	joined := strings.Join(out, "\n")
	waitIdx := strings.Index(joined, "## Waiting")
	nextIdx := strings.Index(joined, "## Next")
	somedayIdx := strings.Index(joined, "## Someday")
	if waitIdx < 0 {
		t.Fatal("## Waiting not inserted")
	}
	if waitIdx > nextIdx {
		t.Errorf("## Waiting (%d) after ## Next (%d)", waitIdx, nextIdx)
	}
	if nextIdx > somedayIdx {
		t.Errorf("## Next (%d) after ## Someday (%d)", nextIdx, somedayIdx)
	}
	// Now section must still be present and before Waiting.
	nowIdx := strings.Index(joined, "## Now")
	if nowIdx < 0 || nowIdx > waitIdx {
		t.Errorf("## Now not before ## Waiting (nowIdx=%d, waitIdx=%d)", nowIdx, waitIdx)
	}
}

func TestInsertSectionBeforeNotFound(t *testing.T) {
	src := "## Now\n\n## Someday\n"
	doc, err := parse.Bytes("WORK.md", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = InsertSectionBefore(doc, model.SectionWaiting, model.SectionNext)
	if err == nil {
		t.Error("expected error when beforeSection not found")
	}
}

func TestWriteAtomic(t *testing.T) {
	tmpdir := t.TempDir()
	dst := filepath.Join(tmpdir, "x.md")
	if err := WriteAtomic(dst, []string{"hello", "world"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\nworld\n" {
		t.Errorf("got %q", string(got))
	}
}

func TestFormatTicketBlockSource(t *testing.T) {
	got := FormatTicketBlock(BlockOptions{
		Title:  "Import from Jira",
		ID:     "jira-1",
		Source: "https://company.atlassian.net/browse/JIRA-1",
	})
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "**Source**: https://company.atlassian.net/browse/JIRA-1") {
		t.Errorf("missing Source line in:\n%s", joined)
	}
	// Source appears between PR and Notes.
	prIdx := strings.Index(joined, "**PR**:")
	srcIdx := strings.Index(joined, "**Source**:")
	if prIdx < 0 || srcIdx < 0 || srcIdx <= prIdx {
		t.Errorf("Source should appear after PR; PR at %d, Source at %d\n%s", prIdx, srcIdx, joined)
	}
}

func TestFormatTicketBlockSourceEmpty(t *testing.T) {
	got := FormatTicketBlock(BlockOptions{
		Title: "No source",
		ID:    "no-src",
	})
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "**Source**") {
		t.Errorf("unexpected Source line when Source is empty:\n%s", joined)
	}
}
