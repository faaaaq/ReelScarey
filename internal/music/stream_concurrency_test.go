package music

// Concurrency behavior of the stream cache and the OpenPlayback fallback
// order, verified with channels and bounded polls instead of sleeps wherever
// the outcome can be made deterministic:
//
//   - N concurrent listeners of one uncached track must share exactly one
//     encode (2, 10 and 50 callers).
//   - Different tracks encode independently.
//   - A listener disconnecting mid-encode must not poison the shared key:
//     the next waiter re-encodes with its own context, no .part survives and
//     no cache row appears.
//   - A failing encode must not cost every waiter its own sequential ffmpeg
//     attempt: a short-lived per-key failure memo bounces concurrent waiters
//     to the on-the-fly pipe immediately, expires quickly and is cleared by
//     any successful encode. Context cancellation and a saturated transcode
//     slot are never memoized.
//   - A waiter whose cache grace expires makes one final cache check before
//     falling back, so an encode that finished in the last instant still
//     serves the waiter instead of a duplicate pipe.
//   - Eviction must not disturb an in-flight served file.
//
// The end-to-end OpenPlayback tests use the hermetic helper (the test binary
// re-executed as "ffmpeg", see transcode_cap_test.go TestMain). With
// streamTestFFmpegFileOutEnv set the fake ffmpeg also writes the file-output
// destination, so a successful cache publish is reachable deterministically;
// without it the publish fails exactly like the existing fallback tests
// assume, exercising the shared-failure storm through OpenPlayback.
//
// Not covered here: rapid sequential requests for many distinct tracks. The
// interesting behavior in that scenario (LRU eviction order under a size
// limit) is covered deterministically by TestStreamCacheEvictsLeastRecentlyUsed
// and TestCacheEvictionWhileServingActiveRequest; a time-pressure version
// would only add scheduling nondeterminism.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// streamTestFFmpegFileOutEnv makes the fake ffmpeg child write the file-output
// destination (see transcode_cap_test.go TestMain). Set per-test via t.Setenv.
const streamTestFFmpegFileOutEnv = "REELSCAREY_TEST_FFMPEG_FILE_OUT"

// concEncoder is a controllable encode func for concurrency tests: non-blocking
// started notifications, an optional gate that blocks encodes until closed
// (honoring ctx so a canceled leader aborts), and switchable failure modes.
type concEncoder struct {
	calls   atomic.Int64
	payload []byte
	fail    bool          // fail with a plain (memoizable) error
	busyErr bool          // fail with the transient music_transcode_busy error
	started chan struct{} // non-blocking notification per started encode
	gate    chan struct{} // encodes block until closed
}

