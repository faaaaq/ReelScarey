package music

// Integration between OpenPlayback and the persistent stream cache, plus the
// streaming quality mapping the Subsonic and web player endpoints rely on.

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// The cache hit path must serve the cached bytes without spawning ffmpeg:
// even a broken ffmpeg binary must not matter once the conversion is cached.
func TestOpenPlaybackCacheHitAvoidsFFmpeg(t *testing.T) {
	helperFFmpeg(t)
	s, _, fileID := transcodeFixture(t)
	ctx := context.Background()

	var encodes atomic.Int32
	file, info, key, err := s.OpenCachedStream(ctx, fileID, "opus", 96, "sha", func(_ context.Context, dest string) error {
		encodes.Add(1)
		return os.WriteFile(dest, []byte("cached-opus-bytes"), 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	if key != "opus-96" {
		t.Fatalf("profile key = %q, want opus-96", key)
	}
	if info.Size() != int64(len("cached-opus-bytes")) {
		t.Fatalf("cached size = %d, want %d", info.Size(), len("cached-opus-bytes"))
	}
	_ = file.Close()

	// A broken ffmpeg cannot be spawned, so a hit that tried to convert would
	// fail; success here proves the cache answered the request.
	brokenFFmpeg(t)
	stream, err := s.OpenPlayback(ctx, fileID, "opus", 96, 0)
	if err != nil {
		t.Fatalf("cached playback: %v", err)
	}
	defer stream.Close()
	if stream.Body != nil || stream.File == nil {
		t.Fatalf("cache hit must serve a file, got body=%v file=%v", stream.Body, stream.File)
	}
	if stream.Name != "01 Song.ogg" {
		t.Fatalf("cached name = %q, want 01 Song.ogg", stream.Name)
	}
	if stream.ContentType != transcodeContentType("opus") {
		t.Fatalf("cached content type = %q", stream.ContentType)
	}
	data, err := io.ReadAll(stream.File)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "cached-opus-bytes" {
		t.Fatalf("cached bytes = %q", data)
	}
	if encodes.Load() != 1 {
		t.Fatalf("cache hit must not re-encode, ran %d encodes", encodes.Load())
	}
	stats, err := s.StreamCacheStats(ctx)
	if err != nil || stats.Entries != 1 || stats.Hits < 1 {
		t.Fatalf("cache stats after hit: %+v (%v)", stats, err)
	}
}

// Offset seeks are never answered from the cache: they always use the
// on-the-fly pipe with output seeking, and a populated cache must not change
// that.
func TestOpenPlaybackOffsetBypassesCache(t *testing.T) {
	helperFFmpeg(t)
	s, _, fileID := transcodeFixture(t)
	ctx := context.Background()

	// Populate the cache first so a wrong cache answer is possible.
	file, _, _, err := s.OpenCachedStream(ctx, fileID, "opus", 96, "sha", func(_ context.Context, dest string) error {
		return os.WriteFile(dest, []byte("cached-opus-bytes"), 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()

	stream, err := s.OpenPlayback(ctx, fileID, "opus", 96, 30)
	if err != nil {
		t.Fatalf("offset playback: %v", err)
	}
	defer stream.Close()
	if stream.Body == nil {
		t.Fatal("an offset request must use the on-the-fly pipe")
	}
	data, err := io.ReadAll(stream.Body)
	if err != nil {
		t.Fatal(err)
	}
	// The helper "ffmpeg" echoes the source bytes it receives on stdin.
	if string(data) != "audio-bytes-for-conversion" {
		t.Fatalf("offset pipe bytes = %q", data)
	}
}

// A failing cache encode must degrade to the on-the-fly pipe, never fail the
// stream: the fake ffmpeg here writes nothing to the .part destination, so
// OpenCachedStream reports a failed encode and OpenPlayback falls back.
func TestOpenPlaybackCacheEncodeFailureFallsBackToPipe(t *testing.T) {
	helperFFmpeg(t)
	s, _, fileID := transcodeFixture(t)

	stream, err := s.OpenPlayback(context.Background(), fileID, "opus", 96, 0)
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
		t.Fatalf("pipe fallback bytes = %q", data)
	}
	stats, err := s.StreamCacheStats(context.Background())
	if err != nil || stats.Entries != 0 {
		t.Fatalf("a failed encode must not leave a cache entry: %+v (%v)", stats, err)
	}
}

// An unusable cache directory (here: the cache dir path replaced by a regular
// file) must also degrade to the on-the-fly pipe.
func TestOpenPlaybackBrokenCacheDirFallsBackToPipe(t *testing.T) {
	helperFFmpeg(t)
	s, _, fileID := transcodeFixture(t)
	if err := os.WriteFile(streamCacheDir(s.mediaDir), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	stream, err := s.OpenPlayback(context.Background(), fileID, "opus", 96, 0)
	if err != nil {
		t.Fatalf("playback with a broken cache dir: %v", err)
	}
	defer stream.Close()
	if stream.Body == nil {
		t.Fatal("a broken cache dir must fall back to the on-the-fly pipe")
	}
	data, err := io.ReadAll(stream.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "audio-bytes-for-conversion" {
		t.Fatalf("pipe fallback bytes = %q", data)
	}
}

// The source digest is what makes a cache entry trustworthy: a changed
// source_sha256 must invalidate the entry instead of serving stale bytes.
func TestOpenPlaybackCacheInvalidatesOnSourceChange(t *testing.T) {
	helperFFmpeg(t)
	s, _, fileID := transcodeFixture(t)
	ctx := context.Background()

	file, _, _, err := s.OpenCachedStream(ctx, fileID, "opus", 96, "old-sha", func(_ context.Context, dest string) error {
		return os.WriteFile(dest, []byte("stale-bytes"), 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()

	// The registered row carries sha256="sha" (transcodeFixture), so the
	// playback request must treat the "old-sha" entry as stale. The stale
	// bytes must never be served: the entry is dropped and the stream falls
	// back to on-the-fly piping (the fake ffmpeg cannot publish a fresh
	// cache file here).
	stream, err := s.OpenPlayback(ctx, fileID, "opus", 96, 0)
	if err != nil {
		t.Fatalf("playback after source change: %v", err)
	}
	defer stream.Close()
	if stream.Body == nil {
		t.Fatal("a stale cache entry must be dropped, not served")
	}
	data, err := io.ReadAll(stream.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "audio-bytes-for-conversion" {
		t.Fatalf("fallback bytes = %q (the stale entry must not be served)", data)
	}
	var entries int64
	if err := s.store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM music_stream_cache WHERE file_id=?`, fileID).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if entries != 0 {
		t.Fatalf("the stale cache row must be gone, %d remain", entries)
	}
}

// Streaming quality mapping: quality names resolve to (format, maxBitRate).
func TestStreamingQualityParams(t *testing.T) {
	cases := []struct {
		quality  string
		wantName string
		wantRate int
	}{
		{"data-saver", "opus", 64},
		{"balanced", "opus", 96},
		{"high", "opus", 128},
		{"premium", "opus", 160},
		{"auto", "opus", 96},
		{"original", "", 0},
		{"", "opus", 96},
		{"mystery", "opus", 96},
	}
	for _, tc := range cases {
		format, rate := StreamingQualityParams(tc.quality)
		if format != tc.wantName || rate != tc.wantRate {
			t.Fatalf("StreamingQualityParams(%q) = (%q, %d), want (%q, %d)", tc.quality, format, rate, tc.wantName, tc.wantRate)
		}
	}
}

// Effective quality resolution: user > global > built-in default.
func TestResolveStreamQuality(t *testing.T) {
	cases := []struct{ user, global, want string }{
		{"premium", "balanced", "premium"},
		{"", "high", "high"},
		{"", "", DefaultStreamingQuality},
		{"  data-saver  ", "", "data-saver"},
	}
	for _, tc := range cases {
		if got := ResolveStreamQuality(tc.user, tc.global); got != tc.want {
			t.Fatalf("ResolveStreamQuality(%q, %q) = %q, want %q", tc.user, tc.global, got, tc.want)
		}
	}
}

// Per-server-account stream_quality/download_profile: create, partial edit
// keeps, empty clears, invalid values fail closed.
func TestMusicServerUserStreamQualityPersistence(t *testing.T) {
	ctx := context.Background()
	service := newMusicServerUserService(t)
	enableMusicServer(t, service)

	quality := "premium"
	profile := "opus-balanced"
	user, _, err := service.CreateMusicServerUser(ctx, MusicServerUserInput{
		Username:        "Quality",
		StreamQuality:   &quality,
		DownloadProfile: &profile,
	})
	if err != nil {
		t.Fatal(err)
	}
	if user.StreamQuality != "premium" || user.DownloadProfile != "opus-balanced" {
		t.Fatalf("create should persist the quality fields: %+v", user)
	}

	// An omitted field keeps its stored value.
	kept, err := service.UpdateMusicServerUser(ctx, user.ID, MusicServerUserInput{Username: "Quality"})
	if err != nil {
		t.Fatal(err)
	}
	if kept.StreamQuality != "premium" || kept.DownloadProfile != "opus-balanced" {
		t.Fatalf("an omitted quality field must keep its value: %+v", kept)
	}

	// An explicit empty string clears the override (follow global again);
	// a supplied value replaces the stored one.
	empty := ""
	other := "mp3-compatible"
	cleared, err := service.UpdateMusicServerUser(ctx, user.ID, MusicServerUserInput{
		Username:        "Quality",
		StreamQuality:   &empty,
		DownloadProfile: &other,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.StreamQuality != "" || cleared.DownloadProfile != "mp3-compatible" {
		t.Fatalf("an empty stream_quality must clear the override: %+v", cleared)
	}

	// The default account follows global.
	fresh, _, err := service.CreateMusicServerUser(ctx, MusicServerUserInput{Username: "Default"})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.StreamQuality != "" || fresh.DownloadProfile != "" {
		t.Fatalf("new accounts must follow global by default: %+v", fresh)
	}

	// Invalid values fail closed.
	bad := "ultra"
	badProfile := "does-not-exist"
	if _, err := service.UpdateMusicServerUser(ctx, user.ID, MusicServerUserInput{Username: "Quality", StreamQuality: &bad}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("an unknown stream quality must be rejected: %v", err)
	}
	if _, err := service.UpdateMusicServerUser(ctx, user.ID, MusicServerUserInput{Username: "Quality", DownloadProfile: &badProfile}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("an unknown download profile must be rejected: %v", err)
	}
	if _, _, err := service.CreateMusicServerUser(ctx, MusicServerUserInput{Username: "Bad", StreamQuality: &bad}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("an unknown stream quality must be rejected on create: %v", err)
	}
}

// A user quality override must reach the conversion planner through the
// effective resolution order (user > global) once the endpoints resolve it.
func TestStreamQualityEndToEndPlanning(t *testing.T) {
	withFFmpeg(t)
	// premium (opus 160) on a flac file must plan an opus-160 conversion.
	format, rate := StreamingQualityParams(ResolveStreamQuality("premium", "balanced"))
	codec, planned := planTranscode("flac", format, rate)
	if codec != "opus" || planned != 160 {
		t.Fatalf("premium flac plan = (%q, %d), want (opus, 160)", codec, planned)
	}
	// original on the same flac must stay byte-for-byte.
	format, rate = StreamingQualityParams(ResolveStreamQuality("", "original"))
	codec, planned = planTranscode("flac", format, rate)
	if codec != "" {
		t.Fatalf("original flac plan = (%q, %d), want untouched", codec, planned)
	}
	// The compatibility fallback survives the quality default: an
	// unplayable wma is converted even with "original".
	format, rate = StreamingQualityParams(ResolveStreamQuality("", "original"))
	codec, planned = planTranscode("wma", format, rate)
	if codec != "mp3" || planned != 192 {
		t.Fatalf("original wma plan = (%q, %d), want the mp3 compat fallback", codec, planned)
	}
}

// transcodeFileArgs must share the pipe variant's conversion options and end
// with the destination path, never a registered source path.
func TestTranscodeFileArgs(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "opus-96.part")
	args := strings.Join(transcodeFileArgs("opus", 96, 0, dest), " ")
	for _, want := range []string{"-y", "-i pipe:0", "-vn", "-c:a libopus", "-b:a 96k", "-f ogg", dest} {
		if !strings.Contains(args, want) {
			t.Fatalf("file args missing %q: %s", want, args)
		}
	}
	if !strings.HasSuffix(args, dest) {
		t.Fatalf("file args must end with the dest path: %s", args)
	}
	if strings.Contains(args, "pipe:1") {
		t.Fatalf("file args must not pipe to stdout: %s", args)
	}
}
