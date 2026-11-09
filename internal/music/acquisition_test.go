package music

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

type fakeAudioSource struct {
	failDownload bool
}

func (f fakeAudioSource) SearchAudio(context.Context, string, int) ([]AudioCandidate, error) {
	return []AudioCandidate{{ID: "abcdefghijk", Title: "Song", DurationSeconds: 200}}, nil
}

func (f fakeAudioSource) DownloadAudio(_ context.Context, _ string, directory, _ string) (AudioFile, error) {
	if f.failDownload {
		return AudioFile{}, errors.New("download failed")
	}
	path := filepath.Join(directory, "track.mp3")
	if err := os.WriteFile(path, []byte("audio"), 0o600); err != nil {
		return AudioFile{}, err
	}
	return AudioFile{Path: path, Format: "mp3"}, nil
}

func (f fakeAudioSource) InspectAudio(context.Context, string) (AudioInspection, error) {
	return AudioInspection{}, errors.New("not used")
}

func TestAcquisitionQueuesDownloadAndCatalogImport(t *testing.T) {
	ctx := context.Background()
	downloads := t.TempDir()
	media := t.TempDir()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := NewService(db, media)
	service.SetDownloadsDir(downloads)
	service.SetAudioSource(fakeAudioSource{})

	job, err := service.StartAcquisition(ctx, "", "top_track", "Song", "Music", "source", []AcquisitionTrackInput{{
		Title: "Song", Artist: "Artist", Album: "Album", AlbumArtist: "Artist",
		TrackNumber: 1, DiscNumber: 1, DurationMS: 200000, SearchQuery: "Artist Song",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if job.State != "queued" || len(job.Tracks) != 1 {
		t.Fatal("job", job)
	}
	worked, err := service.ProcessAcquisition(ctx)
	if err != nil || !worked {
		t.Fatal("process", worked, err)
	}
	job, err = service.Acquisition(ctx, job.ID)
	if err != nil || job.Tracks[0].State != "importing" || job.Tracks[0].ImportJobID == nil {
		t.Fatal("import queue", job, err)
	}
	var albums, tracks, imports int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM music_albums),(SELECT COUNT(*) FROM music_tracks),(SELECT COUNT(*) FROM music_import_jobs)`).
		Scan(&albums, &tracks, &imports); err != nil {
		t.Fatal(err)
	}
	if albums != 1 || tracks != 1 || imports != 1 {
		t.Fatal("catalog/import rows", albums, tracks, imports)
	}

	failing := NewService(db, media)
	failing.SetDownloadsDir(downloads)
	failing.SetAudioSource(fakeAudioSource{failDownload: true})
	failedJob, err := failing.StartAcquisition(ctx, "", "top_track", "Other", "Music", "source", []AcquisitionTrackInput{{
		Title: "Other", Artist: "Artist", Album: "Album", TrackNumber: 2, DiscNumber: 1, SearchQuery: "Artist Other",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := failing.ProcessAcquisition(ctx); err != nil || !worked {
		t.Fatal("failed process", worked, err)
	}
	failedJob, err = failing.Acquisition(ctx, failedJob.ID)
	if err != nil || failedJob.Tracks[0].State != "failed" || failedJob.State != "failed" {
		t.Fatal("failed acquisition", failedJob, err)
	}
}