func (e *concEncoder) encode(ctx context.Context, dest string) error {
	e.calls.Add(1)
	if e.started != nil {
		select {
		case e.started <- struct{}{}:
		default:
		}
	}
	if e.gate != nil {
		select {
		case <-e.gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if e.busyErr {
		return &Error{Code: "music_transcode_busy", Message: "transient slot exhaustion"}
	}
	if e.fail {
		return errors.New("synthetic encode failure")
	}
	return os.WriteFile(dest, e.payload, 0o644)
}

// registerSecondCacheFile registers one more music file reusing the first
// file's artist/album/track rows, so two distinct tracks share a fixture
// without a second SaveArtist.
func registerSecondCacheFile(t *testing.T, db *sql.DB, relative, sha string) int64 {
	t.Helper()
	var artistID, albumID, trackID int64
	if err := db.QueryRow(`SELECT artist_id, album_id, track_id FROM music_files LIMIT 1`).
		Scan(&artistID, &albumID, &trackID); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec(`INSERT INTO music_files(artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,sample_rate,channels,duration_ms,tag_json,imported_at,origin,source_name)
		VALUES(?,?,?,'Music',?,10,?,'flac',0,0,0,1000,'{}',1,'existing','other.flac')`,
		artistID, albumID, trackID, relative, sha)
	if err != nil {
		t.Fatal(err)
	}
	fileID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return fileID
}

// openCachedConcurrent runs one OpenCachedStream call in a goroutine and
// returns the result via a channel, so tests can coordinate with channels.
type cachedResult struct {
	file *os.File
	info os.FileInfo
	key  string
	err  error
}

func openCachedAsync(service *Service, fileID int64, codec string, bitrate int, sha string, encode func(context.Context, string) error) <-chan cachedResult {
	out := make(chan cachedResult, 1)
	go func() {
		file, info, key, err := service.OpenCachedStream(context.Background(), fileID, codec, bitrate, sha, encode)
		out <- cachedResult{file, info, key, err}
	}()
	return out
}

// readAndClose drains a served cache file and compares it to the payload.
func readAndClose(t *testing.T, file *os.File, want []byte) {
	t.Helper()
	data, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if string(data) != string(want) {
		t.Fatalf("served bytes = %q, want %q", data, want)
	}
}

func TestConcurrentListenersShareOneEncode(t *testing.T) {
	for _, callers := range []int{2, 10, 50} {
		t.Run(fmt.Sprintf("%d listeners", callers), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			service, db, _ := cacheTestService(t)
			fileID := registerCacheFile(t, db, "Music/A/Song.flac", "sha")
			payload := cacheTestPayload("opus", 128)
			enc := &concEncoder{payload: payload, started: make(chan struct{}, callers+1), gate: make(chan struct{})}

			// The first caller becomes the leader and blocks inside the gate;
			// every other caller must wait for that same encode.
			leader := openCachedAsync(service, fileID, "opus", 128, "sha", enc.encode)
			<-enc.started

			results := make([]cachedResult, callers-1)
			var wg sync.WaitGroup
			for i := 1; i < callers; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					file, info, key, err := service.OpenCachedStream(ctx, fileID, "opus", 128, "sha", enc.encode)
					results[i-1] = cachedResult{file, info, key, err}
				}(i)
			}
			// Closing the gate publishes the leader's encode; waiters then
			// serve the fresh entry, whether they were queued on the dedup
			// lock or arrived after publication.
			close(enc.gate)
			wg.Wait()

			leaderRes := <-leader
			if leaderRes.err != nil {
				t.Fatalf("leader: %v", leaderRes.err)
			}
			readAndClose(t, leaderRes.file, payload)
			for i, res := range results {
				if res.err != nil {
					t.Fatalf("listener %d: %v", i+1, res.err)
				}
				if res.key != "opus-128" {
					t.Fatalf("listener %d key = %q", i+1, res.key)
				}
				readAndClose(t, res.file, payload)
			}
			if calls := enc.calls.Load(); calls != 1 {
				t.Fatalf("encode ran %d times, want exactly 1", calls)
			}
			stats, err := service.StreamCacheStats(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if stats.Hits != int64(callers-1) || stats.Misses != 1 {
				t.Fatalf("hit/miss = %d/%d, want %d/1", stats.Hits, stats.Misses, callers-1)
			}
			rt := service.streamCache()
			rt.mu.Lock()
			locks, holds := len(rt.locks), len(rt.holds)
			rt.mu.Unlock()
			if locks != 0 || holds != 0 {
				t.Fatalf("dedup state leaked: %d locks, %d holds", locks, holds)
			}
		})
	}
}

