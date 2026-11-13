package music

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
)

// The fake "ffmpeg" is the test binary itself: a transcode child process is
// always started with transcodeArgs, whose first argument is -hide_banner, so
// TestMain re-executed as a child detects its own transcode role from the
// arguments and streams stdin to stdout like a real conversion would. This
// keeps every test hermetic: no real ffmpeg, no network, no media workload.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "-hide_banner" {
		buf := make([]byte, 512)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				os.Stdout.Write(buf[:n])
			}
			if err != nil {
				os.Exit(0)
			}
		}
	}
	os.Exit(m.Run())
}

// transcodeFixture registers one music file whose container (wma) always
// requires conversion, so OpenPlayback takes the ffmpeg path when ffmpeg is
// "available".
func transcodeFixture(t *testing.T) (*Service, *sql.DB, int64) {
	t.Helper()
	ctx := context.Background()
	mediaDir := t.TempDir()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
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
	relative := "Music/Artist/Album/01 Song.wma"
	path := filepath.Join(mediaDir, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("audio-bytes-for-conversion"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec(`INSERT INTO music_files(artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,sample_rate,channels,duration_ms,tag_json,imported_at,origin,source_name)
		VALUES(?,?,?,'Music',?,26,'sha','wma',192,44100,2,1000,'{}',1,'existing','01 Song.wma')`,
		saved.ID, albums[0].ID, tracks[0].ID, relative)
	if err != nil {
		t.Fatal(err)
	}
	fileID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(db, mediaDir)
	return service, db, fileID
}

// shortGrace bounds the busy wait so over-limit requests fail fast in tests.
func shortGrace(t *testing.T) {
	t.Helper()
	previous := transcodeGracePeriod
	transcodeGracePeriod = 50 * time.Millisecond
	t.Cleanup(func() { transcodeGracePeriod = previous })
}

// helperFFmpeg pins ffmpeg detection on a self-referential test executable
// (the test binary playing the helper role) so conversion spawns succeed.
func helperFFmpeg(t *testing.T) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	transcodeLookup = func(string) (string, error) { return exe, nil }
	resetTranscodeCacheForTest()
	t.Cleanup(func() {
		transcodeLookup = exec.LookPath
		resetTranscodeCacheForTest()
	})
}

// brokenFFmpeg pins ffmpeg detection on a path that can never start, so
// process spawn fails after the slot is taken.
func brokenFFmpeg(t *testing.T) {
	t.Helper()
	transcodeLookup = func(string) (string, error) { return filepath.Join(t.TempDir(), "no-such-ffmpeg"), nil }
	resetTranscodeCacheForTest()
	t.Cleanup(func() {
		transcodeLookup = exec.LookPath
		resetTranscodeCacheForTest()
	})
}

func openTranscode(t *testing.T, s *Service, fileID int64) (PlaybackStream, error) {
	t.Helper()
	return s.OpenPlayback(context.Background(), fileID, "raw", 0, 0)
}

// NEW-A1: concurrent transcode requests must be bounded; over-limit callers
// get an honest busy error, and closing streams frees slots for new work.
func TestTranscodeCapsConcurrentProcesses(t *testing.T) {
	shortGrace(t)
	helperFFmpeg(t)
	s, _, fileID := transcodeFixture(t)

	const callers = maxTranscodes + 2
	results := make([]PlaybackStream, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = openTranscode(t, s, fileID)
		}(i)
	}
	wg.Wait()

	bad := 0
	for i := 0; i < callers; i++ {
		if errs[i] == nil {
			if results[i].Body == nil {
				t.Fatalf("call %d: expected a transcode stream", i)
			}
			continue
		}
		var musicErr *Error
		if !errors.As(errs[i], &musicErr) || musicErr.Code != "music_transcode_busy" {
			t.Fatalf("call %d: expected music_transcode_busy, got %v", i, errs[i])
		}
		bad++
	}
	if bad != 2 {
		t.Fatalf("expected exactly 2 busy rejections at limit %d, got %d", maxTranscodes, bad)
	}
	for i := 0; i < callers; i++ {
		if errs[i] == nil {
			results[i].Close()
		}
	}

	// Capacity released: new transcodes work again.
	stream, err := openTranscode(t, s, fileID)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	stream.Close()
}

