package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// invokeMigrate drives the migrate cobra subcommand, pointing the worklog
// source at worklogDir (via the package's --dir-equivalent global) and
// devboard at an empty temp dir unless the caller has already set
// DEVBOARD_DATA.
func invokeMigrate(t *testing.T, worklogDir string, args ...string) (string, error) {
	t.Helper()
	prev := flagDir
	flagDir = worklogDir
	t.Cleanup(func() { flagDir = prev })

	cmd := newMigrateCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func cliWorkMDFixture() string {
	return `# Work

## Now
- [ ] **SOLO-A** — First ticket
  - **ID**: solo-a
  - **Repo**: repo

## Next
## Someday
`
}

func newCLIFixtureDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "WORK.md"), []byte(cliWorkMDFixture()), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestMigrateRespectsDataDirOverride(t *testing.T) {
	worklogDir := newCLIFixtureDir(t)
	t.Setenv("DEVBOARD_DATA", t.TempDir())

	t.Run("env var", func(t *testing.T) {
		dataDir := filepath.Join(t.TempDir(), "via-env")
		t.Setenv("WORKLOG_MIGRATION_DATA", dataDir)

		if _, err := invokeMigrate(t, worklogDir, "--json"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dataDir, "worklog.db")); err != nil {
			t.Errorf("expected the output db under %s: %v", dataDir, err)
		}
	})

	t.Run("flag wins over env var", func(t *testing.T) {
		envDir := filepath.Join(t.TempDir(), "via-env")
		flagOutDir := filepath.Join(t.TempDir(), "via-flag")
		t.Setenv("WORKLOG_MIGRATION_DATA", envDir)

		if _, err := invokeMigrate(t, worklogDir, "--json", "--out", flagOutDir); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(flagOutDir, "worklog.db")); err != nil {
			t.Errorf("expected the output db under the --out dir %s: %v", flagOutDir, err)
		}
		if _, err := os.Stat(filepath.Join(envDir, "worklog.db")); err == nil {
			t.Error("--out should take priority over $WORKLOG_MIGRATION_DATA, but the env dir got the output db")
		}
	})
}

func TestMigrateJSONSingleDocument(t *testing.T) {
	worklogDir := newCLIFixtureDir(t)
	t.Setenv("DEVBOARD_DATA", t.TempDir())
	t.Setenv("WORKLOG_MIGRATION_DATA", t.TempDir())

	out, err := invokeMigrate(t, worklogDir, "--json")
	if err != nil {
		t.Fatal(err)
	}

	dec := json.NewDecoder(strings.NewReader(out))
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, out)
	}
	if dec.More() {
		t.Fatalf("stdout has more than one JSON value:\n%s", out)
	}
	for _, key := range []string{"tickets", "feedback", "diff", "staleRows", "backedUp", "outputPath"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("JSON document missing %q: %v", key, doc)
		}
	}
	diff, ok := doc["diff"].(map[string]any)
	if !ok {
		t.Fatalf("diff is not an object: %v", doc["diff"])
	}
	for _, key := range []string{"added", "removed", "changed"} {
		if _, isSlice := diff[key].([]any); !isSlice {
			t.Errorf("diff.%s should serialize as an array, not %#v (null breaks naive array consumers)", key, diff[key])
		}
	}
}

// TestMigrateJSONReportsTimestampedBackupPath is contract criterion 5's
// filename half: --json carries the exact backup filename a real backup
// landed at, distinguishable from the plain ".bak" it replaced.
func TestMigrateJSONReportsTimestampedBackupPath(t *testing.T) {
	worklogDir := newCLIFixtureDir(t)
	t.Setenv("DEVBOARD_DATA", t.TempDir())
	t.Setenv("WORKLOG_MIGRATION_DATA", t.TempDir())

	out1, err := invokeMigrate(t, worklogDir, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var doc1 map[string]any
	if err := json.Unmarshal([]byte(out1), &doc1); err != nil {
		t.Fatal(err)
	}
	if bp, ok := doc1["backupPath"]; ok && bp != "" {
		t.Errorf("first run should have no backupPath, got %v", bp)
	}

	out2, err := invokeMigrate(t, worklogDir, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var doc2 map[string]any
	if err := json.Unmarshal([]byte(out2), &doc2); err != nil {
		t.Fatal(err)
	}
	bp, _ := doc2["backupPath"].(string)
	outputPath, _ := doc2["outputPath"].(string)
	if bp == "" {
		t.Fatal("second run should report a backupPath")
	}
	if bp == outputPath+".bak" {
		t.Errorf("backupPath = %q looks like the old single overwritten .bak, not a timestamped name", bp)
	}
	if !strings.HasPrefix(bp, outputPath+".bak.") {
		t.Errorf("backupPath = %q, want prefix %q", bp, outputPath+".bak.")
	}
}

// TestMigrateSharesRuntimeStorePath is contract criterion 5's other half:
// migrate's OutputPath and the store-backed write path's runtime open
// call (storeDataDir, in task_store.go/storesync.go) must resolve to the
// same directory whenever the final cutover migrate run takes no --out
// override — otherwise the flipped-default CLI would open a different db
// than the one the freeze-window migrate just produced.
func TestMigrateSharesRuntimeStorePath(t *testing.T) {
	t.Run("default (no env)", func(t *testing.T) {
		t.Setenv("WORKLOG_MIGRATION_DATA", "")
		migrateDir, err := resolveMigrateDataDir("")
		if err != nil {
			t.Fatal(err)
		}
		runtimeDir, err := storeDataDir()
		if err != nil {
			t.Fatal(err)
		}
		if migrateDir != runtimeDir {
			t.Errorf("migrate default dir %q != runtime store dir %q", migrateDir, runtimeDir)
		}
	})

	t.Run("WORKLOG_MIGRATION_DATA set", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("WORKLOG_MIGRATION_DATA", dir)
		migrateDir, err := resolveMigrateDataDir("")
		if err != nil {
			t.Fatal(err)
		}
		runtimeDir, err := storeDataDir()
		if err != nil {
			t.Fatal(err)
		}
		if migrateDir != dir || runtimeDir != dir {
			t.Errorf("migrate dir %q, runtime dir %q, want both %q", migrateDir, runtimeDir, dir)
		}
	})
}

func TestMigrateTextSummary(t *testing.T) {
	worklogDir := newCLIFixtureDir(t)
	t.Setenv("DEVBOARD_DATA", t.TempDir())
	t.Setenv("WORKLOG_MIGRATION_DATA", t.TempDir())

	out, err := invokeMigrate(t, worklogDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"converted 1 tickets", "id-set diff", "baseline run"} {
		if !strings.Contains(out, want) {
			t.Errorf("text summary missing %q:\n%s", want, out)
		}
	}

	// Second run: no-flag output should read as a legible verdict without
	// --json, including the backup line.
	out2, err := invokeMigrate(t, worklogDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2, "backed up to") {
		t.Errorf("second run's summary missing the backup line:\n%s", out2)
	}
}