func TestConcurrentListenersDifferentTracksEncodeIndependently(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	service, db, _ := cacheTestService(t)
	fileA := registerCacheFile(t, db, "Music/A/Song.flac", "sha-a")
	fileB := registerSecondCacheFile(t, db, "Music/A/Other.flac", "sha-b")
	payloadA := []byte("encoded-track-a")
	payloadB := []byte("encoded-track-b")
	encA := &concEncoder{payload: payloadA, started: make(chan struct{}, 4), gate: make(chan struct{})}
	encB := &concEncoder{payload: payloadB, started: make(chan struct{}, 4), gate: make(chan struct{})}

	var wg sync.WaitGroup
	resultsA := make([]cachedResult, 2)
	resultsB := make([]cachedResult, 2)
	collect := func(fileID int64, sha string, enc *concEncoder, out []cachedResult, slot int) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			file, info, key, err := service.OpenCachedStream(ctx, fileID, "opus", 128, sha, enc.encode)
			out[slot] = cachedResult{file, info, key, err}
		}()
	}
	collect(fileA, "sha-a", encA, resultsA, 0)
	<-encA.started
	collect(fileB, "sha-b", encB, resultsB, 0)
	<-encB.started
	// A second listener per track while both encodes run.
	collect(fileA, "sha-a", encA, resultsA, 1)
	collect(fileB, "sha-b", encB, resultsB, 1)
	close(encA.gate)
	close(encB.gate)
	wg.Wait()

	wantBytes := map[string][]byte{"a": payloadA, "b": payloadB}
	for name, group := range map[string][]cachedResult{"a": resultsA, "b": resultsB} {
		if len(group) != 2 {
			t.Fatalf("track %s: %d results", name, len(group))
		}
		for i, res := range group {
			if res.err != nil {
				t.Fatalf("track %s listener %d: %v", name, i+1, res.err)
			}
			if res.key != "opus-128" {
				t.Fatalf("track %s listener %d key = %q", name, i+1, res.key)
			}
			readAndClose(t, res.file, wantBytes[name])
		}
	}
	if encA.calls.Load() != 1 || encB.calls.Load() != 1 {
		t.Fatalf("independent encodes duplicated: a=%d b=%d", encA.calls.Load(), encB.calls.Load())
	}
	if rows := cacheRowCount(t, db); rows != 2 {
		t.Fatalf("cache rows = %d, want 2", rows)
	}
}

// A leader whose request context is canceled mid-encode must not poison the
// shared key: its ffmpeg is killed, the .part staging file is removed, no
// cache row is written, and the waiting listener transparently re-encodes
// with its own context and succeeds.
func TestStreamListenerDisconnectDuringSharedEncodeReencodes(t *testing.T) {
	service, db, mediaDir := cacheTestService(t)
	fileID := registerCacheFile(t, db, "Music/A/Song.flac", "sha")
	payload := cacheTestPayload("opus", 128)
	enc := &concEncoder{payload: payload, started: make(chan struct{}, 4), gate: make(chan struct{})}

	leaderCtx, stopLeader := context.WithCancel(context.Background())
	leaderDone := make(chan error, 1)
	go func() {
		_, _, _, err := service.OpenCachedStream(leaderCtx, fileID, "opus", 128, "sha", enc.encode)
		leaderDone <- err
	}()
	<-enc.started // the leader holds the key and is inside the encode

	waiter := openCachedAsync(service, fileID, "opus", 128, "sha", enc.encode)
	// The leader disconnects while the waiter waits for the shared encode.
	stopLeader()

	leaderErr := <-leaderDone
	if !errors.Is(leaderErr, context.Canceled) {
		t.Fatalf("leader error = %v, want context.Canceled", leaderErr)
	}
	if !errors.Is(leaderErr, ErrCacheEncodeFailed) {
		t.Fatalf("leader error = %v, want ErrCacheEncodeFailed wrapped", leaderErr)
	}

	// The waiter's own encode must land and publish.
	close(enc.gate)
	res := <-waiter
	if res.err != nil {
		t.Fatalf("waiter after leader disconnect: %v", res.err)
	}
	readAndClose(t, res.file, payload)
	if calls := enc.calls.Load(); calls != 2 {
		t.Fatalf("encode ran %d times, want the leader's canceled attempt plus the waiter's re-encode", calls)
	}
	if rows := cacheRowCount(t, db); rows != 1 {
		t.Fatalf("cache rows = %d, want only the waiter's published entry", rows)
	}
	if names := cacheDirNames(t, mediaDir); len(names) != 1 {
		t.Fatalf("cache dir = %v, want only the published file", names)
	}
	// A canceled encode never feeds the failure memo: the waiter re-encoded
	// immediately instead of being bounced for the memo's lifetime.
	rt := service.streamCache()
	if rt.recentEncodeFailure(cacheRelativePath(fileID, "opus-128"), time.Now()) {
		t.Fatal("a canceled encode must not be memoized as a failure")
	}
}

