package music

import (
	"context"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func TestStoreSavesArtistsAlbumsAndTracks(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStore(db)

	year := 1997
	artist := Artist{MusicBrainzID: testMBID, Name: "Radiohead", SortName: "Radiohead", Country: "GB", Type: "Group", Monitored: true, Status: "missing"}
	albums := []Album{{
		MusicBrainzID: testRGID,
		Title:         "OK Computer",
		PrimaryType:   "Album",
		Year:          &year,
		ReleaseDate:   "1997-05-21",
		CoverURL:      "https://coverartarchive.org/release-group/" + testRGID + "/front-500",
		Monitored:     true,
		Status:        "missing",
	}}
	saved, err := store.SaveArtist(ctx, artist, albums)
	if err != nil || saved.ID < 1 || saved.AlbumCount != 1 {
		t.Fatal("save artist", saved, err)
	}
	byMBID, err := store.ArtistByMusicBrainzID(ctx, testMBID)
	if err != nil || byMBID.ID != saved.ID || byMBID.Name != "Radiohead" {
		t.Fatal("artist by mbid", byMBID, err)
	}
	page, err := store.ListArtists(ctx, "radio", 1)
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].Name != "Radiohead" {
		t.Fatal("list artists", page, err)
	}
	details, err := store.Artist(ctx, saved.ID)
	if err != nil || details.AlbumCount != 1 {
		t.Fatal("artist details", details, err)
	}
	artistAlbums, err := store.Albums(ctx, saved.ID)
	if err != nil || len(artistAlbums) != 1 || artistAlbums[0].ArtistName != "Radiohead" || artistAlbums[0].TrackCount != 0 {
		t.Fatal("albums", artistAlbums, err)
	}
	album, err := store.Album(ctx, artistAlbums[0].ID)
	if err != nil || album.Title != "OK Computer" || album.Year == nil || *album.Year != 1997 {
		t.Fatal("album", album, err)
	}
	tracks := []Track{{MusicBrainzID: testTrackID, RecordingID: testRecordingID, Disc: 1, Number: 1, Title: "Airbag", DurationMS: 284000}}
	if err := store.SaveTracks(ctx, album.ID, tracks); err != nil {
		t.Fatal(err)
	}
	got, err := store.Tracks(ctx, album.ID)
	if err != nil || len(got) != 1 || got[0].Title != "Airbag" || got[0].DurationMS != 284000 {
		t.Fatal("tracks", got, err)
	}
	refreshed, err := store.Album(ctx, album.ID)
	if err != nil || refreshed.TrackCount != 1 {
		t.Fatal("track count", refreshed, err)
	}

	// A repeated add refreshes metadata without duplicating albums or
	// changing the existing monitoring choice.
	artist.Name = "Radiohead (updated)"
	artist.Monitored = false
	if _, err := store.SaveArtist(ctx, artist, albums); err != nil {
		t.Fatal(err)
	}
	again, err := store.ArtistByMusicBrainzID(ctx, testMBID)
	if err != nil || again.Name != "Radiohead (updated)" || again.AlbumCount != 1 || !again.Monitored {
		t.Fatal("idempotent save", again, err)
	}
}

func TestStoreDoesNotCopyReleaseGroupUnderAnotherArtist(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStore(db)
	first := Artist{MusicBrainzID: testMBID, Name: "First Artist", Monitored: true, Status: "missing"}
	album := Album{MusicBrainzID: testRGID, Title: "Shared Release", Monitored: true, Status: "missing"}
	saved, err := store.SaveArtist(ctx, first, []Album{album})
	if err != nil || saved.AlbumCount != 1 {
		t.Fatal("first artist", saved, err)
	}
	second := Artist{MusicBrainzID: discoveryArtist2, Name: "Second Artist", Monitored: true, Status: "missing"}
	savedSecond, err := store.SaveArtist(ctx, second, []Album{album})
	if err != nil {
		t.Fatal(err)
	}
	if savedSecond.AlbumCount != 0 {
		t.Fatal("release group was copied to another artist", savedSecond)
	}
	var albums int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_albums`).Scan(&albums); err != nil || albums != 1 {
		t.Fatal("album rows", albums, err)
	}
}

func TestStoreRejectsInvalidPaging(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := NewStore(db).ListArtists(ctx, "", 0); err == nil {
		t.Fatal("invalid page accepted")
	}
	if _, err := NewStore(db).ListArtists(ctx, "", 100001); err == nil {
		t.Fatal("oversized page accepted")
	}
}
