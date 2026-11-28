package music

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLibraryEditorScopesBatchMetadataAndRejectsStaleRevision(t *testing.T) {
	ctx := context.Background()
	service, db, _ := editFixture(t)
	if _, err := db.Exec(`INSERT INTO music_tracks(id,album_id,disc,number,title,created_at,updated_at)
		VALUES(4,2,1,2,'Second',1,1),(5,2,1,3,'Third',1,1)`); err != nil {
		t.Fatal(err)
	}
	for i := int64(3); i <= 5; i++ {
		insertMusicFile(t, db, i, i, "Music/"+string(rune('a'+i))+".flac", matchingTags())
	}
	snapshot, err := service.LibraryEditor(ctx, "tracks", []int64{3, 4})
	if err != nil {
		t.Fatal(err)
	}
	update := LibraryEditorUpdate{Kind: "tracks", IDs: []int64{3, 4}, Revision: snapshot.Revision,
		Fields: map[string]string{"artist": "Guest Artist", "genre": "Jazz", "year": ""}}
	if result, err := service.UpdateLibraryEditor(ctx, update); err != nil || result.EditJobID != 0 {
		t.Fatalf("save: %+v %v", result, err)
	}
	if _, err := service.UpdateLibraryEditor(ctx, update); !errors.Is(err, ErrPlanChanged) {
		t.Fatalf("stale update: %v", err)
	}
	found, err := service.store.ListTracks(ctx, "Guest Artist", 1, 100)
	if err != nil || len(found.Items) != 2 || found.Items[0].Title != "Song" || found.Items[1].Title != "Second" {
		t.Fatalf("selected tracks: %+v %v", found, err)
	}
	third, err := service.LibraryEditor(ctx, "tracks", []int64{5})
	if err != nil || third.Items[0].Fields["genre"] != "" {
		t.Fatalf("unselected track: %+v %v", third, err)
	}
	artist, err := service.store.Artist(ctx, 1)
	if err != nil || artist.Name != "Artist" {
		t.Fatalf("artist: %+v %v", artist, err)
	}
}

