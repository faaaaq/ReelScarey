//go:build !windows

package filecheck

import (
	"fmt"
	"os"
	"syscall"
)

func stamp(f *os.File) (string, string, error) {
	info, err := f.Stat()
	if err != nil {
		return "", "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", "", ErrChanged
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), fmt.Sprintf("%d:%d", stat.Ctim.Sec, stat.Ctim.Nsec), nil
}
func open(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
