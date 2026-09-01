package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain hard-disables devboard side effects for every test in this
// package by pointing DEVBOARD_DATA at a nonexistent path. Without this,
// commands under test (start/done/pr) would write task files into the
// developer's real ~/.local/share/devboard. Tests that exercise devboard
// behavior override per-test with t.Setenv.
func TestMain(m *testing.M) {
	os.Setenv("DEVBOARD_DATA", filepath.Join(os.TempDir(), "devboard-disabled-for-tests"))
	os.Exit(m.Run())
}
