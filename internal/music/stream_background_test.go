package music

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

type playbackCallResult struct {
	stream PlaybackStream
	err    error
}

func waitForStreamCondition(t *testing.T, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", description)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func backgroundStartCount(t *testing.T, marker string) int {
	t.Helper()
	data, err := os.ReadFile(marker)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(data)
}

func cacheStagingNames(t *testing.T, mediaDir string, fileID int64) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(mediaDir, streamCacheDirName, strconv.FormatInt(fileID, 10)))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".part" {
			parts = append(parts, entry.Name())
		}
	}
	return parts
}

// A gated background encoder proves that a cold miss returns live audio before
// the cache is complete. Disconnecting that live request must neither cancel
// the fill nor share its source offset; the eventually published bytes are the
// complete source, and the next request is a synchronous regular-file hit.
func TestOpenPlaybackColdMissFirstByteBeforeCacheComplete(t *testing.T) {
	t.Setenv(streamTestFFmpegFileOutEnv, "1")
	marker := filepath.Join(t.TempDir(), "background-started")
	gate := filepath.Join(t.TempDir(), "release-background")
	t.Setenv(streamTestFFmpegStartedEnv, marker)
	t.Setenv(streamTestFFmpegGateEnv, gate)
	helperFFmpeg(t)
	s, _, fileID := transcodeFixture(t)

	calls := make(chan playbackCallResult, 1)
	requestCtx, disconnect := context.WithCancel(context.Background())
	go func() {
		stream, err := s.OpenPlayback(requestCtx, fileID, "opus", 96, 0)
		calls <- playbackCallResult{stream: stream, err: err}
	}()
	var result playbackCallResult
	select {
	case result = <-calls:
	case <-time.After(5 * time.Second):
		t.Fatal("cold-cache OpenPlayback did not return a live stream promptly")
	}
	if result.err != nil {
		t.Fatal(result.err)
	}
	stream := result.stream
	if stream.Body == nil || stream.Mode != "live-miss" {
		t.Fatalf("cold miss = mode %q body %v, want a live pipe", stream.Mode, stream.Body)
	}
	waitForStreamCondition(t, "the gated background encoder", func() bool {
		return backgroundStartCount(t, marker) == 1
	})
	if rows := cacheRowCount(t, s.store.db); rows != 0 {
		t.Fatalf("cache completed before the encoder gate was released: %d rows", rows)
	}

	type readResult struct {
		data []byte
		err  error
	}
	firstByte := make(chan readResult, 1)
	go func() {
		data, err := io.ReadAll(io.LimitReader(stream.Body, 1))
		firstByte <- readResult{data: data, err: err}
	}()
	select {
	case read := <-firstByte:
		if read.err != nil || string(read.data) != "a" {
			t.Fatalf("first live byte = %q, %v", read.data, read.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("live first byte waited for the gated cache fill")
	}
	// One byte is consumed before disconnect. An independently reopened fill
	// must nevertheless publish the entire source, not bytes starting at +1.
	disconnect()
	stream.Close()
	if rows := cacheRowCount(t, s.store.db); rows != 0 {
		t.Fatal("listener disconnect unexpectedly published the gated cache")
	}
	if err := os.WriteFile(gate, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForStreamCondition(t, "detached cache publication", func() bool {
		return cacheRowCount(t, s.store.db) == 1
	})
	if got := backgroundStartCount(t, marker); got != 1 {
		t.Fatalf("background encodes = %d, want one", got)
	}
	if parts := cacheStagingNames(t, s.mediaDir, fileID); len(parts) != 0 {
		t.Fatalf("staging files survived publication: %v", parts)
	}

	cached, err := s.OpenPlayback(context.Background(), fileID, "opus", 96, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cached.Close()
	if cached.File == nil || cached.Body != nil || cached.Mode != "cache-hit" {
		t.Fatalf("published hit = file %v body %v mode %q", cached.File, cached.Body, cached.Mode)
	}
	data, err := io.ReadAll(cached.File)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "audio-bytes-for-conversion" {
		t.Fatalf("cached bytes = %q; live/background source handles were shared", data)
	}
}

// One background fill plus three live pipes saturates all four slots. Further
// cold requests deduplicate the fill, fail honestly at the process bound, and
// closing a live stream releases capacity. Publication still happens once.
func TestOpenPlaybackSharedSlotsDedupAndSinglePublication(t *testing.T) {
	shortGrace(t)
	t.Setenv(streamTestFFmpegFileOutEnv, "1")
	marker := filepath.Join(t.TempDir(), "background-started")
	gate := filepath.Join(t.TempDir(), "release-background")
	t.Setenv(streamTestFFmpegStartedEnv, marker)
	t.Setenv(streamTestFFmpegGateEnv, gate)
	helperFFmpeg(t)
	s, _, fileID := transcodeFixture(t)

	first, err := s.OpenPlayback(context.Background(), fileID, "opus", 96, 0)
	if err != nil {
		t.Fatal(err)
	}
	held := []PlaybackStream{first}
	defer func() {
		for _, stream := range held {
			stream.Close()
		}
	}()
	waitForStreamCondition(t, "the background encoder", func() bool {
		return backgroundStartCount(t, marker) == 1
	})
	for i := 0; i < 2; i++ {
		stream, err := s.OpenPlayback(context.Background(), fileID, "opus", 96, 0)
		if err != nil {
			t.Fatalf("live stream %d: %v", i+2, err)
		}
		held = append(held, stream)
	}
	if _, err := s.OpenPlayback(context.Background(), fileID, "opus", 96, 0); err == nil {
		t.Fatal("fifth simultaneous encoder must fail at the four-slot bound")
	} else {
		var musicErr *Error
		if !errors.As(err, &musicErr) || musicErr.Code != "music_transcode_busy" {
			t.Fatalf("over-limit error = %v, want music_transcode_busy", err)
		}
	}
	if got := backgroundStartCount(t, marker); got != 1 {
		t.Fatalf("deduplicated fill started %d times", got)
	}

	held[0].Close()
	replacement, err := s.OpenPlayback(context.Background(), fileID, "opus", 96, 0)
	if err != nil {
		t.Fatalf("released live slot was not reusable: %v", err)
	}
	held = append(held, replacement)
	for _, stream := range held {
		stream.Close()
	}
	if err := os.WriteFile(gate, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForStreamCondition(t, "single cache publication", func() bool {
		return cacheRowCount(t, s.store.db) == 1
	})
	if rows := cacheRowCount(t, s.store.db); rows != 1 {
		t.Fatalf("published rows = %d, want exactly one", rows)
	}
	if got := backgroundStartCount(t, marker); got != 1 {
		t.Fatalf("background encodes after publication = %d, want one", got)
	}
	if parts := cacheStagingNames(t, s.mediaDir, fileID); len(parts) != 0 {
		t.Fatalf("staging files survived single publication: %v", parts)
	}
}

// Service shutdown cancels a detached encoder even after its listener leaves,
// waits for process/staging cleanup, and prevents subsequent fills.
func TestStreamCacheFillShutdownCleansProcessAndPart(t *testing.T) {
	t.Setenv(streamTestFFmpegFileOutEnv, "1")
	marker := filepath.Join(t.TempDir(), "background-started")
	gate := filepath.Join(t.TempDir(), "never-released")
	t.Setenv(streamTestFFmpegStartedEnv, marker)
	t.Setenv(streamTestFFmpegGateEnv, gate)
	helperFFmpeg(t)
	s, _, fileID := transcodeFixture(t)

	stream, err := s.OpenPlayback(context.Background(), fileID, "opus", 96, 0)
	if err != nil {
		t.Fatal(err)
	}
	waitForStreamCondition(t, "the gated background encoder", func() bool {
		return backgroundStartCount(t, marker) == 1
	})
	stream.Close()
	part := filepath.Join(s.mediaDir, streamCacheDirName, strconv.FormatInt(fileID, 10), "opus-96.part")
	if _, err := os.Stat(part); err != nil {
		t.Fatalf("gated encoder did not create its staging file: %v", err)
	}

	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("service Close did not cancel and reap the background encoder")
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Fatalf("shutdown left a staging file, stat error = %v", err)
	}
	if rows := cacheRowCount(t, s.store.db); rows != 0 {
		t.Fatalf("shutdown published a canceled fill: %d rows", rows)
	}
	if s.startStreamCacheFill(fileID, "opus", 96, "sha", ffmpegPath()) {
		t.Fatal("a closed service accepted another background fill")
	}
}
