package database

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/faaaaq/ReelScarey/internal/quality"
)

// youtube2UpgradeFixture builds a database migrated only up to (excluding)
// cutoff (e.g. "101_") using the same isolated fstest pattern as the other
// upgrade tests: the app never sees a partially migrated schema.
func youtube2UpgradeFixture(t *testing.T, cutoff string) (*sql.DB, []string, fstest.MapFS) {
	t.Helper()
	old := fstest.MapFS{}
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if filepath.Base(name) >= cutoff {
			continue
		}
		body, err := fs.ReadFile(migrationFiles, name)
		if err != nil {
			t.Fatal(err)
		}
		old[name] = &fstest.MapFile{Data: body}
	}
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "reelscarey.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	if err = migrate(context.Background(), db, old); err != nil {
		t.Fatal(err)
	}
	return db, names, old
}

type ledgerRow struct {
	version   int
	name      string
	checksum  string
	appliedAt string
}

func youtube2Ledger(t *testing.T, db *sql.DB) []ledgerRow {
	t.Helper()
	rows, err := db.Query(`SELECT version,name,checksum,applied_at FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []ledgerRow
	for rows.Next() {
		var row ledgerRow
		if err = rows.Scan(&row.version, &row.name, &row.checksum, &row.appliedAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, row)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// musicAcquisitionIssues mirrors the banner's issue count: failed jobs and
// partial completions that have not been acknowledged.
func musicAcquisitionIssues(t *testing.T, db *sql.DB) int {
	t.Helper()
	var issues int
	if err := db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN (state='failed' OR (state='completed' AND completed_count<track_count)) AND acknowledged_at=0 THEN 1 ELSE 0 END),0)
		FROM music_acquisition_jobs`).Scan(&issues); err != nil {
		t.Fatal(err)
	}
	return issues
}

const youtube2SeedJobColumns = `INSERT INTO music_acquisition_jobs(id,source_url,source_kind,title,root,format,state,track_count,completed_count,error,created_at,updated_at) VALUES(?,?,'top_track',?, 'Music','source',?,?,?,?,unixepoch(),unixepoch())`

func youtube2SeedJob(t *testing.T, db *sql.DB, id int, title, state string, tracks, completed int) {
	t.Helper()
	if _, err := db.Exec(youtube2SeedJobColumns, id, "", title, state, tracks, completed, "seeded"); err != nil {
		t.Fatal(err)
	}
}

// Migration 101 adds acknowledged_at to a populated database. Mixed historical
// states must survive unchanged, the column must default to "needs attention"
// (0), the banner query must count only unacknowledged failed/partial jobs,
// and a repeated startup must be a no-op that keeps both the ledger and the
// acknowledgment column intact.
func TestYouTube2MusicAcknowledgeUpgradeMixedStatesAndRerun(t *testing.T) {
	ctx := context.Background()
	db, names, _ := youtube2UpgradeFixture(t, "101_")
	youtube2SeedJob(t, db, 1, "Failed partial", "failed", 2, 0)
	youtube2SeedJob(t, db, 2, "Completed partial", "completed", 2, 1)
	youtube2SeedJob(t, db, 3, "Queued", "queued", 3, 0)
	youtube2SeedJob(t, db, 4, "Running", "running", 3, 1)
	youtube2SeedJob(t, db, 5, "Fully completed", "completed", 2, 2)

	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if version, err := Version(ctx, db); err != nil || version != len(names) {
		t.Fatal("unexpected schema version", version, len(names), err)
	}
	// The new column exists and defaults to 0 ("needs attention").
	var dflt *string
	if err := db.QueryRow(`SELECT dflt_value FROM pragma_table_info('music_acquisition_jobs') WHERE name='acknowledged_at'`).Scan(&dflt); err != nil {
		t.Fatal("acknowledged_at column missing after upgrade", err)
	}
	if dflt == nil || *dflt != "0" {
		t.Fatal("unexpected acknowledged_at default", dflt)
	}
	// Every pre-existing row keeps its state and starts unacknowledged.
	var states = map[string]string{}
	rows, err := db.Query(`SELECT id,state FROM music_acquisition_jobs ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int
		var state string
		if err = rows.Scan(&id, &state); err != nil {
			t.Fatal(err)
		}
		states[state] = state
	}
	rows.Close()
	if len(states) != 4 {
		t.Fatal("seeded states lost", states)
	}
	if got := musicAcquisitionIssues(t, db); got != 2 {
		t.Fatal("issues must count only the unacknowledged failed/partial jobs", got)
	}
	// Acknowledging a job after the upgrade clears the warning while the
	// diagnostic history stays.
	if _, err = db.Exec(`UPDATE music_acquisition_jobs SET acknowledged_at=unixepoch() WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if got := musicAcquisitionIssues(t, db); got != 1 {
		t.Fatal("acknowledged job still counts as an issue", got)
	}

	// A repeated startup (second Migrate call) is a no-op: the ledger is
	// unchanged, the column stays and the acknowledgment survives.
	before := youtube2Ledger(t, db)
	if err = Migrate(ctx, db); err != nil {
		t.Fatal("second migrate must be a no-op, not an error", err)
	}
	after := youtube2Ledger(t, db)
	if len(before) != len(after) || len(after) != len(names) {
		t.Fatal("ledger changed on re-run", len(before), len(after), len(names))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatal("ledger row changed on re-run", before[i], after[i])
		}
	}
	var acknowledged int
	if err = db.QueryRow(`SELECT acknowledged_at FROM music_acquisition_jobs WHERE id=1`).Scan(&acknowledged); err != nil || acknowledged == 0 {
		t.Fatal("acknowledgment lost on re-run", acknowledged, err)
	}
	if got := musicAcquisitionIssues(t, db); got != 1 {
		t.Fatal("issue count changed on re-run", got)
	}
}

