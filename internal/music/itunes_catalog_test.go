package music

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestITunesDiscoveryDuringMusicBrainzCooldown(t *testing.T) {
	ctx := context.Background()
	for _, scenario := range []string{"match all", "musicbrainz miss", "incomplete folder", "ambiguous edition"} {
		t.Run(scenario, func(t *testing.T) {
			brainz := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if scenario != "musicbrainz miss" {
					t.Errorf("MusicBrainz called during cooldown: %s", r.URL)
				}
				if r.URL.Path == "/ws/2/artist" {
					writeDiscoveryJSON(w, `{"count":0,"artists":[]}`)
				} else {
					writeDiscoveryJSON(w, `{"count":0,"release-groups":[]}`)
				}
			})
			server := httptest.NewServer(brainz)
			defer server.Close()
			service, db, scanID := discoveryFixture(t, server)
			service.client.http.Transport = correctionFixtureTransport{brainz}
			if scenario != "musicbrainz miss" {
				if err := service.brainz.SetCooldown(ctx, time.Now().Add(time.Hour), ""); err != nil {
					t.Fatal(err)
				}
			}
			apple := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Host != "itunes.apple.com" || r.URL.Query().Get("country") != "US" {
					t.Errorf("unexpected iTunes URL %s", r.URL)
				}
				var items []itunesItem
				switch r.URL.Path {
				case "/search":
					if r.URL.Query().Get("entity") != "album" {
						t.Errorf("search must return albums: %s", r.URL)
					}
					items = []itunesItem{{Wrapper: "collection", AlbumID: 30, Album: "Raydiation", Artist: "C. Ray", Count: 2},
						{Wrapper: "collection", AlbumID: 31, Album: "Space Coast", Artist: "C. Ray", Count: 2}}
					if scenario == "ambiguous edition" {
						items = append(items, itunesItem{Wrapper: "collection", AlbumID: 32, Album: "Raydiation", Artist: "C. Ray", Count: 2})
					}
				case "/lookup":
					if r.URL.Query().Get("entity") != "song" {
						t.Errorf("lookup must return songs: %s", r.URL)
					}
					id, title := int64(30), "Raydiation"
					secondTitle := "Ashes"
					switch r.URL.Query().Get("id") {
					case "31":
						id, title, secondTitle = 31, "Space Coast", "Activated"
					case "32":
						id = 32
					}
					items = []itunesItem{{Wrapper: "collection", AlbumID: id, Album: title, Artist: "C. Ray", Count: 2},
						{Wrapper: "track", Kind: "song", AlbumID: id, Title: "Opening", Disc: 1, Number: 1},
						{Wrapper: "track", Kind: "song", AlbumID: id, Title: secondTitle, Disc: 1, Number: 2}}
				default:
					t.Errorf("unexpected Apple call: %s", r.URL)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"resultCount": len(items), "results": items})
			})
			service.itunes = &itunesCatalog{http: &http.Client{Transport: correctionFixtureTransport{apple}}}
			var ids []int64
			add := func(album, title string) {
				ids = append(ids, insertDiscoveryFile(t, db, scanID, discoveryFile{
					path: "Music/C. Ray/" + album + "/" + title + ".mp3", artist: "C. Ray", album: album, title: title,
				}))
			}
			add("Raydiation", "Ashes (feat. Twisted Insane)")
			if scenario != "incomplete folder" {
				add("Raydiation", "Opening")
			}
			if scenario == "match all" {
				add("Space Coast", "Activated")
				add("Space Coast", "Opening")
				if _, err := service.RequestMatchAll(ctx, scanID, true); err != nil {
					t.Fatal(err)
				}
				if worked, err := service.ProcessWorkflow(ctx); err != nil || !worked {
					t.Fatalf("processed=%v err=%v", worked, err)
				}
				scan, err := service.Scan(ctx, scanID)
				if err != nil || scan.MatchedCount != 4 || !strings.Contains(scan.Message, "Matched 4 through iTunes") {
					t.Fatalf("scan=%+v err=%v", scan, err)
				}
			} else {
				result, err := service.DiscoverScan(ctx, scanID, ids, false)
				want := 0
				if scenario == "musicbrainz miss" {
					want = 2
				}
				if err != nil || result.ITunesMatched != want || result.Matched != want {
					t.Fatalf("discovery=%+v err=%v", result, err)
				}
				if want == 0 {
					return
				}
			}
			var trackNumber int
			var trackID string
			err := db.QueryRow(`SELECT t.number,t.musicbrainz_id FROM music_scan_files f JOIN music_tracks t ON t.id=f.track_id WHERE f.id=?`, ids[0]).Scan(&trackNumber, &trackID)
			if err != nil || trackNumber != 2 || !strings.HasPrefix(trackID, localTrackIDPrefix) {
				t.Fatalf("track number=%d id=%s err=%v", trackNumber, trackID, err)
			}
		})
	}
}
