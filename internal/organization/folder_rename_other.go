//go:build !linux

package organization

import "os"

func renameFolderNoReplace(_ *os.Root, _, _ string) error {
	return problem("unsupported_platform", "Whole-folder moves require the Linux Docker deployment.")
}
