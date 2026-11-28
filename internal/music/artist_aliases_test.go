package music

import (
	"context"
	"errors"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func TestReviewedArtistAliasesReuseOneLocalCatalogArtist(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := NewService(db, t.TempDir())
	files := []ScanFile{
		{Artist: "(hed) p.e.", AlbumArtist: "(hed) p.e.", Album: "Evolution", Title: "The Higher Crown", Track: intPtr(1), Disc: intPtr(1)},
		{Artist: "(hed)p.e.", AlbumArtist: "(hed)p.e.", Album: "Insomnia", Title: "Insomnia", Track: intPtr(1), Disc: intPtr(1)},
		{Artist: "(hed) Planet Earth", AlbumArtist: "(hed) Planet Earth", Album: "Blackout", Title: "Blackout", Track: intPtr(1), Disc: intPtr(1)},
	}
	artistID := int64(0)
	for _, file := range files {
		catalog := newLocalCatalog(service)
		gotArtist, _, _, err := catalog.matchIdentity(ctx, file, tagIdentityFor(file))
		if err != nil {
			t.Fatal(err)
		}
		if artistID == 0 {
			artistID = gotArtist
		} else if gotArtist != artistID {
			t.Fatalf("alias %q created artist %d; want %d", file.Artist, gotArtist, artistID)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_artists`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("saved %d artists for three spellings; want one", count)
	}
}

func TestReviewedArtistAliasRejectsConflictingTaggedMBID(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,created_at,updated_at) VALUES(1,?,'ABK',1,1)`, testMBID); err != nil {
		t.Fatal(err)
	}
	file := ScanFile{Artist: "Anybody Killa", AlbumArtist: "Anybody Killa", Album: "Dirty History", Title: "Song", Track: intPtr(1), Disc: intPtr(1), MusicBrainzArtistID: "00000000-0000-0000-0000-000000000001"}
	assertConflict := func(err error) {
		t.Helper()
		var typed *Error
		if !errors.As(err, &typed) || typed.Code != "music_conflicting_artist_identity" {
			t.Fatalf("conflicting genuine identities were not refused: %v", err)
		}
	}
	service := NewService(db, t.TempDir())
	_, _, _, err = newLocalCatalog(service).matchIdentity(ctx, file, tagIdentityFor(file))
	assertConflict(err)
	var albums int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_albums`).Scan(&albums); err != nil {
		t.Fatal(err)
	}
	if albums != 0 {
		t.Fatalf("refused identity created %d albums", albums)
	}
	if _, err := db.Exec(`INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,created_at,updated_at) VALUES(1,1,'local-album:dirty','Dirty History',1,1);
		INSERT INTO music_tracks(id,album_id,disc,number,title,created_at,updated_at) VALUES(1,1,1,1,'Song',1,1)`); err != nil {
		t.Fatal(err)
	}
	_, _, _, err = service.matchFile(ctx, file)
	assertConflict(err)
}

func TestConsolidateArtistAliasesPreservesOwnerRenamedSurvivor(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,sort_name,created_at,updated_at,name_overridden)
		VALUES(3,'local-artist:one','(hed) Planet Earth','(hed) Planet Earth',1,1,0),
		(4,'local-artist:two','(hed)p.e.','(hed)p.e.',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	report, err := NewService(db, t.TempDir()).ConsolidateArtistAliases(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Merged) != 1 || report.Merged[0].SurvivorID != 4 || report.Merged[0].Name != "(hed)p.e." {
		t.Fatalf("owner-renamed row did not survive: %+v", report)
	}
	var name, sortName string
	var overridden int
	if err := db.QueryRow(`SELECT name,sort_name,name_overridden FROM music_artists WHERE id=4`).Scan(&name, &sortName, &overridden); err != nil {
		t.Fatal(err)
	}
	if name != "(hed)p.e." || sortName != name || overridden != 1 {
		t.Fatalf("owner override was not preserved: %q %q %d", name, sortName, overridden)
	}
}

func TestConsolidateArtistAliasesPreservesCatalogReferences(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`
		INSERT INTO music_artists(id,musicbrainz_id,name,sort_name,monitored,status,created_at,updated_at)
		VALUES(3,'local-artist:hedpe','(hed) p.e.','(hed) p.e.',0,'available',10,10),
			(4,'local-artist:hedplanetearth','(hed) Planet Earth','(hed) Planet Earth',1,'available',11,11);
		INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at)
		VALUES(10,3,'local-album:evolution','Evolution',1,'available',10,10),
			(14,4,'local-album:insomnia','Insomnia',1,'available',11,11);
		INSERT INTO music_tracks(id,album_id,disc,number,title,created_at,updated_at)
		VALUES(113,10,1,1,'The Higher Crown',10,10),(117,14,1,1,'Insomnia',11,11);
		INSERT INTO music_files(id,artist_id,album_id,track_id,root,relative_path,size_bytes,imported_at)
		VALUES(1,3,10,113,'Music','hed/evolution.flac',100,10),(2,4,14,117,'Music','hed/insomnia.flac',100,11);
		INSERT INTO music_scans(id,root,state,created_at,updated_at) VALUES(1,'Music','completed',10,10);
		INSERT INTO music_scan_files(id,scan_id,relative_path,size_bytes,modified_at,artist_id,album_id,track_id,state,created_at,updated_at)
		VALUES(1,1,'hed/evolution.flac',100,10,3,10,113,'registered',10,10),
			(2,1,'hed/insomnia.flac',100,11,4,14,117,'registered',11,11);
		INSERT INTO music_server_users(id,username,password_sealed,created_at,updated_at) VALUES(1,'alice',X'00',1,1);
		INSERT INTO music_playback_marks(user_id,kind,object_id,starred_at,rating,updated_at)
		VALUES(1,'artist',3,0,2,10),(1,'artist',4,20,5,20);
		INSERT INTO music_upgrade_jobs(id,scope,scope_id,scope_name,target_quality,rate_per_hour,state,created_at,updated_at)
		VALUES(1,'artist',4,'(hed) Planet Earth','lossless',200,'completed',10,20);
		INSERT INTO music_upgrade_items(id,job_id,track_id,music_file_id,artist_id,album_id,title,artist,album,state,created_at,updated_at)
		VALUES(1,1,117,2,4,14,'Insomnia','(hed) Planet Earth','Insomnia','skipped',10,20);
		UPDATE music_title_updates SET revision=7,next_attempt=123 WHERE music_file_id=1;
		DELETE FROM music_title_updates WHERE music_file_id=2;
	`)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(db, t.TempDir())
	report, err := service.ConsolidateArtistAliases(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Merged) != 1 || len(report.Skipped) != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
	merged := report.Merged[0]
	if merged.SurvivorID != 3 || len(merged.MergedIDs) != 1 || merged.MergedIDs[0] != 4 || merged.Name != "(hed) p.e." {
		t.Fatalf("unexpected merge: %+v", merged)
	}
	var artistCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_artists`).Scan(&artistCount); err != nil {
		t.Fatal(err)
	}
	if artistCount != 1 {
		t.Fatalf("artist count = %d", artistCount)
	}
	var name, sortName, status string
	var monitored int
	if err := db.QueryRow(`SELECT name,sort_name,monitored,status FROM music_artists WHERE id=3`).Scan(&name, &sortName, &monitored, &status); err != nil {
		t.Fatal(err)
	}
	if name != "(hed) p.e." || sortName != name || monitored != 1 || status != "available" {
		t.Fatalf("survivor = %q %q monitored=%d status=%q", name, sortName, monitored, status)
	}
	for _, table := range []string{"music_albums", "music_files", "music_scan_files", "music_upgrade_items"} {
		var wrong int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table + ` WHERE artist_id<>3`).Scan(&wrong); err != nil {
			t.Fatal(err)
		}
		if wrong != 0 {
			t.Errorf("%s retained %d references to a removed artist", table, wrong)
		}
	}
	var scopeID, itemArtist int64
	if err := db.QueryRow(`SELECT scope_id FROM music_upgrade_jobs WHERE id=1`).Scan(&scopeID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT artist_id FROM music_upgrade_items WHERE id=1`).Scan(&itemArtist); err != nil {
		t.Fatal(err)
	}
	if scopeID != 3 || itemArtist != 3 {
		t.Fatalf("upgrade references = scope %d item artist %d", scopeID, itemArtist)
	}
	var starredAt, rating, updatedAt int64
	if err := db.QueryRow(`SELECT starred_at,rating,updated_at FROM music_playback_marks WHERE user_id=1 AND kind='artist' AND object_id=3`).Scan(&starredAt, &rating, &updatedAt); err != nil {
		t.Fatal(err)
	}
	if starredAt != 20 || rating != 5 || updatedAt != 20 {
		t.Fatalf("merged playback mark = starred %d rating %d updated %d", starredAt, rating, updatedAt)
	}
	var markCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_playback_marks WHERE kind='artist'`).Scan(&markCount); err != nil {
		t.Fatal(err)
	}
	if markCount != 1 {
		t.Fatalf("playback mark count = %d", markCount)
	}
	var revision, nextAttempt, queued int64
	if err := db.QueryRow(`SELECT revision,next_attempt FROM music_title_updates WHERE music_file_id=1`).Scan(&revision, &nextAttempt); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_title_updates`).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if revision != 7 || nextAttempt != 123 || queued != 1 {
		t.Fatalf("consolidation changed metadata work queue: revision=%d next=%d queued=%d", revision, nextAttempt, queued)
	}
	if rows, err := db.Query(`PRAGMA foreign_key_check`); err != nil {
		t.Fatal(err)
	} else if rows.Next() {
		rows.Close()
		t.Fatal("merge left a foreign-key violation")
	} else {
		rows.Close()
	}
	second, err := service.ConsolidateArtistAliases(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Merged) != 0 || len(second.Skipped) != 0 {
		t.Fatalf("second consolidation was not idempotent: %+v", second)
	}
}

func TestConsolidateArtistAliasesSkipsUnsafeGroups(t *testing.T) {
	tests := []struct {
		name  string
		setup string
	}{
		{
			name: "overlapping album titles",
			setup: `INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,created_at,updated_at)
				VALUES(1,3,'local-album:one','Evolution',1,1),(2,4,'local-album:two','Evolution',1,1);`,
		},
		{
			name: "awaiting review scan",
			setup: `INSERT INTO music_scans(id,root,state,created_at,updated_at) VALUES(1,'Music','review',1,1);
				INSERT INTO music_scan_files(scan_id,relative_path,size_bytes,modified_at,artist_id,state,created_at,updated_at)
				VALUES(1,'song.flac',1,1,4,'matched',1,1);`,
		},
		{
			name: "running tag edit",
			setup: `INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,created_at,updated_at) VALUES(1,4,'local-album:one','Insomnia',1,1);
				INSERT INTO music_tracks(id,album_id,disc,number,title,created_at,updated_at) VALUES(1,1,1,1,'Insomnia',1,1);
				INSERT INTO music_files(id,artist_id,album_id,track_id,root,relative_path,size_bytes,imported_at) VALUES(1,4,1,1,'Music','song.flac',1,1);
				INSERT INTO music_edit_jobs(id,state,file_count,created_at,updated_at) VALUES(1,'running',1,1,1);
				INSERT INTO music_edit_files(job_id,music_file_id,source_path,destination_path,action,state,created_at,updated_at)
				VALUES(1,1,'song.flac','song.flac','retag','pending',1,1);`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, err := database.Open(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,created_at,updated_at)
				VALUES(3,'local-artist:hedpe','(hed) p.e.',1,1),(4,'local-artist:hedplanetearth','(hed) Planet Earth',1,1);` + tc.setup); err != nil {
				t.Fatal(err)
			}
			report, err := NewService(db, t.TempDir()).ConsolidateArtistAliases(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Merged) != 0 || len(report.Skipped) != 1 || report.Skipped[0].Reason == "" {
				t.Fatalf("unsafe group was not skipped explicitly: %+v", report)
			}
			var count int
			if err := db.QueryRow(`SELECT COUNT(*) FROM music_artists`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 2 {
				t.Fatalf("unsafe merge changed artist count to %d", count)
			}
		})
	}
}
