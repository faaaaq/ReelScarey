package music

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func TestOpenAudioFileUsesRegisteredFileBelowMediaRoot(t *testing.T) {
	ctx := context.Background()
	mediaDir := t.TempDir()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStore(db)
	year := 2024
	saved, err := store.SaveArtist(ctx,
		Artist{MusicBrainzID: testMBID, Name: "Artist", Monitored: true, Status: "missing"},
		[]Album{{MusicBrainzID: testRGID, Title: "Album", Year: &year, Monitored: true, Status: "missing"}})
	if err != nil {
		t.Fatal(err)
	}
	albums, err := store.Albums(ctx, saved.ID)
	if err != nil || len(albums) != 1 {
		t.Fatal("albums", albums, err)
	}
	if err := store.SaveTracks(ctx, albums[0].ID, []Track{{Disc: 1, Number: 1, Title: "Song", DurationMS: 1000}}); err != nil {
		t.Fatal(err)
	}
	tracks, err := store.Tracks(ctx, albums[0].ID)
	if err != nil || len(tracks) != 1 {
		t.Fatal("tracks", tracks, err)
	}
	relative := "Music/Artist/Album/01 Song.mp3"
	path := filepath.Join(mediaDir, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("audio-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec(`INSERT INTO music_files(artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,sample_rate,channels,duration_ms,tag_json,imported_at,origin,source_name)
		VALUES(?,?,?,'Music',?,11,'sha','mp3',192,44100,2,1000,'{}',1,'existing','01 Song.mp3')`,
		saved.ID, albums[0].ID, tracks[0].ID, relative)
	if err != nil {
		t.Fatal(err)
	}
	fileID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	service := NewService(db, mediaDir)
	stream, err := service.OpenAudioFile(ctx, fileID)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.File.Close()
	if stream.ContentType != "audio/mpeg" || stream.Name != "01 Song.mp3" || stream.Size != 11 {
		t.Fatal("stream metadata", stream)
	}
	body, err := io.ReadAll(stream.File)
	if err != nil || string(body) != "audio-bytes" {
		t.Fatal("stream body", string(body), err)
	}

	// Album and artist track reads expose the same file for playback.
	got, err := store.Tracks(ctx, albums[0].ID)
	if err != nil || len(got) != 1 || got[0].FileID == nil || *got[0].FileID != fileID {
		t.Fatal("album track file id", got, err)
	}
	artistTracks, err := store.ArtistTracks(ctx, saved.ID, 1, 15)
	if err != nil || len(artistTracks.Items) != 1 || artistTracks.Items[0].FileID == nil || *artistTracks.Items[0].FileID != fileID {
		t.Fatal("artist track file id", artistTracks, err)
	}

	if _, err := service.OpenAudioFile(ctx, fileID+1); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("missing catalog row accepted", err)
	}
	if _, err := db.Exec(`DELETE FROM music_files WHERE id=?`, fileID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.OpenAudioFile(ctx, fileID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("deleted catalog row accepted", err)
	}
}
