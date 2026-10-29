//go:build !linux

package transfer

import (
	"errors"
	"os"
)

// Other hosts use the same verified staged-copy path. Linux can safely link
// across two anchored roots using linkat; an absolute-path fallback is unsafe.
func linkSource(*os.File, *os.File, string) error {
	return errors.New("anchored cross-root hardlink is unavailable on this platform")
}
