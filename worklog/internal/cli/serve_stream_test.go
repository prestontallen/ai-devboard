package cli

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/serve"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/sqlitestore"
	"github.com/prestontallen/ai-devboard/worklog/internal/storepath"
)

// The change stream's signal, end to end against a real database. The
// watcher's own loop is serve's; what is proved here is that the
// fingerprint moves when the board changes and holds still when it does
// not, which is the half a stub cannot answer for.

// streamFixture points the loader at a fresh store and returns a
// fingerprint plus a writer that mutates the same database the way a CLI
// verb in another process would: open, write, close.
func streamFixture(t *testing.T) (func() (string, error), func(func(*store.Ticket))) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DEVBOARD_WORKLOG", dir)
	t.Setenv("WORKLOG_DIR", dir)
	path := storepath.DB(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(mutate func(*store.Ticket)) {
		t.Helper()
		s, err := sqlitestore.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		tk, err := s.TicketBySlug("card")
		if err != nil {
			tk = &store.Ticket{Slug: "card", Title: "Card", Type: store.TypeTicket,
				State: store.StateActive, Section: store.SectionNow,
				Repo: "r", BoardTracked: true, Phase: "intake"}
		}
		mutate(tk)
		if err := s.PutTicket(tk); err != nil {
			t.Fatal(err)
		}
	}
	write(func(*store.Ticket) {})
	return newStoreFingerprint(), write
}

// A field the board draws changes the fingerprint.
func TestFingerprintMovesOnARealChange(t *testing.T) {
	fp, write := streamFixture(t)
	before, err := fp()
	if err != nil {
		t.Fatal(err)
	}
	write(func(tk *store.Ticket) { tk.Phase = "verify" })
	after, err := fp()
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Error("a phase change did not move the fingerprint")
	}
}

// A write that sets a field to what it already held changes nothing the
// board draws, so it must not fire. The file watcher got this for free:
// an identical render was skipped and the mtime never moved. It is the
// obligation that travelled through three tickets without ever landing as
// a criterion, so it is pinned here.
func TestFingerprintHoldsStillOnANoOpWrite(t *testing.T) {
	fp, write := streamFixture(t)
	before, err := fp()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		write(func(tk *store.Ticket) { tk.Phase = "intake" }) // already intake
		after, err := fp()
		if err != nil {
			t.Fatal(err)
		}
		if after != before {
			t.Fatalf("no-op write %d moved the fingerprint: %s -> %s", i+1, before[:12], after[:12])
		}
	}
}

// Repeated calls with nothing happening in between are stable, which is
// what stops the board refetching every second.
func TestFingerprintIsStableWhileNothingHappens(t *testing.T) {
	fp, _ := streamFixture(t)
	first, err := fp()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		again, err := fp()
		if err != nil {
			t.Fatal(err)
		}
		if again != first {
			t.Fatalf("call %d moved the fingerprint with no write in between", i+2)
		}
	}
}

// Notes and feedback are store-backed, so appending either is a board
// change. Under the file watcher these were separate watched paths; now
// they fall out of the same signal.
func TestFingerprintMovesOnNotesAndFeedback(t *testing.T) {
	fp, write := streamFixture(t)
	before, err := fp()
	if err != nil {
		t.Fatal(err)
	}
	write(func(tk *store.Ticket) {
		tk.NoteEntries = append(tk.NoteEntries, store.NoteEntry{Stamp: "2026-09-09 10:00", Body: "a note"})
	})
	withNote, err := fp()
	if err != nil {
		t.Fatal(err)
	}
	if withNote == before {
		t.Error("a note append did not move the fingerprint")
	}

	s, err := sqlitestore.Open(storepath.DB(os.Getenv("WORKLOG_DIR")))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutFeedback(&store.FeedbackEntry{
		Seconds: 1756800000, Signal: "missing-feature", Trigger: "wanted a verb"}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	withFeedback, err := fp()
	if err != nil {
		t.Fatal(err)
	}
	if withFeedback == withNote {
		t.Error("a feedback entry did not move the fingerprint")
	}
}

// The same two obligations the loader carries apply on the watcher's path,
// because it is the same loader: never create the database, and leave no
// handle open. A watcher polling every second is the likeliest thing to
// break either.
func TestFingerprintOnAStorelessMachineCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DEVBOARD_WORKLOG", dir)
	t.Setenv("WORKLOG_DIR", dir)

	fp := newStoreFingerprint()
	for i := 0; i < 3; i++ {
		if _, err := fp(); err != nil {
			t.Fatalf("store-less machine: want a silent answer, got %v", err)
		}
	}
	if _, err := os.Stat(storepath.DB(dir)); !os.IsNotExist(err) {
		t.Fatalf("polling created a database at %s", storepath.DB(dir))
	}
}

