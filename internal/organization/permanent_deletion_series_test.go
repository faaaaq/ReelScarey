package organization

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// A whole-series permanent deletion must remove every registered episode file
// and its catalog closure without staging anything in .reelscarey-trash.
func TestPermanentDeletionOfWholeSeriesRemovesEpisodeFiles(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	const (
		show       = "TV/Series"
		s1e1       = show + "/Season 1/S01E01.mkv"
		s1e1Sub    = show + "/Season 1/S01E01.en.srt"
		s1e2       = show + "/Season 1/S01E02.mkv"
		s2e1       = show + "/Season 2/S02E01.mkv"
		otherMovie = "Movies/Untouched/movie.mkv"
	)
	series := func() {
		t.Helper()
		if _, err := f.db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,status) VALUES(1,'series',500,'Fixture Show','available');
			INSERT INTO series_details(media_id) VALUES(1);
			INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,loaded_at) VALUES(11,1,600,1,'Season 1','','',2,1),(12,1,601,2,'Season 2','','',1,1);
			INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date) VALUES(21,11,700,1,'One','',''),(22,11,701,2,'Two','',''),(23,12,702,1,'Three','','')`); err != nil {
			t.Fatal(err)
		}
	}
	series()
	file := func(id int64, path string, body []byte) {
		t.Helper()
		dest := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, body, 0600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		if _, err := f.db.Exec(`INSERT INTO media_files(id,series_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name)
			VALUES(?,1,?,?,?,'{}',1,'existing','fixture.mkv')`, id, path, len(body), hex.EncodeToString(sum[:])); err != nil {
			t.Fatal(err)
		}
	}
	file(20, s1e1, []byte("episode one"))
	file(21, s1e2, []byte("episode two"))
	file(22, s2e1, []byte("season two episode"))
	if err := os.WriteFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(s1e1Sub)), []byte("owned sidecar"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO episode_files(episode_id,media_file_id) VALUES(21,20),(22,21),(23,22)`); err != nil {
		t.Fatal(err)
	}
	f.movie(t, 9, otherMovie, []byte("unrelated movie"))

	plan, err := f.s.PreviewDeletion(ctx, DeletionRequest{Selection: "titles", IDs: []int64{1}, Mode: "delete"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{s1e1: true, s1e1Sub: true, s1e2: true, s2e1: true}
	if len(plan.Removal.Paths) != len(want) {
		t.Fatalf("reviewed paths: %v", plan.Removal.Paths)
	}
	for _, path := range plan.Removal.Paths {
		if !want[path] {
			t.Fatalf("unexpected reviewed path %q", path)
		}
	}
	if _, err = f.s.Apply(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	if err = New(f.db, f.cfg).Tick(ctx); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{s1e1, s1e1Sub, s1e2, s2e1} {
		if _, err = os.Stat(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(name))); !os.IsNotExist(err) {
			t.Fatalf("episode file remains: %s (%v)", name, err)
		}
	}
	if _, err = os.Stat(filepath.Join(f.cfg.MediaDir, ".reelscarey-trash")); !os.IsNotExist(err) {
		t.Fatal("permanent deletion staged a trash folder", err)
	}
	if _, err = os.Stat(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(otherMovie))); err != nil {
		t.Fatal("unrelated title was touched", err)
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM media_items WHERE id=1`,
		`SELECT COUNT(*) FROM episode_files WHERE episode_id IN (21,22,23)`,
		`SELECT COUNT(*) FROM media_files WHERE series_id=1`,
	} {
		var count int
		if err = f.db.QueryRow(query).Scan(&count); err != nil || count != 0 {
			t.Fatalf("catalog closure remains (%s): %d %v", query, count, err)
		}
	}
	var untouched int
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM media_items WHERE id=9`).Scan(&untouched); err != nil || untouched != 1 {
		t.Fatalf("unrelated title affected: %d %v", untouched, err)
	}
}
