package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/sqlitestore"
	"github.com/prestontallen/ai-devboard/worklog/internal/storepath"
)

// The board has no opt-in any more. It used to be os.Stat of the devboard
// data directory, and while that held, deleting the directory turned every
// `worklog task` verb into a silent exit 0 and stopped new tickets ever
// reaching the board. Both were invisible: an agent following the workflow
// would run the commands all turn and see success
// (adb-retire-devboard-dir-2).

// noBoardDir points the CLI at a scratch worklog with no devboard
// directory anywhere, and returns the worklog root.
func noBoardDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("WORKLOG_DIR", dir)
	t.Setenv("DEVBOARD_DATA", filepath.Join(dir, "nowhere"))
	if err := os.WriteFile(filepath.Join(dir, "WORK.md"),
		[]byte("# Worklog — active\n\n## Now\n\n## Waiting\n\n## Next\n\n## Someday\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A store has to exist for the write verbs to run at all; what this
	// fixture withholds is the devboard directory, nothing else.
	dbPath := storepath.DB(dir)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	sq, err := sqlitestore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := sq.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

func openLive(t *testing.T, dir string) model.Workdir {
	t.Helper()
	wd, err := model.NewWorkdir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

// Every write verb writes, and says so. The failure this replaces was not
// an error: it was success with nothing written.
func TestWriteVerbsNeedNoDevboardDir(t *testing.T) {
	dir := noBoardDir(t)
	if _, err := os.Stat(filepath.Join(dir, "nowhere")); !os.IsNotExist(err) {
		t.Fatal("the fixture starts with a devboard directory; the test proves nothing")
	}
	if _, _, err := runCLIAllowErr(t, "add", "--id", "gate-check", "--title", "Gate check", "--repo", "r"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runCLIAllowErr(t, "start", "gate-check"); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		name string
		args []string
		want func(tk map[string]string) bool
	}{
		{"task phase", []string{"task", "phase", "verify", "--id", "gate-check"}, nil},
		{"task plan add", []string{"task", "plan", "add", "write it", "--id", "gate-check"}, nil},
		{"task complexity", []string{"task", "complexity", "low", "--id", "gate-check"}, nil},
		{"edit", []string{"edit", "gate-check", "--acceptance", "it works"}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, errOut, err := runCLIAllowErr(t, c.args...)
			if err != nil {
				t.Fatalf("%v\nstdout: %s\nstderr: %s", err, out, errOut)
			}
			if strings.Contains(errOut, "no-op") {
				t.Errorf("reported a no-op: %s", errOut)
			}
		})
	}

	// And the writes actually landed, which is the half a clean exit code
	// cannot tell you.
	ss, err := openStoreForWrite(openLive(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	defer ss.close()
	tk, err := ss.s.TicketBySlug("gate-check")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Phase != "verify" {
		t.Errorf("phase = %q, want verify", tk.Phase)
	}
	if len(tk.PlanSteps) != 1 {
		t.Errorf("plan steps = %d, want 1", len(tk.PlanSteps))
	}
	if tk.Complexity != "low" {
		t.Errorf("complexity = %q, want low", tk.Complexity)
	}
	if tk.Acceptance != "it works" {
		t.Errorf("acceptance = %q", tk.Acceptance)
	}
}

// Starting a ticket board-tracks it and fills in the fields the board's
// resume button and repo attribution need. While the gate held, all four
// were skipped and the board simply stopped gaining cards for new work.
func TestStartBoardTracksWithoutADirectory(t *testing.T) {
	dir := noBoardDir(t)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "session-abc")
	if _, _, err := runCLIAllowErr(t, "add", "--id", "fresh-ticket", "--title", "Fresh", "--repo", "ai-devboard"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runCLIAllowErr(t, "start", "fresh-ticket"); err != nil {
		t.Fatal(err)
	}

	ss, err := openStoreForWrite(openLive(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	defer ss.close()
	tk, err := ss.s.TicketBySlug("fresh-ticket")
	if err != nil {
		t.Fatal(err)
	}
	if !tk.BoardTracked {
		t.Error("a started ticket is not board-tracked, so it will never get a card")
	}
	if tk.Session != "session-abc" {
		t.Errorf("session = %q, want the one in the environment", tk.Session)
	}
}

// Adoption used to walk a second, rendered board root. It passed that path
// unconditionally, and a walk over a non-empty root that does not exist
// errors rather than skipping — so the moment the directory went, adopt
// would have failed outright (adb-retire-devboard-dir-2).
func TestAdoptWithoutABoardRoot(t *testing.T) {
	dir := noBoardDir(t)
	if _, _, err := runCLIAllowErr(t, "add", "--id", "adopted", "--title", "Adopted", "--repo", "r"); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"adopt", "--dir", dir},
		{"adopt", "--commit", "--dir", dir},
	} {
		out, errOut, err := runCLIAllowErr(t, args...)
		if err != nil {
			t.Fatalf("%v: %v\nstdout: %s\nstderr: %s", args, err, out, errOut)
		}
		if strings.Contains(errOut, "rolled back") {
			t.Errorf("%v rolled back: %s", args, errOut)
		}
	}
	// And the ticket is still there afterwards, which a rollback would undo.
	// `add` does not board-track; `start` does, so this asks only that the
	// ticket survived.
	if boardOf(t, dir, "adopted").Title != "Adopted" {
		t.Error("the ticket did not survive adoption")
	}
}

// The installer created the board directory and reported its absence as
// drift, so a machine without one was permanently in drift and every
// install put it back. Both are gone.
func TestInstallIgnoresTheDevboardDir(t *testing.T) {
	dir := noBoardDir(t)
	out, errOut, err := runCLIAllowErr(t, "install", "--check")
	combined := out + errOut
	if err != nil && !strings.Contains(combined, "drift") {
		t.Fatalf("install --check: %v\n%s", err, combined)
	}
	if strings.Contains(combined, "devboard data dir") {
		t.Errorf("--check still reports the devboard data dir:\n%s", combined)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "nowhere")); !os.IsNotExist(statErr) {
		t.Error("install --check created the devboard directory")
	}
}
