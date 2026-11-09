package duplicates

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func exec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func insertMovie(t *testing.T, db *sql.DB, id int64, title string) {
	t.Helper()
	exec(t, db, `INSERT INTO media_items(id,kind,tmdb_id,title,year) VALUES(?,?,?,?,2020)`, id, "movie", 1000+id, title)
}

func insertMovieFile(t *testing.T, db *sql.DB, id, movieID, size int64, sha, probe, path string) {
	t.Helper()
	exec(t, db, `INSERT INTO media_files(id,movie_id,file_index,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name)
 VALUES(?,?,-1,?,?,?,?,1,'existing',?)`, id, movieID, path, size, sha, probe, path)
}

func setupDuplicates(t *testing.T) *sql.DB {
	t.Helper()
	db := openTestDB(t)

	// Alpha: byte-identical copies and the same inspected resolution.
	insertMovie(t, db, 1, "Alpha")
	insertMovieFile(t, db, 1, 1, 100, "same-sha", `{"width":1920,"height":1080}`, "Movies/Alpha/a.mkv")
	insertMovieFile(t, db, 2, 1, 100, "same-sha", `{"width":1920,"height":1080}`, "Movies/Alpha/b.mkv")

	// Bravo: equal sizes but different content.
	insertMovie(t, db, 2, "Bravo")
	insertMovieFile(t, db, 3, 2, 200, "bravo-one", `{"width":1920,"height":1080}`, "Movies/Bravo/a.mkv")
	insertMovieFile(t, db, 4, 2, 200, "bravo-two", `{"width":1920,"height":1080}`, "Movies/Bravo/b.mkv")

	// Charlie: different sizes.
	insertMovie(t, db, 3, "Charlie")
	insertMovieFile(t, db, 5, 3, 300, "charlie", `{"width":1920,"height":1080}`, "Movies/Charlie/a.mkv")
	insertMovieFile(t, db, 6, 3, 301, "charlie", `{"width":1920,"height":1080}`, "Movies/Charlie/b.mkv")

	// Delta: one file is not a duplicate review.
	insertMovie(t, db, 4, "Delta")
	insertMovieFile(t, db, 7, 4, 400, "delta", `{"width":1920,"height":1080}`, "Movies/Delta/a.mkv")

	// Echo: identical bytes but conflicting inspection metadata.
	insertMovie(t, db, 5, "Echo")
	insertMovieFile(t, db, 8, 5, 500, "echo", `{"width":1920,"height":1080}`, "Movies/Echo/a.mkv")
	insertMovieFile(t, db, 9, 5, 500, "echo", `{"width":3840,"height":2160}`, "Movies/Echo/b.mkv")

	// Foxtrot: a missing checksum is never enough evidence.
	insertMovie(t, db, 6, "Foxtrot")
	insertMovieFile(t, db, 10, 6, 600, "", `{"width":1920,"height":1080}`, "Movies/Foxtrot/a.mkv")
	insertMovieFile(t, db, 11, 6, 600, "", `{"width":1920,"height":1080}`, "Movies/Foxtrot/b.mkv")

	// A show with one file per episode, plus two byte-identical files attached
	// to different episodes. Neither is a same-episode duplicate group.
	exec(t, db, `INSERT INTO media_items(id,kind,tmdb_id,title) VALUES(10,'series',5000,'A Show')`)
	exec(t, db, `INSERT INTO series_details(media_id) VALUES(10)`)
	exec(t, db, `INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,loaded_at)
 VALUES(1,10,6000,1,'Season 1','','',2,'loaded')`)
	exec(t, db, `INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date)
 VALUES(1,1,7000,1,'One','',''),(2,1,7001,2,'Two','','')`)
	exec(t, db, `INSERT INTO media_files(id,series_id,file_index,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name)
 VALUES(20,10,-1,'TV/A Show/S01E01.mkv',50,'shared-sha','{"width":1280,"height":720}',1,'existing','one')`)
	exec(t, db, `INSERT INTO media_files(id,series_id,file_index,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name)
 VALUES(21,10,-1,'TV/A Show/S01E02.mkv',50,'shared-sha','{"width":1280,"height":720}',1,'existing','two')`)
	exec(t, db, `INSERT INTO episode_files(episode_id,media_file_id) VALUES(1,20),(2,21)`)
	return db
}

