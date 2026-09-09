package cli

import (
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/storepath"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
)

// TestStoreArchiveMoveSyncsBoardArchived covers the dashboard server's
// MutateBoard hook. Since adb-archive-store-desync the flag is not a record
// of a move the server already made — it IS the move: BoardArchived decides
// which path the board task renders to, and the re-render places the file.
func TestStoreArchiveMoveSyncsBoardArchived(t *testing.T) {
	live, _, _ := storeWriteFixture(t)
	wd := mustWorkdirForTest(t, live)

	// The flag IS the move. It used to be observable as a rendered file
	// changing directory; with the board projection retired the flag is
	// the whole of it (adb-retire-devboard-dir-2).
	owned, err := storeArchiveMove(wd, "an-epic", true)
	if err != nil {
		t.Fatal(err)
	}
	if !owned {
		t.Fatal("the store board-tracks an-epic, so it must own the move")
	}
	if !archivedInStore(t, live, "an-epic") {
		t.Error("the archive move did not set BoardArchived")
	}

	if owned, err = storeArchiveMove(wd, "an-epic", false); err != nil || !owned {
		t.Fatalf("unarchive: owned=%v err=%v", owned, err)
	}
	if archivedInStore(t, live, "an-epic") {
		t.Error("the unarchive move did not clear BoardArchived")
	}
}

// archivedInStore reads the flag the move sets.
func archivedInStore(t *testing.T, dir, slug string) bool {
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
		t.Fatal(err)
	}
	return tk.BoardArchived
}

// TestStoreArchiveMoveDisownsWhatItCannotPlace: this used to be an error,
// on the reasoning that an id with no ticket was a caller mistake. It is
// not — a hand-dropped producer file is supported input with no ticket
// behind it, and the board can archive one. So the store reports that it
// does not own the move and the handler renames the file instead
// (adb-archive-store-desync, Decision 1).
//
// The second case is the one that would have been missed by reading: a
// ticket that resolves but is not board-tracked. Setting a field on it
// would look like success while the projection never writes that file, so
// nothing would move and the endpoint would report a move that never
// happened.
func TestStoreArchiveMoveDisownsWhatItCannotPlace(t *testing.T) {
	live, _, _ := storeWriteFixture(t)
	wd := mustWorkdirForTest(t, live)

	owned, err := storeArchiveMove(wd, "does-not-exist", true)
	if err != nil {
		t.Fatalf("an unknown id is a hand-off, not an error: %v", err)
	}
	if owned {
		t.Error("claimed ownership of an id with no ticket")
	}

	// Now a real ticket the board does not track.
	ss, err := openStoreForWrite(wd)
	if err != nil {
		t.Fatal(err)
	}
	tk, err := ss.s.TicketBySlug("solo")
	if err != nil {
		t.Fatal(err)
	}
	tk.BoardTracked = false
	if err := ss.commit(tk); err != nil {
		t.Fatal(err)
	}
	ss.close()

	if owned, err = storeArchiveMove(wd, "solo", true); err != nil {
		t.Fatalf("untracked ticket: %v", err)
	}
	if owned {
		t.Error("claimed ownership of a ticket the projection never writes")
	}
}
