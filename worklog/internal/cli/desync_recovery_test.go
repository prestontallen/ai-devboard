package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDesyncRecoveryUnblocksWrites reconstructs the state one board click
// produced on 2026-09-08 and asserts a write goes through.
//
// The old handler renamed the file into _archive/ and then failed to record
// it, so the store still rendered the task live while the bytes sat at the
// archived path. EditedIn saw a rendered file missing from disk, called it a
// hand edit, and refused — so the CLI could not write anything at all until
// someone happened to know that un-archiving from the board would put the
// file back.
//
// A file byte-identical to its render at its sibling path is a move the
// store missed, not something a person wrote, and the next render puts it
// where the store says it belongs. Waving it through loses nothing.
func TestDesyncRecoveryUnblocksWrites(t *testing.T) {
	live, board, _ := storeWriteFixture(t)
	livePath := filepath.Join(board, "ai-devboard", "an-epic.yaml")
	arcPath := filepath.Join(board, "ai-devboard", "_archive", "an-epic.yaml")

	body, err := os.ReadFile(livePath)
	if err != nil {
		t.Fatalf("fixture should render an-epic live: %v", err)
	}
	// Exactly what the old handler left behind: file moved, store untouched.
	if err := os.MkdirAll(filepath.Dir(arcPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(arcPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(livePath); err != nil {
		t.Fatal(err)
	}

	// The write that used to be refused. an-epic's own detail lives per
	// child, so the verb takes --child; the file it rewrites is the epic's.
	_, stderr := runCLI(t, "task", "phase", "verify", "--id", "an-epic",
		"--child", "kid-live", "--dir", live)
	if strings.Contains(stderr, "refusing to write") {
		t.Fatalf("a missed move still blocks writes: %s", stderr)
	}
	if strings.Contains(stderr, "error") {
		t.Fatalf("write failed: %s", stderr)
	}

	// And it healed rather than merely tolerating: the file is back where the
	// store says it lives, with nothing left at the other path.
	if _, err := os.Stat(livePath); err != nil {
		t.Errorf("the write did not restore the file to its rendered path: %v", err)
	}
	if _, err := os.Stat(arcPath); err == nil {
		t.Error("the stale copy survived, so the board would show the task twice")
	}
}

// A genuine hand edit at the sibling path must still refuse: the recovery
// above is narrow on purpose, and this is the line it must not cross.
func TestDesyncRecoveryStillRefusesAnEdit(t *testing.T) {
	live, board, _ := storeWriteFixture(t)
	livePath := filepath.Join(board, "ai-devboard", "an-epic.yaml")
	arcPath := filepath.Join(board, "ai-devboard", "_archive", "an-epic.yaml")
	if err := os.MkdirAll(filepath.Dir(arcPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(arcPath, []byte("title: written by a person\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(livePath); err != nil {
		t.Fatal(err)
	}

	err := runCLIExpectingFailure(t, "task", "phase", "verify", "--id", "an-epic",
		"--child", "kid-live", "--dir", live)
	if err == nil || !strings.Contains(err.Error(), "refusing to write") {
		t.Errorf("a hand-edited projection was overwritten instead of refused: %v", err)
	}
}
