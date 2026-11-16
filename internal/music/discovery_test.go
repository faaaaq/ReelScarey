package music

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

const (
	discoveryArtist2    = "11111111-1111-4111-8111-111111111112"
	discoveryRG2        = "22222222-2222-4222-8222-222222222222"
	discoveryRelease2   = "33333333-3333-4333-8333-333333333333"
	discoveryTrack2     = "44444444-4444-4444-8444-444444444444"
	discoveryRecording2 = "55555555-5555-4555-8555-555555555555"
)

type discoveryFile struct {
	path            string
	artist          string
	albumArtist     string
	album           string
	title           string
	track           *int
	disc            *int
	year            *int
	artistMBID      string
	albumArtistMBID string
	rg              string
	trackMBID       string
	recording       string
}

// tagJSONForFile stores the tag facts that do not have a dedicated scan
// column. Album-artist MBIDs live only in the tag document so discovery can
// tell a compilation's track artist from its album artist.
func tagJSONForFile(file discoveryFile) string {
	if file.albumArtistMBID == "" {
		return "{}"
	}
	return `{"musicbrainz_album_artist_id":"` + file.albumArtistMBID + `"}`
}

func discoveryFixture(t *testing.T, server *httptest.Server) (*Service, *sql.DB, int64) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	result, err := db.Exec(`INSERT INTO music_scans(root,state,created_at,updated_at) VALUES('Music','review',1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	scanID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	service := NewServiceWithClient(db, newClient(server.URL+"/ws/2", server.Client(), 0), t.TempDir())
	return service, db, scanID
}

func insertDiscoveryFile(t *testing.T, db *sql.DB, scanID int64, file discoveryFile) int64 {
	t.Helper()
	result, err := db.Exec(`INSERT INTO music_scan_files(scan_id,relative_path,size_bytes,modified_at,sha256,format,duration_ms,bitrate,sample_rate,channels,
		tag_artist,tag_album_artist,tag_album,tag_title,tag_track,tag_disc,tag_year,
		tag_musicbrainz_artist,tag_musicbrainz_album,tag_musicbrainz_release_group,tag_musicbrainz_track,tag_musicbrainz_recording,
		tag_json,state,error,created_at,updated_at)
		VALUES(?,?,100,1,'','flac',1000,0,44100,2,?,?,?,?,?,?,?,?,'',?,?,?,?,'unresolved','No catalog album matches this file.',1,1)`,
		scanID, file.path, file.artist, file.albumArtist, file.album, file.title, file.track, file.disc, file.year,
		file.artistMBID, file.rg, file.trackMBID, file.recording, tagJSONForFile(file))
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func writeDiscoveryJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, body)
}

func artistLookupBody(id, name string) string {
	return `{"id":"` + id + `","name":"` + name + `","sort-name":"` + name + `","country":"GB","type":"Group","life-span":{"begin":"1991","ended":false}}`
}

func releaseLookupBody(releaseID, rgID, title, artistID, artistName string) string {
	return `{"id":"` + rgID + `","title":"` + title + `","primary-type":"Album","secondary-types":[],"first-release-date":"1997-05-21",
		"artist-credit":[{"name":"` + artistName + `","artist":{"id":"` + artistID + `","name":"` + artistName + `"}}]}`
}

func releaseListBody(releaseID, title string) string {
	return `{"release-count":1,"release-offset":0,"releases":[{"id":"` + releaseID + `","title":"` + title + `","status":"Official","date":"1997-05-21"}]}`
}

func tracksBody(releaseID, title, trackID, recordingID string) string {
	return `{"id":"` + releaseID + `","title":"` + title + `","media":[{"position":1,"tracks":[{"id":"` + trackID + `","position":1,"number":"1","title":"` + title + `","length":284000,"recording":{"id":"` + recordingID + `","length":284000}}]}]}`
}

// recordingSearchBody answers a /recording search with one credited recording
// and its release groups, the evidence the recording-level matcher scores.
func recordingSearchBody(recordingID, title, artistID, artistName, rgID, rgTitle string) string {
	return `{"count":1,"offset":0,"recordings":[{"id":"` + recordingID + `","score":100,"title":"` + title + `","length":284000,` +
		`"artist-credit":[{"name":"` + artistName + `","artist":{"id":"` + artistID + `","name":"` + artistName + `"}}],` +
		`"release-groups":[{"id":"` + rgID + `","title":"` + rgTitle + `","primary-type":"Album","first-release-date":"1997-05-21"}]}]}`
}

func emptyRecordingSearchBody() string {
	return `{"count":0,"offset":0,"recordings":[]}`
}

func TestDiscoverReleaseGroupIDCreatesCatalogAndRematches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/2/release-group/"+testRGID:
			writeDiscoveryJSON(w, releaseLookupBody(testReleaseID, testRGID, "OK Computer", testMBID, "Radiohead"))
		case r.URL.Path == "/ws/2/artist/"+testMBID:
			writeDiscoveryJSON(w, artistLookupBody(testMBID, "Radiohead"))
		case r.URL.Path == "/ws/2/release" && r.URL.Query().Get("release-group") == testRGID:
			writeDiscoveryJSON(w, releaseListBody(testReleaseID, "OK Computer"))
		case r.URL.Path == "/ws/2/release/"+testReleaseID:
			writeDiscoveryJSON(w, tracksBody(testReleaseID, "Airbag", testTrackID, testRecordingID))
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	service, db, scanID := discoveryFixture(t, server)
	fileID := insertDiscoveryFile(t, db, scanID, discoveryFile{
		path: "Music/Radiohead/OK Computer/01 Airbag.flac", artist: "Radiohead", albumArtist: "Radiohead",
		album: "OK Computer", title: "Airbag", track: intPtr(1), disc: intPtr(1), year: intPtr(1997),
		rg: testRGID,
	})
	result, err := service.DiscoverScan(context.Background(), scanID, []int64{fileID}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Selected != 1 || result.Matched != 1 || result.ArtistsAdded != 1 || result.AlbumsAdded != 1 ||
		result.Unresolved != 0 || result.Remaining != 0 || result.Deferred != 0 || len(result.Warnings) != 0 {
		t.Fatal("discover result", result)
	}
	scan, err := service.Scan(context.Background(), scanID)
	if err != nil {
		t.Fatal(err)
	}
	if scan.State != "review" || scan.FileCount != 1 || scan.MatchedCount != 1 || scan.UnresolvedCount != 0 {
		t.Fatal("scan counts", scan)
	}
	var artistID, albumID, trackID sql.NullInt64
	var state, message string
	if err := db.QueryRow(`SELECT artist_id,album_id,track_id,state,error FROM music_scan_files WHERE id=?`, fileID).
		Scan(&artistID, &albumID, &trackID, &state, &message); err != nil {
		t.Fatal(err)
	}
	if !artistID.Valid || !albumID.Valid || !trackID.Valid || state != "matched" || message != "" {
		t.Fatal("scan file", artistID, albumID, trackID, state, message)
	}
	var artists, albums, tracks, files int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM music_artists),(SELECT COUNT(*) FROM music_albums),(SELECT COUNT(*) FROM music_tracks),(SELECT COUNT(*) FROM music_files)`).
		Scan(&artists, &albums, &tracks, &files); err != nil {
		t.Fatal(err)
	}
	if artists != 1 || albums != 1 || tracks != 1 || files != 0 {
		t.Fatal("catalog counts", artists, albums, tracks, files)
	}
	again, err := service.DiscoverScan(context.Background(), scanID, []int64{fileID}, false)
	if err != nil || again.Selected != 0 || again.Remaining != 0 {
		t.Fatal("second discovery", again, err)
	}
}

