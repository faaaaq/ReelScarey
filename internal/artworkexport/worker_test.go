package artworkexport

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// The tests mirror only the schema columns the worker reads (settings,
// media_items, media_files; artwork_export column from
// internal/database/migrations/084_artwork_export.sql) so they stay hermetic
// and fast without running the full migration set.

type fixture struct {
	t         *testing.T
	db        *sql.DB
	root      string
	worker    *Worker
	transport *posterTransport
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	schema := []string{
		`CREATE TABLE settings (id INTEGER PRIMARY KEY, artwork_export INTEGER NOT NULL DEFAULT 0 CHECK(artwork_export IN (0,1)))`,
		`CREATE TABLE media_items (id INTEGER PRIMARY KEY, poster_path TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE media_files (id INTEGER PRIMARY KEY, relative_path TEXT NOT NULL, movie_id INTEGER)`,
	}
	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("create schema: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO settings(id, artwork_export) VALUES(1, 1)`); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	root := t.TempDir()
	f := &fixture{t: t, db: db, root: root, transport: &posterTransport{}}
	f.worker = &Worker{DB: db, MediaRoot: root, Interval: time.Millisecond}
	f.worker.client = &http.Client{Transport: f.transport}
	return f
}

func (f *fixture) addMovie(id int64, relative, posterPath string) {
	f.t.Helper()
	if _, err := f.db.Exec(`INSERT INTO media_items(id, poster_path) VALUES(?, ?)`, id, posterPath); err != nil {
		f.t.Fatalf("seed media item: %v", err)
	}
	if _, err := f.db.Exec(`INSERT INTO media_files(id, relative_path, movie_id) VALUES(?, ?, ?)`, id, relative, id); err != nil {
		f.t.Fatalf("seed media file: %v", err)
	}
	dir := filepath.Join(f.root, filepath.Dir(relative))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.t.Fatalf("create media folder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(f.root, relative), []byte("movie-bytes"), 0o644); err != nil {
		f.t.Fatalf("create media file: %v", err)
	}
}

func (f *fixture) posterOf(id int64, relative string) string {
	f.t.Helper()
	_ = id
	return filepath.Join(f.root, filepath.Dir(relative), "poster.jpg")
}

func (f *fixture) setEnabled(enabled bool) {
	f.t.Helper()
	value := 0
	if enabled {
		value = 1
	}
	if _, err := f.db.Exec(`UPDATE settings SET artwork_export=? WHERE id=1`, value); err != nil {
		f.t.Fatalf("update settings: %v", err)
	}
}

// posterTransport answers poster downloads from memory; no network is touched.
// The real worker always locks its HTTP client to the fixed image.tmdb.org
// host via metadata.PosterURL, so the transport also records the requested
// URLs to prove the host stays fixed.
type posterTransport struct {
	mu          sync.Mutex
	status      int
	contentType string
	body        []byte
	err         error
	requests    []string
}

func (t *posterTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.requests = append(t.requests, r.URL.String())
	status, body, err := t.status, t.body, t.err
	t.mu.Unlock()
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	if status == 0 {
		status = http.StatusOK
	}
	header := http.Header{}
	if status == http.StatusOK {
		contentType := t.contentType
		if contentType == "" {
			contentType = "image/jpeg"
		}
		header.Set("Content-Type", contentType)
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    r,
	}, nil
}

func (t *posterTransport) hitCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.requests)
}

func (t *posterTransport) urls() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.requests...)
}

const posterBytes = "\xff\xd8\xff\xe0fake-jpeg-payload"

