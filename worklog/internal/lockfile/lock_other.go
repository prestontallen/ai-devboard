//go:build !unix

package lockfile

import "time"

// Non-unix fallback: no advisory locking. Atomic rename still prevents torn
// files; concurrent read-modify-write may lose an update. Windows users are
// directed to WSL (see repo install docs).
func Acquire(string) (func(), error) { return func() {}, nil }

// AcquireWithin is the deadline-bounded form, and off unix it is the same
// no-op as Acquire: it reports success immediately and never times out.
// Every release target is unix, so this path ships to nobody; it exists so
// `GOOS=windows go build ./...` keeps working.
func AcquireWithin(string, time.Duration) (func(), error) { return func() {}, nil }
