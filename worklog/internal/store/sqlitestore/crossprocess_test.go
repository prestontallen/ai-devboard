package sqlitestore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/projection"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// loadTestSoakEnv gates TestCrossProcessReadWriteLoad. Unlike
// TestCrossProcessWriteLoad's testing.Short() skip — a no-op in this
// repo's CI, since ci.yml never passes -short — this is a genuinely
// opt-in gate: the soak below runs for minutes, not seconds, and would
// slow every push if it ran unconditionally. Deliberately not named
// under the WORKLOG_SNAPSHOT/DEVBOARD_SNAPSHOT convention used elsewhere
// in this tree (adopt, hazard, census, projection/live_snapshot_test.go)
// — those gate personal-data-corpus tests for a privacy reason; this
// gates a load test for a duration reason, and reusing that namespace
// would make an engineer who already exports WORKLOG_SNAPSHOT for corpus
// work unknowingly also trigger this soak.
const loadTestSoakEnv = "WORKLOG_LOADTEST_SOAK"

const (
	// soakDuration is wall-clock, not an iteration count: RenderSnapshot's
	// cost scales with ticket count and every Open() shares a 5s
	// busy_timeout, so under real contention a reader/writer cycle can
	// stretch — that cadence drift is the thing this test is proving safe,
	// not a bug to normalize away.
	soakDuration = 150 * time.Second
	// soakTimeoutGrace is the hard backstop above soakDuration: subprocesses
	// self-terminate at their own deadline (HELPER_DEADLINE), but
	// exec.CommandContext's ctx is the guarantee — if a subprocess ever
	// hangs (a stuck checkpoint, a lock that never releases), the context
	// deadline kills it rather than leaving it running past the parent
	// test's return, against a t.TempDir() that's about to be removed.
	soakTimeoutGrace = 30 * time.Second
	soakNumWriters   = 8
	// soakReaderInterval matches serve.ConfigFromEnv's default
	// ScanInterval — the cadence the real shadow server actually reads at.
	soakReaderInterval = time.Second
)

// TestCrossProcessWriteLoad shows that modernc's pure-Go WAL locking
// survives a BURST of real concurrent OS-process writers. Open already
// caps database/sql's pool at one connection, so goroutines inside a
// single process can't exercise the cross-process file-locking path this
// test targets — each worker here is a genuine subprocess of this test
// binary, re-exec'd via the standard GO_WANT_HELPER_PROCESS pattern.
//
// What it does NOT cover, despite once being cited as proof of
// adb-cutover M1: sustained load. 8 workers x 15 bare-ticket writes
// finishes in about 140ms, far short of the five-second busy_timeout a
// starved writer has to burn before it fails, and bare tickets are too
// light a transaction to make anyone hold the write lock long enough to
// starve anyone else. It passed on the build that starved 7 of 8 writers.
// TestCrossProcessWriterStarvation in starvation_test.go is the test that
// covers that; this one stays for what it genuinely checks, which is that
// a burst of concurrent processes neither loses nor duplicates writes.
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

