package database

import (
	"context"
	"database/sql"
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestTransmissionMigrationPreservesClientsAndDefaults(t *testing.T) {
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
	for _, name := range names[:39] {
		b, _ := fs.ReadFile(migrationFiles, name)
		files[name] = &fstest.MapFile{Data: b}
	}
	if e = migrate(ctx, db, files); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO download_clients VALUES('original','Original client','qbittorrent',1,7,100,200); UPDATE download_client_preferences SET torrent='original',revision=8; INSERT INTO integration_configs(name,ciphertext) VALUES('opensubtitles',x'010203')`); e != nil {
		t.Fatal(e)
	}
	if e = Migrate(ctx, db); e != nil {
		t.Fatal(e)
	}
	var name, kind, id string
	var enabled, revision, created, updated int
	if e = db.QueryRow(`SELECT name,kind,enabled,revision,created_at,updated_at FROM download_clients WHERE id='original'`).Scan(&name, &kind, &enabled, &revision, &created, &updated); e != nil || name != "Original client" || kind != "qbittorrent" || enabled != 1 || revision != 7 || created != 100 || updated != 200 {
		t.Fatal("client changed", e)
	}
	if e = db.QueryRow(`SELECT torrent,revision FROM download_client_preferences`).Scan(&id, &revision); e != nil || id != "original" || revision != 8 {
		t.Fatal("defaults changed", e)
	}
	if _, e = db.Exec(`INSERT INTO download_clients VALUES('new','New client','transmission',0,1,100,200)`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO download_clients VALUES('bad','Bad client','unsupported',0,1,100,200)`); e == nil {
		t.Fatal("kind guard lost")
	}
}
