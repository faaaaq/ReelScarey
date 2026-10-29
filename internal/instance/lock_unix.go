//go:build !windows

package instance

import (
	"golang.org/x/sys/unix"
	"os"
)

func lock(f *os.File, shared bool) error {
	mode := unix.LOCK_EX
	if shared {
		mode = unix.LOCK_SH
	}
	return unix.Flock(int(f.Fd()), mode|unix.LOCK_NB)
}
func unlock(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_UN) }