// Every listener going away at once must leave no rows, no staging files, no
// leaked dedup state and no failure memo.
func TestStreamAllListenersDisconnectLeavesCleanState(t *testing.T) {
	service, db, mediaDir := cacheTestService(t)
	fileID := registerCacheFile(t, db, "Music/A/Song.flac", "sha")
	enc := &concEncoder{payload: []byte("never-served"), started: make(chan struct{}, 8), gate: make(chan struct{})}

	const callers = 4
	ctxs := make([]context.CancelFunc, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		callCtx, stop := context.WithCancel(context.Background())
		ctxs[i] = stop
		go func() {
			defer wg.Done()
			_, _, _, err := service.OpenCachedStream(callCtx, fileID, "opus", 128, "sha", enc.encode)
			errs <- err
		}()
		if i == 0 {
			<-enc.started // the first caller holds the key and is encoding
		}
	}
	// Wait until every caller registered its dedup hold, so the cancel below
	// races neither the acquire path nor anything else.
	rt := service.streamCache()
	key := cacheRelativePath(fileID, "opus-128")
	deadline := time.Now().Add(10 * time.Second)
	for {
		rt.mu.Lock()
		holds := rt.holds[key]
		rt.mu.Unlock()
		if holds == callers {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("callers never all reached the dedup wait: %d holds", holds)
		}
		time.Sleep(2 * time.Millisecond)
	}

	for _, stop := range ctxs {
		stop()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err == nil {
			t.Fatal("a canceled listener must not be served")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("listener error = %v, want context.Canceled", err)
		}
	}
	if rows := cacheRowCount(t, db); rows != 0 {
		t.Fatalf("cache rows = %d, want 0", rows)
	}
	if names := cacheDirNames(t, mediaDir); len(names) != 0 {
		t.Fatalf("cache dir = %v, want empty", names)
	}
	rt.mu.Lock()
	locks, holds := len(rt.locks), len(rt.holds)
	rt.mu.Unlock()
	if locks != 0 || holds != 0 {
		t.Fatalf("dedup state leaked: %d locks, %d holds", locks, holds)
	}
	if rt.recentEncodeFailure(key, time.Now()) {
		t.Fatal("a canceled encode must not leave a failure memo")
	}

	// A fresh listener still works normally afterwards.
	fresh := &concEncoder{payload: cacheTestPayload("opus", 128)}
	file, _, _, err := service.OpenCachedStream(context.Background(), fileID, "opus", 128, "sha", fresh.encode)
	if err != nil {
		t.Fatalf("fresh listener after the mass disconnect: %v", err)
	}
	readAndClose(t, file, fresh.payload)
}

