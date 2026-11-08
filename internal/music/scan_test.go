package music

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
)

type fakeProber map[string]AudioTags

func (p fakeProber) ProbeAudio(_ context.Context, path string) (AudioTags, error) {
	tags, ok := p[filepath.Base(path)]
	if !ok {
		return AudioTags{}, errors.New("unreadable fixture")
	}
	return tags, nil
}

func TestScanMatchesRegistersWithoutMoving(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	media := t.TempDir()
	dir := filepath.Join(media, "Music", "Artist", "Album")
	if err = os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	matchedPath := filepath.Join(dir, "01 - Song.flac")
	unknownPath := filepath.Join(dir, "02 - Other.mp3")
	for _, name := range []string{matchedPath, unknownPath} {
		if err = os.WriteFile(name, []byte("fixture audio bytes"), 0o644); err != nil {
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
		"01 - Song.flac": {Artist: "Artist", AlbumArtist: "Artist", Album: "Album", Title: "Song", Track: intPtr(1), Disc: intPtr(1), Format: "flac", DurationMS: 1000},
		"02 - Other.mp3": {Artist: "Unknown", AlbumArtist: "Unknown", Album: "Unknown", Title: "Other", Track: intPtr(2), Disc: intPtr(1), Format: "mp3"},
	})
	scan, err := service.StartScan(ctx, "Music")
	if err != nil {
		t.Fatal(err)
	}
	scan = waitForScan(t, ctx, service, scan.ID)
	if scan.State != "review" || scan.FileCount != 2 || scan.MatchedCount != 1 || scan.UnresolvedCount != 1 {
		t.Fatal("scan counts", scan)
	}
	page, err := service.ScanPage(ctx, scan.ID, 1)
	if err != nil || len(page.Items) != 2 {
		t.Fatal("scan page", page, err)
	}
	var matched, unresolved *ScanFile
	for i := range page.Items {
		switch page.Items[i].RelativePath {
		case "Music/Artist/Album/01 - Song.flac":
			matched = &page.Items[i]
		case "Music/Artist/Album/02 - Other.mp3":
			unresolved = &page.Items[i]
		}
	}
	if matched == nil || matched.State != "matched" || matched.AlbumID == nil || *matched.AlbumID != 2 || matched.TrackID == nil || *matched.TrackID != 3 {
		t.Fatal("matched file", matched)
	}
	if unresolved == nil || unresolved.State != "unresolved" || unresolved.Error == "" {
		t.Fatal("unresolved file", unresolved)
	}
	registered, err := service.RegisterScan(ctx, scan.ID, []int64{matched.ID, unresolved.ID})
	if err != nil || registered != 1 {
		t.Fatal("register", registered, err)
	}
	var relative, origin, sha string
	if err = db.QueryRow(`SELECT relative_path,origin,sha256 FROM music_files`).Scan(&relative, &origin, &sha); err != nil {
		t.Fatal(err)
	}
	if relative != "Music/Artist/Album/01 - Song.flac" || origin != "existing" || sha == "" {
		t.Fatal("registered file", relative, origin, sha)
	}
	if _, err = os.Stat(matchedPath); err != nil {
		t.Fatal("source file was moved", err)
	}
	var artistStatus, albumStatus string
	if err = db.QueryRow(`SELECT status FROM music_artists WHERE id=1`).Scan(&artistStatus); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT status FROM music_albums WHERE id=2`).Scan(&albumStatus); err != nil {
		t.Fatal(err)
	}
	if artistStatus != "available" || albumStatus != "available" {
		t.Fatal("availability", artistStatus, albumStatus)
	}
}

func TestScanRejectsEscapingRoots(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := NewService(db, t.TempDir())
	for _, root := range []string{"", ".", "..", "../outside", "/etc", "Music/../../outside"} {
		if _, err := service.StartScan(ctx, root); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("escaping root accepted", root, err)
		}
	}
}

func waitForScan(t *testing.T, ctx context.Context, service *Service, id int64) Scan {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		scan, err := service.Scan(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if scan.State != "scanning" {
			return scan
		}
		if time.Now().After(deadline) {
			t.Fatal("scan timed out", scan)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func intPtr(value int) *int { return &value }
