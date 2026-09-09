package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// amendFixture builds a real worklog ticket "tkt", the same
// resolveStoreTarget-backed shape task_test.go's taskStoreFixture uses.
// extra carries whatever devboard-visible fields the test cares about
// (Complexity, Phase, Scout, ...); identity fields (Slug/Title/Type/
// State/Section/Repo) are filled in here if extra leaves them zero, and
// BoardTracked is always forced true — Complexity/Phase/Scout are
// devboard-only content that only persists through the rendered YAML,
// which only renders when BoardTracked, and some callers need the file
// to already exist before their first mutation (before/after unchanged
// checks). Returns the worklog root and the ticket slug to read back.
func amendFixture(t *testing.T, extra store.Ticket) (dir, slug string) {
	t.Helper()
	tk := extra
	tk.Slug = "tkt"
	if tk.Title == "" {
		tk.Title = "T"
	}
	if tk.Type == "" {
		tk.Type = store.TypeTicket
	}
	if tk.State == "" {
		tk.State = store.StatePending
	}
	if tk.Section == "" && tk.Type != store.TypeEpic {
		tk.Section = store.SectionNext
	}
	if tk.Repo == "" {
		tk.Repo = devboard.RepoName()
	}
	tk.BoardTracked = true
	return seedStore(t, &tk), "tkt"
}

// amendEpicChildFixture builds a real epic "epic" (BoardTracked) with two
// children: "kid" (given complexity) and "sib" (no complexity) — for the
// --child amend/scout path and its sibling-isolation check.
func amendEpicChildFixture(t *testing.T, kidComplexity string) (dir, slug string) {
	t.Helper()
	epic := &store.Ticket{
		ID: store.NewID(), Slug: "epic", Title: "E", Type: store.TypeEpic,
		State: store.StatePending, Section: store.SectionNext,
		Repo: devboard.RepoName(), BoardTracked: true,
	}
	kid := &store.Ticket{
		Slug: "kid", Title: "K", Type: store.TypeTicket,
		State: store.StateActive, ParentID: epic.ID, Complexity: kidComplexity,
	}
	sib := &store.Ticket{
		Slug: "sib", Title: "S", Type: store.TypeTicket,
		State: store.StateActive, ParentID: epic.ID,
	}
	dir = seedStore(t, epic, kid, sib)
	return dir, "epic"
}

// loadAmendTask reads the ticket's board shape from the store. It read the
// rendered YAML at `path` until that projection was retired.
func loadAmendTask(t *testing.T, dir, slug string) devboard.Task {
	t.Helper()
	return boardOf(t, dir, slug)
}

// ---- criterion 1 ----

func TestTaskAmendRequiresComplexity(t *testing.T) {
	amendFixture(t, store.Ticket{Complexity: "low"})

	_, _, err := runTask(t, "amend", "scope grew", "--why", "w", "--id", "tkt")
	ec, ok := err.(exitCoder)
	if !ok || ec.ExitCode() != 64 {
		t.Fatalf("exit = %v, want 64", ec)
	}
	if !strings.Contains(err.Error(), "--complexity") {
		t.Errorf("error should name the flag: %v", err)
	}

	// Under --json the refusal must still be one parseable document, which
	// cobra's own required-flag machinery would not produce.
	out, _, _ := runTask(t, "amend", "scope grew", "--why", "w", "--id", "tkt", "--json")
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, out)
	}
	if _, has := doc["error"]; !has {
		t.Errorf("want an error key, got %s", out)
	}
}

// ---- criterion 2 ----

func TestTaskAmendRejectionLeavesFileUnchanged(t *testing.T) {
	dir, slug := amendFixture(t, store.Ticket{Complexity: "low"})
	before := loadAmendTask(t, dir, slug)
	for _, args := range [][]string{
		{"amend", "x", "--why", "w", "--id", "tkt"},                          // no --complexity
		{"amend", "x", "--why", "w", "--complexity", "bogus", "--id", "tkt"}, // bad value
	} {
		if _, _, err := runTask(t, args...); err == nil {
			t.Fatalf("%v: expected refusal", args)
		}
		if after := loadAmendTask(t, dir, slug); !reflect.DeepEqual(after, before) {
			t.Errorf("%v changed the ticket:\n%+v", args, after)
		}
	}
}

// ---- criterion 3 ----

