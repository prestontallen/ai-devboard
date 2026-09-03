package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// ---- criterion 1 ----

func TestTaskScoutRecordsAttestation(t *testing.T) {
	_, path := amendFixture(t, store.Ticket{Complexity: "high"})

	if _, _, err := runTask(t, "scout", "inline",
		"--why", "subagents unavailable; walked the lenses single-pass", "--id", "tkt"); err != nil {
		t.Fatalf("scout: %v", err)
	}
	got := loadAmendTask(t, path)
	if got.Scout == nil {
		t.Fatal("no attestation recorded")
	}
	if got.Scout.Mode != "inline" || got.Scout.Why == "" {
		t.Errorf("attestation = %+v", got.Scout)
	}
	if len(got.Scout.When) != 10 || strings.Count(got.Scout.When, "-") != 2 {
		t.Errorf("when = %q, want yyyy-mm-dd", got.Scout.When)
	}

	before, _ := os.ReadFile(path)
	for _, args := range [][]string{
		{"scout", "bogus", "--why", "w", "--id", "tkt"},
		{"scout", "ran", "--id", "tkt"}, // missing --why
	} {
		_, _, err := runTask(t, args...)
		ec, ok := err.(exitCoder)
		if !ok || ec.ExitCode() != 64 {
			t.Errorf("%v exit = %v, want 64", args, ec)
		}
		after, _ := os.ReadFile(path)
		if string(after) != string(before) {
			t.Errorf("%v changed the file", args)
		}
	}
}

// ---- criterion 2 ----

func TestTaskScoutChildPathPersists(t *testing.T) {
	_, path := amendEpicChildFixture(t, "high")

	if _, _, err := runTask(t, "scout", "ran", "--why", "4 lenses",
		"--id", "epic", "--child", "kid"); err != nil {
		t.Fatalf("scout on child: %v", err)
	}
	kid := func() devboard.ChildEntry {
		t.Helper()
		for _, c := range loadAmendTask(t, path).Children {
			if c.ID == "kid" {
				return c
			}
		}
		t.Fatal("child kid missing")
		return devboard.ChildEntry{}
	}
	if s := kid().Scout; s == nil || s.Mode != "ran" {
		t.Fatalf("child attestation = %+v", s)
	}
	// A write targeting a different child must not drop it: the view pair
	// enumerates fields by hand.
	if _, _, err := runTask(t, "phase", "verify", "--id", "epic", "--child", "sib"); err != nil {
		t.Fatal(err)
	}
	if s := kid().Scout; s == nil || s.Mode != "ran" {
		t.Errorf("sibling write dropped the child's attestation: %+v", s)
	}
}

// ---- criterion 3 ----
//
// TestChildWorkViewCoversRoundTripFields is deliberately gone along with
// childWorkView/applyChildWorkView (adb-cutover M4 legacy retirement):
// those hand-enumerated ChildEntry's in-flight fields for the legacy
// YAML-splice epic-child mutation path, which no task<sub> command uses
// any more (storeMutateTaskOrChild replaced it). The store-model
// equivalent, projection.BoardTask/ApplyBoardTask, has its own round-trip
// guard — internal/projection/board_test.go's TestBoardTaskRoundTrip.

// ---- criteria 4 and 5 ----

func TestScoutGateWarnsOnPhase(t *testing.T) {
	for _, level := range []string{"medium", "high"} {
		for _, phase := range []string{"plan", "implementing"} {
			amendFixture(t, store.Ticket{Complexity: level})
			out, _, err := runTask(t, "phase", phase, "--id", "tkt")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, "worklog task scout") {
				t.Errorf("%s/%s: warning should lead with the command:\n%s", level, phase, out)
			}
			if !strings.Contains(out, level) {
				t.Errorf("%s/%s: warning should name the rating:\n%s", level, phase, out)
			}
		}
	}
}

func TestScoutGateSilentWhenNotOwed(t *testing.T) {
	cases := []struct {
		name string
		task store.Ticket
	}{
		{"low complexity", store.Ticket{Complexity: "low"}},
		{"no rating at all", store.Ticket{}},
		{"already attested", store.Ticket{Complexity: "high",
			Scout: &store.Scout{Mode: "ran", Why: "did it"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			amendFixture(t, tc.task)
			out, _, err := runTask(t, "phase", "plan", "--id", "tkt")
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out, "NOTE:") {
				t.Errorf("expected silence, got:\n%s", out)
			}
		})
	}
}

// An early phase must not warn even at high complexity — the scout has not
// been owed yet.
func TestScoutGateSilentBeforeContract(t *testing.T) {
	amendFixture(t, store.Ticket{Complexity: "high"})
	out, _, err := runTask(t, "phase", "clarify", "--id", "tkt")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "NOTE:") {
		t.Errorf("clarify should not warn:\n%s", out)
	}
}

