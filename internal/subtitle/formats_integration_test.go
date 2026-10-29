package subtitle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/transfer"
)

// Real generated video, FFprobe, provider HTTP serialization and journaled
// imports. The remote provider/CDN are owned protocol fixtures, never accounts.
func TestRealSubtitleOriginalFormats(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_SUBTITLES") != "1" {
		t.Skip("set REELSCAREY_LIVE_SUBTITLES=1 for generated format-preserving imports")
	}
	for _, format := range []string{"srt", "ass", "ssa", "vtt"} {
		t.Run(format, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			if b, e := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=10", "-t", "8", "-c:v", "libx264", "-y", f.video).CombinedOutput(); e != nil {
				t.Fatal(e, string(b))
			}
			video, e := os.ReadFile(f.video)
			if e != nil {
				t.Fatal(e)
			}
			sum := sha256.Sum256(video)
			if _, e = f.db.Exec(`UPDATE media_files SET size_bytes=?,sha256=?,probe_json='{}' WHERE id=1`, len(video), hex.EncodeToString(sum[:])); e != nil {
				t.Fatal(e)
			}
			body := validASS
			if format == "srt" {
				body = validSRT
			}
			if format == "ssa" {
				body = validSSA
			}
			if format == "vtt" {
				body = validVTT
			}
			downloads := 0
			provider := protocol(t, func(r *http.Request) *http.Response {
				switch r.URL.Path {
				case "/api/v1/login":
					return response(200, `{"token":"owned-token","base_url":"api.opensubtitles.com"}`)
				case "/api/v1/subtitles":
					if r.URL.Query().Get("moviehash") == "" || r.URL.Query().Get("tmdb_id") != "123" {
						t.Fatal("missing canonical/hash evidence")
					}
					return response(200, fmt.Sprintf(`{"data":[{"id":"owned","attributes":{"language":"en","release":"Test.Movie.2020.1080p.WEB","moviehash_match":true,"feature_details":{"feature_type":"Movie","tmdb_id":123},"files":[{"file_id":45,"file_name":"Owned.en.%s"}]}}]}`, format))
				case "/api/v1/download":
					var in map[string]any
					json.NewDecoder(r.Body).Decode(&in)
					if len(in) != 1 || in["file_id"] != float64(45) {
						t.Fatal("styled conversion requested", in)
					}
					downloads++
					return response(200, `{"link":"https://dl.opensubtitles.com/owned","remaining":4}`)
				case "/owned":
					if r.URL.Host != "dl.opensubtitles.com" || r.Header.Get("Authorization") != "" || r.Header.Get("Api-Key") != "" {
						t.Fatal("credentials escaped API host")
					}
					return response(200, body)
				default:
					t.Fatal("unexpected provider request", r.URL)
					return nil
				}
			})
			f.s.provider = provider
			f.s.Prober = transfer.FFprobe{}
			enable(t, f, true, Track{"en", "full"})
			prefs, _ := f.s.Preferences(ctx)
			prefs.DownloadFormat = "original"
			if _, e = f.s.SavePreferences(ctx, prefs); e != nil {
				t.Fatal(e)
			}
			process(t, f.s)
			view, e := f.s.File(ctx, 1, true)
			if e != nil || view.Job.State != "completed" || len(view.Tracks) != 1 || downloads != 1 {
				t.Fatal(view, downloads, e)
			}
			path := strings.TrimSuffix(f.video, ".mkv") + ".en." + format
			actual, e := os.ReadFile(path)
			if e != nil || string(actual) != body {
				t.Fatal("provider styles changed", e)
			}
			if b, e := exec.Command("ffprobe", "-v", "error", "-show_entries", "stream=codec_name", "-of", "json", path).CombinedOutput(); e != nil || (!bytes.Contains(b, []byte(`"ass"`)) && !bytes.Contains(b, []byte(`"webvtt"`)) && !bytes.Contains(b, []byte(`"subrip"`))) {
				t.Fatal("real subtitle parser rejected output", e, string(b))
			}
			after, e := os.ReadFile(f.video)
			if e != nil || !bytes.Equal(after, video) {
				t.Fatal("generated video changed", e)
			}
			reopen(t, &f)
			f.s.provider = provider
			f.s.Prober = transfer.FFprobe{}
			if _, e = f.db.Exec(`UPDATE subtitle_jobs SET next_run=0`); e != nil {
				t.Fatal(e)
			}
			process(t, f.s)
			if downloads != 1 {
				t.Fatal("existing styled track downloaded again")
			}
			t.Log("Original format, styles, FFprobe readability, quota, source bytes and reopen verified")
		})
	}
}
