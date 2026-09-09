package cli

import (
	"os"
	"path/filepath"
	"strings"
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
//
// HOME and the XDG variables are pinned for the same reason, and the reason
// is newer: uninstall RESOLVES ITS TARGETS FROM HOME and then deletes them.
// Until it existed, no command in this package could destroy anything outside
// the worklog data directory, so isolating that directory was enough. Now a
// test that forgets installSandbox would resolve the developer's real binary,
// their real skills, their real settings.json — and remove them. WORKLOG_DIR
// would not have saved any of it.
//
// realHome is captured BEFORE the override, because os.UserHomeDir reads $HOME:
// once it is pinned, nothing can tell what the real home was, and the guard
// tests below need exactly that to prove they are not standing on it.
func TestMain(m *testing.M) {
	if h, err := os.UserHomeDir(); err == nil {
		realHome = h
	}
	sandbox := filepath.Join(os.TempDir(), "home-disabled-for-tests")
	os.Setenv("HOME", sandbox)
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(sandbox, ".config"))
	os.Setenv("XDG_DATA_HOME", filepath.Join(sandbox, ".local", "share"))
	os.Setenv("DEVBOARD_DATA", filepath.Join(os.TempDir(), "devboard-disabled-for-tests"))
	os.Setenv("WORKLOG_DIR", filepath.Join(os.TempDir(), "worklog-disabled-for-tests"))
	os.Exit(m.Run())
}

// realHome is the developer's actual home directory, captured before TestMain
// pins $HOME. Empty only when the machine has no resolvable home.
var realHome string

// TestGuardKeepsTestsOffTheRealStore is the test that proves the guard
// above still guards. It is deliberately paranoid: this package's write
// verbs open a real SQLite database, and the failure mode being prevented
// is a test run quietly writing the developer's actual worklog.
func TestGuardKeepsTestsOffTheRealStore(t *testing.T) {
	if realHome == "" {
		t.Skip("no home directory")
	}
	realCorpus := filepath.Join(realHome, ".local", "share", "worklog")

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

// TestGuardKeepsTestsOffTheRealHome is the store guard's twin, and it exists
// because `uninstall` changed what a forgotten sandbox costs. A test that
// reaches the real store writes rows a human can read back; a test that
// reaches the real HOME deletes a binary, four skill directories and two
// files inside settings a human owns. The blast radius stopped being
// recoverable, so the guard stopped being optional.
//
// It asserts on the environment rather than on any one command, because the
// property wanted is "nothing in this package can resolve the real home",
// which no single call site can prove.
func TestGuardKeepsTestsOffTheRealHome(t *testing.T) {
	if realHome == "" {
		t.Skip("no home directory")
	}
	for _, v := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME"} {
		got := os.Getenv(v)
		if got == "" {
			t.Errorf("%s is unset; TestMain must pin it away from %s", v, realHome)
			continue
		}
		if got == realHome || strings.HasPrefix(got, realHome+string(filepath.Separator)) {
			t.Errorf("%s=%s is inside the real home %s", v, got, realHome)
		}
	}
	// os.UserHomeDir is what the installer paths actually call, so pin the
	// thing itself and not only the variable it reads.
	if h, err := os.UserHomeDir(); err == nil && h == realHome {
		t.Errorf("os.UserHomeDir still resolves the real home %s", realHome)
	}
}
