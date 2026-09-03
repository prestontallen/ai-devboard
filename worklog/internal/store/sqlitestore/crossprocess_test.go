package sqlitestore

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// TestCrossProcessWriteLoad proves adb-cutover M1: modernc's pure-Go WAL
// locking survives real concurrent OS-process writers. Open already caps
// database/sql's pool at one connection, so goroutines inside a single
// process can't exercise the cross-process file-locking path this test
// targets — each worker here is a genuine subprocess of this test binary,
// re-exec'd via the standard GO_WANT_HELPER_PROCESS pattern.
func TestCrossProcessWriteLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("cross-process load test skipped in -short mode")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "worklog.db")

	// Run migrations once up front so workers race writes, not schema DDL.
	seed, err := Open(path)
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("seed close: %v", err)
	}

	const numWorkers = 8
	const writesPerWorker = 15
	const budget = 45 * time.Second

	start := time.Now()
	var wg sync.WaitGroup
	errs := make([]error, numWorkers)
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestHelperCrossProcessWriter$")
			cmd.Env = append(os.Environ(),
				"GO_WANT_HELPER_PROCESS=1",
				"HELPER_DB_PATH="+path,
				"HELPER_WORKER_ID="+strconv.Itoa(worker),
				"HELPER_WRITE_COUNT="+strconv.Itoa(writesPerWorker),
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				errs[worker] = fmt.Errorf("worker %d: %w\n%s", worker, err, out)
			}
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	for _, err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	t.Logf("cross-process load: %d workers x %d writes in %s", numWorkers, writesPerWorker, elapsed)
	if elapsed > budget {
		t.Errorf("cross-process load took %s, want < %s (possible lock contention or deadlock)", elapsed, budget)
	}

	// Every write must have landed exactly once — lost or duplicated
	// writes under contention are the failure mode this test exists to
	// catch, not just outright SQLITE_BUSY errors.
	verify, err := Open(path)
	if err != nil {
		t.Fatalf("verify open: %v", err)
	}
	defer verify.Close()
	tickets, err := verify.Tickets()
	if err != nil {
		t.Fatalf("verify tickets: %v", err)
	}
	want := numWorkers * writesPerWorker
	if len(tickets) != want {
		t.Errorf("got %d tickets after concurrent load, want %d (writes lost or duplicated under contention)", len(tickets), want)
	}
	seen := make(map[string]bool, len(tickets))
	for _, tk := range tickets {
		if seen[tk.Slug] {
			t.Errorf("duplicate slug %q survived concurrent write", tk.Slug)
		}
		seen[tk.Slug] = true
	}
}

// TestHelperCrossProcessWriter is not a real test: it's the subprocess
// body TestCrossProcessWriteLoad re-execs via os.Args[0]. Run normally
// (go test ./...) it's a silent no-op — it only acts under the
// GO_WANT_HELPER_PROCESS sentinel the parent test sets.
func TestHelperCrossProcessWriter(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	path := os.Getenv("HELPER_DB_PATH")
	worker := os.Getenv("HELPER_WORKER_ID")
	n, err := strconv.Atoi(os.Getenv("HELPER_WRITE_COUNT"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "bad HELPER_WRITE_COUNT: %v\n", err)
		os.Exit(1)
	}

	s, err := Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "worker %s open: %v\n", worker, err)
		os.Exit(1)
	}
	defer s.Close()

	for i := 0; i < n; i++ {
		tk := &store.Ticket{
			Slug:    fmt.Sprintf("loadtest-%s-%d", worker, i),
			Title:   "cross-process load test ticket",
			Type:    store.TypeTicket,
			State:   store.StateActive,
			Section: store.SectionNow,
		}
		if err := s.PutTicket(tk); err != nil {
			fmt.Fprintf(os.Stderr, "worker %s write %d: %v\n", worker, i, err)
			os.Exit(1)
		}
	}
	os.Exit(0)
}
