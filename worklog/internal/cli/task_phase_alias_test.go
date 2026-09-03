package cli

import (
	"strings"
	"testing"
)

// The dev-context skill prescribes "phase implement"; the stored value is
// "implementing". The alias makes the documented word work without changing
// what lands on disk.
func TestTaskPhaseImplementAlias(t *testing.T) {
	dir := taskStoreFixture(t, false)
	p := taskFilePath(dir)

	if _, _, err := runTask(t, "phase", "implement", "--id", "tkt"); err != nil {
		t.Fatalf("phase implement: %v", err)
	}
	if got := loadTask(t, p).Phase; got != "implementing" {
		t.Errorf("stored phase = %q, want %q", got, "implementing")
	}
}

func TestTaskPhaseCanonicalStillAccepted(t *testing.T) {
	dir := taskStoreFixture(t, false)
	p := taskFilePath(dir)

	if _, _, err := runTask(t, "phase", "implementing", "--id", "tkt"); err != nil {
		t.Fatalf("phase implementing: %v", err)
	}
	if got := loadTask(t, p).Phase; got != "implementing" {
		t.Errorf("stored phase = %q, want %q", got, "implementing")
	}
}

// An alias must not widen what counts as a phase, and the error still lists
// the canonical set rather than the aliases.
func TestTaskPhaseUnknownStillRefused(t *testing.T) {
	t.Setenv("DEVBOARD_DATA", t.TempDir())

	_, _, err := runTask(t, "phase", "implementer", "--id", "tkt")
	if err == nil {
		t.Fatal("expected refusal for an unknown phase")
	}
	ec, ok := err.(exitCoder)
	if !ok || ec.ExitCode() != 64 {
		t.Fatalf("exit code = %v (want 64)", ec)
	}
	if !strings.Contains(err.Error(), "implementing") {
		t.Errorf("error should list the canonical phase set: %v", err)
	}
}
