package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/convert"
	"github.com/prestontallen/ai-devboard/worklog/internal/projection"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
)

// TestStoreSyncDisabledIsSilent: without WORKLOG_STORE_SYNC, a real write
// verb through the actual CLI must never mention storesync — the flag's
// off-by-default cost must be exactly zero, in output as well as behavior.
func TestStoreSyncDisabledIsSilent(t *testing.T) {
	live, dataDir := canonicalWorklogFixture(t)
	t.Setenv("WORKLOG_STORE_SYNC", "")
	t.Setenv("WORKLOG_MIGRATION_DATA", filepath.Join(t.TempDir(), "migration"))
	t.Setenv("DEVBOARD_DATA", dataDir)

	_, stderr := runCLI(t, "edit", "--dir", live, "solo", "--status", "no sync")
	if strings.Contains(stderr, "storesync") {
		t.Errorf("disabled shadow-sync produced output: %q", stderr)
	}
}

// TestStoreSyncCleanAfterRealWrites is adb-cutover M2's per-verb parity
// proof (contract criterion 2): starting from a genuinely canonical
// corpus (a render fixpoint — the hand-authored fixture is deliberately
// not one, see internal/verify's TestVerifyCleanCorpus), a representative
// write verb from each of the three integration shapes wired this
// milestone — a WORK.md-family verb (edit), a notes-file verb (note),
// and the task<sub> family (task scorecard add, via devboard.Mutate) —
// must each report zero drift when run with the shadow-sync flag on.
// storesync's own package tests already prove the derive+verify mechanism
// itself; this proves the CLI-layer wiring calls it correctly, once per
// command, from clean state.
func TestStoreSyncCleanAfterRealWrites(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"edit", []string{"edit", "solo", "--status", "clean status"}},
		{"note", []string{"note", "solo", "a clean smoke-test note"}},
		{"task-scorecard-add", []string{"task", "scorecard", "add", "clean criterion", "--id", "an-epic", "--child", "kid-live"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			live, dataDir := canonicalWorklogFixture(t)
			t.Setenv("WORKLOG_STORE_SYNC", "1")
			t.Setenv("WORKLOG_MIGRATION_DATA", filepath.Join(t.TempDir(), "migration"))
			t.Setenv("DEVBOARD_DATA", dataDir)

			args := append([]string{}, tc.args...)
			// --dir only applies to worklog-rooted commands; task addresses
			// the devboard file by --id/--child and has no --dir flag, so it
			// relies on $WORKLOG_DIR for the shadow-sync's own resolveWorkdir.
			if tc.name == "task-scorecard-add" {
				t.Setenv("WORKLOG_DIR", live)
			} else {
				args = append(args[:1], append([]string{"--dir", live}, args[1:]...)...)
			}

			_, stderr := runCLI(t, args...)
			if strings.Contains(stderr, "storesync: drift found") {
				t.Errorf("%s: unexpected drift against a canonical corpus:\n%s", tc.name, stderr)
			}
			if strings.Contains(stderr, "storesync: derive") || strings.Contains(stderr, "storesync: verify") || strings.Contains(stderr, "storesync: open") {
				t.Errorf("%s: shadow-sync hard error:\n%s", tc.name, stderr)
			}
		})
	}
}

// canonicalWorklogFixture renders the hazard-covering fixture corpus once
// into a fresh directory, producing a genuine render fixpoint (0 drift
// against itself) so a subsequent single write's drift, if any, is
// attributable to that write and not to pre-existing fixture quirks.
func canonicalWorklogFixture(t *testing.T) (live, devboardDataDir string) {
	t.Helper()
	s := memstore.New()
	c, err := convert.ReadCorpusDir("../convert/testdata/corpus")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := convert.Load(s, c); err != nil {
		t.Fatal(err)
	}
	live = t.TempDir()
	if err := projection.RenderAll(s, live); err != nil {
		t.Fatal(err)
	}
	devboardDataDir = filepath.Join(live, "devboard")
	if err := os.MkdirAll(devboardDataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return live, devboardDataDir
}

// runCLI executes the real root command against args and returns its
// stdout/stderr, restoring the package-level --dir flag state afterward
// (see start_worktree_test.go's runStartCmd for the same pattern).
//
// storesync.WarnAfterWrite writes to the process's real os.Stderr, not
// cobra's cmd.ErrOrStderr() — root.SetErr alone would miss it — so the
// real fd is redirected through a pipe for the duration of the call.
func runCLI(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()
	prev := flagDir
	t.Cleanup(func() { flagDir = prev })

	realStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	captured := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		captured <- string(b)
	}()

	root := newRoot()
	var out, cobraErrOut strings.Builder
	root.SetOut(&out)
	root.SetErr(&cobraErrOut)
	root.SetArgs(args)
	execErr := root.Execute()

	w.Close()
	os.Stderr = realStderr
	realStderrOutput := <-captured

	if execErr != nil {
		t.Fatalf("worklog %v: %v\nstdout: %s\nstderr: %s%s",
			args, execErr, out.String(), cobraErrOut.String(), realStderrOutput)
	}
	return out.String(), cobraErrOut.String() + realStderrOutput
}
