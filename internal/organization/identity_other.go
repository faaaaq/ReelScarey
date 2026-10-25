//go:build !windows

package organization

import (
	"fmt"
	"os"
	"syscall"
)

func identity(f *os.File) (string, error) {
	info, e := f.Stat()
	if e != nil {
		return "", e
	}
	v, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", problem("storage_unavailable", "The media filesystem cannot supply stable identities.")
	}
	return fmt.Sprintf("%d:%d", v.Dev, v.Ino), nil
}
