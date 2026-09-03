package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
)

func mustWorkdirForTest(t *testing.T, root string) model.Workdir {
	t.Helper()
	wd, err := model.NewWorkdir(root)
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

// TestStoreStartPromotesToNow covers start's store-backed twin (M3d): a
// standalone Next ticket moves to Now, stamped Started, with the write
// visible in WORK.md before the process exits (write-through, not an
// async gap).
func TestStoreStartPromotesToNow(t *testing.T) {
	live, _, _ := storeWriteFixture(t)

	_, stderr := runCLI(t, "start", "solo", "--dir", live)
	if strings.Contains(stderr, "refus") || strings.Contains(stderr, "error") {
		t.Fatalf("start: %s", stderr)
	}

	data, err := os.ReadFile(live + "/WORK.md")
	if err != nil {
		t.Fatal(err)
	}
	workMD := string(data)
	if !strings.Contains(workMD, "## Now") {
		t.Fatal("WORK.md missing ## Now section")
	}
	nowSection := workMD[strings.Index(workMD, "## Now"):]
	if idx := strings.Index(nowSection[1:], "## "); idx >= 0 {
		nowSection = nowSection[:idx+1]
	}
	if !strings.Contains(nowSection, "**SOLO**") {
		t.Errorf("solo not found under ## Now:\n%s", nowSection)
	}
}

// TestStoreStartAlreadyActiveRefuses proves the store-backed twin still
// enforces start's own domain error (not silently succeeding).
func TestStoreStartAlreadyActiveRefuses(t *testing.T) {
	live, _, _ := storeWriteFixture(t)

	err := runCLIExpectingFailure(t, "start", "kid-live", "--dir", live)
	if err == nil {
		t.Fatal("expected a refusal starting an already-active ticket")
	}
	if !strings.Contains(err.Error(), "already") {
		t.Errorf("error = %v, want it to mention 'already'", err)
	}
}

// TestStoreDoneArchivesChild covers done's store-backed twin: archiving
// kid-live (a child of an-epic) sets Archived/Completed/Summary and
// reports EpicCompletable once it's the epic's only child.
func TestStoreDoneArchivesChild(t *testing.T) {
	live, _, _ := storeWriteFixture(t)

	out, stderr := runCLI(t, "done", "kid-live", "--summary", "wrapped up", "--dir", live)
	if strings.Contains(stderr, "refus") || strings.Contains(stderr, "error") {
		t.Fatalf("done: %s", stderr)
	}
	if !strings.Contains(out, "archived") {
		t.Errorf("expected archived confirmation, got: %s", out)
	}

	data, err := os.ReadFile(live + "/archive/2026-09.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "kid-live") {
		t.Errorf("archive file missing kid-live entry:\n%s", data)
	}

	workMD, err := os.ReadFile(live + "/WORK.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(workMD), "KID-LIVE") {
		t.Error("kid-live should have left WORK.md after being archived")
	}
}

// TestStoreDoneEpicRefusesWithOpenChildren proves the store-backed twin
// still enforces the open-children guard.
func TestStoreDoneEpicRefusesWithOpenChildren(t *testing.T) {
	live, _, _ := storeWriteFixture(t)

	err := runCLIExpectingFailure(t, "done", "an-epic", "--summary", "wrapped up", "--dir", live)
	if err == nil {
		t.Fatal("expected a refusal archiving an epic with an open child")
	}
	if !strings.Contains(err.Error(), "kid-live") {
		t.Errorf("error = %v, want it to name the open child", err)
	}
}

// TestStoreWaitParksAndBlocksResume covers wait's store-backed twin, plus
// (via runStoreStart's shared resume fast path) that a waiting ticket
// resumes back into ## Now.
func TestStoreWaitParksAndBlocksResume(t *testing.T) {
	live, _, _ := storeWriteFixture(t)

	_, stderr := runCLI(t, "wait", "kid-live", "--dir", live)
	if strings.Contains(stderr, "refus") || strings.Contains(stderr, "error") {
		t.Fatalf("wait: %s", stderr)
	}
	workMD, err := os.ReadFile(live + "/WORK.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workMD), "## Waiting") {
		t.Fatalf("WORK.md missing ## Waiting section:\n%s", workMD)
	}

	// A second wait on the same ticket refuses (already waiting).
	if err := runCLIExpectingFailure(t, "wait", "kid-live", "--dir", live); err == nil {
		t.Error("expected a refusal waiting an already-waiting ticket")
	}

	// Resume via start's fast path.
	_, stderr = runCLI(t, "start", "kid-live", "--dir", live)
	if strings.Contains(stderr, "refus") || strings.Contains(stderr, "error") {
		t.Fatalf("resume via start: %s", stderr)
	}
	workMD, err = os.ReadFile(live + "/WORK.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(workMD), "## Waiting") {
		t.Errorf("kid-live should have left ## Waiting after resume:\n%s", workMD)
	}
}

// TestStoreEditSetsFields covers edit's store-backed twin, including the
// Notes field being refused (it's derived under the store model, not a
// settable field).
func TestStoreEditSetsFields(t *testing.T) {
	live, _, _ := storeWriteFixture(t)

	_, stderr := runCLI(t, "edit", "solo", "--status", "in review", "--tags", "a,b", "--dir", live)
	if strings.Contains(stderr, "refus") || strings.Contains(stderr, "error") {
		t.Fatalf("edit: %s", stderr)
	}
	workMD, err := os.ReadFile(live + "/WORK.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workMD), "in review") {
		t.Errorf("WORK.md missing updated status:\n%s", workMD)
	}
	if !strings.Contains(string(workMD), "a, b") {
		t.Errorf("WORK.md missing updated tags:\n%s", workMD)
	}

	if err := runCLIExpectingFailure(t, "edit", "solo", "--notes", "custom.md", "--dir", live); err == nil {
		t.Error("expected --notes to be refused under the store model")
	}
}

// TestStorePRSetAndClear covers pr's store-backed twin.
func TestStorePRSetAndClear(t *testing.T) {
	live, _, _ := storeWriteFixture(t)

	_, stderr := runCLI(t, "pr", "solo", "https://example.com/pr/1", "--dir", live)
	if strings.Contains(stderr, "refus") || strings.Contains(stderr, "error") {
		t.Fatalf("pr set: %s", stderr)
	}
	workMD, err := os.ReadFile(live + "/WORK.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workMD), "https://example.com/pr/1") {
		t.Errorf("WORK.md missing PR url:\n%s", workMD)
	}

	_, stderr = runCLI(t, "pr", "solo", "--clear", "--dir", live)
	if strings.Contains(stderr, "refus") || strings.Contains(stderr, "error") {
		t.Fatalf("pr clear: %s", stderr)
	}
	workMD, err = os.ReadFile(live + "/WORK.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(workMD), "https://example.com/pr/1") {
		t.Errorf("WORK.md still has the cleared PR url:\n%s", workMD)
	}
}

// TestStoreLinkSetAndClear covers link's store-backed twin.
func TestStoreLinkSetAndClear(t *testing.T) {
	live, _, _ := storeWriteFixture(t)

	_, stderr := runCLI(t, "link", "solo", "jira", "https://jira.example.com/T-1", "--dir", live)
	if strings.Contains(stderr, "refus") || strings.Contains(stderr, "error") {
		t.Fatalf("link set: %s", stderr)
	}
	workMD, err := os.ReadFile(live + "/WORK.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workMD), "jira") || !strings.Contains(string(workMD), "https://jira.example.com/T-1") {
		t.Errorf("WORK.md missing link:\n%s", workMD)
	}

	_, stderr = runCLI(t, "link", "solo", "jira", "--clear", "--dir", live)
	if strings.Contains(stderr, "refus") || strings.Contains(stderr, "error") {
		t.Fatalf("link clear: %s", stderr)
	}
	workMD, err = os.ReadFile(live + "/WORK.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(workMD), "https://jira.example.com/T-1") {
		t.Errorf("WORK.md still has the cleared link:\n%s", workMD)
	}
}

// TestStoreNoteAppend covers note's store-backed twin: appending creates
// notes/<id>.md (via write-through) and links it in WORK.md
// automatically.
func TestStoreNoteAppend(t *testing.T) {
	live, _, _ := storeWriteFixture(t)

	_, stderr := runCLI(t, "note", "solo", "a store-backed note", "--dir", live)
	if strings.Contains(stderr, "refus") || strings.Contains(stderr, "error") {
		t.Fatalf("note: %s", stderr)
	}

	data, err := os.ReadFile(live + "/notes/solo.md")
	if err != nil {
		t.Fatalf("notes/solo.md not rendered: %v", err)
	}
	if !strings.Contains(string(data), "a store-backed note") {
		t.Errorf("notes/solo.md missing the appended body:\n%s", data)
	}

	workMD, err := os.ReadFile(live + "/WORK.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workMD), "notes/solo.md") {
		t.Errorf("WORK.md missing the auto Notes: line for solo:\n%s", workMD)
	}
}

// TestStoreNoteEditorIngestsEditedContent covers note --editor's
// store-backed twin: the seed file is opened, the fake "editor" appends
// a second entry, and the save is parsed back into the store — a second
// append call then proves it round-tripped instead of being lost.
func TestStoreNoteEditorIngestsEditedContent(t *testing.T) {
	live, _, _ := storeWriteFixture(t)

	ss, err := openStoreForWrite(mustWorkdirForTest(t, live))
	if err != nil {
		t.Fatal(err)
	}
	fakeEditor := func(path string) error {
		existing, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(path, append(existing, []byte("\n## 2026-09-03 12:00\nedited in by hand\n")...), 0o644)
	}
	path, created, err := runStoreNoteEditor(ss, "solo", fakeEditor)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Error("expected CreatedFile=true for solo's first notes content")
	}
	ss.close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "edited in by hand") {
		t.Errorf("notes file missing the editor's content:\n%s", data)
	}

	// Re-open and confirm the store itself has the entry (not just the
	// rendered file, which a broken ingest could still coincidentally show
	// since it's the same bytes the editor wrote).
	ss2, err := openStoreForWrite(mustWorkdirForTest(t, live))
	if err != nil {
		t.Fatal(err)
	}
	defer ss2.close()
	tk, err := ss2.s.TicketBySlug("solo")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range tk.NoteEntries {
		if strings.Contains(e.Body, "edited in by hand") {
			found = true
		}
	}
	if !found {
		t.Errorf("store's NoteEntries missing the editor's entry: %+v", tk.NoteEntries)
	}
}