func TestTaskAmendRecordsEntry(t *testing.T) {
	dir, slug := amendFixture(t, store.Ticket{Complexity: "low"})

	if _, _, err := runTask(t, "amend", "retargeted to a generic link",
		"--why", "the storage layer had already generalised",
		"--complexity", "high", "--id", "tkt"); err != nil {
		t.Fatalf("amend: %v", err)
	}
	got := loadAmendTask(t, dir, slug)
	if len(got.Decision) != 1 {
		t.Fatalf("decisions = %d, want 1", len(got.Decision))
	}
	d := got.Decision[0]
	if d.What != "retargeted to a generic link" {
		t.Errorf("what = %q", d.What)
	}
	if d.Why == "" {
		t.Errorf("why is empty")
	}
	// The cli package has no injectable clock, so assert the shape.
	if len(d.When) != 10 || strings.Count(d.When, "-") != 2 {
		t.Errorf("when = %q, want yyyy-mm-dd", d.When)
	}
	if d.Complexity != "low → high" {
		t.Errorf("complexity transition = %q, want %q", d.Complexity, "low → high")
	}
}

// ---- criterion 4 ----

func TestTaskAmendUpdatesRating(t *testing.T) {
	dir, slug := amendFixture(t, store.Ticket{Complexity: "low"})

	if _, _, err := runTask(t, "amend", "scope doubled", "--why", "w",
		"--complexity", "high", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	got := loadAmendTask(t, dir, slug)
	if got.Complexity != "high" {
		t.Errorf("complexity = %q, want high", got.Complexity)
	}
	if got.Decision[0].Complexity != "low → high" {
		t.Errorf("transition = %q", got.Decision[0].Complexity)
	}
}

// ---- criterion 5 ----

func TestTaskAmendUnchangedNeverPersisted(t *testing.T) {
	dir, slug := amendFixture(t, store.Ticket{Complexity: "medium"})

	if _, _, err := runTask(t, "amend", "wording only", "--why", "w",
		"--complexity", "unchanged", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	got := loadAmendTask(t, dir, slug)
	if got.Complexity != "medium" {
		t.Errorf("complexity = %q, want medium preserved", got.Complexity)
	}
	// The sentinel must never reach the field that gates the risk scout.
	if got.Complexity == "unchanged" {
		t.Error("the sentinel leaked into the complexity field")
	}
	if got.Decision[0].Complexity != "medium (unchanged)" {
		t.Errorf("transition = %q", got.Decision[0].Complexity)
	}
}

// ---- criterion 6 ----

// Only 5 of 23 contracts carried a rating at all, so "unchanged from nothing"
// is the common case and is exactly the skip this verb exists to prevent.
func TestTaskAmendUnchangedWithoutPriorRating(t *testing.T) {
	dir, slug := amendFixture(t, store.Ticket{})
	before := loadAmendTask(t, dir, slug)

	_, _, err := runTask(t, "amend", "x", "--why", "w", "--complexity", "unchanged", "--id", "tkt")
	ec, ok := err.(exitCoder)
	if !ok || ec.ExitCode() != 64 {
		t.Fatalf("exit = %v, want 64", ec)
	}
	if !strings.Contains(err.Error(), "low|medium|high") {
		t.Errorf("error should say what to state instead: %v", err)
	}
	if after := loadAmendTask(t, dir, slug); !reflect.DeepEqual(after, before) {
		t.Errorf("refusal changed the ticket:\n%+v", after)
	}
}

// ---- criterion 7 ----

func TestTaskAmendPrintsResyncChecklist(t *testing.T) {
	amendFixture(t, store.Ticket{Complexity: "low"})

	out, _, err := runTask(t, "amend", "x", "--why", "w", "--complexity", "medium", "--id", "tkt")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"contract file", "worklog edit title", "acceptance",
		"scorecard", "plan", "slug",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("checklist missing %q:\n%s", want, out)
		}
	}
}

// The child form names the notes checkbox line instead, because the roster
// sync rewrites a child's YAML title from it on every sibling start or done.
func TestTaskAmendChecklistNamesNotesLineForChild(t *testing.T) {
	if got := strings.Join(resyncChecklist("kid"), "\n"); !strings.Contains(got, "notes/<epic>.md") {
		t.Errorf("child checklist should name the notes line:\n%s", got)
	}
	if got := strings.Join(resyncChecklist(""), "\n"); strings.Contains(got, "notes/<epic>.md") {
		t.Errorf("plain-ticket checklist should not mention the epic notes line:\n%s", got)
	}
}

// ---- criterion 8 ----

