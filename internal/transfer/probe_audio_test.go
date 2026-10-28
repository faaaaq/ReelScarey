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
	for _, extension := range []string{"mp4", "mkv"} {
		t.Run(extension, func(t *testing.T) {
			name := filepath.Join(t.TempDir(), "audio."+extension)
			args := []string{"-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=10", "-f", "lavfi", "-i", "sine=frequency=1000", "-t", "0.5", "-c:v", "mpeg4", "-c:a", "aac", "-metadata", "title=Incorrect fixture title", "-metadata:s:a:0", "language=jpn"}
			if extension == "mp4" {
				args = append(args, "-movflags", "faststart")
			}
			args = append(args, name)
			cmd := exec.Command("ffmpeg", args...)
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
			if e != nil || len(p.Audio) != 1 || p.Audio[0].Language != "jpn" || p.Audio[0].Codec != "aac" || p.Title == nil || *p.Title != "Incorrect fixture title" {
				t.Fatal(p, e)
			}
		})
	}
}
