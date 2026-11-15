package httpapi

// Independent production-readiness audit (streaming/client-compat focus):
// regression tests for byte-range serving on file-backed streams, the honest
// no-range contract of on-the-fly conversions, and the fail-safe handling of
// invalid stored per-user streaming quality values.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/music"
)

// writeSongFixture writes the 10-byte song body the fakes stream.
func writeSongFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// streamRequest issues an authenticated Subsonic stream request with extra
// headers against a test fixture.
func streamRequest(t *testing.T, service MusicService, query string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	handler, _ := musicFixture(t, service)
	r := httptest.NewRequest("GET", "/rest/stream.view?u=reelscarey&p=test-token&v=1.16.1&c=test&id=1"+query, nil)
	for key, value := range headers {
		r.Header.Set(key, value)
	}
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, r)
	return out
}

// webStreamRequest issues an authenticated browser-player stream request with
// extra headers.
func webStreamRequest(t *testing.T, service MusicService, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	handler, s := musicFixture(t, service)
	r := httptest.NewRequest("GET", "/api/v1/library/music/files/1/stream", nil)
	for key, value := range headers {
		r.Header.Set(key, value)
	}
	r.AddCookie(&http.Cookie{Name: cookieName, Value: s.Token})
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, r)
	return out
}

// A file-backed stream (direct file or cache hit) must keep byte-range
// seeking: the Subsonic handler must not break http.ServeContent's 206
// handling for clients that seek.
func TestSubsonicStreamRangeOnStreamableFile(t *testing.T) {
	service := &recordingMusic{
		fakeMusic: &fakeMusic{serverTokenOK: true, audioPath: writeSongFixture(t)},
	}
	out := streamRequest(t, service, "", map[string]string{"Range": "bytes=2-5"})
	if out.Code != 206 {
		t.Fatalf("range stream: %d %s", out.Code, out.Body.String())
	}
	if got := out.Body.String(); got != "2345" {
		t.Fatalf("range body = %q, want 2345", got)
	}
	if out.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("Accept-Ranges = %q, want bytes", out.Header().Get("Accept-Ranges"))
	}
	if !strings.Contains(out.Header().Get("Content-Range"), "bytes 2-5/10") {
		t.Fatalf("Content-Range = %q", out.Header().Get("Content-Range"))
	}
}

// An on-the-fly conversion has no known length: the response must honestly
// advertise that ranges are not supported instead of letting a client seek
// into a live pipe.
func TestSubsonicStreamLiveTranscodeHasNoRanges(t *testing.T) {
	service := &bodyPlaybackMusic{recordingMusic: &recordingMusic{
		fakeMusic: &fakeMusic{serverTokenOK: true, audioPath: writeSongFixture(t)},
	}}
	out := streamRequest(t, service, "&format=mp3", map[string]string{"Range": "bytes=2-5"})
	if out.Code != 200 {
		t.Fatalf("live transcode: %d %s", out.Code, out.Body.String())
	}
	if out.Header().Get("Accept-Ranges") != "none" {
		t.Fatalf("Accept-Ranges = %q, want none", out.Header().Get("Accept-Ranges"))
	}
	if out.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", out.Header().Get("Cache-Control"))
	}
	if !strings.Contains(out.Body.String(), "converted-bytes") {
		t.Fatalf("body = %q", out.Body.String())
	}
}

// The browser player path keeps byte-range seeking on file-backed streams,
// which is what makes the UI seek bar work without downloading the track.
func TestWebPlayerStreamRangeOnStreamableFile(t *testing.T) {
	service := &recordingMusic{
		fakeMusic: &fakeMusic{audioPath: writeSongFixture(t)},
		prefs:     music.MusicPreferences{StreamingQuality: "original"},
	}
	out := webStreamRequest(t, service, map[string]string{"Range": "bytes=2-5"})
	if out.Code != 206 || out.Body.String() != "2345" {
		t.Fatalf("web player range: %d %s", out.Code, out.Body.String())
	}
	if out.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("Accept-Ranges = %q, want bytes", out.Header().Get("Accept-Ranges"))
	}
}

// A hand-edited (invalid) stored per-user stream quality must fail safe: the
// stream still works with the built-in default conversion target instead of
// erroring or planning an unknown codec.
func TestSubsonicStreamInvalidStoredUserQualityFailsSafe(t *testing.T) {
	user := music.MusicServerUser{ID: 3, Username: "kid", DownloadRole: true, StreamQuality: "hand-edited-garbage"}
	service := &recordingMusic{
		fakeMusic: &fakeMusic{serverTokenOK: true, serverUser: user},
		prefs:     music.MusicPreferences{StreamingQuality: "premium"},
	}
	out := streamRequest(t, service, "", nil)
	if out.Code != 200 {
		t.Fatalf("stream with an invalid stored quality: %d %s", out.Code, out.Body.String())
	}
	if service.gotFormat != "opus" || service.gotRate != 96 {
		t.Fatalf("invalid stored quality must fail safe to opus/96, got format=%q rate=%d", service.gotFormat, service.gotRate)
	}
}

// bodyPlaybackMusic makes OpenPlayback return an on-the-fly conversion body
// so the live-pipe response path can be exercised without ffmpeg.
type bodyPlaybackMusic struct {
	*recordingMusic
}

func (b *bodyPlaybackMusic) OpenPlayback(context.Context, int64, string, int, int) (music.PlaybackStream, error) {
	return music.PlaybackStream{
		Name:        "song.mp3",
		ContentType: "audio/mpeg",
		Body:        io.NopCloser(strings.NewReader("converted-bytes")),
		Codec:       "mp3",
	}, nil
}
