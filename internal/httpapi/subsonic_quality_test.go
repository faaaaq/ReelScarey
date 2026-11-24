package httpapi

// Effective Subsonic stream quality: an explicit client format/maxBitRate
// parameter always wins; otherwise the per-server-account stream quality
// ('' = follow global) then the global streaming quality preference pick the
// default conversion target the music service receives.

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/music"
)

// recordingMusic wraps the shared fakeMusic with the preference and playback
// overrides the quality tests need, recording what subsonicStream passes to
// OpenPlayback. Everything else is promoted from fakeMusic, including the
// opt-in token authentication.
type recordingMusic struct {
	*fakeMusic
	prefs      music.MusicPreferences
	gotFormat  string
	gotRate    int
	gotOffset  int
	gotFormats int
}

func (r *recordingMusic) GetPreferences(context.Context) (music.MusicPreferences, error) {
	return r.prefs, nil
}

func (r *recordingMusic) OpenPlayback(ctx context.Context, id int64, format string, rate, offset int) (music.PlaybackStream, error) {
	r.gotFormats++
	r.gotFormat, r.gotRate, r.gotOffset = format, rate, offset
	return r.fakeMusic.OpenPlayback(ctx, id, format, rate, offset)
}

func qualityRequest(t *testing.T, service *recordingMusic, query string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	service.audioPath = path
	handler, _ := musicFixture(t, service)
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, httptest.NewRequest("GET", "/rest/stream.view?u=reelscarey&p=test-token&v=1.16.1&c=test&id=1"+query, nil))
	if out.Code != 200 || out.Body.String() != "0123456789" {
		t.Fatalf("stream: %d %s", out.Code, out.Body.String())
	}
}

func TestSubsonicStreamExplicitParamsWin(t *testing.T) {
	service := &recordingMusic{fakeMusic: &fakeMusic{serverTokenOK: true}, prefs: music.MusicPreferences{StreamingQuality: "premium"}}
	qualityRequest(t, service, "&format=mp3&maxBitRate=320")
	if service.gotFormat != "mp3" || service.gotRate != 320 || service.gotOffset != 0 {
		t.Fatalf("explicit client params must pass through unchanged, got format=%q rate=%d offset=%d", service.gotFormat, service.gotRate, service.gotOffset)
	}
}

func TestSubsonicStreamUserQualityBeatsGlobal(t *testing.T) {
	// Stream playback requires the download role for a named account.
	user := music.MusicServerUser{ID: 3, Username: "kid", DownloadRole: true, StreamQuality: "premium"}
	service := &recordingMusic{fakeMusic: &fakeMusic{serverTokenOK: true, serverUser: user}, prefs: music.MusicPreferences{StreamingQuality: "balanced"}}
	qualityRequest(t, service, "")
	if service.gotFormat != "opus" || service.gotRate != 160 {
		t.Fatalf("per-user stream_quality must beat the global preference, got format=%q rate=%d", service.gotFormat, service.gotRate)
	}
}

func TestSubsonicStreamGlobalQualityFallback(t *testing.T) {
	user := music.MusicServerUser{ID: 3, Username: "kid", DownloadRole: true}
	service := &recordingMusic{fakeMusic: &fakeMusic{serverTokenOK: true, serverUser: user}, prefs: music.MusicPreferences{StreamingQuality: "high"}}
	qualityRequest(t, service, "")
	if service.gotFormat != "opus" || service.gotRate != 128 {
		t.Fatalf("an empty per-user quality must follow global, got format=%q rate=%d", service.gotFormat, service.gotRate)
	}
}

func TestSubsonicStreamDefaultQuality(t *testing.T) {
	user := music.MusicServerUser{ID: 3, Username: "kid", DownloadRole: true}
	service := &recordingMusic{fakeMusic: &fakeMusic{serverTokenOK: true, serverUser: user}}
	qualityRequest(t, service, "")
	if service.gotFormat != "opus" || service.gotRate != 96 {
		t.Fatalf("missing preferences must fall back to the built-in default, got format=%q rate=%d", service.gotFormat, service.gotRate)
	}
}

func TestSubsonicStreamExplicitMaxBitRateOnlyIsUntouched(t *testing.T) {
	service := &recordingMusic{fakeMusic: &fakeMusic{serverTokenOK: true}, prefs: music.MusicPreferences{StreamingQuality: "premium"}}
	qualityRequest(t, service, "&maxBitRate=64")
	if service.gotFormat != "" || service.gotRate != 64 {
		t.Fatalf("an explicit maxBitRate without format must pass through, got format=%q rate=%d", service.gotFormat, service.gotRate)
	}
}