func TestWorkerConfiguredDisabledAndUnavailableRoot(t *testing.T) {
	t.Run("unconfigured worker reports ErrConfig", func(t *testing.T) {
		f := newFixture(t)
		f.worker.DB = nil
		if worked, err := f.worker.ProcessOne(context.Background()); worked || !errors.Is(err, ErrConfig) {
			t.Fatalf("got (%v, %v), want (false, ErrConfig)", worked, err)
		}
	})
	t.Run("empty media root reports ErrConfig", func(t *testing.T) {
		f := newFixture(t)
		f.worker.MediaRoot = "  "
		if worked, err := f.worker.ProcessOne(context.Background()); worked || !errors.Is(err, ErrConfig) {
			t.Fatalf("got (%v, %v), want (false, ErrConfig)", worked, err)
		}
	})
	t.Run("missing media root reports ErrMediaRoot", func(t *testing.T) {
		f := newFixture(t)
		f.addMovie(1, "Movies/Test Movie (2020) [tmdb-1]/movie.mkv", "/poster.jpg")
		f.worker.MediaRoot = filepath.Join(t.TempDir(), "absent")
		if worked, err := f.worker.ProcessOne(context.Background()); worked || !errors.Is(err, ErrMediaRoot) {
			t.Fatalf("got (%v, %v), want (false, ErrMediaRoot)", worked, err)
		}
	})
	t.Run("media root pointing at a file reports ErrMediaRoot", func(t *testing.T) {
		f := newFixture(t)
		f.addMovie(1, "Movies/movie.mkv", "/poster.jpg")
		f.worker.MediaRoot = filepath.Join(f.root, "Movies", "movie.mkv")
		if worked, err := f.worker.ProcessOne(context.Background()); worked || !errors.Is(err, ErrMediaRoot) {
			t.Fatalf("got (%v, %v), want (false, ErrMediaRoot)", worked, err)
		}
	})
	t.Run("disabled export does nothing and contacts nothing", func(t *testing.T) {
		f := newFixture(t)
		f.addMovie(1, "Movies/Test Movie (2020) [tmdb-1]/movie.mkv", "/poster.jpg")
		f.setEnabled(false)
		if worked, err := f.worker.ProcessOne(context.Background()); worked || err != nil {
			t.Fatalf("got (%v, %v), want (false, nil)", worked, err)
		}
		if hits := f.transport.hitCount(); hits != 0 {
			t.Fatalf("disabled export still made %d requests", hits)
		}
	})
	t.Run("missing settings row fails closed", func(t *testing.T) {
		f := newFixture(t)
		if _, err := f.db.Exec(`DELETE FROM settings WHERE id=1`); err != nil {
			t.Fatalf("delete settings: %v", err)
		}
		_, err := f.worker.ProcessOne(context.Background())
		if err == nil || !strings.Contains(err.Error(), "settings row is missing") {
			t.Fatalf("got %v, want settings-row-missing error", err)
		}
	})
}

func TestWorkerSuccessfulExportAndIdempotence(t *testing.T) {
	f := newFixture(t)
	relative := "Movies/Test Movie (2020) [tmdb-1]/movie.mkv"
	f.addMovie(1, relative, "/abc123.jpg")
	f.transport.status = http.StatusOK
	f.transport.body = []byte(posterBytes)

	worked, err := f.worker.ProcessOne(context.Background())
	if err != nil || !worked {
		t.Fatalf("ProcessOne: got (%v, %v), want (true, nil)", worked, err)
	}
	poster := f.posterOf(1, relative)
	data, err := os.ReadFile(poster)
	if err != nil {
		t.Fatalf("read poster: %v", err)
	}
	if string(data) != posterBytes {
		t.Fatalf("poster content mismatch: %q", data)
	}
	info, err := os.Stat(poster)
	if err != nil {
		t.Fatalf("stat poster: %v", err)
	}
	// The 0644 permission bits are asserted only where POSIX mode bits are
	// enforced; Windows ACLs report a different FileMode.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o644 {
		t.Fatalf("poster mode: got %v, want 0644", info.Mode().Perm())
	}
	urls := f.transport.urls()
	if len(urls) != 1 || !strings.HasPrefix(urls[0], "https://image.tmdb.org/t/p/w500/") {
		t.Fatalf("poster requests %v, want one fixed image.tmdb.org URL", urls)
	}

	// A repeat pass in the same worker must not download or rewrite anything.
	if worked, err := f.worker.ProcessOne(context.Background()); worked || err != nil {
		t.Fatalf("second pass: got (%v, %v), want (false, nil)", worked, err)
	}
	if hits := f.transport.hitCount(); hits != 1 {
		t.Fatalf("second pass made %d extra requests", hits-1)
	}

	// After a restart (fresh worker, empty attempt state) an existing poster
	// is never replaced: the run finds the candidate but skips it silently.
	restarted := &Worker{DB: f.db, MediaRoot: f.root, Interval: time.Millisecond}
	restarted.client = &http.Client{Transport: f.transport}
	if worked, err := restarted.ProcessOne(context.Background()); worked || err != nil {
		t.Fatalf("restarted pass: got (%v, %v), want (false, nil)", worked, err)
	}
	if hits := f.transport.hitCount(); hits != 1 {
		t.Fatalf("restart re-downloaded the poster (%d requests total)", hits)
	}
	data, err = os.ReadFile(poster)
	if err != nil || string(data) != posterBytes {
		t.Fatalf("existing poster was modified: %q (%v)", data, err)
	}
}

