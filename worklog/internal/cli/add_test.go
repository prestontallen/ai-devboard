package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/parse"
)

// loadFixture writes a minimal WORK.md at root and returns the parsed
// document plus a Workdir.
func loadFixture(t *testing.T, body string) (model.Workdir, *model.WorkDoc) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "WORK.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := model.NewWorkdir(root)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		t.Fatal(err)
	}
	return wd, doc
}

const baseFixture = `## Now
## Next
- [ ] **EXISTING** — first
  - **ID**: existing
## Someday
`

func TestValidateAddInputsRequiresTitle(t *testing.T) {
	_, doc := loadFixture(t, baseFixture)
	err := validateAddInputs(doc, addInputs{ID: "x", Section: "Next"})
	if err == nil || !strings.Contains(err.Error(), "title") {
		t.Errorf("expected title-required error, got %v", err)
	}
}

func TestValidateAddInputsRequiresID(t *testing.T) {
	_, doc := loadFixture(t, baseFixture)
	err := validateAddInputs(doc, addInputs{Title: "T", Section: "Next"})
	if err == nil || !strings.Contains(err.Error(), "ID") {
		t.Errorf("expected ID-required error, got %v", err)
	}
}

func TestValidateAddInputsRejectsDuplicateID(t *testing.T) {
	_, doc := loadFixture(t, baseFixture)
	err := validateAddInputs(doc, addInputs{Title: "T", ID: "existing", Section: "Next"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected duplicate-ID error, got %v", err)
	}
}

func TestValidateAddInputsRejectsBadSection(t *testing.T) {
	_, doc := loadFixture(t, baseFixture)
	err := validateAddInputs(doc, addInputs{Title: "T", ID: "new", Section: "Now"})
	if err == nil || !strings.Contains(err.Error(), "section") {
		t.Errorf("expected section error, got %v", err)
	}
}

func TestApplyAddInsertsIntoNext(t *testing.T) {
	wd, doc := loadFixture(t, baseFixture)
	out, err := applyAdd(wd, doc, addInputs{
		Title:   "Refactor auth",
		ID:      "auth-1",
		Repo:    "api",
		Tags:    []string{"refactor", "auth"},
		Section: "Next",
	})
	if err != nil {
		t.Fatalf("applyAdd: %v", err)
	}
	if out.Status != "added" || out.ID != "auth-1" || out.Section != "Next" {
		t.Errorf("output = %+v", out)
	}
	data, _ := os.ReadFile(wd.WorkMD())
	work := string(data)
	for _, want := range []string{
		"- [ ] **AUTH-1** — Refactor auth",
		"  - **ID**: auth-1",
		"  - **Repo**: api",
		"  - **Tags**: refactor, auth",
	} {
		if !strings.Contains(work, want) {
			t.Errorf("WORK.md missing %q\nfull:\n%s", want, work)
		}
	}
}

func TestApplyAddPersistsWarnings(t *testing.T) {
	wd, doc := loadFixture(t, baseFixture)
	out, err := applyAdd(wd, doc, addInputs{
		Title: "T", ID: "tt", Section: "Someday",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Warnings) == 0 {
		t.Error("expected at least one warning about INDEX.md")
	}
	if !strings.Contains(out.Warnings[0], "INDEX.md") {
		t.Errorf("warning text unexpected: %q", out.Warnings[0])
	}
}

func TestApplyStandaloneKindIsTicket(t *testing.T) {
	wd, doc := loadFixture(t, baseFixture)
	out, err := applyStandalone(wd, doc, addInputs{
		Title: "x", ID: "x-1", Section: "Next",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != "ticket" {
		t.Errorf("Kind = %q, want ticket", out.Kind)
	}
}

func TestEpicAddCreatesBlockAndNotes(t *testing.T) {
	root := t.TempDir()
	wdPath := filepath.Join(root, "WORK.md")
	if err := os.WriteFile(wdPath,
		[]byte("## Now\n## Next\n## Someday\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := model.NewWorkdir(root)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		t.Fatal(err)
	}

	cmd := newAddCmd()
	cmd.SetOut(new(bytes.Buffer))
	if err := runAddEpic(cmd, wd, doc,
		addInputs{
			Title:   "Big epic",
			ID:      "epic-a",
			Repo:    "api",
			Tags:    []string{"epic", "refactor"},
			Section: "Next",
			Type:    "epic",
		}, true); err != nil {
		// errWithExit with code 0 message would have err != nil; sanity-check
		t.Fatalf("runAddEpic: %v", err)
	}
	workBytes, _ := os.ReadFile(wd.WorkMD())
	w := string(workBytes)
	for _, want := range []string{
		"- [ ] **EPIC-A** — Big epic",
		"  - **Type**: epic",
		"  - **Notes**: notes/epic-a.md",
		"  - **Active children**: <none>",
	} {
		if !strings.Contains(w, want) {
			t.Errorf("WORK.md missing %q:\n%s", want, w)
		}
	}
	notesBytes, err := os.ReadFile(wd.NotesFile("epic-a"))
	if err != nil {
		t.Fatalf("notes file missing: %v", err)
	}
	n := string(notesBytes)
	if !strings.Contains(n, "# Big epic") {
		t.Errorf("notes scaffold missing title:\n%s", n)
	}
	if !strings.Contains(n, "## Children") {
		t.Errorf("notes scaffold missing Children section:\n%s", n)
	}
}

func TestEpicAddRejectsParentCombo(t *testing.T) {
	// Test via the runAdd dispatcher because that's where the invalid-combo
	// rule lives.
	wd, _ := loadFixture(t, baseFixture)
	cmd := newAddCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{
		"--dir", wd.Root,
		"--type", "epic",
		"--parent", "some-epic",
		"--title", "x",
		"--id", "y",
	})
	// We can't easily wire --dir through here because resolveWorkdir uses
	// flagDir at the global level. Sidestep by calling runAdd directly:
	err := runAdd(cmd, "Bad", "bad-1", "", "", "", "Next", "epic", "some-parent", true)
	if err == nil {
		t.Fatal("expected error from epic+parent combo")
	}
	if !strings.Contains(err.Error(), "") { // codedError carries msg=""
		// Look for the underlying err in cmd output (JSON path).
	}
}

func TestEpicAddRefusesExistingNotes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "WORK.md"),
		[]byte("## Now\n## Next\n## Someday\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	notesDir := filepath.Join(root, "notes")
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-existing notes file
	if err := os.WriteFile(filepath.Join(notesDir, "epic-a.md"),
		[]byte("# pre-existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, _ := model.NewWorkdir(root)
	doc, _ := parse.File(wd.WorkMD())

	cmd := newAddCmd()
	cmd.SetOut(new(bytes.Buffer))
	err := runAddEpic(cmd, wd, doc, addInputs{
		Title: "Big", ID: "epic-a", Section: "Next", Type: "epic",
	}, true)
	if err == nil {
		t.Fatal("expected error for pre-existing notes file")
	}
}

func TestChildAddAppendsToNotes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "WORK.md"),
		[]byte(`## Now
## Next
- [ ] **EPIC-A** — Big epic
  - **ID**: epic-a
  - **Type**: epic
  - **Notes**: notes/epic-a.md
  - **Active children**: <none>
## Someday
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes", "epic-a.md"),
		[]byte("# Big epic\n\n## Children\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, _ := model.NewWorkdir(root)
	doc, _ := parse.File(wd.WorkMD())

	cmd := newAddCmd()
	cmd.SetOut(new(bytes.Buffer))
	err := runAddChild(cmd, wd, doc, addInputs{
		Title:  "First child task",
		ID:     "child-1",
		Parent: "epic-a",
	}, true)
	if err != nil {
		t.Fatalf("runAddChild: %v", err)
	}
	notes, _ := os.ReadFile(filepath.Join(root, "notes", "epic-a.md"))
	if !strings.Contains(string(notes), "- [ ] child-1: First child task") {
		t.Errorf("notes missing child line:\n%s", string(notes))
	}
}

func TestChildAddRejectsMissingEpic(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "WORK.md"),
		[]byte("## Now\n## Next\n## Someday\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, _ := model.NewWorkdir(root)
	doc, _ := parse.File(wd.WorkMD())

	cmd := newAddCmd()
	cmd.SetOut(new(bytes.Buffer))
	err := runAddChild(cmd, wd, doc, addInputs{
		Title:  "x",
		ID:     "child-1",
		Parent: "nonexistent-epic",
	}, true)
	if err == nil {
		t.Fatal("expected error for missing parent epic")
	}
}

func TestSplitTags(t *testing.T) {
	cases := map[string][]string{
		"":               nil,
		"  ":             nil,
		"one":            {"one"},
		"a, b,c":         {"a", "b", "c"},
		"trim, ,empties": {"trim", "empties"},
	}
	for in, want := range cases {
		got := splitTags(in)
		if (got == nil) != (want == nil) || len(got) != len(want) {
			t.Errorf("splitTags(%q) = %v, want %v", in, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("splitTags(%q)[%d] = %q, want %q", in, i, got[i], want[i])
			}
		}
	}
}
