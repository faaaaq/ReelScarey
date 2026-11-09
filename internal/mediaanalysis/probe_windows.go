//go:build windows

package mediaanalysis

import (
	"os"
	"os/exec"
	"syscall"
)

func openFile(root *os.Root, name string) (*os.File, error) { return root.Open(name) }

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

func probeInput() string { return "pipe:0" }

func probeProtocols() string { return "pipe" }
