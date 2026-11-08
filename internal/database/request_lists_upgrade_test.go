package database

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// The Trakt watchlist migration rebuilds request_lists to widen its provider
// allowlist. Existing rows, IDs and activity triggers must survive unchanged.
func TestRequestListsUpgradePreservesRowsAndTriggers(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
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
		if filepath.Base(name) >= "058_" {
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
		`INSERT INTO request_accounts(id,username,password_hash,created_at) VALUES(1,'viewer','hash',1)`,
		`INSERT INTO request_lists(id,account_id,provider,owner,list_id,enabled,revision) VALUES(7,1,'trakt','viewer','my-list',1,3)`,
	} {
		if _, err = db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var provider, owner, listID string
	var revision int64
	if err = db.QueryRow(`SELECT provider,owner,list_id,revision FROM request_lists WHERE id=7`).Scan(&provider, &owner, &listID, &revision); err != nil {
		t.Fatal("existing list was not preserved", err)
	}
	if provider != "trakt" || owner != "viewer" || listID != "my-list" || revision != 3 {
		t.Fatal("existing list changed", provider, owner, listID, revision)
	}
	if _, err = db.Exec(`INSERT INTO request_lists(account_id,provider,owner,list_id,enabled) VALUES(1,'trakt_watchlist','viewer','watchlist',1)`); err != nil {
		t.Fatal("watchlist provider was not accepted", err)
	}
	if _, err = db.Exec(`INSERT INTO request_lists(account_id,provider,owner,list_id,enabled) VALUES(1,'bogus','viewer','watchlist',1)`); err == nil {
		t.Fatal("unknown provider was accepted")
	}
	var triggers int
	if err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND name LIKE 'event_request_list_%'`).Scan(&triggers); err != nil || triggers != 3 {
		t.Fatal("request list triggers were not restored", triggers, err)
	}
}