func TestTaskAmendChildPathPersists(t *testing.T) {
	dir, slug := amendEpicChildFixture(t, "low")

	if _, _, err := runTask(t, "amend", "child scope grew", "--why", "w",
		"--complexity", "high", "--id", "epic", "--child", "kid"); err != nil {
		t.Fatalf("amend on child: %v", err)
	}

	child := func() devboard.ChildEntry {
		t.Helper()
		for _, c := range loadAmendTask(t, dir, slug).Children {
			if c.ID == "kid" {
				return c
			}
		}
		t.Fatal("child kid missing")
		return devboard.ChildEntry{}
	}

	got := child()
	if len(got.Decision) != 1 || got.Decision[0].Complexity != "low → high" {
		t.Fatalf("child decisions = %+v, want one entry with the transition", got.Decision)
	}
	if got.Complexity != "high" {
		t.Errorf("child complexity = %q, want high", got.Complexity)
	}

	// An unrelated write to a sibling must not clobber it: BoardTask and
	// ApplyBoardTask enumerate fields by hand, so a field they forget is
	// silently dropped on the next write-back.
	if _, _, err := runTask(t, "phase", "verify", "--id", "epic", "--child", "sib"); err != nil {
		t.Fatal(err)
	}
	if after := child(); len(after.Decision) != 1 {
		t.Errorf("sibling write dropped the child's amendment: %+v", after.Decision)
	} else if after.Decision[0].Complexity != "low → high" {
		t.Errorf("sibling write lost the transition: %q", after.Decision[0].Complexity)
	}
}

// ---- criterion 9 ----

// task complexity stays the initial-rating path and must not learn the
// amend-only sentinel: it has no old value to keep.
func TestTaskComplexityRejectsUnchanged(t *testing.T) {
	amendFixture(t, store.Ticket{})

	_, _, err := runTask(t, "complexity", "unchanged", "--id", "tkt")
	ec, ok := err.(exitCoder)
	if !ok || ec.ExitCode() != 64 {
		t.Fatalf("exit = %v, want 64", ec)
	}
	for _, level := range []string{"low", "medium", "high"} {
		if _, _, err := runTask(t, "complexity", level, "--id", "tkt"); err != nil {
			t.Errorf("complexity %s: %v", level, err)
		}
	}
}

// ---- criterion 10 ----
//
// Superseded by TestTaskAmendClearsScoutAndKeepsOtherUnknownKeys in
// task_scout_test.go: adb-scout-gate made `scout:` a typed field that amend
// must CLEAR on a medium/high outcome, which is the opposite of what this
// asserted. The unknown-key invariant it guarded lives on in the replacement.

// TestAmendSaysItClearedTheScout is the fix for a confusion that hit twice.
//
// An amendment deliberately discards the risk-scout attestation, because a
// scout run against the old scope does not attest a new one. That reasoning
// is right. But it used to happen silently, and the field is not journaled,
// so a cleared attestation looked exactly like a lost write — one session
// filed a data-loss bug over it and a later one nearly did.
func TestAmendSaysItClearedTheScout(t *testing.T) {
	dir, slug := amendFixture(t, store.Ticket{
		Complexity: "high",
		Scout:      &store.Scout{Mode: "ran", Why: "four lenses", When: "2026-09-08"},
	})

	stdout, stderr, err := runTask(t, "amend", "scope moved", "--why", "found more",
		"--complexity", "unchanged", "--id", "tkt")
	if err != nil {
		t.Fatalf("amend: %v", err)
	}
	if !strings.Contains(stdout+stderr, "discarded the risk-scout attestation") {
		t.Errorf("amend did not say it cleared the attestation; got %q / %q", stdout, stderr)
	}
	if got := loadAmendTask(t, dir, slug); got.Scout != nil {
		t.Errorf("attestation survived a high-complexity amendment: %+v", got.Scout)
	}
}

// TestAmendKeepsALowComplexityScoutSilently: the gate only fires at medium
// and high, so a low-complexity amendment has no attestation to invalidate
// and must not claim it cleared one.
func TestAmendKeepsALowComplexityScoutSilently(t *testing.T) {
	dir, slug := amendFixture(t, store.Ticket{
		Complexity: "low",
		Scout:      &store.Scout{Mode: "skipped", Why: "rated low", When: "2026-09-08"},
	})

	stdout, stderr, err := runTask(t, "amend", "small change", "--why", "typo",
		"--complexity", "unchanged", "--id", "tkt")
	if err != nil {
		t.Fatalf("amend: %v", err)
	}
	if strings.Contains(stdout+stderr, "discarded the risk-scout attestation") {
		t.Errorf("a low-complexity amendment claimed to clear an attestation: %q / %q", stdout, stderr)
	}
	got := loadAmendTask(t, dir, slug)
	if got.Scout == nil || got.Scout.Mode != "skipped" {
		t.Errorf("low-complexity amendment cleared the attestation: %+v", got.Scout)
	}
}
