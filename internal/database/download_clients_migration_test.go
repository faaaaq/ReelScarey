package database

import (
	"bytes"
	"context"
	"database/sql"
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestDownloadClientMigrationPreservesLegacyCiphertextAndLedger(t *testing.T) {
	ctx := context.Background()
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, e = db.Exec(`PRAGMA foreign_keys=ON`); e != nil {
		t.Fatal(e)
	}
	files := fstest.MapFS{}
	names, _ := fs.Glob(migrationFiles, "migrations/*.sql")
	for _, name := range names[:37] {
		body, _ := fs.ReadFile(migrationFiles, name)
		files[name] = &fstest.MapFile{Data: body}
	}
	if e = migrate(ctx, db, files); e != nil {
		t.Fatal(e)
	}
	old := []byte{0, 1, 9, 255, 7}
	for _, name := range []string{"qbittorrent", "sabnzbd", "torznab", "opensubtitles", "emby"} {
		if _, e = db.Exec(`INSERT INTO integration_configs(name,ciphertext,updated_at) VALUES(?,?,'original timestamp')`, name, old); e != nil {
			t.Fatal(e)
		}
	}
	var checksum, applied string
	if e = db.QueryRow(`SELECT checksum,applied_at FROM schema_migrations WHERE version=37`).Scan(&checksum, &applied); e != nil {
		t.Fatal(e)
	}
	if e = Migrate(ctx, db); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"qbittorrent", "sabnzbd", "torznab", "opensubtitles", "emby"} {
		var ciphertext []byte
		var timestamp string
		if e = db.QueryRow(`SELECT ciphertext,updated_at FROM integration_configs WHERE name=?`, name).Scan(&ciphertext, &timestamp); e != nil || !bytes.Equal(old, ciphertext) || timestamp != "original timestamp" {
			t.Fatal(name, e)
		}
	}
	var savedChecksum, savedApplied string
	if e = db.QueryRow(`SELECT checksum,applied_at FROM schema_migrations WHERE version=37`).Scan(&savedChecksum, &savedApplied); e != nil || savedChecksum != checksum || savedApplied != applied {
		t.Fatal("released ledger changed", e)
	}
	var torrent, usenet string
	var count int
	if e = db.QueryRow(`SELECT torrent,usenet FROM download_client_preferences`).Scan(&torrent, &usenet); e != nil || torrent != "" || usenet != "" {
		t.Fatal("legacy defaults changed", e)
	}
	if e = db.QueryRow(`SELECT COUNT(*) FROM download_clients`).Scan(&count); e != nil || count != 0 {
		t.Fatal("clients fabricated", e)
	}
	if _, e = db.Exec(`INSERT INTO integration_configs(name,ciphertext) VALUES('download-client:not-a-valid-identifier',?)`, old); e == nil {
		t.Fatal("invalid credential name accepted")
	}
}
