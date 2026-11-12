package transfer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func removalFixture(t *testing.T, shared bool) (*workflowFixture, int64, int64, string) {
	t.Helper()
	f := setupWorkflow(t)
	if _, e := f.db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,year,category,monitored) VALUES(9,'series',9,'Removal Show',2020,'standard',1);INSERT INTO series_details(media_id,revision) VALUES(9,1);INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored,loaded_at) VALUES(90,9,90,1,'Season 01','','2020-01-01',2,1,'loaded');INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(901,90,901,1,'One','','2020-01-01',1),(902,90,902,2,'Two','','2020-01-02',1)`); e != nil {
		t.Fatal(e)
	}
	rel := "TV/Removal Show (2020) [tmdb-9]/Season 01/Removal Show - S01E01 - One.mkv"
	full := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(rel))
	if e := os.MkdirAll(filepath.Dir(full), 0o755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(full, []byte("owned fixture media"), 0o644); e != nil {
		t.Fatal(e)
	}
	if _, e := f.db.Exec(`INSERT INTO media_files(id,series_id,download_id,file_index,relative_path,size_bytes,sha256,probe_json,imported_at,origin) VALUES(900,9,NULL,0,?,19,'fixture','{}',1,'existing')`, rel); e != nil {
		t.Fatal(e)
	}
	if _, e := f.db.Exec(`INSERT INTO episode_files(episode_id,media_file_id) VALUES(901,900)`); e != nil {
		t.Fatal(e)
	}
	if shared {
		if _, e := f.db.Exec(`INSERT INTO episode_files(episode_id,media_file_id) VALUES(902,900)`); e != nil {
			t.Fatal(e)
		}
	}
	return f, 9, 901, full
}

func TestRemoveEpisodeFileDeletesMediaCopyAndRegistration(t *testing.T) {
	f, series, episode, full := removalFixture(t, false)
	if e := f.s.RemoveEpisodeFile(context.Background(), series, 1, episode, true); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(full); !os.IsNotExist(e) {
		t.Fatal("the media copy was not deleted", e)
	}
	var files, links int
	if e := f.db.QueryRow(`SELECT (SELECT COUNT(*) FROM media_files WHERE id=900),(SELECT COUNT(*) FROM episode_files WHERE media_file_id=900)`).Scan(&files, &links); e != nil || files != 0 || links != 0 {
		t.Fatal("the registration was not removed", files, links, e)
	}
	var events int
	if e := f.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='title' AND object_id='9' AND state='episode and its file deleted'`).Scan(&events); e != nil || events != 1 {
		t.Fatal("the deletion left no honest event", events, e)
	}
}

