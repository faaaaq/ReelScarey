//go:build linux

package maintenance

import (
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
)

func publishRootFolder(root *os.Root, source, destination string) error {
	if filepath.Base(source) != source || filepath.Base(destination) != destination {
		return ErrRootMigration
	}
	dir, e := root.Open(".")
	if e != nil {
		return e
	}
	defer dir.Close()
	if e = unix.Renameat2(int(dir.Fd()), source, int(dir.Fd()), destination, unix.RENAME_NOREPLACE); e != nil {
		return e
	}
	return dir.Sync()
}
