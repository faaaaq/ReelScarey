package nfoexport

import (
	"context"
	"database/sql"
	"encoding/xml"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
)

type fixture struct {
	db   *sql.DB
	root string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := database.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err = db.Exec(`UPDATE settings SET nfo_export=1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	return &fixture{db: db, root: t.TempDir()}
}

func (f *fixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.db.Exec(query, args...); err != nil {
		t.Fatalf("exec %s: %v", query, err)
	}
}

func (f *fixture) worker() *Worker {
	return &Worker{DB: f.db, MediaRoot: f.root, Interval: time.Millisecond}
}

func (f *fixture) writeMedia(t *testing.T, relative, content string) {
	t.Helper()
	name := filepath.Join(f.root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) addMovie(t *testing.T, mediaID, fileID int64, title string, year int, tmdbID int64, overview, releaseDate, relative string) {
	t.Helper()
	f.exec(t, `INSERT INTO media_items(id,kind,tmdb_id,title,year,overview,release_date) VALUES(?, 'movie', ?, ?, ?, ?, ?)`,
		mediaID, tmdbID, title, year, overview, releaseDate)
	f.exec(t, `INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin) VALUES(?,?,?,?,?,?,?, 'existing')`,
		fileID, mediaID, relative, 11, "sha-movie", `{"width":1920}`, 1)
}

func (f *fixture) addSeries(t *testing.T, seriesID int64, title string, year int, seasonID int64, seasonNumber int) {
	t.Helper()
	f.exec(t, `INSERT INTO media_items(id,kind,tmdb_id,title,year) VALUES(?, 'series', ?, ?, ?)`,
		seriesID, 9000+seriesID, title, year)
	f.exec(t, `INSERT INTO series_details(media_id) VALUES(?)`, seriesID)
	f.exec(t, `INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,loaded_at) VALUES(?,?,?,?,?,?,?,?, 'loaded')`,
		seasonID, seriesID, 8000+seasonID, seasonNumber, "Season", "", "", 10)
}

func (f *fixture) addEpisode(t *testing.T, episodeID, seasonID int64, number int, title, overview, airDate string, tmdbID int64) {
	t.Helper()
	f.exec(t, `INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date) VALUES(?,?,?,?,?,?,?)`,
		episodeID, seasonID, tmdbID, number, title, overview, airDate)
}

func (f *fixture) addEpisodeFile(t *testing.T, mediaID, seriesID, episodeID int64, relative string) {
	t.Helper()
	f.exec(t, `INSERT INTO media_files(id,series_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin) VALUES(?,?,?,?,?,?,?, 'existing')`,
		mediaID, seriesID, relative, 13, "sha-episode", `{"width":1920}`, 1)
	f.exec(t, `INSERT INTO episode_files(episode_id,media_file_id) VALUES(?,?)`, episodeID, mediaID)
}

func sidecarName(t *testing.T, root, relative string) string {
	t.Helper()
	clean := filepath.FromSlash(relative)
	extension := filepath.Ext(clean)
	return filepath.Join(root, strings.TrimSuffix(clean, extension)+".nfo")
}

type movieNFO struct {
	XMLName   xml.Name `xml:"movie"`
	Title     string   `xml:"title"`
	Year      int      `xml:"year"`
	Plot      string   `xml:"plot"`
	Premiered string   `xml:"premiered"`
	UniqueID  struct {
		Type  string `xml:"type,attr"`
		Value int64  `xml:",chardata"`
	} `xml:"uniqueid"`
}

type episodeNFO struct {
	XMLName   xml.Name `xml:"episodedetails"`
	Title     string   `xml:"title"`
	Year      int      `xml:"year"`
	Season    int      `xml:"season"`
	Episode   int      `xml:"episode"`
	Plot      string   `xml:"plot"`
	Premiered string   `xml:"premiered"`
	UniqueID  struct {
		Type  string `xml:"type,attr"`
		Value int64  `xml:",chardata"`
	} `xml:"uniqueid"`
}

func TestWritesMovieNFOWithoutChangingMedia(t *testing.T) {
	f := newFixture(t)
	relative := "Movies/Test Movie (2020)/Test Movie (2020).mkv"
	f.writeMedia(t, relative, "generated movie bytes")
	f.addMovie(t, 1, 1, "Test Movie & Friends", 2020, 4242, "A <plot> for the test.", "2020-05-01", relative)
	w := f.worker()

	worked, err := w.ProcessOne(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !worked {
		t.Fatal("movie NFO was not written")
	}
	target := sidecarName(t, f.root, relative)
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	var parsed movieNFO
	if err = xml.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Title != "Test Movie & Friends" || parsed.Year != 2020 || parsed.Plot != "A <plot> for the test." ||
		parsed.Premiered != "2020-05-01" || parsed.UniqueID.Type != "tmdb" || parsed.UniqueID.Value != 4242 {
		t.Fatalf("unexpected movie NFO: %+v", parsed)
	}
	media, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	if string(media) != "generated movie bytes" {
		t.Fatal("media bytes were modified")
	}
	var probe, analysis string
	if err = f.db.QueryRow(`SELECT probe_json,analysis_json FROM media_files WHERE id=1`).Scan(&probe, &analysis); err != nil {
		t.Fatal(err)
	}
	if probe != `{"width":1920}` || analysis != "" {
		t.Fatalf("library metadata was modified: probe=%q analysis=%q", probe, analysis)
	}
}

func TestWritesEpisodeNFO(t *testing.T) {
	f := newFixture(t)
	relative := "TV Shows/Test Show/Season 1/Test Show.S01E02.mkv"
	f.writeMedia(t, relative, "generated episode bytes")
	f.addSeries(t, 10, "Test Show", 2019, 20, 1)
	f.addEpisode(t, 30, 20, 2, "Second Episode", "An episode <plot>.", "2019-03-02", 5555)
	f.addEpisodeFile(t, 40, 10, 30, relative)
	w := f.worker()

	worked, err := w.ProcessOne(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !worked {
		t.Fatal("episode NFO was not written")
	}
	raw, err := os.ReadFile(sidecarName(t, f.root, relative))
	if err != nil {
		t.Fatal(err)
	}
	var parsed episodeNFO
	if err = xml.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Title != "Second Episode" || parsed.Year != 2019 || parsed.Season != 1 || parsed.Episode != 2 ||
		parsed.Plot != "An episode <plot>." || parsed.Premiered != "2019-03-02" || parsed.UniqueID.Value != 5555 {
		t.Fatalf("unexpected episode NFO: %+v", parsed)
	}
}

func TestDoesNothingWhenDisabled(t *testing.T) {
	f := newFixture(t)
	relative := "Movies/Disabled/Disabled.mkv"
	f.writeMedia(t, relative, "bytes")
	f.addMovie(t, 1, 1, "Disabled", 2020, 1, "Plot", "2020-01-01", relative)
	f.exec(t, `UPDATE settings SET nfo_export=0 WHERE id=1`)

	worked, err := f.worker().ProcessOne(context.Background())
	if err != nil || worked {
		t.Fatalf("ProcessOne = (%v, %v), want (false, nil)", worked, err)
	}
	if _, err = os.Stat(sidecarName(t, f.root, relative)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disabled export wrote a sidecar: %v", err)
	}
}

func TestRefusesToReplaceExistingNFO(t *testing.T) {
	f := newFixture(t)
	relative := "Movies/Existing/Existing.mkv"
	f.writeMedia(t, relative, "bytes")
	f.addMovie(t, 1, 1, "Existing", 2020, 7, "New plot", "2020-01-01", relative)
	target := sidecarName(t, f.root, relative)
	if err := os.WriteFile(target, []byte("owner supplied nfo"), 0o644); err != nil {
		t.Fatal(err)
	}

	worked, err := f.worker().ProcessOne(context.Background())
	if err != nil || worked {
		t.Fatalf("ProcessOne = (%v, %v), want (false, nil)", worked, err)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "owner supplied nfo" {
		t.Fatal("existing NFO was replaced")
	}
}

func TestSecondPassDoesNotRewrite(t *testing.T) {
	f := newFixture(t)
	relative := "Movies/Once/Once.mkv"
	f.writeMedia(t, relative, "bytes")
	f.addMovie(t, 1, 1, "Once", 2021, 8, "Plot", "2021-02-03", relative)
	w := f.worker()
	worked, err := w.ProcessOne(context.Background())
	if err != nil || !worked {
		t.Fatalf("first ProcessOne = (%v, %v)", worked, err)
	}
	target := sidecarName(t, f.root, relative)
	before, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if worked, err = w.ProcessOne(context.Background()); err != nil || worked {
		t.Fatalf("second ProcessOne = (%v, %v), want (false, nil)", worked, err)
	}
	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("second pass rewrote the NFO")
	}
}

func TestSkipsAmbiguousSharedEpisodeFile(t *testing.T) {
	f := newFixture(t)
	relative := "TV Shows/Shared/Season 1/Shared.mkv"
	f.writeMedia(t, relative, "shared bytes")
	f.addSeries(t, 10, "Shared Show", 2021, 20, 1)
	f.addEpisode(t, 30, 20, 1, "First", "", "2021-01-01", 1001)
	f.addEpisode(t, 31, 20, 2, "Second", "", "2021-01-08", 1002)
	f.exec(t, `INSERT INTO media_files(id,series_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin) VALUES(40,10,?,1,'shared','{}',1,'existing')`, relative)
	f.exec(t, `INSERT INTO episode_files(episode_id,media_file_id) VALUES(30,40),(31,40)`)

	worked, err := f.worker().ProcessOne(context.Background())
	if !errors.Is(err, ErrAmbiguousEpisode) || !worked {
		t.Fatalf("ProcessOne = (%v, %v), want (true, ErrAmbiguousEpisode)", worked, err)
	}
	if _, statErr := os.Stat(sidecarName(t, f.root, relative)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("ambiguous shared file wrote a sidecar: %v", statErr)
	}
}

func TestUsesSingleExactEpisodeLabelForSharedFile(t *testing.T) {
	f := newFixture(t)
	relative := "TV Shows/Labeled/Season 1/Labeled.S01E02.mkv"
	f.writeMedia(t, relative, "shared bytes")
	f.addSeries(t, 10, "Labeled Show", 2021, 20, 1)
	f.addEpisode(t, 30, 20, 1, "First", "", "2021-01-01", 1001)
	f.addEpisode(t, 31, 20, 2, "Second", "", "2021-01-08", 1002)
	f.exec(t, `INSERT INTO media_files(id,series_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin) VALUES(40,10,?,1,'shared','{}',1,'existing')`, relative)
	f.exec(t, `INSERT INTO episode_files(episode_id,media_file_id) VALUES(30,40),(31,40)`)

	worked, err := f.worker().ProcessOne(context.Background())
	if err != nil || !worked {
		t.Fatalf("ProcessOne = (%v, %v)", worked, err)
	}
	raw, err := os.ReadFile(sidecarName(t, f.root, relative))
	if err != nil {
		t.Fatal(err)
	}
	var parsed episodeNFO
	if err = xml.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Title != "Second" || parsed.Season != 1 || parsed.Episode != 2 {
		t.Fatalf("labeled shared file selected the wrong episode: %+v", parsed)
	}
}

func TestRejectsTraversalSymlinkAndMissingMedia(t *testing.T) {
	t.Run("traversal", func(t *testing.T) {
		f := newFixture(t)
		f.addMovie(t, 1, 1, "Escape", 2020, 1, "", "", "../outside.mkv")
		worked, err := f.worker().ProcessOne(context.Background())
		if !errors.Is(err, ErrUnsafePath) || !worked {
			t.Fatalf("ProcessOne = (%v, %v), want (true, ErrUnsafePath)", worked, err)
		}
		if _, statErr := os.Stat(filepath.Join(f.root, "outside.nfo")); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("traversal wrote outside the root: %v", statErr)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		f := newFixture(t)
		real := filepath.Join(f.root, "real")
		if err := os.MkdirAll(real, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(real, "linked.mkv"), []byte("bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, filepath.Join(f.root, "link")); err != nil {
			t.Skipf("symbolic links are unavailable: %v", err)
		}
		f.addMovie(t, 1, 1, "Linked", 2020, 1, "", "", "link/linked.mkv")
		worked, err := f.worker().ProcessOne(context.Background())
		if !errors.Is(err, ErrUnsafePath) || !worked {
			t.Fatalf("ProcessOne = (%v, %v), want (true, ErrUnsafePath)", worked, err)
		}
		if _, statErr := os.Stat(filepath.Join(real, "linked.nfo")); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("symlink wrote through to a real folder: %v", statErr)
		}
	})

	t.Run("missing", func(t *testing.T) {
		f := newFixture(t)
		f.addMovie(t, 1, 1, "Missing", 2020, 1, "", "", "Movies/Missing/Missing.mkv")
		worked, err := f.worker().ProcessOne(context.Background())
		if !errors.Is(err, ErrMissingMedia) || !worked {
			t.Fatalf("ProcessOne = (%v, %v), want (true, ErrMissingMedia)", worked, err)
		}
		if _, statErr := os.Stat(filepath.Join(f.root, "Movies/Missing/Missing.nfo")); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("missing media wrote a sidecar: %v", statErr)
		}
	})
}

func TestRunStopsWhenContextIsCancelled(t *testing.T) {
	f := newFixture(t)
	w := f.worker()
	w.Interval = time.Millisecond
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx, logger)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
}