// TestCrossProcessReadWriteLoad proves the other half of adb-cutover's
// caveat that TestCrossProcessWriteLoad left untested: a long-lived
// READER mixed with concurrent writers, not writers alone. It targets
// the devboard shadow server's actual shape (cli/serve.go's
// loadStoreSnapshot: open a fresh connection, RenderSnapshot, close —
// every ~1s, for as long as the server runs) rather than a single
// held-open read transaction, which is not how the real server reads.
//
// Skipped unless WORKLOG_LOADTEST_SOAK=1 — see loadTestSoakEnv's doc.
// Run it directly with a generous -timeout, since the soak duration
// alone can approach Go's default 10m per-binary budget once stacked
// against this package's other sequential tests:
//
//	WORKLOG_LOADTEST_SOAK=1 go test ./internal/store/sqlitestore/... \
//	  -run TestCrossProcessReadWriteLoad -timeout=5m -v
func TestCrossProcessReadWriteLoad(t *testing.T) {
	if os.Getenv(loadTestSoakEnv) != "1" {
		t.Skip("cross-process read/write soak skipped unless WORKLOG_LOADTEST_SOAK=1 (opt-in: a multi-minute soak, not part of the default suite)")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "worklog.db")

	seed, err := Open(path)
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("seed close: %v", err)
	}

	logFDLimit(t)

	walPath := path + "-wal"
	// Baseline taken on an idle, freshly-migrated db (seed already closed
	// its connection, so any startup checkpoint has already run) — the
	// bound in assertWALBounded is relative to this, not to a sample
	// taken mid-run (which "early" would have been) or after every
	// subprocess has closed (which auto-checkpoints on last-close and
	// would trivially read near-zero regardless of what happened during
	// the soak — that shape doesn't distinguish "healthy" from "the WAL
	// hit 500MB for a minute and then cleaned up").
	preLoadWAL := statSizeOrZero(walPath)
	sampler := startWALSampler(walPath)

	deadline := time.Now().Add(soakDuration)
	ctx, cancel := context.WithTimeout(context.Background(), soakDuration+soakTimeoutGrace)
	t.Cleanup(cancel)

	var wg sync.WaitGroup
	writerErrs := make([]error, soakNumWriters)
	writerWrote := make([]int, soakNumWriters)
	for i := 0; i < soakNumWriters; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHelperCrossProcessSoakWriter$")
			cmd.Env = append(os.Environ(),
				"GO_WANT_HELPER_PROCESS=1",
				"HELPER_DB_PATH="+path,
				"HELPER_WORKER_ID="+strconv.Itoa(worker),
				"HELPER_DEADLINE="+deadline.Format(time.RFC3339Nano),
			)
			out, err := cmd.Output()
			// Parse the count regardless of err: a writer that fails
			// mid-loop still reports (via its last-gasp print, see
			// TestHelperCrossProcessSoakWriter) how many writes it
			// already committed before the failure — those rows are
			// genuinely in the store, and crediting them 0 would make
			// legitimate partial progress look like lost writes.
			if n, perr := parseHelperCount("WROTE", out); perr == nil {
				writerWrote[worker] = n
			} else if err == nil {
				writerErrs[worker] = fmt.Errorf("writer %d: %w", worker, perr)
			}
			if err != nil {
				writerErrs[worker] = fmt.Errorf("writer %d: %w\n%s", worker, err, helperStderr(err))
			}
		}(i)
	}

	var readErr error
	var readCount int
	wg.Add(1)
	go func() {
		defer wg.Done()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHelperCrossProcessSoakReader$")
		cmd.Env = append(os.Environ(),
			"GO_WANT_HELPER_PROCESS=1",
			"HELPER_DB_PATH="+path,
			"HELPER_DEADLINE="+deadline.Format(time.RFC3339Nano),
		)
		out, err := cmd.Output()
		if n, perr := parseHelperCount("READ", out); perr == nil {
			readCount = n
		} else if err == nil {
			readErr = fmt.Errorf("reader: %w", perr)
		}
		if err != nil {
			readErr = fmt.Errorf("reader: %w\n%s", err, helperStderr(err))
		}
	}()

	start := time.Now()
	wg.Wait()
	elapsed := time.Since(start)
	maxWAL, walSamples := sampler.stopAndMax()

	for _, err := range writerErrs {
		if err != nil {
			t.Error(err)
		}
	}
	if readErr != nil {
		t.Error(readErr)
	}
	t.Logf("cross-process read/write soak: %d writers over %s, reader completed %d cycles, elapsed %s",
		soakNumWriters, soakDuration, readCount, elapsed)

	expectedWrites := 0
	for _, n := range writerWrote {
		expectedWrites += n
	}
	if expectedWrites < soakNumWriters {
		t.Errorf("soak made almost no forward progress: %d total writes across %d workers over %s (system may be deadlocked, not just slow)",
			expectedWrites, soakNumWriters, soakDuration)
	}

	// Every write must have landed exactly once, with the reader
	// concurrently polling the whole time — lost or duplicated writes
	// under contention are the failure mode this test exists to catch.
	verify, err := Open(path)
	if err != nil {
		t.Fatalf("verify open: %v", err)
	}
	defer verify.Close()
	tickets, err := verify.Tickets()
	if err != nil {
		t.Fatalf("verify tickets: %v", err)
	}
	if len(tickets) != expectedWrites {
		t.Errorf("got %d tickets after soak, want %d (writes lost or duplicated under contention)", len(tickets), expectedWrites)
	}
	seen := make(map[string]bool, len(tickets))
	for _, tk := range tickets {
		if seen[tk.Slug] {
			t.Errorf("duplicate slug %q survived concurrent soak", tk.Slug)
		}
		seen[tk.Slug] = true
	}

	t.Logf("wal size: pre-load=%d bytes, max-during-soak=%d bytes (%d samples)", preLoadWAL, maxWAL, walSamples)
	assertWALBounded(t, preLoadWAL, maxWAL)
}

