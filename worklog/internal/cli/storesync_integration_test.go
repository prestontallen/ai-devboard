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
// off-by-default cost must be exactly zero, in output as well as
// behavior. reindex is the exemplar (not a task<sub> verb, edit, note, or
// any other write command: adb-cutover M4 made every one of them
// unconditionally store-backed, leaving storesync.WarnAfterWrite with no
// legacy write anywhere left to shadow-verify against). reindex still
// calls it as a belt-and-suspenders check after regenerating INDEX.md,
// independent of that retirement — the one production call site left.
func TestStoreSyncDisabledIsSilent(t *testing.T) {
	live, _, _ := storeWriteFixture(t)
	t.Setenv("WORKLOG_STORE_SYNC", "")

	_, stderr := runCLI(t, "reindex", "--dir", live)
	if strings.Contains(stderr, "storesync") {
		t.Errorf("disabled shadow-sync produced output: %q", stderr)
	}
}

// TestStoreSyncCleanAfterRealWrites is adb-cutover M2's per-verb parity
// proof (contract criterion 2), starting from a genuinely canonical
// corpus (a render fixpoint — the hand-authored fixture is deliberately
// not one, see internal/verify's TestVerifyCleanCorpus). storesync's own
// package tests already prove the derive+verify mechanism itself; this
// proves the CLI-layer wiring (reindex, the one remaining call site —
// see TestStoreSyncDisabledIsSilent above) calls it correctly, from
// clean state.
func TestStoreSyncCleanAfterRealWrites(t *testing.T) {
	live, _, _ := storeWriteFixture(t)
	t.Setenv("WORKLOG_STORE_SYNC", "1")

	_, stderr := runCLI(t, "reindex", "--dir", live)
	if strings.Contains(stderr, "storesync: drift found") {
		t.Errorf("unexpected drift against a canonical corpus:\n%s", stderr)
	}
	if strings.Contains(stderr, "storesync: derive") || strings.Contains(stderr, "storesync: verify") || strings.Contains(stderr, "storesync: open") {
		t.Errorf("shadow-sync hard error:\n%s", stderr)
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
	out, errOut, execErr := runCLIAllowErr(t, args...)
	if execErr != nil {
		t.Fatalf("worklog %v: %v\nstdout: %s\nstderr: %s", args, execErr, out, errOut)
	}
	return out, errOut
}

// runCLIAllowErr is runCLI for a command expected to fail: it returns the
// error instead of failing the test, so refusal paths can be asserted.
func runCLIAllowErr(t *testing.T, args ...string) (stdout, stderr string, execErr error) {
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
	execErr = root.Execute()

	w.Close()
	os.Stderr = realStderr
	realStderrOutput := <-captured

	return out.String(), cobraErrOut.String() + realStderrOutput, execErr
}