func TestLibraryEditorArtworkAndCatalogFieldsSurviveProviderSave(t *testing.T) {
	ctx := context.Background()
	service, _, _ := editFixture(t)
	var img bytes.Buffer
	if err := png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	for _, target := range []struct {
		kind   string
		id     int64
		fields map[string]string
	}{
		{"artists", 1, map[string]string{"name": "Artist edited", "country": "CA", "overview": "Saved bio"}},
		{"albums", 2, map[string]string{"title": "Album edited", "album_artist": "Various performers", "year": "2020", "release_date": "2020-02"}},
	} {
		snapshot, err := service.LibraryEditor(ctx, target.kind, []int64{target.id})
		if err != nil {
			t.Fatal(err)
		}
		update := LibraryEditorUpdate{Kind: target.kind, IDs: []int64{target.id}, Revision: snapshot.Revision, Fields: target.fields,
			Artwork: &LibraryArtworkInput{Data: img.Bytes()}}
		if _, err := service.UpdateLibraryEditor(ctx, update); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.store.SaveArtist(ctx, Artist{MusicBrainzID: testMBID, Name: "Provider name", Country: "GB", Status: "available"},
		[]Album{{MusicBrainzID: testRGID, Title: "Provider title", ReleaseDate: "2025", Status: "available"}}); err != nil {
		t.Fatal(err)
	}
	artist, err := service.store.Artist(ctx, 1)
	if err != nil || artist.Name != "Artist edited" || artist.Country != "CA" || artist.Overview != "Saved bio" || !strings.Contains(artist.ImageURL, "?v=") {
		t.Fatalf("artist: %+v %v", artist, err)
	}
	album, err := service.store.Album(ctx, 2)
	if err != nil || album.Title != "Album edited" || album.ReleaseDate != "2020-02" || album.ArtistName != "Various performers" || album.Year == nil || *album.Year != 2020 {
		t.Fatalf("album: %+v %v", album, err)
	}
	art, err := service.AlbumImage(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer art.File.Close()
	data, err := io.ReadAll(art.File)
	if err != nil || art.ContentType != "image/png" || !bytes.Equal(data, img.Bytes()) {
		t.Fatalf("cover: %s %d %v", art.ContentType, len(data), err)
	}
}

func TestLibraryEditorRenamesAndRetagsGeneratedAudio(t *testing.T) {
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is required: %v", tool, err)
		}
	}
	ctx := context.Background()
	service, db, root := editFixture(t)
	dir := filepath.Join(root, "Music")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, "original.flac")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=0.2",
		"-metadata", "title=Song", "-metadata", "artist=Artist", "-metadata", "album=Album", "-metadata", "track=1/10",
		"-metadata", "disc=1", "-metadata", "genre=Old", "-c:a", "flac", name).CombinedOutput(); err != nil {
		t.Fatalf("generate audio: %v %s", err, out)
	}
	insertMusicFile(t, db, 1, 3, "Music/original.flac", matchingTags())
	snapshot, err := service.LibraryEditor(ctx, "tracks", []int64{3})
	if err != nil {
		t.Fatal(err)
	}
	apply := LibraryEditorUpdate{Kind: "tracks", IDs: []int64{3}, Revision: snapshot.Revision, WriteTags: true, Filename: "renamed.flac",
		Fields: map[string]string{"title": "Edited", "genre": "Jazz", "number": "2"}}
	if err := os.WriteFile(filepath.Join(dir, "renamed.flac"), []byte("occupied"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateLibraryEditor(ctx, apply); err == nil {
		t.Fatal("occupied target accepted")
	}
	if err := os.Remove(filepath.Join(dir, "renamed.flac")); err != nil {
		t.Fatal(err)
	}
	result, err := service.UpdateLibraryEditor(ctx, apply)
	if err != nil {
		t.Fatal(err)
	}
	job := waitForEditJob(t, ctx, service, result.EditJobID)
	if job.State != "completed" {
		t.Fatalf("edit: %+v", job)
	}
	changed, err := service.prober.ProbeAudio(ctx, filepath.Join(dir, "renamed.flac"))
	if err != nil || changed.Title != "Edited" || changed.Genre != "Jazz" || changed.Artist != "Artist" ||
		changed.Album != "Album" || changed.Track == nil || *changed.Track != 2 || changed.TrackTotal == nil || *changed.TrackTotal != 10 {
		t.Fatalf("audio tags: %+v %v", changed, err)
	}
	tracks, err := service.store.Tracks(ctx, 2)
	if err != nil || len(tracks) != 1 || tracks[0].Title != "Edited" || tracks[0].RelativePath != "Music/renamed.flac" {
		t.Fatalf("catalog: %+v %v", tracks, err)
	}
}

func TestLibraryEditorBatchRenameContinuesAfterMissingSource(t *testing.T) {
	ctx := context.Background()
	service, db, root := editFixture(t)
	if _, err := db.Exec(`INSERT INTO music_tracks(id,album_id,disc,number,title,created_at,updated_at)
		VALUES(4,2,1,2,'Second',1,1)`); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "Music"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Music", "second.flac"), []byte("unchanged audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	insertMusicFile(t, db, 1, 3, "Music/missing.flac", matchingTags())
	insertMusicFile(t, db, 2, 4, "Music/second.flac", matchingTags())
	ids := []int64{3, 4}
	snapshot, err := service.LibraryEditor(ctx, "tracks", ids)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.UpdateLibraryEditor(ctx, LibraryEditorUpdate{Kind: "tracks", IDs: ids,
		Revision: snapshot.Revision, Filename: "{track} - {title}"})
	if err != nil {
		t.Fatal(err)
	}
	job := waitForEditJob(t, ctx, service, result.EditJobID)
	if job.State != "failed" || len(job.Files) != 2 || job.Files[0].State != "failed" || job.Files[1].State != "completed" {
		t.Fatalf("file results: %+v", job)
	}
	contents, err := os.ReadFile(filepath.Join(root, "Music", "2 - Second.flac"))
	if err != nil || string(contents) != "unchanged audio" {
		t.Fatalf("renamed content: %q %v", contents, err)
	}
}
