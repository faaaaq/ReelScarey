package transfer

import (
	"context"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/music"
)

type fakeMusicImporter struct {
	startCalls  int
	importCalls int
	albumID     int64
	folder      string
	root        string
	importState string
	importError string
}

func (f *fakeMusicImporter) StartImport(_ context.Context, albumID int64, folder, root string) (music.ImportJob, error) {
	f.startCalls++
	f.albumID, f.folder, f.root = albumID, folder, root
	state := f.importState
	if state == "" {
		state = "running"
	}
	return music.ImportJob{ID: 1, AlbumID: albumID, Root: root, SourcePath: folder, State: state}, nil
}

func (f *fakeMusicImporter) ImportJob(_ context.Context, id int64) (music.ImportJob, error) {
	f.importCalls++
	state := f.importState
	if state == "" {
		state = "running"
	}
	return music.ImportJob{ID: id, AlbumID: f.albumID, State: state, Error: f.importError}, nil
}

func musicTestCompletedTorrent(t *testing.T, f *workflowFixture, job MusicJob) {
	t.Helper()
	f.client.torrent = &Torrent{
		Hash:       testHash,
		Tags:       "reelscarey-" + job.ID,
		SavePath:   "/downloads/ReelScarey/" + job.ID,
		State:      "uploading",
		Progress:   1,
		AmountLeft: 0,
		Size:       200,
		Completed:  200,
	}
	f.client.files = []TorrentFile{{Index: 0, Name: "01 - Track.flac", Size: 200, Progress: 1, Priority: 1}}
}

func TestMusicWorkerTracksCompletionAndPollsImport(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	job := musicTestQueue(t, f)

	// While the client is still downloading, the worker only mirrors progress.
	f.client.torrent = &Torrent{
		Hash:       testHash,
		Tags:       "reelscarey-" + job.ID,
		SavePath:   "/downloads/ReelScarey/" + job.ID,
		State:      "downloading",
		Progress:   0.5,
		Size:       200,
		Completed:  100,
		AmountLeft: 100,
	}
	if processed, e := musicTestProcess(t, f); e != nil || !processed {
		t.Fatal("the music worker did not claim the queued torrent", processed, e)
	}
	got := musicTestState(t, f, job.ID)
	if got.State != "downloading" || got.Progress != 0.5 || got.TotalBytes != 200 || got.DownloadedBytes != 100 {
		t.Fatal("music download progress was not mirrored", got)
	}
	if _, e := f.s.MusicJobs(ctx, 1); e != nil {
		t.Fatal(e)
	}

	// A completed torrent hands its dedicated folder to the music importer and
	// moves to importing; the import itself is polled, never spawned here.
	imp := &fakeMusicImporter{}
	f.s.SetMusicImporter(imp)
	musicTestCompletedTorrent(t, f, job)
	if processed, e := musicTestProcess(t, f); e != nil || !processed {
		t.Fatal("the completed music torrent was not processed", processed, e)
	}
	if imp.startCalls != 1 || imp.albumID != job.AlbumID || imp.root != "Music" || imp.folder != "ReelScarey/"+job.ID {
		t.Fatal("the completed folder was not handed to the music importer", imp)
	}
	got = musicTestState(t, f, job.ID)
	if got.State != "importing" || got.ImportJobID == nil || *got.ImportJobID != 1 || got.Error != "" {
		t.Fatal("the music download did not enter importing", got)
	}

	imp.importState = "running"
	if processed, e := musicTestProcess(t, f); e != nil || !processed {
		t.Fatal("a running music import was not polled", processed, e)
	}
	if got = musicTestState(t, f, job.ID); got.State != "importing" {
		t.Fatal("a running music import left the importing state", got)
	}

	imp.importState = "completed"
	if processed, e := musicTestProcess(t, f); e != nil || !processed {
		t.Fatal("a completed music import was not reconciled", processed, e)
	}
	got = musicTestState(t, f, job.ID)
	if got.State != "completed" || got.Progress != 1 || got.Error != "" {
		t.Fatal("the music download was not completed", got)
	}
	// Completed rows are not claimed again.
	if processed, e := musicTestProcess(t, f); e != nil || processed {
		t.Fatal("a completed music download was claimed", processed, e)
	}
}

