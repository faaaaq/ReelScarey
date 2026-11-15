package music

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
)

// cacheTestService opens a disposable database plus media root, mirroring the
// store test setup: database.Open(ctx, t.TempDir()) then NewService.
func cacheTestService(t *testing.T) (*Service, *sql.DB, string) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	mediaDir := t.TempDir()
	return NewService(db, mediaDir), db, mediaDir
}

// registerCacheFile saves one artist/album/track and registers a music file
// with the given source hash, mirroring the stream test fixture.
func registerCacheFile(t *testing.T, db *sql.DB, relative, sha string) int64 {
	t.Helper()
	ctx := context.Background()
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
	result, err := db.Exec(`INSERT INTO music_files(artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,sample_rate,channels,duration_ms,tag_json,imported_at,origin,source_name)
		VALUES(?,?,?,'Music',?,10,?,'flac',0,0,0,1000,'{}',1,'existing','song.flac')`,
		saved.ID, albums[0].ID, tracks[0].ID, relative, sha)
	if err != nil {
		t.Fatal(err)
	}
	fileID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return fileID
}

// cacheTestEncoder is a fake encode func that writes deterministic bytes and
// records how often it ran.
type cacheTestEncoder struct {
	calls   atomic.Int64
	payload []byte
	fail    bool
	started chan struct{} // optional: one token per started encode
	gate    chan struct{} // optional: encodes block until this closes
}

