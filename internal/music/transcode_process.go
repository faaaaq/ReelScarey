package music

import (
	"context"
	"io"
	"os/exec"
)

// transcodeProcess is one running ffmpeg conversion.
type transcodeProcess struct {
	body io.ReadCloser
	cmd  *exec.Cmd
}

// newTranscodeStream starts ffmpeg with the registered file on stdin and its
// audio output on a pipe. Standard error is discarded so a conversion failure
// can never leak a local path into the HTTP response.
func newTranscodeStream(ctx context.Context, ffmpeg string, input io.Reader, codec string, bitRate, timeOffset int) (transcodeProcess, error) {
	cmd := exec.CommandContext(ctx, ffmpeg, transcodeArgs(codec, bitRate, timeOffset)...)
	cmd.Stdin = input
	cmd.Stderr = io.Discard
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return transcodeProcess{}, err
	}
	if err := cmd.Start(); err != nil {
		return transcodeProcess{}, err
	}
	return transcodeProcess{body: stdout, cmd: cmd}, nil
}
