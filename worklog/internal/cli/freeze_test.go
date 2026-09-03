package cli

import (
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/freeze"
)

// TestFreezeRefusesEveryWriteVerb is adb-cutover contract criterion 4:
// given the freeze lock is held, every write verb (legacy or store-backed)
// refuses with a clear error naming the freeze. Args are chosen only to
// satisfy each command's arity — cobra validates Args before
// PersistentPreRunE runs, but flag values and ticket existence are not
// checked until the command body, which the freeze must prevent from ever
// running.
func TestFreezeRefusesEveryWriteVerb(t *testing.T) {
	live, _ := canonicalWorklogFixture(t)
	if _, err := freeze.Acquire(live, "test cutover"); err != nil {
		t.Fatal(err)
	}

	verbs := [][]string{
		{"add", "--dir", live},
		{"start", "nonexistent", "--dir", live},
		{"done", "nonexistent", "--dir", live},
		{"edit", "nonexistent", "--dir", live},
		{"pr", "nonexistent", "--dir", live},
		{"link", "nonexistent", "--dir", live},
		{"note", "nonexistent", "text", "--dir", live},
		{"reindex", "--dir", live},
		{"feedback", "append", "--dir", live},
		{"feedback", "resolve", "12345", "--dir", live},
		{"import", "--dir", live},
		{"wait", "nonexistent", "--dir", live},
		{"task", "scorecard", "add", "criterion", "--id", "x", "--child", "y", "--dir", live},
	}

	for _, args := range verbs {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			err := runCLIExpectingFailure(t, args...)
			if err == nil {
				t.Fatalf("worklog %v: succeeded while frozen", args)
			}
			if !strings.Contains(err.Error(), "frozen") {
				t.Errorf("worklog %v: error %q does not name the freeze", args, err)
			}
			if !strings.Contains(err.Error(), "test cutover") {
				t.Errorf("worklog %v: error %q does not name the freeze reason", args, err)
			}
		})
	}
}

// TestFreezeAllowsReadCommands proves the freeze gate is a default-deny
// allowlist, not a default-allow one that happens to block writes: named
// read-only commands keep working while frozen.
func TestFreezeAllowsReadCommands(t *testing.T) {
	live, _ := canonicalWorklogFixture(t)
	if _, err := freeze.Acquire(live, "test cutover"); err != nil {
		t.Fatal(err)
	}

	reads := [][]string{
		{"status", "--dir", live},
		{"validate", "--dir", live},
		{"standup", "--dir", live},
	}
	for _, args := range reads {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			// Only assert the freeze gate let the command through — the
			// fixture corpus is not guaranteed to be validate-clean, so a
			// command's own domain error is not a freeze-gate failure.
			if err := runCLIExpectingFailure(t, args...); err != nil && strings.Contains(err.Error(), "frozen") {
				t.Errorf("worklog %v: refused by the freeze gate: %v", args, err)
			}
		})
	}
}

// TestFreezeCommandItselfRunsWhileFrozen: "freeze status"/"freeze release"
// must work while frozen, or a held freeze could never be inspected or
// lifted.
func TestFreezeCommandItselfRunsWhileFrozen(t *testing.T) {
	live, _ := canonicalWorklogFixture(t)
	if _, err := freeze.Acquire(live, "test cutover"); err != nil {
		t.Fatal(err)
	}

	if err := runCLIExpectingFailure(t, "freeze", "status", "--dir", live); err != nil {
		t.Errorf("freeze status: refused while frozen: %v", err)
	}
	if err := runCLIExpectingFailure(t, "freeze", "release", "--dir", live); err != nil {
		t.Errorf("freeze release: refused while frozen: %v", err)
	}
	frozen, _, err := freeze.Check(live)
	if err != nil {
		t.Fatal(err)
	}
	if frozen {
		t.Error("still frozen after freeze release")
	}
}

// TestFreezeWriteVerbsSucceedUnfrozen guards against the gate being
// accidentally inverted (blocking everything or nothing): a representative
// write verb must still succeed with no freeze held.
func TestFreezeWriteVerbsSucceedUnfrozen(t *testing.T) {
	live, _, _ := storeWriteFixture(t)
	if err := runCLIExpectingFailure(t, "edit", "solo", "--status", "unfrozen edit", "--dir", live); err != nil {
		t.Errorf("edit failed with no freeze held: %v", err)
	}
}