func TestFingerprintLeavesNoOpenHandle(t *testing.T) {
	fp, _ := streamFixture(t)
	if _, err := fp(); err != nil {
		t.Fatal(err)
	}
	path := storepath.DB(os.Getenv("WORKLOG_DIR"))
	for _, sidecar := range []string{path + "-wal", path + "-shm"} {
		if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
			t.Errorf("%s outlives the poll — a held handle stops the WAL checkpointing, and store relocate then refuses forever", sidecar)
		}
	}
}

// The gate is sampled before the read and cached as such. A write that
// lands while the snapshot is being read is not in that snapshot, but it
// has already moved the files — so the next poll sees a gate that differs
// and reads again. Caching the post-read gate instead would pin a stale
// hash to a current gate, and the board would stop updating until
// something else happened to write.
//
// The window is a few milliseconds wide in practice, so racing for it
// proves nothing. The read is injected and the write is placed inside it.
func TestFingerprintDoesNotCacheAwayAConcurrentWrite(t *testing.T) {
	_, write := streamFixture(t)

	var wroteDuringRead bool
	fp := newStoreFingerprintWith(func() (*serve.StoreSnapshot, error) {
		snap, err := loadStoreTickets()
		if err != nil {
			return nil, err
		}
		if !wroteDuringRead {
			// Lands after this snapshot was taken but before the caller
			// caches anything: exactly the window.
			wroteDuringRead = true
			write(func(tk *store.Ticket) { tk.Title = "written mid-read" })
		}
		return snap, nil
	})

	if _, err := fp(); err != nil {
		t.Fatal(err)
	}
	got, err := fp()
	if err != nil {
		t.Fatal(err)
	}
	want, err := newStoreFingerprint()()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("a write made during the read was cached away:\n polled %s\n fresh  %s", got, want)
	}
}

// End to end, which is what the criterion asks for: a connected SSE
// client, a real store, no devboard directory anywhere, and a CLI-shaped
// write in another connection. The event has to arrive on its own.
func TestSSEClientSeesACLIWrite(t *testing.T) {
	fp, write := streamFixture(t)

	srv := serve.New(serve.Config{
		WorklogDir:   os.Getenv("WORKLOG_DIR"),
		ScanInterval: 20 * time.Millisecond,
	})
	srv.StoreFingerprint = fp
	srv.LoadStoreSnapshot = loadStoreTickets
	stop := make(chan struct{})
	defer close(stop)
	go srv.Watch(stop)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	events := bufio.NewReader(resp.Body)

	next := func(within time.Duration) (string, bool) {
		t.Helper()
		type line struct {
			s   string
			err error
		}
		ch := make(chan line, 1)
		go func() {
			for {
				l, err := events.ReadString('\n')
				if err != nil {
					ch <- line{err: err}
					return
				}
				if strings.HasPrefix(l, "data:") {
					ch <- line{s: strings.TrimSpace(strings.TrimPrefix(l, "data:"))}
					return
				}
			}
		}()
		select {
		case l := <-ch:
			return l.s, l.err == nil
		case <-time.After(within):
			return "", false
		}
	}

	if _, ok := next(2 * time.Second); !ok {
		t.Fatal("no event on connect")
	}

	// A real change reaches the client within a couple of scan intervals.
	write(func(tk *store.Ticket) { tk.Phase = "verify" })
	if _, ok := next(2 * time.Second); !ok {
		t.Fatal("a phase change never reached the SSE client")
	}

	// And a write that changes nothing stays silent. Generous window: the
	// claim is that no event arrives, so waiting longer only strengthens it.
	write(func(tk *store.Ticket) { tk.Phase = "verify" })
	if got, ok := next(500 * time.Millisecond); ok {
		t.Errorf("a no-op write produced an event: %s", got)
	}
}
