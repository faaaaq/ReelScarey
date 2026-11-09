//go:build !windows

package mediaanalysis

import (
	"os"
	"os/exec"
	"syscall"
)

func openFile(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}

func hideWindow(*exec.Cmd) {}

func probeInput() string { return "/proc/self/fd/0" }

func probeProtocols() string { return "file" }
