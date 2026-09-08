package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStoreArchiveMoveSyncsBoardArchived covers the dashboard server's
// MutateBoard hook. Since adb-archive-store-desync the flag is not a record
// of a move the server already made — it IS the move: BoardArchived decides
// which path the board task renders to, and the re-render places the file.
func TestStoreArchiveMoveSyncsBoardArchived(t *testing.T) {
	live, board, _ := storeWriteFixture(t)
	wd := mustWorkdirForTest(t, live)

	// an-epic rather than solo: the fixture board-tracks it, so the
	// projection actually writes its file and the move is observable. The
	// earlier version of this test flipped the flag on a ticket render never
	// writes, so it proved the field changed and nothing about the file.
	livePath := filepath.Join(board, "ai-devboard", "an-epic.yaml")
	arcPath := filepath.Join(board, "ai-devboard", "_archive", "an-epic.yaml")
	if _, err := os.Stat(livePath); err != nil {
		t.Fatalf("fixture should render an-epic live: %v", err)
	}

	owned, err := storeArchiveMove(wd, "an-epic", true)
	if err != nil {
		t.Fatal(err)
	}
	if !owned {
		t.Fatal("the store board-tracks an-epic, so it must own the move")
	}
	// The flag IS the move: the re-render places the file and clears the
	// path it left.
	if _, err := os.Stat(arcPath); err != nil {
		t.Errorf("archived file not written: %v", err)
	}
	if _, err := os.Stat(livePath); err == nil {
		t.Error("the live file survived; the board would show the task twice")
	}

	ss, err := openStoreForWrite(wd)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.close()
	tk, err := ss.s.TicketBySlug("an-epic")
	if err != nil {
		t.Fatal(err)
	}
	if !tk.BoardArchived {
		t.Error("BoardArchived not set after storeArchiveMove(archived=true)")
	}

	if owned, err := storeArchiveMove(wd, "an-epic", false); err != nil || !owned {
		t.Fatalf("un-archive: owned=%v err=%v", owned, err)
	}
	tk2, err := ss.s.TicketBySlug("an-epic")
	if err != nil {
		t.Fatal(err)
	}
	if tk2.BoardArchived {
		t.Error("BoardArchived still set after storeArchiveMove(archived=false)")
	}
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
