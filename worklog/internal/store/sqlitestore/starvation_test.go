package sqlitestore

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/lockfile"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// starvationDuration must exceed the DSN's busy_timeout(5000). That is
// what makes this test discriminating rather than decorative: without
// the gate, the losing writers block for the full five seconds and only
// then fail, so a run shorter than that would pass on a broken build.
const starvationDuration = 8 * time.Second

const starvationWriters = 8

// heavyTicket builds a ticket whose PutTicket costs roughly two hundred
// statements, because transaction WEIGHT is what decides whether this bug
// shows up at all. Measured on the diagnosis: with a one-statement
// transaction the losing writers still commit tens of thousands of rows
// before one of them times out, so a test writing bare tickets — which is
// what TestCrossProcessWriteLoad does — reports success on a build that
// starves. At this weight, 7 of 8 writers fail on their FIRST transaction.
func heavyTicket(slug string) *store.Ticket {
	t := &store.Ticket{
		Slug:    slug,
		Title:   "starvation regression ticket",
		Type:    store.TypeTicket,
		State:   store.StateActive,
		Section: store.SectionNow,
	}
	const each = 20
	for i := 0; i < each; i++ {
		n := strconv.Itoa(i)
		t.PlanSteps = append(t.PlanSteps, store.PlanStep{Rank: i, Text: "step " + n, State: "pending"})
		t.Scorecard = append(t.Scorecard, store.ScoreItem{Rank: i, Text: "criterion " + n, Verify: "go test", Status: "pending"})
		t.Decisions = append(t.Decisions, store.Decision{Rank: i, What: "what " + n, Why: "why " + n, When: "2026-09-08"})
		t.CodeRefs = append(t.CodeRefs, store.CodeRef{Rank: i, File: "f" + n + ".go", Lines: "1-2", Lang: "go", Note: "note " + n})
		t.NeedsYou = append(t.NeedsYou, store.NeedsItem{Rank: i, Type: "question", Text: "q " + n})
		t.WaitingOn = append(t.WaitingOn, store.WaitingItem{Rank: i, Text: "w " + n, Who: "someone", Asked: "2026-09-08"})
		t.Links = append(t.Links, store.Link{Rank: i, Kind: store.LinkRef, Label: "ref " + n, URL: "https://example.invalid/" + n})
		t.Transitions = append(t.Transitions, store.PhaseTransition{Rank: i, From: "intake", To: "clarify", At: "2026-09-08T00:00:00Z"})
		t.NoteEntries = append(t.NoteEntries, store.NoteEntry{Rank: i, Stamp: "2026-09-08 00:00", Body: "body " + n})
	}
	return t
}

// TestCrossProcessWriterStarvation is the regression test for
// adb-sqlite-writer-starvation. Eight real OS processes write heavy
// transactions against one database for longer than busy_timeout.
//
// The assertion is a FAIRNESS INVARIANT, not a throughput or wall-clock
// threshold: every writer commits at least once, and nobody is refused.
// That phrasing is deliberate. The bug was found on an 8-core machine and
// CI runners have fewer, so any count- or duration-based bound would be
// measuring the runner rather than the property. "Nobody starves" holds
// on any machine. Distribution is expected to be uneven, since flock is
// not strictly FIFO — uneven is fine, zero is not.
func TestCrossProcessWriterStarvation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "worklog.db")

	seed, err := Open(path)
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("seed close: %v", err)
	}

	deadline := time.Now().Add(starvationDuration)

	var wg sync.WaitGroup
	errs := make([]error, starvationWriters)
	wrote := make([]int, starvationWriters)
	for i := 0; i < starvationWriters; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestHelperStarvationWriter$")
			cmd.Env = append(os.Environ(),
				"GO_WANT_HELPER_PROCESS=1",
				"HELPER_DB_PATH="+path,
				"HELPER_WORKER_ID="+strconv.Itoa(worker),
				"HELPER_DEADLINE="+deadline.Format(time.RFC3339Nano),
			)
			out, err := cmd.Output()
			// Parse regardless of err: a writer that failed partway still
			// reports what it committed first, and crediting it zero would
			// misreport partial progress as starvation.
			if n, perr := parseHelperCount("WROTE", out); perr == nil {
				wrote[worker] = n
			}
			if err != nil {
				errs[worker] = fmt.Errorf("writer %d: %s\n%s", worker, classifyErr(err), helperStderr(err))
			}
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	for worker, n := range wrote {
		if n == 0 {
			t.Errorf("writer %d committed nothing in %s — starved", worker, starvationDuration)
		}
	}
	t.Logf("writes per worker over %s: %v", starvationDuration, wrote)
}

