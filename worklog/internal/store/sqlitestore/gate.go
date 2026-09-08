package sqlitestore

import (
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/lockfile"
)

// writeGateWait is how long a writer waits for its turn before giving up.
//
// Generous on purpose. Under the gate only one process is inside SQLite's
// write path at a time, so the wait is other writers' work, not lock
// thrash, and every wait ends in a grant rather than a retry. Ten seconds
// is long enough that an interactive verb queued behind a couple of other
// writers still succeeds, and short enough that a wedged holder surfaces
// as a named error instead of a hang.
const writeGateWait = 10 * time.Second

// gateSuffix names the sidecar. It must end in ".lock" so census already
// classifies it transient, and it must be a path distinct from the
// database and its -wal/-shm sidecars: modernc's -shm coordination locks
// are plain POSIX record locks, owned by (process, inode), and any close()
// of any other descriptor on that same inode inside this process would
// silently drop every lock the process holds on it. A separate file in a
// separate lock space (flock, not fcntl) cannot collide with them.
const gateSuffix = ".lock"

// gate serializes writers. It is what actually fixes the starvation this
// package used to exhibit — see Open's concurrency note.
//
// Two layers, always taken in this order. The in-process mutex comes first
// because flock is held per OPEN FILE DESCRIPTION, not per process: two
// goroutines in one process each opening their own descriptor would both
// be granted the "exclusive" lock, and the server does run write handlers
// concurrently. The file lock then excludes other processes.
//
// Callers must not take a lockfile lock while holding the gate. The one
// nested-lock site today is the server's archive handler, which takes a
// per-file lock and then enters a gated write; file-lock-outer,
// gate-inner is the permitted order, and there is no cycle as long as
// nothing under the gate reaches back for a file lock.
func (s *SQLite) gate() (func(), error) {
	s.mu.Lock()
	release, err := lockfile.AcquireWithin(s.path+gateSuffix, writeGateWait)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	return func() {
		release()
		s.mu.Unlock()
	}, nil
}
