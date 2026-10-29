//go:build !windows

package transfer

import (
	"fmt"
	"os"
	"syscall"
)

func openExistingFile(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}

func existingIdentity(file *os.File) (string, error) {
	info, e := file.Stat()
	if e != nil {
		return "", e
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", problem("storage_unavailable", "This filesystem cannot supply a stable file identity.")
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
}
