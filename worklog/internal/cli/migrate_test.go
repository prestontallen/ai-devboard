package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/storepath"
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

	t.Run("derived from the corpus by default", func(t *testing.T) {
		if _, err := invokeMigrate(t, worklogDir, "--json"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(storepath.DB(worklogDir)); err != nil {
			t.Errorf("expected the output db at %s: %v", storepath.DB(worklogDir), err)
		}
	})

	t.Run("--out wins over the derived location", func(t *testing.T) {
		corpus := newCLIFixtureDir(t)
		flagOutDir := filepath.Join(t.TempDir(), "via-flag")

		if _, err := invokeMigrate(t, corpus, "--json", "--out", flagOutDir); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(storepath.DBIn(flagOutDir)); err != nil {
			t.Errorf("expected the output db under the --out dir %s: %v", flagOutDir, err)
		}
		if _, err := os.Stat(storepath.DB(corpus)); err == nil {
			t.Error("--out should take priority, but the derived location got the output db too")
		}
	})
}

// TestRetiredEnvIsRefused: somebody who still exports the old variable
// meant to send the store somewhere specific. Honoring it would write to a
// location the derived path knows nothing about; ignoring it silently
// would write somewhere they did not intend. Refuse and say so.
func TestRetiredEnvIsRefused(t *testing.T) {
	worklogDir := newCLIFixtureDir(t)
	t.Setenv("DEVBOARD_DATA", t.TempDir())
	t.Setenv(storepath.LegacyEnv, t.TempDir())

	_, err := invokeMigrate(t, worklogDir, "--json")
	if err == nil {
		t.Fatal("migrate accepted a still-set " + storepath.LegacyEnv)
	}
}

func TestMigrateJSONSingleDocument(t *testing.T) {
	worklogDir := newCLIFixtureDir(t)
	t.Setenv("DEVBOARD_DATA", t.TempDir())

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

// TestMigrateSharesRuntimeStorePath: migrate's default output directory
// and the directory the store-backed write path opens at runtime must be
// the same place. They resolve through one function now, so this is close
// to tautological — it stays because the failure it guards against is
// silent and expensive: a CLI that writes one database while migrate
// produces another.
func TestMigrateSharesRuntimeStorePath(t *testing.T) {
	corpus := t.TempDir()
	wd, err := model.NewWorkdir(corpus)
	if err != nil {
		t.Fatal(err)
	}

	migrateDir, err := resolveMigrateDataDir("", wd)
	if err != nil {
		t.Fatal(err)
	}
	if runtimeDir := storepath.Dir(wd.Root); migrateDir != runtimeDir {
		t.Errorf("migrate default dir %q != runtime store dir %q", migrateDir, runtimeDir)
	}
	if _, err := resolveMigrateDataDir("", wd); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateTextSummary(t *testing.T) {
	worklogDir := newCLIFixtureDir(t)
	t.Setenv("DEVBOARD_DATA", t.TempDir())

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
