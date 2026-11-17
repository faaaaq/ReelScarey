//go:build linux

package transfer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRealTrackRewritePreservesRetainedPackets(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_ORGANIZATION") != "1" {
		t.Skip("set REELSCAREY_LIVE_ORGANIZATION=1 to exercise FFmpeg and FFprobe")
	}
	ctx := context.Background()
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub.srt")
	if err := os.WriteFile(sub, []byte("1\n00:00:00,100 --> 00:00:01,000\nSubtitles\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(dir, "source.mkv")
	cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error",
		"-f", "lavfi", "-i", "color=size=64x64:rate=10:duration=2",
		"-f", "lavfi", "-i", "sine=frequency=330:duration=2",
		"-f", "lavfi", "-i", "sine=frequency=660:duration=2",
		"-i", sub, "-map", "0:v", "-map", "1:a", "-map", "2:a", "-map", "3:s",
		"-c:v", "mpeg4", "-c:a", "pcm_s16le", "-c:s", "srt", "-metadata", "title=Canonical title",
		"-metadata:s:a:0", "language=jpn", "-metadata:s:a:1", "language=spa", "-metadata:s:s:0", "language=eng", sourcePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("write generated media: %v\n%s", err, output)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	stage, err := os.Create(filepath.Join(dir, "edited.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Close()
	streams, err := ReadCleanupStreams(ctx, source)
	if err != nil || len(streams) != 3 {
		t.Fatalf("streams: %+v %v", streams, err)
	}
	probe, err := RewriteTracksVerified(ctx, source, stage, ".mkv", []int{streams[1].Index, streams[2].Index})
	if err != nil {
		t.Fatal(err)
	}
	if len(probe.Audio) != 1 || probe.Audio[0].Language != "jpn" || len(probe.Subtitles) != 0 || probe.Title == nil || *probe.Title != "Canonical title" {
		t.Fatalf("rewrite: %+v", probe)
	}
}
