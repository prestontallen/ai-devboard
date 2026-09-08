package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/storepath"
)

// TestWriteVerbSaysNotAdopted is criterion 14.
//
// sqlitestore.Open CREATES the database when the path is absent, so before
// this guard a machine with no store proceeded against an EMPTY one: Render
// emitted a WORK.md the corpus did not match and EditedIn reported every
// file as hand-edited, on a machine where nobody had edited anything. The
// advice that refusal gave — the store is the source, reconcile the files —
// means deleting the corpus when the store is empty.
func TestWriteVerbSaysNotAdopted(t *testing.T) {
	live, board := canonicalWorklogFixture(t)
	t.Setenv("DEVBOARD_DATA", board)
	// A corpus whose derived store directory holds no database: the
	// fresh-machine case.
	t.Setenv("WORKLOG_DIR", live)
	t.Setenv("WORKLOG_STORE_SYNC", "")

	_, stderr, err := runCLIAllowErr(t, "add", "--dir", live, "--id", "fresh", "--title", "First ticket")
	if err == nil {
		t.Fatal("add succeeded on a machine with no store")
	}
	stderr += err.Error()
	if !strings.Contains(stderr, "has not adopted") {
		t.Errorf("stderr = %q, want it to say the machine has not adopted", stderr)
	}
	if !strings.Contains(stderr, "worklog adopt") {
		t.Errorf("stderr = %q, want it to name the command that fixes this", stderr)
	}
	if strings.Contains(stderr, "edited by hand") {
		t.Errorf("stderr blames a hand edit on a machine with no store: %q", stderr)
	}
}

// TestTaskVerbSaysNotAdopted: the task<sub> family opens the store on its
// own path, so it needs the same guard.
func TestTaskVerbSaysNotAdopted(t *testing.T) {
	live, board := canonicalWorklogFixture(t)
	t.Setenv("DEVBOARD_DATA", board)
	t.Setenv("WORKLOG_DIR", live)
	t.Setenv("WORKLOG_STORE_SYNC", "")

	_, stderr, err := runCLIAllowErr(t, "task", "phase", "intake", "--dir", live, "--id", "solo")
	if err == nil {
		t.Fatal("task succeeded on a machine with no store")
	}
	stderr += err.Error()
	if !strings.Contains(stderr, "has not adopted") {
		t.Errorf("stderr = %q, want it to say the machine has not adopted", stderr)
	}
}

// TestAdoptCommitLeavesAWritableMachine pins the gap that only showed up by
// running the command end to end: adopt canonicalised the corpus and verify
// reported clean, but it converted into an ephemeral store, so no database
// existed at the path every write verb opens and the machine was still
// unable to write. Tests passed; the feature did not work.
func TestAdoptCommitLeavesAWritableMachine(t *testing.T) {
	live, board := canonicalWorklogFixture(t)
	dataDir := storepath.Dir(live)
	t.Setenv("DEVBOARD_DATA", board)
	t.Setenv("WORKLOG_DIR", live)
	t.Setenv("WORKLOG_STORE_SYNC", "")

	if _, stderr := runCLI(t, "adopt", "--commit", "--dir", live); strings.Contains(stderr, "error") {
		t.Fatalf("adopt --commit: %s", stderr)
	}
	if _, err := os.Stat(storepath.DBIn(dataDir)); err != nil {
		t.Fatalf("no store at the path write verbs open: %v", err)
	}
	// The actual proof: a write verb now works.
	if _, stderr := runCLI(t, "add", "--dir", live, "--id", "after-adopt", "--title", "Works now"); strings.Contains(stderr, "error") {
		t.Fatalf("add after adopt: %s", stderr)
	}
}

// TestAdoptDryRunCreatesNoStore: a preview must leave no trace.
func TestAdoptDryRunCreatesNoStore(t *testing.T) {
	live, board := canonicalWorklogFixture(t)
	dataDir := storepath.Dir(live)
	t.Setenv("DEVBOARD_DATA", board)
	t.Setenv("WORKLOG_DIR", live)
	t.Setenv("WORKLOG_STORE_SYNC", "")

	runCLI(t, "adopt", "--dir", live)
	if _, err := os.Stat(storepath.DBIn(dataDir)); !os.IsNotExist(err) {
		t.Error("a dry run created a store")
	}
}

// TestAdoptRollbackLeavesTheStoreAlone is the reason the store directory
// is a SIBLING of the corpus rather than a child of it.
//
// Restore deletes every file under the live roots that its manifest does
// not name. With the database inside the corpus, a rollback would either
// delete it outright or restore a snapshot-era copy over a newer one —
// and restoring an old .db next to a newer -wal is corruption, not merely
// lost data. Outside the roots, the walk never reaches it. This test
// pins that, because "it happens to be somewhere else" is exactly the
// kind of invariant a later refactor breaks without noticing.
func TestAdoptRollbackLeavesTheStoreAlone(t *testing.T) {
	live, board := canonicalWorklogFixture(t)
	t.Setenv("DEVBOARD_DATA", board)
	t.Setenv("WORKLOG_DIR", live)
	t.Setenv("WORKLOG_STORE_SYNC", "")

	stdout, stderr := runCLI(t, "adopt", "--commit", "--dir", live)
	if strings.Contains(stderr, "error") {
		t.Fatalf("adopt --commit: %s", stderr)
	}
	dbPath := storepath.DB(live)
	before, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("no database after adopt: %v", err)
	}

	snapDir := ""
	for _, line := range strings.Split(stdout, "\n") {
		if _, rest, ok := strings.Cut(line, "snapshot: "); ok {
			snapDir, _, _ = strings.Cut(rest, " (")
			break
		}
	}
	if snapDir == "" {
		t.Fatalf("could not find the snapshot directory in adopt's output:\n%s", stdout)
	}

	// The snapshot itself must live outside the corpus, or the next
	// conversion would read it back as corpus.
	if strings.HasPrefix(snapDir, live+string(os.PathSeparator)) {
		t.Errorf("snapshot %s is inside the live root %s", snapDir, live)
	}

	if _, stderr := runCLI(t, "adopt", "--rollback", snapDir, "--dir", live); strings.Contains(stderr, "error") {
		t.Fatalf("adopt --rollback: %s", stderr)
	}

	after, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("rollback destroyed the database at %s: %v", dbPath, err)
	}
	if len(before) != len(after) {
		t.Errorf("rollback rewrote the database: %d bytes before, %d after", len(before), len(after))
	}
}
