package database

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestCentralAccountsUpgradePreservesProfilesAndDoesNotMergeNames(t *testing.T) {
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
		if filepath.Base(name) >= "116_" {
			continue
		}
		body, readErr := fs.ReadFile(migrationFiles, name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		old[name] = &fstest.MapFile{Data: body}
	}
	if err = migrate(ctx, db, old); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO request_accounts(id,username,password_hash,disabled,created_at) VALUES
		(7,'shared-name','request-hash',0,10),(8,'request-only','other-hash',0,11);
	INSERT INTO music_server_users(id,username,password_sealed,created_at,updated_at) VALUES
		(9,'shared-name',X'010203',12,12),(10,'music-only',X'040506',13,13);
	INSERT INTO media_requests(id,account_id,kind,tmdb_id,title,state,created_at,updated_at) VALUES
		(20,7,'movie',100,'Owned request','pending',14,14);
	INSERT INTO music_playback_marks(user_id,kind,object_id,rating,updated_at) VALUES
		(9,'song',30,5,15)`)
	if err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("central account count = %d, %v", count, err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM accounts WHERE username='shared-name'`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("same-named legacy identities were merged: %d, %v", count, err)
	}
	var requestID, musicID int64
	var hash string
	var requests, player, subsonic bool
	if err = db.QueryRow(`SELECT request_account_id,COALESCE(music_server_user_id,0),password_hash,requests_access,music_player_access,subsonic_access FROM accounts WHERE request_account_id=7`).Scan(&requestID, &musicID, &hash, &requests, &player, &subsonic); err != nil {
		t.Fatal(err)
	}
	if requestID != 7 || musicID != 0 || hash != "request-hash" || !requests || player || subsonic {
		t.Fatalf("request profile changed: %d %d %q %v %v %v", requestID, musicID, hash, requests, player, subsonic)
	}
	var sealed []byte
	if err = db.QueryRow(`SELECT music_server_user_id,password_hash,password_sealed,subsonic_access FROM accounts WHERE music_server_user_id=9`).Scan(&musicID, &hash, &sealed, &subsonic); err != nil {
		t.Fatal(err)
	}
	if musicID != 9 || hash != "" || string(sealed) != "" || !subsonic {
		t.Fatalf("music profile changed: %d %q %x %v", musicID, hash, sealed, subsonic)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM media_requests r JOIN accounts a ON a.request_account_id=r.account_id WHERE r.id=20 AND a.request_account_id=7`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("request ownership changed: %d, %v", count, err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM music_playback_marks m JOIN accounts a ON a.music_server_user_id=m.user_id WHERE m.user_id=9 AND m.rating=5`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("playback ownership changed: %d, %v", count, err)
	}
	if _, err = db.Exec(`INSERT INTO accounts(username,password_hash,created_at,updated_at) VALUES('shared-name','another-browser-hash',1,1)`); err == nil {
		t.Fatal("ambiguous browser username accepted")
	}
}
