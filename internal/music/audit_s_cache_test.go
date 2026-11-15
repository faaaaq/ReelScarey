package music

// Independent production-readiness audit (streaming/cache focus): regression
// tests for the defects found and fixed during the audit.
//
//  1. A failed cache encode consumed the registered source on stdin, so the
//     on-the-fly pipe fallback re-encoded from EOF and served silence.
//  2. The in-flight dedup runtime kept one map entry per (file, profile)
//     forever and stranded holds when waiters timed out.
//  3. ClearStreamCache aborted at the first unremovable file while rows were
//     already deleted.
//  4. Nothing pruned orphaned cache rows/files for deleted music_files ids or
//     stale .part staging files left by crashes.
//  5. Hand-edited streaming quality values must fail safe.

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The cache encode reads the registered file on stdin, and both processes
// share the open file description: once a cache attempt got as far as running
// ffmpeg and then failed to publish, the fallback pipe below re-opened the
// same handle already drained to EOF and converted silence. The hermetic fake
// ffmpeg drains stdin in file-output mode too, so this test fails without the
// rewind fix and passes with it.
func TestOpenPlaybackFallbackRewindsSourceAfterFailedCacheEncode(t *testing.T) {
	helperFFmpeg(t)
	s, _, fileID := transcodeFixture(t)

	stream, err := s.OpenPlayback(t.Context(), fileID, "opus", 96, 0)
	if err != nil {
		t.Fatalf("playback after failed cache encode: %v", err)
	}
	defer stream.Close()
	if stream.Body == nil {
		t.Fatal("a failed cache encode must fall back to the on-the-fly pipe")
	}
	data, err := io.ReadAll(stream.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "audio-bytes-for-conversion" {
		t.Fatalf("fallback pipe bytes = %q, want the whole source (an exhausted stdin means the rewind fix is missing)", data)
	}
	var entries int64
	if err := s.store.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM music_stream_cache WHERE file_id=?`, fileID).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if entries != 0 {
		t.Fatalf("a failed encode must not leave a cache entry, %d remain", entries)
	}
}

// Fifty concurrent requests for the same uncached (file, profile): exactly
// one encodes, the rest fail with a bounded busy error instead of queueing
// forever, and the dedup state is fully cleaned up afterwards so the runtime
// maps cannot grow without bound.
func TestStreamCacheBusyWaitersFailFastAndCleanUp(t *testing.T) {
	previous := cacheEncodeGrace
	cacheEncodeGrace = 50 * time.Millisecond
	t.Cleanup(func() { cacheEncodeGrace = previous })

	service, db, _ := cacheTestService(t)
	fileID := registerCacheFile(t, db, "Music/A/Song.flac", "sha")
	enc := &cacheTestEncoder{payload: cacheTestPayload("opus", 128), gate: make(chan struct{})}

	const waiters = 50
	errc := make(chan error, waiters)
	var wg sync.WaitGroup
	for i := 0; i < waiters; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _, err := service.OpenCachedStream(context.Background(), fileID, "opus", 128, "sha", enc.encode)
			errc <- err
		}()
	}
	// The winner blocks inside the encode gate; the other 49 must each hit
	// the bounded busy error without queueing behind it.
	blocked := 0
	sawWinner := 0
	timeout := time.After(10 * time.Second)
	for blocked < waiters-1 {
		select {
		case err := <-errc:
			if err == nil {
				sawWinner++
				continue
			}
			blocked++
		case <-timeout:
			t.Fatalf("only %d of %d waiters failed within the grace window", blocked, waiters-1)
		}
	}
	close(enc.gate)
	wg.Wait()

	busy := 0
	for {
		select {
		case err := <-errc:
			if err == nil {
				sawWinner++
				continue
			}
			if code := cacheErrorCode(t, err); code != "music_cache_busy" {
				t.Fatalf("waiter error = %v, want music_cache_busy", err)
			}
			busy++
		default:
			goto drained
		}
	}
drained:
	if blocked+busy != waiters-1 || sawWinner != 1 {
		t.Fatalf("busy=%d+%d winners=%d, want %d busy waiters and one successful encode", blocked, busy, sawWinner, waiters-1)
	}
	if calls := enc.calls.Load(); calls != 1 {
		t.Fatalf("encode ran %d times, want exactly 1", calls)
	}
	rt := service.streamCache()
	rt.mu.Lock()
	locks, holds := len(rt.locks), len(rt.holds)
	rt.mu.Unlock()
	if locks != 0 || holds != 0 {
		t.Fatalf("dedup state leaked: %d locks, %d holds", locks, holds)
	}

	// The winner's encode published the entry: a fresh request is a hit, and
	// the runtime maps stay empty afterwards.
	file, _, _, err := service.OpenCachedStream(context.Background(), fileID, "opus", 128, "sha", enc.encode)
	if err != nil {
		t.Fatalf("cache hit after the dedup storm: %v", err)
	}
	_ = file.Close()
	rt.mu.Lock()
	locks, holds = len(rt.locks), len(rt.holds)
	rt.mu.Unlock()
	if locks != 0 || holds != 0 {
		t.Fatalf("dedup state leaked after a plain hit: %d locks, %d holds", locks, holds)
	}
}

// ClearStreamCache while an encode is in flight must never deadlock, never
// panic and always converge to a consistent state: the rows go first, the
// files are removed as far as the platform allows, and the in-flight encode
// either publishes a valid entry again or fails cleanly.
func TestClearStreamCacheWhileEncodeInFlight(t *testing.T) {
	service, db, mediaDir := cacheTestService(t)
	fileID := registerCacheFile(t, db, "Music/A/Song.flac", "sha")
	payload := cacheTestPayload("opus", 128)

	released := make(chan struct{})
	partPath := filepath.Join(mediaDir, streamCacheDirName, strconv.FormatInt(fileID, 10), "opus-128.part")
	encode := func(ctx context.Context, dest string) error {
		file, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		if _, err := file.Write(payload); err != nil {
			_ = file.Close()
			return err
		}
		// Hold the staging file open until the clear has raced it.
		select {
		case <-released:
		case <-ctx.Done():
			_ = file.Close()
			return ctx.Err()
		}
		return file.Close()
	}

	done := make(chan error, 1)
	go func() {
		_, _, _, err := service.OpenCachedStream(context.Background(), fileID, "opus", 128, "sha", encode)
		done <- err
	}()

	// Wait until the staging file exists, i.e. the encode is mid-flight.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(partPath); err == nil {
			break
		} else if time.Now().After(deadline) {
			t.Fatal("the encode never reached the staging file")
		}
		time.Sleep(2 * time.Millisecond)
	}

	cleared := make(chan error, 1)
	go func() { cleared <- service.ClearStreamCache(context.Background()) }()
	select {
	case <-cleared:
	case <-time.After(10 * time.Second):
		t.Fatal("ClearStreamCache deadlocked while an encode was in flight")
	}
	close(released)

	var openErr error
	select {
	case openErr = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("OpenCachedStream deadlocked after the clear")
	}

	var rows int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_stream_cache`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if openErr == nil {
		// The encode survived the race and published a valid entry again.
		if rows != 1 {
			t.Fatalf("published entry must be served again, %d rows", rows)
		}
	} else if rows != 0 {
		t.Fatalf("a failed encode must not leave a cache entry, %d remain", rows)
	}
	// No staging garbage may survive either outcome.
	if _, err := os.Stat(partPath); err == nil {
		t.Fatal("the .part staging file must be gone once the encode finished")
	}
	// A later request must still be able to cache the stream.
	if _, _, _, err := service.OpenCachedStream(context.Background(), fileID, "opus", 128, "sha",
		func(_ context.Context, dest string) error { return os.WriteFile(dest, payload, 0o600) }); err != nil {
		t.Fatalf("cache unusable after the racing clear: %v", err)
	}
}

// Orphaned cache state must be reclaimable: rows for deleted music_files ids
// (a converted-over original, a removed file) and .part staging files a crash
// left behind. Live entries and in-flight staging files stay untouched.
func TestPruneStreamCacheRemovesOrphansAndStaleParts(t *testing.T) {
	service, db, mediaDir := cacheTestService(t)
	ctx := context.Background()
	deadID := registerCacheFile(t, db, "Music/A/Dead.flac", "sha-dead")

	// A second registered file reuses the first one's catalog parents so the
	// fixture does not rewrite the shared artist/album/track rows.
	var artistID, albumID, trackID int64
	if err := db.QueryRow(`SELECT artist_id, album_id, track_id FROM music_files WHERE id=?`, deadID).
		Scan(&artistID, &albumID, &trackID); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec(`INSERT INTO music_files(artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,sample_rate,channels,duration_ms,tag_json,imported_at,origin,source_name)
		VALUES(?,?,?,'Music',?,10,'sha-live','flac',0,0,0,1000,'{}',1,'existing','live.flac')`,
		artistID, albumID, trackID, "Music/A/Live.flac")
	if err != nil {
		t.Fatal(err)
	}
	liveID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	enc := &cacheTestEncoder{payload: []byte("payload")}
	if _, _, _, err := service.OpenCachedStream(ctx, deadID, "opus", 128, "sha-dead", enc.encode); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := service.OpenCachedStream(ctx, liveID, "mp3", 192, "sha-live", enc.encode); err != nil {
		t.Fatal(err)
	}
	// The original was replaced and its row deleted: the cache row for the
	// dead id is now unreachable garbage.
	if _, err := db.Exec(`DELETE FROM music_files WHERE id=?`, deadID); err != nil {
		t.Fatal(err)
	}

	liveDir := filepath.Join(mediaDir, streamCacheDirName, strconv.FormatInt(liveID, 10))
	stalePart := filepath.Join(liveDir, "flac-0.part")
	if err := os.WriteFile(stalePart, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stalePart, old, old); err != nil {
		t.Fatal(err)
	}
	freshPart := filepath.Join(liveDir, "opus-64.part")
	if err := os.WriteFile(freshPart, []byte("fresh"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := service.PruneStreamCache(ctx, time.Hour); err != nil {
		t.Fatalf("prune: %v", err)
	}

	var count int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_stream_cache WHERE file_id=?`, deadID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("the cache row for the deleted music_files id must be pruned")
	}
	if _, err := os.Stat(filepath.Join(mediaDir, streamCacheDirName, strconv.FormatInt(deadID, 10), "opus-128")); !os.IsNotExist(err) {
		t.Fatalf("the dead entry's cache file must be removed, stat err=%v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_stream_cache WHERE file_id=?`, liveID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("the live entry must survive the prune, %d rows (%v)", count, err)
	}
	if _, err := os.Stat(stalePart); !os.IsNotExist(err) {
		t.Fatalf("the stale .part must be removed, stat err=%v", err)
	}
	if _, err := os.Stat(freshPart); err != nil {
		t.Fatalf("a recent .part from a live encode must be kept: %v", err)
	}
	// The surviving entry still opens and serves.
	file, _, key, err := service.OpenCachedStream(ctx, liveID, "mp3", 192, "sha-live", nil)
	if err != nil {
		t.Fatalf("live entry unusable after prune: %v", err)
	}
	_ = file.Close()
	if key != "mp3-192" {
		t.Fatalf("key = %q", key)
	}
}

// A tiny cache limit must terminate eviction instead of looping forever, and
// the eviction must never delete the entry that was just inserted — even when
// that entry alone exceeds the limit.
func TestStreamCacheEvictionTerminatesAndKeepsJustInserted(t *testing.T) {
	service, db, _ := cacheTestService(t)
	ctx := context.Background()
	fileID := registerCacheFile(t, db, "Music/A/Song.flac", "sha")
	if _, err := db.Exec(`UPDATE music_preferences SET cache_limit_mb=1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	big := make([]byte, 2*1024*1024) // one entry alone busts the 1 MiB limit

	enc := &cacheTestEncoder{payload: big}
	if _, _, _, err := service.OpenCachedStream(ctx, fileID, "opus", 128, "sha", enc.encode); err != nil {
		t.Fatalf("first encode: %v", err)
	}
	// A second entry must evict the first, and the eviction loop must stop
	// even though the just-inserted entry alone still exceeds the limit.
	if _, _, _, err := service.OpenCachedStream(ctx, fileID, "mp3", 192, "sha", enc.encode); err != nil {
		t.Fatalf("second encode: %v", err)
	}
	stats, err := service.StreamCacheStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Entries != 1 {
		t.Fatalf("entries = %d, want exactly the just-inserted entry", stats.Entries)
	}
	var key string
	if err := db.QueryRow(`SELECT profile_key FROM music_stream_cache`).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if key != "mp3-192" {
		t.Fatalf("surviving key = %q, want the just-inserted mp3-192", key)
	}
	// A third insert terminates promptly as well (no livelock; the test
	// timeout is the bound).
	if _, _, _, err := service.OpenCachedStream(ctx, fileID, "flac", 0, "sha", enc.encode); err != nil {
		t.Fatalf("third encode: %v", err)
	}
	stats, err = service.StreamCacheStats(ctx)
	if err != nil || stats.Entries != 1 {
		t.Fatalf("stats after third encode: %+v (%v)", stats, err)
	}
}

// A hand-edited or otherwise invalid stored streaming quality must fail safe:
// the stream keeps working with the built-in default conversion target
// instead of erroring or producing an unknown codec.
func TestStreamingQualityParamsInvalidValuesFailSafe(t *testing.T) {
	for _, invalid := range []string{"", "   ", "garbage", "192", "DROP TABLE music", "aac; drop"} {
		format, rate := StreamingQualityParams(invalid)
		if format != "opus" || rate != 96 {
			t.Fatalf("quality %q resolved to (%q, %d), want the safe opus/96 default", invalid, format, rate)
		}
		// The resolved name also flows through ResolveStreamQuality when a
		// hand-edited per-user row carries it.
		resolved := ResolveStreamQuality(invalid, "")
		if resolved != strings.TrimSpace(invalid) && strings.TrimSpace(invalid) != "" {
			t.Fatalf("ResolveStreamQuality must pass a non-empty stored value through, got %q", resolved)
		}
		if _, rate := StreamingQualityParams(resolved); rate != 96 && format != "opus" {
			t.Fatalf("resolved %q did not fail safe", resolved)
		}
	}
	// Empty per-user values still follow the global preference, and an empty
	// global falls back to the built-in default.
	if got := ResolveStreamQuality("", "high"); got != "high" {
		t.Fatalf("empty per-user must follow global, got %q", got)
	}
	if got := ResolveStreamQuality("", ""); got != DefaultStreamingQuality {
		t.Fatalf("empty everywhere must fall back to the default, got %q", got)
	}
}
