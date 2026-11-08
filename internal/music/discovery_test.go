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
	path        string
	artist      string
	albumArtist string
	album       string
	title       string
	track       *int
	disc        *int
	year        *int
	rg          string
	trackMBID   string
	recording   string
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
		VALUES(?,?,100,1,'','flac',1000,0,44100,2,?,?,?,?,?,?,?,'','',?,?,?,'{}','unresolved','No catalog album matches this file.',1,1)`,
		scanID, file.path, file.artist, file.albumArtist, file.album, file.title, file.track, file.disc, file.year,
		file.rg, file.trackMBID, file.recording)
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
			if r.URL.Query().Get("query") != "Radiohead" {
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

func TestDiscoverLeavesAmbiguousOrMissingEvidenceUnresolved(t *testing.T) {
	t.Run("ambiguous artist", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/ws/2/artist" {
				t.Errorf("unexpected request %s", r.URL.String())
				http.NotFound(w, r)
				return
			}
			writeDiscoveryJSON(w, `{"count":2,"offset":0,"artists":[`+
				artistLookupBody(testMBID, "Radiohead")+`,`+artistLookupBody(discoveryArtist2, "Radiohead")+`]}`)
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
		if result.Matched != 0 || result.ArtistsAdded != 0 || result.AlbumsAdded != 0 || result.Unresolved != 1 || result.Remaining != 1 {
			t.Fatal("discover result", result)
		}
		var state, message string
		if err := db.QueryRow(`SELECT state,error FROM music_scan_files WHERE id=?`, fileID).Scan(&state, &message); err != nil {
			t.Fatal(err)
		}
		if state != "unresolved" || !strings.Contains(message, "more than one exact artist") {
			t.Fatal("scan file", state, message)
		}
		var artists int
		if err := db.QueryRow(`SELECT COUNT(*) FROM music_artists`).Scan(&artists); err != nil || artists != 0 {
			t.Fatal("artist rows", artists, err)
		}
	})

	t.Run("missing album", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/ws/2/artist":
				writeDiscoveryJSON(w, `{"count":1,"offset":0,"artists":[`+artistLookupBody(testMBID, "Radiohead")+`]}`)
			case "/ws/2/release-group":
				writeDiscoveryJSON(w, `{"count":0,"offset":0,"release-groups":[]}`)
			default:
				t.Errorf("unexpected request %s", r.URL.String())
				http.NotFound(w, r)
			}
		}))
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
		if result.Matched != 0 || result.Unresolved != 1 || result.Remaining != 1 {
			t.Fatal("discover result", result)
		}
		var message string
		if err := db.QueryRow(`SELECT error FROM music_scan_files WHERE id=?`, fileID).Scan(&message); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(message, "no album exactly titled") {
			t.Fatal("message", message)
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
		if !strings.Contains(message, "Discovery needs album artist and album tags") {
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
	if result.Selected != MaxDiscoverAlbums+1 || result.Matched != 0 || result.Unresolved != MaxDiscoverAlbums+1 ||
		result.Remaining != MaxDiscoverAlbums+1 || result.Deferred != 1 {
		t.Fatal("discover result", result)
	}
	var message string
	if err := db.QueryRow(`SELECT error FROM music_scan_files WHERE id=?`, ids[len(ids)-1]).Scan(&message); err != nil {
		t.Fatal(err)
	}
	if message != "No catalog album matches this file." {
		t.Fatal("deferred file was processed", message)
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
