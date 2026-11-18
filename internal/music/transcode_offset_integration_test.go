package music

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Exercise output seeking with real audio rather than the echo helper. The
// source is generated in a disposable root; no owner media or opt-in live
// service is involved. Machines without both tools keep an explicit skip.
func TestOpenPlaybackRealFFmpegOffsetProducesShortenedAudio(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed; real offset-audio validation was not run")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is not installed; real offset-audio validation was not run")
	}
	transcodeLookup = func(string) (string, error) { return ffmpeg, nil }
	resetTranscodeCacheForTest()
	t.Cleanup(func() {
		transcodeLookup = exec.LookPath
		resetTranscodeCacheForTest()
	})
	s, _, fileID := transcodeFixture(t)
	source := filepath.Join(s.mediaDir, "Music", "Artist", "Album", "01 Song.wma")
	generate := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100:duration=3",
		"-c:a", "pcm_s16le", "-f", "wav", source)
	if output, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("generate owned three-second WAV fixture: %v: %s", err, output)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stream, err := s.OpenPlayback(ctx, fileID, "wav", 0, 1)
	if err != nil {
		t.Fatalf("real offset conversion: %v", err)
	}
	if stream.Body == nil || stream.Mode != "offset-live" {
		t.Fatalf("offset response = body %v mode %q", stream.Body, stream.Mode)
	}
	data, err := io.ReadAll(stream.Body)
	stream.Close()
	if err != nil {
		t.Fatalf("read real offset audio: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("real offset conversion returned no audio")
	}
	output := filepath.Join(t.TempDir(), "offset.wav")
	if err := os.WriteFile(output, data, 0o600); err != nil {
		t.Fatal(err)
	}
	probe := exec.Command(ffprobe, "-v", "error", "-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", output)
	probed, err := probe.CombinedOutput()
	if err != nil {
		t.Fatalf("probe offset output: %v: %s", err, probed)
	}
	duration, err := strconv.ParseFloat(strings.TrimSpace(string(probed)), 64)
	if err != nil {
		t.Fatalf("parse probed duration %q: %v", probed, err)
	}
	// Three seconds minus a one-second output seek must produce approximately
	// two seconds. PCM/WAV makes this substantially tighter than MP3 padding.
	if duration < 1.9 || duration > 2.1 {
		t.Fatalf("offset output duration = %.3fs, want 2s (source 3s, offset 1s)", duration)
	}
}
