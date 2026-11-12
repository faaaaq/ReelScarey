package music

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func importFixture(t *testing.T) (*Service, string, string) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	media := t.TempDir()
	downloads := t.TempDir()
	if _, err = db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES(1,?, 'Artist',1,'missing',1,1)`, testMBID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(2,1,?, 'Album',1,'missing',1,1)`, testRGID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO music_tracks(id,album_id,musicbrainz_id,recording_id,disc,number,title,created_at,updated_at) VALUES(3,2,?,?,1,1,'Song',1,1),(4,2,?,?,1,2,'Other',1,1)`, testTrackID, testRecordingID, testTrackID2, testRecordingID2); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, media)
	service.SetDownloadsDir(downloads)
	service.SetProber(FFprobeTagger{})
	return service, media, downloads
}

func generateFLAC(t *testing.T, path, title string, track int) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-c:a", "flac",
		"-metadata", "artist=Artist", "-metadata", "album=Album", "-metadata", "title="+title,
		"-metadata", "track="+string(rune('0'+track)), "-metadata", "disc=1", "-y", path)
	if err := cmd.Run(); err != nil {
		t.Skip("could not generate a FLAC fixture", err)
	}
}

func TestImportFolderCopiesTagsAndRegisters(t *testing.T) {
	service, media, downloads := importFixture(t)
	source := filepath.Join(downloads, "Album")
	generateFLAC(t, filepath.Join(source, "01.flac"), "Song", 1)
	generateFLAC(t, filepath.Join(source, "02.flac"), "Other", 2)
	job, err := service.StartImport(context.Background(), 2, "Album", "Music")
	if err != nil {
		t.Fatal(err)
	}
	job = waitForImport(t, context.Background(), service, job.ID)
	if job.State != "completed" || job.ImportedCount != 2 {
		t.Fatal("import job", job)
	}
	for _, name := range []string{"01 Song.flac", "02 Other.flac"} {
		path := filepath.Join(media, "Music", "Artist", "Album", name)
		if _, err := os.Stat(path); err != nil {
			t.Fatal("imported file missing", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(downloads, "Album", "01.flac")); err != nil {
		t.Fatal("source was modified", err)
	}
	var count int
	if err := service.store.db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE origin='download'`).Scan(&count); err != nil || count != 2 {
		t.Fatal("registered files", count, err)
	}
}

func TestImportMappedFolderUsesExplicitTracks(t *testing.T) {
	service, media, downloads := importFixture(t)
	source := filepath.Join(downloads, "Album")
	generateFLAC(t, filepath.Join(source, "01.flac"), "Wrong One", 9)
	generateFLAC(t, filepath.Join(source, "02.flac"), "Wrong Two", 9)
	job, err := service.StartImportMapped(context.Background(), 2, "Album", "Music", map[int]int64{0: 3, 1: 4})
	if err != nil {
		t.Fatal(err)
	}
	job = waitForImport(t, context.Background(), service, job.ID)
	if job.State != "completed" || job.ImportedCount != 2 {
		t.Fatal("mapped import", job)
	}
	for _, name := range []string{"01 Song.flac", "02 Other.flac"} {
		if _, err := os.Stat(filepath.Join(media, "Music", "Artist", "Album", name)); err != nil {
			t.Fatal("mapped file missing", name, err)
		}
	}
}

func TestImportFolderRequiresACompleteTrackSet(t *testing.T) {
	service, _, downloads := importFixture(t)
	source := filepath.Join(downloads, "Album")
	generateFLAC(t, filepath.Join(source, "01.flac"), "Song", 1)
	if _, err := service.StartImport(context.Background(), 2, "Album", "Music"); err == nil {
		t.Fatal("incomplete album accepted")
	}
}

