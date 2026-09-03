package cli

import (
	"strings"
	"testing"
)

// TestStoreArchiveMoveSyncsBoardArchived covers the dashboard server's
// MutateBoard hook: after the server has already renamed a task file into
// _archive/, storeArchiveMove must flip the store's BoardArchived field to
// match, or the next store-backed write would find the moved file
// disagreeing with what the store renders (adb-cutover gap: the server's
// archive/unarchive endpoint was never ported alongside the CLI verbs).
func TestStoreArchiveMoveSyncsBoardArchived(t *testing.T) {
	live, _, _ := storeWriteFixture(t)
	wd := mustWorkdirForTest(t, live)

	if err := storeArchiveMove(wd, "solo", true); err != nil {
		t.Fatal(err)
	}

	ss, err := openStoreForWrite(wd)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.close()
	tk, err := ss.s.TicketBySlug("solo")
	if err != nil {
		t.Fatal(err)
	}
	if !tk.BoardArchived {
		t.Error("BoardArchived not set after storeArchiveMove(archived=true)")
	}

	if err := storeArchiveMove(wd, "solo", false); err != nil {
		t.Fatal(err)
	}
	tk2, err := ss.s.TicketBySlug("solo")
	if err != nil {
		t.Fatal(err)
	}
	if tk2.BoardArchived {
		t.Error("BoardArchived still set after storeArchiveMove(archived=false)")
	}
}

// TestStoreArchiveMoveRefusesUnknownTicket proves the error path is a
// clean refusal, not a panic or silent no-op.
func TestStoreArchiveMoveRefusesUnknownTicket(t *testing.T) {
	live, _, _ := storeWriteFixture(t)
	wd := mustWorkdirForTest(t, live)

	err := storeArchiveMove(wd, "does-not-exist", true)
	if err == nil {
		t.Fatal("expected a refusal for an unknown ticket")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("error should name the id: %v", err)
	}
}
