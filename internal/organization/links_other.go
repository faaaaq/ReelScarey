//go:build !windows

package organization

import (
	"os"
	"syscall"
)

func mediaLinkCount(file *os.File) (uint64, error) {
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, problem("storage_unavailable", "The media filesystem cannot report hardlink counts.")
	}
	return uint64(stat.Nlink), nil
}