// The web player path applies the global streaming quality as its default.
// Original also reaches the common planner with an empty format/rate, which
// serves playable sources byte-for-byte while retaining compatibility fallback.
func TestWebPlayerStreamQuality(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	converted := &recordingMusic{fakeMusic: &fakeMusic{audioPath: path}, prefs: music.MusicPreferences{StreamingQuality: "balanced"}}
	handler, session := musicFixture(t, converted)
	if out := request(handler, "GET", "/api/v1/library/music/files/1/stream", "", session, false); out.Code != 200 {
		t.Fatal("stream", out.Code, out.Body.String())
	}
	if converted.gotFormat != "opus" || converted.gotRate != 96 {
		t.Fatalf("web player must default to the global streaming quality, got format=%q rate=%d", converted.gotFormat, converted.gotRate)
	}

	original := &recordingMusic{fakeMusic: &fakeMusic{audioPath: path}, prefs: music.MusicPreferences{StreamingQuality: "original"}}
	handler, session = musicFixture(t, original)
	if out := request(handler, "GET", "/api/v1/library/music/files/1/stream", "", session, false); out.Code != 200 {
		t.Fatal("stream", out.Code, out.Body.String())
	}
	if original.gotFormats != 1 || original.gotFormat != "" || original.gotRate != 0 {
		t.Fatalf("original quality must use the direct planner without a conversion target: calls=%d format=%q rate=%d", original.gotFormats, original.gotFormat, original.gotRate)
	}
}

func TestWebPlayerStreamInfoDescribesTheDeliveredAudio(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	musicService := &recordingMusic{
		fakeMusic: &fakeMusic{audioPath: path},
		prefs:     music.MusicPreferences{StreamingQuality: "high"},
	}
	handler, session := musicFixture(t, musicService)
	const query = "?stream_id=0123456789abcdef0123456789abcdef"
	const track = "/api/v1/library/music/files/1"
	if out := request(handler, "GET", track+"/stream-info"+query, "", session, false); out.Code != 404 {
		t.Fatalf("unopened stream: %d %s", out.Code, out.Body.String())
	}
	if out := request(handler, "GET", track+"/stream"+query+"&compat=mp3", "", session, false); out.Code != 200 {
		t.Fatalf("open: %d %s", out.Code, out.Body.String())
	}
	if musicService.gotFormat != "mp3" || musicService.gotRate != 128 {
		t.Fatalf("compatibility should use the saved bitrate target: %q %d", musicService.gotFormat, musicService.gotRate)
	}
	out := request(handler, "GET", track+"/stream-info"+query, "", session, false)
	var info browserMusicStreamInfo
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &info) != nil {
		t.Fatalf("stream info: %d %s", out.Code, out.Body.String())
	}
	if info.Codec != "mp3" || info.Mode != "direct" || !info.RangeSeekable || info.BitrateKbps != 0 {
		t.Fatalf("the fake provided direct audio rather than an encoder target: %+v", info)
	}
	if musicService.gotFormats != 1 {
		t.Fatalf("reading stream details reopened the audio: %d opens", musicService.gotFormats)
	}
	if out.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("transient stream details must not be cached")
	}
	if other := request(handler, "GET", "/api/v1/library/music/files/2/stream-info"+query, "", session, false); other.Code != 404 {
		t.Fatalf("stream info must be scoped to the track ID: %d %s", other.Code, other.Body.String())
	}
}

func TestWebPlayerExplicitRateAndOffsetReachPlanner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, query, format string
		rate, offset        int
	}{
		{name: "rate only cap", query: "?maxBitRate=16", rate: 16},
		{name: "original quality active seek", query: "?timeOffset=42", offset: 42},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &recordingMusic{fakeMusic: &fakeMusic{audioPath: path}, prefs: music.MusicPreferences{StreamingQuality: "original"}}
			handler, session := musicFixture(t, service)
			if out := request(handler, "GET", "/api/v1/library/music/files/1/stream"+tc.query, "", session, false); out.Code != 200 {
				t.Fatal("stream", out.Code, out.Body.String())
			}
			if service.gotFormat != tc.format || service.gotRate != tc.rate || service.gotOffset != tc.offset {
				t.Fatalf("planner received format=%q rate=%d offset=%d", service.gotFormat, service.gotRate, service.gotOffset)
			}
		})
	}
}