// TestStoreAddEpicAndChild covers add's store-backed twin across the
// epic and child branches: an epic add creates a ticket row of type
// epic, and a child add under it creates a row with ParentID set and no
// WORK.md section yet (matching legacy: a child occupies no section
// until started).
func TestStoreAddEpicAndChild(t *testing.T) {
	live, _, _ := storeWriteFixture(t)

	_, stderr := runCLI(t, "add", "--type", "epic", "--title", "New Epic", "--id", "new-epic", "--dir", live)
	if strings.Contains(stderr, "refus") || strings.Contains(stderr, "error") {
		t.Fatalf("add epic: %s", stderr)
	}
	workMD, err := os.ReadFile(live + "/WORK.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workMD), "NEW-EPIC") {
		t.Errorf("WORK.md missing the new epic:\n%s", workMD)
	}
	if _, err := os.Stat(live + "/notes/new-epic.md"); err != nil {
		t.Errorf("notes/new-epic.md not rendered: %v", err)
	}

	_, stderr = runCLI(t, "add", "--parent", "new-epic", "--title", "New Child", "--id", "new-child", "--dir", live)
	if strings.Contains(stderr, "refus") || strings.Contains(stderr, "error") {
		t.Fatalf("add child: %s", stderr)
	}
	workMD, err = os.ReadFile(live + "/WORK.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(workMD), "NEW-CHILD") {
		t.Errorf("a not-yet-started child should not appear in WORK.md yet:\n%s", workMD)
	}

	// Confirm the child is a real store row with ParentID set, by starting
	// it and checking it lands under the right parent.
	_, stderr = runCLI(t, "start", "new-child", "--dir", live)
	if strings.Contains(stderr, "refus") || strings.Contains(stderr, "error") {
		t.Fatalf("start new-child: %s", stderr)
	}
	workMD, err = os.ReadFile(live + "/WORK.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workMD), "NEW-CHILD") {
		t.Errorf("started child should now appear in WORK.md:\n%s", workMD)
	}
}

