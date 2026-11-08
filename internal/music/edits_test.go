package music

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
)

const (
	testTrackID2     = "11111111-1111-4111-8111-111111111111"
	testRecordingID2 = "22222222-2222-4222-8222-222222222222"
)

func editFixture(t *testing.T) (*Service, *sql.DB, string) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	media := t.TempDir()
	if _, err = db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES(1,?, 'Artist',1,'available',1,1)`, testMBID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(2,1,?, 'Album',1,'available',1,1)`, testRGID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO music_tracks(id,album_id,musicbrainz_id,recording_id,disc,number,title,created_at,updated_at) VALUES(3,2,?,?,1,1,'Song',1,1)`, testTrackID, testRecordingID); err != nil {
		t.Fatal(err)
	}
	return NewService(db, media), db, media
}

func insertMusicFile(t *testing.T, db *sql.DB, id, trackID int64, relative, tagJSON string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO music_files(id,artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,duration_ms,tag_json,imported_at,origin,source_name) VALUES(?,1,2,?,?,?,?,?, 'flac',1000,?,1,'existing','file.flac')`,
		id, trackID, "Music", relative, len("fixture"), "sha", tagJSON); err != nil {
		t.Fatal(err)
	}
}

func tagJSONFor(title, trackMBID, recordingID string, number int) string {
	return fmt.Sprintf(`{"artist":"Artist","album_artist":"Artist","album":"Album","title":%q,"track":%d,"disc":1,"musicbrainz_artist_id":%q,"musicbrainz_release_group_id":%q,"musicbrainz_track_id":%q,"musicbrainz_recording_id":%q}`,
		title, number, testMBID, testRGID, trackMBID, recordingID)
}

func matchingTags() string { return tagJSONFor("Song", testTrackID, testRecordingID, 1) }

func TestPreviewDetectsMoveRetagAndNoop(t *testing.T) {
	ctx := context.Background()
	service, db, _ := editFixture(t)
	if _, err := db.Exec(`INSERT INTO music_tracks(id,album_id,musicbrainz_id,recording_id,disc,number,title,created_at,updated_at) VALUES(4,2,?,?,1,2,'Other',1,1)`, testTrackID2, testRecordingID2); err != nil {
		t.Fatal(err)
	}
	insertMusicFile(t, db, 1, 3, "Music/Artist/Album/Song.flac", matchingTags())
	insertMusicFile(t, db, 2, 3, "Music/Artist/Album/01 Song.flac", `{}`)
	insertMusicFile(t, db, 3, 4, "Music/Artist/Album/02 Other.flac", tagJSONFor("Other", testTrackID2, testRecordingID2, 2))
	plan, err := service.PreviewEdits(ctx, []int64{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if plan.PlanHash == "" || len(plan.Files) != 2 {
		t.Fatal("plan", plan)
	}
	if plan.Files[0].MusicFileID != 1 || plan.Files[0].Action != "move" {
		t.Fatal("move file", plan.Files[0])
	}
	if plan.Files[1].MusicFileID != 2 || plan.Files[1].Action != "retag" {
		t.Fatal("retag file", plan.Files[1])
	}
}

func TestApplyAndRollbackMove(t *testing.T) {
	ctx := context.Background()
	service, db, media := editFixture(t)
	sourceRel := "Music/Artist/Album/Song.flac"
	source := filepath.Join(media, filepath.FromSlash(sourceRel))
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	insertMusicFile(t, db, 1, 3, sourceRel, matchingTags())
	plan, err := service.PreviewEdits(ctx, []int64{1})
	if err != nil || len(plan.Files) != 1 || plan.Files[0].Action != "move" {
		t.Fatal("preview", plan, err)
	}
	job, err := service.ApplyEdits(ctx, []int64{1}, plan.PlanHash, true)
	if err != nil {
		t.Fatal(err)
	}
	job = waitForEditJob(t, ctx, service, job.ID)
	if job.State != "completed" {
		t.Fatal("job", job)
	}
	destination := filepath.Join(media, filepath.FromSlash(plan.Files[0].DestinationPath))
	if _, err := os.Stat(destination); err != nil {
		t.Fatal("destination missing", err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatal("source was not moved", err)
	}
	var relative string
	if err := db.QueryRow(`SELECT relative_path FROM music_files WHERE id=1`).Scan(&relative); err != nil || relative != plan.Files[0].DestinationPath {
		t.Fatal("catalog path", relative, err)
	}
	if err := os.WriteFile(source, []byte("unrelated replacement"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RollbackEdits(ctx, job.ID, true); err == nil {
		t.Fatal("rollback accepted an occupied source")
	}
	for name, want := range map[string]string{source: "unrelated replacement", destination: "fixture"} {
		if got, err := os.ReadFile(name); err != nil || string(got) != want {
			t.Fatalf("rollback touched %s: %q, %v", name, got, err)
		}
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RollbackEdits(ctx, job.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatal("rollback did not restore the source", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatal("rollback left the destination", err)
	}
	if err := db.QueryRow(`SELECT relative_path FROM music_files WHERE id=1`).Scan(&relative); err != nil || relative != sourceRel {
		t.Fatal("rollback path", relative, err)
	}
}

func TestApplyRetagWithFFmpeg(t *testing.T) {
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " is not installed")
		}
	}
	ctx := context.Background()
	service, db, media := editFixture(t)
	sourceRel := "Music/Artist/Album/Song.flac"
	source := filepath.Join(media, filepath.FromSlash(sourceRel))
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-c:a", "flac", "-y", source)
	if err := cmd.Run(); err != nil {
		t.Skip("could not generate a FLAC fixture", err)
	}
	insertMusicFile(t, db, 1, 3, sourceRel, `{}`)
	service.SetProber(FFprobeTagger{})
	plan, err := service.PreviewEdits(ctx, []int64{1})
	if err != nil || len(plan.Files) != 1 || plan.Files[0].Action != "move_retag" {
		t.Fatal("preview", plan, err)
	}
	job, err := service.ApplyEdits(ctx, []int64{1}, plan.PlanHash, true)
	if err != nil {
		t.Fatal(err)
	}
	job = waitForEditJob(t, ctx, service, job.ID)
	if job.State != "completed" {
		t.Fatal("job", job)
	}
	destination := filepath.Join(media, filepath.FromSlash(plan.Files[0].DestinationPath))
	tags, err := (FFprobeTagger{}).ProbeAudio(ctx, destination)
	if err != nil {
		t.Fatal(err)
	}
	if tags.Artist != "Artist" || tags.Album != "Album" || tags.Title != "Song" || tags.Track == nil || *tags.Track != 1 {
		t.Fatal("written tags", tags)
	}
	if _, err := os.Stat(source + ".reelscarey-backup"); err != nil {
		t.Fatal("backup missing", err)
	}
	backup := source + ".reelscarey-backup"
	if err := os.Rename(backup, backup+".fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RollbackEdits(ctx, job.ID, true); err == nil {
		t.Fatal("rollback accepted a missing retained original")
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatal("missing backup must leave the edited file intact", err)
	}
	if err := os.Rename(backup+".fixture", backup); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RollbackEdits(ctx, job.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatal("rollback did not restore the original", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatal("rollback left the tagged destination", err)
	}
}

func waitForEditJob(t *testing.T, ctx context.Context, service *Service, id int64) EditJob {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		job, err := service.EditJob(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if job.State != "running" {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatal("edit job timed out", job)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
