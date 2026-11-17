package organization

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLibraryDeletionRemovesSelectedFilesAndRetainsSeeds(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	const movie = "Movies/Titles/first.mkv"
	const sub = "Movies/Titles/first.eng.srt"
	const other = "Movies/Titles/second.mkv"
	f.movie(t, 1, movie, []byte("original movie"))
	f.movie(t, 2, other, []byte("another movie"))
	pathFor := func(name string) string { return filepath.Join(f.cfg.MediaDir, filepath.FromSlash(name)) }
	if err := os.WriteFile(pathFor(sub), []byte("English sidecar"), 0600); err != nil {
		t.Fatal(err)
	}
	seed := filepath.Join(f.cfg.DownloadsDir, "seed.mkv")
	if err := os.Link(pathFor(movie), seed); err != nil {
		t.Fatal(err)
	}
	plan, err := f.s.PreviewDeletion(ctx, DeletionRequest{Selection: "video_files", IDs: []int64{1}, Mode: "trash"})
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Removal.Paths; !reflect.DeepEqual(got, []string{movie, sub}) {
		t.Fatalf("paths: %v", got)
	}
	if _, err = f.s.Apply(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	if err = New(f.db, f.cfg).Tick(ctx); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{movie, sub} {
		if _, err = os.Stat(pathFor(name)); !os.IsNotExist(err) {
			t.Fatalf("source %s remains: %v", name, err)
		}
		if _, err = os.Stat(pathFor(".reelscarey-trash/" + plan.ID + "/" + name)); err != nil {
			t.Fatal(err)
		}
	}
	if b, err := os.ReadFile(seed); err != nil || string(b) != "original movie" {
		t.Fatalf("seed: %q %v", b, err)
	}
	var status string
	if err = f.db.QueryRow("SELECT status FROM media_items WHERE id=1").Scan(&status); err != nil || status != "missing" {
		t.Fatalf("status: %q %v", status, err)
	}
	if _, err = os.Stat(pathFor(other)); err != nil {
		t.Fatal(err)
	}
}

func TestMusicDeletionDeletesSelectedTracksAndTheirFiles(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	_, err := f.db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,status,created_at,updated_at) VALUES(1,'local-a','Artist','available',1,1);
		INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,status,created_at,updated_at) VALUES(2,1,'local-b','Album','available',1,1);
		INSERT INTO music_tracks(id,album_id,disc,number,title,created_at,updated_at) VALUES(3,2,1,1,'One',1,1),(4,2,1,2,'Two',1,1);
		INSERT INTO music_files(id,artist_id,album_id,track_id,relative_path,size_bytes,imported_at) VALUES(6,1,2,3,'Music/one.flac',3,1),(7,1,2,4,'Music/two.flac',3,1)`)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.s.PreviewDeletion(ctx, DeletionRequest{Selection: "tracks", IDs: []int64{4, 3}, Mode: "keep"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err = New(f.db, f.cfg).Tick(ctx); err != nil {
		t.Fatal(err)
	}
	var tracks, files int
	var status string
	err = f.db.QueryRow(`SELECT (SELECT count(*) FROM music_tracks),(SELECT count(*) FROM music_files),(SELECT status FROM music_albums WHERE id=2)`).Scan(&tracks, &files, &status)
	if err != nil || tracks != 0 || files != 0 || status != "missing" {
		t.Fatalf("tracks %d files %d album %q: %v", tracks, files, status, err)
	}
}

func TestTrackCleanupScopeAndForcedSidecarPolicy(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/Film/movie.mkv", []byte("video"))
	f.movie(t, 2, "Anime/Film/anime.mkv", []byte("anime"))
	if _, err := f.db.Exec(`UPDATE media_items SET category='anime' WHERE id=2; UPDATE subtitle_preferences SET tracks='[{"language":"en","variant":"full"}]'`); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"eng.srt", "spa.srt", "spa.forced.srt", "srt"} {
		name := filepath.Join(f.cfg.MediaDir, "Movies", "Film", "movie."+suffix)
		if err := os.WriteFile(name, []byte("sidecar"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	f.s.SetTrackCleaner(func(context.Context, *os.File) ([]TrackStream, error) { return nil, nil },
		func(context.Context, *os.File, *os.File, string, []int) (string, error) {
			t.Fatal("sidecar-only run attempted a remux")
			return "", nil
		})
	p, err := f.s.PreviewTrackCleanup(ctx, TrackCleanupRequest{Mode: "other_non_forced", Library: "movies"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.TrackCleanup.Targets) != 1 || p.TrackCleanup.Targets[0].ID != 1 {
		t.Fatalf("scope: %+v", p.TrackCleanup.Targets)
	}
	if _, err = f.s.Apply(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = f.s.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	p, err = f.s.Plan(ctx, p.ID)
	if err != nil || p.State != "completed" || p.TrackCleanup.Dropped != 2 || p.TrackCleanup.Skipped != 0 {
		t.Fatalf("cleanup: %+v %v", p, err)
	}
	for _, suffix := range []string{"eng.srt", "spa.forced.srt"} {
		if _, err = os.Stat(filepath.Join(f.cfg.MediaDir, "Movies", "Film", "movie."+suffix)); err != nil {
			t.Fatal(err)
		}
	}
	for _, suffix := range []string{"spa.srt", "srt"} {
		if _, err = os.Stat(filepath.Join(f.cfg.MediaDir, "Movies", "Film", "movie."+suffix)); !os.IsNotExist(err) {
			t.Fatalf("%s was retained: %v", suffix, err)
		}
	}
}

func TestTrackCleanupProtectsOriginalConfiguredAndUnknownAudio(t *testing.T) {
	target := cleanupTarget{Original: "jpn", Languages: []string{"en", "pt-br"}}
	streams := []TrackStream{
		{Index: 1, Kind: "audio", Language: "ja"},
		{Index: 2, Kind: "audio", Language: "eng"},
		{Index: 3, Kind: "audio", Language: "spa"},
		{Index: 4, Kind: "audio", Language: "und"},
		{Index: 5, Kind: "audio", Language: "pt-PT"},
		{Index: 6, Kind: "subtitle", Language: "spa", Forced: true},
	}
	if got := dropEmbedded("other_audio", target, streams); !reflect.DeepEqual(got, []int{3, 5}) {
		t.Fatal(got)
	}
	if got := dropEmbedded("other_non_forced", target, streams); len(got) != 0 {
		t.Fatal(got)
	}
	if got := dropEmbedded("all_subtitles", target, streams); !reflect.DeepEqual(got, []int{6}) {
		t.Fatal(got)
	}
	if got := dropEmbedded("other_audio", target, streams[2:3]); len(got) != 0 {
		t.Fatal("would remove all audio", got)
	}
}

func TestRegisteredVideoQueuesCanonicalEmbeddedTitle(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/film.mkv", []byte("registered video"))
	if _, err := f.db.Exec(`UPDATE media_items SET title='Provider Title' WHERE id=1; UPDATE media_files SET probe_json='{"embedded_title":"Old title"}' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	f.s.SetProbe(func(context.Context, *os.File) (string, error) { return "{}", nil })
	f.s.SetTagger(func(context.Context, *os.File, *os.File, string, string) (string, error) {
		t.Fatal("planning rewrote the source")
		return "", nil
	})
	if err := f.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := f.db.QueryRow(`SELECT json_extract(snapshot,'$.replacement.tag_title') FROM organization_plans WHERE state='queued'`).Scan(&raw); err != nil || raw != "Provider Title" {
		t.Fatalf("queued title %q: %v", raw, err)
	}
	if got := registeredEmbeddedTitle("Show", []Episode{{Season: 1, Number: 2, Title: "Canonical Episode"}}); got != "Canonical Episode" {
		t.Fatal(got)
	}
}
