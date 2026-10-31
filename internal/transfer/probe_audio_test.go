package transfer

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// FFmpeg versions differ in whether these audio/container combinations preserve
// timing. Compare independently probed packets: a safe remux may succeed, while
// any changed packet must still be rejected.
func TestRewriteTitlePreservesPacketsOrRejectsUnsafeRemux(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg unavailable")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("FFprobe unavailable")
	}
	for _, fixture := range []struct{ ext, video, audio string }{
		{"avi", "mpeg4", "aac"}, {"webm", "libvpx-vp9", "libopus"},
	} {
		t.Run(fixture.ext, func(t *testing.T) {
			dir := t.TempDir()
			name := filepath.Join(dir, "original."+fixture.ext)
			cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=10", "-f", "lavfi", "-i", "sine=frequency=1000", "-t", "0.5", "-c:v", fixture.video, "-c:a", fixture.audio, "-metadata", "title=Original", name)
			hideProbeWindow(cmd)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("generate: %v %s", err, out)
			}
			original, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			source, err := os.Open(name)
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			stage, err := os.Create(filepath.Join(dir, "stage"))
			if err != nil {
				t.Fatal(err)
			}
			defer stage.Close()
			_, rewriteErr := RewriteTitleVerified(context.Background(), source, stage, "."+fixture.ext, "New title")
			packets := func(filename string) []byte {
				t.Helper()
				probe := exec.Command("ffprobe", "-v", "error", "-show_packets", "-show_data_hash", "sha256", "-show_entries", "packet=stream_index,pts_time,dts_time,duration_time,size,data_hash", "-of", "json", filename)
				hideProbeWindow(probe)
				out, e := probe.CombinedOutput()
				if e != nil {
					t.Fatalf("inspect packets: %v %s", e, out)
				}
				return out
			}
			if !bytes.Equal(packets(name), packets(stage.Name())) && rewriteErr == nil {
				t.Fatal("accepted a remux that changes packet timing or content")
			}
			if rewriteErr == nil {
				t.Log("This FFmpeg version preserved packet timing and content.")
			} else {
				t.Log("Unsafe remux rejected:", rewriteErr)
			}
			after, err := os.ReadFile(name)
			if err != nil || !bytes.Equal(original, after) {
				t.Fatal("source changed", err)
			}
		})
	}
}

