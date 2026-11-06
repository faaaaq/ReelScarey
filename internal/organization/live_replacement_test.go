//go:build linux

package organization_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/organization"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

func TestLiveReplacementWithFFprobe(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_REPLACEMENT") != "1" {
		t.Skip("requires generated media and FFmpeg")
	}
	ctx := context.Background()
	dir := t.TempDir()
	cfg := config.Config{MediaDir: filepath.Join(dir, "media"), ConfigDir: filepath.Join(dir, "config"), DownloadsDir: filepath.Join(dir, "downloads")}
	for _, v := range []string{cfg.MediaDir, cfg.DownloadsDir} {
		if e := os.MkdirAll(v, 0700); e != nil {
			t.Fatal(e)
		}
	}
	db, e := database.Open(ctx, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	makeVideo := func(rel, color string) string {
		name := filepath.Join(cfg.MediaDir, filepath.FromSlash(rel))
		if e := os.MkdirAll(filepath.Dir(name), 0700); e != nil {
			t.Fatal(e)
		}
		cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c="+color+":s=128x96:d=1", "-c:v", "mpeg4", "-y", name)
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("generate: %v %s", e, b)
		}
		return name
	}
	old := makeVideo("Movies/Film/Film.mp4", "red")
	candidate := makeVideo("Movies/New/Film.mp4", "blue")
	data, e := os.ReadFile(old)
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(data)
	_, e = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,year,status) VALUES(1,'movie',1,'Fixture',2020,'available')`)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name) VALUES(1,1,'Movies/Film/Film.mp4',?,?,'{}',1,'existing','Film.mp4')`, len(data), hex.EncodeToString(sum[:]))
	if e != nil {
		t.Fatal(e)
	}
	stat, e := os.Stat(candidate)
	if e != nil {
		t.Fatal(e)
	}
	inode := stat.Sys().(*syscall.Stat_t)
	ident := fmt.Sprintf("%d:%d", inode.Dev, inode.Ino)
	_, e = db.Exec(`INSERT INTO library_scans(id,kind,media_root,resolved_root,folder,state,created_at,updated_at) VALUES('scan','movie',?,?,'Movies','completed',1,1)`, cfg.MediaDir, cfg.MediaDir)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`INSERT INTO library_scan_files(id,scan_id,relative_path,size_bytes,mtime,file_identity,updated_at) VALUES('candidate','scan','Movies/New/Film.mp4',?,?,?,1)`, stat.Size(), stat.ModTime().UnixNano(), ident)
	if e != nil {
		t.Fatal(e)
	}
	s := organization.New(db, cfg)
	s.SetProbe(func(ctx context.Context, f *os.File) (string, error) {
		p, e := (transfer.FFprobe{}).Probe(ctx, f)
		if e != nil {
			return "", e
		}
		b, e := json.Marshal(p)
		return string(b), e
	})
	p, e := s.PreviewReplacement(ctx, 1, "candidate")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Apply(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	actual, e := os.ReadFile(old)
	if e != nil {
		t.Fatal(e)
	}
	want, e := os.ReadFile(candidate)
	if e != nil {
		t.Fatal(e)
	}
	if string(actual) != string(want) {
		t.Fatal("incoming video not installed")
	}
	s = organization.New(db, cfg)
	restored, e := s.PreviewRollback(ctx, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Apply(ctx, restored.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	actual, e = os.ReadFile(old)
	if e != nil || string(actual) != string(data) {
		t.Fatal("original video not restored", e)
	}
}

func TestLiveEmbeddedTitleReplacesInPlace(t *testing.T) {
	for _, ext := range []string{"mp4", "m4v", "mkv", "mov", "avi", "webm"} {
		t.Run(ext, func(t *testing.T) { liveEmbeddedTitleReplacesInPlace(t, ext) })
	}
}

func liveEmbeddedTitleReplacesInPlace(t *testing.T, ext string) {
	if os.Getenv("REELSCAREY_LIVE_REPLACEMENT") != "1" {
		t.Skip("requires generated media and FFmpeg")
	}
	ctx := context.Background()
	dir := t.TempDir()
	cfg := config.Config{MediaDir: filepath.Join(dir, "media"), ConfigDir: filepath.Join(dir, "config"), DownloadsDir: filepath.Join(dir, "downloads")}
	for _, v := range []string{cfg.MediaDir, cfg.DownloadsDir} {
		if e := os.MkdirAll(v, 0700); e != nil {
			t.Fatal(e)
		}
	}
	db, e := database.Open(ctx, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	rel := "Movies/Film/Film." + ext
	name := filepath.Join(cfg.MediaDir, filepath.FromSlash(rel))
	if e = os.MkdirAll(filepath.Dir(name), 0700); e != nil {
		t.Fatal(e)
	}
	video, audio := "mpeg4", "aac"
	if ext == "avi" {
		audio = "pcm_s16le"
	}
	if ext == "webm" {
		video, audio = "libvpx-vp9", "libvorbis"
	}
	args := []string{"-v", "error", "-f", "lavfi", "-i", "color=c=red:s=128x96:r=10", "-f", "lavfi", "-i", "sine=frequency=1000", "-t", "1", "-c:v", video, "-c:a", audio, "-metadata", "title=Wrong Title", "-metadata:s:a:0", "language=jpn"}
	if ext == "mp4" || ext == "m4v" || ext == "mov" {
		args = append(args, "-movflags", "faststart")
	}
	cmd := exec.Command("ffmpeg", append(args, "-y", name)...)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("generate: %v %s", e, b)
	}
	original, e := os.ReadFile(name)
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(original)
	if _, e = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,year,status) VALUES(1,'movie',1,'Correct Title',2020,'available')`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name) VALUES(1,1,?,?,?,'{}',1,'existing',?)`, rel, len(original), hex.EncodeToString(sum[:]), "Film."+ext); e != nil {
		t.Fatal(e)
	}
	seed := filepath.Join(cfg.DownloadsDir, "seed.mp4")
	if e = os.Link(name, seed); e != nil {
		t.Fatal(e)
	}
	s := organization.New(db, cfg)
	s.SetProbe(func(ctx context.Context, f *os.File) (string, error) {
		p, e := (transfer.FFprobe{}).Probe(ctx, f)
		if e != nil {
			return "", e
		}
		b, e := json.Marshal(p)
		return string(b), e
	})
	s.SetTagger(func(ctx context.Context, source, stage *os.File, ext, title string) (string, error) {
		p, e := transfer.RewriteTitleVerified(ctx, source, stage, ext, title)
		if e != nil {
			return "", e
		}
		b, e := json.Marshal(p)
		return string(b), e
	})
	p, e := s.PreviewEmbeddedTitle(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Apply(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	f, e := os.Open(name)
	if e != nil {
		t.Fatal(e)
	}
	info, e := (transfer.FFprobe{}).Probe(ctx, f)
	f.Close()
	if e != nil || info.Title == nil || *info.Title != "Correct Title" || len(info.Audio) != 1 || (ext != "avi" && info.Audio[0].Language != "jpn") {
		t.Fatal(info, e)
	}
	seeded, e := os.ReadFile(seed)
	if e != nil || string(seeded) != string(original) {
		t.Fatal("seeding source changed", e)
	}
	saved, e := s.Plan(ctx, p.ID)
	if e != nil || saved.State != "completed" || saved.Replacement.PreviousTitle == nil || *saved.Replacement.PreviousTitle != "Wrong Title" {
		t.Fatal(saved, e)
	}
	if backups, _ := filepath.Glob(filepath.Join(cfg.MediaDir, ".reelscarey-versions", ".reelscarey-version-*")); len(backups) != 0 {
		t.Fatal("title update retained a video copy", backups)
	}
	if _, e = s.PreviewRollback(ctx, p.ID); e == nil {
		t.Fatal("metadata-only rollback accepted without a retained video")
	}
}

func TestLiveHDRTitleRewritePreservesTransfer(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_REPLACEMENT") != "1" {
		t.Skip("requires generated media and FFmpeg")
	}
	ctx := context.Background()
	dir := t.TempDir()
	cfg := config.Config{MediaDir: filepath.Join(dir, "media"), ConfigDir: filepath.Join(dir, "config"), DownloadsDir: filepath.Join(dir, "downloads")}
	for _, v := range []string{cfg.MediaDir, cfg.DownloadsDir} {
		if e := os.MkdirAll(v, 0700); e != nil {
			t.Fatal(e)
		}
	}
	db, e := database.Open(ctx, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	rel := "Movies/Film/Film.mkv"
	name := filepath.Join(cfg.MediaDir, filepath.FromSlash(rel))
	if e = os.MkdirAll(filepath.Dir(name), 0700); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=10", "-t", "1",
		"-vf", "setparams=color_trc=smpte2084:color_primaries=bt2020:colorspace=bt2020nc",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-metadata", "title=Wrong Title", "-y", name)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("generate: %v %s", e, b)
	}
	original, e := os.ReadFile(name)
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(original)
	f, e := os.Open(name)
	if e != nil {
		t.Fatal(e)
	}
	probe, e := (transfer.FFprobe{}).Probe(ctx, f)
	f.Close()
	if e != nil || !probe.HDR {
		t.Fatal("generated HDR fixture was not detected", probe, e)
	}
	raw, e := json.Marshal(probe)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,year,status) VALUES(1,'movie',1,'Correct Title',2020,'available')`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name) VALUES(1,1,?,?,?,?,1,'existing',?)`, rel, len(original), hex.EncodeToString(sum[:]), string(raw), "Film.mkv"); e != nil {
		t.Fatal(e)
	}
	s := organization.New(db, cfg)
	s.SetProbe(func(ctx context.Context, f *os.File) (string, error) {
		p, e := (transfer.FFprobe{}).Probe(ctx, f)
		if e != nil {
			return "", e
		}
		b, e := json.Marshal(p)
		return string(b), e
	})
	s.SetTagger(func(ctx context.Context, source, stage *os.File, ext, title string) (string, error) {
		p, e := transfer.RewriteTitleVerified(ctx, source, stage, ext, title)
		if e != nil {
			return "", e
		}
		b, e := json.Marshal(p)
		return string(b), e
	})
	p, e := s.PreviewEmbeddedTitle(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Apply(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	f, e = os.Open(name)
	if e != nil {
		t.Fatal(e)
	}
	after, e := (transfer.FFprobe{}).Probe(ctx, f)
	f.Close()
	if e != nil || !after.HDR || after.Title == nil || *after.Title != "Correct Title" {
		t.Fatal("HDR transfer or title was not preserved", after, e)
	}
	saved, e := s.Plan(ctx, p.ID)
	if e != nil || saved.Replacement.PreviousTitle == nil || *saved.Replacement.PreviousTitle != "Wrong Title" {
		t.Fatal(saved, e)
	}
	if backups, _ := filepath.Glob(filepath.Join(cfg.MediaDir, ".reelscarey-versions", ".reelscarey-version-*")); len(backups) != 0 {
		t.Fatal("title update retained a video copy", backups)
	}
}

func TestLiveOrganizationUpdatesEmbeddedTitle(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_REPLACEMENT") != "1" {
		t.Skip("requires generated media and FFmpeg")
	}
	ctx := context.Background()
	dir := t.TempDir()
	cfg := config.Config{MediaDir: filepath.Join(dir, "media"), ConfigDir: filepath.Join(dir, "config"), DownloadsDir: filepath.Join(dir, "downloads")}
	for _, v := range []string{cfg.MediaDir, cfg.DownloadsDir} {
		if e := os.MkdirAll(v, 0700); e != nil {
			t.Fatal(e)
		}
	}
	db, e := database.Open(ctx, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	name := filepath.Join(cfg.MediaDir, "Movies", "Film", "Film.mp4")
	if e = os.MkdirAll(filepath.Dir(name), 0700); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=red:s=128x96:r=10", "-t", "1", "-c:v", "mpeg4", "-metadata", "title=Wrong Title", "-movflags", "faststart", "-y", name)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("generate: %v %s", e, b)
	}
	original, e := os.ReadFile(name)
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(original)
	if _, e = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,year,status) VALUES(1,'movie',1,'Correct Title',2020,'available')`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name) VALUES(1,1,'Movies/Film/Film.mp4',?,?,'{}',1,'existing','Film.mp4')`, len(original), hex.EncodeToString(sum[:])); e != nil {
		t.Fatal(e)
	}
	seed := filepath.Join(cfg.DownloadsDir, "seed.mp4")
	if e = os.Link(name, seed); e != nil {
		t.Fatal(e)
	}
	s := organization.New(db, cfg)
	s.SetTagger(func(ctx context.Context, source, stage *os.File, ext, title string) (string, error) {
		p, e := transfer.RewriteTitleVerified(ctx, source, stage, ext, title)
		if e != nil {
			return "", e
		}
		b, e := json.Marshal(p)
		return string(b), e
	})
	p, e := s.PreviewWithEmbeddedTitle(ctx, []int64{1}, "anime")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Apply(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if e = s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	var rel string
	if e = db.QueryRow(`SELECT relative_path FROM media_files WHERE id=1`).Scan(&rel); e != nil {
		t.Fatal(e)
	}
	file, e := os.Open(filepath.Join(cfg.MediaDir, filepath.FromSlash(rel)))
	if e != nil {
		t.Fatal(e)
	}
	probe, e := (transfer.FFprobe{}).Probe(ctx, file)
	file.Close()
	if e != nil || probe.Title == nil || *probe.Title != "Correct Title" {
		t.Fatal(probe, e)
	}
	data, e := os.ReadFile(seed)
	if e != nil || string(data) != string(original) {
		t.Fatal("seeding source changed", e)
	}
}

