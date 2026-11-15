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

// runTranscodeFile is the file-output variant the stream cache uses: ffmpeg
// reads the registered file on stdin and writes the encoded audio to dest
// (the .part staging path inside the cache dir). The process is awaited, so
// the caller learns whether the encode actually finished before the cache
// publishes the file. Standard error is discarded for the same reason as the
// pipe variant: a conversion failure must never leak a local path.
func runTranscodeFile(ctx context.Context, ffmpeg string, input io.Reader, codec string, bitRate, timeOffset int, dest string) error {
	cmd := exec.CommandContext(ctx, ffmpeg, transcodeFileArgs(codec, bitRate, timeOffset, dest)...)
	cmd.Stdin = input
	cmd.Stderr = io.Discard
	return cmd.Run()
}
