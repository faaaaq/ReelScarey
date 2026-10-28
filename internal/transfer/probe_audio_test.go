package transfer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRealFFprobeReadsEmbeddedAudioLanguage(t *testing.T) {
	if _, e := exec.LookPath("ffmpeg"); e != nil {
		t.Skip("FFmpeg unavailable")
	}
	if _, e := exec.LookPath("ffprobe"); e != nil {
		t.Skip("FFprobe unavailable")
	}
	name := filepath.Join(t.TempDir(), "audio.mp4")
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=10", "-f", "lavfi", "-i", "sine=frequency=1000", "-t", "0.5", "-c:v", "mpeg4", "-c:a", "aac", "-metadata:s:a:0", "language=jpn", "-movflags", "faststart", name)
	hideProbeWindow(cmd)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("generate: %v %s", e, out)
	}
	f, e := os.Open(name)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	p, e := (FFprobe{}).Probe(context.Background(), f)
	if e != nil || len(p.Audio) != 1 || p.Audio[0].Language != "jpn" || p.Audio[0].Codec != "aac" {
		t.Fatal(p, e)
	}
}