func TestDiscoverExactArtistAndAlbumRematches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/2/artist" && r.URL.Query().Get("query") != "":
			if !strings.Contains(r.URL.Query().Get("query"), `"Radiohead"`) {
				t.Errorf("artist query %q", r.URL.Query().Get("query"))
			}
			writeDiscoveryJSON(w, `{"count":1,"offset":0,"artists":[`+artistLookupBody(testMBID, "Radiohead")+`]}`)
		case r.URL.Path == "/ws/2/release-group" && r.URL.Query().Get("query") != "":
			query := r.URL.Query().Get("query")
			if !strings.Contains(query, "arid:"+testMBID) || !strings.Contains(query, `releasegroup:"OK Computer"`) {
				t.Errorf("release-group query %q", query)
			}
			writeDiscoveryJSON(w, `{"count":1,"offset":0,"release-groups":[{"id":"`+testRGID+`","title":"OK Computer","primary-type":"Album","secondary-types":[],"first-release-date":"1997-05-21"}]}`)
		case r.URL.Path == "/ws/2/artist/"+testMBID:
			writeDiscoveryJSON(w, artistLookupBody(testMBID, "Radiohead"))
		case r.URL.Path == "/ws/2/release" && r.URL.Query().Get("release-group") == testRGID:
			writeDiscoveryJSON(w, releaseListBody(testReleaseID, "OK Computer"))
		case r.URL.Path == "/ws/2/release/"+testReleaseID:
			writeDiscoveryJSON(w, tracksBody(testReleaseID, "Airbag", testTrackID, testRecordingID))
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	service, db, scanID := discoveryFixture(t, server)
	fileID := insertDiscoveryFile(t, db, scanID, discoveryFile{
		path: "Music/Radiohead/OK Computer/01 Airbag.flac", artist: "Radiohead", albumArtist: "Radiohead",
		album: "OK Computer", title: "Airbag", track: intPtr(1), disc: intPtr(1), year: intPtr(1997),
	})
	result, err := service.DiscoverScan(context.Background(), scanID, []int64{fileID}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Selected != 1 || result.Matched != 1 || result.ArtistsAdded != 1 || result.AlbumsAdded != 1 || result.Remaining != 0 {
		t.Fatal("discover result", result)
	}
	var state string
	if err := db.QueryRow(`SELECT state FROM music_scan_files WHERE id=?`, fileID).Scan(&state); err != nil || state != "matched" {
		t.Fatal("scan file state", state, err)
	}
}

