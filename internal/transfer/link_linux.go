package transfer

import (
	"fmt"
	"golang.org/x/sys/unix"
	"os"
)

// Link the already-open source into the already-open destination directory.
// No source or destination ancestor is re-resolved through an absolute path.
func linkSource(source, parent *os.File, name string) error {
	return unix.Linkat(unix.AT_FDCWD, fmt.Sprintf("/proc/self/fd/%d", source.Fd()), int(parent.Fd()), name, unix.AT_SYMLINK_FOLLOW)
}
