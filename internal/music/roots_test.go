package music

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func TestScanRootsReadMultipleMusicFoldersAndRegisterTheirOwnRoot(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	media := t.TempDir()
	for _, root := range []string{"Music", "Music Archive"} {
		dir := filepath.Join(media, root, "Artist", "Album")
		if err = os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		name := filepath.Join(dir, "01 - Song.flac")
		if err = os.WriteFile(name, []byte("fixture audio "+root), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES(1,?, 'Artist',1,'missing',1,1)`, testMBID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(2,1,?, 'Album',1,'missing',1,1)`, testRGID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO music_tracks(id,album_id,musicbrainz_id,disc,number,title,created_at,updated_at) VALUES(3,2,?,1,1,'Song',1,1)`, testTrackID); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, media)
	service.SetProber(fakeProber{
		"01 - Song.flac": {Artist: "Artist", AlbumArtist: "Artist", Album: "Album", Title: "Song", Track: intPtr(1), Disc: intPtr(1), Format: "flac"},
	})
	scan, err := service.StartScanRoots(ctx, []string{"Music", "Music Archive"})
	if err != nil {
		t.Fatal(err)
	}
	scan = waitForScan(t, ctx, service, scan.ID)
	if scan.State != "review" || scan.FileCount != 2 || scan.MatchedCount != 2 || len(scan.Roots) != 2 {
		t.Fatal(scan)
	}
	page, err := service.ScanPage(ctx, scan.ID, 1)
	if err != nil || len(page.Items) != 2 {
		t.Fatal(page, err)
	}
	ids := []int64{}
	for _, file := range page.Items {
		ids = append(ids, file.ID)
	}
	if _, err = service.RegisterScan(ctx, scan.ID, ids); err != nil {
		t.Fatal(err)
	}
	var roots []string
	rows, err := db.Query(`SELECT DISTINCT root FROM music_files ORDER BY root`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var root string
		if err = rows.Scan(&root); err != nil {
			t.Fatal(err)
		}
		roots = append(roots, root)
	}
	if len(roots) != 2 || roots[0] != "Music" || roots[1] != "Music Archive" {
		t.Fatal("registered files did not retain their own root", roots)
	}
}