// A failing encode for a shared key must not cost every waiter its own
// sequential encode attempt: concurrent waiters get the short-lived failure
// memo error immediately and fall back to on-the-fly piping. The memo expires
// after cacheFailureMemoTTL, and any successful encode clears it at once, so
// failures are never cached indefinitely.
func TestStreamSharedEncodeFailureFailsFastForWaiters(t *testing.T) {
	previous := cacheFailureMemoTTL
	cacheFailureMemoTTL = 300 * time.Millisecond
	t.Cleanup(func() { cacheFailureMemoTTL = previous })

	service, db, mediaDir := cacheTestService(t)
	fileID := registerCacheFile(t, db, "Music/A/Song.flac", "sha")
	enc := &concEncoder{payload: cacheTestPayload("opus", 128), fail: true}

	// The leader's encode fails and records the memo before releasing the key.
	_, _, _, leaderErr := service.OpenCachedStream(context.Background(), fileID, "opus", 128, "sha", enc.encode)
	if !errors.Is(leaderErr, ErrCacheEncodeFailed) {
		t.Fatalf("leader error = %v, want ErrCacheEncodeFailed", leaderErr)
	}
	key := cacheRelativePath(fileID, "opus-128")
	if !service.streamCache().recentEncodeFailure(key, time.Now()) {
		t.Fatal("the failed encode must be memoized")
	}
	if rows := cacheRowCount(t, db); rows != 0 {
		t.Fatalf("cache rows = %d, want 0", rows)
	}
	if names := cacheDirNames(t, mediaDir); len(names) != 0 {
		t.Fatalf("partial files survived the failed encode: %v", names)
	}

	// Concurrent waiters must bounce off the memo instead of each re-running
	// the encode sequentially.
	const waiters = 8
	var wg sync.WaitGroup
	waiterErrs := make([]error, waiters)
	for i := 0; i < waiters; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, _, err := service.OpenCachedStream(context.Background(), fileID, "opus", 128, "sha", enc.encode)
			waiterErrs[i] = err
		}(i)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("waiters did not finish within the memo window")
	}
	for i, err := range waiterErrs {
		if !errors.Is(err, ErrCacheRecentlyFailed) {
			t.Fatalf("waiter %d error = %v, want ErrCacheRecentlyFailed", i+1, err)
		}
	}
	if calls := enc.calls.Load(); calls != 1 {
		t.Fatalf("encode ran %d times, want exactly the leader's failing attempt", calls)
	}

	// The memo expires: a later caller re-attempts the encode and succeeds,
	// which also clears the memo for good. The still-failing encoder is not
	// reused, so the retry proves a real re-attempt happened.
	time.Sleep(2 * cacheFailureMemoTTL)
	okEnc := &concEncoder{payload: enc.payload}
	file, _, _, err := service.OpenCachedStream(context.Background(), fileID, "opus", 128, "sha", okEnc.encode)
	if err != nil {
		t.Fatalf("retry after the memo expired: %v", err)
	}
	readAndClose(t, file, okEnc.payload)
	if calls := okEnc.calls.Load(); calls != 1 {
		t.Fatalf("retry encode ran %d times, want the expired-memo retry", calls)
	}
	if calls := enc.calls.Load(); calls != 1 {
		t.Fatalf("the failing encode ran %d times, want no further attempts", calls)
	}
	if service.streamCache().recentEncodeFailure(key, time.Now()) {
		t.Fatal("a successful encode must clear the failure memo")
	}
	if rows := cacheRowCount(t, db); rows != 1 {
		t.Fatalf("cache rows = %d, want 1", rows)
	}
}

// Transient failures must never be memoized: a saturated transcode slot or a
// canceled context describes the server's moment, not the source, so the very
// next caller must re-attempt the encode instead of being bounced for the
// memo's lifetime.
func TestStreamEncodeFailureMemoExcludesTransientFailures(t *testing.T) {
	service, db, _ := cacheTestService(t)
	fileID := registerCacheFile(t, db, "Music/A/Song.flac", "sha")
	key := cacheRelativePath(fileID, "opus-128")
	rt := service.streamCache()

	// A saturated transcode slot surfaces inside the encode as the typed
	// music_transcode_busy error: no memo may be recorded.
	busy := &concEncoder{busyErr: true}
	if _, _, _, err := service.OpenCachedStream(context.Background(), fileID, "opus", 128, "sha", busy.encode); err == nil {
		t.Fatal("expected the busy encode to fail")
	}
	if rt.recentEncodeFailure(key, time.Now()) {
		t.Fatal("a saturated transcode slot must not be memoized")
	}

	// A canceled listener surfaces as a context error: no memo either.
	cancelled := &concEncoder{payload: []byte("x")}
	ctx, stop := context.WithCancel(context.Background())
	stop()
	if _, _, _, err := service.OpenCachedStream(ctx, fileID, "opus", 128, "sha", cancelled.encode); err == nil {
		t.Fatal("expected the canceled encode to fail")
	}
	if rt.recentEncodeFailure(key, time.Now()) {
		t.Fatal("a canceled encode must not be memoized")
	}

	// The very next caller re-encodes immediately in both cases.
	retry := &concEncoder{payload: cacheTestPayload("opus", 128)}
	file, _, _, err := service.OpenCachedStream(context.Background(), fileID, "opus", 128, "sha", retry.encode)
	if err != nil {
		t.Fatalf("immediate retry after transient failures: %v", err)
	}
	readAndClose(t, file, retry.payload)
	if calls := retry.calls.Load(); calls != 1 {
		t.Fatalf("retry encode ran %d times, want 1", calls)
	}
}

