package lockfile

import (
	"errors"
	"fmt"
	"time"
)

// TimeoutError is what AcquireWithin returns when the wait elapses before
// the lock is granted. It is a distinct, named error on purpose: the
// caller that gives up needs to say which lock it gave up on, and a bare
// "database is locked" from SQLite would send a reader looking in the
// wrong place.
//
// The message deliberately contains the word "locked" — the cross-process
// test's classifyErr buckets failures on "busy" or "locked", and a
// timeout that used neither word would be filed as an unattributed
// failure rather than as lock contention.
type TimeoutError struct {
	Path string
	Wait time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("gave up after %s: %s is locked by another process", e.Wait, e.Path)
}

// IsTimeout reports whether err is a lock-wait timeout.
func IsTimeout(err error) bool {
	var te *TimeoutError
	return errors.As(err, &te)
}