func TestWorkerExistingPosterAndCursorDrains(t *testing.T) {
	f := newFixture(t)
	first := "Movies/First (2020) [tmdb-1]/movie.mkv"
	second := "Movies/Second (2021) [tmdb-2]/movie.mkv"
	f.addMovie(1, first, "/one.jpg")
	f.addMovie(2, second, "/two.jpg")
	f.setEnabled(true)
	// Pre-existing complete posters for both candidates.
	for _, rel := range []string{first, second} {
		if err := os.WriteFile(f.posterOf(0, rel), []byte("existing-poster"), 0o644); err != nil {
			t.Fatalf("seed poster: %v", err)
		}
	}
	if worked, err := f.worker.ProcessOne(context.Background()); worked || err != nil {
		t.Fatalf("ProcessOne: got (%v, %v), want (false, nil)", worked, err)
	}
	if hits := f.transport.hitCount(); hits != 0 {
		t.Fatalf("existing posters triggered %d downloads", hits)
	}
}

func TestWorkerDownloadFailuresRetryThenGiveUp(t *testing.T) {
	f := newFixture(t)
	relative := "Movies/Test Movie (2020) [tmdb-1]/movie.mkv"
	f.addMovie(1, relative, "/abc123.jpg")

	scenarios := []struct {
		name        string
		status      int
		contentType string
		body        []byte
		wantErr     string
	}{
		{"provider error", http.StatusInternalServerError, "", nil, "HTTP 500"},
		{"non-image content type is rejected", http.StatusOK, "text/html", []byte("<html>"), "non-image response"},
		{"empty body is rejected", http.StatusOK, "", nil, "empty or too large"},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			f2 := newFixture(t)
			f2.addMovie(1, relative, "/abc123.jpg")
			f2.transport.status = scenario.status
			f2.transport.contentType = scenario.contentType
			f2.transport.body = scenario.body
			// Three failures exhaust the retry budget and mark the item
			// attempted; a fourth pass finds nothing to do.
			for i := 0; i < 3; i++ {
				worked, err := f2.worker.ProcessOne(context.Background())
				if err == nil || !worked {
					t.Fatalf("pass %d: got (%v, %v), want (true, error)", i+1, worked, err)
				}
				if !strings.Contains(err.Error(), scenario.wantErr) {
					t.Fatalf("pass %d error: %v, want it to contain %q", i+1, err, scenario.wantErr)
				}
				if _, statErr := os.Stat(f2.posterOf(1, relative)); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("pass %d left a poster behind: %v", i+1, statErr)
				}
			}
			if worked, err := f2.worker.ProcessOne(context.Background()); worked || err != nil {
				t.Fatalf("final pass: got (%v, %v), want (false, nil)", worked, err)
			}
		})
	}

	t.Run("oversized response is rejected", func(t *testing.T) {
		f.transport.status = http.StatusOK
		f.transport.body = make([]byte, maxImageBytes+1)
		worked, err := f.worker.ProcessOne(context.Background())
		if err == nil || !worked || !strings.Contains(err.Error(), "empty or too large") {
			t.Fatalf("got (%v, %v), want oversized rejection", worked, err)
		}
		if _, statErr := os.Stat(f.posterOf(1, relative)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("oversized download left a poster behind: %v", statErr)
		}
	})

	t.Run("unsafe poster path is rejected without a request", func(t *testing.T) {
		f2 := newFixture(t)
		f2.addMovie(1, relative, "/../evil.jpg")
		_, err := f2.worker.ProcessOne(context.Background())
		if err == nil || !strings.Contains(err.Error(), "unsafe") {
			t.Fatalf("got %v, want unsafe-path error", err)
		}
		if hits := f2.transport.hitCount(); hits != 0 {
			t.Fatalf("unsafe path still made %d requests", hits)
		}
	})
}

func TestWorkerRetryLaterRewindsTheSameCandidate(t *testing.T) {
	f := newFixture(t)
	relative := "Movies/Test Movie (2020) [tmdb-1]/movie.mkv"
	f.addMovie(1, relative, "/abc123.jpg")
	// A transport failure (as opposed to a provider error) also keeps the
	// candidate queued: the cursor rewinds so the next pass retries it.
	f.transport.err = errors.New("connection reset")
	if worked, err := f.worker.ProcessOne(context.Background()); !worked || err == nil {
		t.Fatalf("got (%v, %v), want (true, error)", worked, err)
	}
	f.transport.err = nil
	f.transport.body = []byte(posterBytes)
	if worked, err := f.worker.ProcessOne(context.Background()); err != nil || !worked {
		t.Fatalf("retry pass: got (%v, %v), want (true, nil)", worked, err)
	}
	if data, err := os.ReadFile(f.posterOf(1, relative)); err != nil || string(data) != posterBytes {
		t.Fatalf("poster after retry: %q (%v)", data, err)
	}
}

