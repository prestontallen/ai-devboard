package cli

import (
	"bytes"
	"encoding/json"
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

// invokeAdd drives `add` through the real root command, so the test sees
// the same wiring the binary does.
func invokeAdd(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	prev := flagDir
	t.Cleanup(func() { flagDir = prev })

	root := newRoot()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"add", "--dir", dir}, args...))
	err := root.Execute()
	return stdout.String(), err
}

func TestValidateAddInputsRequiresTitle(t *testing.T) {
	wd, doc := loadFixture(t, baseFixture)
	err := validateStandaloneInputs(wd, doc, addInputs{ID: "x", Section: "Next"})
	if err == nil || !strings.Contains(err.Error(), "title") {
		t.Errorf("expected title-required error, got %v", err)
	}
}

func TestValidateAddInputsRequiresID(t *testing.T) {
	wd, doc := loadFixture(t, baseFixture)
	err := validateStandaloneInputs(wd, doc, addInputs{Title: "T", Section: "Next"})
	if err == nil || !strings.Contains(err.Error(), "ID") {
		t.Errorf("expected ID-required error, got %v", err)
	}
}

func TestValidateAddInputsRejectsDuplicateID(t *testing.T) {
	wd, doc := loadFixture(t, baseFixture)
	err := validateStandaloneInputs(wd, doc, addInputs{Title: "T", ID: "existing", Section: "Next"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected duplicate-ID error, got %v", err)
	}
}

func TestValidateAddInputsRejectsBadSection(t *testing.T) {
	wd, doc := loadFixture(t, baseFixture)
	err := validateStandaloneInputs(wd, doc, addInputs{Title: "T", ID: "new", Section: "Now"})
	if err == nil || !strings.Contains(err.Error(), "section") {
		t.Errorf("expected section error, got %v", err)
	}
}

func TestApplyAddInsertsIntoNext(t *testing.T) {
	dir, _, _ := storeWriteFixture(t)
	out, err := invokeAdd(t, dir,
		"--title", "Refactor auth", "--id", "auth-1", "--repo", "api",
		"--tags", "refactor,auth", "--section", "Next", "--json")
	if err != nil {
		t.Fatalf("invokeAdd: %v\nout: %s", err, out)
	}
	var res addOutput
	if jerr := json.Unmarshal([]byte(out), &res); jerr != nil {
		t.Fatalf("json: %v\nout: %s", jerr, out)
	}
	if res.Status != "added" || res.ID != "auth-1" || res.Section != "Next" {
		t.Errorf("output = %+v", res)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "WORK.md"))
	work := string(data)
	for _, want := range []string{
		"— Refactor auth",
		"  - **ID**: auth-1",
		"  - **Repo**: api",
		"  - **Tags**: refactor, auth",
	} {
		if !strings.Contains(work, want) {
			t.Errorf("WORK.md missing %q\nfull:\n%s", want, work)
		}
	}
}

func TestApplyStandaloneKindIsTicket(t *testing.T) {
	dir, _, _ := storeWriteFixture(t)
	out, err := invokeAdd(t, dir, "--title", "x", "--id", "x-1", "--section", "Next", "--json")
	if err != nil {
		t.Fatalf("invokeAdd: %v\nout: %s", err, out)
	}
	var res addOutput
	if jerr := json.Unmarshal([]byte(out), &res); jerr != nil {
		t.Fatalf("json: %v\nout: %s", jerr, out)
	}
	if res.Kind != "ticket" {
		t.Errorf("Kind = %q, want ticket", res.Kind)
	}
}

func TestEpicAddCreatesBlockAndNotes(t *testing.T) {
	live, _, _ := storeWriteFixture(t)
	wd, err := model.NewWorkdir(live)
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
		t.Fatalf("runAddEpic: %v", err)
	}
	workBytes, _ := os.ReadFile(wd.WorkMD())
	w := string(workBytes)
	for _, want := range []string{
		"— Big epic",
		"  - **Type**: epic",
		"  - **Notes**: notes/epic-a.md",
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
	if !strings.Contains(n, "## Background") {
		t.Errorf("notes scaffold missing Background section:\n%s", n)
	}
}

func TestEpicAddRejectsParentCombo(t *testing.T) {
	// Test via the runAdd dispatcher because that's where the invalid-combo
	// rule lives. This refuses before the store is ever touched (the
	// epic+parent check runs ahead of runStoreAdd), so a bare WORK.md-only
	// fixture is enough.
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

// --- spike type -------------------------------------------------------------

func TestAddSpikeWritesTypeIntoBlock(t *testing.T) {
	dir, _, _ := storeWriteFixture(t)
	out, err := invokeAdd(t, dir,
		"--title", "Investigate the thing", "--id", "spike-1", "--section", "Next", "--type", "spike", "--json")
	if err != nil {
		t.Fatalf("invokeAdd: %v\nout: %s", err, out)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "WORK.md"))
	if !strings.Contains(string(body), "- **Type**: spike") {
		t.Errorf("block missing Type line:\n%s", body)
	}
}

// A spike must survive a parse round-trip, or start/done can't read it back.
func TestAddSpikeRoundTripsThroughParse(t *testing.T) {
	dir, _, _ := storeWriteFixture(t)
	out, err := invokeAdd(t, dir,
		"--title", "Investigate the thing", "--id", "spike-1", "--section", "Next", "--type", "spike", "--json")
	if err != nil {
		t.Fatalf("invokeAdd: %v\nout: %s", err, out)
	}
	reparsed, err := parse.File(filepath.Join(dir, "WORK.md"))
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	b := reparsed.BlockByID("spike-1")
	if b == nil {
		t.Fatal("spike-1 not found after re-parse")
	}
	if b.Type != model.TypeSpike {
		t.Errorf("Type = %q, want %q", b.Type, model.TypeSpike)
	}
}

// The default type stays off the block so ordinary tickets are unchanged.
func TestAddTicketOmitsTypeLine(t *testing.T) {
	dir, _, _ := storeWriteFixture(t)
	out, err := invokeAdd(t, dir,
		"--title", "Ordinary work", "--id", "tkt-1", "--section", "Next", "--type", "ticket", "--json")
	if err != nil {
		t.Fatalf("invokeAdd: %v\nout: %s", err, out)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "WORK.md"))
	if strings.Contains(string(body), "- **Type**: ticket") {
		t.Errorf("ordinary ticket should carry no Type line:\n%s", body)
	}
}

// Sad path: an unknown --type is refused, not silently dropped.
func TestAddRejectsUnknownType(t *testing.T) {
	wd, _ := loadFixture(t, baseFixture)
	cmd := newAddCmd()
	cmd.SetOut(new(bytes.Buffer))
	err := runAdd(cmd, "Typo", "typo-1", "", "", "", "Next", "spke", "", true)
	if err == nil {
		t.Fatal("expected refusal for unknown --type")
	}
	body, _ := os.ReadFile(wd.WorkMD())
	if strings.Contains(string(body), "typo-1") {
		t.Error("refused add must not write a block")
	}
}

func TestAddRejectsSpikeWithParent(t *testing.T) {
	_, _ = loadFixture(t, baseFixture)
	cmd := newAddCmd()
	cmd.SetOut(new(bytes.Buffer))
	err := runAdd(cmd, "Child spike", "spike-kid", "", "", "", "Next", "spike", "some-epic", true)
	if err == nil {
		t.Fatal("expected refusal for spike+parent combo")
	}
}
