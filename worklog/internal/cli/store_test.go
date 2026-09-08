package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/lockfile"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/sqlitestore"
	"github.com/prestontallen/ai-devboard/worklog/internal/storepath"
)

// oldStore builds a database at a pre-move location with one ticket in it,
// closed cleanly so no sidecars remain.
func oldStore(t *testing.T) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "old", "worklog.db")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := sqlitestore.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutTicket(&store.Ticket{
		Slug: "moved", Title: "Came across", Type: store.TypeTicket,
		State: store.StateActive, Section: store.SectionNow,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return src
}

func TestRelocateCopiesAndVerifies(t *testing.T) {
	src := oldStore(t)
	corpus := t.TempDir()
	t.Setenv("WORKLOG_DIR", corpus)

	stdout, stderr := runCLI(t, "store", "relocate", "--from", src, "--dir", corpus)
	if strings.Contains(stderr, "error") {
		t.Fatalf("relocate: %s", stderr)
	}

	dst := storepath.DB(corpus)
	s, err := sqlitestore.Open(dst)
	if err != nil {
		t.Fatalf("relocated database does not open: %v", err)
	}
	defer s.Close()
	if _, err := s.TicketBySlug("moved"); err != nil {
		t.Errorf("relocated database lost its ticket: %v", err)
	}

	// The original is a safety net; it must survive.
	if _, err := os.Stat(src); err != nil {
		t.Errorf("relocate removed the original at %s: %v", src, err)
	}
	if !strings.Contains(stdout, "untouched") {
		t.Errorf("stdout should say the original is untouched, got %q", stdout)
	}
}

// TestRelocateRefusesWithAPendingWAL is the criterion that stops relocate
// from being a fancy `mv`. A -wal beside the source holds committed
// transactions the .db file does not, so copying the .db alone loses them
// silently — no error, just missing history.
func TestRelocateRefusesWithAPendingWAL(t *testing.T) {
	src := oldStore(t)
	corpus := t.TempDir()
	t.Setenv("WORKLOG_DIR", corpus)

	if err := os.WriteFile(src+"-wal", []byte("pending frames"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := runCLIAllowErr(t, "store", "relocate", "--from", src, "--dir", corpus)
	if err == nil {
		t.Fatal("relocate copied a database with a pending write-ahead log")
	}
	if all := stderr + err.Error(); !strings.Contains(all, "-wal") {
		t.Errorf("refusal should name the sidecar, got %q", all)
	}
	if _, statErr := os.Stat(storepath.DB(corpus)); !os.IsNotExist(statErr) {
		t.Error("relocate created a destination database despite refusing")
	}
}

// TestRelocateRefusesWhileAnotherProcessWrites: copying underneath a live
// writer captures a torn database that opens fine and is wrong.
func TestRelocateRefusesWhileAnotherProcessWrites(t *testing.T) {
	src := oldStore(t)
	corpus := t.TempDir()
	t.Setenv("WORKLOG_DIR", corpus)

	release, err := lockfile.Acquire(src + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	// Shorten the wait: this test is about the refusal, not the duration.
	prev := relocateGateWait
	relocateGateWait = 50 * time.Millisecond
	defer func() { relocateGateWait = prev }()

	_, stderr, runErr := runCLIAllowErr(t, "store", "relocate", "--from", src, "--dir", corpus)
	if runErr == nil {
		t.Fatal("relocate copied while another process held the write gate")
	}
	if all := stderr + runErr.Error(); !strings.Contains(all, "another process") {
		t.Errorf("refusal should say another process is writing, got %q", all)
	}
}

func TestRelocateRefusesWhenTheDestinationExists(t *testing.T) {
	src := oldStore(t)
	corpus := t.TempDir()
	t.Setenv("WORKLOG_DIR", corpus)

	dst := storepath.DB(corpus)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("already here"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := runCLIAllowErr(t, "store", "relocate", "--from", src, "--dir", corpus)
	if err == nil {
		t.Fatal("relocate overwrote an existing database")
	}
	if all := stderr + err.Error(); !strings.Contains(all, "already") {
		t.Errorf("refusal should say there is already a database, got %q", all)
	}
}
