package music

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

// streamCacheDirName is the dotted directory inside the media root that holds
// transcoded stream cache files. Both scan walkers (countScanEligible,
// walkScan) already skip dotted directories, so cache files never appear as
// catalog entries and never masquerade as music_files content.
const streamCacheDirName = ".reelscarey-stream-cache"

// defaultStreamCacheLimitMB matches the migration default for cache_limit_mb
// and is used when the preference row is missing, unreadable or zero.
const defaultStreamCacheLimitMB = 512

// cacheEncodeGrace is how long a stream request that finds another caller
// already encoding the same (file, profile) may wait for that encode to land
// before failing with a busy error. Encodes run longer than streams, so this
// is far longer than transcodeGracePeriod; it is a separate variable and
// transcodeGracePeriod is never mutated for cache waits. Tests in this
// package shorten it so busy paths finish quickly.
var cacheEncodeGrace = 2 * time.Minute

// maxCacheBitrate bounds the bitrate component of a profile key. The key is
// part of the cache file name, so it must stay a bounded, path-safe value.
const maxCacheBitrate = 4096

// ErrCacheMiss is returned when no cached entry exists for the requested key
// and the caller did not provide an encode func, so the HTTP layer can fall
// back to on-the-fly piping. As a *Error sentinel it is both errors.Is-checkable
// and carries the machine-readable code for the HTTP boundary.
var ErrCacheMiss = &Error{Code: "music_cache_miss", Message: "This stream is not cached yet."}

// ErrCacheEncodeFailed is returned when a cache encode fails; the underlying
// cause stays wrapped for logs.
var ErrCacheEncodeFailed = &Error{Code: "music_cache_encode_failed", Message: "The server could not prepare this cached stream."}

// ErrCacheBusy is the sentinel acquire returns when the per-key grace period
// expires. As a package-level sentinel the caller can distinguish "another
// encode is still running" from a canceled context and make one final cache
// check before giving up.
var ErrCacheBusy = &Error{Code: "music_cache_busy", Message: "The server is already preparing this stream. Wait a moment and try again."}

// ErrCacheRecentlyFailed is returned to a caller whose (file, profile) encode
// failed moments ago. It is a short-lived failure memo, not a cached failure:
// after cacheFailureMemoTTL the next caller re-attempts the encode, and a
// successful encode anywhere clears the memo immediately. OpenPlayback treats
// it like every other cache error and falls back to on-the-fly piping, so a
// permanently failing source costs concurrent listeners one bounded pipe
// attempt each instead of a sequential encode attempt per waiter.
var ErrCacheRecentlyFailed = &Error{Code: "music_cache_recent_failure", Message: "The server could not prepare this stream just now. Wait a moment and try again."}

// cacheFailureMemoTTL is how long a failed encode for a key deters
// immediately-retried encodes for the same key. It must stay short: the memo
// only stops N concurrent waiters from re-running a just-failed ffmpeg one
// after another, never a genuine retry minutes later.
var cacheFailureMemoTTL = 5 * time.Second

// maxEncodeFailureMemos bounds the failure memo map. The memo keys are the
// same bounded profile keys as the dedup locks, and entries also expire by
// TTL, so the cap is a belt-and-braces bound against a pathological burst of
// distinct failing keys.
const maxEncodeFailureMemos = 1024

// StreamCacheStats is a snapshot of the persistent transcoding stream cache.
// Hits/Misses are in-memory atomic counters for the process lifetime; they
// reset on restart and are best-effort observability, never a source of
// truth.
type StreamCacheStats struct {
	Entries    int64
	SizeBytes  int64
	LimitBytes int64
	Hits       int64
	Misses     int64
}

// streamCacheRuntime holds the per-Service in-process cache state: the
// in-flight dedup locks and the hit/miss counters. It lives in a registry
// keyed by the Service pointer because the Service struct is shared with
// other feature owners; entries are created lazily so hand-built test
// Services keep working, and a fresh Service always starts at zero.
type streamCacheRuntime struct {
	mu     sync.Mutex
	locks  map[string]chan struct{}
	holds  map[string]int
	hits   atomic.Int64
	misses atomic.Int64

	// failMu guards the short-lived per-key encode-failure memo. Entries
	// expire after cacheFailureMemoTTL and disappear when a later encode
	// for the same key succeeds, so failures are never cached indefinitely.
	failMu      sync.Mutex
	recentFails map[string]time.Time
}