func TestDiscoverResolvesAmbiguousOrMissingEvidenceThroughRecordings(t *testing.T) {
	// The recording-level cascade turns the two cases the strict album path
	// used to abandon into confident matches: an ambiguous artist name is
	// resolved through the recording's credited artist, and a missing or
	// wrong album never blocks a recording identification.
	recordingSearch := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws/2/recording" {
			return
		}
		query := r.URL.Query().Get("query")
		if !strings.Contains(query, `recording:"Airbag"`) || !strings.Contains(query, `artist:"Radiohead"`) {
			t.Errorf("recording query %q", query)
		}
		writeDiscoveryJSON(w, recordingSearchBody(testRecordingID, "Airbag", testMBID, "Radiohead", testRGID, "OK Computer"))
	}
	fullCatalogHandler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/2/artist":
			writeDiscoveryJSON(w, `{"count":2,"offset":0,"artists":[`+
				artistLookupBody(testMBID, "Radiohead")+`,`+artistLookupBody(discoveryArtist2, "Radiohead")+`]}`)
		case r.URL.Path == "/ws/2/recording":
			recordingSearch(w, r)
		case r.URL.Path == "/ws/2/release-group/"+testRGID:
			writeDiscoveryJSON(w, releaseLookupBody(testReleaseID, testRGID, "OK Computer", testMBID, "Radiohead"))
		case r.URL.Path == "/ws/2/artist/"+testMBID:
			writeDiscoveryJSON(w, artistLookupBody(testMBID, "Radiohead"))
		case r.URL.Path == "/ws/2/release" && r.URL.Query().Get("release-group") == testRGID:
			writeDiscoveryJSON(w, releaseListBody(testReleaseID, "OK Computer"))
		case r.URL.Path == "/ws/2/release/"+testReleaseID:
			writeDiscoveryJSON(w, tracksBody(testReleaseID, "Airbag", testTrackID, testRecordingID))
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}

	t.Run("ambiguous artist", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(fullCatalogHandler))
		defer server.Close()
		service, db, scanID := discoveryFixture(t, server)
		fileID := insertDiscoveryFile(t, db, scanID, discoveryFile{
			path: "Music/Radiohead/OK Computer/01 Airbag.flac", artist: "Radiohead", albumArtist: "Radiohead",
			album: "OK Computer", title: "Airbag", track: intPtr(1), disc: intPtr(1),
		})
		result, err := service.DiscoverScan(context.Background(), scanID, []int64{fileID}, false)
		if err != nil {
			t.Fatal(err)
		}
		if result.Matched != 1 || result.Unresolved != 0 || result.Remaining != 0 {
			t.Fatal("discover result", result)
		}
		var mbid string
		if err := db.QueryRow(`SELECT musicbrainz_id FROM music_artists`).Scan(&mbid); err != nil {
			t.Fatal(err)
		}
		if mbid != testMBID {
			t.Fatal("the recording did not resolve the ambiguous artist", mbid)
		}
		var state string
		if err := db.QueryRow(`SELECT state FROM music_scan_files WHERE id=?`, fileID).Scan(&state); err != nil || state != "matched" {
			t.Fatal("scan file state", state, err)
		}
	})

	t.Run("missing album", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(fullCatalogHandler))
		defer server.Close()
		service, db, scanID := discoveryFixture(t, server)
		fileID := insertDiscoveryFile(t, db, scanID, discoveryFile{
			path: "Music/Radiohead/Missing/01 Airbag.flac", artist: "Radiohead", albumArtist: "Radiohead",
			album: "Missing", title: "Airbag", track: intPtr(1), disc: intPtr(1),
		})
		result, err := service.DiscoverScan(context.Background(), scanID, []int64{fileID}, false)
		if err != nil {
			t.Fatal(err)
		}
		if result.Matched != 1 || result.Unresolved != 0 || result.Remaining != 0 {
			t.Fatal("discover result", result)
		}
		var albumID, trackID int64
		var title string
		if err := db.QueryRow(`SELECT album_id,track_id FROM music_scan_files WHERE id=?`, fileID).Scan(&albumID, &trackID); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT title FROM music_albums WHERE id=?`, albumID).Scan(&title); err != nil {
			t.Fatal(err)
		}
		if title != "OK Computer" || trackID == 0 {
			t.Fatal("the recording did not resolve into the real album", title, trackID)
		}
	})

	t.Run("missing tags", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("provider should not be called for missing tags: %s", r.URL.String())
			http.NotFound(w, r)
		}))
		defer server.Close()
		service, db, scanID := discoveryFixture(t, server)
		fileID := insertDiscoveryFile(t, db, scanID, discoveryFile{
			path: "Music/Unknown/01 Other.flac", title: "Other", track: intPtr(1), disc: intPtr(1),
		})
		result, err := service.DiscoverScan(context.Background(), scanID, []int64{fileID}, false)
		if err != nil {
			t.Fatal(err)
		}
		if result.Matched != 0 || result.Unresolved != 1 || result.Remaining != 1 {
			t.Fatal("discover result", result)
		}
		var message string
		if err := db.QueryRow(`SELECT error FROM music_scan_files WHERE id=?`, fileID).Scan(&message); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(message, "no artist name") {
			t.Fatal("message", message)
		}
	})
}

func TestDiscoverCachesDuplicateArtist(t *testing.T) {
	var artistLookups atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/2/release-group/"+testRGID:
			writeDiscoveryJSON(w, releaseLookupBody(testReleaseID, testRGID, "OK Computer", testMBID, "Radiohead"))
		case r.URL.Path == "/ws/2/release-group/"+discoveryRG2:
			writeDiscoveryJSON(w, releaseLookupBody(discoveryRelease2, discoveryRG2, "The Bends", testMBID, "Radiohead"))
		case r.URL.Path == "/ws/2/artist/"+testMBID:
			artistLookups.Add(1)
			writeDiscoveryJSON(w, artistLookupBody(testMBID, "Radiohead"))
		case r.URL.Path == "/ws/2/release" && r.URL.Query().Get("release-group") == testRGID:
			writeDiscoveryJSON(w, releaseListBody(testReleaseID, "OK Computer"))
		case r.URL.Path == "/ws/2/release" && r.URL.Query().Get("release-group") == discoveryRG2:
			writeDiscoveryJSON(w, releaseListBody(discoveryRelease2, "The Bends"))
		case r.URL.Path == "/ws/2/release/"+testReleaseID:
			writeDiscoveryJSON(w, tracksBody(testReleaseID, "Airbag", testTrackID, testRecordingID))
		case r.URL.Path == "/ws/2/release/"+discoveryRelease2:
			writeDiscoveryJSON(w, tracksBody(discoveryRelease2, "Paranoid Android", discoveryTrack2, discoveryRecording2))
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	service, db, scanID := discoveryFixture(t, server)
	first := insertDiscoveryFile(t, db, scanID, discoveryFile{
		path: "Music/Radiohead/OK Computer/01 Airbag.flac", artist: "Radiohead", albumArtist: "Radiohead",
		album: "OK Computer", title: "Airbag", track: intPtr(1), disc: intPtr(1), rg: testRGID,
	})
	second := insertDiscoveryFile(t, db, scanID, discoveryFile{
		path: "Music/Radiohead/The Bends/01 Paranoid Android.flac", artist: "Radiohead", albumArtist: "Radiohead",
		album: "The Bends", title: "Paranoid Android", track: intPtr(1), disc: intPtr(1), rg: discoveryRG2,
	})
	result, err := service.DiscoverScan(context.Background(), scanID, []int64{first, second}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Matched != 2 || result.ArtistsAdded != 1 || result.AlbumsAdded != 2 || result.Unresolved != 0 {
		t.Fatal("discover result", result)
	}
	if artistLookups.Load() != 1 {
		t.Fatalf("artist was looked up %d times", artistLookups.Load())
	}
	var artists, albums int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM music_artists),(SELECT COUNT(*) FROM music_albums)`).Scan(&artists, &albums); err != nil {
		t.Fatal(err)
	}
	if artists != 1 || albums != 2 {
		t.Fatal("catalog counts", artists, albums)
	}
}

func TestDiscoverUsesYearToDisambiguateExactTitle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/2/artist":
			writeDiscoveryJSON(w, `{"count":1,"offset":0,"artists":[`+artistLookupBody(testMBID, "Radiohead")+`]}`)
		case r.URL.Path == "/ws/2/release-group":
			writeDiscoveryJSON(w, `{"count":2,"offset":0,"release-groups":[`+
				`{"id":"`+testRGID+`","title":"Album","primary-type":"Album","secondary-types":[],"first-release-date":"1999-01-01"},`+
				`{"id":"`+discoveryRG2+`","title":"Album","primary-type":"Album","secondary-types":[],"first-release-date":"2003-01-01"}]}`)
		case r.URL.Path == "/ws/2/artist/"+testMBID:
			writeDiscoveryJSON(w, artistLookupBody(testMBID, "Radiohead"))
		case r.URL.Path == "/ws/2/release" && r.URL.Query().Get("release-group") == discoveryRG2:
			writeDiscoveryJSON(w, releaseListBody(discoveryRelease2, "Album"))
		case r.URL.Path == "/ws/2/release/"+discoveryRelease2:
			writeDiscoveryJSON(w, tracksBody(discoveryRelease2, "Song", discoveryTrack2, discoveryRecording2))
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	service, db, scanID := discoveryFixture(t, server)
	fileID := insertDiscoveryFile(t, db, scanID, discoveryFile{
		path: "Music/Radiohead/Album/01 Song.flac", artist: "Radiohead", albumArtist: "Radiohead",
		album: "Album", title: "Song", track: intPtr(1), disc: intPtr(1), year: intPtr(2003),
	})
	result, err := service.DiscoverScan(context.Background(), scanID, []int64{fileID}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Matched != 1 {
		t.Fatal("discover result", result)
	}
	var mbid string
	if err := db.QueryRow(`SELECT musicbrainz_id FROM music_albums`).Scan(&mbid); err != nil {
		t.Fatal(err)
	}
	if mbid != discoveryRG2 {
		t.Fatal("year did not disambiguate the exact title", mbid)
	}
}