// TestHelperCrossProcessSoakWriter is the soak's writer subprocess body.
// Unlike TestHelperCrossProcessWriter's fixed write count, it writes
// until HELPER_DEADLINE — the soak is bounded by wall-clock time, not by
// how many writes complete, since contention slowing that count is
// exactly what's under test. Silent no-op outside GO_WANT_HELPER_PROCESS,
// same as its sibling.
func TestHelperCrossProcessSoakWriter(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	worker := os.Getenv("HELPER_WORKER_ID")
	n, err := runSoakWriter(os.Getenv("HELPER_DB_PATH"), worker, os.Getenv("HELPER_DEADLINE"))
	// os.Exit skips deferred calls, so the count is printed here, once,
	// before either exit path — never inside the loop below — or a
	// mid-loop failure would report 0 despite rows it already committed
	// still landing in the store (see the parent's parseHelperCount use).
	fmt.Printf("WROTE %d\n", n)
	if err != nil {
		fmt.Fprintf(os.Stderr, "worker %s: %v\n", worker, classifyErr(err))
		os.Exit(1)
	}
	os.Exit(0)
}

// runSoakWriter writes tickets until deadlineStr elapses, returning how
// many committed successfully even when it returns an error — the
// caller's job is to report both, never to conflate "it failed" with
// "it made zero progress."
func runSoakWriter(path, worker, deadlineStr string) (n int, err error) {
	deadline, err := time.Parse(time.RFC3339Nano, deadlineStr)
	if err != nil {
		return 0, fmt.Errorf("bad HELPER_DEADLINE: %w", err)
	}

	s, err := Open(path)
	if err != nil {
		return 0, fmt.Errorf("open: %w", err)
	}
	defer s.Close()

	for time.Now().Before(deadline) {
		tk := &store.Ticket{
			Slug:    fmt.Sprintf("soak-writer-%s-%d", worker, n),
			Title:   "cross-process read/write soak ticket",
			Type:    store.TypeTicket,
			State:   store.StateActive,
			Section: store.SectionNow,
		}
		if err := s.PutTicket(tk); err != nil {
			return n, fmt.Errorf("write %d: %w", n, err)
		}
		n++
	}
	return n, nil
}

// TestHelperCrossProcessSoakReader is the soak's reader subprocess body:
// open, RenderSnapshot, close, sleep, repeat — the exact shape
// cli/serve.go's loadStoreSnapshot uses per /api/tasks request, run as a
// genuine OS subprocess (goroutines share one process's connection pool
// and can't exercise cross-process file locking). Fails fast — any
// error or malformed payload exits non-zero immediately rather than
// continuing, so a problem is attributed to the cycle that caused it.
func TestHelperCrossProcessSoakReader(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	n, err := runSoakReader(os.Getenv("HELPER_DB_PATH"), os.Getenv("HELPER_DEADLINE"))
	fmt.Printf("READ %d\n", n) // printed before either exit path — see runSoakWriter's comment
	if err != nil {
		fmt.Fprintf(os.Stderr, "reader: %v\n", classifyErr(err))
		os.Exit(1)
	}
	os.Exit(0)
}

// runSoakReader cycles open/RenderSnapshot/close until deadlineStr
// elapses, at the cadence serve.ConfigFromEnv's default ScanInterval
// uses — the real shadow server's read pattern, not a single held-open
// connection. Fails fast: the first error or malformed payload returns
// immediately so a failure is attributable to the cycle that caused it,
// not buried among however many more cycles would otherwise follow.
func runSoakReader(path, deadlineStr string) (n int, err error) {
	deadline, err := time.Parse(time.RFC3339Nano, deadlineStr)
	if err != nil {
		return 0, fmt.Errorf("bad HELPER_DEADLINE: %w", err)
	}

	for time.Now().Before(deadline) {
		s, err := Open(path)
		if err != nil {
			return n, fmt.Errorf("open (cycle %d): %w", n, err)
		}
		files, renderErr := projection.Render(s)
		closeErr := s.Close()
		if renderErr != nil {
			return n, fmt.Errorf("render (cycle %d): %w", n, renderErr)
		}
		if closeErr != nil {
			return n, fmt.Errorf("close (cycle %d): %w", n, closeErr)
		}
		if files == nil {
			return n, fmt.Errorf("render (cycle %d): nil Files map from a well-formed call", n)
		}
		n++
		time.Sleep(soakReaderInterval)
	}
	return n, nil
}

