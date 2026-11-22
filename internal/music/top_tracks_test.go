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

func TestResolveDeezerArtistPrefersCatalogCorroborationOverPopularity(t *testing.T) {
	// Two same-named artists exist on Deezer; the popular one is a stranger.
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/artist":
			_, _ = w.Write([]byte(`{"data":[
				{"id":377,"name":"ABK","nb_fan":3011},
				{"id":157,"name":"Anybody Killa","nb_fan":761}
			]}`))
		case "/artist/377/albums":
			_, _ = w.Write([]byte(`{"data":[{"title":"The Real ABK"},{"title":"En Realidad"}]}`))
		case "/artist/157/albums":
			_, _ = w.Write([]byte(`{"data":[{"title":"Dirty History"},{"title":"Hatchet Warrior"}]}`))
		case "/search/album":
			_, _ = w.Write([]byte(`{"data":[
				{"title":"Dirty History","artist":{"id":157,"name":"Anybody Killa"}},
				{"title":"Dirty History","artist":{"id":909,"name":"Somebody Else"}}
			]}`))
		case "/artist/909/albums":
			_, _ = w.Write([]byte(`{"data":[{"title":"Dirty History (Deluxe Reissue)"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := &Service{store: NewStore(db), topBaseURL: provider.URL}
	if _, err := db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES(15,'f851528b-e574-4309-92df-4b33ffde1340','ABK',1,'available',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(50,15,'c7a0c651-0178-33eb-8eb6-d6563f2cb095','Dirty History',1,'available',1,1)`); err != nil {
		t.Fatal(err)
	}

	// The saved catalog album corroborates the less popular candidate even
	// though the stranger has the exact display name.
	id, verified, err := service.resolveDeezerArtist(ctx, provider.URL, "ABK")
	if err != nil || id != 157 || !verified {
		t.Fatalf("corroborated artist id=%d verified=%v err=%v", id, verified, err)
	}

	// A catalog with no albums yet cannot corroborate; the exact-name
	// popularity fallback applies as before.
	if _, err := db.Exec(`DELETE FROM music_albums`); err != nil {
		t.Fatal(err)
	}
	id, verified, err = service.resolveDeezerArtist(ctx, provider.URL, "ABK")
	if err != nil || id != 377 || verified {
		t.Fatalf("uncorroborated fallback id=%d verified=%v err=%v", id, verified, err)
	}

	// A catalog album that no candidate carries refuses the resolution; a
	// popular same-named stranger must not own the page.
	if _, err := db.Exec(`INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(51,15,'16735596-37a1-49ef-9b46-2f7c776bd249','Hatchet Warrior',1,'available',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE music_albums SET title='Mud, Water, Air, Blood' WHERE id=51`); err != nil {
		t.Fatal(err)
	}
	id, verified, err = service.resolveDeezerArtist(ctx, provider.URL, "ABK")
	if err != nil || id != 0 || verified {
		t.Fatalf("refused resolution id=%d verified=%v err=%v", id, verified, err)
	}
}

func TestResolveDeezerArtistCorroboratesCompilationCatalogsByTrackEvidence(t *testing.T) {
	// Real-world shape: Deezer's artist album list omits compilation
	// releases entirely ("The Very Best of Cher" never appears in
	// /artist/{id}/albums) and album search does not surface the compilation
	// either, so a "Best of"-only catalog can only be corroborated by the
	// saved tracks, which still name the artist.
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/artist":
			_, _ = w.Write([]byte(`{"data":[{"id":3156,"name":"Cher","nb_fan":565070}]}`))
		case "/artist/3156/albums":
			_, _ = w.Write([]byte(`{"data":[{"title":"Believe"},{"title":"Living Proof"}]}`))
		case "/search/album":
			_, _ = w.Write([]byte(`{"data":[{"title":"The Very Best of Chicago","artist":{"id":2592,"name":"Chicago"}}]}`))
		case "/search/track":
			_, _ = w.Write([]byte(`{"data":[{"id":786717,"title":"Believe","artist":{"id":3156,"name":"Cher"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStore(db)
	saved, err := store.SaveArtist(ctx,
		Artist{MusicBrainzID: testMBID, Name: "Cher", Monitored: true, Status: "missing"},
		[]Album{{MusicBrainzID: testRGID, Title: "The Very Best of Cher", Monitored: true, Status: "missing"}})
	if err != nil {
		t.Fatal(err)
	}
	albums, err := store.Albums(ctx, saved.ID)
	if err != nil || len(albums) != 1 {
		t.Fatal(albums, err)
	}
	if err := store.SaveTracks(ctx, albums[0].ID, []Track{{Disc: 1, Number: 1, Title: "Believe", DurationMS: 239000}}); err != nil {
		t.Fatal(err)
	}
	service := &Service{store: NewStore(db), topBaseURL: provider.URL}
	id, verified, err := service.resolveDeezerArtist(ctx, provider.URL, "Cher")
	if err != nil || id != 3156 || !verified {
		t.Fatalf("track evidence did not corroborate the compilation catalog: id=%d verified=%v err=%v", id, verified, err)
	}

	// When the tracks belong to nobody on Deezer the resolution is refused;
	// a popular same-named stranger must still not own the page.
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/artist":
			_, _ = w.Write([]byte(`{"data":[{"id":3156,"name":"Cher","nb_fan":565070}]}`))
		case "/artist/3156/albums":
			_, _ = w.Write([]byte(`{"data":[{"title":"Believe"},{"title":"Living Proof"}]}`))
		case "/search/album":
			_, _ = w.Write([]byte(`{"data":[]}`))
		case "/search/track":
			_, _ = w.Write([]byte(`{"data":[{"id":1,"title":"Believe","artist":{"id":999,"name":"Sher"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer refusing.Close()
	id, verified, err = service.resolveDeezerArtist(ctx, refusing.URL, "Cher")
	if err != nil || id != 0 || verified {
		t.Fatalf("unattributed track evidence must refuse the resolution: id=%d verified=%v err=%v", id, verified, err)
	}
}

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
		VALUES(?,?,?,'Music','Music/Radiohead/OK Computer/01 Airbag.mp3',5,'sha','mp3',192000,0,0,284000,'{}',1,'existing','01 Airbag.mp3')`,
		saved.ID, albums[0].ID, tracks[0].ID); err != nil {
		t.Fatal(err)
	}

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/artist":
			_, _ = w.Write([]byte(`{"data":[{"id":1,"name":"Radiohead"}]}`))
		case "/artist/1/albums":
			_, _ = w.Write([]byte(`{"data":[{"title":"OK Computer"},{"title":"The Bends"}]}`))
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
	if page.Items[0].Bitrate != 192000 || page.Items[0].SizeBytes != 5 || page.Items[0].Format != "mp3" || page.Items[0].RelativePath != "Music/Radiohead/OK Computer/01 Airbag.mp3" {
		t.Fatal("top track file metadata", page.Items[0])
	}
	charts, err := service.ChartTracks(ctx, 20)
	if err != nil || len(charts.Items) != 1 || !charts.Items[0].InLibrary {
		t.Fatal("charts", charts, err)
	}
	if charts.Items[0].Bitrate != 192000 || charts.Items[0].RelativePath == "" {
		t.Fatal("chart file metadata", charts.Items[0])
	}
}
