//go:build linux

package organization

import (
	"os"
	"path"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func renameFolderNoReplace(root *os.Root, source, destination string) error {
	for _, dir := range []string{path.Dir(source), path.Dir(destination)} {
		if err := segments(root, dir, true); err != nil {
			return err
		}
		if err := root.MkdirAll(filepath.FromSlash(dir), 0755); err != nil {
			return err
		}
		if err := segments(root, dir, false); err != nil {
			return err
		}
	}
	src, err := root.Open(filepath.FromSlash(path.Dir(source)))
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := root.Open(filepath.FromSlash(path.Dir(destination)))
	if err != nil {
		return err
	}
	defer dst.Close()
	if err = unix.Renameat2(int(src.Fd()), path.Base(source), int(dst.Fd()), path.Base(destination), unix.RENAME_NOREPLACE); err != nil {
		if err == unix.EXDEV {
			return problem("cross_device", "These collections are on different filesystems; whole-folder moves across filesystems are not yet supported.")
		}
		if err == unix.EEXIST || err == unix.ENOTEMPTY {
			return problem("destination_exists", "The destination folder already exists.")
		}
		return err
	}
	if err = src.Sync(); err != nil {
		return err
	}
	return dst.Sync()
}