func TestRealFFprobeReadsEmbeddedAudioLanguage(t *testing.T) {
	if _, e := exec.LookPath("ffmpeg"); e != nil {
		t.Skip("FFmpeg unavailable")
	}
	if _, e := exec.LookPath("ffprobe"); e != nil {
		t.Skip("FFprobe unavailable")
	}
	for _, extension := range []string{"mp4", "m4v", "mkv", "mov", "avi", "webm"} {
		t.Run(extension, func(t *testing.T) {
			name := filepath.Join(t.TempDir(), "audio."+extension)
			args := []string{"-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=10", "-f", "lavfi", "-i", "sine=frequency=1000", "-t", "0.5", "-c:v", "mpeg4", "-c:a", "aac", "-metadata", "title=Incorrect fixture title", "-metadata", "comment=Keep this comment", "-metadata:s:a:0", "language=jpn"}
			if extension == "avi" {
				for i, arg := range args {
					if arg == "aac" {
						args[i] = "pcm_s16le"
					}
				}
			}
			if extension == "webm" {
				for i, arg := range args {
					if arg == "mpeg4" {
						args[i] = "libvpx-vp9"
					}
					if arg == "aac" {
						args[i] = "libvorbis"
					}
				}
			}
			if extension == "mp4" || extension == "m4v" || extension == "mov" {
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
			if e != nil || len(p.Audio) != 1 || (extension != "avi" && p.Audio[0].Language != "jpn") || p.Title == nil || *p.Title != "Incorrect fixture title" {
				t.Fatal(p, e)
			}
			stage, e := os.Create(filepath.Join(t.TempDir(), "stage"))
			if e != nil {
				t.Fatal(e)
			}
			defer stage.Close()
			p, e = RewriteTitleVerified(context.Background(), f, stage, "."+extension, "Correct fixture title")
			if e != nil || p.Title == nil || *p.Title != "Correct fixture title" {
				a, _ := retagInspect(context.Background(), f)
				b, _ := retagInspect(context.Background(), stage)
				t.Fatalf("rewrite: %v\nbefore: %+v %x\nafter: %+v %x", e, a.Manifest, a.PacketHash, b.Manifest, b.PacketHash)
			}
			old, e := (FFprobe{}).Probe(context.Background(), f)
			if e != nil || old.Title == nil || *old.Title != "Incorrect fixture title" {
				t.Fatal("source changed", old, e)
			}
		})
	}
}

func TestRewriteTitlePreservesChaptersSubtitlesAndAttachments(t *testing.T) {
	if _, e := exec.LookPath("ffmpeg"); e != nil {
		t.Skip("FFmpeg unavailable")
	}
	if _, e := exec.LookPath("ffprobe"); e != nil {
		t.Skip("FFprobe unavailable")
	}
	dir := t.TempDir()
	subtitle := filepath.Join(dir, "subtitle.srt")
	font := filepath.Join(dir, "font.ttf")
	chapters := filepath.Join(dir, "chapters.txt")
	name := filepath.Join(dir, "input.mkv")
	for filename, content := range map[string]string{subtitle: "1\n00:00:00,000 --> 00:00:01,000\nHello\n", font: "disposable attachment bytes", chapters: ";FFMETADATA1\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=0\nEND=1000\ntitle=Chapter One\n"} {
		if e := os.WriteFile(filename, []byte(content), 0600); e != nil {
			t.Fatal(e)
		}
	}
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=10", "-f", "lavfi", "-i", "sine=frequency=1000", "-i", subtitle, "-f", "ffmetadata", "-i", chapters, "-t", "1", "-map", "0:v", "-map", "1:a", "-map", "2:s", "-map_metadata", "3", "-map_chapters", "3", "-c:v", "mpeg4", "-c:a", "aac", "-c:s", "srt", "-metadata", "title=Wrong title", "-metadata:s:a:0", "language=jpn", "-metadata:s:s:0", "language=eng", "-attach", font, "-metadata:s:t:0", "mimetype=application/x-truetype-font", "-metadata:s:t:0", "filename=font.ttf", "-y", name)
	hideProbeWindow(cmd)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("generate: %v %s", e, out)
	}
	source, e := os.Open(name)
	if e != nil {
		t.Fatal(e)
	}
	defer source.Close()
	stage, e := os.Create(filepath.Join(dir, "stage.mkv"))
	if e != nil {
		t.Fatal(e)
	}
	defer stage.Close()
	p, e := RewriteTitleVerified(context.Background(), source, stage, ".mkv", "Correct title")
	if e != nil {
		a, _ := retagInspect(context.Background(), source)
		b, _ := retagInspect(context.Background(), stage)
		t.Fatalf("rewrite: %v\nbefore: %+v\nafter: %+v", e, a.Manifest, b.Manifest)
	}
	if p.Title == nil || *p.Title != "Correct title" {
		t.Fatal(p)
	}
	extract := filepath.Join(dir, "extracted.ttf")
	cmd = exec.Command("ffmpeg", "-v", "error", "-dump_attachment:t:0", extract, "-i", stage.Name(), "-f", "null", "-")
	hideProbeWindow(cmd)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("extract: %v %s", e, out)
	}
	content, e := os.ReadFile(extract)
	if e != nil || string(content) != "disposable attachment bytes" {
		t.Fatal(string(content), e)
	}
}

func TestRewriteTitleRejectsSharedOrUnsupportedStage(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "source.mkv")
	if e := os.WriteFile(name, []byte("source"), 0600); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(dir, "link.mkv")
	if e := os.Link(name, link); e != nil {
		t.Fatal(e)
	}
	source, e := os.Open(name)
	if e != nil {
		t.Fatal(e)
	}
	defer source.Close()
	stage, e := os.OpenFile(link, os.O_RDWR, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer stage.Close()
	if _, e = RewriteTitleVerified(context.Background(), source, stage, ".mkv", "Title"); e == nil {
		t.Fatal("shared inode accepted")
	}
	if _, e = RewriteTitleVerified(context.Background(), source, stage, ".ts", "Title"); e == nil {
		t.Fatal("unsupported container accepted")
	}
	content, e := os.ReadFile(name)
	if e != nil || string(content) != "source" {
		t.Fatal(string(content), e)
	}
}
