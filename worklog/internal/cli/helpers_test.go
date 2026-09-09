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

	"github.com/prestontallen/ai-devboard/worklog/internal/boardmap"
	"github.com/prestontallen/ai-devboard/worklog/internal/convert"
	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/projection"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/sqlitestore"
	"github.com/prestontallen/ai-devboard/worklog/internal/storepath"
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

// boardOf returns a ticket's board shape, read from the store.
//
// Tests used to read this off the rendered devboard YAML. The shape is
// identical either way because boardmap produces both, and the rendered
// projection is retired (adb-retire-devboard-dir-2), so the store is where
// the shape now comes from.
func boardOf(t *testing.T, dir, slug string) devboard.Task {
	t.Helper()
	wd, err := model.NewWorkdir(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, _, err := openStore(storeExisting, storepath.DB(wd.Root))
	if err != nil {
		t.Fatalf("opening the store under %s: %v", dir, err)
	}
	defer s.Close()
	tk, err := s.TicketBySlug(slug)
	if err != nil {
		t.Fatalf("no ticket %q in the store: %v", slug, err)
	}
	var kids []*store.Ticket
	if tk.Type == store.TypeEpic {
		if kids, err = s.Children(tk.ID); err != nil {
			t.Fatal(err)
		}
	}
	return *boardmap.BoardTask(tk, kids)
}

// rendersOwnCard reports whether a ticket would render a top-level card of
// its own, which is the renderer's rule: board-tracked and not a child. A
// child is board-tracked too; what it does not get is a card, because it
// nests in its epic's.
func rendersOwnCard(t *testing.T, dir, slug string) bool {
	t.Helper()
	wd, err := model.NewWorkdir(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, _, err := openStore(storeExisting, storepath.DB(wd.Root))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tk, err := s.TicketBySlug(slug)
	if err != nil {
		return false
	}
	return tk.BoardTracked && tk.ParentID == ""
}

// seedStore stands up a worklog root with the given tickets written
// straight into the store, and no devboard directory anywhere.
//
// Fixtures used to build this state by rendering to files and adopting
// them back. That worked while the rendered board YAML carried in-flight
// detail; it does not now, because the markdown surfaces have nowhere to
// put a phase, a plan step or a scout attestation, so a round trip through
// files silently dropped exactly the fields these tests are about
// (adb-retire-devboard-dir-2).
func seedStore(t *testing.T, tickets ...*store.Ticket) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("WORKLOG_DIR", dir)
	t.Setenv("DEVBOARD_DATA", filepath.Join(dir, "nowhere"))
	if err := os.WriteFile(filepath.Join(dir, "WORK.md"),
		[]byte("# Worklog — active\n\n## Now\n\n## Waiting\n\n## Next\n\n## Someday\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dbPath := storepath.DB(dir)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := sqlitestore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range tickets {
		if err := s.PutTicket(tk); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// The markdown surfaces still render; seed them from what was written
	// so a command reading WORK.md sees the same corpus.
	wd, err := model.NewWorkdir(dir)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := sqlitestore.Open(storepath.DB(wd.Root))
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if err := projection.RenderTo(s2, projection.Layout{WorklogDir: wd.Root}); err != nil {
		t.Fatal(err)
	}
	return dir
}