func TestGraceExpiredWaiterServesFinalCacheCheck(t *testing.T) {
	previous := cacheEncodeGrace
	cacheEncodeGrace = 750 * time.Millisecond
	t.Cleanup(func() { cacheEncodeGrace = previous })

	service, db, mediaDir := cacheTestService(t)
	fileID := registerCacheFile(t, db, "Music/A/Song.flac", "sha")
	key := cacheRelativePath(fileID, "opus-128")
	payload := cacheTestPayload("opus", 128)
	enc := &concEncoder{payload: payload}

	// The test itself holds the per-key token, so the waiter below sees a
	// long-running "encode" without any encode func running.
	rt := service.streamCache()
	release, err := rt.acquire(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	waiter := openCachedAsync(service, fileID, "opus", 128, "sha", enc.encode)
	// Wait until the waiter registered its hold and is blocked on the key.
	deadline := time.Now().Add(10 * time.Second)
	for {
		rt.mu.Lock()
		holds := rt.holds[key]
		rt.mu.Unlock()
		if holds == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("waiter never reached the dedup wait")
		}
		time.Sleep(2 * time.Millisecond)
	}

	// The encode "finishes" while the waiter still waits: publish the entry
	// directly so the waiter's grace-expiry final check finds it.
	dir := filepath.Join(mediaDir, streamCacheDirName, strconv.FormatInt(fileID, 10))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "opus-128"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_stream_cache
			(file_id, profile_key, relative_path, size_bytes, source_sha256, codec, bitrate_kbps, created_at, last_used_at)
		VALUES(?,?,'`+key+`',?,?,'opus',128,unixepoch(),unixepoch())`,
		fileID, "opus-128", len(payload), "sha"); err != nil {
		t.Fatal(err)
	}

	res := <-waiter
	if res.err != nil {
		t.Fatalf("waiter whose grace expired: %v, want the final cache check to serve the fresh entry", res.err)
	}
	readAndClose(t, res.file, payload)
	if calls := enc.calls.Load(); calls != 0 {
		t.Fatalf("the final check must serve the cache, encode ran %d times", calls)
	}
	stats, err := service.StreamCacheStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Hits != 1 {
		t.Fatalf("hits = %d, want the final-check hit", stats.Hits)
	}
}

// Eviction must never disturb a request that is actively being served: the
// open file handle keeps delivering the full payload even after its row is
// evicted, and the next open for the evicted key re-encodes cleanly.
func TestCacheEvictionWhileServingActiveRequest(t *testing.T) {
	ctx := context.Background()
	service, db, _ := cacheTestService(t)
	fileA := registerCacheFile(t, db, "Music/A/Song.flac", "sha-a")
	fileB := registerSecondCacheFile(t, db, "Music/A/Other.flac", "sha-b")
	payload := make([]byte, 600*1024)
	for i := range payload {
		payload[i] = byte(i % 251)
	}

	// Encode A and keep it open: an active listener.
	encA := &concEncoder{payload: payload}
	servedA, _, _, err := service.OpenCachedStream(ctx, fileA, "opus", 128, "sha-a", encA.encode)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE music_preferences SET cache_limit_mb=1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}

	// Encoding B pushes the 1MB limit and evicts A while A is being served.
	encB := &concEncoder{payload: payload}
	servedB, _, _, err := service.OpenCachedStream(ctx, fileB, "opus", 128, "sha-b", encB.encode)
	if err != nil {
		t.Fatal(err)
	}
	readAndClose(t, servedB, payload)
	var rows int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_stream_cache`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("cache rows after eviction = %d, want only the newest entry", rows)
	}

	// The active listener still reads its full payload from the open handle.
	readAndClose(t, servedA, payload)

	// The next open for A is a miss (its row was evicted) and re-encodes.
	fileA2, _, keyA2, err := service.OpenCachedStream(ctx, fileA, "opus", 128, "sha-a", encA.encode)
	if err != nil {
		t.Fatalf("re-encode after eviction: %v", err)
	}
	if keyA2 != "opus-128" {
		t.Fatalf("key = %q", keyA2)
	}
	readAndClose(t, fileA2, payload)
	if calls := encA.calls.Load(); calls != 2 {
		t.Fatalf("encode A ran %d times, want the original plus the re-encode", calls)
	}
	// The re-encode of A evicts B (older last_used, lower id), keeping the
	// cache inside the limit.
	stats, err := service.StreamCacheStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Entries != 1 || stats.SizeBytes != int64(len(payload)) {
		t.Fatalf("stats after re-encode = %+v, want only entry A inside the limit", stats)
	}
}

