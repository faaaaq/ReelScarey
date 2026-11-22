package music

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func TestUnifiedLibraryAlbumAndTrackLists(t *testing.T) {
	ctx := context.Background()
	media := t.TempDir()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStore(db)
	year := 1997
	saved, err := store.SaveArtist(ctx,
		Artist{MusicBrainzID: testMBID, Name: "Radiohead", Monitored: true, Status: "available"},
		[]Album{{MusicBrainzID: testRGID, Title: "OK Computer", Year: &year, Monitored: true, Status: "available"}})
	if err != nil {
		t.Fatal(err)
	}
	albums, _ := store.Albums(ctx, saved.ID)
	if err := store.SaveTracks(ctx, albums[0].ID, []Track{{Disc: 1, Number: 1, Title: "Airbag", DurationMS: 284000}}); err != nil {
		t.Fatal(err)
	}
	tracks, _ := store.Tracks(ctx, albums[0].ID)
	relative := "Music/Radiohead/OK Computer/01 Airbag.mp3"
	path := filepath.Join(media, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_files(artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,sample_rate,channels,duration_ms,tag_json,imported_at,origin,source_name)
		VALUES(?,?,?,'Music',?,5,'sha','mp3',320000,0,0,284000,'{}',1,'existing','01 Airbag.mp3')`, saved.ID, albums[0].ID, tracks[0].ID, relative); err != nil {
		t.Fatal(err)
	}

	page, err := store.ListAlbums(ctx, "radio", 1, 48)
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ArtistName != "Radiohead" {
		t.Fatal("albums", page, err)
	}
	trackPage, err := store.ListTracks(ctx, "airbag", 1, 100)
	if err != nil || trackPage.Total != 1 || len(trackPage.Items) != 1 {
		t.Fatal("tracks", trackPage, err)
	}
	track := trackPage.Items[0]
	if track.ArtistName != "Radiohead" || track.AlbumTitle != "OK Computer" || track.FileID == nil {
		t.Fatal("track context", track)
	}
	if track.Bitrate != 320000 || track.SizeBytes != 5 || track.Format != "mp3" || track.RelativePath != relative {
		t.Fatal("track file metadata", track)
	}
	artistTracks, err := store.ArtistTracks(ctx, saved.ID, 1, 100)
	if err != nil || len(artistTracks.Items) != 1 || artistTracks.Items[0].Bitrate != 320000 || artistTracks.Items[0].RelativePath != relative {
		t.Fatal("artist track file metadata", artistTracks, err)
	}
	albumTracks, err := store.Tracks(ctx, albums[0].ID)
	if err != nil || len(albumTracks) != 1 || albumTracks[0].SizeBytes != 5 || albumTracks[0].Format != "mp3" {
		t.Fatal("album track file metadata", albumTracks, err)
	}
	if empty, err := store.ListTracks(ctx, "missing", 1, 100); err != nil || empty.Total != 0 {
		t.Fatal("filtered tracks", empty, err)
	}
}
