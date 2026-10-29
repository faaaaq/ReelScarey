package youtube

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/organization"
	"github.com/faaaaq/ReelScarey/internal/transfer"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestYouTubeReplacementWithSelectedSubtitlesAndRollback(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	cfg := config.Config{ConfigDir: filepath.Join(base, "config"), MediaDir: filepath.Join(base, "media"), DownloadsDir: filepath.Join(base, "downloads")}
	for _, p := range []string{cfg.MediaDir, cfg.DownloadsDir} {
		if e := os.MkdirAll(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	db, e := database.Open(ctx, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if e = os.MkdirAll(filepath.Join(cfg.MediaDir, "Movies"), 0700); e != nil {
		t.Fatal(e)
	}
	name := filepath.Join(cfg.MediaDir, "Movies", "original.mp4")
	data := []byte("original video version")
	incoming := []byte("owned generated video")
	if os.Getenv("REELSCAREY_LIVE_REPLACEMENT") == "1" {
		data = youtubeReplacementBytes(t, "red")
		incoming = youtubeReplacementBytes(t, "blue")
	}
	if e = os.WriteFile(name, data, 0600); e != nil {
		t.Fatal(e)
	}
	sha := fmt.Sprintf("%x", sha256.Sum256(data))
	if _, e = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title) VALUES(1,'movie',1,'Movie'); INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name) VALUES(1,1,'Movies/original.mp4',?,?,'{}',1,'existing','Original')`, len(data), sha); e != nil {
		t.Fatal(e)
	}
	s := NewStore(db)
	r, e := s.PreviewWithSubtitles(ctx, Video{ID: "abcdefghijk", Title: "New version"}, Target{Kind: "movie", MediaID: 1}, DownloadOptions{Languages: []string{"en"}})
	if e != nil {
		t.Fatal(e)
	}
	j, e := s.Queue(ctx, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	w := Worker{Store: s, Downloader: replacementDownloader{incoming}, Prober: testProber{}, StagingRoot: filepath.Join(base, "staging")}
	if os.Getenv("REELSCAREY_LIVE_REPLACEMENT") == "1" {
		w.Prober = transfer.FFprobe{}
	}
	if _, e = w.ProcessOne(ctx); e != nil {
		t.Fatal(e)
	}
	if os.Getenv("REELSCAREY_LIVE_REPLACEMENT") == "1" {
		w.Prober = transfer.FFprobe{}
	}
	ir, e := s.ReviewImport(ctx, cfg, j.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ApproveImport(ctx, cfg, ir); e != nil {
		t.Fatal(e)
	}
	if _, e = w.ProcessImport(ctx, cfg); e != nil {
		t.Fatal(e)
	}
	if b, e := os.ReadFile(name); e != nil || string(b) != string(data) {
		t.Fatal("changed before replacement review", e)
	}
	org := organization.New(db, cfg)
	org.SetProbe(func(ctx context.Context, f *os.File) (string, error) {
		if os.Getenv("REELSCAREY_LIVE_REPLACEMENT") != "1" {
			return `{}`, nil
		}
		p, e := (transfer.FFprobe{}).Probe(ctx, f)
		if e != nil {
			return "", e
		}
		b, e := json.Marshal(p)
		return string(b), e
	})
	p, e := org.PreviewAcquisition(ctx, "youtube", j.ID)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Replacement.AcquisitionSubtitles) != 1 {
		t.Fatal("subtitle missing", p)
	}
	if _, e = org.Apply(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	if e = org.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if b, e := os.ReadFile(name); e != nil || string(b) != string(incoming) {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(cfg.MediaDir, filepath.FromSlash(p.Replacement.AcquisitionSubtitles[0].File.Destination))); e != nil {
		t.Fatal(e)
	}
	undo, e := org.PreviewRollback(ctx, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = org.Apply(ctx, undo.ID); e != nil {
		t.Fatal(e)
	}
	if e = org.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if b, e := os.ReadFile(name); e != nil || string(b) != string(data) {
		t.Fatal("version not restored", e)
	}
	var n int
	if e = db.QueryRow(`SELECT COUNT(*) FROM youtube_files WHERE media_file_id=1`).Scan(&n); e != nil || n != 0 {
		t.Fatal("wrong restored provenance", e)
	}
}

type replacementDownloader struct{ data []byte }

func (d replacementDownloader) Download(ctx context.Context, id, dir string) (string, error) {
	name := id + ".mp4"
	return name, os.WriteFile(filepath.Join(dir, name), d.data, 0600)
}
func (d replacementDownloader) DownloadWithSubtitles(ctx context.Context, id, dir string, opts DownloadOptions) (DownloadResult, error) {
	name, e := d.Download(ctx, id, dir)
	if e != nil {
		return DownloadResult{}, e
	}
	sub := id + ".en.vtt"
	if e = os.WriteFile(filepath.Join(dir, sub), []byte("WEBVTT\n\n00:00:00.000 --> 00:00:01.000\nGenerated\n"), 0600); e != nil {
		return DownloadResult{}, e
	}
	return DownloadResult{Video: name, Subtitles: []SubtitleFile{{Language: "en", Name: sub}}}, nil
}
func youtubeReplacementBytes(t *testing.T, color string) []byte {
	t.Helper()
	name := filepath.Join(t.TempDir(), "video.mp4")
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c="+color+":s=128x96:r=10", "-t", "1", "-c:v", "mpeg4", "-movflags", "faststart", name)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("generate: %v %s", e, out)
	}
	b, e := os.ReadFile(name)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
