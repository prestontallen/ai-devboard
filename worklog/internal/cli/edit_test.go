package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/edit"
	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/parse"
)

// invokeEdit drives `edit` through the real root command, so the test sees
// the same wiring the binary does. Notably SilenceUsage: without it cobra
// appends a usage banner to the --json error document.
func invokeEdit(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	prev := flagDir
	t.Cleanup(func() { flagDir = prev })

	root := newRoot()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"edit", "--dir", dir}, args...))
	err := root.Execute()
	return stdout.String(), err
}

func readWork(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "WORK.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// blockIn re-parses WORK.md and returns the named block, so assertions run
// against what the parser sees rather than against raw text.
func blockIn(t *testing.T, dir, id string) *model.Block {
	t.Helper()
	doc, err := parse.File(filepath.Join(dir, "WORK.md"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	b := doc.BlockByID(id)
	if b == nil {
		t.Fatalf("block %q not found", id)
	}
	return b
}

func exitCodeOf(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	ec, ok := err.(exitCoder)
	if !ok {
		t.Fatalf("error %v is not an exitCoder", err)
	}
	return ec.ExitCode()
}

func TestEditInsert(t *testing.T) {
	dir, _, _ := storeWriteFixture(t)
	before := blockIn(t, dir, "solo")
	if before.Status != "" {
		t.Fatalf("setup: solo already has a Status: %q", before.Status)
	}

	out, err := invokeEdit(t, dir, "solo", "--status", "in review", "--json")
	if err != nil {
		t.Fatalf("invokeEdit: %v\nout: %s", err, out)
	}

	after := blockIn(t, dir, "solo")
	if after.Status != "in review" {
		t.Errorf("status = %q, want %q", after.Status, "in review")
	}
	// Everything else on the block is untouched.
	if after.Repo != before.Repo || after.Acceptance != before.Acceptance || after.Title != before.Title {
		t.Errorf("unrelated fields changed: before=%+v after=%+v", before, after)
	}

	// Status ranks last, so it lands after Acceptance and inside the block.
	workmd := readWork(t, dir)
	iStatus := strings.Index(workmd, "**Status**: in review")
	iAcceptance := strings.Index(workmd, "**Acceptance**: "+before.Acceptance)
	if iStatus < 0 || iAcceptance < 0 || iStatus < iAcceptance {
		t.Errorf("Status did not land after Acceptance inside solo's block:\n%s", workmd)
	}
}

func TestEditRewritesInPlace(t *testing.T) {
	dir, _, _ := storeWriteFixture(t)
	if out, err := invokeEdit(t, dir, "solo", "--acceptance", "logout works", "--json"); err != nil {
		t.Fatalf("invokeEdit: %v\nout: %s", err, out)
	}
	after := readWork(t, dir)
	if n := strings.Count(after, "**Acceptance**"); n != 1 {
		t.Errorf("Acceptance appears %d times, want 1:\n%s", n, after)
	}
	if got := blockIn(t, dir, "solo").Acceptance; got != "logout works" {
		t.Errorf("acceptance = %q", got)
	}
}

func TestEditClearVsAbsent(t *testing.T) {
	dir, _, _ := storeWriteFixture(t)
	baseline := blockIn(t, dir, "solo")

	// Flag passed with an empty value removes the line.
	if out, err := invokeEdit(t, dir, "solo", "--acceptance", "", "--json"); err != nil {
		t.Fatalf("clear: %v\nout: %s", err, out)
	}
	after := readWork(t, dir)
	if strings.Contains(after, "**Acceptance**") {
		t.Errorf("Acceptance line survived the clear:\n%s", after)
	}

	// A flag not passed leaves its field alone: editing Status must not
	// disturb Repo or Started.
	if out, err := invokeEdit(t, dir, "solo", "--status", "blocked", "--json"); err != nil {
		t.Fatalf("set status: %v\nout: %s", err, out)
	}
	b := blockIn(t, dir, "solo")
	if b.Repo != baseline.Repo {
		t.Errorf("repo = %q, want it untouched (%q)", b.Repo, baseline.Repo)
	}
	if b.Started != baseline.Started {
		t.Errorf("started = %q, want it untouched (%q)", b.Started, baseline.Started)
	}
}

func TestEditCSVRoundTrip(t *testing.T) {
	dir, _, _ := storeWriteFixture(t)
	out, err := invokeEdit(t, dir, "solo",
		"--tags", "auth,  api ,cli", "--files", "a.go, b.go", "--json")
	if err != nil {
		t.Fatalf("invokeEdit: %v\nout: %s", err, out)
	}

	b := blockIn(t, dir, "solo")
	if got, want := strings.Join(b.Tags, "|"), "auth|api|cli"; got != want {
		t.Errorf("tags = %q, want %q", got, want)
	}
	if got, want := strings.Join(b.Files, "|"), "a.go|b.go"; got != want {
		t.Errorf("files = %q, want %q", got, want)
	}

	// The rendered line matches what the block formatter would emit.
	after := readWork(t, dir)
	if !strings.Contains(after, "  - **Tags**: auth, api, cli") {
		t.Errorf("tags line not normalized:\n%s", after)
	}
}

func TestEditTitle(t *testing.T) {
	dir, _, _ := storeWriteFixture(t)
	baseline := blockIn(t, dir, "solo")

	if out, err := invokeEdit(t, dir, "solo", "--title", "Rework auth", "--json"); err != nil {
		t.Fatalf("invokeEdit: %v\nout: %s", err, out)
	}
	after := readWork(t, dir)
	if !strings.Contains(after, "— Rework auth") {
		t.Errorf("bullet line not rewritten as expected:\n%s", after)
	}
	if b := blockIn(t, dir, "solo"); b.State != baseline.State {
		t.Errorf("state = %q, want it preserved (%q)", b.State, baseline.State)
	}
}

func TestEditAppliesMultipleFieldsInOneWrite(t *testing.T) {
	dir, _, _ := storeWriteFixture(t)
	baseline := blockIn(t, dir, "solo")

	out, err := invokeEdit(t, dir, "solo",
		"--repo", "acme/api", "--status", "in review", "--acceptance", "all green", "--json")
	if err != nil {
		t.Fatalf("invokeEdit: %v\nout: %s", err, out)
	}

	var res edit.Result
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	// Changes come back in canonical field order regardless of flag order.
	var fields []string
	for _, c := range res.Changes {
		fields = append(fields, c.Field)
	}
	if got, want := strings.Join(fields, ","), "Repo,Acceptance,Status"; got != want {
		t.Errorf("changed fields = %q, want %q", got, want)
	}
	if res.Changes[0].From != baseline.Repo {
		t.Errorf("repo from = %q, want %q", res.Changes[0].From, baseline.Repo)
	}

	b := blockIn(t, dir, "solo")
	if b.Repo != "acme/api" || b.Status != "in review" || b.Acceptance != "all green" {
		t.Errorf("block not fully updated: %+v", *b)
	}
}

func TestEditUnknownID(t *testing.T) {
	dir, _, _ := storeWriteFixture(t)
	before := readWork(t, dir)

	out, err := invokeEdit(t, dir, "nope", "--status", "x", "--json")
	if code := exitCodeOf(t, err); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}

	var je jsonError
	if uerr := json.Unmarshal([]byte(out), &je); uerr != nil {
		t.Fatalf("stdout is not a single JSON document: %v\nout: %s", uerr, out)
	}
	if !strings.Contains(je.Error, "not found") {
		t.Errorf("error = %q, want it to mention not found", je.Error)
	}
	if readWork(t, dir) != before {
		t.Error("WORK.md was modified on the failure path")
	}
}

func TestEditNoFlags(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "WORK.md"), []byte("## Now\n\n## Next\n\n## Someday\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := readWork(t, dir)

	out, err := invokeEdit(t, dir, "whatever", "--json")
	if code := exitCodeOf(t, err); code != 64 {
		t.Errorf("exit code = %d, want 64", code)
	}
	if !strings.Contains(out, "--acceptance") {
		t.Errorf("error should list the available flags:\n%s", out)
	}
	if readWork(t, dir) != before {
		t.Error("WORK.md was modified on the failure path")
	}
}

func TestEditEmptyTitleRejected(t *testing.T) {
	dir, _, _ := storeWriteFixture(t)
	before := readWork(t, dir)

	if _, err := invokeEdit(t, dir, "solo", "--title", "", "--json"); exitCodeOf(t, err) != 64 {
		t.Errorf("exit code = %d, want 64", exitCodeOf(t, err))
	}
	if readWork(t, dir) != before {
		t.Error("WORK.md was modified on the failure path")
	}
}

func TestEditRejectsFieldsOwnedElsewhere(t *testing.T) {
	// The lifecycle-owned fields have no flag at all, which is what keeps a
	// single writer per field. Guard the table so one can't be added by
	// accident.
	for _, f := range editFlags {
		switch f.field {
		case "ID", "Type", "Parent", "PR", "Started", "Waiting since", "Active children":
			t.Errorf("edit exposes %q, which another command owns", f.field)
		}
	}

	// And the operation layer refuses them even if a flag appeared.
	dir, _, _ := storeWriteFixture(t)
	prev := flagDir
	flagDir = dir
	t.Cleanup(func() { flagDir = prev })
	wd, err := resolveWorkdir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runStoreEdit(wd, "solo", []edit.Assignment{{Field: "Started", Value: "2026-01-01"}}); err == nil {
		t.Error("runStoreEdit accepted Started, want ErrNotEditable")
	}
}
