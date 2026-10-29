package database

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestReviewMappedAnimeMigrationPreservesLegacyConsentWithoutApprovingOrders(t *testing.T) {
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
	prior := fstest.MapFS{}
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if filepath.Base(name) >= "015_" {
			continue
		}
		body, err := fs.ReadFile(migrationFiles, name)
		if err != nil {
			t.Fatal(err)
		}
		prior[name] = &fstest.MapFile{Data: body}
	}
	if err = migrate(ctx, db, prior); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO media_items(id,kind,tmdb_id,title,category,monitored) VALUES(1,'series',100,'Existing disabled','anime',1),(2,'series',200,'Existing season','anime',1),(3,'series',300,'Existing absolute','anime',1)`,
		`INSERT INTO series_details(media_id,revision) VALUES(1,4),(2,5),(3,6)`,
		`INSERT INTO anime_search_settings(media_id,mode,search_title,alternate_titles,revision) VALUES(1,'disabled','Retained disabled alias','["Retained disabled alias"]',7),(2,'season','Retained season alias','["Retained season alias"]',8),(3,'absolute','Retained absolute alias','["Retained absolute alias"]',9)`,
		`INSERT INTO anime_orders(media_id,revision) VALUES(1,2),(2,3),(3,4)`,
		`INSERT INTO release_candidates(id,movie_id,title,size_bytes,published_at,payload,expires_at,scope) VALUES('release',3,'Original release',1024,'',X'0102',1,'tv:2:1')`,
		`INSERT INTO downloads(id,movie_id,release_id,title,info_hash,payload,client_url,save_path,state,created_at,updated_at,kind,anime_search_snapshot,anime_order_snapshot) VALUES('queued',3,'release','Original release','original-hash',X'0304','https://unused.invalid','/downloads','queued',1,2,'series','{"mode":"absolute","policy_revision":9}','{"revision":4,"immutable":"order labels"}')`,
	} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct{ q, want string }{
		{`SELECT mode||':'||search_title||':'||alternate_titles||':'||revision||':'||include_specials||':'||allow_packs||':'||max_pack_episodes||':'||approved_order_revision FROM anime_search_settings WHERE media_id=1`, `disabled:Retained disabled alias:["Retained disabled alias"]:7:0:0:20:0`},
		{`SELECT mode||':'||search_title||':'||alternate_titles||':'||revision||':'||include_specials||':'||allow_packs||':'||max_pack_episodes||':'||approved_order_revision FROM anime_search_settings WHERE media_id=2`, `season:Retained season alias:["Retained season alias"]:8:0:0:20:0`},
		{`SELECT mode||':'||search_title||':'||alternate_titles||':'||revision||':'||include_specials||':'||allow_packs||':'||max_pack_episodes||':'||approved_order_revision FROM anime_search_settings WHERE media_id=3`, `absolute:Retained absolute alias:["Retained absolute alias"]:9:0:0:20:0`},
		{`SELECT anime_search_snapshot||':'||anime_order_snapshot||':'||state||':'||hex(payload) FROM downloads WHERE id='queued'`, `{"mode":"absolute","policy_revision":9}:{"revision":4,"immutable":"order labels"}:queued:0304`},
		{`SELECT revision FROM anime_orders WHERE media_id=3`, "4"},
		{`SELECT revision FROM series_details WHERE media_id=3`, "6"},
	} {
		var got string
		if err = db.QueryRow(check.q).Scan(&got); err != nil || got != check.want {
			t.Fatal("mapped migration changed previous authorization/evidence", check.q, got, check.want, err)
		}
	}
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() || rows.Err() != nil {
		t.Fatal("mapped migration damaged references", rows.Err())
	}
}
