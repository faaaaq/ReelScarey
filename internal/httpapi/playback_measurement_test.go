package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/music"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

// TestPlaybackLatencyThroughReverseProxy is an independent server-side
// measurement, not a device benchmark. It uses generated audio and an actual
// reverse proxy; timings are logged with the host and cache state, never used
// as fragile wall-clock pass/fail budgets.
func TestPlaybackLatencyThroughReverseProxy(t *testing.T) {
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " is not installed; device-independent latency check unavailable")
		}
	}
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	media := t.TempDir()
	service := music.NewService(db, media)
	defer func() { _ = service.Close() }()
	secrets, err := securestore.New(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	service.SetSecrets(secrets)
	if _, err = service.SaveMusicServer(ctx, true, 1); err != nil {
		t.Fatal(err)
	}
	if _, _, err = service.CreateMusicServerUser(ctx, music.MusicServerUserInput{Username: "measure", Password: "measure-pass"}); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(media, "fixture.mp3")
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=4", "-c:a", "libmp3lame", "-b:a", "128k", "-y", source)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("could not generate audio fixture: %v (%s)", err, output)
	}
	if _, err = db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES(1,'artist-local','Artist',1,'available',1,1);
		INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(2,1,'album-local','Album',1,'available',1,1);
		INSERT INTO music_tracks(id,album_id,musicbrainz_id,recording_id,disc,number,title,duration_ms,created_at,updated_at) VALUES(3,2,'track-local','recording-local',1,1,'Song',4000,1,1);
		INSERT INTO music_files(id,artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,duration_ms,tag_json,imported_at,origin,source_name) VALUES(1,1,2,3,'Music','fixture.mp3',1,'fixture-sha','mp3',4000,'{}',1,'existing','fixture.mp3')`); err != nil {
		t.Fatal(err)
	}
	handler, session := musicFixture(t, service)
	backend := httptest.NewServer(handler)
	defer backend.Close()
	backendURL, _ := url.Parse(backend.URL)
	proxy := httptest.NewServer(httputil.NewSingleHostReverseProxy(backendURL))
	defer proxy.Close()
	client := proxy.Client()
	client.Timeout = 90 * time.Second

	type measurement struct {
		path     string
		mode     string
		openMS   float64
		status   int
		body     []byte
		response *http.Response
	}
	measure := func(name, rawURL string, cookie bool, readAll bool) measurement {
		t.Helper()
		req, err := http.NewRequest("GET", proxy.URL+rawURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		if cookie {
			req.AddCookie(&http.Cookie{Name: cookieName, Value: session.Token})
		}
		started := time.Now()
		response, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		defer response.Body.Close()
		// Always time the first audio bytes, including requests whose complete
		// body is needed afterwards for FFprobe. Reading to EOF before recording
		// elapsed time would report listening/download time, not startup latency.
		first := make([]byte, 32)
		n, readErr := io.ReadFull(response.Body, first)
		elapsed := float64(time.Since(started).Microseconds()) / 1000
		if readErr != nil && readErr != io.ErrUnexpectedEOF {
			t.Fatalf("%s first audio: %v", name, readErr)
		}
		body := first[:n]
		if readAll {
			rest, restErr := io.ReadAll(response.Body)
			if restErr != nil {
				t.Fatalf("%s remaining audio: %v", name, restErr)
			}
			body = append(body, rest...)
		}
		return measurement{path: rawURL, mode: response.Header.Get("X-ReelScarey-Playback-Path"), openMS: elapsed, status: response.StatusCode, body: body, response: response}
	}
	setQuality := func(value string) {
		t.Helper()
		prefs, err := service.GetPreferences(ctx)
		if err != nil {
			t.Fatal(err)
		}
		prefs.StreamingQuality = value
		if _, err = service.SavePreferences(ctx, prefs); err != nil {
			t.Fatal(err)
		}
	}

	setQuality("original")
	direct := measure("direct", "/api/v1/library/music/files/1/stream", true, false)
	if direct.status != 200 || direct.mode != "direct" || len(direct.body) == 0 {
		t.Fatalf("direct playback: %#v", direct)
	}
	setQuality("data-saver")
	fresh := measure("fresh transcode", "/api/v1/library/music/files/1/stream", true, false)
	if fresh.status != 200 || fresh.mode != "live-miss" || len(fresh.body) == 0 {
		t.Fatalf("fresh transcode: mode=%q status=%d body=%d headers=%v", fresh.mode, fresh.status, len(fresh.body), fresh.response.Header)
	}
	// The listener has already received audio while the detached fill finishes.
	// Wait for publication instead of issuing a racing second request and
	// mistaking another legitimate live miss for a cache regression.
	deadline := time.Now().Add(20 * time.Second)
	for {
		stats, statsErr := service.StreamCacheStats(ctx)
		if statsErr != nil {
			t.Fatal(statsErr)
		}
		if stats.Entries == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("detached cache fill did not publish")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cached := measure("cached transcode", "/api/v1/library/music/files/1/stream", true, false)
	if cached.status != 200 || cached.mode != "cache-hit" || len(cached.body) == 0 {
		t.Fatalf("cached transcode: mode=%q status=%d headers=%v", cached.mode, cached.status, cached.response.Header)
	}
	seek := measure("active offset", "/rest/stream.view?u=measure&p=measure-pass&v=1.16.1&c=measure&id=tr1&format=mp3&timeOffset=1", false, true)
	if seek.status != 200 || seek.mode != "offset-live" || len(seek.body) == 0 {
		t.Fatalf("offset seek: mode=%q status=%d headers=%v", seek.mode, seek.status, seek.response.Header)
	}
	offsetFile := filepath.Join(t.TempDir(), "offset.mp3")
	if err = os.WriteFile(offsetFile, seek.body, 0600); err != nil {
		t.Fatal(err)
	}
	tags, err := (music.FFprobeTagger{}).ProbeAudio(ctx, offsetFile)
	if err != nil {
		t.Fatalf("offset output is not decodable audio: %v", err)
	}
	if tags.DurationMS < 2000 || tags.DurationMS > 3400 {
		t.Fatalf("offset did not seek within expected tolerance: %dms", tags.DurationMS)
	}
	t.Logf("playback measurement: fixture=generated 4s 128kbps MP3 sine; host=%s/%s cpus=%d; proxy=httputil.NewSingleHostReverseProxy; cache=cold then warm; direct first-audio=%.2fms mode=%s; fresh-cache first-audio=%.2fms mode=%s; warm-cache first-audio=%.2fms mode=%s; active-seek first-audio=%.2fms mode=%s remaining=%dms; real iOS/Android clients not available",
		runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), direct.openMS, direct.mode, fresh.openMS, fresh.mode, cached.openMS, cached.mode, seek.openMS, seek.mode, tags.DurationMS)
	for name, result := range map[string]measurement{"direct": direct, "fresh": fresh, "cached": cached, "seek": seek} {
		if result.response.Header.Get("X-ReelScarey-Delivered-Format") == "" || result.response.Header.Get("Server-Timing") == "" {
			t.Fatalf("%s diagnostics missing: %v", name, result.response.Header)
		}
	}
}
