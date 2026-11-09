package music

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func TestArtworkIsDownloadedCachedAndServedLocally(t *testing.T) {
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

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/artist":
			_, _ = w.Write([]byte(`{"data":[{"id":1,"name":"Radiohead","picture_xl":"https://e-cdns-images.dzcdn.net/images/artist/1/1000x1000.jpg"}]}`))
		case "/search/album":
			_, _ = w.Write([]byte(`{"data":[{"title":"OK Computer","artist":{"name":"Radiohead"},"cover_xl":"https://e-cdns-images.dzcdn.net/images/cover/1/1000x1000.jpg"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()

	downloads := 0
	original := artworkFetch
	artworkFetch = func(context.Context, string) ([]byte, string, error) {
		downloads++
		return []byte("image-bytes"), "image/jpeg", nil
	}
	t.Cleanup(func() { artworkFetch = original })

	service := NewService(db, t.TempDir())
	service.topBaseURL = provider.URL
	service.SetArtworkDir(t.TempDir())
	artistArt, err := service.ArtistImage(ctx, artist.ID)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(artistArt.File)
	artistArt.File.Close()
	if !bytes.Equal(body, []byte("image-bytes")) || artistArt.ContentType != "image/jpeg" {
		t.Fatal("artist artwork", string(body), artistArt.ContentType)
	}
	albumArt, err := service.AlbumImage(ctx, albums[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(albumArt.File)
	albumArt.File.Close()
	if !bytes.Equal(body, []byte("image-bytes")) {
		t.Fatal("album artwork", string(body))
	}
	if downloads != 2 {
		t.Fatal("downloads", downloads)
	}
	if cached, err := service.ArtistImage(ctx, artist.ID); err != nil {
		t.Fatal(err)
	} else {
		cached.File.Close()
	}
	if cached, err := service.AlbumImage(ctx, albums[0].ID); err != nil {
		t.Fatal(err)
	} else {
		cached.File.Close()
	}
	if downloads != 2 {
		t.Fatal("cached artwork downloaded again", downloads)
	}
}