func TestMusicWorkerBlocksFailedImportAndRetriesIt(t *testing.T) {
	f := setupWorkflow(t)
	job := musicTestQueue(t, f)
	imp := &fakeMusicImporter{}
	f.s.SetMusicImporter(imp)
	musicTestCompletedTorrent(t, f, job)
	if _, e := musicTestProcess(t, f); e != nil {
		t.Fatal(e)
	}
	if got := musicTestState(t, f, job.ID); got.State != "importing" {
		t.Fatal("the music download did not enter importing", got)
	}

	imp.importState = "failed"
	imp.importError = "No catalog track matches 01 - Track.flac."
	if _, e := musicTestProcess(t, f); e != nil {
		t.Fatal(e)
	}
	got := musicTestState(t, f, job.ID)
	if got.State != "blocked" || !strings.Contains(got.Error, "No catalog track matches") {
		t.Fatal("a failed music import was not blocked for review", got)
	}
	// A blocked import is never claimed again until the owner retries it.
	if processed, e := musicTestProcess(t, f); e != nil || processed {
		t.Fatal("a blocked music import was claimed", processed, e)
	}

	if e := f.s.RetryMusic(context.Background(), job.ID); e != nil {
		t.Fatal(e)
	}
	if got = musicTestState(t, f, job.ID); got.State != "importing" || got.Attempts != 0 {
		t.Fatal("retrying a failed import did not resume polling it", got)
	}
	imp.importState = "completed"
	imp.importError = ""
	if _, e := musicTestProcess(t, f); e != nil {
		t.Fatal(e)
	}
	if got = musicTestState(t, f, job.ID); got.State != "completed" {
		t.Fatal("the retried music import did not complete", got)
	}
}

func TestMusicWorkerNeverResubmitsAnUncertainTorrent(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	job := musicTestQueue(t, f)
	if f.client.adds != 1 {
		t.Fatal(f.client.adds)
	}
	// The client lost the submission: the job is not visible and is already
	// past the reconciliation grace period.
	f.client.torrent = nil
	if _, e := f.db.Exec(`UPDATE music_downloads SET created_at=created_at-200 WHERE id=?`, job.ID); e != nil {
		t.Fatal(e)
	}
	if processed, e := musicTestProcess(t, f); e != nil || !processed {
		t.Fatal("the uncertain music torrent was not reconciled", processed, e)
	}
	got := musicTestState(t, f, job.ID)
	if got.State != "blocked" || got.Error == "" {
		t.Fatal("an uncertain music submission was not blocked for review", got)
	}
	if f.client.adds != 1 {
		t.Fatal("an uncertain music submission was resubmitted", f.client.adds)
	}
	// Even an explicit retry only reconciles; it never calls Add again.
	if e := f.s.RetryMusic(ctx, job.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := musicTestProcess(t, f); e != nil {
		t.Fatal(e)
	}
	if got = musicTestState(t, f, job.ID); got.State != "blocked" {
		t.Fatal("retrying an uncertain music submission did not return it to review", got)
	}
	if f.client.adds != 1 {
		t.Fatal("retrying an uncertain music submission resubmitted it", f.client.adds)
	}
}

func TestMusicWorkerRefusesAChangedClientSnapshot(t *testing.T) {
	f := setupWorkflow(t)
	job := musicTestQueue(t, f)
	if _, e := f.db.Exec(`UPDATE music_downloads SET client_download_root='/other-downloads' WHERE id=?`, job.ID); e != nil {
		t.Fatal(e)
	}
	musicTestCompletedTorrent(t, f, job)
	if _, e := musicTestProcess(t, f); e != nil {
		t.Fatal(e)
	}
	got := musicTestState(t, f, job.ID)
	if got.State != "blocked" || !strings.Contains(got.Error, "original download client") {
		t.Fatal("a changed music client snapshot was not blocked", got)
	}
}
