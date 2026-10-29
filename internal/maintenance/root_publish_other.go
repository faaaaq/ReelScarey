//go:build !linux

package maintenance

import (
	"errors"
	"os"
)

func publishRootFolder(_ *os.Root, _, _ string) error {
	return errors.New("Media-root migration requires the Linux Docker deployment.")
}