// TestStoreImportCreatesTicketsAndResolvesIntraBatchParent covers
// import's store-backed twin, including a second ticket in the same
// batch naming the first (freshly-created) one as --parent.
func TestStoreImportCreatesTicketsAndResolvesIntraBatchParent(t *testing.T) {
	live, _, _ := storeWriteFixture(t)

	payload := `[
		{"id": "batch-epic", "title": "Batch Epic", "type": "epic", "section": "next"},
		{"id": "batch-child", "title": "Batch Child", "parent": "batch-epic", "section": "next"}
	]`
	jsonFile := t.TempDir() + "/import.json"
	if err := os.WriteFile(jsonFile, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr := runCLI(t, "import", "--file", jsonFile, "--dir", live)
	if strings.Contains(stderr, "error") {
		t.Fatalf("import: %s", stderr)
	}

	workMD, err := os.ReadFile(live + "/WORK.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workMD), "BATCH-EPIC") || !strings.Contains(string(workMD), "BATCH-CHILD") {
		t.Errorf("WORK.md missing imported tickets:\n%s", workMD)
	}
}

// TestStoreFeedbackAppendAndResolve covers feedback's store-backed twin:
// append renders a new entry into FEEDBACK.md, and resolve stamps it
// **Resolved**: without disturbing anything else.
func TestStoreFeedbackAppendAndResolve(t *testing.T) {
	live, _, _ := storeWriteFixture(t)

	out, stderr := runCLI(t, "feedback", "append",
		"--signal", "missing-feature", "--trigger", "no undo button", "--dir", live)
	if strings.Contains(stderr, "refus") || strings.Contains(stderr, "error") {
		t.Fatalf("feedback append: %s", stderr)
	}
	fields := strings.Fields(out)
	if len(fields) < 2 {
		t.Fatalf("unexpected append output: %q", out)
	}
	ts := fields[1]

	data, err := os.ReadFile(live + "/FEEDBACK.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "no undo button") {
		t.Errorf("FEEDBACK.md missing the appended trigger:\n%s", data)
	}

	_, stderr = runCLI(t, "feedback", "resolve", ts, "--dir", live)
	if strings.Contains(stderr, "refus") || strings.Contains(stderr, "error") {
		t.Fatalf("feedback resolve: %s", stderr)
	}
	data, err = os.ReadFile(live + "/FEEDBACK.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "**Resolved**:") {
		t.Errorf("FEEDBACK.md missing the Resolved line:\n%s", data)
	}
}

// TestStoreStartCapExceededRefuses fills ## Now to cap (5) then confirms
// a sixth promotion is refused, matching the legacy cap-enforcement
// behavior faithfully.
func TestStoreStartCapExceededRefuses(t *testing.T) {
	live, dataDir, _ := storeWriteFixture(t)

	// The fixture's ## Now already holds an-epic + kid-live. Add three
	// more standalone Next tickets via the store-backed add path so
	// ## Now can be filled to cap without touching legacy machinery.
	for _, id := range []string{"filler-1", "filler-2", "filler-3"} {
		_, stderr := runCLI(t, "add", "--title", "Filler", "--id", id, "--dir", live)
		if strings.Contains(stderr, "error") {
			t.Fatalf("add %s: %s", id, stderr)
		}
	}
	_ = dataDir

	for _, id := range []string{"filler-1", "filler-2", "filler-3"} {
		_, stderr := runCLI(t, "start", id, "--dir", live)
		if strings.Contains(stderr, "refus") || strings.Contains(stderr, "error") {
			t.Fatalf("start %s: %s", id, stderr)
		}
	}

	// ## Now is now at 5 (an-epic, kid-live, filler-1..3). "solo" should
	// be refused.
	err := runCLIExpectingFailure(t, "start", "solo", "--dir", live)
	if err == nil {
		t.Fatal("expected a cap-exceeded refusal")
	}
	if !strings.Contains(err.Error(), "cap") {
		t.Errorf("error = %v, want it to mention the cap", err)
	}
}
