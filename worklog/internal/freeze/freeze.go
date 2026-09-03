// Package freeze implements the code-enforced write freeze used for the
// worklog store cutover: a sentinel file that outlives the process that
// creates it, so a "minutes-long freeze window" survives past the command
// that starts it. Not an flock (see internal/lockfile) — a flock releases
// the moment its holding process exits, which is exactly wrong here: the
// process that acquires the freeze exits immediately after printing
// confirmation, while the freeze itself must keep blocking every other
// worklog invocation until a separate "release" command lifts it.
package freeze

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const sentinelName = ".freeze"

// ErrAlreadyFrozen is returned by Acquire when a freeze is already held.
var ErrAlreadyFrozen = errors.New("freeze: already frozen")

// Info is the sentinel file's JSON body.
type Info struct {
	PID      int       `json:"pid"`
	Reason   string    `json:"reason"`
	Acquired time.Time `json:"acquired"`
}

func sentinelPath(dir string) string {
	return filepath.Join(dir, sentinelName)
}

// Acquire creates the sentinel file under dir, failing with ErrAlreadyFrozen
// if one already exists. Uses O_EXCL so two concurrent acquire attempts
// cannot both win. The freeze persists after the calling process exits;
// only Release lifts it.
func Acquire(dir, reason string) (Info, error) {
	info := Info{PID: os.Getpid(), Reason: reason, Acquired: time.Now()}
	b, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return Info{}, err
	}
	f, err := os.OpenFile(sentinelPath(dir), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return Info{}, ErrAlreadyFrozen
		}
		return Info{}, err
	}
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		return Info{}, err
	}
	return info, nil
}

// Release removes the sentinel file under dir. Not an error if no freeze is
// held.
func Release(dir string) error {
	err := os.Remove(sentinelPath(dir))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Check reports whether dir is frozen. A sentinel that exists but fails to
// parse still reports frozen (with a zero Info) — a corrupt sentinel must
// never read as permission to write.
func Check(dir string) (frozen bool, info Info, err error) {
	b, err := os.ReadFile(sentinelPath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return false, Info{}, nil
		}
		// Unreadable for some other reason (permissions, I/O error): fail
		// safe as frozen, since we cannot rule out a freeze being held.
		return true, Info{}, err
	}
	if jerr := json.Unmarshal(b, &info); jerr != nil {
		return true, Info{}, nil
	}
	return true, info, nil
}

// RefusalError formats the error a blocked write verb should return,
// naming the freeze so the caller knows why and by whom.
func RefusalError(info Info) error {
	who := "unknown"
	if info.PID != 0 {
		who = fmt.Sprintf("pid %d", info.PID)
	}
	reason := info.Reason
	if reason == "" {
		reason = "no reason given"
	}
	when := "unknown time"
	if !info.Acquired.IsZero() {
		when = info.Acquired.Format(time.RFC3339)
	}
	return fmt.Errorf("worklog is frozen (%s, acquired %s by %s) — run 'worklog freeze release' to lift it", reason, when, who)
}