// Migration 102 upgrades only the byte-identical 093 default dub rule, bumps
// its revision like a saved edit, and is a no-op on a repeated startup. A
// near-miss rule (whitespace-only difference) is treated as customized and
// must not be touched.
func TestYouTube2AnimeEnglishAudioUpgradeRerunAndNearMiss(t *testing.T) {
	ctx := context.Background()

	// Default-rule installation: upgraded, then a re-run changes nothing.
	db, names, _ := youtube2UpgradeFixture(t, "102_")
	var rule string
	var revision int
	if err := db.QueryRow(`SELECT dub_rule,revision FROM anime_preferences WHERE id=1`).Scan(&rule, &revision); err != nil {
		t.Fatal(err)
	}
	if rule == quality.DefaultDubRule {
		t.Fatal("fixture already uses the new default")
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT dub_rule,revision FROM anime_preferences WHERE id=1`).Scan(&rule, &revision); err != nil || rule != quality.DefaultDubRule || revision != 3 {
		t.Fatal("default rule not upgraded", revision, err)
	}
	before := youtube2Ledger(t, db)
	if err := Migrate(ctx, db); err != nil {
		t.Fatal("second migrate must be a no-op, not an error", err)
	}
	if after := youtube2Ledger(t, db); len(before) != len(after) {
		t.Fatal("ledger changed on re-run", len(before), len(after))
	}
	if err := db.QueryRow(`SELECT dub_rule,revision FROM anime_preferences WHERE id=1`).Scan(&rule, &revision); err != nil || rule != quality.DefaultDubRule || revision != 3 {
		t.Fatal("re-run changed the upgraded rule", revision, err)
	}
	if version, err := Version(ctx, db); err != nil || version != len(names) {
		t.Fatal("unexpected schema version", version, len(names), err)
	}
	db.Close()

	// Near-miss: a whitespace-only difference from the 093 default is a
	// customized rule and must survive the upgrade untouched.
	db, _, _ = youtube2UpgradeFixture(t, "102_")
	defer db.Close()
	var original string
	if err := db.QueryRow(`SELECT dub_rule FROM anime_preferences WHERE id=1`).Scan(&original); err != nil {
		t.Fatal(err)
	}
	nearMiss := original + " "
	if _, err := db.Exec(`UPDATE anime_preferences SET dub_rule=?, revision=2 WHERE id=1`, nearMiss); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT dub_rule,revision FROM anime_preferences WHERE id=1`).Scan(&rule, &revision); err != nil || rule != nearMiss || revision != 2 {
		t.Fatal("near-miss rule was upgraded", revision, err)
	}
}

