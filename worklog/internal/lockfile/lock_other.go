//go:build !unix

package lockfile

// Non-unix fallback: no advisory locking. Atomic rename still prevents torn
// files; concurrent read-modify-write may lose an update. Windows users are
// directed to WSL (see repo install docs).
func Acquire(string) (func(), error) { return func() {}, nil }