// A failed process start must not leak a slot: after a spawn failure the
// service must still be able to reach its full concurrency.
func TestTranscodeSlotReleasedOnProcessStartFailure(t *testing.T) {
	shortGrace(t)
	helperFFmpeg(t)
	s, _, fileID := transcodeFixture(t)

	// Hold maxTranscodes-1 slots with live streams.
	held := make([]PlaybackStream, 0, maxTranscodes-1)
	for i := 0; i < maxTranscodes-1; i++ {
		stream, err := openTranscode(t, s, fileID)
		if err != nil {
			t.Fatalf("warm-up transcode %d: %v", i, err)
		}
		held = append(held, stream)
	}
	t.Cleanup(func() {
		for _, stream := range held {
			stream.Close()
		}
	})

	// The last available slot is taken by a spawn that fails.
	brokenFFmpeg(t)
	if _, err := openTranscode(t, s, fileID); err == nil {
		t.Fatal("expected spawn failure")
	}
	helperFFmpeg(t)

	// The released slot must be usable again.
	stream, err := openTranscode(t, s, fileID)
	if err != nil {
		t.Fatalf("slot leaked by failed spawn: %v", err)
	}
	stream.Close()
}

// Cancelling a stream releases its slot for the next caller.
func TestTranscodeSlotReleasedOnCancel(t *testing.T) {
	shortGrace(t)
	helperFFmpeg(t)
	s, _, fileID := transcodeFixture(t)

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := s.OpenPlayback(ctx, fileID, "raw", 0, 0)
	if err != nil {
		t.Fatalf("first transcode: %v", err)
	}
	cancel()
	stream.Close()

	next, err := s.OpenPlayback(context.Background(), fileID, "raw", 0, 0)
	if err != nil {
		t.Fatalf("slot not released after cancellation: %v", err)
	}
	next.Close()
}

// Double-close must be a single release: the second close must not underflow
// the semaphore (which would block) or corrupt capacity.
func TestTranscodeDoubleCloseIsSingleRelease(t *testing.T) {
	shortGrace(t)
	helperFFmpeg(t)
	s, _, fileID := transcodeFixture(t)

	stream, err := openTranscode(t, s, fileID)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		stream.Close()
		stream.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("double close deadlocked: release is not idempotent")
	}

	// Full capacity must still be available for four fresh streams.
	holds := make([]PlaybackStream, 0, maxTranscodes)
	for i := 0; i < maxTranscodes; i++ {
		next, err := openTranscode(t, s, fileID)
		if err != nil {
			t.Fatalf("stream %d after double close: %v", i, err)
		}
		holds = append(holds, next)
	}
	for _, next := range holds {
		next.Close()
	}
}

// Native (non-converted) streams never spawn a process and must be served
// even when the transcode semaphore is saturated.
func TestTranscodeNativeStreamsNotCapped(t *testing.T) {
	withoutFFmpeg(t)
	s, db, _ := transcodeFixture(t)
	// Register an mp3 file: native passthrough, no conversion needed.
	relative := "Music/Artist/Album/02 Song.mp3"
	path := filepath.Join(s.mediaDir, filepath.FromSlash(relative))
	if err := os.WriteFile(path, []byte("native-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec(`INSERT INTO music_files(artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,sample_rate,channels,duration_ms,tag_json,imported_at,origin,source_name)
		VALUES((SELECT artist_id FROM music_files LIMIT 1),(SELECT album_id FROM music_files LIMIT 1),(SELECT track_id FROM music_files LIMIT 1),'Music',?,13,'sha','mp3',192,44100,2,1000,'{}',1,'existing','02 Song.mp3')`,
		relative)
	if err != nil {
		t.Fatal(err)
	}
	fileID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	// Saturate the semaphore directly.
	slots := s.transcodeSlots()
	for i := 0; i < maxTranscodes; i++ {
		slots <- struct{}{}
	}
	t.Cleanup(func() {
		for i := 0; i < maxTranscodes; i++ {
			<-slots
		}
	})

	stream, err := s.OpenPlayback(context.Background(), fileID, "raw", 0, 0)
	if err != nil {
		t.Fatalf("native stream must not be capped: %v", err)
	}
	if stream.Body != nil {
		t.Fatal("native stream must not be converted")
	}
	stream.Close()
}

// An already-cancelled request fails fast without occupying a slot.
func TestTranscodeAcquireRespectsCancelledContext(t *testing.T) {
	shortGrace(t)
	helperFFmpeg(t)
	s, _, fileID := transcodeFixture(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var succeeded atomic.Int32
	for i := 0; i < maxTranscodes; i++ {
		go func() {
			if stream, err := s.OpenPlayback(context.Background(), fileID, "raw", 0, 0); err == nil {
				succeeded.Add(1)
				stream.Close()
			}
		}()
	}
	_, err := s.OpenPlayback(ctx, fileID, "raw", 0, 0)
	if err == nil {
		t.Fatal("cancelled request must fail")
	}
	if errors.Is(err, context.Canceled) {
		// Expected: fail fast on the caller's cancelled context.
	} else {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if int(succeeded.Load()) > maxTranscodes {
		t.Fatalf("more than %d concurrent transcodes succeeded", maxTranscodes)
	}
}