// The full 101/102 chain on a populated database must leave YouTube series
// sources, entries, manual matches and their catalog references byte-identical
// while the music acknowledgment column arrives with usable defaults. This
// complements the 094 rebuild test by proving the two newest migrations do not
// disturb the YouTube tables they do not touch.
func TestYouTube2PopulatedUpgradePreservesYouTubeSourcesAndMusicJobs(t *testing.T) {
	ctx := context.Background()
	db, names, _ := youtube2UpgradeFixture(t, "101_")
	for _, query := range []string{
		`INSERT INTO media_items(id,kind,tmdb_id,title,year,status,monitored) VALUES(1,'series',1,'Fixture Show',2024,'missing',1)`,
		`INSERT INTO series_details(media_id) VALUES(1)`,
		`INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored) VALUES(1,1,1,1,'Season 1','','',2,1)`,
		`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(1,1,11,1,'One','','',1)`,
		`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(2,1,12,2,'Two','','',1)`,
		`INSERT INTO youtube_series_sources(id,media_id,source_url,title,mapping,season_number,start_episode,quality,subtitles_json,auto_import,enabled,interval_hours,initialized,revision,requested,next_run,last_checked,state,message,target_snapshot,created_at,lease_owner,lease_until) VALUES('src-1',1,'https://www.youtube.com/playlist?list=PLupgrade','Fixture Show','episode',1,1,'1080','{"languages":["en"],"automatic":true,"quality":"1080"}',1,1,12,1,5,0,4444,3333,'idle','kept','{"Title":"Fixture Show"}',2222,'owner-1',5555)`,
		`INSERT INTO youtube_series_entries(source_id,video_id,position,title,channel,state,episode_id,job_id,note,discovered_at) VALUES('src-1','aaaaaaaaaaa',0,'S01E01 — One','Fixture','queued',1,NULL,'S01E01 — One',111)`,
		`INSERT INTO youtube_series_matches(source_id,video_id,episode_id,position,title,created_at) VALUES('src-1','bbbbbbbbbbb',2,1,'X1',222)`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(query, err)
		}
	}
	youtube2SeedJob(t, db, 1, "Failed partial", "failed", 2, 0)
	youtube2SeedJob(t, db, 2, "Completed partial", "completed", 2, 1)

	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if version, err := Version(ctx, db); err != nil || version != len(names) {
		t.Fatal("unexpected schema version", version, len(names), err)
	}
	var mapping, quality, subtitles, state, message, snapshot, owner, url string
	var season, start, interval, initialized, revision, requested, nextRun, lastChecked, autoImport, enabled, created, leaseUntil int
	if err := db.QueryRow(`SELECT mapping,quality,subtitles_json,state,message,target_snapshot,lease_owner,lease_until,source_url,season_number,start_episode,interval_hours,initialized,revision,requested,next_run,last_checked,auto_import,enabled,created_at FROM youtube_series_sources WHERE id='src-1'`).Scan(&mapping, &quality, &subtitles, &state, &message, &snapshot, &owner, &leaseUntil, &url, &season, &start, &interval, &initialized, &revision, &requested, &nextRun, &lastChecked, &autoImport, &enabled, &created); err != nil {
		t.Fatal("existing source lost", err)
	}
	if mapping != "episode" || quality != "1080" || state != "idle" || message != "kept" || snapshot != `{"Title":"Fixture Show"}` || owner != "owner-1" || leaseUntil != 5555 || revision != 5 || initialized != 1 || enabled != 1 {
		t.Fatal("source fields changed", mapping, quality, state, message, snapshot, owner, leaseUntil, revision)
	}
	var entryState, note, matchTitle string
	var entryEpisode, matchEpisode int64
	if err := db.QueryRow(`SELECT state,episode_id,note FROM youtube_series_entries WHERE source_id='src-1' AND video_id='aaaaaaaaaaa'`).Scan(&entryState, &entryEpisode, &note); err != nil || entryState != "queued" || entryEpisode != 1 || note != "S01E01 — One" {
		t.Fatal("entry changed", entryState, entryEpisode, note, err)
	}
	if err := db.QueryRow(`SELECT episode_id,title FROM youtube_series_matches WHERE source_id='src-1' AND video_id='bbbbbbbbbbb'`).Scan(&matchEpisode, &matchTitle); err != nil || matchEpisode != 2 || matchTitle != "X1" {
		t.Fatal("manual match changed", matchEpisode, matchTitle, err)
	}
	if got := musicAcquisitionIssues(t, db); got != 2 {
		t.Fatal("music jobs lost their attention state", got)
	}
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign key consistency lost after the full-chain upgrade")
	}
}
