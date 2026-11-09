package music

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func TestArtistTopTracksUsesProviderOverallTracksAndMarksLibraryMatches(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStore(db)
	year := 1997
	saved, err := store.SaveArtist(ctx,
		Artist{MusicBrainzID: testMBID, Name: "Radiohead", Monitored: true, Status: "missing"},
		[]Album{{MusicBrainzID: testRGID, Title: "OK Computer", Year: &year, Monitored: true, Status: "missing"}})
	if err != nil {
		t.Fatal(err)
	}
	albums, _ := store.Albums(ctx, saved.ID)
	if err := store.SaveTracks(ctx, albums[0].ID, []Track{{Disc: 1, Number: 1, Title: "Airbag", DurationMS: 284000}}); err != nil {
		t.Fatal(err)
	}
	tracks, _ := store.Tracks(ctx, albums[0].ID)
	mediaDir := t.TempDir()
	filePath := mediaDir + "/Music/Radiohead/OK Computer/01 Airbag.mp3"
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_files(artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,sample_rate,channels,duration_ms,tag_json,imported_at,origin,source_name)
		VALUES(?,?,?,'Music','Music/Radiohead/OK Computer/01 Airbag.mp3',5,'sha','mp3',0,0,0,284000,'{}',1,'existing','01 Airbag.mp3')`,
		saved.ID, albums[0].ID, tracks[0].ID); err != nil {
		t.Fatal(err)
	}

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/artist":
			_, _ = w.Write([]byte(`{"data":[{"id":1,"name":"Radiohead"}]}`))
		case "/artist/1/top":
			_, _ = w.Write([]byte(`{"data":[{"id":10,"title":"Airbag","duration":284,"artist":{"name":"Radiohead"},"album":{"id":2,"title":"OK Computer","cover_medium":""}}]}`))
		case "/chart/0/tracks":
			_, _ = w.Write([]byte(`{"data":[{"id":11,"title":"Airbag","duration":284,"artist":{"name":"Radiohead"},"album":{"id":2,"title":"OK Computer","cover_medium":""}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()

	service := NewService(db, mediaDir)
	service.topBaseURL = provider.URL
	page, err := service.ArtistTopTracks(ctx, saved.ID, 20)
	if err != nil || len(page.Items) != 1 {
		t.Fatal("top tracks", page, err)
	}
	if !page.Items[0].InLibrary || page.Items[0].FileID == nil || page.Items[0].Title != "Airbag" {
		t.Fatal("library match", page.Items[0])
	}
	charts, err := service.ChartTracks(ctx, 20)
	if err != nil || len(charts.Items) != 1 || !charts.Items[0].InLibrary {
		t.Fatal("charts", charts, err)
	}
}
