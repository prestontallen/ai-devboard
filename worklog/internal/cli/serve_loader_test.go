package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/sqlitestore"
	"github.com/prestontallen/ai-devboard/worklog/internal/storepath"
)

// Two obligations the store-direct loader inherited from the shadow that
// introduced them. They are re-homed here rather than deleted with it,
// because both describe the loader and neither described the diff.

// On a machine that has never adopted a store, a board read is a silent
// no-op and must not create the database: sqlitestore.Open creates and
// migrates, and an empty store minted by a read makes every CLI write
// refuse from then on. The stat-before-open is the whole guard.
func TestLoaderNeverCreatesTheDatabase(t *testing.T) {
	dir := t.TempDir()
	// The loader follows the SERVER's corpus (DEVBOARD_WORKLOG), not the
	// CLI's, so serve renders and reads the same worklog.
	t.Setenv("DEVBOARD_WORKLOG", dir)
	t.Setenv("WORKLOG_DIR", dir)

	snap, err := loadStoreTickets()
	if err != nil {
		t.Fatalf("store-less machine: want a silent no-op, got %v", err)
	}
	if snap != nil {
		t.Fatal("store-less machine: want a nil snapshot, got one")
	}
	if _, err := os.Stat(storepath.DB(dir)); !os.IsNotExist(err) {
		t.Fatalf("the read created a database at %s", storepath.DB(dir))
	}
}

// The positive half, and the second obligation: with a database present
// the loader returns the board's tickets and closes the handle before
// returning. A held handle stops the WAL checkpointing, after which
// `store relocate` refuses forever because it will not copy a database
// with a non-empty WAL.
func TestLoaderReadsAndClosesTheStore(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DEVBOARD_WORKLOG", dir)
	t.Setenv("WORKLOG_DIR", dir)
	path := storepath.DB(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	sq, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	tk := &store.Ticket{Slug: "loader-ticket", Title: "T", Type: store.TypeTicket,
		State: store.StateActive, Section: store.SectionNow, Repo: "r1", BoardTracked: true}
	if err := sq.PutTicket(tk); err != nil {
		t.Fatal(err)
	}
	if err := sq.Close(); err != nil {
		t.Fatal(err)
	}

	snap, err := loadStoreTickets()
	if err != nil {
		t.Fatal(err)
	}
	if snap == nil {
		t.Fatal("adopted store: want a snapshot, got nil")
	}
	var found bool
	for _, task := range snap.Tasks {
		if task.Slug == "loader-ticket" && task.Repo == "r1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("snapshot is missing the ticket; it holds %d", len(snap.Tasks))
	}
	for _, sidecar := range []string{path + "-wal", path + "-shm"} {
		if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
			t.Fatalf("%s outlives the load — the handle was not closed", sidecar)
		}
	}
}

// A ticket the store holds but does not board-track has no card. Dir
// presence used to be the board's opt-in; the flag is what decides it now,
// and getting this wrong would put every ticket ever written on the board.
func TestUntrackedTicketsStayOffTheBoard(t *testing.T) {
	s := seedFromCorpus(t)
	if err := s.PutTicket(&store.Ticket{
		Slug: "not-on-the-board", Title: "Untracked", Type: store.TypeTicket,
		State: store.StateActive, Section: store.SectionNow, Repo: "ai-devboard",
	}); err != nil {
		t.Fatal(err)
	}
	snap, err := storeSnapshot(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range snap.Tasks {
		if task.Slug == "not-on-the-board" {
			t.Fatal("a ticket that is not board-tracked reached the payload")
		}
	}
}