func TestDiscoverDefersAlbumsBeyondBatchLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()
	service, db, scanID := discoveryFixture(t, server)
	ids := make([]int64, 0, MaxDiscoverAlbums+1)
	for i := 1; i <= MaxDiscoverAlbums+1; i++ {
		ids = append(ids, insertDiscoveryFile(t, db, scanID, discoveryFile{
			path:        fmt.Sprintf("Music/Artist/Album %02d/01 Song.flac", i),
			artist:      "Artist",
			albumArtist: "Artist",
			album:       fmt.Sprintf("Album %02d", i),
			title:       "Song",
			track:       intPtr(1),
			disc:        intPtr(1),
			rg:          fmt.Sprintf("%08d-0000-4000-8000-%012d", i, i),
		}))
	}
	result, err := service.DiscoverScan(context.Background(), scanID, ids, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Selected != MaxDiscoverAlbums+1 || result.Deferred != 1 {
		t.Fatal("discover result", result)
	}
	// The provider-bounded album pass defers the last album group, but the
	// recording pass and the local name fallback still resolve every file
	// from its own evidence without fabricating a release-group identity.
	if result.Matched != MaxDiscoverAlbums+1 || result.Unresolved != 0 || result.Remaining != 0 {
		t.Fatal("discover result", result)
	}
	var artists, albums int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM music_artists),(SELECT COUNT(*) FROM music_albums)`).Scan(&artists, &albums); err != nil {
		t.Fatal(err)
	}
	if artists != 1 || albums != MaxDiscoverAlbums+1 {
		t.Fatalf("catalog rows artists=%d albums=%d", artists, albums)
	}
	var state string
	if err := db.QueryRow(`SELECT state FROM music_scan_files WHERE id=?`, ids[len(ids)-1]).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "matched" {
		t.Fatal("deferred file was not recovered", state)
	}
}

func TestDiscoverProviderFailureRetainsUnresolvedFiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	service, db, scanID := discoveryFixture(t, server)
	fileID := insertDiscoveryFile(t, db, scanID, discoveryFile{
		path: "Music/Radiohead/OK Computer/01 Airbag.flac", artist: "Radiohead", albumArtist: "Radiohead",
		album: "OK Computer", title: "Airbag", track: intPtr(1), disc: intPtr(1),
	})
	result, err := service.DiscoverScan(context.Background(), scanID, []int64{fileID}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Selected != 1 || result.Matched != 0 || result.ArtistsAdded != 0 || result.AlbumsAdded != 0 ||
		result.Unresolved != 1 || result.Remaining != 1 || len(result.Warnings) != 1 {
		t.Fatal("discover result", result)
	}
	var state, message string
	if err := db.QueryRow(`SELECT state,error FROM music_scan_files WHERE id=?`, fileID).Scan(&state, &message); err != nil {
		t.Fatal(err)
	}
	if state != "unresolved" || !strings.Contains(message, "MusicBrainz") {
		t.Fatal("scan file", state, message)
	}
	var artists, albums int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM music_artists),(SELECT COUNT(*) FROM music_albums)`).Scan(&artists, &albums); err != nil {
		t.Fatal(err)
	}
	if artists != 0 || albums != 0 {
		t.Fatal("provider failure changed the catalog", artists, albums)
	}
	scan, err := service.Scan(context.Background(), scanID)
	if err != nil {
		t.Fatal(err)
	}
	if scan.MatchedCount != 0 || scan.UnresolvedCount != 1 {
		t.Fatal("scan counts", scan)
	}
}

func TestDiscoverUsesTaggedArtistMBID(t *testing.T) {
	var nameSearched atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/2/artist" && r.URL.Query().Get("query") != "":
			nameSearched.Store(true)
			writeDiscoveryJSON(w, `{"count":0,"offset":0,"artists":[]}`)
		case r.URL.Path == "/ws/2/artist/"+testMBID:
			writeDiscoveryJSON(w, artistLookupBody(testMBID, "Radiohead"))
		case r.URL.Path == "/ws/2/release-group" && r.URL.Query().Get("query") != "":
			writeDiscoveryJSON(w, `{"count":1,"offset":0,"release-groups":[{"id":"`+testRGID+`","title":"OK Computer","primary-type":"Album","secondary-types":[],"first-release-date":"1997-05-21"}]}`)
		case r.URL.Path == "/ws/2/release" && r.URL.Query().Get("release-group") == testRGID:
			writeDiscoveryJSON(w, releaseListBody(testReleaseID, "OK Computer"))
		case r.URL.Path == "/ws/2/release/"+testReleaseID:
			writeDiscoveryJSON(w, tracksBody(testReleaseID, "Airbag", testTrackID, testRecordingID))
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	service, db, scanID := discoveryFixture(t, server)
	fileID := insertDiscoveryFile(t, db, scanID, discoveryFile{
		path: "Music/Radiohead/OK Computer/01 Airbag.flac", artist: "Radiohead", albumArtist: "Radiohead",
		album: "OK Computer", title: "Airbag", track: intPtr(1), disc: intPtr(1), artistMBID: testMBID,
	})
	result, err := service.DiscoverScan(context.Background(), scanID, []int64{fileID}, false)
	if err != nil {
		t.Fatal(err)
	}
	if nameSearched.Load() {
		t.Fatal("discovery searched the artist by name despite a tagged MusicBrainz artist ID")
	}
	if result.Matched != 1 || result.ArtistsAdded != 1 || result.AlbumsAdded != 1 || result.Remaining != 0 {
		t.Fatal("discover result", result)
	}
}