func TestImportResumesPublishedFileAndKeepsDownloadIdentity(t *testing.T) {
	ctx := context.Background()
	s, mediaDir, downloads := importFixture(t)
	generateFLAC(t, filepath.Join(downloads, "Album", "01.flac"), "Song", 1)
	generateFLAC(t, filepath.Join(downloads, "Album", "02.flac"), "Other", 2)
	job, err := s.StartDownloadImport(ctx, "fixture-download", 2, "Album", "Music", nil)
	if err != nil {
		t.Fatal(err)
	}
	album, err := s.loadImportAlbum(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	media, err := os.OpenRoot(mediaDir)
	if err != nil {
		t.Fatal(err)
	}
	defer media.Close()
	file := job.Files[0]
	p, err := s.prepareMusicImport(ctx, media, album, *trackByID(album.Tracks, *file.TrackID), "Music", file.SourcePath)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.db.Exec(`UPDATE music_import_files SET publication=? WHERE id=?`, string(raw), file.ID); err != nil {
		t.Fatal(err)
	}
	if err := media.Link(p.Stage, p.Destination); err != nil {
		t.Fatal(err)
	}
	// Stop just after publishing, before committing the catalog row. A new
	// service must recognize the link and finish without needing that source.
	if err := os.Remove(filepath.Join(downloads, filepath.FromSlash(file.SourcePath))); err != nil {
		t.Fatal(err)
	}
	reopened := NewService(s.store.db, mediaDir)
	reopened.SetDownloadsDir(downloads)
	if again, err := reopened.StartDownloadImport(ctx, "fixture-download", 2, "Album", "Music", nil); err != nil || again.ID != job.ID {
		t.Fatal("import handoff was not idempotent", again, err)
	}
	if out := waitForImport(t, ctx, reopened, job.ID); out.State != "completed" || out.ImportedCount != 2 {
		t.Fatal("interrupted import did not recover", out)
	}
	var count int
	if err := s.store.db.QueryRow(`SELECT COUNT(*) FROM music_files`).Scan(&count); err != nil || count != 2 {
		t.Fatal("duplicate or missing catalog rows", count, err)
	}
}

func TestImportDeduplicatesIdenticalDestination(t *testing.T) {
	ctx := context.Background()
	service, media, downloads := importFixture(t)
	source := filepath.Join(downloads, "Album")
	generateFLAC(t, filepath.Join(source, "01.flac"), "Song", 1)
	generateFLAC(t, filepath.Join(source, "02.flac"), "Other", 2)
	// Import once to publish the destination files.
	job, err := service.StartImport(ctx, 2, "Album", "Music")
	if err != nil {
		t.Fatal(err)
	}
	job = waitForImport(t, ctx, service, job.ID)
	if job.State != "completed" || job.ImportedCount != 2 {
		t.Fatal("first import job", job)
	}
	// Reset the catalog registrations and re-import the same folder. The
	// destinations already exist with identical content (writeTags is
	// bitexact), so the re-import must succeed without versioning anything
	// aside and without duplicating the downloads.
	if _, err := service.store.db.Exec(`DELETE FROM music_files WHERE album_id=2`); err != nil {
		t.Fatal(err)
	}
	job, err = service.StartImport(ctx, 2, "Album", "Music")
	if err != nil {
		t.Fatal(err)
	}
	job = waitForImport(t, ctx, service, job.ID)
	if job.State != "completed" || job.ImportedCount != 2 {
		t.Fatal("conflicting import job", job)
	}
	albumDir := filepath.Join(media, "Music", "Artist", "Album")
	for _, name := range []string{"01 Song.flac", "02 Other.flac"} {
		if _, err := os.Stat(filepath.Join(albumDir, name)); err != nil {
			t.Fatal("imported file missing", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(albumDir, "01 Song (2).flac")); !os.IsNotExist(err) {
		t.Fatal("identical destination was versioned aside", err)
	}
	if _, err := os.Stat(filepath.Join(source, "01.flac")); err != nil {
		t.Fatal("download source was modified", err)
	}
	var count int
	if err := service.store.db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE album_id=2`).Scan(&count); err != nil || count != 2 {
		t.Fatal("registered files", count, err)
	}
}

func TestImportVersionsAsideDifferingDestination(t *testing.T) {
	ctx := context.Background()
	service, media, downloads := importFixture(t)
	source := filepath.Join(downloads, "Album")
	generateFLAC(t, filepath.Join(source, "01.flac"), "Song", 1)
	generateFLAC(t, filepath.Join(source, "02.flac"), "Other", 2)
	// A different file already occupies the first track's destination. The
	// import supersedes it and preserves the previous content beside it.
	albumDir := filepath.Join(media, "Music", "Artist", "Album")
	if err := os.MkdirAll(albumDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(albumDir, "01 Song.flac"), []byte("occupied"), 0o644); err != nil {
		t.Fatal(err)
	}
	job, err := service.StartImport(ctx, 2, "Album", "Music")
	if err != nil {
		t.Fatal(err)
	}
	job = waitForImport(t, ctx, service, job.ID)
	if job.State != "completed" || job.ImportedCount != 2 {
		t.Fatal("conflicting import job", job)
	}
	destination := filepath.Join(albumDir, "01 Song.flac")
	info, err := os.Stat(destination)
	if err != nil || info.Size() == int64(len("occupied")) {
		t.Fatal("destination was not replaced", info, err)
	}
	if got, err := os.ReadFile(filepath.Join(albumDir, "01 Song (2).flac")); err != nil || string(got) != "occupied" {
		t.Fatal("previous destination content was not set aside", got, err)
	}
	if _, err := os.Stat(filepath.Join(source, "01.flac")); err != nil {
		t.Fatal("download source was modified", err)
	}
	var count int
	if err := service.store.db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE album_id=2`).Scan(&count); err != nil || count != 2 {
		t.Fatal("registered files", count, err)
	}
}

func waitForImport(t *testing.T, ctx context.Context, service *Service, id int64) ImportJob {
	t.Helper()
	if _, err := service.ProcessImport(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		job, err := service.ImportJob(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if job.State != "running" {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatal("import timed out", job)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
