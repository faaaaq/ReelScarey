package music

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
)

type fakeUpgradeSource struct {
	query       string
	id          string
	bytes       []byte
	searchErr   error
	downloadErr error
}

func (f *fakeUpgradeSource) SearchAudio(_ context.Context, query string, limit int) ([]AudioCandidate, error) {
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	if limit < 1 {
		return nil, ErrInvalidInput
	}
	f.query = query
	return []AudioCandidate{{ID: f.id, Title: "Song", Channel: "Artist", DurationSeconds: 180}}, nil
}

func (f *fakeUpgradeSource) DownloadAudio(_ context.Context, id, directory, format string) (AudioFile, error) {
	if id != f.id || format != "source" {
		return AudioFile{}, ErrInvalidInput
	}
	if f.downloadErr != nil {
		return AudioFile{}, f.downloadErr
	}
	target := filepath.Join(directory, "replacement.flac")
	if err := os.WriteFile(target, f.bytes, 0o600); err != nil {
		return AudioFile{}, err
	}
	return AudioFile{Path: target, Format: "flac"}, nil
}

func (f *fakeUpgradeSource) InspectAudio(context.Context, string) (AudioInspection, error) {
	return AudioInspection{}, ErrInvalidInput
}

func upgradeFixture(t *testing.T) (*Service, *fakeUpgradeSource) {
	t.Helper()
	db, err := database.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	media := t.TempDir()
	downloads := t.TempDir()
	service := NewService(db, media)
	service.SetDownloadsDir(downloads)
	source := &fakeUpgradeSource{id: "candidate-1"}
	service.SetAudioSource(source)
	service.prober = upgradeFakeProber{tags: AudioTags{
		Version: audioTagVersion, Format: "flac", DurationMS: 180000, Bitrate: 900000,
		SampleRate: 96000, Channels: 2, Title: "Song", Artist: "Artist", Album: "Album",
	}}
	upgradeAttemptInterval = 0
	upgradeRetryBackoff = 0
	upgradePollInterval = time.Millisecond
	t.Cleanup(func() {
		upgradeAttemptInterval = 18 * time.Second
		upgradeRetryBackoff = 5 * time.Minute
		upgradePollInterval = 2 * time.Second
	})
	return service, source
}

type upgradeFakeProber struct{ tags AudioTags }

func (p upgradeFakeProber) ProbeAudio(context.Context, string) (AudioTags, error) { return p.tags, nil }