func (e *cacheTestEncoder) encode(ctx context.Context, dest string) error {
	e.calls.Add(1)
	if e.started != nil {
		e.started <- struct{}{}
	}
	if e.gate != nil {
		select {
		case <-e.gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if e.fail {
		return errors.New("synthetic encode failure")
	}
	return os.WriteFile(dest, e.payload, 0o644)
}

func cacheTestPayload(codec string, bitrate int) []byte {
	return []byte(fmt.Sprintf("encoded:%s-%d", codec, bitrate))
}

func cacheErrorCode(t *testing.T, err error) string {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("expected typed *Error, got %v", err)
	}
	return e.Code
}

func cacheRowCount(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var count int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_stream_cache`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func cacheDirNames(t *testing.T, mediaDir string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(mediaDir, streamCacheDirName))
	if os.IsNotExist(err) {
		return map[string]bool{}
	}
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name()] = true
	}
	return names
}

func TestOpenCachedStreamEncodesOnceThenServesHits(t *testing.T) {
	ctx := context.Background()
	service, db, _ := cacheTestService(t)
	fileID := registerCacheFile(t, db, "Music/Artist/Album/01 Song.flac", "sha-a")
	enc := &cacheTestEncoder{payload: cacheTestPayload("opus", 128)}

	file, info, key, err := service.OpenCachedStream(ctx, fileID, "opus", 128, "sha-a", enc.encode)
	if err != nil {
		t.Fatal(err)
	}
	if key != "opus-128" {
		t.Fatal("profile key", key)
	}
	body, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if string(body) != string(enc.payload) {
		t.Fatal("encoded body", string(body))
	}
	if info.Size() != int64(len(enc.payload)) {
		t.Fatal("cached size", info.Size())
	}
	stats, err := service.StreamCacheStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Entries != 1 || stats.SizeBytes != int64(len(enc.payload)) || stats.Hits != 0 || stats.Misses != 1 {
		t.Fatal("stats after encode", stats)
	}

	// A second open for the same key must serve the cache without encoding.
	again, infoAgain, _, err := service.OpenCachedStream(ctx, fileID, "opus", 128, "sha-a", enc.encode)
	if err != nil {
		t.Fatal(err)
	}
	body, err = io.ReadAll(again)
	if err != nil {
		t.Fatal(err)
	}
	if err := again.Close(); err != nil {
		t.Fatal(err)
	}
	if string(body) != string(enc.payload) {
		t.Fatal("cached body", string(body))
	}
	if infoAgain.Size() != info.Size() {
		t.Fatal("cached size changed", infoAgain.Size())
	}
	if enc.calls.Load() != 1 {
		t.Fatal("encode ran again on a cache hit", enc.calls.Load())
	}
	stats, err = service.StreamCacheStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Hits != 1 || stats.Misses != 1 {
		t.Fatal("hit/miss counters", stats)
	}

	// The cache outlives the Service: a reopened Service serves the same
	// entry without encoding, while the in-memory counters restart at zero.
	reopened := NewService(db, service.mediaDir)
	reopenedFile, _, _, err := reopened.OpenCachedStream(ctx, fileID, "opus", 128, "sha-a", enc.encode)
	if err != nil {
		t.Fatal(err)
	}
	body, err = io.ReadAll(reopenedFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopenedFile.Close(); err != nil {
		t.Fatal(err)
	}
	if string(body) != string(enc.payload) {
		t.Fatal("reopened body", string(body))
	}
	if enc.calls.Load() != 1 {
		t.Fatal("encode ran again after reopen", enc.calls.Load())
	}
	reopenedStats, err := reopened.StreamCacheStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The counters restarted at zero and this single reopened hit counted once.
	if reopenedStats.Entries != 1 || reopenedStats.Hits != 1 || reopenedStats.Misses != 0 {
		t.Fatal("reopened counters must restart at zero", reopenedStats)
	}
}

func TestOpenCachedStreamWithoutEncodeReturnsCacheMiss(t *testing.T) {
	ctx := context.Background()
	service, db, _ := cacheTestService(t)
	fileID := registerCacheFile(t, db, "Music/Artist/Album/01 Song.flac", "sha-a")

	_, _, _, err := service.OpenCachedStream(ctx, fileID, "mp3", 192, "sha-a", nil)
	if cacheErrorCode(t, err) != "music_cache_miss" {
		t.Fatal("expected music_cache_miss", err)
	}
	if !errors.Is(err, ErrCacheMiss) {
		t.Fatal("miss error must be checkable", err)
	}
	stats, err := service.StreamCacheStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Misses != 1 || stats.Hits != 0 {
		t.Fatal("miss not counted", stats)
	}

	// With an entry cached, the same call is a plain hit.
	enc := &cacheTestEncoder{payload: cacheTestPayload("mp3", 192)}
	file, _, key, err := service.OpenCachedStream(ctx, fileID, "mp3", 192, "sha-a", enc.encode)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if key != "mp3-192" {
		t.Fatal("profile key", key)
	}
	_, _, _, err = service.OpenCachedStream(ctx, fileID, "mp3", 192, "sha-a", nil)
	if err != nil {
		t.Fatal("cached entry served without encode func", err)
	}
	if enc.calls.Load() != 1 {
		t.Fatal("encode ran unexpectedly", enc.calls.Load())
	}
}

func TestOpenCachedStreamInvalidatesWhenSourceChanges(t *testing.T) {
	ctx := context.Background()
	service, db, mediaDir := cacheTestService(t)
	fileID := registerCacheFile(t, db, "Music/Artist/Album/01 Song.flac", "sha-a")
	first := &cacheTestEncoder{payload: []byte("first-encode")}
	second := &cacheTestEncoder{payload: []byte("second-encode")}

	file, _, _, err := service.OpenCachedStream(ctx, fileID, "opus", 128, "sha-a", first.encode)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	// A different source hash invalidates the entry and re-encodes.
	file, info, key, err := service.OpenCachedStream(ctx, fileID, "opus", 128, "sha-b", second.encode)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if key != "opus-128" || string(info.Name()) == "" {
		t.Fatal("re-encoded stream", key, info.Name())
	}
	body, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "second-encode" {
		t.Fatal("stale cached bytes served", string(body))
	}
	if first.calls.Load() != 1 || second.calls.Load() != 1 {
		t.Fatal("encode calls", first.calls.Load(), second.calls.Load())
	}
	var stored string
	if err := db.QueryRow(`SELECT source_sha256 FROM music_stream_cache WHERE file_id=? AND profile_key='opus-128'`, fileID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "sha-b" {
		t.Fatal("row kept the old source hash", stored)
	}
	stats, err := service.StreamCacheStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Entries != 1 || stats.SizeBytes != int64(len("second-encode")) || stats.Misses != 2 {
		t.Fatal("stats after invalidation", stats)
	}
	// The old cache file must be gone; only the replacement remains.
	entries, err := os.ReadDir(filepath.Join(mediaDir, streamCacheDirName, fmt.Sprint(fileID)))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatal("invalidated cache file survived", entries)
	}
}

func TestOpenCachedStreamDedupsConcurrentEncodes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	service, db, _ := cacheTestService(t)
	fileID := registerCacheFile(t, db, "Music/Artist/Album/01 Song.flac", "sha-a")
	payload := cacheTestPayload("opus", 128)
	enc := &cacheTestEncoder{payload: payload, started: make(chan struct{}, 3), gate: make(chan struct{})}

	const callers = 3
	results := make(chan struct {
		file *os.File
		info os.FileInfo
		key  string
		err  error
	}, callers)
	var wg sync.WaitGroup
	// The first caller starts the encode and holds the in-flight lock while
	// it runs; the others must wait for that same encode instead of encoding
	// again.
	wg.Add(1)
	go func() {
		defer wg.Done()
		file, info, key, err := service.OpenCachedStream(ctx, fileID, "opus", 128, "sha-a", enc.encode)
		results <- struct {
			file *os.File
			info os.FileInfo
			key  string
			err  error
		}{file, info, key, err}
	}()
	<-enc.started // the encode is running and the lock is held
	for i := 1; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			file, info, key, err := service.OpenCachedStream(ctx, fileID, "opus", 128, "sha-a", enc.encode)
			results <- struct {
				file *os.File
				info os.FileInfo
				key  string
				err  error
			}{file, info, key, err}
		}()
	}
	// Give the waiters time to reach the dedup lock before the encode lands.
	time.Sleep(100 * time.Millisecond)
	close(enc.gate)
	wg.Wait()
	close(results)

	hits := 0
	for result := range results {
		if result.err != nil {
			t.Fatal("concurrent caller failed", result.err)
		}
		if result.key != "opus-128" {
			t.Fatal("profile key", result.key)
		}
		body, err := io.ReadAll(result.file)
		if err != nil {
			t.Fatal(err)
		}
		if err := result.file.Close(); err != nil {
			t.Fatal(err)
		}
		if string(body) != string(payload) {
			t.Fatal("body", string(body))
		}
		if result.info.Size() != int64(len(payload)) {
			t.Fatal("size", result.info.Size())
		}
		hits++
	}
	if hits != callers {
		t.Fatal("callers", hits)
	}
	if enc.calls.Load() != 1 {
		t.Fatal("concurrent callers duplicated the encode", enc.calls.Load())
	}
	stats, err := service.StreamCacheStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Hits != callers-1 || stats.Misses != 1 {
		t.Fatal("hit/miss counters", stats)
	}
}

func TestStreamCacheEvictsLeastRecentlyUsed(t *testing.T) {
	ctx := context.Background()
	service, db, mediaDir := cacheTestService(t)
	fileID := registerCacheFile(t, db, "Music/Artist/Album/01 Song.flac", "sha-a")
	if _, err := db.Exec(`UPDATE music_preferences SET cache_limit_mb=1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}

	// Three 600KB entries push the cache past its 1MB limit; the oldest
	// entries are evicted until the total fits.
	payload := make([]byte, 600*1024)
	profiles := []struct {
		codec   string
		bitrate int
	}{
		{"opus", 128},
		{"mp3", 192},
		{"aac", 96},
	}
	for _, profile := range profiles {
		enc := &cacheTestEncoder{payload: payload}
		file, _, key, err := service.OpenCachedStream(ctx, fileID, profile.codec, profile.bitrate, "sha-a", enc.encode)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if key != fmt.Sprintf("%s-%d", profile.codec, profile.bitrate) {
			t.Fatal("profile key", key)
		}
	}

	var count int64
	var total int64
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(size_bytes),0) FROM music_stream_cache`).Scan(&count, &total); err != nil {
		t.Fatal(err)
	}
	if count != 1 || total != int64(len(payload)) {
		t.Fatal("eviction did not shrink the cache to the newest entry", count, total)
	}
	entries, err := os.ReadDir(filepath.Join(mediaDir, streamCacheDirName, fmt.Sprint(fileID)))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "aac-96" {
		t.Fatal("evicted cache files survived", entries)
	}
	stats, err := service.StreamCacheStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.LimitBytes != 1024*1024 {
		t.Fatal("limit", stats.LimitBytes)
	}
	if stats.SizeBytes > stats.LimitBytes {
		t.Fatal("cache stayed over the limit", stats)
	}
}

func TestClearStreamCacheRemovesCacheFilesOnly(t *testing.T) {
	ctx := context.Background()
	service, db, mediaDir := cacheTestService(t)
	fileID := registerCacheFile(t, db, "Music/Artist/Album/01 Song.flac", "sha-a")
	enc := &cacheTestEncoder{payload: cacheTestPayload("flac", 0)}
	file, _, _, err := service.OpenCachedStream(ctx, fileID, "flac", 0, "sha-a", enc.encode)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	// A real music file lives in the media root, outside the cache dir.
	musicPath := filepath.Join(mediaDir, filepath.FromSlash("Music/Artist/Album/01 Song.flac"))
	if err := os.MkdirAll(filepath.Dir(musicPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(musicPath, []byte("original-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := service.ClearStreamCache(ctx); err != nil {
		t.Fatal(err)
	}
	if count := cacheRowCount(t, db); count != 0 {
		t.Fatal("cache rows survived the clear", count)
	}
	if names := cacheDirNames(t, mediaDir); len(names) != 0 {
		t.Fatal("cache files survived the clear", names)
	}
	body, err := os.ReadFile(musicPath)
	if err != nil || string(body) != "original-bytes" {
		t.Fatal("registered music file was touched", string(body), err)
	}
	stats, err := service.StreamCacheStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Entries != 0 || stats.SizeBytes != 0 {
		t.Fatal("stats after clear", stats)
	}
}

func TestOpenCachedStreamEncodeFailureCleansUp(t *testing.T) {
	ctx := context.Background()
	service, db, mediaDir := cacheTestService(t)
	fileID := registerCacheFile(t, db, "Music/Artist/Album/01 Song.flac", "sha-a")
	enc := &cacheTestEncoder{payload: cacheTestPayload("opus", 128), fail: true}

	_, _, _, err := service.OpenCachedStream(ctx, fileID, "opus", 128, "sha-a", enc.encode)
	if cacheErrorCode(t, err) != "music_cache_encode_failed" {
		t.Fatal("expected music_cache_encode_failed", err)
	}
	if !errors.Is(err, ErrCacheEncodeFailed) {
		t.Fatal("encode failure must be checkable", err)
	}
	if count := cacheRowCount(t, db); count != 0 {
		t.Fatal("failed encode inserted a row", count)
	}
	if names := cacheDirNames(t, mediaDir); len(names) != 0 {
		t.Fatal("partial files survived the failed encode", names)
	}
	stats, err := service.StreamCacheStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Entries != 0 || stats.Misses != 1 {
		t.Fatal("stats after failure", stats)
	}

	// A retry after the failure can still succeed.
	enc.fail = false
	file, _, key, err := service.OpenCachedStream(ctx, fileID, "opus", 128, "sha-a", enc.encode)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if key != "opus-128" {
		t.Fatal("profile key", key)
	}
}

func TestOpenCachedStreamFailsClosedOnTamperedCachePath(t *testing.T) {
	ctx := context.Background()
	service, db, mediaDir := cacheTestService(t)
	fileID := registerCacheFile(t, db, "Music/Artist/Album/01 Song.flac", "sha-a")
	enc := &cacheTestEncoder{payload: cacheTestPayload("opus", 128)}
	file, _, _, err := service.OpenCachedStream(ctx, fileID, "opus", 128, "sha-a", enc.encode)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	// A file outside the cache dir that a tampered row would point at.
	escaped := filepath.Join(mediaDir, "escaped.bin")
	if err := os.WriteFile(escaped, []byte("do-not-serve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE music_stream_cache SET relative_path='../escaped.bin' WHERE file_id=?`, fileID); err != nil {
		t.Fatal(err)
	}

	// Without an encode func the tampered entry must fail closed as a miss.
	_, _, _, err = service.OpenCachedStream(ctx, fileID, "opus", 128, "sha-a", nil)
	if err == nil {
		t.Fatal("tampered cache path was served")
	}
	if cacheErrorCode(t, err) != "music_cache_miss" {
		t.Fatal("expected a cache miss for the tampered row", err)
	}
	body, err := os.ReadFile(escaped)
	if err != nil || string(body) != "do-not-serve" {
		t.Fatal("file outside the cache dir was touched", string(body), err)
	}
	// The tampered row is forgotten so the next open re-encodes cleanly.
	if count := cacheRowCount(t, db); count != 0 {
		t.Fatal("tampered row survived", count)
	}

	recovered := &cacheTestEncoder{payload: []byte("re-encode")}
	serve, _, _, err := service.OpenCachedStream(ctx, fileID, "opus", 128, "sha-a", recovered.encode)
	if err != nil {
		t.Fatal(err)
	}
	body, err = io.ReadAll(serve)
	if err != nil {
		t.Fatal(err)
	}
	if err := serve.Close(); err != nil {
		t.Fatal(err)
	}
	if string(body) != "re-encode" {
		t.Fatal("recovered body", string(body))
	}
	if body, err := os.ReadFile(escaped); err != nil || string(body) != "do-not-serve" {
		t.Fatal("re-encode escaped the cache dir", string(body), err)
	}
}

func TestOpenCachedStreamRejectsInvalidKeys(t *testing.T) {
	ctx := context.Background()
	service, db, _ := cacheTestService(t)
	fileID := registerCacheFile(t, db, "Music/Artist/Album/01 Song.flac", "sha-a")
	enc := &cacheTestEncoder{payload: []byte("never-written")}
	tests := []struct {
		codec   string
		bitrate int
	}{
		{"", 128},
		{"op/us", 128},
		{"opus\\", 128},
		{"..", 128},
		{"opus", -1},
	}
	for _, test := range tests {
		_, _, _, err := service.OpenCachedStream(ctx, fileID, test.codec, test.bitrate, "sha-a", enc.encode)
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("codec %q bitrate %d accepted: %v", test.codec, test.bitrate, err)
		}
	}
	if enc.calls.Load() != 0 {
		t.Fatal("encode ran for an invalid key", enc.calls.Load())
	}
	if count := cacheRowCount(t, db); count != 0 {
		t.Fatal("invalid key inserted a row", count)
	}
}