// TestHelperStarvationWriter is the subprocess body, not a real test.
func TestHelperStarvationWriter(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	worker := os.Getenv("HELPER_WORKER_ID")
	deadline, err := time.Parse(time.RFC3339Nano, os.Getenv("HELPER_DEADLINE"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "bad HELPER_DEADLINE: %v\n", err)
		os.Exit(1)
	}

	s, err := Open(os.Getenv("HELPER_DB_PATH"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "worker %s open: %v\n", worker, err)
		os.Exit(1)
	}
	defer s.Close()

	n := 0
	for time.Now().Before(deadline) {
		if err := s.PutTicket(heavyTicket(fmt.Sprintf("starve-%s-%d", worker, n))); err != nil {
			fmt.Printf("WROTE %d\n", n)
			fmt.Fprintf(os.Stderr, "worker %s write %d: %v\n", worker, n, err)
			os.Exit(1)
		}
		n++
	}
	fmt.Printf("WROTE %d\n", n)
	os.Exit(0)
}

// TestReadsDoNotWaitOnTheWriteGate is the criterion that keeps the
// dashboard responsive. The server opens the store on every /api/tasks
// request at a one-second cadence, and Open calls migrate, so gating
// migrate unconditionally would put that read path behind the write gate.
func TestReadsDoNotWaitOnTheWriteGate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worklog.db")
	seed, err := Open(path)
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	if err := seed.PutTicket(&store.Ticket{Slug: "read-me", Title: "t", Type: store.TypeTicket, State: store.StateActive, Section: store.SectionNow}); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	seed.Close()

	// Somebody else is mid-write and holding the gate for a long time.
	holder, err := lockfile.Acquire(path + gateSuffix)
	if err != nil {
		t.Fatalf("hold gate: %v", err)
	}
	defer holder()

	done := make(chan error, 1)
	go func() {
		r, err := Open(path)
		if err != nil {
			done <- err
			return
		}
		defer r.Close()
		_, err = r.Tickets()
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("read while the gate was held: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a read blocked on the write gate — Open must not gate when no migration is pending")
	}
}

// TestConcurrentOpenMigratesOnce covers the double-checked read inside
// migrate. Two openers of an unmigrated database race; exactly one
// applies the migrations and neither may error on the other's DDL.
func TestConcurrentOpenMigratesOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worklog.db")

	const openers = 6
	var wg sync.WaitGroup
	errs := make([]error, openers)
	for i := 0; i < openers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			s, err := Open(path)
			if err != nil {
				errs[n] = err
				return
			}
			errs[n] = s.Close()
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("opener %d: %v", i, err)
		}
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("verify open: %v", err)
	}
	defer s.Close()
	v, err := s.userVersion()
	if err != nil {
		t.Fatalf("user_version: %v", err)
	}
	if v != len(migrations) {
		t.Errorf("user_version = %d, want %d", v, len(migrations))
	}
}

// TestSameProcessWritersAreSerialized is why the gate has an in-process
// mutex as well as a file lock. flock is held per OPEN FILE DESCRIPTION,
// so two goroutines sharing one store would each be granted the
// "exclusive" lock and neither would exclude the other. The server does
// run write handlers concurrently, so this is a live shape.
func TestSameProcessWritersAreSerialized(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worklog.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	const writers, each = 4, 15
	var wg sync.WaitGroup
	errs := make(chan error, writers*each)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				if err := s.PutTicket(heavyTicket(fmt.Sprintf("same-%d-%d", w, i))); err != nil {
					errs <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent same-process write: %v", err)
	}

	tickets, err := s.Tickets()
	if err != nil {
		t.Fatalf("tickets: %v", err)
	}
	if len(tickets) != writers*each {
		t.Errorf("got %d tickets, want %d — writes lost under same-process contention", len(tickets), writers*each)
	}
}

// TestGateFileIsASeparateInode guards the hazard recorded in
// notes/adb-store-wal-loadtest.md: modernc's -shm coordination locks are
// plain POSIX record locks, owned by (process, inode), and any close() of
// another descriptor on that same inode inside this process silently
// drops every lock the process holds on it. The gate must therefore never
// be a second descriptor on the database or its sidecars. The .lock
// suffix is load-bearing too — census classifies transient files by it.
func TestGateFileIsASeparateInode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worklog.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	if err := s.PutTicket(&store.Ticket{Slug: "gate", Title: "t", Type: store.TypeTicket, State: store.StateActive, Section: store.SectionNow}); err != nil {
		t.Fatalf("write: %v", err)
	}

	gate := path + gateSuffix
	if filepath.Ext(gate) != ".lock" {
		t.Errorf("gate file %q does not end in .lock — census would not classify it transient", gate)
	}

	gateStat, err := os.Stat(gate)
	if err != nil {
		t.Fatalf("gate file was never created: %v", err)
	}
	gateIno := gateStat.Sys().(*syscall.Stat_t).Ino

	for _, other := range []string{path, path + "-wal", path + "-shm"} {
		st, err := os.Stat(other)
		if err != nil {
			continue // -wal/-shm may not exist at rest
		}
		if st.Sys().(*syscall.Stat_t).Ino == gateIno {
			t.Errorf("gate file shares an inode with %s", other)
		}
	}
}
