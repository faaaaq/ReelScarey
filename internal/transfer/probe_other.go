//go:build !windows

package transfer

import "os/exec"

func hideProbeWindow(*exec.Cmd) {}

// stdin is the already anchored regular file. Opening its inherited descriptor
// permits seeks to MP4/MOV metadata at the end without streaming the whole video.
func probeInput() string     { return "/proc/self/fd/0" }
func probeProtocols() string { return "file" }
