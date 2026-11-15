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
// is far longer than transcodeGracePeriod; it is a separate constant and
// transcodeGracePeriod is never mutated for cache waits.
const cacheEncodeGrace = 2 * time.Minute

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
	hits   atomic.Int64
	misses atomic.Int64
}

var streamCacheRuntimes sync.Map // *Service -> *streamCacheRuntime

func (s *Service) streamCache() *streamCacheRuntime {
	if rt, ok := streamCacheRuntimes.Load(s); ok {
		return rt.(*streamCacheRuntime)
	}
	rt := &streamCacheRuntime{locks: map[string]chan struct{}{}}
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
// the deterministic name for its key and is opened through os.Root around the
// cache dir, mirroring OpenAudioFile's containment, so a stale or tampered
// path can never escape that root.
func (s *Service) openCacheFile(relative string) (*os.File, os.FileInfo, error) {
	if s.mediaDir == "" {
		return nil, nil, &Error{Code: "music_file_missing", Message: "This registered music file is not available on the server."}
	}
	name, ok := cleanCacheRelative(relative)
	if !ok {
		return nil, nil, ErrInvalidInput
	}
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

// acquireCacheLock takes the per-key in-flight dedup slot with a bounded,
// ctx-aware wait (cacheEncodeGrace). A caller whose encode outlives the grace
// period lets the waiter fail with a busy error instead of queueing
// unbounded work, mirroring acquireTranscodeSlot.
func (rt *streamCacheRuntime) acquire(ctx context.Context, key string) (chan struct{}, error) {
	rt.mu.Lock()
	ch, ok := rt.locks[key]
	if !ok {
		ch = make(chan struct{}, 1)
		rt.locks[key] = ch
	}
	rt.mu.Unlock()
	select {
	case ch <- struct{}{}:
		return ch, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(cacheEncodeGrace):
		return nil, &Error{Code: "music_cache_busy", Message: "The server is already preparing this stream. Wait a moment and try again."}
	}
}

func (rt *streamCacheRuntime) release(ch chan struct{}) {
	select {
	case <-ch:
	default:
	}
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
		} else if file, info, err := s.openCacheFile(row.RelativePath); err == nil {
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
	lock, err := rt.acquire(ctx, expected)
	if err != nil {
		return nil, nil, "", err
	}
	defer rt.release(lock)

	// Another caller may have finished the encode while we waited.
	if row, cached, err := s.cacheRow(ctx, fileID, profileKey); err == nil && cached {
		if row.SourceSHA == sourceSHA {
			if file, info, err := s.openCacheFile(row.RelativePath); err == nil {
				s.streamCache().hits.Add(1)
				s.touchCache(ctx, row.ID)
				return file, info, profileKey, nil
			}
		} else {
			_ = s.deleteCacheEntry(ctx, fileID, profileKey, row.RelativePath)
		}
	}
	s.streamCache().misses.Add(1)

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
		// return the typed encode failure with the cause attached.
		_ = root.Remove(part)
		_ = root.Remove(strconv.FormatInt(fileID, 10))
		return nil, nil, "", fmt.Errorf("%w: %w", ErrCacheEncodeFailed, err)
	}
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

	file, info, err = s.openCacheFile(expected)
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
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		files, err := listRootDir(root, dir.Name())
		if err != nil {
			return err
		}
		for _, f := range files {
			if !f.Type().IsRegular() {
				continue
			}
			if err := root.Remove(path.Join(dir.Name(), f.Name())); err != nil {
				return err
			}
		}
		if err := root.Remove(dir.Name()); err != nil {
			return err
		}
	}
	return nil
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
