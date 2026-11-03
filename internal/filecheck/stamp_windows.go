package filecheck

import (
	"fmt"
	"golang.org/x/sys/windows"
	"os"
)

func stamp(f *os.File) (string, string, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); err != nil {
		return "", "", err
	}
	return fmt.Sprintf("%d:%d:%d", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow), "", nil
}
func open(root *os.Root, name string) (*os.File, error) { return root.Open(name) }
