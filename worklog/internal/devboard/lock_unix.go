//go:build unix

package devboard

import (
	"os"
	"syscall"
)

// lock takes an exclusive flock on lockPath, blocking until acquired. The
// lock file is deliberately never unlinked: removing it while another
// process waits on the same path lets a third process lock a fresh inode
// and defeats mutual exclusion.
func lock(lockPath string) (func(), error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
