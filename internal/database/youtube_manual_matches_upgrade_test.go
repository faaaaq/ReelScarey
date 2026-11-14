package database

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// Migration 094 widens youtube_series_sources.mapping to add the explicit
// "manual" mode and introduces youtube_series_matches. Widening the CHECK
// rebuilds the sources table, so this test proves the existing sources and the
// entries that reference them survive the rebuild with their ids, references
// and unique guard intact. It runs through the same file-backed runner the
// application uses.
func TestYouTubeManualMatchesUpgradePreservesSourcesAndEntries(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "reelscarey.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	old := fstest.MapFS{}
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if filepath.Base(name) >= "094_" {
			continue
		}
		body, err := fs.ReadFile(migrationFiles, name)
		if err != nil {
			t.Fatal(err)
		}
		old[name] = &fstest.MapFile{Data: body}
	}
	if err = migrate(ctx, db, old); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`INSERT INTO media_items(id,kind,tmdb_id,title,year,status,monitored) VALUES(1,'series',1,'Fixture Show',2024,'missing',1)`,
		`INSERT INTO series_details(media_id) VALUES(1)`,
		`INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored) VALUES(1,1,1,1,'Season 1','','',2,1)`,
		`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(1,1,11,1,'One','','',1)`,
		`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(2,1,12,2,'Two','','',1)`,
		`INSERT INTO youtube_series_sources(id,media_id,source_url,title,mapping,season_number,start_episode,quality,subtitles_json,auto_import,enabled,interval_hours,initialized,revision,requested,next_run,last_checked,state,message,target_snapshot,created_at,lease_owner,lease_until) VALUES('src-old',1,'https://www.youtube.com/playlist?list=PLabcdefghijk','Fixture Show','episode',1,1,'1080','{"languages":["en"],"automatic":true,"quality":"1080"}',1,1,12,1,5,0,4444,3333,'idle','kept','{"Title":"Fixture Show"}',2222,'owner-1',5555)`,
		`INSERT INTO youtube_series_entries(source_id,video_id,position,title,channel,state,episode_id,job_id,note,discovered_at) VALUES('src-old','aaaaaaaaaaa',0,'S01E01','Fixture','queued',1,NULL,'S01E01 — One',111)`,
		`INSERT INTO youtube_series_entries(source_id,video_id,position,title,channel,state,episode_id,job_id,note,discovered_at) VALUES('src-old','bbbbbbbbbbb',1,'S01E02','Fixture','skipped',2,NULL,'already has a file',222)`,
	} {
		if _, err = db.Exec(query); err != nil {
			t.Fatal(query, err)
		}
	}
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if version, err := Version(ctx, db); err != nil || version != 100 {
		t.Fatal("unexpected schema version", version, err)
	}
	var mapping, quality, subtitles, state, message, snapshot, owner, url, title string
	var season, start, interval, initialized, revision, requested, nextRun, lastChecked, autoImport, enabled, created, leaseUntil int
	if err = db.QueryRow(`SELECT mapping,quality,subtitles_json,state,message,target_snapshot,lease_owner,lease_until,source_url,title,season_number,start_episode,interval_hours,initialized,revision,requested,next_run,last_checked,auto_import,enabled,created_at FROM youtube_series_sources WHERE id='src-old'`).Scan(&mapping, &quality, &subtitles, &state, &message, &snapshot, &owner, &leaseUntil, &url, &title, &season, &start, &interval, &initialized, &revision, &requested, &nextRun, &lastChecked, &autoImport, &enabled, &created); err != nil {
		t.Fatal("existing source lost", err)
	}
	if mapping != "episode" || quality != "1080" || !strings.Contains(subtitles, `"en"`) || state != "idle" || message != "kept" || snapshot != `{"Title":"Fixture Show"}` || owner != "owner-1" || leaseUntil != 5555 {
		t.Fatal("source fields changed", mapping, quality, subtitles, state, message, snapshot, owner, leaseUntil)
	}
	if url != "https://www.youtube.com/playlist?list=PLabcdefghijk" || title != "Fixture Show" || season != 1 || start != 1 || interval != 12 || initialized != 1 || revision != 5 || requested != 0 || nextRun != 4444 || lastChecked != 3333 || autoImport != 1 || enabled != 1 || created != 2222 {
		t.Fatal("source numeric fields changed", url, title, season, start, interval, initialized, revision, requested, nextRun, lastChecked, autoImport, enabled, created)
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM youtube_series_entries WHERE source_id='src-old'`).Scan(&count); err != nil || count != 2 {
		t.Fatal("entries were not preserved across the rebuild", count, err)
	}
	var entryState, note string
	var episodeID sql.NullInt64
	var position, discovered int
	if err = db.QueryRow(`SELECT state,episode_id,note,position,discovered_at FROM youtube_series_entries WHERE source_id='src-old' AND video_id='aaaaaaaaaaa'`).Scan(&entryState, &episodeID, &note, &position, &discovered); err != nil {
		t.Fatal("entry lost", err)
	}
	if entryState != "queued" || !episodeID.Valid || episodeID.Int64 != 1 || note != "S01E01 — One" || position != 0 || discovered != 111 {
		t.Fatal("entry fields changed", entryState, episodeID, note, position, discovered)
	}

	// The widened CHECK accepts the new manual mode and still rejects junk.
	if _, err = db.Exec(`UPDATE youtube_series_sources SET mapping='manual' WHERE id='src-old'`); err != nil {
		t.Fatal("manual mapping rejected", err)
	}
	if _, err = db.Exec(`UPDATE youtube_series_sources SET mapping='order' WHERE id='src-old'`); err != nil {
		t.Fatal("order mapping rejected", err)
	}
	if _, err = db.Exec(`UPDATE youtube_series_sources SET mapping='guess' WHERE id='src-old'`); err == nil {
		t.Fatal("unknown mapping accepted")
	}
	// The unique source guard survived the rebuild.
	if _, err = db.Exec(`INSERT INTO youtube_series_sources(id,media_id,source_url,title,mapping,created_at) VALUES('src-dup',1,'https://www.youtube.com/playlist?list=PLabcdefghijk','Dup','episode',1)`); err == nil {
		t.Fatal("duplicate media/source_url accepted after rebuild")
	}
	// Entries still cascade from their source.
	if _, err = db.Exec(`DELETE FROM youtube_series_sources WHERE id='src-old'`); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM youtube_series_entries WHERE source_id='src-old'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("entries did not cascade", count, err)
	}

	// The new match table is present, references episodes and the source, and
	// rejects orphans.
	if _, err = db.Exec(`INSERT INTO youtube_series_sources(id,media_id,source_url,title,mapping,created_at) VALUES('src-manual',1,'https://www.youtube.com/playlist?list=PLzzzzzzzzzz','Manual','manual',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO youtube_series_matches(source_id,video_id,episode_id,position,title,created_at) VALUES('src-manual','ccccccccccc',2,0,'X1',1)`); err != nil {
		t.Fatal("manual match insert failed", err)
	}
	if _, err = db.Exec(`INSERT INTO youtube_series_matches(source_id,video_id,episode_id) VALUES('src-manual','ddddddddddd',999)`); err == nil {
		t.Fatal("orphan episode accepted")
	}
	if _, err = db.Exec(`INSERT INTO youtube_series_matches(source_id,video_id,episode_id) VALUES('missing-source','eeeeeeeeeee',1)`); err == nil {
		t.Fatal("orphan source accepted")
	}
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign key consistency lost after rebuild")
	}
}