// ---- criterion 6 ----

func TestScoutGateWarnsOnLateRating(t *testing.T) {
	t.Run("complexity raised after the work started", func(t *testing.T) {
		amendFixture(t, store.Ticket{Phase: "implementing"})
		out, _, err := runTask(t, "complexity", "high", "--id", "tkt")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "worklog task scout") {
			t.Errorf("a late medium/high rating should warn:\n%s", out)
		}
	})

	t.Run("complexity rated at intake stays silent", func(t *testing.T) {
		amendFixture(t, store.Ticket{Phase: "intake"})
		out, _, err := runTask(t, "complexity", "high", "--id", "tkt")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "NOTE:") {
			t.Errorf("rating at intake precedes the scout; must not warn:\n%s", out)
		}
	})
}

// ---- criterion 7 ----

func TestTaskAmendClearsAttestation(t *testing.T) {
	_, path := amendFixture(t, store.Ticket{
		Complexity: "medium", Phase: "implementing",
		Scout: &store.Scout{Mode: "ran", Why: "against the old scope"},
	})

	out, _, err := runTask(t, "amend", "scope doubled", "--why", "w",
		"--complexity", "high", "--id", "tkt")
	if err != nil {
		t.Fatal(err)
	}
	if got := loadAmendTask(t, path); got.Scout != nil {
		t.Errorf("amend to high left the stale attestation: %+v", got.Scout)
	}
	if !strings.Contains(out, "worklog task scout") {
		t.Errorf("the re-arm line should be printed:\n%s", out)
	}
	// Re-arm before the checklist: it is the action, the checklist is context.
	if i, j := strings.Index(out, "worklog task scout"), strings.Index(out, "re-sync checklist"); i > j {
		t.Errorf("re-arm line should precede the checklist:\n%s", out)
	}
}

func TestTaskAmendKeepsAttestationWhenResultIsLow(t *testing.T) {
	_, path := amendFixture(t, store.Ticket{
		Complexity: "medium",
		Scout:      &store.Scout{Mode: "ran", Why: "did it"},
	})

	if _, _, err := runTask(t, "amend", "scope shrank", "--why", "w",
		"--complexity", "low", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	if got := loadAmendTask(t, path); got.Scout == nil {
		t.Error("a low outcome needs no scout, so the record should stand")
	}
}

// ---- criterion 8: replaces TestTaskAmendPreservesUnknownKeys ----

// That test asserted `scout:` SURVIVES an amend, which is now the opposite of
// the requirement. It is replaced rather than deleted so the unknown-key
// invariant it guarded is not lost with it.
func TestTaskAmendClearsScoutAndKeepsOtherUnknownKeys(t *testing.T) {
	_, path := amendFixture(t, store.Ticket{
		Complexity: "low",
		Scout:      &store.Scout{Mode: "ran", Why: "because"},
		Extra:      map[string]any{"custom_field": "keep me"},
	})

	if _, _, err := runTask(t, "amend", "x", "--why", "w",
		"--complexity", "medium", "--id", "tkt"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "scout:") {
		t.Errorf("amend to medium must clear the attestation:\n%s", raw)
	}
	if !strings.Contains(string(raw), "custom_field") {
		t.Errorf("amend dropped an unrelated unknown key:\n%s", raw)
	}
}

// ---- criterion 9 ----

// phase is the most frequently run subcommand; a hook that shelled out would
// be felt on every transition.
func TestScoutGateHookSpawnsNothing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir) // nothing executable is reachable
	if _, err := exec.LookPath("go"); err == nil {
		t.Fatal("PATH not actually stripped")
	}
	amendFixture(t, store.Ticket{Complexity: "high"})

	out, _, err := runTask(t, "phase", "plan", "--id", "tkt")
	if err != nil {
		t.Fatalf("the gate must not need a subprocess: %v", err)
	}
	if !strings.Contains(out, "worklog task scout") {
		t.Errorf("gate should still warn with no binaries on PATH:\n%s", out)
	}
}

// ---- criterion 10 ----

func TestScoutGateJSONSingleDocument(t *testing.T) {
	amendFixture(t, store.Ticket{Complexity: "high"})

	out, _, err := runTask(t, "phase", "plan", "--id", "tkt", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Action   string   `json:"action"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, out)
	}
	if res.Action == "" || len(res.Warnings) == 0 {
		t.Errorf("want action plus a non-empty warnings array, got %s", out)
	}
}

// A differently-cased --child must not make the gate vanish.
func TestScoutGateChildLookupIsCaseInsensitive(t *testing.T) {
	amendEpicChildFixture(t, "high")

	out, _, err := runTask(t, "phase", "plan", "--id", "epic", "--child", "KID")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "worklog task scout") {
		t.Errorf("gate should fire for a differently-cased child:\n%s", out)
	}
}
