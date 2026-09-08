package lockfile

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func lockPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "gate.lock")
}

func TestAcquireWithinUncontended(t *testing.T) {
	release, err := AcquireWithin(lockPath(t), time.Second)
	if err != nil {
		t.Fatalf("uncontended acquire: %v", err)
	}
	release()
}

// TestAcquireWithinTimesOut is the property a CLI write path depends on:
// the wait ends, and it ends with an error that says which lock was not
// granted rather than a bare "database is locked" from SQLite.
func TestAcquireWithinTimesOut(t *testing.T) {
	path := lockPath(t)

	holder, err := Acquire(path)
	if err != nil {
		t.Fatalf("holder acquire: %v", err)
	}
	defer holder()

	start := time.Now()
	release, err := AcquireWithin(path, 150*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		release()
		t.Fatal("acquired a lock another descriptor is holding")
	}
	if !IsTimeout(err) {
		t.Fatalf("got %v, want a timeout error", err)
	}
	if elapsed < 150*time.Millisecond {
		t.Errorf("returned after %s, want at least the full 150ms wait", elapsed)
	}
	// The cross-process test's classifyErr buckets on "busy" or "locked";
	// a timeout using neither word is filed as an unattributed failure.
	if !strings.Contains(strings.ToLower(err.Error()), "locked") {
		t.Errorf("error %q does not contain %q", err.Error(), "locked")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name the lock path", err.Error())
	}
}

// TestAbandonedAcquisitionIsReleased covers the failure mode the
// deadline design could easily have introduced. The wait happens on a
// goroutine that cannot be cancelled, so when the caller gives up, that
// goroutine is still queued in the kernel. If it simply kept the lock
// once granted, one timeout would wedge the gate permanently for every
// later writer.
func TestAbandonedAcquisitionIsReleased(t *testing.T) {
	path := lockPath(t)

	holder, err := Acquire(path)
	if err != nil {
		t.Fatalf("holder acquire: %v", err)
	}

	if _, err := AcquireWithin(path, 100*time.Millisecond); !IsTimeout(err) {
		holder()
		t.Fatalf("got %v, want a timeout error", err)
	}

	// Hand the lock to the abandoned waiter. It must take it and let go.
	holder()

	release, err := AcquireWithin(path, 3*time.Second)
	if err != nil {
		t.Fatalf("lock never came back after an abandoned wait: %v", err)
	}
	release()
}