func TestReviewClassifiesMovieGroups(t *testing.T) {
	db := setupDuplicates(t)
	report, err := Review(context.Background(), db, 10)
	if err != nil {
		t.Fatal(err)
	}
	if report.Total != 5 || report.Truncated || len(report.Groups) != 5 {
		t.Fatalf("bad report bounds: %+v", report)
	}
	want := []struct {
		title, status, reason string
		files                 int
	}{
		{"Alpha", StatusProbableDuplicate, "identical size, SHA-256 and resolution", 2},
		{"Bravo", StatusNeedsAttention, "SHA-256 differs", 2},
		{"Charlie", StatusNeedsAttention, "size 301 differs", 2},
		{"Echo", StatusNeedsAttention, "resolution 2160p differs", 2},
		{"Foxtrot", StatusNeedsAttention, "no SHA-256", 2},
	}
	for i, expected := range want {
		g := report.Groups[i]
		if g.Kind != "movie" || g.Title != expected.title || g.Status != expected.status || g.FileCount != expected.files || len(g.Files) != expected.files {
			t.Fatalf("group %d = %+v", i, g)
		}
		if !strings.Contains(g.Reason, expected.reason) {
			t.Fatalf("group %q reason %q does not contain %q", g.Title, g.Reason, expected.reason)
		}
	}
	alpha := report.Groups[0]
	if alpha.MediaID != 1 || alpha.EpisodeID != nil || alpha.Files[0].RelativePath != "Movies/Alpha/a.mkv" || alpha.Files[1].RelativePath != "Movies/Alpha/b.mkv" {
		t.Fatalf("unexpected alpha group: %+v", alpha)
	}
	if alpha.Files[0].Resolution != 1080 || alpha.Files[0].SizeBytes != 100 || alpha.Files[0].SHA256 != "same-sha" {
		t.Fatalf("unexpected alpha file: %+v", alpha.Files[0])
	}
}

func TestReviewDoesNotMergeEpisodeIdentities(t *testing.T) {
	db := setupDuplicates(t)
	report, err := Review(context.Background(), db, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range report.Groups {
		if g.Kind == "episode" {
			t.Fatalf("one registered file per episode must not form a group: %+v", g)
		}
	}

	// A shared file serving several episodes is also not a duplicate of itself.
	exec(t, db, `INSERT INTO episode_files(episode_id,media_file_id) VALUES(2,20) ON CONFLICT(episode_id) DO UPDATE SET media_file_id=20`)
	report, err = Review(context.Background(), db, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range report.Groups {
		if g.Kind == "episode" {
			t.Fatalf("shared range file must not be reported as a duplicate group: %+v", g)
		}
	}
}

func TestReviewLimitIsBoundedAndReportsTruncation(t *testing.T) {
	db := setupDuplicates(t)
	report, err := Review(context.Background(), db, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Groups) != 2 || report.Total != 5 || !report.Truncated || report.Limit != 2 {
		t.Fatalf("unexpected bounded report: %+v", report)
	}
	report, err = Review(context.Background(), db, 0)
	if err != nil {
		t.Fatal(err)
	}
	if report.Limit != DefaultLimit || len(report.Groups) != 5 || report.Truncated {
		t.Fatalf("unexpected default report: %+v", report)
	}
	report, err = Review(context.Background(), db, MaxLimit+1000)
	if err != nil {
		t.Fatal(err)
	}
	if report.Limit != MaxLimit {
		t.Fatalf("limit not clamped: %d", report.Limit)
	}
}

func TestReviewIsReadOnly(t *testing.T) {
	db := setupDuplicates(t)
	var beforeFiles, beforeEpisodes int
	if err := db.QueryRow(`SELECT COUNT(*) FROM media_files`).Scan(&beforeFiles); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM episode_files`).Scan(&beforeEpisodes); err != nil {
		t.Fatal(err)
	}
	if _, err := Review(context.Background(), db, 50); err != nil {
		t.Fatal(err)
	}
	var afterFiles, afterEpisodes int
	var path, sha string
	if err := db.QueryRow(`SELECT COUNT(*) FROM media_files`).Scan(&afterFiles); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM episode_files`).Scan(&afterEpisodes); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT relative_path,sha256 FROM media_files WHERE id=1`).Scan(&path, &sha); err != nil {
		t.Fatal(err)
	}
	if beforeFiles != afterFiles || beforeEpisodes != afterEpisodes || path != "Movies/Alpha/a.mkv" || sha != "same-sha" {
		t.Fatal("duplicate review changed library rows")
	}
}

func TestReviewHonorsCancellation(t *testing.T) {
	db := setupDuplicates(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Review(ctx, db, 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestClassifyEpisodeFiles(t *testing.T) {
	identical := []File{
		{ID: 1, RelativePath: "a.mkv", SizeBytes: 10, SHA256: "x", Resolution: 1080},
		{ID: 2, RelativePath: "b.mkv", SizeBytes: 10, SHA256: "x", Resolution: 1080},
	}
	if status, _ := classify(identical); status != StatusProbableDuplicate {
		t.Fatalf("identical episode files should be probable duplicates: %s", status)
	}
	different := []File{
		{ID: 1, RelativePath: "a.mkv", SizeBytes: 10, SHA256: "x", Resolution: 1080},
		{ID: 2, RelativePath: "b.mkv", SizeBytes: 11, SHA256: "x", Resolution: 1080},
	}
	if status, reason := classify(different); status != StatusNeedsAttention || !strings.Contains(reason, "size") {
		t.Fatalf("different episode files should need attention: %s %s", status, reason)
	}
}
