package music

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestDiscoverMatchesRaydiationByCompleteTracklist(t *testing.T) {
	titles := []string{"God Virus", "Remain Ill", "Demigod", "Lethal", "Mary Jane", "Ashes",
		"Therapy", "Do It Big", "Polarity", "Drip", "Top of the World", "Wisdom"}
	const title = "Raydiation"
	const group = "c316814b-5abc-42a7-b55d-91bff2cbdcdf"
	const release = "432bed1c-e433-46b9-ba64-a8befc80d78c"
	const artist = "b0822a87-0372-4301-869d-6194b665f272"
	for _, scenario := range []string{"full album", "match all", "missing file", "duplicate title", "two matching albums"} {
		t.Run(scenario, func(t *testing.T) {
			trackList := func(edition, groupID string) any {
				tracks := []map[string]any{}
				for i, name := range titles {
					if groupID != group && scenario != "two matching albums" {
						name = "Wrong track " + strconv.Itoa(i+1)
					}
					tracks = append(tracks, map[string]any{"id": fmt.Sprintf("44444444-4444-4444-8444-%012d", i+1), "title": name, "position": i + 1})
				}
				return map[string]any{"id": edition, "media": []any{map[string]any{"position": 1, "tracks": tracks}}}
			}
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/ws/2/artist":
					// The index found the person under a different primary name.
					writeDiscoveryJSON(w, `{"count":1,"artists":[`+artistLookupBody(artist, "Charlie Ray")+`]}`)
				case "/ws/2/release-group":
					if got := r.URL.Query().Get("query"); got != `releasegroup:"Raydiation"` {
						t.Errorf("query = %q", got)
					}
					writeDiscoveryJSON(w, `{"count":2,"release-groups":[{"id":"`+group+`","title":"Raydiation"},{"id":"`+discoveryRG2+`","title":"Raydiation"}]}`)
				case "/ws/2/release":
					if r.URL.Query().Get("release-group") == group {
						writeDiscoveryJSON(w, releaseListBody(release, title))
					} else {
						writeDiscoveryJSON(w, releaseListBody(discoveryRelease2, title))
					}
				case "/ws/2/release/" + release:
					_ = json.NewEncoder(w).Encode(trackList(release, group))
				case "/ws/2/release/" + discoveryRelease2:
					_ = json.NewEncoder(w).Encode(trackList(discoveryRelease2, discoveryRG2))
				case "/ws/2/release-group/" + group:
					writeDiscoveryJSON(w, `{"id":"`+group+`","title":"Raydiation","artist-credit":[{"name":"C. Ray","artist":{"id":"`+artist+`","name":"Charlie Ray"}}]}`)
				case "/ws/2/artist/" + artist:
					writeDiscoveryJSON(w, artistLookupBody(artist, "Charlie Ray"))
				default:
					t.Errorf("unexpected request %s", r.URL)
					http.NotFound(w, r)
				}
			})
			server := httptest.NewServer(handler)
			defer server.Close()
			service, db, scanID := discoveryFixture(t, server)
			service.client.http.Transport = correctionFixtureTransport{handler}
			var selected []int64
			for i, name := range titles {
				if i == 0 && scenario == "missing file" {
					continue
				}
				tag := name
				if name == "Ashes" {
					tag += " (feat. Twisted Insane)"
				}
				if i == 1 && scenario == "duplicate title" {
					tag = titles[0]
				}
				id := insertDiscoveryFile(t, db, scanID, discoveryFile{
					path: "Music/C. Ray/Raydiation/" + strconv.Itoa(i+1) + ".mp3", artist: "C. Ray", album: title, title: tag,
				})
				if i > 0 {
					selected = append(selected, id)
				}
			}
			ctx := context.Background()
			if scenario == "match all" {
				if _, err := service.RequestMatchAll(ctx, scanID, true); err != nil {
					t.Fatal(err)
				}
				if worked, err := service.ProcessWorkflow(ctx); err != nil || !worked {
					t.Fatalf("processed=%v, err=%v", worked, err)
				}
				scan, err := service.Scan(ctx, scanID)
				if err != nil || scan.MatchedCount != len(titles) || scan.UnresolvedCount != 0 {
					t.Fatalf("match all scan=%+v, err=%v", scan, err)
				}
				return
			}
			result, err := service.DiscoverScan(ctx, scanID, selected, false)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "full album" {
				if result.Matched != len(selected) || result.AlbumsAdded != 1 {
					t.Fatalf("expected one album and all selected files matched, got %+v", result)
				}
				var trackNumber int
				if err := db.QueryRow(`SELECT t.number FROM music_scan_files f JOIN music_tracks t ON t.id=f.track_id WHERE f.id=?`, selected[4]).Scan(&trackNumber); err != nil || trackNumber != 6 {
					t.Fatalf("Ashes track number = %d, %v", trackNumber, err)
				}
			} else if result.Matched != 0 || result.AlbumsAdded != 0 {
				t.Fatalf("expected discovery to leave review open, got %+v", result)
			}
			if scenario == "missing file" {
				correction := ScanCorrections{ReleaseGroupID: group, Files: []ScanCorrection{
					{FileID: selected[0], TrackID: discoveryTrack2},
				}}
				if err := service.CorrectScanMatches(ctx, scanID, correction); err != ErrPlanChanged {
					t.Fatalf("foreign track should be rejected: %v", err)
				}
				correction.Files[0].TrackID = "44444444-4444-4444-8444-000000000002"
				if err := service.CorrectScanMatches(ctx, scanID, correction); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

type correctionFixtureTransport struct {
	handler http.Handler
}

func (transport correctionFixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	transport.handler.ServeHTTP(recorder, request)
	return recorder.Result(), nil
}

func TestCorrectScanMatchesUsesReviewedLocalMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("local correction used provider: %s", r.URL)
		http.NotFound(w, r)
	}))
	defer server.Close()
	service, db, scanID := discoveryFixture(t, server)
	first := insertDiscoveryFile(t, db, scanID, discoveryFile{path: "Music/Activated.mp3", title: "Activated"})
	second := insertDiscoveryFile(t, db, scanID, discoveryFile{path: "Music/Classic.mp3", title: "Classic"})
	correction := ScanCorrections{Artist: "C. Ray", Album: "Space Coast", Files: []ScanCorrection{
		{FileID: first, Title: "Activated", Disc: 1, Number: 2},
		{FileID: second, Title: "Classic", Disc: 1, Number: 2},
	}}
	ctx := context.Background()
	if err := service.CorrectScanMatches(ctx, scanID, correction); err != ErrInvalidInput {
		t.Fatalf("duplicate track position: %v", err)
	}
	correction.Files[1].Number = 10
	if err := service.CorrectScanMatches(ctx, scanID, correction); err != nil {
		t.Fatal(err)
	}
	var state, mbid string
	var number int
	var originalTag *int
	err := db.QueryRow(`SELECT f.state,t.musicbrainz_id,t.number,f.tag_track FROM music_scan_files f JOIN music_tracks t ON t.id=f.track_id WHERE f.id=?`, second).
		Scan(&state, &mbid, &number, &originalTag)
	if err != nil || state != "matched" || !strings.HasPrefix(mbid, localTrackIDPrefix) || number != 10 || originalTag != nil {
		t.Fatalf("state=%s id=%s number=%d observed tag=%v err=%v", state, mbid, number, originalTag, err)
	}
	if err := service.CorrectScanMatches(ctx, scanID, correction); err != ErrPlanChanged {
		t.Fatalf("stale correction: %v", err)
	}
}
