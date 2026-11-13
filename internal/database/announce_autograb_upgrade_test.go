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

// Migration 089 turns IRC announce capture into opt-in automation. Existing
// sources and their captured events must survive as capture-only history: the
// rebuilt event table keeps every id and source reference, while every older
// row is marked done/capture_only so no automatic grab is retroactively
// authorized. The test runs against a disposable directory so the upgrade runs
// through the same file-backed runner the application uses.
func TestAnnounceAutograbUpgradePreservesCaptureOnlyHistory(t *testing.T) {
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
		if filepath.Base(name) >= "089_" {
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
	// One pre-existing owner-enabled source and two captures with explicit,
	// non-sequential ids: the rebuild must preserve both the ids and the
	// source_id foreign references exactly.
	for _, query := range []string{
		`INSERT INTO announce_sources(id,tracker,name,enabled,revision,settings,last_error,last_event_at,created_at,updated_at) VALUES('src-ipt','iptorrents','IPT announce',1,4,X'0102','last failure',111,100,200)`,
		`INSERT INTO announce_events(id,source_id,tracker,release_name,category,size_bytes,freeleech,torrent_id,info_url,dedupe,received_at) VALUES(5,'src-ipt','iptorrents','Older capture','movie',2048,'free','torrent-5','https://tracker/5','dedupe-5',1000)`,
		`INSERT INTO announce_events(id,source_id,tracker,release_name,category,size_bytes,freeleech,torrent_id,info_url,dedupe,received_at) VALUES(9,'src-ipt','iptorrents','Newer capture','tv',4096,'','torrent-9','https://tracker/9','dedupe-9',2000)`,
	} {
		if _, err = db.Exec(query); err != nil {
			t.Fatal(query, err)
		}
	}
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if version, err := Version(ctx, db); err != nil || version != 96 {
		t.Fatal("unexpected schema version", version, err)
	}
	// The source stays enabled for capture, but auto_grab defaults off so the
	// upgrade never opts an existing tracker into automatic grabbing.
	var autoGrab, enabled, revision, lastEvent int
	var tracker, name, lastError string
	var settings []byte
	if err = db.QueryRow(`SELECT auto_grab,enabled,revision,last_event_at,tracker,name,last_error,settings FROM announce_sources WHERE id='src-ipt'`).Scan(&autoGrab, &enabled, &revision, &lastEvent, &tracker, &name, &lastError, &settings); err != nil {
		t.Fatal("existing source lost", err)
	}
	if autoGrab != 0 || enabled != 1 || revision != 4 || lastEvent != 111 || tracker != "iptorrents" || name != "IPT announce" || lastError != "last failure" || len(settings) != 2 {
		t.Fatal("existing source changed", autoGrab, enabled, revision, lastEvent, tracker, name, lastError, settings)
	}
	type captured struct {
		id         int
		source     string
		tracker    string
		release    string
		category   string
		size       int
		freeleech  string
		torrentID  string
		infoURL    string
		dedupe     string
		receivedAt int
		revision   int
		state      string
		outcome    string
		message    string
		attempts   int
		nextRun    sql.NullInt64
		leaseOwner sql.NullString
		leaseUntil sql.NullInt64
		processed  sql.NullInt64
		downloadID sql.NullString
	}
	var got []captured
	rows, err := db.Query(`SELECT id,source_id,tracker,release_name,category,size_bytes,freeleech,torrent_id,info_url,dedupe,received_at,source_revision,processing_state,outcome,message,attempts,next_run,lease_owner,lease_until,processed_at,download_id FROM announce_events ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var c captured
		if err = rows.Scan(&c.id, &c.source, &c.tracker, &c.release, &c.category, &c.size, &c.freeleech, &c.torrentID, &c.infoURL, &c.dedupe, &c.receivedAt, &c.revision, &c.state, &c.outcome, &c.message, &c.attempts, &c.nextRun, &c.leaseOwner, &c.leaseUntil, &c.processed, &c.downloadID); err != nil {
			t.Fatal(err)
		}
		got = append(got, c)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatal("captured history was not preserved", len(got))
	}
	first, second := got[0], got[1]
	if first.id != 5 || second.id != 9 {
		t.Fatal("event ids changed", first.id, second.id)
	}
	if first.source != "src-ipt" || second.source != "src-ipt" {
		t.Fatal("source reference changed", first.source, second.source)
	}
	if first.release != "Older capture" || first.category != "movie" || first.size != 2048 || first.freeleech != "free" || first.torrentID != "torrent-5" || first.infoURL != "https://tracker/5" || first.dedupe != "dedupe-5" || first.receivedAt != 1000 {
		t.Fatal("older capture fields changed", first)
	}
	if second.release != "Newer capture" || second.category != "tv" || second.size != 4096 || second.freeleech != "" || second.torrentID != "torrent-9" || second.infoURL != "https://tracker/9" || second.dedupe != "dedupe-9" || second.receivedAt != 2000 {
		t.Fatal("newer capture fields changed", second)
	}
	for _, c := range got {
		if c.state != "done" || c.outcome != "capture_only" || c.revision != 0 || c.message != "" || c.attempts != 0 {
			t.Fatal("historic row was not marked capture-only", c.id, c.state, c.outcome, c.revision, c.message, c.attempts)
		}
		if c.nextRun.Valid || c.leaseOwner.Valid || c.leaseUntil.Valid || c.downloadID.Valid {
			t.Fatal("historic row gained an automation intent", c.id, c.nextRun, c.leaseOwner, c.leaseUntil, c.downloadID)
		}
		if !c.processed.Valid || int(c.processed.Int64) != c.receivedAt {
			t.Fatal("processed_at should mirror received_at", c.id, c.processed, c.receivedAt)
		}
	}
	// The rebuilt table keeps its unique capture guard and foreign key.
	if _, err = db.Exec(`INSERT INTO announce_events(source_id,tracker,release_name,dedupe,received_at) VALUES('src-ipt','iptorrents','Duplicate dedupe','dedupe-5',3000)`); err == nil {
		t.Fatal("duplicate source/dedupe accepted after rebuild")
	}
	if _, err = db.Exec(`INSERT INTO announce_events(source_id,tracker,release_name,dedupe,received_at) VALUES('missing-source','iptorrents','Orphan','dedupe-orphan',3000)`); err == nil {
		t.Fatal("orphan source reference accepted after rebuild")
	}
	if _, err = db.Exec(`UPDATE announce_sources SET auto_grab=2 WHERE id='src-ipt'`); err == nil {
		t.Fatal("auto_grab accepted a value outside 0/1")
	}
	if _, err = db.Exec(`INSERT INTO announce_events(source_id,tracker,release_name,dedupe,received_at,processing_state) VALUES('src-ipt','iptorrents','Bad state','dedupe-state',3000,'unknown')`); err == nil {
		t.Fatal("unknown processing state accepted after rebuild")
	}
	rows, err = db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign key consistency lost after rebuild")
	}
}

// Migration 089 rebuilds announce_events with AUTOINCREMENT so the retention
// prune can never recycle a terminal event id that a durable release or
// candidate already references. The high-water mark must also survive a reopen
// of the disposable database.
func TestAnnounceEventIDsAreNotReusedAfterPruning(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	// db is reassigned after the reopen, so cleanup must resolve it at the end.
	t.Cleanup(func() { _ = db.Close() })
	var ddl string
	if err = db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='announce_events'`).Scan(&ddl); err != nil || !strings.Contains(ddl, "AUTOINCREMENT") {
		t.Fatal("announce_events was not rebuilt with AUTOINCREMENT", ddl, err)
	}
	if _, err = db.Exec(`INSERT INTO announce_sources(id,tracker,name,enabled,revision,settings,created_at,updated_at) VALUES('src','iptorrents','IPT',1,1,X'00',1,1)`); err != nil {
		t.Fatal(err)
	}
	// Seed a retained audit row plus a terminal (highest-id) done capture that
	// the retention prune then removes, exactly as the announce service does.
	if _, err = db.Exec(`INSERT INTO announce_events(id,source_id,tracker,release_name,dedupe,received_at) VALUES(7,'src','iptorrents','Retained audit','dedupe-keep',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO announce_events(id,source_id,tracker,release_name,dedupe,received_at) VALUES(8,'src','iptorrents','Pruned terminal','dedupe-prune',2)`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`DELETE FROM announce_events WHERE id=8`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO announce_events(source_id,tracker,release_name,dedupe,received_at) VALUES('src','iptorrents','New capture','dedupe-new',3)`); err != nil {
		t.Fatal(err)
	}
	var next int
	if err = db.QueryRow(`SELECT id FROM announce_events WHERE dedupe='dedupe-new'`).Scan(&next); err != nil {
		t.Fatal(err)
	}
	if next != 9 {
		t.Fatal("pruned terminal id was recycled", next)
	}
	// The AUTOINCREMENT high-water mark is durable: reopening the disposable
	// database must not let a later prune hand out an id already used.
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`DELETE FROM announce_events WHERE id=9`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO announce_events(source_id,tracker,release_name,dedupe,received_at) VALUES('src','iptorrents','After reopen','dedupe-reopen',4)`); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT id FROM announce_events WHERE dedupe='dedupe-reopen'`).Scan(&next); err != nil {
		t.Fatal(err)
	}
	if next != 10 {
		t.Fatal("pruned terminal id was recycled after reopen", next)
	}
	var seq int
	if err = db.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name='announce_events'`).Scan(&seq); err != nil || seq != 10 {
		t.Fatal("autoincrement high-water mark not durable", seq, err)
	}
}
