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

// perTitleAudioSource fails only for tracks whose search resolves to the
// "Bad" candidate, so one test can exercise success, failure and partial
// completion inside a single job.
type perTitleAudioSource struct{}

func (perTitleAudioSource) SearchAudio(_ context.Context, query string, _ int) ([]AudioCandidate, error) {
	if strings.Contains(strings.ToLower(query), "bad") {
		return []AudioCandidate{{ID: "badtrack000", Title: "Bad", DurationSeconds: 200}}, nil
	}
	return []AudioCandidate{{ID: "goodtrack0", Title: "Good", DurationSeconds: 200}}, nil
}

func (perTitleAudioSource) DownloadAudio(_ context.Context, id, directory, _ string) (AudioFile, error) {
	if id == "badtrack000" {
		return AudioFile{}, errors.New("the download did not finish")
	}
	path := filepath.Join(directory, "track.mp3")
	if err := os.WriteFile(path, []byte("audio"), 0o600); err != nil {
		return AudioFile{}, err
	}
	return AudioFile{Path: path, Format: "mp3"}, nil
}

func (perTitleAudioSource) InspectAudio(context.Context, string) (AudioInspection, error) {
	return AudioInspection{}, errors.New("not used")
}

func startAttentionJob(t *testing.T, service *Service, source AudioSource, titles ...string) AcquisitionJob {
	t.Helper()
	service.SetAudioSource(source)
	inputs := []AcquisitionTrackInput{}
	for i, title := range titles {
		inputs = append(inputs, AcquisitionTrackInput{
			Title: title, Artist: "Artist", Album: "Album", AlbumArtist: "Artist",
			TrackNumber: i + 1, DiscNumber: 1, DurationMS: 200000, SearchQuery: "Artist " + title,
		})
	}
	job, err := service.StartAcquisition(context.Background(), "", "top_track", "Attention", "Music", "source", inputs)
	if err != nil {
		t.Fatal(err)
	}
	return job
}

// drain processes queued acquisition tracks until the worker reports no more
// immediate work.
func drain(t *testing.T, service *Service) {
	t.Helper()
	for {
		worked, err := service.ProcessAcquisition(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			return
		}
	}
}

func TestAcquisitionIssuesCountRetryAndAcknowledge(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := NewService(db, t.TempDir())
	service.SetDownloadsDir(t.TempDir())

	// 1. Two active failures → an accurate issue count across all jobs.
	job := startAttentionJob(t, service, fakeAudioSource{failDownload: true}, "First", "Second")
	drain(t, service)
	got, err := service.Acquisition(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "failed" || got.Error != "No tracks completed." {
		t.Fatal("failed job state", got.State, got.Error)
	}
	list, err := service.ListAcquisitions(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if list.Issues != 1 || list.Active != 0 || list.Total != 1 {
		t.Fatal("issue count after failures", list.Issues, list.Active, list.Total)
	}

	// 2. Acknowledging a historical failure clears the warning while the
	// diagnostic record stays.
	if _, err := service.AcknowledgeAcquisition(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	list, err = service.ListAcquisitions(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if list.Issues != 0 {
		t.Fatal("acknowledged job still counts", list.Issues)
	}
	got, err = service.Acquisition(ctx, job.ID)
	if err != nil || got.AcknowledgedAt == 0 || got.Error != "No tracks completed." {
		t.Fatal("acknowledged job history", got, err)
	}

	// 3. Acknowledgment survives a reopen (restart).
	reopened := NewService(db, t.TempDir())
	reopened.SetDownloadsDir(t.TempDir())
	list, err = reopened.ListAcquisitions(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if list.Issues != 0 {
		t.Fatal("acknowledgment lost across reopen", list.Issues)
	}

	// 4. A new failure re-surfaces the warning; the acknowledged job stays cleared.
	fresh := startAttentionJob(t, service, fakeAudioSource{failDownload: true}, "Fresh")
	drain(t, service)
	list, err = service.ListAcquisitions(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if list.Issues != 1 || list.Total != 2 {
		t.Fatal("new failure after acknowledgment", list.Issues, list.Total)
	}
	freshJob, err := service.Acquisition(ctx, fresh.ID)
	if err != nil || freshJob.AcknowledgedAt != 0 {
		t.Fatal("fresh job must start unacknowledged", freshJob, err)
	}

	// 5. Retry requeues the failed tracks with the existing worker; the
	// historical job stays acknowledged.
	if _, err := service.RetryAcquisitionTracks(ctx, fresh.ID); err != nil {
		t.Fatal(err)
	}
	retried, err := service.Acquisition(ctx, fresh.ID)
	if err != nil || retried.State != "running" || retried.Error != "" {
		t.Fatal("retry state", retried, err)
	}
	for _, track := range retried.Tracks {
		if track.State != "queued" || track.Error != "" {
			t.Fatal("retried track", track)
		}
	}
	list, err = service.ListAcquisitions(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if list.Active != 1 || list.Issues != 0 {
		t.Fatal("retried job counts", list.Active, list.Issues)
	}

	// 6. Retry on an active job and on a missing job are rejected.
	if _, err := service.RetryAcquisitionTracks(ctx, fresh.ID); err == nil {
		t.Fatal("retry on running job accepted")
	}
	if _, err := service.RetryAcquisitionTracks(ctx, 999999); err == nil {
		t.Fatal("retry on missing job accepted")
	}

	// 7. Acknowledge is rejected for active jobs and missing jobs.
	if _, err := service.AcknowledgeAcquisition(ctx, fresh.ID); err == nil {
		t.Fatal("acknowledge on running job accepted")
	}
	if _, err := service.AcknowledgeAcquisition(ctx, 999999); err == nil {
		t.Fatal("acknowledge on missing job accepted")
	}
}

func TestAcquisitionPartialCompletionNeedsAttention(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := NewService(db, t.TempDir())
	service.SetDownloadsDir(t.TempDir())

	// One track downloads, one fails: the job must not report plain success.
	job := startAttentionJob(t, service, perTitleAudioSource{}, "Good", "Bad")
	drain(t, service)
	got, err := service.Acquisition(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, track := range got.Tracks {
		states[track.Title] = track.State
	}
	if states["Good"] != "importing" || states["Bad"] != "failed" {
		t.Fatal("mixed track states", states, got.Tracks)
	}

	// Simulate the finished import of the good track so the job reaches its
	// final mixed state.
	if _, err := db.ExecContext(ctx, `UPDATE music_acquisition_tracks SET state='completed' WHERE job_id=? AND title='Good'`, job.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.refreshAcquisitionJob(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	got, err = service.Acquisition(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "failed" {
		t.Fatal("partial completion must not report plain completed", got.State)
	}
	if !strings.Contains(got.Error, "1 of 2 tracks completed") {
		t.Fatal("partial message", got.Error)
	}
	list, err := service.ListAcquisitions(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if list.Issues != 1 {
		t.Fatal("partial completion not counted", list.Issues)
	}

	// Retrying just the failed track leaves the completed one alone and the
	// worker requeues only the failure.
	retried, err := service.RetryAcquisitionTracks(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	retriedStates := map[string]string{}
	for _, track := range retried.Tracks {
		retriedStates[track.Title] = track.State
	}
	if retriedStates["Good"] != "completed" || retriedStates["Bad"] != "queued" {
		t.Fatal("selective retry", retriedStates)
	}
}