var streamCacheRuntimes sync.Map // *Service -> *streamCacheRuntime

func (s *Service) streamCache() *streamCacheRuntime {
	if rt, ok := streamCacheRuntimes.Load(s); ok {
		return rt.(*streamCacheRuntime)
	}
	rt := &streamCacheRuntime{locks: map[string]chan struct{}{}, holds: map[string]int{}}
	actual, _ := streamCacheRuntimes.LoadOrStore(s, rt)
	return actual.(*streamCacheRuntime)
}

// cacheRow is one music_stream_cache row needed for the cache decisions.
type cacheRow struct {
	ID           int64
	RelativePath string
	SizeBytes    int64
	SourceSHA    string
}

// cacheProfileKey derives the profile_key used in the database and the cache
// file name, e.g. "opus-128", "flac-0", "mp3-192". It fails closed on values
// that could escape the cache dir or smuggle control characters into a file
// name.
func cacheProfileKey(codec string, bitrate int) (string, bool) {
	if bitrate < 0 || bitrate > maxCacheBitrate {
		return "", false
	}
	codec = strings.TrimSpace(codec)
	if codec == "" {
		return "", false
	}
	key := codec + "-" + strconv.Itoa(bitrate)
	if len(key) > 64 {
		return "", false
	}
	if key == "." || strings.HasPrefix(key, "..") || strings.Contains(key, "..") ||
		strings.ContainsAny(key, `/\`) ||
		strings.ContainsFunc(key, unicode.IsControl) {
		return "", false
	}
	return key, true
}

// cacheRelativePath is the deterministic cache path for a key: one directory
// per file id, one file per profile. ClearStreamCache then only has to unlink
// one level of directories.
func cacheRelativePath(fileID int64, profileKey string) string {
	return strconv.FormatInt(fileID, 10) + "/" + profileKey
}

// cleanCacheRelative slash-normalizes and validates a stored relative cache
// path the same way OpenAudioFile validates catalog paths, rejecting
// escapes and absolute paths before anything is opened.
func cleanCacheRelative(value string) (string, bool) {
	name := path.Clean(strings.ReplaceAll(strings.TrimSpace(value), "\\", "/"))
	if name == "." || name == ".." || strings.HasPrefix(name, "../") || strings.HasPrefix(name, "/") {
		return "", false
	}
	return name, true
}

// cachePathAllowed fails closed unless relative is exactly the deterministic
// path for the key. A tampered row can therefore never point the open, the
// rename or a delete at any other file, inside or outside the cache dir.
func cachePathAllowed(fileID int64, profileKey, relative string) bool {
	name, ok := cleanCacheRelative(relative)
	return ok && name == cacheRelativePath(fileID, profileKey)
}

func streamCacheDir(mediaDir string) string {
	return filepath.Join(mediaDir, streamCacheDirName)
}

// cacheRow reads the row for a key. ok=false when the key is not cached.
func (s *Service) cacheRow(ctx context.Context, fileID int64, profileKey string) (cacheRow, bool, error) {
	var row cacheRow
	err := s.store.db.QueryRowContext(ctx, `SELECT id, relative_path, size_bytes, source_sha256
		FROM music_stream_cache WHERE file_id=? AND profile_key=?`, fileID, profileKey).
		Scan(&row.ID, &row.RelativePath, &row.SizeBytes, &row.SourceSHA)
	if errors.Is(err, sql.ErrNoRows) {
		return cacheRow{}, false, nil
	}
	if err != nil {
		return cacheRow{}, false, err
	}
	return row, true, nil
}

// openCacheFile opens a cached file for streaming. The stored path must match
// the deterministic name for its key exactly (cachePathAllowed, the same fence
// deleteCacheEntry applies) and is opened through os.Root around the cache
// dir, mirroring OpenAudioFile's containment, so a stale or tampered path can
// never escape that root — and a tampered row can never make one key's open
// serve another key's cached bytes.
func (s *Service) openCacheFile(fileID int64, profileKey, relative string) (*os.File, os.FileInfo, error) {
	if s.mediaDir == "" {
		return nil, nil, &Error{Code: "music_file_missing", Message: "This registered music file is not available on the server."}
	}
	if !cachePathAllowed(fileID, profileKey, relative) {
		return nil, nil, ErrInvalidInput
	}
	name, _ := cleanCacheRelative(relative)
	root, err := os.OpenRoot(streamCacheDir(s.mediaDir))
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	file, err := root.Open(name)
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, os.ErrNotExist
	}
	return file, info, nil
}

// deleteCacheEntry removes one row and its cache file. Both steps are
// best-effort so a stale row can always be forgotten; the file is only
// touched when its path matches the deterministic name for the row's key.
func (s *Service) deleteCacheEntry(ctx context.Context, fileID int64, profileKey, relative string) error {
	if _, err := s.store.db.ExecContext(ctx, `DELETE FROM music_stream_cache WHERE file_id=? AND profile_key=?`, fileID, profileKey); err != nil {
		return err
	}
	if s.mediaDir == "" {
		return nil
	}
	if !cachePathAllowed(fileID, profileKey, relative) {
		return nil
	}
	root, err := os.OpenRoot(streamCacheDir(s.mediaDir))
	if err != nil {
		return nil
	}
	defer root.Close()
	name, _ := cleanCacheRelative(relative)
	_ = root.Remove(name)
	// Best-effort removal of the now possibly empty file directory.
	_ = root.Remove(strconv.FormatInt(fileID, 10))
	return nil
}

// touchCache refreshes last_used_at for a hit. Best-effort: a failed update
// only risks the entry being evicted early.
func (s *Service) touchCache(ctx context.Context, id int64) {
	_, _ = s.store.db.ExecContext(ctx, `UPDATE music_stream_cache SET last_used_at=unixepoch() WHERE id=?`, id)
}

// acquire takes the per-key in-flight dedup slot with a bounded, ctx-aware
// wait (cacheEncodeGrace). A caller whose encode outlives the grace
// period lets the waiter fail with a busy error instead of queueing
// unbounded work, mirroring acquireTranscodeSlot.
//
// On success the returned release func must be called exactly once; it hands
// the slot to the next waiter and drops this caller's hold. A caller that
// loses the wait gets a nil release — acquire has already dropped its hold —
// so the per-key state disappears as soon as the last interested caller
// leaves instead of accumulating one map entry per (file, profile) forever.
func (rt *streamCacheRuntime) acquire(ctx context.Context, key string) (func(), error) {
	rt.mu.Lock()
	rt.holds[key]++
	ch, ok := rt.locks[key]
	if !ok {
		ch = make(chan struct{}, 1)
		rt.locks[key] = ch
	}
	rt.mu.Unlock()
	select {
	case ch <- struct{}{}:
		return func() {
			select {
			case <-ch:
			default:
			}
			rt.dropHold(key)
		}, nil
	case <-ctx.Done():
		rt.dropHold(key)
		return nil, ctx.Err()
	case <-time.After(cacheEncodeGrace):
		rt.dropHold(key)
		return nil, ErrCacheBusy
	}
}

// dropHold removes one hold on key and deletes the per-key state when the
// last hold leaves. Holds are counted under the runtime mutex, so a new
// caller either observes the entry before the last hold drops (and keeps it
// alive) or creates a fresh one afterwards; no waiter is ever stranded on a
// deleted channel.
func (rt *streamCacheRuntime) dropHold(key string) {
	rt.mu.Lock()
	remaining := rt.holds[key] - 1
	if remaining <= 0 {
		delete(rt.holds, key)
		delete(rt.locks, key)
	} else {
		rt.holds[key] = remaining
	}
	rt.mu.Unlock()
}

func (rt *streamCacheRuntime) release(ch chan struct{}) {
	select {
	case <-ch:
	default:
	}
}

// noteEncodeFailure records a just-failed encode for key at now, pruning
// expired entries first and shedding arbitrary entries at the cap so the
// memo map can never grow without bound.
func (rt *streamCacheRuntime) noteEncodeFailure(key string, now time.Time) {
	rt.failMu.Lock()
	defer rt.failMu.Unlock()
	if rt.recentFails == nil {
		rt.recentFails = make(map[string]time.Time)
	}
	rt.pruneEncodeFailuresLocked(now)
	if len(rt.recentFails) >= maxEncodeFailureMemos {
		for k := range rt.recentFails {
			delete(rt.recentFails, k)
			break
		}
	}
	rt.recentFails[key] = now
}

// clearEncodeFailure forgets the memo for key; called when a later encode
// for the same key succeeds so recovery is immediate, not TTL-bound.
func (rt *streamCacheRuntime) clearEncodeFailure(key string) {
	rt.failMu.Lock()
	delete(rt.recentFails, key)
	rt.failMu.Unlock()
}

// recentEncodeFailure reports whether key failed within cacheFailureMemoTTL.
// Expired entries are dropped on read.
func (rt *streamCacheRuntime) recentEncodeFailure(key string, now time.Time) bool {
	rt.failMu.Lock()
	defer rt.failMu.Unlock()
	at, ok := rt.recentFails[key]
	if !ok {
		return false
	}
	if now.Sub(at) >= cacheFailureMemoTTL {
		delete(rt.recentFails, key)
		return false
	}
	return true
}

func (rt *streamCacheRuntime) pruneEncodeFailuresLocked(now time.Time) {
	for k, at := range rt.recentFails {
		if now.Sub(at) >= cacheFailureMemoTTL {
			delete(rt.recentFails, k)
		}
	}
}

// encodeFailureMemoizable reports whether a failed cache encode should feed
// the short-lived failure memo. Context cancellation (a listener going away
// or a request timing out) and a saturated transcode slot are transient by
// definition: the next waiter must transparently re-encode with its own
// context instead of being bounced to the pipe for the memo's lifetime.
// Everything else — a real ffmpeg failure, an unwritable cache dir — is what
// the memo exists for.
func encodeFailureMemoizable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var musicErr *Error
	if errors.As(err, &musicErr) && musicErr.Code == "music_transcode_busy" {
		return false
	}
	return true
}

// OpenCachedStream returns the cached transcoded stream for
// (fileID, codec, bitrate) or creates it. The returned *os.File is the opened
// cache file the HTTP layer serves with http.ServeContent; the string is the
// profile_key actually used, e.g. "opus-128". The caller must close the file.
//
// The encode func writes the encoded bytes to the given .part staging
// destination inside the cache dir; OpenCachedStream publishes it with an
// fsync-rename, INSERTs the cache row and opens the final file. When encode
// is nil and nothing is cached yet, a music_cache_miss error is returned so
// callers fall back to on-the-fly piping.
//
// The cache dir is <mediaDir>/.reelscarey-stream-cache, created lazily via
// os.MkdirAll. Every file access goes through os.Root on that dir, mirroring
// OpenAudioFile's containment, so a tampered relative_path can never escape.
func (s *Service) OpenCachedStream(ctx context.Context, fileID int64, codec string, bitrate int, sourceSHA string, encode func(ctx context.Context, dest string) error) (*os.File, os.FileInfo, string, error) {
	profileKey, valid := cacheProfileKey(codec, bitrate)
	if fileID < 1 || !valid {
		return nil, nil, "", ErrInvalidInput
	}
	expected := cacheRelativePath(fileID, profileKey)

	// Cached hit: verify the source identity, then open the file.
	row, cached, err := s.cacheRow(ctx, fileID, profileKey)
	if err != nil {
		return nil, nil, "", err
	}
	if cached {
		if row.SourceSHA != sourceSHA {
			// The source changed, so the cached bytes are stale.
			_ = s.deleteCacheEntry(ctx, fileID, profileKey, row.RelativePath)
		} else if file, info, err := s.openCacheFile(fileID, profileKey, row.RelativePath); err == nil {
			s.streamCache().hits.Add(1)
			s.touchCache(ctx, row.ID)
			return file, info, profileKey, nil
		} else {
			// The cache file is gone or unreadable; forget the row and
			// encode below.
			_ = s.deleteCacheEntry(ctx, fileID, profileKey, row.RelativePath)
		}
	}
	if encode == nil {
		s.streamCache().misses.Add(1)
		return nil, nil, "", ErrCacheMiss
	}

	// In-flight dedup: concurrent callers for the same key wait on the same
	// encode call with a bounded wait.
	rt := s.streamCache()
	release, err := rt.acquire(ctx, expected)
	if release != nil {
		defer release()
	}
	if err != nil {
		// Grace expiry: one final cache check before giving up. The encode
		// may have finished publishing in the instant between the waiter's
		// timer firing and this check; serving the fresh entry here keeps
		// the listener on the shared cached bytes instead of a duplicate
		// on-the-fly conversion competing for a transcode slot. A canceled
		// context is returned untouched.
		if errors.Is(err, ErrCacheBusy) {
			if row, cached, rowErr := s.cacheRow(ctx, fileID, profileKey); rowErr == nil && cached && row.SourceSHA == sourceSHA {
				if file, info, openErr := s.openCacheFile(fileID, profileKey, row.RelativePath); openErr == nil {
					s.streamCache().hits.Add(1)
					s.touchCache(ctx, row.ID)
					return file, info, profileKey, nil
				}
			}
		}
		return nil, nil, "", err
	}

	// Another caller may have finished the encode while we waited.
	if row, cached, err := s.cacheRow(ctx, fileID, profileKey); err == nil && cached {
		if row.SourceSHA == sourceSHA {
			if file, info, err := s.openCacheFile(fileID, profileKey, row.RelativePath); err == nil {
				s.streamCache().hits.Add(1)
				s.touchCache(ctx, row.ID)
				return file, info, profileKey, nil
			}
		} else {
			_ = s.deleteCacheEntry(ctx, fileID, profileKey, row.RelativePath)
		}
	}
	s.streamCache().misses.Add(1)

	// A very recent encode failure for this key short-circuits the retry so
	// N concurrent waiters of a failing source do not each re-run ffmpeg one
	// after another. The memo is short-lived (cacheFailureMemoTTL) and is
	// cleared by any successful encode, so this never caches a failure
	// indefinitely; the caller falls back to on-the-fly piping.
	if rt.recentEncodeFailure(expected, time.Now()) {
		return nil, nil, "", ErrCacheRecentlyFailed
	}

	// Encode into a .part file, fsync-rename it to the final name, INSERT the
	// row, then open+stat+return. The cache dir and the per-file directory
	// are created lazily via os.MkdirAll before anything is opened.
	part := expected + ".part"
	dest := filepath.Join(streamCacheDir(s.mediaDir), filepath.FromSlash(part))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return nil, nil, "", err
	}
	root, err := os.OpenRoot(streamCacheDir(s.mediaDir))
	if err != nil {
		return nil, nil, "", err
	}
	defer root.Close()
	if err := encode(ctx, dest); err != nil {
		// Remove the in-flight entry and delete the partial temp file, then
		// return the typed encode failure with the cause attached. Transient
		// failures (context cancellation, a saturated transcode slot) do not
		// feed the failure memo, so the next waiter re-encodes transparently.
		_ = root.Remove(part)
		_ = root.Remove(strconv.FormatInt(fileID, 10))
		if encodeFailureMemoizable(err) {
			rt.noteEncodeFailure(expected, time.Now())
		}
		return nil, nil, "", fmt.Errorf("%w: %w", ErrCacheEncodeFailed, err)
	}
	rt.clearEncodeFailure(expected)
	file, err := root.OpenFile(part, os.O_RDWR, 0)
	if err != nil {
		return nil, nil, "", err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return nil, nil, "", err
	}
	if err := file.Close(); err != nil {
		return nil, nil, "", err
	}
	if err := root.Rename(part, expected); err != nil {
		return nil, nil, "", err
	}
	info, err := root.Stat(expected)
	if err != nil {
		return nil, nil, "", err
	}
	if _, err := s.store.db.ExecContext(ctx, `INSERT INTO music_stream_cache
			(file_id, profile_key, relative_path, size_bytes, source_sha256, codec, bitrate_kbps, created_at, last_used_at)
		VALUES (?,?,?,?,?,?,?,unixepoch(),unixepoch())
		ON CONFLICT(file_id, profile_key) DO UPDATE SET
			relative_path=excluded.relative_path, size_bytes=excluded.size_bytes,
			source_sha256=excluded.source_sha256, codec=excluded.codec,
			bitrate_kbps=excluded.bitrate_kbps, created_at=excluded.created_at,
			last_used_at=unixepoch()`,
		fileID, profileKey, expected, info.Size(), sourceSHA, codec, bitrate); err != nil {
		return nil, nil, "", err
	}
	s.evictStreamCache(ctx, fileID, profileKey)

	file, info, err = s.openCacheFile(fileID, profileKey, expected)
	if err != nil {
		return nil, nil, "", err
	}
	return file, info, profileKey, nil
}

// evictStreamCache evicts least-recently-used entries until the cache fits
// the configured limit. It is best-effort: the encode already succeeded, so a
// failed eviction only risks the cache staying over the limit. The entry that
// was just inserted is never evicted by this call. Only rows whose stored
// path matches the deterministic name for their key have their file removed,
// so eviction can never touch anything outside the cache dir.
func (s *Service) evictStreamCache(ctx context.Context, keepFileID int64, keepProfileKey string) {
	limit := s.streamCacheLimitBytes(ctx)
	if limit <= 0 {
		return
	}
	for i := 0; i < 10000; i++ {
		var total int64
		if err := s.store.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(size_bytes),0) FROM music_stream_cache`).Scan(&total); err != nil || total <= limit {
			return
		}
		var id, fileID int64
		var profileKey, relative string
		err := s.store.db.QueryRowContext(ctx, `SELECT id, file_id, profile_key, relative_path
			FROM music_stream_cache
			WHERE NOT (file_id=? AND profile_key=?)
			ORDER BY last_used_at ASC, id ASC LIMIT 1`, keepFileID, keepProfileKey).
			Scan(&id, &fileID, &profileKey, &relative)
		if err != nil {
			return
		}
		if err := s.deleteCacheEntry(ctx, fileID, profileKey, relative); err != nil {
			return
		}
	}
}

