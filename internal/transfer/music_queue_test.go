package transfer

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// musicTestAlbum inserts a minimal artist/album pair. Music downloads are
// keyed by the album foreign key; MusicBrainz identities are opaque fixtures.
func musicTestAlbum(t *testing.T, f *workflowFixture) int64 {
	t.Helper()
	res, e := f.db.Exec(`INSERT INTO music_artists(musicbrainz_id,name,sort_name,created_at,updated_at) VALUES(?,?,?,unixepoch(),unixepoch())`,
		fmt.Sprintf("fixture-artist-%d", time.Now().UnixNano()), "Fixture Artist", "Fixture Artist")
	if e != nil {
		t.Fatal(e)
	}
	artistID, e := res.LastInsertId()
	if e != nil {
		t.Fatal(e)
	}
	res, e = f.db.Exec(`INSERT INTO music_albums(artist_id,musicbrainz_id,title,created_at,updated_at) VALUES(?,?,'Fixture Album',unixepoch(),unixepoch())`,
		artistID, fmt.Sprintf("fixture-album-%d", time.Now().UnixNano()))
	if e != nil {
		t.Fatal(e)
	}
	albumID, e := res.LastInsertId()
	if e != nil {
		t.Fatal(e)
	}
	return albumID
}

func musicTestCandidate(t *testing.T, f *workflowFixture, albumID int64, releaseID string, expires int64) {
	t.Helper()
	cfg, e := f.s.indexerConfig(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	c := Candidate{Release: Release{
		ID:        releaseID,
		Title:     "Fixture Artist - Fixture Album [FLAC]",
		Size:      1000,
		Published: "2026-11-01T00:00:00Z",
	}, Magnet: testMagnet, Generation: cfg.Generation}
	body, e := json.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	sealed, e := f.s.secrets.Seal("music-release", body)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.db.Exec(`INSERT INTO music_release_candidates(id,album_id,title,size_bytes,payload,expires_at,created_at) VALUES(?,?,?,?,?,?,unixepoch())
		ON CONFLICT(id) DO UPDATE SET album_id=excluded.album_id,title=excluded.title,size_bytes=excluded.size_bytes,payload=excluded.payload,expires_at=excluded.expires_at`,
		fmt.Sprintf("%d:%s", albumID, releaseID), albumID, c.Title, c.Size, sealed, expires); e != nil {
		t.Fatal(e)
	}
}

func musicTestQueue(t *testing.T, f *workflowFixture) MusicJob {
	t.Helper()
	albumID := musicTestAlbum(t, f)
	musicTestCandidate(t, f, albumID, "release-1", time.Now().Unix()+600)
	job, e := f.s.QueueMusic(context.Background(), albumID, "release-1", "Music")
	if e != nil {
		t.Fatal(e)
	}
	return job
}

func musicTestState(t *testing.T, f *workflowFixture, id string) MusicJob {
	t.Helper()
	job, e := f.s.MusicJob(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	return job
}

func musicTestProcess(t *testing.T, f *workflowFixture) (bool, error) {
	t.Helper()
	if _, e := f.db.Exec(`UPDATE music_downloads SET next_run=0 WHERE state IN (` + musicActiveStates + `) AND lease_until<unixepoch()`); e != nil {
		t.Fatal(e)
	}
	return f.s.ProcessMusic(context.Background())
}

func TestMusicQueueTracksSnapshotAndRefusesDuplicate(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	albumID := musicTestAlbum(t, f)
	musicTestCandidate(t, f, albumID, "release-1", time.Now().Unix()+600)

	job, e := f.s.QueueMusic(ctx, albumID, "release-1", "Music")
	if e != nil {
		t.Fatal(e)
	}
	if job.AlbumID != albumID || job.ReleaseID != "release-1" || job.Root != "Music" {
		t.Fatal("queued music job lost its selection", job)
	}
	if job.State != "downloading" || job.Protocol != "torrent" || job.InfoHash != testHash {
		t.Fatal("queued music job is not tracking the submitted payload", job)
	}
	if job.ClientLabel != "qBittorrent (Settings)" || job.ClientRoot != "/downloads" || job.SavePath != "/downloads/ReelScarey/"+job.ID {
		t.Fatal("queued music job did not snapshot its client", job)
	}
	if f.client.adds != 1 || f.client.lastTag != "reelscarey-"+job.ID || f.client.lastPayload.Magnet == "" {
		t.Fatal("the music payload was not submitted through the torrent adapter", f.client.adds, f.client.lastTag)
	}

	// A second active download for the same album is refused without another
	// submission, even before the release candidate is loaded.
	if _, e = f.s.QueueMusic(ctx, albumID, "release-1", "Music"); problemCode(e) != "duplicate_music_download" {
		t.Fatal("a duplicate music download was not refused", e)
	}
	if f.client.adds != 1 {
		t.Fatal("the duplicate selection submitted the payload again", f.client.adds)
	}

	page, e := f.s.MusicJobs(ctx, 1)
	if e != nil || page.Total != 1 || page.Limit != 24 || len(page.Items) != 1 || page.Items[0].ID != job.ID {
		t.Fatal("music job listing is not page-scoped", page, e)
	}

	// Cancel only applies to an active job and leaves the client job alone.
	if e = f.s.CancelMusic(ctx, job.ID); e != nil {
		t.Fatal(e)
	}
	if got := musicTestState(t, f, job.ID); got.State != "cancelled" {
		t.Fatal("cancelling an active music download did not stop tracking", got)
	}
	if f.client.action != "" || f.client.adds != 1 {
		t.Fatal("cancelling changed the client job", f.client.action)
	}
	if e = f.s.CancelMusic(ctx, job.ID); problemCode(e) != "music_download_busy" {
		t.Fatal("a cancelled music download was cancelled again", e)
	}

	// Retry re-arms only blocked/failed jobs and resets the worker counters.
	if _, e = f.db.Exec(`UPDATE music_downloads SET state='blocked',error='review',attempts=3 WHERE id=?`, job.ID); e != nil {
		t.Fatal(e)
	}
	if e = f.s.RetryMusic(ctx, job.ID); e != nil {
		t.Fatal(e)
	}
	got := musicTestState(t, f, job.ID)
	if got.State != "downloading" || got.Error != "" || got.Attempts != 0 {
		t.Fatal("retry did not reset the blocked music job", got)
	}
	if e = f.s.RetryMusic(ctx, job.ID); problemCode(e) != "music_download_busy" {
		t.Fatal("an active music download was retried", e)
	}
}

func TestMusicQueueRejectsExpiredAndForeignCandidates(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	albumID := musicTestAlbum(t, f)
	if _, e := f.s.MusicJob(ctx, "missing"); problemCode(e) != "music_job_not_found" {
		t.Fatal("an unknown music job was returned", e)
	}
	if _, e := f.s.QueueMusic(ctx, albumID, "release-1", "Music"); problemCode(e) != "release_expired" {
		t.Fatal("a missing candidate was queued", e)
	}
	musicTestCandidate(t, f, albumID, "release-1", time.Now().Unix()-1)
	if _, e := f.s.QueueMusic(ctx, albumID, "release-1", "Music"); problemCode(e) != "release_expired" {
		t.Fatal("an expired candidate was queued", e)
	}
	musicTestCandidate(t, f, albumID, "release-1", time.Now().Unix()+600)
	if _, e := f.s.QueueMusic(ctx, albumID, "release-1", "../Music"); problemCode(e) != "invalid_music_root" {
		t.Fatal("an escaping music root was accepted", e)
	}
	if _, e := f.s.QueueMusic(ctx, albumID+1000, "release-1", "Music"); problemCode(e) != "invalid_music_album" {
		t.Fatal("a music download was queued for an unknown album", e)
	}
	if f.client.adds != 0 {
		t.Fatal("a refused music selection reached the client", f.client.adds)
	}
}

func TestMusicQueueRecordsUncertainSubmissionAsBlocked(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	albumID := musicTestAlbum(t, f)
	musicTestCandidate(t, f, albumID, "release-1", time.Now().Unix()+600)
	f.client.lostResponse = true

	job, e := f.s.QueueMusic(ctx, albumID, "release-1", "Music")
	if e != nil {
		t.Fatal("an uncertain submission must be reviewable, not lost", e)
	}
	if job.State != "blocked" || job.Error == "" {
		t.Fatal("an uncertain music submission was not recorded for review", job)
	}
	if f.client.adds != 1 {
		t.Fatal(f.client.adds)
	}
	// Blocked rows are never claimed, so the worker cannot resubmit them.
	if processed, e := musicTestProcess(t, f); e != nil || processed {
		t.Fatal("a blocked music submission was claimed", processed, e)
	}
	if f.client.adds != 1 {
		t.Fatal("an uncertain music submission was resubmitted", f.client.adds)
	}
}