func TestDiscoverFindsExactArtistOnLaterBoundedPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/2/artist" && r.URL.Query().Get("query") != "":
			switch r.URL.Query().Get("offset") {
			case "0":
				items := make([]string, 0, 25)
				for i := 1; i <= 25; i++ {
					id := fmt.Sprintf("%08d-0000-4000-8000-%012d", i, i)
					items = append(items, artistLookupBody(id, fmt.Sprintf("Tribute Band %02d", i)))
				}
				writeDiscoveryJSON(w, `{"count":40,"offset":0,"artists":[`+strings.Join(items, ",")+`]}`)
			case "25":
				writeDiscoveryJSON(w, `{"count":40,"offset":25,"artists":[`+artistLookupBody(testMBID, "Radiohead")+`]}`)
			default:
				writeDiscoveryJSON(w, `{"count":40,"offset":50,"artists":[]}`)
			}
		case r.URL.Path == "/ws/2/artist/"+testMBID:
			writeDiscoveryJSON(w, artistLookupBody(testMBID, "Radiohead"))
		case r.URL.Path == "/ws/2/release-group" && r.URL.Query().Get("query") != "":
			writeDiscoveryJSON(w, `{"count":1,"offset":0,"release-groups":[{"id":"`+testRGID+`","title":"OK Computer","primary-type":"Album","secondary-types":[],"first-release-date":"1997-05-21"}]}`)
		case r.URL.Path == "/ws/2/release" && r.URL.Query().Get("release-group") == testRGID:
			writeDiscoveryJSON(w, releaseListBody(testReleaseID, "OK Computer"))
		case r.URL.Path == "/ws/2/release/"+testReleaseID:
			writeDiscoveryJSON(w, tracksBody(testReleaseID, "Airbag", testTrackID, testRecordingID))
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	service, db, scanID := discoveryFixture(t, server)
	fileID := insertDiscoveryFile(t, db, scanID, discoveryFile{
		path: "Music/Radiohead/OK Computer/01 Airbag.flac", artist: "Radiohead", albumArtist: "Radiohead",
		album: "OK Computer", title: "Airbag", track: intPtr(1), disc: intPtr(1),
	})
	result, err := service.DiscoverScan(context.Background(), scanID, []int64{fileID}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Matched != 1 || result.ArtistsAdded != 1 || result.AlbumsAdded != 1 {
		t.Fatal("discover result", result)
	}
	var message string
	if err := db.QueryRow(`SELECT error FROM music_scan_files WHERE id=?`, fileID).Scan(&message); err != nil {
		t.Fatal(err)
	}
	if message != "" {
		t.Fatal("scan file message", message)
	}
}

