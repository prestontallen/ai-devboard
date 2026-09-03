package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain hard-disables devboard and store-backed-write side effects for
// every test in this package by pointing DEVBOARD_DATA/WORKLOG_MIGRATION_DATA
// at nonexistent paths. Without this, commands under test
// (start/done/pr/task/...) would write into the developer's real
// ~/.local/share/devboard and ~/.local/share/worklog-migration — every
// write verb is unconditionally store-backed (adb-cutover M3d/M4), so a
// real WORKLOG_MIGRATION_DATA is a live risk, not an opt-in one. Tests
// that exercise store-backed or devboard behavior override per-test with
// t.Setenv, same as before — pointing WORKLOG_MIGRATION_DATA at a real
// migrated store built from a scratch fixture.
func TestMain(m *testing.M) {
	os.Setenv("DEVBOARD_DATA", filepath.Join(os.TempDir(), "devboard-disabled-for-tests"))
	os.Setenv("WORKLOG_MIGRATION_DATA", filepath.Join(os.TempDir(), "worklog-migration-disabled-for-tests"))
	os.Exit(m.Run())
}
