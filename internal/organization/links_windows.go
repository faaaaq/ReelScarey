//go:build windows

package organization

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

type fileStandardInformation struct {
	AllocationSize int64
	EndOfFile      int64
	NumberOfLinks  uint32
	DeletePending  uint8
	Directory      uint8
}

func mediaLinkCount(file *os.File) (uint64, error) {
	var info fileStandardInformation
	if err := windows.GetFileInformationByHandleEx(windows.Handle(file.Fd()), windows.FileStandardInfo, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return 0, err
	}
	return uint64(info.NumberOfLinks), nil
}