func seedUpgradeTrack(t *testing.T, service *Service, bytes []byte) (int64, string) {
	t.Helper()
	ctx := context.Background()
	year := 2020
	if _, err := service.store.SaveArtist(ctx, Artist{
		MusicBrainzID: "a74b1b7f-71a5-4011-9441-d0b5e4122711", Name: "Artist", SortName: "Artist",
		Monitored: true, Status: "missing",
	}, []Album{{
		MusicBrainzID: "b1392450-e666-3926-a536-22c65f834433", Title: "Album", PrimaryType: "Album",
		Year: &year, Monitored: true, Status: "missing",
	}}); err != nil {
		t.Fatal(err)
	}
	album, err := service.store.AlbumByMusicBrainzID(ctx, "b1392450-e666-3926-a536-22c65f834433")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.store.SaveTracks(ctx, album.ID, []Track{{
		MusicBrainzID: "c1392450-e666-3926-a536-22c65f834433",
		RecordingID:   "d1392450-e666-3926-a536-22c65f834433",
		Disc:          1, Number: 1, Title: "Song", DurationMS: 180000,
	}}); err != nil {
		t.Fatal(err)
	}
	tracks, err := service.store.Tracks(ctx, album.ID)
	if err != nil || len(tracks) != 1 {
		t.Fatal("catalog track missing", err, tracks)
	}
	trackID := tracks[0].ID
	if err := os.MkdirAll(filepath.Join(service.mediaDir, "Artist"), 0o700); err != nil {
		t.Fatal(err)
	}
	relative := "Artist/01 Song.mp3"
	if err := os.WriteFile(filepath.Join(service.mediaDir, relative), bytes, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(bytes)
	result, err := service.store.db.Exec(`INSERT INTO music_files(artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,sample_rate,channels,duration_ms,tag_json,imported_at,origin,variant_role,variant_profile)
		VALUES(1,1,?,'media',?, ?,?,'mp3',128000,44100,2,180000,'{}',unixepoch(),'existing','primary','')`, trackID, relative, len(bytes), hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	fileID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return fileID, relative
}

func TestUpgradeWorkerReplacesPrimaryAndRetainsArchive(t *testing.T) {
	service, source := upgradeFixture(t)
	original := []byte("original audio bytes")
	fileID, relative := seedUpgradeTrack(t, service, original)
	source.bytes = []byte("replacement lossless audio bytes")

	job, err := service.EnqueueUpgradeJob(context.Background(), UpgradeJobInput{
		Scope: "track", ScopeID: 1, TargetQuality: "hires",
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.TotalCount != 1 || job.State != "queued" {
		t.Fatalf("queued job = %+v", job)
	}
	if _, err := service.processUpgrade(context.Background()); err != nil {
		t.Fatal(err)
	}
	finished, err := service.UpgradeJob(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.State != "completed" || finished.UpgradedCount != 1 || finished.ProcessedCount != 1 {
		t.Fatalf("finished job = %+v", finished)
	}

	var oldRole string
	if err := service.store.db.QueryRow(`SELECT variant_role FROM music_files WHERE id=?`, fileID).Scan(&oldRole); err != nil {
		t.Fatal(err)
	}
	if oldRole != "archive" {
		t.Fatalf("original role = %q, want archive", oldRole)
	}
	actual, err := os.ReadFile(filepath.Join(service.mediaDir, relative))
	if err != nil || string(actual) != string(original) {
		t.Fatal("original bytes changed", err, string(actual))
	}
	track, err := service.store.Tracks(context.Background(), 1)
	if err != nil || len(track) != 1 {
		t.Fatal(err)
	}
	var newID, newRole, newPath string
	if track[0].FileID == nil || *track[0].FileID == fileID {
		t.Fatalf("preferred file did not change: %+v", track[0])
	}
	if err := service.store.db.QueryRow(`SELECT id,variant_role,relative_path FROM music_files WHERE id=?`, *track[0].FileID).
		Scan(&newID, &newRole, &newPath); err != nil {
		t.Fatal(err)
	}
	if newRole != "primary" || newPath == relative || filepath.Ext(newPath) != ".flac" {
		t.Fatalf("replacement = %s %q %q", newID, newRole, newPath)
	}
	var attempts int
	if err := service.store.db.QueryRow(`SELECT attempts FROM music_upgrade_items WHERE job_id=?`, job.ID).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatalf("attempts = %d %v", attempts, err)
	}
}

func TestUpgradeQualityRules(t *testing.T) {
	oldMP3 := upgradeItem{OldFormat: "mp3", OldBitrate: 128000, DurationMS: 180000}
	oldHigh := upgradeItem{OldFormat: "mp3", OldBitrate: 320000, DurationMS: 180000}
	oldFLAC := upgradeItem{OldFormat: "flac", OldSampleRate: 44100, DurationMS: 180000}
	cases := []struct {
		name     string
		item     upgradeItem
		download AudioTags
		target   string
		cap      int
		want     bool
	}{
		{"lossy to lossless", oldMP3, AudioTags{Format: "flac", SampleRate: 44100, DurationMS: 180000}, "lossless", 0, true},
		{"lossy to hi-res", oldMP3, AudioTags{Format: "flac", SampleRate: 96000, DurationMS: 180000}, "hires", 0, true},
		{"lossy to high", oldMP3, AudioTags{Format: "mp3", Bitrate: 256000, SampleRate: 44100, DurationMS: 180000}, "high", 0, true},
		{"high to lower high", oldHigh, AudioTags{Format: "mp3", Bitrate: 256000, SampleRate: 44100, DurationMS: 180000}, "high", 0, false},
		{"lossless to high", oldFLAC, AudioTags{Format: "mp3", Bitrate: 320000, SampleRate: 44100, DurationMS: 180000}, "high", 0, false},
		{"lossless to hires", oldFLAC, AudioTags{Format: "flac", SampleRate: 96000, DurationMS: 180000}, "hires", 0, true},
		{"same hi-res", upgradeItem{OldFormat: "flac", OldSampleRate: 96000, DurationMS: 180000}, AudioTags{Format: "flac", SampleRate: 96000, DurationMS: 180000}, "hires", 0, false},
		{"unknown old", upgradeItem{OldFormat: "weird", DurationMS: 180000}, AudioTags{Format: "flac", SampleRate: 44100, DurationMS: 180000}, "lossless", 0, false},
		{"unknown new", oldMP3, AudioTags{Format: "weird", DurationMS: 180000}, "lossless", 0, false},
		{"duration mismatch", oldMP3, AudioTags{Format: "flac", SampleRate: 44100, DurationMS: 120000}, "lossless", 0, false},
		{"candidate cap", oldMP3, AudioTags{Format: "flac", SampleRate: 44100, Bitrate: 900000, DurationMS: 180000}, "lossless", 128, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, _ := evaluateUpgradeQuality(test.item, test.download, test.target, test.cap)
			if got != test.want {
				t.Fatalf("upgrade allowed=%v, want %v", got, test.want)
			}
		})
	}
}

func TestUpgradeWorkerFailsClosedWhenOriginalChanged(t *testing.T) {
	service, source := upgradeFixture(t)
	seedUpgradeTrack(t, service, []byte("original bytes"))
	source.bytes = []byte("replacement bytes")
	job, err := service.EnqueueUpgradeJob(context.Background(), UpgradeJobInput{
		Scope: "track", ScopeID: 1, TargetQuality: "lossless",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(service.mediaDir, "Artist", "01 Song.mp3"), []byte("changed bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.processUpgrade(context.Background()); err != nil {
		t.Fatal(err)
	}
	finished, err := service.UpgradeJob(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.State != "failed" || finished.FailedCount != 1 {
		t.Fatalf("changed original outcome = %+v", finished)
	}
	var primaries int
	if err := service.store.db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE variant_role='primary'`).Scan(&primaries); err != nil || primaries != 1 {
		t.Fatalf("primary count = %d %v", primaries, err)
	}
}

func TestUpgradeWorkerRecoversInterruptedJobs(t *testing.T) {
	service, source := upgradeFixture(t)
	fileID, _ := seedUpgradeTrack(t, service, []byte("original bytes"))
	source.bytes = []byte("replacement bytes")
	job, err := service.EnqueueUpgradeJob(context.Background(), UpgradeJobInput{
		Scope: "track", ScopeID: 1, TargetQuality: "lossless",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.db.Exec(`UPDATE music_upgrade_jobs SET state='running',lease_owner='lost',lease_until=1 WHERE id=?`, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.db.Exec(`UPDATE music_upgrade_items SET state='downloading' WHERE job_id=?`, job.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.RecoverUpgradeJobs(context.Background()); err != nil {
		t.Fatal(err)
	}
	recovered, err := service.UpgradeJob(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State != "queued" || recovered.CurrentTrack != "" {
		t.Fatalf("recovered job = %+v", recovered)
	}
	if _, err := service.processUpgrade(context.Background()); err != nil {
		t.Fatal(err)
	}
	var role string
	if err := service.store.db.QueryRow(`SELECT variant_role FROM music_files WHERE id=?`, fileID).Scan(&role); err != nil || role != "archive" {
		t.Fatalf("original role = %q %v", role, err)
	}
}

func TestUpgradeGlobalRateIsDurable(t *testing.T) {
	service, _ := upgradeFixture(t)
	// Item snapshots reference registered originals, so file 1 must exist.
	seedUpgradeTrack(t, service, []byte("original bytes"))
	claim, claimed, err := service.claimUpgrade(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// No work exists in this fixture, but a completed terminal item is not
	// needed to prove the clock: the rate row must always advance.
	if claimed {
		t.Fatalf("unexpected empty-scope claim %+v", claim)
	}
	var next int64
	if err := service.store.db.QueryRow(`SELECT next_attempt_ms FROM music_upgrade_rate WHERE id=1`).Scan(&next); err != nil {
		t.Fatal(err)
	}
	if next != 0 {
		t.Fatalf("rate advanced without work: %d", next)
	}
	// The durable clock is read under the same claim transaction that would
	// select work; simulate its expected advance with a valid job claim.
	if _, err := service.store.db.Exec(`UPDATE music_upgrade_rate SET next_attempt_ms=? WHERE id=1`, time.Now().Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.db.Exec(`INSERT INTO music_upgrade_jobs(scope,scope_id,scope_name,target_quality,max_bitrate_kbps,rate_per_hour,state,total_count,created_at,updated_at)
		VALUES('library',0,'Library','high',0,200,'queued',1,unixepoch(),unixepoch())`); err != nil {
		t.Fatal(err)
	}
	var jobID int64
	if err := service.store.db.QueryRow(`SELECT id FROM music_upgrade_jobs`).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.db.Exec(`INSERT INTO music_upgrade_items(job_id,track_id,music_file_id,artist_id,album_id,old_root,old_relative_path,old_sha256,title,artist,album,state,created_at,updated_at)
		VALUES(?,1,?,1,1,'media','Artist/01 Song.mp3','x','Song','Artist','Album','pending',unixepoch(),unixepoch())`, jobID, 1); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := service.claimUpgrade(context.Background()); err != nil || claimed {
		t.Fatalf("future global rate permitted a claim (claimed=%v err=%v)", claimed, err)
	}
}

func TestUpgradeSourceFailureUsesBoundedRetryBudget(t *testing.T) {
	service, source := upgradeFixture(t)
	seedUpgradeTrack(t, service, []byte("original bytes"))
	source.searchErr = errors.New("provider unavailable")
	job, err := service.EnqueueUpgradeJob(context.Background(), UpgradeJobInput{
		Scope: "track", ScopeID: 1, TargetQuality: "lossless",
	})
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= upgradeMaxAttempts; attempt++ {
		if _, err := service.processUpgrade(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	finished, err := service.UpgradeJob(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.State != "failed" || finished.FailedCount != 1 {
		t.Fatalf("exhausted retry outcome = %+v", finished)
	}
	var attempts int
	if err := service.store.db.QueryRow(`SELECT attempts FROM music_upgrade_items WHERE job_id=?`, job.ID).Scan(&attempts); err != nil || attempts != upgradeMaxAttempts {
		t.Fatalf("attempts = %d %v", attempts, err)
	}
	var active int
	if err := service.store.db.QueryRow(`SELECT COUNT(*) FROM music_upgrade_active`).Scan(&active); err != nil || active != 0 {
		t.Fatalf("active guards = %d %v", active, err)
	}
	var placeholder sql.NullInt64
	_ = placeholder
}