func TestDiscoverFallsBackToArtistDiscography(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/2/artist" && r.URL.Query().Get("query") != "":
			writeDiscoveryJSON(w, `{"count":1,"offset":0,"artists":[`+artistLookupBody(testMBID, "Radiohead")+`]}`)
		case r.URL.Path == "/ws/2/artist/"+testMBID:
			writeDiscoveryJSON(w, artistLookupBody(testMBID, "Radiohead"))
		case r.URL.Path == "/ws/2/release-group" && r.URL.Query().Get("query") != "":
			writeDiscoveryJSON(w, `{"count":150,"offset":0,"release-groups":[{"id":"`+discoveryRG2+`","title":"Unrelated","primary-type":"Album","secondary-types":[],"first-release-date":"2000-01-01"}]}`)
		case r.URL.Path == "/ws/2/release-group" && r.URL.Query().Get("artist") == testMBID:
			writeDiscoveryJSON(w, `{"release-group-count":2,"release-group-offset":0,"release-groups":[
				{"id":"`+discoveryRG2+`","title":"Unrelated","primary-type":"Album","secondary-types":[],"first-release-date":"2000-01-01"},
				{"id":"`+testRGID+`","title":"Right Album \u2014 Deluxe","primary-type":"Album","secondary-types":[],"first-release-date":"1997-05-21"}]}`)
		case r.URL.Path == "/ws/2/release" && r.URL.Query().Get("release-group") == testRGID:
			writeDiscoveryJSON(w, releaseListBody(testReleaseID, "Right Album - Deluxe"))
		case r.URL.Path == "/ws/2/release/"+testReleaseID:
			writeDiscoveryJSON(w, tracksBody(testReleaseID, "Airbag", testTrackID, testRecordingID))
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	service, db, scanID := discoveryFixture(t, server)
	fileID := insertDiscoveryFile(t, db, scanID, discoveryFile{
		path: "Music/Radiohead/Right Album/01 Airbag.flac", artist: "Radiohead", albumArtist: "Radiohead",
		album: "Right Album - Deluxe", title: "Airbag", track: intPtr(1), disc: intPtr(1),
	})
	result, err := service.DiscoverScan(context.Background(), scanID, []int64{fileID}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Matched != 1 || result.AlbumsAdded != 1 {
		t.Fatal("discover result", result)
	}
	var mbid string
	if err := db.QueryRow(`SELECT musicbrainz_id FROM music_albums`).Scan(&mbid); err != nil {
		t.Fatal(err)
	}
	if mbid != testRGID {
		t.Fatal("wrong album", mbid)
	}
}

func TestDiscoverMatchesTrackWithoutDiscTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/2/artist" && r.URL.Query().Get("query") != "":
			writeDiscoveryJSON(w, `{"count":1,"offset":0,"artists":[`+artistLookupBody(testMBID, "Radiohead")+`]}`)
		case r.URL.Path == "/ws/2/artist/"+testMBID:
			writeDiscoveryJSON(w, artistLookupBody(testMBID, "Radiohead"))
		case r.URL.Path == "/ws/2/release-group" && r.URL.Query().Get("query") != "":
			writeDiscoveryJSON(w, `{"count":1,"offset":0,"release-groups":[{"id":"`+testRGID+`","title":"OK Computer","primary-type":"Album","secondary-types":[],"first-release-date":"1997-05-21"}]}`)
		case r.URL.Path == "/ws/2/release" && r.URL.Query().Get("release-group") == testRGID:
			writeDiscoveryJSON(w, releaseListBody(testReleaseID, "OK Computer"))
		case r.URL.Path == "/ws/2/release/"+testReleaseID:
			writeDiscoveryJSON(w, tracksBody(testReleaseID, "Airbag", testTrackID, testRecordingID))
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	service, db, scanID := discoveryFixture(t, server)
	fileID := insertDiscoveryFile(t, db, scanID, discoveryFile{
		path: "Music/Radiohead/OK Computer/01 Airbag.flac", artist: "Radiohead", albumArtist: "Radiohead",
		album: "OK Computer", title: "Wrong title", track: intPtr(1),
	})
	result, err := service.DiscoverScan(context.Background(), scanID, []int64{fileID}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Matched != 1 {
		t.Fatal("missing disc tag prevented the number match", result)
	}
}

func TestDiscoverMatchesMultiDiscByTitleWhenDiscIsMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/2/artist" && r.URL.Query().Get("query") != "":
			writeDiscoveryJSON(w, `{"count":1,"offset":0,"artists":[`+artistLookupBody(testMBID, "Queen")+`]}`)
		case r.URL.Path == "/ws/2/artist/"+testMBID:
			writeDiscoveryJSON(w, artistLookupBody(testMBID, "Queen"))
		case r.URL.Path == "/ws/2/release-group" && r.URL.Query().Get("query") != "":
			writeDiscoveryJSON(w, `{"count":1,"offset":0,"release-groups":[{"id":"`+testRGID+`","title":"Greatest Hits","primary-type":"Album","secondary-types":[],"first-release-date":"1981-01-01"}]}`)
		case r.URL.Path == "/ws/2/release" && r.URL.Query().Get("release-group") == testRGID:
			writeDiscoveryJSON(w, releaseListBody(testReleaseID, "Greatest Hits"))
		case r.URL.Path == "/ws/2/release/"+testReleaseID:
			writeDiscoveryJSON(w, `{"id":"`+testReleaseID+`","title":"Greatest Hits","media":[
				{"position":1,"tracks":[{"id":"`+testTrackID+`","position":1,"number":"1","title":"Intro","recording":{"id":"`+testRecordingID+`"}}]},
				{"position":2,"tracks":[{"id":"`+discoveryTrack2+`","position":1,"number":"1","title":"Don\u2019t Stop Me Now","recording":{"id":"`+discoveryRecording2+`"}}]}]}`)
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	service, db, scanID := discoveryFixture(t, server)
	fileID := insertDiscoveryFile(t, db, scanID, discoveryFile{
		path: "Music/Queen/Greatest Hits/01 Dont Stop Me Now.flac", artist: "Queen", albumArtist: "Queen",
		album: "Greatest Hits", title: "Don't Stop Me Now", track: intPtr(1),
	})
	result, err := service.DiscoverScan(context.Background(), scanID, []int64{fileID}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Matched != 1 {
		t.Fatal("multi-disc title match failed", result)
	}
	var trackID int64
	if err := db.QueryRow(`SELECT track_id FROM music_scan_files WHERE id=?`, fileID).Scan(&trackID); err != nil {
		t.Fatal(err)
	}
	var number, disc int
	if err := db.QueryRow(`SELECT disc,number FROM music_tracks WHERE id=?`, trackID).Scan(&disc, &number); err != nil {
		t.Fatal(err)
	}
	if disc != 2 || number != 1 {
		t.Fatal("matched the wrong disc", disc, number)
	}
}

func TestDiscoverCompilationUsesAlbumArtistMBID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/2/artist/"+discoveryArtist2:
			t.Error("discovery used the track artist MBID for a compilation album")
			http.NotFound(w, r)
		case r.URL.Path == "/ws/2/artist/"+testMBID:
			writeDiscoveryJSON(w, artistLookupBody(testMBID, "Various Artists"))
		case r.URL.Path == "/ws/2/artist" && r.URL.Query().Get("query") != "":
			t.Error("compilation discovery searched the album artist by name despite a tagged MBID")
			writeDiscoveryJSON(w, `{"count":0,"offset":0,"artists":[]}`)
		case r.URL.Path == "/ws/2/release-group" && r.URL.Query().Get("query") != "":
			writeDiscoveryJSON(w, `{"count":1,"offset":0,"release-groups":[{"id":"`+testRGID+`","title":"Now That Is Music","primary-type":"Album","secondary-types":["Compilation"],"first-release-date":"1999-01-01"}]}`)
		case r.URL.Path == "/ws/2/release" && r.URL.Query().Get("release-group") == testRGID:
			writeDiscoveryJSON(w, releaseListBody(testReleaseID, "Now That Is Music"))
		case r.URL.Path == "/ws/2/release/"+testReleaseID:
			writeDiscoveryJSON(w, tracksBody(testReleaseID, "Song", testTrackID, testRecordingID))
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	service, db, scanID := discoveryFixture(t, server)
	fileID := insertDiscoveryFile(t, db, scanID, discoveryFile{
		path: "Music/Various Artists/Now That Is Music/01 Song.flac", artist: "Some Band", albumArtist: "Various Artists",
		album: "Now That Is Music", title: "Song", track: intPtr(1), disc: intPtr(1),
		artistMBID: discoveryArtist2, albumArtistMBID: testMBID,
	})
	result, err := service.DiscoverScan(context.Background(), scanID, []int64{fileID}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Matched != 1 || result.ArtistsAdded != 1 || result.AlbumsAdded != 1 {
		t.Fatal("discover result", result)
	}
	var mbid string
	if err := db.QueryRow(`SELECT musicbrainz_id FROM music_artists`).Scan(&mbid); err != nil {
		t.Fatal(err)
	}
	if mbid != testMBID {
		t.Fatal("compilation was attached to the track artist", mbid)
	}
}

func TestDiscoverAlbumTrackMissingIsRecoveredFromNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/2/artist" && r.URL.Query().Get("query") != "":
			writeDiscoveryJSON(w, `{"count":1,"offset":0,"artists":[`+artistLookupBody(testMBID, "Radiohead")+`]}`)
		case r.URL.Path == "/ws/2/artist/"+testMBID:
			writeDiscoveryJSON(w, artistLookupBody(testMBID, "Radiohead"))
		case r.URL.Path == "/ws/2/release-group" && r.URL.Query().Get("query") != "":
			writeDiscoveryJSON(w, `{"count":1,"offset":0,"release-groups":[{"id":"`+testRGID+`","title":"OK Computer","primary-type":"Album","secondary-types":[],"first-release-date":"1997-05-21"}]}`)
		case r.URL.Path == "/ws/2/recording":
			writeDiscoveryJSON(w, emptyRecordingSearchBody())
		case r.URL.Path == "/ws/2/release" && r.URL.Query().Get("release-group") == testRGID:
			writeDiscoveryJSON(w, releaseListBody(testReleaseID, "OK Computer"))
		case r.URL.Path == "/ws/2/release/"+testReleaseID:
			writeDiscoveryJSON(w, tracksBody(testReleaseID, "Different Song", testTrackID, testRecordingID))
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	service, db, scanID := discoveryFixture(t, server)
	fileID := insertDiscoveryFile(t, db, scanID, discoveryFile{
		path: "Music/Radiohead/OK Computer/05 Missing.flac", artist: "Radiohead", albumArtist: "Radiohead",
		album: "OK Computer", title: "Missing", track: intPtr(5), disc: intPtr(1),
	})
	result, err := service.DiscoverScan(context.Background(), scanID, []int64{fileID}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Matched != 1 || result.ArtistsAdded != 1 || result.AlbumsAdded != 1 || result.Unresolved != 0 {
		t.Fatal("discover result", result)
	}
	var state string
	if err := db.QueryRow(`SELECT state FROM music_scan_files WHERE id=?`, fileID).Scan(&state); err != nil || state != "matched" {
		t.Fatal("scan file state", state, err)
	}
	again, err := service.DiscoverScan(context.Background(), scanID, []int64{fileID}, false)
	if err != nil {
		t.Fatal(err)
	}
	if again.Selected != 0 || again.Matched != 0 {
		t.Fatal("second discovery found work again", again)
	}
	var artists, albums, tracks int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM music_artists),(SELECT COUNT(*) FROM music_albums),(SELECT COUNT(*) FROM music_tracks)`).Scan(&artists, &albums, &tracks); err != nil || artists != 1 || albums != 1 || tracks != 2 {
		t.Fatal("rerun duplicated catalog rows", artists, albums, tracks, err)
	}
	var trackTitle, trackMBID string
	if err := db.QueryRow(`SELECT title,musicbrainz_id FROM music_tracks WHERE number=5`).Scan(&trackTitle, &trackMBID); err != nil {
		t.Fatal(err)
	}
	if trackTitle != "Missing" || !IsLocalMusicID(trackMBID) {
		t.Fatalf("added track %q %q", trackTitle, trackMBID)
	}
}

func TestDiscoverProviderOutageRetainsExistingCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	service, db, scanID := discoveryFixture(t, server)
	if _, err := db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES(1,?,'Radiohead',1,'missing',1,1)`, testMBID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(2,1,?,'Kid A',1,'missing',1,1)`, testRGID); err != nil {
		t.Fatal(err)
	}
	fileID := insertDiscoveryFile(t, db, scanID, discoveryFile{
		path: "Music/Radiohead/OK Computer/01 Airbag.flac", artist: "Radiohead", albumArtist: "Radiohead",
		album: "OK Computer", title: "Airbag", track: intPtr(1), disc: intPtr(1),
	})
	result, err := service.DiscoverScan(context.Background(), scanID, []int64{fileID}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Matched != 0 || len(result.Warnings) != 1 || result.Remaining != 1 {
		t.Fatal("discover result", result)
	}
	var artists, albums int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM music_artists),(SELECT COUNT(*) FROM music_albums)`).Scan(&artists, &albums); err != nil {
		t.Fatal(err)
	}
	if artists != 1 || albums != 1 {
		t.Fatal("provider outage changed the catalog", artists, albums)
	}
}

