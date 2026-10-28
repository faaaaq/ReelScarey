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
