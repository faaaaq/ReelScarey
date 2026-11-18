package music

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func TestMusicFieldOverridesSurviveRefreshAndRestoreProviderValues(t *testing.T) {
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
	albums, err := store.Albums(ctx, artist.ID)
	if err != nil || len(albums) != 1 {
		t.Fatal(albums, err)
	}
	albumID := albums[0].ID
	if _, err = db.Exec(`UPDATE music_albums SET title='Owner edition label',title_overridden=1,year=6666 WHERE id=?`, albumID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE music_artists SET name='Owner artist credit',name_overridden=1 WHERE id=?`, artist.ID); err != nil {
		t.Fatal(err)
	}

	brainz := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ws/2/release-group/" + testRGID:
			writeDiscoveryJSON(w, `{"id":"`+testRGID+`","title":"OK Computer","primary-type":"Album","first-release-date":"1997-05-21","artist-credit":[{"name":"Radiohead","artist":{"id":"`+testMBID+`","name":"Radiohead"}}]}`)
		case "/ws/2/artist/" + testMBID:
			writeDiscoveryJSON(w, `{"id":"`+testMBID+`","name":"Radiohead","sort-name":"Radiohead","type":"Group"}`)
		default:
			t.Errorf("unexpected MusicBrainz request %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer brainz.Close()
	service := NewServiceWithClient(db, newClient(brainz.URL+"/ws/2", brainz.Client(), 0), t.TempDir())
	deezer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search/album" {
			_, _ = w.Write([]byte(`{"data":[{"title":"Owner edition label","artist":{"name":"Owner artist credit"},"cover_xl":"https://e-cdns-images.dzcdn.net/images/cover/new/1000x1000.jpg"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer deezer.Close()
	service.topBaseURL = deezer.URL
	service.SetArtworkDir(t.TempDir())

	if _, err = service.RefreshAlbumMetadata(ctx, albumID); err != nil {
		t.Fatal(err)
	}
	saved, err := store.Album(ctx, albumID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Title != "Owner edition label" || !saved.TitleOverridden || saved.Year == nil || *saved.Year != 1997 || saved.YearOverridden {
		t.Fatalf("mixed refresh did not preserve only the overridden field: %#v", saved)
	}
	year2000 := 2000
	if _, err = store.SaveArtist(ctx,
		Artist{MusicBrainzID: testMBID, Name: "Radiohead", Status: "missing"},
		[]Album{{MusicBrainzID: testRGID, Title: "Provider replacement", Year: &year2000, Status: "missing"}}); err != nil {
		t.Fatal(err)
	}
	saved, _ = store.Album(ctx, albumID)
	if saved.Title != "Owner edition label" || saved.Year == nil || *saved.Year != 2000 {
		t.Fatalf("discography save bypassed provenance: %#v", saved)
	}
	savedArtist, err := store.Artist(ctx, artist.ID)
	if err != nil || savedArtist.Name != "Owner artist credit" || !savedArtist.NameOverridden {
		t.Fatalf("artist override was not retained: %#v, %v", savedArtist, err)
	}

	if _, err = service.RestoreAlbumMetadataField(ctx, albumID, "title"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.RestoreArtistMetadataField(ctx, artist.ID, "name"); err != nil {
		t.Fatal(err)
	}
	saved, _ = store.Album(ctx, albumID)
	savedArtist, _ = store.Artist(ctx, artist.ID)
	if saved.Title != "OK Computer" || saved.TitleOverridden || savedArtist.Name != "Radiohead" || savedArtist.NameOverridden {
		t.Fatalf("provider values were not restored: album=%#v artist=%#v", saved, savedArtist)
	}

	if _, err = db.Exec(`UPDATE music_albums SET title='Keep on provider failure',title_overridden=1 WHERE id=?`, albumID); err != nil {
		t.Fatal(err)
	}
	failingService := NewServiceWithClient(db, newClient(brainz.URL+"/ws/2", brainz.Client(), 0), t.TempDir())
	failingService.client.SetCache(nil)
	brainz.Close()
	if _, err = failingService.RestoreAlbumMetadataField(ctx, albumID, "title"); err == nil {
		t.Fatal("provider failure was not returned")
	}
	saved, _ = store.Album(ctx, albumID)
	if saved.Title != "Keep on provider failure" || !saved.TitleOverridden {
		t.Fatalf("failed restore cleared owner correction: %#v", saved)
	}
}

func TestEditJobPersistsOnlyExplicitOverrideFields(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	jobID, err := persistEditJob(ctx, tx, nil, false, (EditOverrides{Year: 1997, Album: "Corrected"}).fields())
	if err != nil {
		t.Fatal(err)
	}
	var mask string
	if err = tx.QueryRow(`SELECT override_fields FROM music_edit_jobs WHERE id=?`, jobID).Scan(&mask); err != nil {
		t.Fatal(err)
	}
	if mask != "year,album" || hasOverrideField(mask, "artist") {
		t.Fatalf("unexpected override mask %q", mask)
	}
}