// classifyErr prefixes an error so a soak failure is attributable at a
// glance: a low ulimit producing "too many open files" is an environment
// artifact, not evidence of the WAL-locking failure this test exists to
// detect, and the two must not be read as the same finding.
func classifyErr(err error) string {
	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "too many open files"):
		return "FDLIMIT: " + msg
	case strings.Contains(lower, "busy") || strings.Contains(lower, "locked"):
		return "LOCK: " + msg
	default:
		return msg
	}
}

// logFDLimit records the process's open-file limit at soak start so a
// low-ulimit environment (macOS defaults to 256) is visible in the test
// log rather than only inferable after the fact from a FDLIMIT error.
func logFDLimit(t *testing.T) {
	var rlimit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rlimit); err != nil {
		t.Logf("fd limit: could not read RLIMIT_NOFILE: %v", err)
		return
	}
	t.Logf("fd limit: soft=%d hard=%d", rlimit.Cur, rlimit.Max)
}

// parseHelperCount extracts the trailing "<label> <n>" line a helper
// subprocess prints on success, tolerating any incidental output before
// it (there is none today, but this keeps parsing robust rather than
// assuming stdout is exactly one line).
func parseHelperCount(label string, out []byte) (int, error) {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	last := lines[len(lines)-1]
	var n int
	if _, err := fmt.Sscanf(last, label+" %d", &n); err != nil {
		return 0, fmt.Errorf("could not parse %q from helper output %q: %w", label, string(out), err)
	}
	return n, nil
}

// helperStderr recovers a failed helper subprocess's stderr — including
// any classifyErr-prefixed message — from the *exec.ExitError that
// cmd.Output() (unlike CombinedOutput) leaves it in rather than mixing
// it into stdout, which would have broken parseHelperCount's assumption
// that stdout carries only the trailing count line.
func helperStderr(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return string(ee.Stderr)
	}
	return ""
}

func statSizeOrZero(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// walSampler tracks the -wal sidecar's peak size across the soak.
// Peak, not a single end-of-run sample: every subprocess closing its
// connection triggers SQLite's own last-close checkpoint, which would
// make a sample taken after wg.Wait() returns read near-zero regardless
// of whether the WAL ballooned for the whole run and only just got
// cleaned up — that shape can't distinguish "healthy" from "stalled for
// two minutes, then recovered."
type walSampler struct {
	mu      sync.Mutex
	max     int64
	samples int
	stop    chan struct{}
	done    chan struct{}
}

// startWALSampler begins polling walPath's size every 3s in the
// background. Callers must call stopAndMax before reading its result —
// there is no other synchronization, so reading max/samples directly
// while the goroutine might still be running would race it.
func startWALSampler(walPath string) *walSampler {
	s := &walSampler{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-ticker.C:
				size := statSizeOrZero(walPath)
				s.mu.Lock()
				if size > s.max {
					s.max = size
				}
				s.samples++
				s.mu.Unlock()
			}
		}
	}()
	return s
}

// stopAndMax stops the background poll, blocks until it has actually
// exited, and only then reads the result — the <-s.done receive is what
// makes the following field reads race-free, not the mutex alone.
func (s *walSampler) stopAndMax() (max int64, samples int) {
	close(s.stop)
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.max, s.samples
}

// assertWALBounded checks that the WAL sidecar didn't grow unboundedly
// at any point during the soak — the failure mode gitlab.com/cznic/sqlite
// issue #179 reports (a 4.2GB -wal file from stalled checkpointing).
// It compares the observed peak against a pre-load baseline (an idle,
// freshly-migrated db, no subprocess has touched it yet), with a
// generous multiplier so ordinary fluctuation under load doesn't read
// as a false failure — issue #179's growth was orders of magnitude
// beyond any legitimate variance this bound allows.
func assertWALBounded(t *testing.T, preLoad, peak int64) {
	const minBound = 2 << 20 // 2MB floor so a near-zero baseline can't make any growth "unbounded"
	bound := preLoad * 10
	if bound < minBound {
		bound = minBound
	}
	if peak > bound {
		t.Errorf("wal sidecar grew unboundedly: pre-load=%d bytes, peak=%d bytes, exceeds bound=%d bytes (checkpointing likely stalled under reader churn)",
			preLoad, peak, bound)
	}
}
