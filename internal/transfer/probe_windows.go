package transfer

import (
	"os/exec"
	"syscall"
)

func hideProbeWindow(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
