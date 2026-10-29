//go:build !windows

package transfer

import "os/exec"

func hideProbeWindow(*exec.Cmd) {}
