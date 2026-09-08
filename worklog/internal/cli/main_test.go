package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/storepath"
)

// TestMain hard-disables devboard and store-backed write side effects for
// every test in this package by pointing DEVBOARD_DATA and WORKLOG_DIR at
// nonexistent paths. Without this, commands under test (start, done, pr,
// task and the rest) would write into the developer's real
// ~/.local/share/devboard and ~/.local/share/worklog — every write verb is
// unconditionally store-backed, so a real data directory is a live risk,
// not an opt-in one.
//
// WORKLOG_DIR is now the whole guard, and that is the point: the store
// directory is DERIVED from it (internal/storepath), so one variable moves
// the corpus and the database together. Previously this needed two, and a
// test that set only one of them reached the real store through the other.
// Tests that exercise store-backed behavior override per-test with
// t.Setenv, pointing WORKLOG_DIR at a scratch fixture.
func TestMain(m *testing.M) {
	os.Setenv("DEVBOARD_DATA", filepath.Join(os.TempDir(), "devboard-disabled-for-tests"))
	os.Setenv("WORKLOG_DIR", filepath.Join(os.TempDir(), "worklog-disabled-for-tests"))
	os.Exit(m.Run())
}

// TestGuardKeepsTestsOffTheRealStore is the test that proves the guard
// above still guards. It is deliberately paranoid: this package's write
// verbs open a real SQLite database, and the failure mode being prevented
// is a test run quietly writing the developer's actual worklog.
func TestGuardKeepsTestsOffTheRealStore(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	realCorpus := filepath.Join(home, ".local", "share", "worklog")

	if got := os.Getenv("WORKLOG_DIR"); got == realCorpus {
		t.Fatalf("WORKLOG_DIR points at the real corpus %s", realCorpus)
	}
	// The derived store must be nowhere near the real one either.
	if storepath.Dir(os.Getenv("WORKLOG_DIR")) == storepath.Dir(realCorpus) {
		t.Fatalf("the derived store directory is the real one at %s", storepath.Dir(realCorpus))
	}

	// A write verb with no --dir must refuse rather than reach anything real.
	_, stderr, err := runCLIAllowErr(t, "start", "anything")
	if err == nil && stderr == "" {
		t.Error("a write verb with no --dir produced no error; it may have reached a real store")
	}
}
