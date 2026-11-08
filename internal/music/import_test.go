package music

import (
	"context"
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

func waitForImport(t *testing.T, ctx context.Context, service *Service, id int64) ImportJob {
	t.Helper()
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