// A replacement that finished or was cancelled keeps its tracking row for
// retry binding, but it must not block the episode's reviewed deletion. Only
// a replacement whose owning job is still active holds the file.
func TestRemoveEpisodeFileIgnoresFinishedReplacements(t *testing.T) {
	f, series, episode, full := removalFixture(t, false)
	if _, e := f.db.Exec(`INSERT INTO release_candidates(id,movie_id,title,size_bytes,published_at,payload,expires_at) VALUES('r1',9,'Old release',1,'2026-01-01T00:00:00Z',x'00',0),('r2',9,'Live replacement',1,'2026-01-01T00:00:00Z',x'00',0)`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.db.Exec(`INSERT INTO downloads(id,movie_id,release_id,title,info_hash,payload,client_url,save_path,state,created_at,updated_at,kind) VALUES('done',9,'r1','Old release','hash',X'01','https://unused.invalid','/unused','completed',1,1,'series'),('active',9,'r2','Live replacement','hash2',X'02','https://unused.invalid','/unused','blocked',1,1,'series')`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.db.Exec(`UPDATE downloads SET error_code='replacement_review' WHERE id='active'`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.db.Exec(`INSERT INTO acquisition_replacements(source_kind,job_id,media_file_id,target_sha) VALUES('download','done',900,X'01'),('download','active',900,X'02')`); e != nil {
		t.Fatal(e)
	}
	// The still-active replacement blocks the deletion.
	if e := f.s.RemoveEpisodeFile(context.Background(), series, 1, episode, true); problemCode(e) != "replacement_pending" {
		t.Fatal("an active replacement did not block the deletion", e)
	}
	if _, e := os.Stat(full); e != nil {
		t.Fatal("the blocked file was touched", e)
	}
	// Cancel the active job: the row stays for retry binding, but the file is
	// no longer held.
	if _, e := f.db.Exec(`UPDATE downloads SET state='cancelled',error_code='' WHERE id='active'`); e != nil {
		t.Fatal(e)
	}
	if e := f.s.RemoveEpisodeFile(context.Background(), series, 1, episode, true); e != nil {
		t.Fatal("a cancelled replacement still blocked the deletion", e)
	}
	if _, e := os.Stat(full); !os.IsNotExist(e) {
		t.Fatal("the media copy was not deleted", e)
	}
	// The file is gone, so its replacement bindings are dead and must not
	// keep a dangling foreign key to the deleted registration.
	var rows int
	if e := f.db.QueryRow(`SELECT COUNT(*) FROM acquisition_replacements WHERE media_file_id=900`).Scan(&rows); e != nil || rows != 0 {
		t.Fatal("dead replacement bindings were not removed with the file", rows, e)
	}
}

// A youtube replacement behaves the same way: an awaiting-review job holds
// the file, a finished one does not.
func TestRemoveEpisodeFileIgnoresFinishedYoutubeReplacements(t *testing.T) {
	f, series, episode, full := removalFixture(t, false)
	if _, e := f.db.Exec(`INSERT INTO youtube_reviews(id,video_id,video_title,channel,target_kind,media_id,target_snapshot,expires_at,consumed_at) VALUES('r','abcdefghijk','Video','','movie',9,'{}',0,0),('r2','lmnopqrstuv','Video 2','','movie',9,'{}',0,0)`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.db.Exec(`INSERT INTO youtube_jobs(id,review_id,video_id,video_title,channel,target_kind,media_id,target_snapshot,state,created_at,updated_at) VALUES('ydone','r','abcdefghijk','Video','','movie',9,'{}','completed',1,1),('yactive','r2','lmnopqrstuv','Video 2','','movie',9,'{}','awaiting_review',1,1)`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.db.Exec(`INSERT INTO acquisition_replacements(source_kind,job_id,media_file_id,target_sha) VALUES('youtube','ydone',900,X'01'),('youtube','yactive',900,X'02')`); e != nil {
		t.Fatal(e)
	}
	if e := f.s.RemoveEpisodeFile(context.Background(), series, 1, episode, true); problemCode(e) != "replacement_pending" {
		t.Fatal("an active youtube replacement did not block the deletion", e)
	}
	if _, e := f.db.Exec(`UPDATE youtube_jobs SET state='cancelled' WHERE id='yactive'`); e != nil {
		t.Fatal(e)
	}
	if e := f.s.RemoveEpisodeFile(context.Background(), series, 1, episode, true); e != nil {
		t.Fatal("a cancelled youtube replacement still blocked the deletion", e)
	}
	if _, e := os.Stat(full); !os.IsNotExist(e) {
		t.Fatal("the media copy was not deleted", e)
	}
	var tracked int
	if e := f.db.QueryRow(`SELECT COUNT(*) FROM acquisition_replacements WHERE media_file_id=?`, 900).Scan(&tracked); e != nil || tracked != 0 {
		t.Fatal("dead youtube replacement bindings were not removed with the file", tracked, e)
	}
}

func TestRemoveEpisodeFileKeepsSharedFilesAndSeedingSources(t *testing.T) {
	f, series, episode, full := removalFixture(t, true)
	if e := f.s.RemoveEpisodeFile(context.Background(), series, 1, episode, true); problemCode(e) != "shared_file" {
		t.Fatal("a shared file was deleted from under another episode", e)
	}
	if _, e := os.Stat(full); e != nil {
		t.Fatal("the shared file was touched", e)
	}
	// Without deleting files the episode is only unlinked.
	if e := f.s.RemoveEpisodeFile(context.Background(), series, 1, episode, false); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(full); e != nil {
		t.Fatal("unlinking deleted the file", e)
	}
	var files int
	if e := f.db.QueryRow(`SELECT COUNT(*) FROM media_files WHERE id=900`).Scan(&files); e != nil || files != 1 {
		t.Fatal("unlinking removed the registration", files, e)
	}
}
