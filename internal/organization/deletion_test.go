package organization

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A subtitle is named for its own video, not for any video whose name it
// starts with. Deleting "Movie.mkv" must leave "Movie.1985.srt" alone, because
// that subtitle belongs to "Movie.1985.mkv" in the same folder.
func TestDeletionKeepsSubtitlesNamedForAnotherVideo(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/Film/Movie.mkv", []byte("the remake"))
	f.movie(t, 2, "Movies/Film/Movie.1985.mkv", []byte("the original"))
	dir := filepath.Join(f.cfg.MediaDir, "Movies/Film")
	own := filepath.Join(dir, "Movie.en.srt")
	theirs := filepath.Join(dir, "Movie.1985.srt")
	for name, content := range map[string]string{own: "remake subtitles", theirs: "original subtitles"} {
		if err := os.WriteFile(name, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := f.s.PreviewDeletion(ctx, DeletionRequest{Selection: "video_files", IDs: []int64{1}, Mode: "delete"})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range p.Removal.Assets {
		if filepath.Join(f.cfg.MediaDir, filepath.FromSlash(a.Source)) == theirs {
			t.Fatalf("the review claims the other film's subtitles: %+v", p.Removal.Assets)
		}
	}
	if _, err = f.s.Apply(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err = New(f.db, f.cfg).Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(theirs); err != nil || string(b) != "original subtitles" {
		t.Fatalf("the other film's subtitles were removed: %q %v", b, err)
	}
	for _, name := range []string{own, filepath.Join(dir, "Movie.mkv")} {
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Fatalf("selected path %s was kept: %v", name, err)
		}
	}
}

// Deleting a track's primary file promotes one of its copies, as the
// conversion queue does. A primary file never carries a variant label.
func TestMusicDeletionPromotesACopyWithoutItsVariantLabel(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	_, err := f.db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,status,created_at,updated_at) VALUES(1,'local-a','Artist','available',1,1);
		INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,status,created_at,updated_at) VALUES(2,1,'local-b','Album','available',1,1);
		INSERT INTO music_tracks(id,album_id,disc,number,title,created_at,updated_at) VALUES(3,2,1,1,'One',1,1);
		INSERT INTO music_files(id,artist_id,album_id,track_id,relative_path,size_bytes,imported_at,variant_role,variant_profile)
		VALUES(6,1,2,3,'Music/one.flac',3,1,'primary',''),(7,1,2,3,'Music/one [mp3-192].mp3',3,1,'variant','mp3-192')`)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.s.PreviewDeletion(ctx, DeletionRequest{Selection: "music_files", IDs: []int64{6}, Mode: "keep"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err = New(f.db, f.cfg).Tick(ctx); err != nil {
		t.Fatal(err)
	}
	var role, profile string
	if err = f.db.QueryRow(`SELECT variant_role,variant_profile FROM music_files WHERE id=7`).Scan(&role, &profile); err != nil {
		t.Fatal(err)
	}
	if role != "primary" || profile != "" {
		t.Fatalf("promoted copy has role %q and variant label %q; want primary with no label", role, profile)
	}
}
