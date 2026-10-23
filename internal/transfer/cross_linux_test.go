package transfer

import (
	"context"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRealCrossFilesystemCopy(t *testing.T) {
	media, e := os.MkdirTemp("/dev/shm", "reelscarey-import-test-")
	if e != nil {
		t.Skip("no writable independent tmpfs")
	}
	defer os.RemoveAll(media)
	f := setupWorkflow(t)
	var a, b unix.Stat_t
	if unix.Stat(f.cfg.DownloadsDir, &a) != nil || unix.Stat(media, &b) != nil || a.Dev == b.Dev {
		t.Skip("test paths are on the same device")
	}
	f.s.cfg.MediaDir = media
	j := f.queue(t)
	f.tick(t)
	data := []byte(strings.Repeat("cross filesystem movie bytes", 10000))
	f.complete(t, j, data)
	f.tick(t)
	job, e := f.s.Job(context.Background(), j.ID)
	if e != nil || job.State != "completed" {
		t.Fatal(job.ErrorCode, e)
	}
	var mode, relative string
	if e = f.db.QueryRow(`SELECT mode,destination_path FROM import_operations WHERE download_id=?`, j.ID).Scan(&mode, &relative); e != nil {
		t.Fatal(e)
	}
	if mode != "copy" {
		t.Fatal("real EXDEV did not choose copy")
	}
	saved, e := os.ReadFile(filepath.Join(media, filepath.FromSlash(relative)))
	if e != nil || string(saved) != string(data) {
		t.Fatal("cross-filesystem output differs", e)
	}
}
