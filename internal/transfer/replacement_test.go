package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/faaaaq/ReelScarey/internal/organization"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTVReplacementRequiresExactReviewedMapping(t *testing.T) {
	ctx := context.Background()
	f, series, targets := setupTV(t)
	original := replacementBytes(t, "red")
	incoming := replacementBytes(t, "blue")
	if os.Getenv("REELSCAREY_LIVE_REPLACEMENT") == "1" {
		f.s.importer.Prober = FFprobe{}
	}
	relative := "TV Shows/Show/shared.mkv"
	name := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(relative))
	if e := os.MkdirAll(filepath.Dir(name), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(name, original, 0600); e != nil {
		t.Fatal(e)
	}
	res, e := f.db.Exec(`INSERT INTO media_files(series_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name) VALUES(?,?,?,?,'{}',1,'existing','Shared')`, series, relative, len(original), fmt.Sprintf("%x", sha256.Sum256(original)))
	if e != nil {
		t.Fatal(e)
	}
	fileID, _ := res.LastInsertId()
	for _, target := range targets {
		if _, e = f.db.Exec(`INSERT INTO episode_files(episode_id,media_file_id) VALUES(?,?)`, target.EpisodeID, fileID); e != nil {
			t.Fatal(e)
		}
	}
	releases, e := f.s.SearchTVReplacement(ctx, series, 1, "", fileID)
	if e != nil || len(releases) == 0 {
		t.Fatal(releases, e)
	}
	j, _, e := f.s.QueueReplacement(ctx, releases[0].ID, fileID)
	if e != nil {
		t.Fatal(e)
	}
	f.tick(t)
	f.source(t, j, "upgrade.mkv", incoming)
	f.client.torrent.Progress = 1
	f.client.torrent.AmountLeft = 0
	f.client.torrent.State = "uploading"
	f.client.files = []TorrentFile{{Index: 0, Name: "upgrade.mkv", Size: int64(len(incoming)), Progress: 1, Priority: 1}}
	f.tick(t)
	got, e := f.s.Job(ctx, j.ID)
	if e != nil || got.ErrorCode != "map_episodes" {
		t.Fatal(got, e)
	}
	ids := []int64{}
	for _, target := range targets {
		ids = append(ids, target.EpisodeID)
	}
	if e = f.s.ConfirmTVMapping(ctx, j.ID, TVMappingUpdate{Files: []TVMapping{{FileIndex: 0, EpisodeIDs: ids[:1]}}, SkipEpisodeIDs: ids[1:]}); e == nil {
		t.Fatal("partial replacement accepted")
	}
	if e = f.s.ConfirmTVMapping(ctx, j.ID, TVMappingUpdate{Files: []TVMapping{{FileIndex: 0, EpisodeIDs: ids}}}); e != nil {
		t.Fatal(e)
	}
	f.tick(t)
	org := organization.New(f.db, f.cfg)
	org.SetProbe(replacementProbe)
	p, e := org.PreviewAcquisition(ctx, "download", j.ID)
	if e != nil {
		t.Fatal(e)
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
	var n int
	if e = f.db.QueryRow(`SELECT COUNT(*) FROM episode_files WHERE media_file_id=?`, fileID).Scan(&n); e != nil || n != len(targets) {
		t.Fatal("canonical mappings changed", n, e)
	}
}

func TestDownloadedMovieReplacementRequiresReviewAndRestoresProvenance(t *testing.T) {
	for _, ext := range []string{"mkv", "mp4"} {
		t.Run(ext, func(t *testing.T) { downloadedReplacement(t, ext) })
	}
}
func downloadedReplacement(t *testing.T, extension string) {
	ctx := context.Background()
	f := setupWorkflow(t)
	original := replacementBytes(t, "red")
	incoming := replacementBytes(t, "blue", extension)
	if os.Getenv("REELSCAREY_LIVE_REPLACEMENT") == "1" {
		f.s.importer.Prober = FFprobe{}
	}
	old := f.queue(t)
	f.tick(t)
	f.complete(t, old, original)
	f.tick(t)
	var fileID int64
	var rel string
	if e := f.db.QueryRow(`SELECT id,relative_path FROM media_files WHERE movie_id=?`, f.movie.ID).Scan(&fileID, &rel); e != nil {
		t.Fatal(e)
	}
	f.indexer.hash = "1123456789abcdef0123456789abcdef01234567"
	f.client.torrent = nil
	releases, e := f.s.Search(ctx, f.movie.ID, "")
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = f.s.Queue(ctx, releases[0].ID); e == nil {
		t.Fatal("normal import accepted occupied target")
	}
	j, _, e := f.s.QueueReplacement(ctx, releases[0].ID, fileID)
	if e != nil {
		t.Fatal(e)
	}
	f.tick(t)
	f.client.torrent.Hash = j.Hash
	f.complete(t, j, incoming)
	if extension != "mkv" {
		f.source(t, j, "movie."+extension, incoming)
		f.client.files[0].Name = "movie." + extension
	}
	f.tick(t)
	got, e := f.s.Job(ctx, j.ID)
	if e != nil || got.ErrorCode != "replacement_review" {
		t.Fatal(got, e)
	}
	name := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(rel))
	if b, e := os.ReadFile(name); e != nil || string(b) != string(original) {
		t.Fatal("original changed before review", e)
	}
	s := organization.New(f.db, f.cfg)
	s.SetProbe(replacementProbe)
	p, e := s.PreviewAcquisition(ctx, "download", j.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Apply(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	name = filepath.Join(f.cfg.MediaDir, filepath.FromSlash(p.Replacement.After.Path))
	if b, e := os.ReadFile(name); e != nil || string(b) != string(incoming) {
		t.Fatal(e)
	}
	undo, e := s.PreviewRollback(ctx, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Apply(ctx, undo.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	var download string
	if e = f.db.QueryRow(`SELECT download_id FROM media_files WHERE id=?`, fileID).Scan(&download); e != nil || download != old.ID {
		t.Fatal("provenance not restored", download, e)
	}
	for _, id := range []string{old.ID, j.ID} {
		if _, e = os.Stat(filepath.Join(f.cfg.DownloadsDir, "ReelScarey", id, map[bool]string{true: "movie." + extension, false: "movie.mkv"}[id == j.ID])); e != nil {
			t.Fatal(e)
		}
	}
}

func replacementBytes(t *testing.T, color string, extensions ...string) []byte {
	ext := "mkv"
	if len(extensions) > 0 {
		ext = extensions[0]
	}
	t.Helper()
	if os.Getenv("REELSCAREY_LIVE_REPLACEMENT") != "1" {
		return []byte(color)
	}
	name := filepath.Join(t.TempDir(), "generated."+ext)
	args := []string{"-v", "error", "-f", "lavfi", "-i", "color=c=" + color + ":s=128x96:r=10", "-t", "1", "-c:v", "mpeg4"}
	if ext == "mp4" {
		args = append(args, "-movflags", "faststart")
	}
	cmd := exec.Command("ffmpeg", append(args, name)...)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("generate: %v %s", e, out)
	}
	b, e := os.ReadFile(name)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func replacementProbe(ctx context.Context, f *os.File) (string, error) {
	if os.Getenv("REELSCAREY_LIVE_REPLACEMENT") != "1" {
		return `{}`, nil
	}
	p, e := (FFprobe{}).Probe(ctx, f)
	if e != nil {
		return "", e
	}
	raw, e := json.Marshal(p)
	return string(raw), e
}
