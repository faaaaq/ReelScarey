package music

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

type routingAudioSource struct {
	name          string
	id            string
	searchErr     error
	searchCalls   int
	downloadCalls int
	candidateIDs  []string
}

func (f *routingAudioSource) AudioSourceName() string { return f.name }
func (f *routingAudioSource) Owns(id string) bool {
	if id == f.id {
		return true
	}
	for _, candidateID := range f.candidateIDs {
		if id == candidateID {
			return true
		}
	}
	return false
}
func (f *routingAudioSource) SearchAudio(context.Context, string, int) ([]AudioCandidate, error) {
	f.searchCalls++
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	ids := f.candidateIDs
	if len(ids) == 0 {
		ids = []string{f.id}
	}
	out := make([]AudioCandidate, 0, len(ids))
	for _, id := range ids {
		out = append(out, AudioCandidate{ID: id, Title: "Artist - Song (Official Audio)", Channel: "Artist", DurationSeconds: 200})
	}
	return out, nil
}
func (f *routingAudioSource) DownloadAudio(_ context.Context, _ string, directory, _ string) (AudioFile, error) {
	f.downloadCalls++
	path := filepath.Join(directory, strings.ToLower(f.name)+".audio")
	if err := os.WriteFile(path, []byte("offline test audio"), 0o600); err != nil {
		return AudioFile{}, err
	}
	return AudioFile{Path: path, Format: "flac"}, nil
}
func (f *routingAudioSource) InspectAudio(context.Context, string) (AudioInspection, error) {
	return AudioInspection{}, errors.New("not used")
}

func sourceSelectionFixture(t *testing.T) (*Service, *routingAudioSource, *routingAudioSource) {
	t.Helper()
	db, err := database.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	service := NewService(db, t.TempDir())
	service.SetDownloadsDir(t.TempDir())
	tidal := &routingAudioSource{name: "TIDAL", id: "https://tidal.com/browse/track/123"}
	youtube := &routingAudioSource{name: "YouTube", id: "abcdefghijk"}
	service.SetAudioSource(youtube)
	service.SetNamedAudioSource("tidal", tidal)
	service.SetNamedAudioSource("youtube", youtube)
	return service, tidal, youtube
}

func sourceSelectionInput() []AcquisitionTrackInput {
	return []AcquisitionTrackInput{{
		Title: "Song", Artist: "Artist", Album: "Album", AlbumArtist: "Artist",
		TrackNumber: 1, DiscNumber: 1, DurationMS: 200000, SearchQuery: "Artist Song",
	}}
}

func TestAcquisitionHonorsExplicitSourceWithoutCallingFallback(t *testing.T) {
	service, tidal, youtube := sourceSelectionFixture(t)
	job, err := service.StartAcquisition(context.Background(), "", "top_track", "Song", "Music", "source", sourceSelectionInput(), "tidal")
	if err != nil {
		t.Fatal(err)
	}
	if job.AudioSource != "tidal" {
		t.Fatalf("source was not persisted on the response: %+v", job)
	}
	worked, err := service.ProcessAcquisition(context.Background())
	if err != nil || !worked {
		t.Fatalf("process: worked=%v err=%v", worked, err)
	}
	if tidal.searchCalls != 1 || tidal.downloadCalls != 1 || youtube.searchCalls != 0 || youtube.downloadCalls != 0 {
		t.Fatalf("explicit source was bypassed: tidal=%+v youtube=%+v", tidal, youtube)
	}
	stored, err := service.Acquisition(context.Background(), job.ID)
	if err != nil || stored.AudioSource != "tidal" || stored.Tracks[0].State != "importing" {
		t.Fatalf("stored acquisition: %+v err=%v", stored, err)
	}
}

