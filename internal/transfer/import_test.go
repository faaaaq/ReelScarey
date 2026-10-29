package transfer

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

func plannedImport(t *testing.T, f *workflowFixture) (Job, importPlan, []byte) {
	t.Helper()
	j := f.queue(t)
	data := []byte(strings.Repeat("crash-safe movie payload ", 10000))
	source := f.source(t, j, "movie.mkv", data)
	owned, e := f.s.claim(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	file, e := os.Open(filepath.Join(f.cfg.DownloadsDir, filepath.FromSlash(source)))
	if e != nil {
		t.Fatal(e)
	}
	defer file.Close()
	p, e := f.s.importer.plan(context.Background(), owned, source, int64(len(data)), file)
	if e != nil {
		t.Fatal(e)
	}
	return owned, p, data
}
func restartImport(t *testing.T, f *workflowFixture, j Job) {
	t.Helper()
	if _, e := f.db.Exec(`UPDATE downloads SET state='importing',lease_owner='',lease_until=0,next_run=0 WHERE id=?`, j.ID); e != nil {
		t.Fatal(e)
	}
	f.client.offline = true
	f.reopen(t)
	f.tick(t)
}
func TestPartialCopyResumesAfterRestart(t *testing.T) {
	f := setupWorkflow(t)
	j, p, data := plannedImport(t, f)
	dir := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(path.Dir(p.Destination)))
	if e := os.MkdirAll(dir, 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dir, p.Stage), data[:len(data)/2], 0600); e != nil {
		t.Fatal(e)
	}
	if e := f.s.importer.phase(context.Background(), j, &p, "planned", "copy"); e != nil {
		t.Fatal(e)
	}
	restartImport(t, f, j)
	job, e := f.s.Job(context.Background(), j.ID)
	if e != nil || job.State != "completed" {
		t.Fatal(job.State, job.ErrorCode, e)
	}
	saved, e := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(p.Destination)))
	if e != nil || string(saved) != string(data) {
		t.Fatal("partial copy did not resume", e)
	}
	if f.probe.calls != 1 {
		t.Fatal("import journal was not reused")
	}
}
func TestPublishedFileReconcilesAfterRestart(t *testing.T) {
	f := setupWorkflow(t)
	j, p, data := plannedImport(t, f)
	dir := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(path.Dir(p.Destination)))
	if e := os.MkdirAll(dir, 0755); e != nil {
		t.Fatal(e)
	}
	stage := filepath.Join(dir, p.Stage)
	if e := os.WriteFile(stage, data, 0644); e != nil {
		t.Fatal(e)
	}
	if e := os.Link(stage, filepath.Join(dir, path.Base(p.Destination))); e != nil {
		t.Fatal(e)
	}
	if e := f.s.importer.phase(context.Background(), j, &p, "staged", "copy"); e != nil {
		t.Fatal(e)
	}
	restartImport(t, f, j)
	job, e := f.s.Job(context.Background(), j.ID)
	if e != nil || job.State != "completed" {
		t.Fatal(job.State, job.ErrorCode, e)
	}
	var count int
	if e = f.db.QueryRow(`SELECT COUNT(*) FROM media_files WHERE download_id=?`, j.ID).Scan(&count); e != nil || count != 1 {
		t.Fatal("published file registered more than once", count, e)
	}
	if _, e = os.Stat(stage); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("committed staging link remained", e)
	}
}
func TestDestinationCollisionDoesNotOverwrite(t *testing.T) {
	f := setupWorkflow(t)
	j, p, _ := plannedImport(t, f)
	dest := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(p.Destination))
	if e := os.MkdirAll(filepath.Dir(dest), 0755); e != nil {
		t.Fatal(e)
	}
	original := []byte("an existing user-owned file")
	if e := os.WriteFile(dest, original, 0644); e != nil {
		t.Fatal(e)
	}
	e := f.s.importer.Execute(context.Background(), j, p.Source, p.Size)
	var failure *Error
	if !errors.As(e, &failure) || failure.Code != "destination_exists" {
		t.Fatal("collision accepted", e)
	}
	after, _ := os.ReadFile(dest)
	if string(after) != string(original) {
		t.Fatal("existing library file overwritten")
	}
	var count int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM media_files`).Scan(&count)
	if count != 0 {
		t.Fatal("collision registered as available")
	}
}
func TestMismatchedPartialCopyIsLeftUnchanged(t *testing.T) {
	f := setupWorkflow(t)
	j, p, _ := plannedImport(t, f)
	dir := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(path.Dir(p.Destination)))
	_ = os.MkdirAll(dir, 0755)
	stage := filepath.Join(dir, p.Stage)
	_ = os.WriteFile(stage, []byte("unexpected staging contents"), 0600)
	if e := f.s.importer.phase(context.Background(), j, &p, "planned", "copy"); e != nil {
		t.Fatal(e)
	}
	e := f.s.importer.Execute(context.Background(), j, p.Source, p.Size)
	var failure *Error
	if !errors.As(e, &failure) || failure.Code != "staging_conflict" {
		t.Fatal(e)
	}
	after, _ := os.ReadFile(stage)
	if string(after) != "unexpected staging contents" {
		t.Fatal("foreign partial file altered")
	}
}
func TestSourceChangeInvalidatesJournal(t *testing.T) {
	f := setupWorkflow(t)
	j, p, data := plannedImport(t, f)
	data[0] = 'X'
	if e := os.WriteFile(filepath.Join(f.cfg.DownloadsDir, filepath.FromSlash(p.Source)), data, 0644); e != nil {
		t.Fatal(e)
	}
	if e := f.s.importer.Execute(context.Background(), j, p.Source, p.Size); e == nil {
		t.Fatal("changed source accepted")
	}
}
func TestSourceSymlinkAndOverlappingRootsRejected(t *testing.T) {
	f := setupWorkflow(t)
	j := f.queue(t)
	outside := filepath.Join(t.TempDir(), "outside.mkv")
	if e := os.WriteFile(outside, []byte("outside movie"), 0644); e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(f.cfg.DownloadsDir, "ReelScarey", j.ID)
	_ = os.MkdirAll(dir, 0755)
	if e := os.Symlink(outside, filepath.Join(dir, "movie.mkv")); e == nil {
		owned, e := f.s.claim(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		if e = f.s.importer.Execute(context.Background(), owned, path.Join("ReelScarey", j.ID, "movie.mkv"), 13); e == nil {
			t.Fatal("external symlink imported")
		}
	} else {
		t.Log("symlink creation unavailable on this host; Linux CI exercises this case")
	}
	if _, _, e := roots(f.cfg.DownloadsDir, f.cfg.DownloadsDir, f.cfg.ConfigDir); e == nil {
		t.Fatal("overlapping roots accepted")
	}
}
func TestRealFFprobeValidatesVideoAndRejectsGarbage(t *testing.T) {
	if _, e := exec.LookPath("ffmpeg"); e != nil {
		t.Skip("FFmpeg not available")
	}
	if _, e := exec.LookPath("ffprobe"); e != nil {
		t.Skip("FFprobe not available")
	}
	name := filepath.Join(t.TempDir(), "generated.mp4")
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=10", "-t", "0.5", "-c:v", "mpeg4", "-movflags", "faststart", name)
	hideProbeWindow(cmd)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("generate test video: %v %s", e, out)
	}
	file, e := os.Open(name)
	if e != nil {
		t.Fatal(e)
	}
	result, e := (FFprobe{}).Probe(context.Background(), file)
	file.Close()
	if e != nil || result.Width != 64 || result.Height != 64 || result.Duration <= 0 {
		t.Fatal("valid generated video rejected", result, e)
	}
	garbage := filepath.Join(t.TempDir(), "bad.mkv")
	_ = os.WriteFile(garbage, []byte("not a movie"), 0600)
	file, _ = os.Open(garbage)
	defer file.Close()
	if _, e = (FFprobe{}).Probe(context.Background(), file); e == nil {
		t.Fatal("garbage accepted as video")
	}
}

func TestCommittedCleanupOnlyRemovesMatchingAppLink(t *testing.T) {
	f := setupWorkflow(t)
	j := f.queue(t)
	f.tick(t)
	f.complete(t, j, []byte("test video bytes"))
	f.tick(t)
	var dest, stage string
	if e := f.db.QueryRow(`SELECT destination_path,stage_name FROM import_operations WHERE download_id=?`, j.ID).Scan(&dest, &stage); e != nil {
		t.Fatal(e)
	}
	absolute := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(dest))
	staged := filepath.Join(filepath.Dir(absolute), stage)
	if e := os.Link(absolute, staged); e != nil {
		t.Fatal(e)
	}
	if e := f.s.cleanupCommitted(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(staged); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("redundant staging link was not cleaned")
	}
	if e := os.WriteFile(staged, []byte("a different file"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := f.s.cleanupCommitted(context.Background()); e != nil {
		t.Fatal(e)
	}
	after, _ := os.ReadFile(staged)
	if string(after) != "a different file" {
		t.Fatal("cleanup removed an unknown file")
	}
	if _, e := os.Stat(absolute); e != nil {
		t.Fatal("cleanup removed committed movie")
	}
}
