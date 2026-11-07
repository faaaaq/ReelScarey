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
