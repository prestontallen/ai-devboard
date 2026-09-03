package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain hard-disables devboard and store-backed-write side effects for
// every test in this package by pointing DEVBOARD_DATA/WORKLOG_MIGRATION_DATA
// at nonexistent paths and forcing WORKLOG_STORE_WRITE off. Without this,
// commands under test (start/done/pr/task/...) would write into the
// developer's real ~/.local/share/devboard and ~/.local/share/worklog-migration
// — the latter is now a live risk since adb-cutover's flip made
// storeWriteEnabled() default true. Tests that exercise store-backed or
// devboard behavior override per-test with t.Setenv, same as before.
func TestMain(m *testing.M) {
	os.Setenv("DEVBOARD_DATA", filepath.Join(os.TempDir(), "devboard-disabled-for-tests"))
	os.Setenv("WORKLOG_STORE_WRITE", "0")
	os.Setenv("WORKLOG_MIGRATION_DATA", filepath.Join(os.TempDir(), "worklog-migration-disabled-for-tests"))
	os.Exit(m.Run())
}