func TestLiveSeriesOrganizationUpdatesEmbeddedTitle(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_REPLACEMENT") != "1" {
		t.Skip("requires generated media and FFmpeg")
	}
	ctx := context.Background()
	dir := t.TempDir()
	cfg := config.Config{MediaDir: filepath.Join(dir, "media"), ConfigDir: filepath.Join(dir, "config"), DownloadsDir: filepath.Join(dir, "downloads")}
	for _, v := range []string{cfg.MediaDir, cfg.DownloadsDir} {
		if e := os.MkdirAll(v, 0700); e != nil {
			t.Fatal(e)
		}
	}
	db, e := database.Open(ctx, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	name := filepath.Join(cfg.MediaDir, "TV Shows", "Example", "Season 01", "episode.mkv")
	if e = os.MkdirAll(filepath.Dir(name), 0700); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=blue:s=128x96:r=10", "-t", "1", "-c:v", "mpeg4", "-metadata", "title=Wrong Episode", "-y", name)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("generate: %v %s", e, b)
	}
	original, e := os.ReadFile(name)
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(original)
	queries := []string{`INSERT INTO media_items(id,kind,tmdb_id,title,year,status) VALUES(1,'series',1,'Example Show',2020,'available')`, `INSERT INTO series_details(media_id) VALUES(1)`, `INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count) VALUES(1,1,1,1,'Season','','',1)`, `INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date) VALUES(1,1,2,1,'Pilot','','')`}
	for _, q := range queries {
		if _, e = db.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = db.Exec(`INSERT INTO media_files(id,series_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name) VALUES(1,1,'TV Shows/Example/Season 01/episode.mkv',?,?,'{}',1,'existing','episode.mkv')`, len(original), hex.EncodeToString(sum[:])); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO episode_files(episode_id,media_file_id) VALUES(1,1)`); e != nil {
		t.Fatal(e)
	}
	s := organization.New(db, cfg)
	s.SetTagger(func(ctx context.Context, source, stage *os.File, ext, title string) (string, error) {
		p, e := transfer.RewriteTitleVerified(ctx, source, stage, ext, title)
		if e != nil {
			return "", e
		}
		b, e := json.Marshal(p)
		return string(b), e
	})
	p, e := s.PreviewWithEmbeddedTitle(ctx, []int64{1}, "anime")
	if e != nil {
		t.Fatal(e)
	}
	if p.EmbeddedTitle != "Example Show - S01E01 - Pilot" {
		t.Fatal(p.EmbeddedTitle)
	}
	if _, e = s.Apply(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if e = s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	var rel string
	if e = db.QueryRow(`SELECT relative_path FROM media_files WHERE id=1`).Scan(&rel); e != nil {
		t.Fatal(e)
	}
	f, e := os.Open(filepath.Join(cfg.MediaDir, filepath.FromSlash(rel)))
	if e != nil {
		t.Fatal(e)
	}
	probe, e := (transfer.FFprobe{}).Probe(ctx, f)
	f.Close()
	if e != nil || probe.Title == nil || *probe.Title != "Example Show - S01E01 - Pilot" {
		t.Fatal(probe, e)
	}
}

func TestLiveNonFaststartMP4TitleRewrite(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_REPLACEMENT") != "1" {
		t.Skip("requires generated media and FFmpeg")
	}
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, e := exec.LookPath(tool); e != nil {
			t.Fatalf("required binary %s: %v", tool, e)
		}
	}
	ctx := context.Background()
	dir := t.TempDir()
	cfg := config.Config{MediaDir: filepath.Join(dir, "media"), ConfigDir: filepath.Join(dir, "config"), DownloadsDir: filepath.Join(dir, "downloads")}
	for _, v := range []string{cfg.MediaDir, cfg.DownloadsDir} {
		if e := os.MkdirAll(v, 0700); e != nil {
			t.Fatal(e)
		}
	}
	db, e := database.Open(ctx, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	rel := "Movies/Film/Film.mp4"
	name := filepath.Join(cfg.MediaDir, filepath.FromSlash(rel))
	if e = os.MkdirAll(filepath.Dir(name), 0700); e != nil {
		t.Fatal(e)
	}
	// Deliberately omit -movflags faststart so the moov atom is at the end,
	// matching common real-world MP4 files.
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=10", "-t", "1", "-c:v", "mpeg4", "-c:a", "aac", "-metadata", "title=Wrong Title", "-y", name)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("generate: %v %s", e, b)
	}
	original, e := os.ReadFile(name)
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(original)
	if _, e = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,year,status) VALUES(1,'movie',1,'Correct Title',2020,'available')`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name) VALUES(1,1,?,?,?,'{}',1,'existing','Film.mp4')`, rel, len(original), hex.EncodeToString(sum[:])); e != nil {
		t.Fatal(e)
	}
	s := organization.New(db, cfg)
	s.SetProbe(func(ctx context.Context, f *os.File) (string, error) {
		p, e := (transfer.FFprobe{}).Probe(ctx, f)
		if e != nil {
			return "", e
		}
		b, e := json.Marshal(p)
		return string(b), e
	})
	s.SetTagger(func(ctx context.Context, source, stage *os.File, ext, title string) (string, error) {
		p, e := transfer.RewriteTitleVerified(ctx, source, stage, ext, title)
		if e != nil {
			return "", e
		}
		b, e := json.Marshal(p)
		return string(b), e
	})
	p, e := s.PreviewEmbeddedTitle(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Apply(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	f, e := os.Open(name)
	if e != nil {
		t.Fatal(e)
	}
	probe, e := (transfer.FFprobe{}).Probe(ctx, f)
	f.Close()
	if e != nil || probe.Title == nil || *probe.Title != "Correct Title" {
		t.Fatal("title was not rewritten for a non-faststart MP4", probe, e)
	}
	saved, e := s.Plan(ctx, p.ID)
	if e != nil || saved.State != "completed" || saved.Replacement.PreviousTitle == nil || *saved.Replacement.PreviousTitle != "Wrong Title" {
		t.Fatal(saved, e)
	}
	if backups, _ := filepath.Glob(filepath.Join(cfg.MediaDir, ".reelscarey-versions", ".reelscarey-version-*")); len(backups) != 0 {
		t.Fatal("title update retained a video copy", backups)
	}
}
