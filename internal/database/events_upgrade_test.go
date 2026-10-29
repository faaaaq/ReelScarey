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

func TestEventUpgradeBackfillsRecordedFactsWithoutInventingTransitions(t *testing.T) {
	ctx := context.Background()
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	old := fstest.MapFS{}
	names, e := fs.Glob(migrationFiles, "migrations/*.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range names {
		if filepath.Base(name) >= "027_" {
			continue
		}
		b, e := fs.ReadFile(migrationFiles, name)
		if e != nil {
			t.Fatal(e)
		}
		old[name] = &fstest.MapFile{Data: b}
	}
	if e = migrate(ctx, db, old); e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,created_at) VALUES(1,'movie',42,'Historic title','2020-01-02T03:04:05Z');INSERT INTO request_accounts(id,username,password_hash,created_at) VALUES(1,'fixture','unused',1577934245);INSERT INTO media_requests(id,account_id,kind,tmdb_id,title,state,created_at,updated_at) VALUES(1,1,'movie',42,'Historic title','approved',1577934245,1578020645)`)
	if e != nil {
		t.Fatal(e)
	}
	if e = Migrate(ctx, db); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='title' AND state='added' AND occurred_at=1577934245`).Scan(&count); e != nil || count != 1 {
		t.Fatal("title time not preserved", count, e)
	}
	if e = db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='request' AND state IN ('historical record created','historical state: approved')`).Scan(&count); e != nil || count != 2 {
		t.Fatal("request facts missing", count, e)
	}
	if e = db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='request' AND state='pending'`).Scan(&count); e != nil || count != 0 {
		t.Fatal("invented request transition", count, e)
	}
}
func TestExpandedHistoryCapturesChangesWithoutCredentials(t *testing.T) {
	ctx := context.Background()
	db, e := Open(ctx, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	secret := "private-test-cookie-never-display"
	_, e = db.Exec(`INSERT INTO integration_configs(name,ciphertext) VALUES('qbittorrent',?);INSERT INTO request_accounts(id,username,password_hash,created_at) VALUES(1,'fixture','secret-hash',unixepoch());INSERT INTO request_lists(id,account_id,provider,list_id,ciphertext) VALUES(1,1,'trakt','private-identifier',?);UPDATE request_accounts SET disabled=1 WHERE id=1;UPDATE request_lists SET revision=revision+1 WHERE id=1`, []byte(secret), []byte(secret))
	if e != nil {
		t.Fatal(e)
	}
	rows, e := db.Query(`SELECT kind,title,state FROM activity_events`)
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	found := map[string]bool{}
	for rows.Next() {
		var kind, title, state string
		if e = rows.Scan(&kind, &title, &state); e != nil {
			t.Fatal(e)
		}
		if strings.Contains(title+state, secret) || strings.Contains(title+state, "private-identifier") || strings.Contains(title+state, "secret-hash") {
			t.Fatal("sensitive data leaked into history")
		}
		found[kind+":"+state] = true
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	for _, v := range []string{"connection:connection saved", "request_account:request account created", "request_account:request account disabled", "request_list:import list added", "request_list:import list updated"} {
		if !found[v] {
			t.Fatal("missing event", v, found)
		}
	}
}
