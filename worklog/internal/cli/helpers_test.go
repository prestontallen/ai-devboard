package cli

// Shared CLI test helpers. These used to live in the storesync integration
// test file, which went with internal/storesync; the helpers themselves are
// used by most of this package's tests and have nothing to do with that
// hook, so they moved here rather than being deleted with their old home.

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
