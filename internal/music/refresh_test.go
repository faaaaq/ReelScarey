package music

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

// abkFixture builds the reported scenario: the catalog artist "ABK" carries
// catalog albums that only the Deezer artist under the alias name owns, while
// the popular same-named stranger does not.
func abkFixture(t *testing.T, aliasAlbums, strangerAlbums string) (*Service, *Store, int64, string) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store := NewStore(db)
	artist, err := store.SaveArtist(ctx,
		Artist{MusicBrainzID: testMBID, Name: "ABK", Monitored: true, Status: "available"},
		[]Album{{MusicBrainzID: testRGID, Title: "Dirty History", Monitored: true, Status: "available"}})
	if err != nil {
		t.Fatal(err)
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/artist":
			_, _ = w.Write([]byte(`{"data":[` +
				`{"id":377,"name":"ABK","nb_fan":3011},` +
				`{"id":157,"name":"Anybody Killa","nb_fan":761}]}`))
		case "/artist/377/albums":
			_, _ = w.Write([]byte(strangerAlbums))
		case "/artist/157/albums":
			_, _ = w.Write([]byte(aliasAlbums))
		case "/search/album":
			_, _ = w.Write([]byte(`{"data":[{"title":"Dirty History","artist":{"id":157,"name":"Anybody Killa"}}]}`))
		case "/artist/157":
			_, _ = w.Write([]byte(`{"picture_xl":"https://e-cdns-images.dzcdn.net/images/artist/ok/1000x1000.jpg"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(provider.Close)
	artworkDir := t.TempDir()
	service := NewServiceWithClient(db, newClient("http://127.0.0.1:1/ws/2", provider.Client(), 0), t.TempDir())
	service.topBaseURL = provider.URL
	service.SetArtworkDir(artworkDir)
	return service, store, artist.ID, artworkDir
}

func TestRefreshArtistReplacesStrangerImageWithVerifiedAlias(t *testing.T) {
	// The artist image was previously resolved by the bare name match and
	// points at the popular same-named stranger.
	service, store, artistID, artworkDir := abkFixture(t,
		`{"data":[{"title":"Dirty History","cover_xl":"https://e-cdns-images.dzcdn.net/images/cover/ok/1000x1000.jpg"}]}`,
		`{"data":[{"title":"The Real ABK"},{"title":"En Realidad"}]}`)
	if _, err := store.db.Exec(`UPDATE music_artists SET image_url='https://e-cdns-images.dzcdn.net/images/artist/stranger/1000x1000.jpg' WHERE id=?`, artistID); err != nil {
		t.Fatal(err)
	}
	// A cached stranger image exists on disk and must be replaced.
	if err := os.WriteFile(filepath.Join(artworkDir, "artist-"+strconv.FormatInt(artistID, 10)+".jpg"), []byte("stranger"), 0o600); err != nil {
		t.Fatal(err)
	}
	original := artworkFetch
	artworkFetch = func(context.Context, string) ([]byte, string, error) {
		return []byte("verified-image-bytes"), "image/jpeg", nil
	}
	t.Cleanup(func() { artworkFetch = original })

	result, err := service.RefreshArtistMetadata(context.Background(), artistID)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ImageUpdated || result.ImageCleared {
		t.Fatalf("refresh result %+v", result)
	}
	var stored string
	if err := store.db.QueryRow(`SELECT image_url FROM music_artists WHERE id=?`, artistID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stored, "/artist/ok/") {
		t.Fatalf("stored image %q", stored)
	}
	art, err := service.ArtistImage(context.Background(), artistID)
	if err != nil {
		t.Fatal("cached image was not refreshed", err)
	}
	body, _ := io.ReadAll(art.File)
	art.File.Close()
	if string(body) != "verified-image-bytes" {
		t.Fatalf("served image %q", string(body))
	}
}

func TestRefreshArtistClearsUnverifiableImage(t *testing.T) {
	// No Deezer candidate carries the catalog's album; the stored stranger
	// image must be removed rather than kept.
	service, store, artistID, artworkDir := abkFixture(t,
		`{"data":[]}`,
		`{"data":[{"title":"The Real ABK"}]}`)
	if _, err := store.db.Exec(`UPDATE music_artists SET image_url='https://e-cdns-images.dzcdn.net/images/artist/stranger/1000x1000.jpg' WHERE id=?`, artistID); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(artworkDir, "artist-"+strconv.FormatInt(artistID, 10)+".jpg")
	if err := os.WriteFile(cachePath, []byte("stranger"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := service.RefreshArtistMetadata(context.Background(), artistID)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ImageCleared || result.ImageUpdated {
		t.Fatalf("refresh result %+v", result)
	}
	var stored string
	if err := store.db.QueryRow(`SELECT image_url FROM music_artists WHERE id=?`, artistID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "" {
		t.Fatalf("unverified image was kept: %q", stored)
	}
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatal("cached stranger image was not removed", err)
	}
}

func TestRefreshAlbumUpdatesCanonicalYearAndCover(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStore(db)
	artist, err := store.SaveArtist(ctx,
		Artist{MusicBrainzID: testMBID, Name: "Radiohead", Monitored: true, Status: "missing"},
		[]Album{{MusicBrainzID: testRGID, Title: "OK Computer", Monitored: true, Status: "missing"}})
	if err != nil {
		t.Fatal(err)
	}
	albums, _ := store.Albums(ctx, artist.ID)

	brainz := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ws/2/release-group/" + testRGID:
			writeDiscoveryJSON(w, `{"id":"`+testRGID+`","title":"OK Computer","primary-type":"Album","first-release-date":"1997-05-21","artist-credit":[{"name":"Radiohead","artist":{"id":"`+testMBID+`","name":"Radiohead"}}]}`)
		default:
			t.Errorf("unexpected MusicBrainz request %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer brainz.Close()
	service := NewServiceWithClient(db, newClient(brainz.URL+"/ws/2", brainz.Client(), 0), t.TempDir())
	deezer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search/album" {
			_, _ = w.Write([]byte(`{"data":[{"title":"OK Computer","artist":{"name":"Radiohead"},"cover_xl":"https://e-cdns-images.dzcdn.net/images/cover/new/1000x1000.jpg"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer deezer.Close()
	service.topBaseURL = deezer.URL
	service.SetArtworkDir(t.TempDir())

	if _, err := db.Exec(`UPDATE music_albums SET year=NULL WHERE id=?`, albums[0].ID); err != nil {
		t.Fatal(err)
	}
	result, err := service.RefreshAlbumMetadata(ctx, albums[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !result.AlbumUpdate || !result.CoverUpdate {
		t.Fatalf("refresh result %+v", result)
	}
	var title string
	var year *int
	if err := db.QueryRow(`SELECT title,year FROM music_albums WHERE id=?`, albums[0].ID).Scan(&title, &year); err != nil {
		t.Fatal(err)
	}
	if title != "OK Computer" || year == nil || *year != 1997 {
		t.Fatalf("canonical details title=%q year=%v", title, year)
	}
	var cover string
	if err := db.QueryRow(`SELECT cover_url FROM music_albums WHERE id=?`, albums[0].ID).Scan(&cover); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cover, "/cover/new/") {
		t.Fatalf("cover %q", cover)
	}
}

func TestRefreshLibraryMetadataBatchProcessesCatalog(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := int64(1)
	for i, name := range []string{"Artist A", "Artist B"} {
		id := "22222222-2222-4222-8222-22222222222" + strconv.Itoa(i)
		if _, err := db.Exec(`INSERT INTO music_artists(musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES(?,?,1,'missing',?,?)`,
			id, name, now, now); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO music_albums(artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at)
			VALUES((SELECT id FROM music_artists WHERE musicbrainz_id=?),'33333333-3333-4333-8333-33333333333'||?,'Album',1,'missing',?,?)`,
			id, strconv.Itoa(i), now, now); err != nil {
			t.Fatal(err)
		}
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search/artist" {
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer provider.Close()
	service := NewServiceWithClient(db, newClient("http://127.0.0.1:1/ws/2", provider.Client(), 0), t.TempDir())
	service.topBaseURL = provider.URL

	result, err := service.RefreshLibraryMetadataBatch(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Processed != 2 || !result.Done {
		t.Fatalf("batch %+v", result)
	}
	again, err := service.RefreshLibraryMetadataBatch(ctx, result.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Done || again.Processed != 0 {
		t.Fatalf("second batch %+v", again)
	}
}