// End to end through OpenPlayback with the hermetic fake ffmpeg: ten
// concurrent listeners of one uncached track must all be served from the
// single published cache entry.
func TestOpenPlaybackConcurrentSameTrackPublishesOnce(t *testing.T) {
	t.Setenv(streamTestFFmpegFileOutEnv, "1")
	helperFFmpeg(t)
	s, _, fileID := transcodeFixture(t)
	ctx := context.Background()

	const callers = 10
	streams := make([]PlaybackStream, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			streams[i], errs[i] = s.OpenPlayback(ctx, fileID, "opus", 96, 0)
		}(i)
	}
	wg.Wait()

	want := []byte("audio-bytes-for-conversion")
	served := 0
	for i, err := range errs {
		if err != nil {
			t.Fatalf("listener %d: %v", i+1, err)
		}
		if streams[i].File == nil || streams[i].Body != nil {
			t.Fatalf("listener %d must be served from the published cache file", i+1)
		}
		data, err := io.ReadAll(streams[i].File)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != string(want) {
			t.Fatalf("listener %d bytes = %q", i+1, data)
		}
		streams[i].Close()
		served++
	}
	if served != callers {
		t.Fatalf("served %d of %d listeners", served, callers)
	}
	if rows := cacheRowCount(t, s.store.db); rows != 1 {
		t.Fatalf("cache rows = %d, want exactly one published entry", rows)
	}
}

// End to end through OpenPlayback without the file-output helper: the cache
// publish fails for every attempt (the shared-failure storm), but concurrent
// listeners must still either get a working on-the-fly pipe or an honest
// bounded busy error — never a hang and never an untyped failure. The failure
// memo keeps the storm from spawning one encode per waiter.
func TestOpenPlaybackConcurrentFailingTrackStaysBounded(t *testing.T) {
	shortGrace(t)
	helperFFmpeg(t)
	s, _, fileID := transcodeFixture(t)
	ctx := context.Background()

	const callers = 10
	streams := make([]PlaybackStream, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			streams[i], errs[i] = s.OpenPlayback(ctx, fileID, "opus", 96, 0)
		}(i)
	}
	wg.Wait()

	successes := 0
	open := make([]PlaybackStream, 0, callers)
	for i, err := range errs {
		if err == nil {
			if streams[i].Body == nil {
				t.Fatalf("listener %d: a failed publish must fall back to the pipe", i+1)
			}
			successes++
			open = append(open, streams[i])
			continue
		}
		if code := cacheErrorCode(t, err); code != "music_transcode_busy" {
			t.Fatalf("listener %d error = %v, want music_transcode_busy", i+1, err)
		}
	}
	if successes < 1 {
		t.Fatal("at least one listener must get a working pipe")
	}
	if rows := cacheRowCount(t, s.store.db); rows != 0 {
		t.Fatalf("cache rows = %d, want 0 (nothing published)", rows)
	}
	for _, stream := range open {
		stream.Close()
	}

	// Capacity is fully released once the pipes close.
	stream, err := s.OpenPlayback(ctx, fileID, "opus", 96, 0)
	if err != nil {
		t.Fatalf("playback after the storm: %v", err)
	}
	stream.Close()
}