func TestAcquisitionSourceAndProviderErrorSurviveReloadAndRetry(t *testing.T) {
	service, tidal, youtube := sourceSelectionFixture(t)
	tidal.searchErr = errors.New("No valid TIDAL session.")
	job, err := service.StartAcquisition(context.Background(), "", "top_track", "Song", "Music", "source", sourceSelectionInput(), "tidal")
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := service.ProcessAcquisition(context.Background()); err != nil || !worked {
		t.Fatalf("process: worked=%v err=%v", worked, err)
	}
	failed, err := service.Acquisition(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	message := failed.Tracks[0].Error
	if !strings.Contains(message, "TIDAL search failed") || !strings.Contains(message, "No valid TIDAL session") || !strings.Contains(message, "Settings > Music") || strings.Contains(message, "YouTube search failed") {
		t.Fatalf("provider error was hidden or mislabeled: %q", message)
	}

	// Simulate a process restart: only the durable job row carries the choice.
	reloaded := NewService(service.store.db, service.mediaDir)
	reloaded.SetDownloadsDir(service.downloadsDir)
	newTidal := &routingAudioSource{name: "TIDAL", id: tidal.id}
	newYouTube := &routingAudioSource{name: "YouTube", id: youtube.id}
	reloaded.SetAudioSource(newYouTube)
	reloaded.SetNamedAudioSource("tidal", newTidal)
	reloaded.SetNamedAudioSource("youtube", newYouTube)
	if _, err = reloaded.RetryAcquisitionTracks(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	if worked, err := reloaded.ProcessAcquisition(context.Background()); err != nil || !worked {
		t.Fatalf("retry process: worked=%v err=%v", worked, err)
	}
	if newTidal.searchCalls != 1 || newTidal.downloadCalls != 1 || newYouTube.searchCalls != 0 || newYouTube.downloadCalls != 0 {
		t.Fatalf("retry did not honor persisted source: tidal=%+v youtube=%+v", newTidal, newYouTube)
	}
}

func TestReviewedCandidateRetryUsesExactSourceWithoutSearchingAgain(t *testing.T) {
	service, tidal, youtube := sourceSelectionFixture(t)
	tidal.candidateIDs = []string{"https://tidal.com/browse/track/123", "https://tidal.com/browse/track/456"}
	job, err := service.StartAcquisition(context.Background(), "", "album", "Song", "Music", "source", sourceSelectionInput(), "tidal")
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := service.ProcessAcquisition(context.Background()); err != nil || !worked {
		t.Fatalf("ambiguous process: worked=%v err=%v", worked, err)
	}
	failed, err := service.Acquisition(context.Background(), job.ID)
	if err != nil || failed.Tracks[0].State != "failed" || !strings.Contains(failed.Tracks[0].Error, "Multiple candidates") {
		t.Fatalf("expected an ambiguous review failure: job=%+v err=%v", failed, err)
	}
	trackID := failed.Tracks[0].ID
	if _, err = service.RetryAcquisitionTrack(context.Background(), job.ID, trackID, tidal.candidateIDs[1], true); err != nil {
		t.Fatal(err)
	}
	if worked, err := service.ProcessAcquisition(context.Background()); err != nil || !worked {
		t.Fatalf("reviewed retry process: worked=%v err=%v", worked, err)
	}
	if tidal.searchCalls != 1 || tidal.downloadCalls != 1 || youtube.searchCalls != 0 || youtube.downloadCalls != 0 {
		t.Fatalf("reviewed candidate was not used directly: tidal=%+v youtube=%+v", tidal, youtube)
	}
	completed, err := service.Acquisition(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	track := completed.Tracks[0]
	if track.SelectedSourceID != tidal.candidateIDs[1] || track.State != "importing" || len(track.Attempts) != 1 || track.Attempts[0].SourceID != tidal.candidateIDs[1] {
		t.Fatalf("reviewed candidate was not journaled: %+v", track)
	}
}

func TestAcquisitionUsesSavedDefaultAndRejectsUnavailableSource(t *testing.T) {
	service, _, youtube := sourceSelectionFixture(t)
	prefs, err := service.GetPreferences(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	prefs.DownloadSource = "youtube"
	prefs.TidalQuality = "LOSSLESS"
	prefs, err = service.SavePreferences(context.Background(), prefs)
	if err != nil {
		t.Fatal(err)
	}
	if prefs.DownloadSource != "youtube" || prefs.TidalQuality != "LOSSLESS" {
		t.Fatalf("source settings were not saved: %+v", prefs)
	}
	job, err := service.StartAcquisition(context.Background(), "", "top_track", "Song", "Music", "source", sourceSelectionInput())
	if err != nil || job.AudioSource != "youtube" {
		t.Fatalf("saved default was not applied: job=%+v err=%v", job, err)
	}
	if worked, err := service.ProcessAcquisition(context.Background()); err != nil || !worked {
		t.Fatalf("process: worked=%v err=%v", worked, err)
	}
	if youtube.searchCalls != 1 {
		t.Fatalf("saved default did not reach the worker: %+v", youtube)
	}
	automaticJob, err := service.StartAcquisition(context.Background(), "", "top_track", "Automatic override", "Music", "source", sourceSelectionInput(), "automatic")
	if err != nil || automaticJob.AudioSource != "" {
		t.Fatalf("explicit automatic did not override the provider-only default: job=%+v err=%v", automaticJob, err)
	}

	unavailable := NewService(service.store.db, t.TempDir())
	unavailable.SetAudioSource(&routingAudioSource{name: "YouTube", id: "abcdefghijk"})
	if _, err := unavailable.StartAcquisition(context.Background(), "", "top_track", "Other", "Music", "source", sourceSelectionInput(), "tidal"); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("unavailable explicit source was accepted: %v", err)
	}
}
