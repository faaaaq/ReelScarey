package database

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestUsenetUpgradePreservesIndexerSecretsAndOldJobProtocol(t *testing.T) {
	ctx := context.Background()
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	old := fstest.MapFS{}
	names, _ := fs.Glob(migrationFiles, "migrations/*.sql")
	for _, n := range names {
		if filepath.Base(n) >= "010_" {
			continue
		}
		b, _ := fs.ReadFile(migrationFiles, n)
		old[n] = &fstest.MapFile{Data: b}
	}
	if e = migrate(ctx, db, old); e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{
		`INSERT INTO integration_configs(name,ciphertext,updated_at) VALUES('qbittorrent',X'0102','old-time'),('opensubtitles',X'0304','subtitle-time')`,
		`INSERT INTO indexers(id,name,kind,enabled,generation,ciphertext,updated_at,revision,last_error) VALUES('old','Existing tracker','iptorrents',0,'old-generation',X'0506',1,3,'offline')`,
		`INSERT INTO media_items(id,kind,tmdb_id,title) VALUES(1,'movie',1,'Existing movie')`,
		`INSERT INTO release_candidates(id,movie_id,title,size_bytes,published_at,payload,expires_at) VALUES('r',1,'release',1,'',X'01',1)`,
		`INSERT INTO downloads(id,movie_id,release_id,title,info_hash,payload,client_url,save_path,state,created_at,updated_at) VALUES('d',1,'r','release','hash',X'02','https://unused.invalid','/unused','queued',1,1)`,
	} {
		if _, e = db.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	if e = Migrate(ctx, db); e != nil {
		t.Fatal(e)
	}
	var protocol, generation, stamp, cipher string
	var revision, enabled int
	if e = db.QueryRow(`SELECT protocol FROM downloads WHERE id='d'`).Scan(&protocol); e != nil || protocol != "torrent" {
		t.Fatal("old download changed protocol", e)
	}
	if e = db.QueryRow(`SELECT generation,revision,enabled,hex(ciphertext) FROM indexers WHERE id='old'`).Scan(&generation, &revision, &enabled, &cipher); e != nil || generation != "old-generation" || revision != 3 || enabled != 0 || cipher != "0506" {
		t.Fatal("tracker state changed", e)
	}
	if e = db.QueryRow(`SELECT updated_at,hex(ciphertext) FROM integration_configs WHERE name='qbittorrent'`).Scan(&stamp, &cipher); e != nil || stamp != "old-time" || cipher != "0102" {
		t.Fatal("existing credential changed", e)
	}
	if _, e = db.Exec(`INSERT INTO integration_configs(name,ciphertext) VALUES('sabnzbd',X'07')`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO indexers(id,name,kind,enabled,generation,ciphertext,updated_at) VALUES('new','Newznab','newznab',1,'new',X'08',1)`); e != nil {
		t.Fatal(e)
	}
}