func TestWorkerWriteFailureLeavesNoPoster(t *testing.T) {
	f := newFixture(t)
	// The registered file sits in a folder that is missing on disk, so the
	// poster sidecar cannot be created (cross-platform stand-in for a
	// permission failure; chmod-based denial is not reliable on Windows).
	relative := "Movies/Missing Folder/movie.mkv"
	if _, err := f.db.Exec(`INSERT INTO media_items(id, poster_path) VALUES(1, '/abc123.jpg')`); err != nil {
		t.Fatalf("seed media item: %v", err)
	}
	if _, err := f.db.Exec(`INSERT INTO media_files(id, relative_path, movie_id) VALUES(1, ?, 1)`, relative); err != nil {
		t.Fatalf("seed media file: %v", err)
	}
	f.transport.body = []byte(posterBytes)
	worked, err := f.worker.ProcessOne(context.Background())
	if err == nil || !worked || !strings.Contains(err.Error(), "create poster sidecar") {
		t.Fatalf("got (%v, %v), want create-poster-sidecar error", worked, err)
	}
	if _, statErr := os.Stat(f.posterOf(1, relative)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed write left a poster behind: %v", statErr)
	}
}

func TestWorkerPartialZeroBytePosterIsReplaced(t *testing.T) {
	f := newFixture(t)
	relative := "Movies/Test Movie (2020) [tmdb-1]/movie.mkv"
	f.addMovie(1, relative, "/abc123.jpg")
	poster := f.posterOf(1, relative)
	// Simulate a previous crashed write: a zero-byte poster is removed and
	// replaced by a complete download instead of being treated as done.
	if err := os.WriteFile(poster, nil, 0o644); err != nil {
		t.Fatalf("seed partial poster: %v", err)
	}
	f.transport.body = []byte(posterBytes)
	if worked, err := f.worker.ProcessOne(context.Background()); err != nil || !worked {
		t.Fatalf("got (%v, %v), want (true, nil)", worked, err)
	}
	if data, err := os.ReadFile(poster); err != nil || string(data) != posterBytes {
		t.Fatalf("poster after recovery: %q (%v)", data, err)
	}
}

func TestWorkerContextCancellation(t *testing.T) {
	f := newFixture(t)
	relative := "Movies/Test Movie (2020) [tmdb-1]/movie.mkv"
	f.addMovie(1, relative, "/abc123.jpg")
	ctx, cancel := context.WithCancel(context.Background())
	f.transport.err = ctx.Err() // transport honors the request context
	cancel()
	// The client may short-circuit a pre-cancelled context before the
	// transport runs; only the error identity and the missing poster matter.
	_, err := f.worker.ProcessOne(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if _, statErr := os.Stat(f.posterOf(1, relative)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("cancelled run left a poster behind: %v", statErr)
	}

	t.Run("Run exits when the context is cancelled", func(t *testing.T) {
		f2 := newFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			f2.worker.Run(ctx, slog.New(slog.DiscardHandler))
			close(done)
		}()
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Run did not return after cancellation")
		}
	})
}

func TestWorkerRunProcessesWorkThenStopsOnCancel(t *testing.T) {
	f := newFixture(t)
	relative := "Movies/Test Movie (2020) [tmdb-1]/movie.mkv"
	f.addMovie(1, relative, "/abc123.jpg")
	f.transport.body = []byte(posterBytes)
	f.worker.Interval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		f.worker.Run(ctx, slog.New(slog.DiscardHandler))
		close(done)
	}()
	poster := f.posterOf(1, relative)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(poster); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	data, err := os.ReadFile(poster)
	if err != nil || string(data) != posterBytes {
		t.Fatalf("Run never published the poster: %q (%v)", data, err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
}

func TestValidateRoot(t *testing.T) {
	dir := t.TempDir()
	if !ValidateRoot(dir) {
		t.Fatal("existing directory rejected")
	}
	if ValidateRoot(filepath.Join(dir, "missing")) {
		t.Fatal("missing directory accepted")
	}
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if ValidateRoot(file) {
		t.Fatal("regular file accepted")
	}
}