// streamCacheLimitBytes reads the configured cache limit from the persisted
// preferences. On error, or when the limit is missing or zero, the built-in
// default applies. (cache_limit_mb lives in music_preferences; the profiles
// owner surfaces it on MusicPreferences, and this read is deliberately local
// so the cache does not depend on that struct's layout.)
func (s *Service) streamCacheLimitBytes(ctx context.Context) int64 {
	limit := int64(defaultStreamCacheLimitMB) * 1024 * 1024
	var configured int64
	if err := s.store.db.QueryRowContext(ctx, `SELECT cache_limit_mb FROM music_preferences WHERE id=1`).Scan(&configured); err == nil && configured > 0 {
		limit = configured * 1024 * 1024
	}
	return limit
}

// StreamCacheStats returns a snapshot of the stream cache: entry count, total
// size, configured limit and the process-lifetime hit/miss counters.
func (s *Service) StreamCacheStats(ctx context.Context) (StreamCacheStats, error) {
	var stats StreamCacheStats
	if err := s.store.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(size_bytes),0) FROM music_stream_cache`).
		Scan(&stats.Entries, &stats.SizeBytes); err != nil {
		return StreamCacheStats{}, err
	}
	rt := s.streamCache()
	stats.Hits = rt.hits.Load()
	stats.Misses = rt.misses.Load()
	stats.LimitBytes = s.streamCacheLimitBytes(ctx)
	return stats, nil
}

// ClearStreamCache removes every cached file and every cache row. The cache
// dir is a separate dotted directory inside the media root, and every removal
// happens through an os.Root opened on that dir, so music_files content is
// never touched and no path can escape the cache dir.
func (s *Service) ClearStreamCache(ctx context.Context) error {
	if _, err := s.store.db.ExecContext(ctx, `DELETE FROM music_stream_cache`); err != nil {
		return err
	}
	if s.mediaDir == "" {
		return nil
	}
	if _, err := os.Lstat(streamCacheDir(s.mediaDir)); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	root, err := os.OpenRoot(streamCacheDir(s.mediaDir))
	if err != nil {
		return err
	}
	defer root.Close()
	dirs, err := listRootDir(root, ".")
	if err != nil {
		return err
	}
	// The rows are already gone, so the clear must reclaim as much of the
	// cache dir as it can instead of stopping at the first failure: a file an
	// in-flight encode is still writing cannot be removed on every platform,
	// and a single such file must not leave the rest of the cache behind.
	var lastErr error
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		files, err := listRootDir(root, dir.Name())
		if err != nil {
			if lastErr == nil {
				lastErr = err
			}
			continue
		}
		for _, f := range files {
			if !f.Type().IsRegular() {
				continue
			}
			if err := root.Remove(path.Join(dir.Name(), f.Name())); err != nil {
				if lastErr == nil {
					lastErr = err
				}
				continue
			}
		}
		if err := root.Remove(dir.Name()); err != nil && lastErr == nil {
			lastErr = err
		}
	}
	return lastErr
}

// listRootDir lists one directory inside an os.Root without following
// symlinks out of the cache dir.
func listRootDir(root *os.Root, name string) ([]os.DirEntry, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return file.ReadDir(-1)
}

// PruneStreamCache reclaims cache storage no request can ever serve again:
// rows whose registered music_files row is gone (a converted-over original, a
// removed file or a rolled-back import leaves the id dangling) and the
// .part staging files a crash or kill leaves behind. Nothing scans or walks
// catalog media; only the cache dir inside the media root is touched, always
// through os.Root.
//
// stale bounds how old an orphaned .part file must be before it is removed,
// so a prune that races a live encode never deletes the .part that encode is
// still writing. Published cache files that no row references are removed
// unconditionally: a file between its rename and its INSERT can be caught by
// this window, and the affected request then simply re-encodes on its next
// attempt, so pruning stays safe to run while streams are being served.
//
// The rows whose file_id still exists are kept untouched; a reused file id
// with different bytes is invalidated by the source_sha256 check at open
// time, so pruning never has to guess about content identity.
func (s *Service) PruneStreamCache(ctx context.Context, stale time.Duration) error {
	rows, err := s.store.db.QueryContext(ctx, `SELECT file_id, profile_key, relative_path
		FROM music_stream_cache
		WHERE NOT EXISTS(SELECT 1 FROM music_files f WHERE f.id=music_stream_cache.file_id)`)
	if err != nil {
		return err
	}
	type orphan struct {
		fileID   int64
		profile  string
		relative string
	}
	var orphans []orphan
	for rows.Next() {
		var o orphan
		if err := rows.Scan(&o.fileID, &o.profile, &o.relative); err != nil {
			rows.Close()
			return err
		}
		orphans = append(orphans, o)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, o := range orphans {
		if err := s.deleteCacheEntry(ctx, o.fileID, o.profile, o.relative); err != nil {
			return err
		}
	}
	if s.mediaDir == "" {
		return nil
	}
	root, err := os.OpenRoot(streamCacheDir(s.mediaDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer root.Close()
	dirs, err := listRootDir(root, ".")
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	// referenced holds every relative path a row still owns; anything else on
	// disk is unreachable garbage.
	referenced := make(map[string]bool)
	refRows, err := s.store.db.QueryContext(ctx, `SELECT relative_path FROM music_stream_cache`)
	if err != nil {
		return err
	}
	for refRows.Next() {
		var relative string
		if err := refRows.Scan(&relative); err != nil {
			refRows.Close()
			return err
		}
		if name, ok := cleanCacheRelative(relative); ok {
			referenced[name] = true
		}
	}
	if err := refRows.Err(); err != nil {
		refRows.Close()
		return err
	}
	refRows.Close()
	deadline := time.Now().Add(-stale)
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		files, err := listRootDir(root, dir.Name())
		if err != nil {
			continue
		}
		empty := true
		for _, f := range files {
			if !f.Type().IsRegular() {
				empty = false
				continue
			}
			relative := dir.Name() + "/" + f.Name()
			if referenced[relative] {
				empty = false
				continue
			}
			if strings.HasSuffix(f.Name(), ".part") {
				// Staging garbage: only old enough files, so an encode that
				// is writing its .part right now keeps it.
				if info, err := f.Info(); err != nil || info.ModTime().After(deadline) {
					empty = false
					continue
				}
			}
			_ = root.Remove(relative)
		}
		if empty {
			_ = root.Remove(dir.Name())
		}
	}
	return nil
}