func TestDiscoverVariousArtistsAliasUsesCompilationIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/2/artist" && r.URL.Query().Get("query") != "":
			if !strings.Contains(r.URL.Query().Get("query"), "Various Artists") {
				t.Errorf("various-artists query %q", r.URL.Query().Get("query"))
			}
			writeDiscoveryJSON(w, `{"count":1,"offset":0,"artists":[`+artistLookupBody(testMBID, "Various Artists")+`]}`)
		case r.URL.Path == "/ws/2/artist/"+testMBID:
			writeDiscoveryJSON(w, artistLookupBody(testMBID, "Various Artists"))
		case r.URL.Path == "/ws/2/release-group" && r.URL.Query().Get("query") != "":
			writeDiscoveryJSON(w, `{"count":1,"offset":0,"release-groups":[{"id":"`+testRGID+`","title":"Hits","primary-type":"Album","secondary-types":["Compilation"],"first-release-date":"1999-01-01"}]}`)
		case r.URL.Path == "/ws/2/release" && r.URL.Query().Get("release-group") == testRGID:
			writeDiscoveryJSON(w, releaseListBody(testReleaseID, "Hits"))
		case r.URL.Path == "/ws/2/release/"+testReleaseID:
			writeDiscoveryJSON(w, tracksBody(testReleaseID, "Song", testTrackID, testRecordingID))
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	service, db, scanID := discoveryFixture(t, server)
	fileID := insertDiscoveryFile(t, db, scanID, discoveryFile{
		path: "Music/Various/Hits/01 Song.flac", artist: "Some Band", albumArtist: "Various",
		album: "Hits", title: "Song", track: intPtr(1), disc: intPtr(1),
	})
	result, err := service.DiscoverScan(context.Background(), scanID, []int64{fileID}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Matched != 1 || result.ArtistsAdded != 1 {
		t.Fatal("discover result", result)
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM music_artists`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "Various Artists" {
		t.Fatal("artist", name)
	}
}

func TestDiscoverFeaturedArtistFallsBackToPrimaryName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/2/artist" && r.URL.Query().Get("query") != "":
			if strings.Contains(r.URL.Query().Get("query"), "feat") {
				writeDiscoveryJSON(w, `{"count":0,"offset":0,"artists":[]}`)
				return
			}
			if !strings.Contains(r.URL.Query().Get("query"), "Calvin Harris") {
				t.Errorf("artist query %q", r.URL.Query().Get("query"))
			}
			writeDiscoveryJSON(w, `{"count":1,"offset":0,"artists":[`+artistLookupBody(testMBID, "Calvin Harris")+`]}`)
		case r.URL.Path == "/ws/2/artist/"+testMBID:
			writeDiscoveryJSON(w, artistLookupBody(testMBID, "Calvin Harris"))
		case r.URL.Path == "/ws/2/release-group" && r.URL.Query().Get("query") != "":
			writeDiscoveryJSON(w, `{"count":1,"offset":0,"release-groups":[{"id":"`+testRGID+`","title":"This Is What You Came For","primary-type":"Single","secondary-types":[],"first-release-date":"2016-04-29"}]}`)
		case r.URL.Path == "/ws/2/release" && r.URL.Query().Get("release-group") == testRGID:
			writeDiscoveryJSON(w, releaseListBody(testReleaseID, "This Is What You Came For"))
		case r.URL.Path == "/ws/2/release/"+testReleaseID:
			writeDiscoveryJSON(w, tracksBody(testReleaseID, "This Is What You Came For", testTrackID, testRecordingID))
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	service, db, scanID := discoveryFixture(t, server)
	featured := "Calvin Harris feat. Rihanna"
	fileID := insertDiscoveryFile(t, db, scanID, discoveryFile{
		path: "Music/Calvin Harris/This Is What You Came For/01 Song.flac", artist: featured, albumArtist: featured,
		album: "This Is What You Came For", title: "This Is What You Came For", track: intPtr(1), disc: intPtr(1),
	})
	result, err := service.DiscoverScan(context.Background(), scanID, []int64{fileID}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Matched != 1 {
		t.Fatal("featured artist fallback failed", result)
	}
}

func TestDiscoverFallsBackWhenTaggedArtistMBIDIsUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/2/artist/"+discoveryArtist2:
			http.NotFound(w, r)
		case r.URL.Path == "/ws/2/artist" && r.URL.Query().Get("query") != "":
			writeDiscoveryJSON(w, `{"count":1,"offset":0,"artists":[`+artistLookupBody(testMBID, "Radiohead")+`]}`)
		case r.URL.Path == "/ws/2/artist/"+testMBID:
			writeDiscoveryJSON(w, artistLookupBody(testMBID, "Radiohead"))
		case r.URL.Path == "/ws/2/release-group" && r.URL.Query().Get("query") != "":
			writeDiscoveryJSON(w, `{"count":1,"offset":0,"release-groups":[{"id":"`+testRGID+`","title":"OK Computer","primary-type":"Album","secondary-types":[],"first-release-date":"1997-05-21"}]}`)
		case r.URL.Path == "/ws/2/release" && r.URL.Query().Get("release-group") == testRGID:
			writeDiscoveryJSON(w, releaseListBody(testReleaseID, "OK Computer"))
		case r.URL.Path == "/ws/2/release/"+testReleaseID:
			writeDiscoveryJSON(w, tracksBody(testReleaseID, "Airbag", testTrackID, testRecordingID))
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	service, db, scanID := discoveryFixture(t, server)
	fileID := insertDiscoveryFile(t, db, scanID, discoveryFile{
		path: "Music/Radiohead/OK Computer/01 Airbag.flac", artist: "Radiohead", albumArtist: "Radiohead",
		album: "OK Computer", title: "Airbag", track: intPtr(1), disc: intPtr(1), artistMBID: discoveryArtist2,
	})
	result, err := service.DiscoverScan(context.Background(), scanID, []int64{fileID}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Matched != 1 || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "has no artist") {
		t.Fatal("discover result", result)
	}
}

func TestDiscoverUnknownReleaseGroupIDFallsBackToOwnNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()
	service, db, scanID := discoveryFixture(t, server)
	fileID := insertDiscoveryFile(t, db, scanID, discoveryFile{
		path: "Music/Radiohead/OK Computer/01 Airbag.flac", artist: "Radiohead", albumArtist: "Radiohead",
		album: "OK Computer", title: "Airbag", track: intPtr(1), disc: intPtr(1), rg: testRGID,
	})
	result, err := service.DiscoverScan(context.Background(), scanID, []int64{fileID}, false)
	if err != nil {
		t.Fatal(err)
	}
	// A release-group ID MusicBrainz does not know is not a reason to leave a
	// well-named file unresolved: the local fallback creates the tag-derived
	// entry without claiming the unknown provider identity.
	if result.Matched != 1 || result.Unresolved != 0 || len(result.Warnings) != 0 {
		t.Fatal("discover result", result)
	}
	var albumMBID string
	if err := db.QueryRow(`SELECT musicbrainz_id FROM music_albums`).Scan(&albumMBID); err != nil {
		t.Fatal(err)
	}
	if albumMBID != testRGID {
		t.Fatalf("album identity %q", albumMBID)
	}
	var state string
	if err := db.QueryRow(`SELECT state FROM music_scan_files WHERE id=?`, fileID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "matched" {
		t.Fatal("file state", state)
	}
}

func TestDiscoverAmbiguousExactAlbumStaysForCorrectMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/2/artist" && r.URL.Query().Get("query") != "":
			writeDiscoveryJSON(w, `{"count":1,"offset":0,"artists":[`+artistLookupBody(testMBID, "Radiohead")+`]}`)
		case r.URL.Path == "/ws/2/release-group" && r.URL.Query().Get("query") != "":
			writeDiscoveryJSON(w, `{"count":2,"offset":0,"release-groups":[
				{"id":"`+testRGID+`","title":"Album","primary-type":"Album","secondary-types":[],"first-release-date":"1999-01-01"},
				{"id":"`+discoveryRG2+`","title":"Album","primary-type":"Album","secondary-types":[],"first-release-date":"2003-01-01"}]}`)
		case r.URL.Path == "/ws/2/recording":
			writeDiscoveryJSON(w, emptyRecordingSearchBody())
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	service, db, scanID := discoveryFixture(t, server)
	fileID := insertDiscoveryFile(t, db, scanID, discoveryFile{
		path: "Music/Radiohead/Album/01 Song.flac", artist: "Radiohead", albumArtist: "Radiohead",
		album: "Album", title: "Song", track: intPtr(1), disc: intPtr(1),
	})
	result, err := service.DiscoverScan(context.Background(), scanID, []int64{fileID}, false)
	if err != nil {
		t.Fatal(err)
	}
	// Two identically titled release groups are a genuine provider ambiguity:
	// ReelScarey does not choose automatically and does not fabricate a local
	// identity that would hide the reviewed Correct-match flow.
	if result.Matched != 0 || result.AlbumsAdded != 0 {
		t.Fatal("discover result", result)
	}
	var state, message string
	if err := db.QueryRow(`SELECT state,error FROM music_scan_files WHERE id=?`, fileID).Scan(&state, &message); err != nil {
		t.Fatal(err)
	}
	if state != "unresolved" || !strings.Contains(message, "more than one release group exactly titled") {
		t.Fatalf("scan file %s %q", state, message)
	}
	var albums int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_albums`).Scan(&albums); err != nil || albums != 0 {
		t.Fatalf("ambiguous album created rows %d", albums)
	}
}

