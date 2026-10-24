package transfer

import (
	"fmt"
	"golang.org/x/sys/windows"
	"os"
)

func openExistingFile(root *os.Root, name string) (*os.File, error) { return root.Open(name) }

func existingIdentity(file *os.File) (string, error) {
	var info windows.ByHandleFileInformation
	if e := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info); e != nil {
		return "", problem("storage_unavailable", "This filesystem cannot supply a stable file identity.")
	}
	return fmt.Sprintf("%d:%d:%d", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow), nil
}
