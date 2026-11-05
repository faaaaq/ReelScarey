package transfer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFFprobeReadsHDRTransfer(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg unavailable")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("FFprobe unavailable")
	}
	dir := t.TempDir()
	for _, fixture := range []struct {
		name   string
		filter string
		hdr    bool
	}{
		{"sdr", "", false},
		{"hdr10", "setparams=color_trc=smpte2084:color_primaries=bt2020:colorspace=bt2020nc", true},
		{"hlg", "setparams=color_trc=arib-std-b67:color_primaries=bt2020:colorspace=bt2020nc", true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			name := filepath.Join(dir, fixture.name+".mkv")
			args := []string{"-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=10", "-t", "0.2"}
			if fixture.filter != "" {
				args = append(args, "-vf", fixture.filter)
			}
			args = append(args, "-c:v", "libx264", "-pix_fmt", "yuv420p", "-y", name)
			cmd := exec.Command("ffmpeg", args...)
			hideProbeWindow(cmd)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("generate: %v %s", err, out)
			}
			f, err := os.Open(name)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			probe, err := (FFprobe{}).Probe(context.Background(), f)
			if err != nil {
				t.Fatal(err)
			}
			if probe.HDR != fixture.hdr || probe.Width != 64 || probe.Height != 64 {
				t.Fatalf("probe = %+v, want HDR %v at 64x64", probe, fixture.hdr)
			}
		})
	}
}
