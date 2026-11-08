package music

import (
	"context"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func TestAlbumPreferencesOverrideAndUpgradeSuggestions(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES(1,?, 'Artist',1,'available',1,1)`, testMBID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,year,monitored,status,created_at,updated_at) VALUES(1,1,?, 'Album',2000,1,'available',1,1)`, testRGID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO music_tracks(id,album_id,musicbrainz_id,disc,number,title,created_at,updated_at) VALUES(1,1,?,1,1,'Song',1,1)`, testTrackID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO music_files(artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,sample_rate,channels,duration_ms,tag_json,imported_at,origin,source_name) VALUES(1,1,1,'Music','Music/Artist/Album/01.mp3',100,'sha','MP3',192,44100,2,1000,'{}',1,'existing','01.mp3')`); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, t.TempDir())
	if _, err = service.SavePreferences(ctx, MusicPreferences{AllowLossless: true, MinBitrate: 0, PreferredFormats: []string{"FLAC"}, Revision: 1}); err != nil {
		t.Fatal(err)
	}
	inherited, err := service.AlbumPreferences(ctx, 1)
	if err != nil || !inherited.Inherited || !inherited.Preferences.AllowLossless {
		t.Fatal("inherited preferences", inherited, err)
	}
	upgrades, err := service.UpgradeSuggestions(ctx, 10)
	if err != nil || len(upgrades) != 1 || upgrades[0].AlbumID != 1 {
		t.Fatal("suggestions before override", upgrades, err)
	}
	saved, err := service.SaveAlbumPreferences(ctx, 1, MusicPreferences{AllowLossless: false, MinBitrate: 0, PreferredFormats: []string{"MP3"}, Revision: inherited.Revision})
	if err != nil || saved.Inherited || saved.Preferences.AllowLossless || saved.Preferences.PreferredFormats[0] != "MP3" {
		t.Fatal("saved override", saved, err)
	}
	upgrades, err = service.UpgradeSuggestions(ctx, 10)
	if err != nil || len(upgrades) != 0 {
		t.Fatal("suggestions after override", upgrades, err)
	}
	reset, err := service.ResetAlbumPreferences(ctx, 1)
	if err != nil || !reset.Inherited {
		t.Fatal("reset", reset, err)
	}
}
