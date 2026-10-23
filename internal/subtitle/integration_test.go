package subtitle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/transfer"
)

// Opt-in generated-media test; the provider is an owned fixture. No external
// account, user video or download-client data is used.
func TestRealSubtitleMedia(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_SUBTITLES") != "1" {
		t.Skip("set REELSCAREY_LIVE_SUBTITLES=1 for generated FFmpeg/FFprobe acceptance")
	}
	for _, embedded := range []bool{false, true} {
		name := "automatic sidecars"
		if embedded {
			name = "embedded full signs forced"
		}
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			srt := filepath.Join(f.dir, "generated.srt")
			if e := os.WriteFile(srt, []byte(validSRT), 0600); e != nil {
				t.Fatal(e)
			}
			args := []string{"-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=15", "-f", "srt", "-i", srt, "-t", "8", "-map", "0:v:0"}
			if embedded {
				args = append(args, "-map", "1:0", "-map", "1:0", "-map", "1:0", "-c:s", "srt", "-metadata:s:s:0", "language=eng", "-metadata:s:s:1", "language=eng", "-metadata:s:s:1", "title=Signs and Songs", "-metadata:s:s:2", "language=eng", "-disposition:s:2", "forced")
			}
			args = append(args, "-c:v", "libx264", "-preset", "ultrafast", "-y", f.video)
			if output, e := exec.Command("ffmpeg", args...).CombinedOutput(); e != nil {
				t.Fatalf("generated video: %v %s", e, output)
			}
			original, e := os.ReadFile(f.video)
			if e != nil {
				t.Fatal(e)
			}
			sum := sha256.Sum256(original)
			if _, e = f.db.Exec(`UPDATE media_files SET size_bytes=?,sha256=?,probe_json='{}' WHERE id=1`, len(original), hex.EncodeToString(sum[:])); e != nil {
				t.Fatal(e)
			}
			f.s.Prober = transfer.FFprobe{}
			enable(t, f, true)
			process(t, f.s)
			view, e := f.s.File(context.Background(), 1, false)
			if e != nil || view.Job.State != "completed" || len(view.Tracks) != 3 {
				t.Fatal(view, view.Job, e)
			}
			if embedded {
				if f.p.searches != 0 || f.p.downloads != 0 {
					t.Fatal("embedded streams consumed provider quota")
				}
				for _, track := range view.Tracks {
					if track.Source != "embedded" {
						t.Fatal(track)
					}
				}
			} else {
				if f.p.downloads != 3 {
					t.Fatal(f.p.downloads)
				}
				for _, suffix := range []string{".en.srt", ".en.signs.srt", ".en.forced.srt"} {
					b, e := os.ReadFile(strings.TrimSuffix(f.video, ".mkv") + suffix)
					if e != nil || string(b) != validSRT {
						t.Fatal(suffix, e)
					}
				}
			}
			after, e := os.ReadFile(f.video)
			if e != nil || !bytes.Equal(after, original) {
				t.Fatal("generated video modified", e)
			}
		})
	}
}
