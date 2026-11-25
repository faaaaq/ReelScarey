package music

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// blockingUpgradeSource holds its first search open until released, so a test
// can act while the worker owns a running item.
type blockingUpgradeSource struct {
	fakeUpgradeSource
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (f *blockingUpgradeSource) SearchAudio(ctx context.Context, query string, limit int) ([]AudioCandidate, error) {
	f.once.Do(func() { close(f.started) })
	select {
	case <-f.release:
	case <-ctx.Done():
	}
	return f.fakeUpgradeSource.SearchAudio(ctx, query, limit)
}

func upgradeCode(err error) string {
	var musicErr *Error
	if errors.As(err, &musicErr) {
		return musicErr.Code
	}
	return ""
}

func TestUpgradeJobsRefuseOverlappingOriginals(t *testing.T) {
	service, _ := upgradeFixture(t)
	seedUpgradeTrack(t, service, []byte("original bytes"))
	ctx := context.Background()
	first, err := service.EnqueueUpgradeJob(ctx, UpgradeJobInput{Scope: "track", ScopeID: 1, TargetQuality: "lossless"})
	if err != nil {
		t.Fatal(err)
	}
	// The library scope contains the same original, so it cannot run beside
	// the track job: two workers must never replace one file.
	_, err = service.EnqueueUpgradeJob(ctx, UpgradeJobInput{Scope: "library", TargetQuality: "high"})
	if code := upgradeCode(err); code != "music_upgrade_overlap" {
		t.Fatalf("overlapping enqueue error = %v (code %q), want music_upgrade_overlap", err, code)
	}
	if _, err := service.CancelUpgradeJob(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	second, err := service.EnqueueUpgradeJob(ctx, UpgradeJobInput{Scope: "library", TargetQuality: "high"})
	if err != nil || second.TotalCount != 1 {
		t.Fatalf("library job after the cancel = %+v, %v", second, err)
	}
}

func TestUpgradeCancelStopsRunningJobAndRetryFinishesIt(t *testing.T) {
	service, plain := upgradeFixture(t)
	fileID, relative := seedUpgradeTrack(t, service, []byte("original bytes"))
	plain.bytes = []byte("replacement bytes")
	blocking := &blockingUpgradeSource{
		fakeUpgradeSource: *plain, started: make(chan struct{}), release: make(chan struct{}),
	}
	service.SetAudioSource(blocking)
	ctx := context.Background()
	job, err := service.EnqueueUpgradeJob(ctx, UpgradeJobInput{Scope: "track", ScopeID: 1, TargetQuality: "lossless"})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := service.processUpgrade(ctx)
		done <- err
	}()
	<-blocking.started
	running, err := service.UpgradeJob(ctx, job.ID)
	if err != nil || running.State != "running" || running.CurrentTrack != "Artist — Song" {
		t.Fatalf("running job = %+v, %v", running, err)
	}
	cancelled, err := service.CancelUpgradeJob(ctx, job.ID)
	if err != nil || cancelled.State != "cancelled" {
		t.Fatalf("cancelled job = %+v, %v", cancelled, err)
	}
	close(blocking.release)
	// The search finished after the cancel took the lease away, so the worker
	// must give up quietly instead of publishing or recording an outcome.
	if err := <-done; err != nil {
		t.Fatalf("worker returned %v after a cancel", err)
	}

	var role, itemState string
	var primaries, active int
	if err := service.store.db.QueryRow(`SELECT variant_role FROM music_files WHERE id=?`, fileID).Scan(&role); err != nil || role != "primary" {
		t.Fatalf("original role after a cancelled run = %q, %v", role, err)
	}
	if err := service.store.db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE variant_role='primary'`).Scan(&primaries); err != nil || primaries != 1 {
		t.Fatalf("primary count = %d, %v; want 1", primaries, err)
	}
	if err := service.store.db.QueryRow(`SELECT COUNT(*) FROM music_upgrade_active`).Scan(&active); err != nil || active != 0 {
		t.Fatalf("active guards = %d, %v; want 0", active, err)
	}
	if err := service.store.db.QueryRow(`SELECT state FROM music_upgrade_items WHERE job_id=?`, job.ID).Scan(&itemState); err != nil || itemState != "pending" {
		t.Fatalf("item state after a cancel = %q, %v", itemState, err)
	}
	entries, err := os.ReadDir(filepath.Join(service.mediaDir, "Artist"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("files beside the original after a cancelled run = %v, %v", entries, err)
	}
	if _, err := service.CancelUpgradeJob(ctx, job.ID); upgradeCode(err) != "music_upgrade_state" {
		t.Fatalf("second cancel error = %v, want music_upgrade_state", err)
	}
	if _, err := service.RetryUpgradeJob(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("retrying a missing job = %v, want ErrNotFound", err)
	}

	service.SetAudioSource(plain)
	retried, err := service.RetryUpgradeJob(ctx, job.ID)
	if err != nil || retried.State != "queued" || retried.TotalCount != 1 {
		t.Fatalf("retried job = %+v, %v", retried, err)
	}
	if _, err := service.processUpgrade(ctx); err != nil {
		t.Fatal(err)
	}
	finished, err := service.UpgradeJob(ctx, job.ID)
	if err != nil || finished.State != "completed" || finished.UpgradedCount != 1 || finished.ProcessedCount != 1 || finished.Error != "" {
		t.Fatalf("finished job = %+v, %v", finished, err)
	}
	if err := service.store.db.QueryRow(`SELECT variant_role FROM music_files WHERE id=?`, fileID).Scan(&role); err != nil || role != "archive" {
		t.Fatalf("original role after the retry = %q, %v", role, err)
	}
	if actual, err := os.ReadFile(filepath.Join(service.mediaDir, relative)); err != nil || string(actual) != "original bytes" {
		t.Fatal("original bytes changed", err, string(actual))
	}
}

func TestUpgradeFailureRetryPolicy(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		retryable bool
	}{
		{"provider search or download", &Error{Code: "music_upgrade_source", Message: "unavailable"}, true},
		{"disk or database", errors.New("write: no space left on device"), true},
		{"changed original", &Error{Code: "music_upgrade_changed", Message: "changed"}, false},
		{"unmet quality", &Error{Code: "music_upgrade_quality", Message: "lower"}, false},
		{"downloads root not configured", &Error{Code: "music_import_source", Message: "unset"}, false},
		{"invalid input", ErrInvalidInput, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := upgradeFailureRetryable(test.err); got != test.retryable {
				t.Fatalf("retryable = %v, want %v", got, test.retryable)
			}
		})
	}
}

func TestUpgradeJobMessageNamesTheFailure(t *testing.T) {
	service, _ := upgradeFixture(t)
	seedUpgradeTrack(t, service, []byte("original bytes"))
	// The only copy found is no better than the original, so the single item
	// is skipped: that is a reason, not a problem with the job.
	service.prober = upgradeFakeProber{tags: AudioTags{
		Version: audioTagVersion, Format: "mp3", DurationMS: 180000, Bitrate: 128000,
		SampleRate: 44100, Channels: 2,
	}}
	job, err := service.EnqueueUpgradeJob(context.Background(), UpgradeJobInput{
		Scope: "track", ScopeID: 1, TargetQuality: "lossless",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.processUpgrade(context.Background()); err != nil {
		t.Fatal(err)
	}
	finished, err := service.UpgradeJob(context.Background(), job.ID)
	if err != nil || finished.State != "completed" || finished.SkippedCount != 1 || finished.Error != "" {
		t.Fatalf("skipped job = %+v, %v", finished, err)
	}
	var attempts, active int
	if err := service.store.db.QueryRow(`SELECT attempts FROM music_upgrade_items WHERE job_id=?`, job.ID).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatalf("attempts = %d, %v", attempts, err)
	}
	if err := service.store.db.QueryRow(`SELECT COUNT(*) FROM music_upgrade_active`).Scan(&active); err != nil || active != 0 {
		t.Fatalf("active guards after a skipped item = %d, %v; want 0", active, err)
	}
}
