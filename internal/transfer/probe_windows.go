package transfer

import (
	"os/exec"
	"syscall"
)

func hideProbeWindow(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }

func probeInput() string     { return "pipe:0" }
func probeProtocols() string { return "pipe" }