func TestDiscoverFindsDiscographyItemOnLaterBoundedPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/2/artist" && r.URL.Query().Get("query") != "":
			writeDiscoveryJSON(w, `{"count":1,"offset":0,"artists":[`+artistLookupBody(testMBID, "Radiohead")+`]}`)
		case r.URL.Path == "/ws/2/artist/"+testMBID:
			writeDiscoveryJSON(w, artistLookupBody(testMBID, "Radiohead"))
		case r.URL.Path == "/ws/2/release-group" && r.URL.Query().Get("query") != "":
			writeDiscoveryJSON(w, `{"count":0,"offset":0,"release-groups":[]}`)
		case r.URL.Path == "/ws/2/release-group" && r.URL.Query().Get("artist") == testMBID:
			switch r.URL.Query().Get("offset") {
			case "0":
				items := make([]string, 0, 100)
				for i := 1; i <= 100; i++ {
					id := fmt.Sprintf("%08d-0000-4000-8000-%012d", i, i)
					items = append(items, `{"id":"`+id+`","title":"Other `+fmt.Sprintf("%03d", i)+`","primary-type":"Album","secondary-types":[],"first-release-date":"2000-01-01"}`)
				}
				writeDiscoveryJSON(w, `{"release-group-count":101,"release-group-offset":0,"release-groups":[`+strings.Join(items, ",")+`]}`)
			case "100":
				writeDiscoveryJSON(w, `{"release-group-count":101,"release-group-offset":100,"release-groups":[{"id":"`+testRGID+`","title":"Later Album","primary-type":"Album","secondary-types":[],"first-release-date":"1997-05-21"}]}`)
			default:
				writeDiscoveryJSON(w, `{"release-group-count":101,"release-group-offset":200,"release-groups":[]}`)
			}
		case r.URL.Path == "/ws/2/release" && r.URL.Query().Get("release-group") == testRGID:
			writeDiscoveryJSON(w, releaseListBody(testReleaseID, "Later Album"))
		case r.URL.Path == "/ws/2/release/"+testReleaseID:
			writeDiscoveryJSON(w, tracksBody(testReleaseID, "Airbag", testTrackID, testRecordingID))
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	service, db, scanID := discoveryFixture(t, server)
	fileID := insertDiscoveryFile(t, db, scanID, discoveryFile{
		path: "Music/Radiohead/Later Album/01 Airbag.flac", artist: "Radiohead", albumArtist: "Radiohead",
		album: "Later Album", title: "Airbag", track: intPtr(1), disc: intPtr(1),
	})
	result, err := service.DiscoverScan(context.Background(), scanID, []int64{fileID}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Matched != 1 || result.AlbumsAdded != 1 {
		t.Fatal("later discography page was not used", result)
	}
}
