package music

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Regression tests for the supplied unresolved-file failure patterns. Each
// test drives the real matching workflow against fixtures that reproduce a
// reported failure and asserts the matching outcome, the persisted state and
// that weaker providers never block stronger ones.

// valid-format MusicBrainz UUIDs for regression fixtures (the provider
// parsing rejects anything that is not a real UUID).
const (
	regArtistUUID  = "11111111-1111-4111-8111-111111111111"
	regArtist2UUID = "22222222-2222-4222-8222-222222222222"
	regArtist3UUID = "33333333-3333-4333-8333-333333333333"
	regArtist4UUID = "44444444-4444-4444-8444-444444444444"
	regRecording1  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	regRecording2  = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	regRG1         = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	regRG2         = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	regRelease1    = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	regTrack1      = "ffffffff-ffff-4fff-8fff-ffffffffffff"
)

// TestMatchAllFingerprintMissKeepsRecordingSearchRunning reproduces Example C
// and D: an AcoustID lookup without a confident match must not stop the
// text-based matching; a file with no album tag is resolved through the
// recording path from artist and title alone.
func TestMatchAllFingerprintMissKeepsRecordingSearchRunning(t *testing.T) {
	ctx := context.Background()
	var brainzCalls int32
	brainz := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&brainzCalls, 1)
		switch {
		case strings.HasPrefix(r.URL.Path, "/ws/2/recording"):
			writeDiscoveryJSON(w, recordingSearchBody(regRecording1, "Big Ole Rimz", regArtistUUID, "308 Boyz", regRG1, "Rimz"))
		case r.URL.Path == "/ws/2/release-group/"+regRG1:
			writeDiscoveryJSON(w, releaseLookupBody(regRelease1, regRG1, "Rimz", regArtistUUID, "308 Boyz"))
		case r.URL.Path == "/ws/2/artist/"+regArtistUUID:
			writeDiscoveryJSON(w, artistLookupBody(regArtistUUID, "308 Boyz"))
		case r.URL.Path == "/ws/2/release" && r.URL.Query().Get("release-group") == regRG1:
			writeDiscoveryJSON(w, releaseListBody(regRelease1, "Rimz"))
		case r.URL.Path == "/ws/2/release/"+regRelease1:
			writeDiscoveryJSON(w, tracksBody(regRelease1, "Big Ole Rimz", regTrack1, regRecording1))
		default:
			t.Errorf("unexpected MusicBrainz request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}
	acoustid := func(w http.ResponseWriter, r *http.Request) {
		writeDiscoveryJSON(w, `{"status":"ok","results":[]}`)
	}
	fingerprinter := &fakeFingerprinter{available: true, fingerprint: Fingerprint{DurationSeconds: 200, Value: "AQADtE"}}
	service, db, scanID, dir := acoustidFixture(t, brainz, acoustid, fingerprinter, true)
	relative := "music/308 Boyz/Unknown/308 Boyz - Big Ole Rimz.MP3"
	size, modified := writeFingerprintMedia(t, dir, relative)
	insertFingerprintFile(t, db, scanID, relative, size, modified)

	if _, err := service.RequestMatchAll(ctx, scanID, true); err != nil {
		t.Fatal(err)
	}
	if worked, err := service.ProcessWorkflow(ctx); err != nil || !worked {
		t.Fatalf("workflow %v %v", worked, err)
	}
	scan, err := service.Scan(ctx, scanID)
	if err != nil {
		t.Fatal(err)
	}
	var fileErr string
	if err := db.QueryRow(`SELECT coalesce(error,'') FROM music_scan_files WHERE scan_id=?`, scanID).Scan(&fileErr); err == nil {
		t.Logf("unresolved file error: %s", fileErr)
	}
	if scan.MatchedCount != 1 || scan.UnresolvedCount != 0 {
		t.Fatalf("scan %+v", scan)
	}
	if !strings.Contains(scan.Message, "matched 1 more through MusicBrainz and iTunes recordings") {
		t.Fatalf("message %q", scan.Message)
	}
	if brainzCalls == 0 {
		t.Fatal("recording search never ran after the fingerprint miss")
	}
	var state, artistMBID string
	if err := db.QueryRow(`SELECT state FROM music_scan_files WHERE scan_id=?`, scanID).Scan(&state); err != nil || state != "matched" {
		t.Fatal("file state", state, err)
	}
	if err := db.QueryRow(`SELECT musicbrainz_id FROM music_artists`).Scan(&artistMBID); err != nil || artistMBID != regArtistUUID {
		t.Fatal("artist identity", artistMBID, err)
	}
}

// TestMatchAlliTunesRecoversPlaceholderArtist reproduces the "Artiste
// inconnu" pattern: MusicBrainz has no recording, iTunes confirms the song
// and supplies a canonical artist name, and the local fallback uses it
// instead of the placeholder.
func TestMatchAlliTunesRecoversPlaceholderArtist(t *testing.T) {
	ctx := context.Background()
	var itunesCalls int32
	brainz := func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("MusicBrainz should not be contacted when no album evidence exists: %s", r.URL.String())
		http.NotFound(w, r)
	}
	itunesHandler := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&itunesCalls, 1)
		writeDiscoveryJSON(w, `{"resultCount":1,"results":[{"wrapperType":"track","artistName":"Makaveli","trackName":"Let's B Friends","collectionName":"Americaz Crucify","trackTimeMillis":241000}]}`)
	}
	itunesServer := httptest.NewServer(http.HandlerFunc(itunesHandler))
	defer itunesServer.Close()
	service, db, scanID, _ := acoustidFixture(t, brainz, nil, nil, false)
	service.SetiTunesClient(&iTunesClient{BaseURL: itunesServer.URL, HTTP: itunesServer.Client(), mu: make(chan struct{}, 1)})

	// The file carries no artist evidence anywhere except the file name; the
	// folder names the artist "unknown" (placeholder).
	insertLocalTagRows(t, db, scanID, []localTagRow{{
		path:   "music/Artiste inconnu/Americaz Crucify/Let's B Friends.wma",
		artist: "", albumArtist: "", album: "", title: "",
	}})
	if _, err := service.RequestMatchAll(ctx, scanID, true); err != nil {
		t.Fatal(err)
	}
	if worked, err := service.ProcessWorkflow(ctx); err != nil || !worked {
		t.Fatalf("workflow %v %v", worked, err)
	}
	if atomic.LoadInt32(&itunesCalls) == 0 {
		t.Fatal("iTunes was never consulted")
	}
	var artists, albums, tracks int
	var artistName, artistMBID string
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM music_artists),(SELECT COUNT(*) FROM music_albums),(SELECT COUNT(*) FROM music_tracks)`).
		Scan(&artists, &albums, &tracks); err != nil {
		t.Fatal(err)
	}
	if artists != 1 || albums != 1 || tracks != 1 {
		t.Fatalf("catalog rows %d/%d/%d", artists, albums, tracks)
	}
	if err := db.QueryRow(`SELECT name,musicbrainz_id FROM music_artists`).Scan(&artistName, &artistMBID); err != nil {
		t.Fatal(err)
	}
	if artistName != "Makaveli" || !IsLocalMusicID(artistMBID) {
		t.Fatalf("artist %q %q", artistName, artistMBID)
	}
	var state string
	if err := db.QueryRow(`SELECT state FROM music_scan_files WHERE scan_id=?`, scanID).Scan(&state); err != nil || state != "matched" {
		t.Fatal("file state", state, err)
	}
}

// TestMatchAllAmbiguousRecordingsNeverAutoAccept asserts the guard against
// false positives: two different MusicBrainz recordings with the same artist
// and title must not be auto-accepted even when both score high.
func TestMatchAllAmbiguousRecordingsNeverAutoAccept(t *testing.T) {
	ctx := context.Background()
	brainz := func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/ws/2/recording") {
			writeDiscoveryJSON(w, `{"count":2,"offset":0,"recordings":[`+
				`{"id":"`+regRecording1+`","score":100,"title":"Broken Wings","length":284000,"artist-credit":[{"artist":{"id":"`+regArtistUUID+`","name":"2Pac"}}],"release-groups":[{"id":"`+regRG1+`","title":"Broken Wings","first-release-date":"2000-01-01"}]},`+
				`{"id":"`+regRecording2+`","score":95,"title":"Broken Wings","length":284000,"artist-credit":[{"artist":{"id":"`+regArtist2UUID+`","name":"2Pac"}}],"release-groups":[{"id":"`+regRG2+`","title":"Broken Wings","first-release-date":"2003-01-01"}]}]}`)
			return
		}
		t.Errorf("unexpected MusicBrainz request %s", r.URL.String())
		http.NotFound(w, r)
	}
	service, db, scanID, _ := acoustidFixture(t, brainz, nil, nil, false)
	insertLocalTagRows(t, db, scanID, []localTagRow{{
		path: "music/2Pac/New Folder/2Pac - Broken Wings.mp3", artist: "2Pac", albumArtist: "2Pac",
		album: "New Folder", title: "Broken Wings",
	}})
	if _, err := service.RequestMatchAll(ctx, scanID, true); err != nil {
		t.Fatal(err)
	}
	if worked, err := service.ProcessWorkflow(ctx); err != nil || !worked {
		t.Fatalf("workflow %v %v", worked, err)
	}
	var state, message string
	if err := db.QueryRow(`SELECT state,error FROM music_scan_files WHERE scan_id=?`, scanID).Scan(&state, &message); err != nil {
		t.Fatal(err)
	}
	if state != "matched" {
		t.Fatalf("file did not resolve from its own names: %s %q", state, message)
	}
	// Neither ambiguous recording was adopted: every catalog row must be a
	// tag-derived local identity, never a guessed provider identity.
	var artists, albums int
	var artistMBID, albumMBID string
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM music_artists),(SELECT COUNT(*) FROM music_albums)`).Scan(&artists, &albums); err != nil {
		t.Fatal(err)
	}
	if artists != 1 || albums != 1 {
		t.Fatalf("ambiguity created unexpected catalog rows %d/%d", artists, albums)
	}
	if err := db.QueryRow(`SELECT musicbrainz_id FROM music_artists`).Scan(&artistMBID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT musicbrainz_id FROM music_albums`).Scan(&albumMBID); err != nil {
		t.Fatal(err)
	}
	if !IsLocalMusicID(artistMBID) || !IsLocalMusicID(albumMBID) {
		t.Fatalf("a guessed provider identity was adopted: artist=%q album=%q", artistMBID, albumMBID)
	}
}

// TestMatchAllDifferentArtistsSimilarTitlesNeverCrossMatch asserts that a
// recording by a different artist with the same title is rejected and the
// file resolves from its own names instead.
func TestMatchAllDifferentArtistsSimilarTitlesNeverCrossMatch(t *testing.T) {
	ctx := context.Background()
	brainz := func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/ws/2/recording") {
			// A same-titled recording credited to a different artist.
			writeDiscoveryJSON(w, recordingSearchBody(regRecording1, "All of Me", regArtist2UUID, "John Legend", regRG2, "Love in the Future"))
			return
		}
		http.NotFound(w, r)
	}
	service, db, scanID, _ := acoustidFixture(t, brainz, nil, nil, false)
	insertLocalTagRows(t, db, scanID, []localTagRow{{
		path: "music/Billie/Holiday/Billie - All of Me.mp3", artist: "Billie", albumArtist: "Billie",
		album: "Holiday", title: "All of Me",
	}})
	if _, err := service.RequestMatchAll(ctx, scanID, true); err != nil {
		t.Fatal(err)
	}
	if worked, err := service.ProcessWorkflow(ctx); err != nil || !worked {
		t.Fatalf("workflow %v %v", worked, err)
	}
	var state, message string
	if err := db.QueryRow(`SELECT state,error FROM music_scan_files WHERE scan_id=?`, scanID).Scan(&state, &message); err != nil {
		t.Fatal(err)
	}
	if state != "matched" {
		t.Fatalf("file did not resolve: %q", message)
	}
	var artistMBID string
	if err := db.QueryRow(`SELECT musicbrainz_id FROM music_artists`).Scan(&artistMBID); err != nil {
		t.Fatal(err)
	}
	if !IsLocalMusicID(artistMBID) {
		t.Fatalf("a different artist's recording was adopted: %q", artistMBID)
	}
	var artists, albums, tracks int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM music_artists),(SELECT COUNT(*) FROM music_albums),(SELECT COUNT(*) FROM music_tracks)`).Scan(&artists, &albums, &tracks); err != nil {
		t.Fatal(err)
	}
	if artists != 1 || albums != 1 || tracks != 1 {
		t.Fatalf("catalog rows %d/%d/%d", artists, albums, tracks)
	}
}

// TestMatchAllRepeatedRunsAreIdempotent asserts repeated matching produces no
// duplicate catalog rows and no repeated provider work.
func TestMatchAllRepeatedRunsAreIdempotent(t *testing.T) {
	ctx := context.Background()
	var brainzCalls int32
	brainz := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&brainzCalls, 1)
		switch {
		case strings.HasPrefix(r.URL.Path, "/ws/2/recording"):
			writeDiscoveryJSON(w, recordingSearchBody(regRecording1, "Gone", regArtistUUID, "Bat Mane", regRG1, "Newest"))
		case r.URL.Path == "/ws/2/artist":
			writeDiscoveryJSON(w, `{"count":0,"offset":0,"artists":[]}`)
		case r.URL.Path == "/ws/2/release-group/"+regRG1:
			writeDiscoveryJSON(w, releaseLookupBody(regRelease1, regRG1, "Newest", regArtistUUID, "Bat Mane"))
		case r.URL.Path == "/ws/2/artist/"+regArtistUUID:
			writeDiscoveryJSON(w, artistLookupBody(regArtistUUID, "Bat Mane"))
		case r.URL.Path == "/ws/2/release" && r.URL.Query().Get("release-group") == regRG1:
			writeDiscoveryJSON(w, releaseListBody(regRelease1, "Newest"))
		case r.URL.Path == "/ws/2/release/"+regRelease1:
			writeDiscoveryJSON(w, tracksBody(regRelease1, "Gone", regTrack1, regRecording1))
		default:
			t.Errorf("unexpected MusicBrainz request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}
	service, db, scanID, _ := acoustidFixture(t, brainz, nil, nil, false)
	insertLocalTagRows(t, db, scanID, []localTagRow{
		{path: "music/Bat Mane/Newest/Bat Mane - Gone.mp3", artist: "Bat Mane", albumArtist: "Bat Mane", album: "Newest", title: "Gone"},
	})
	if _, err := service.RequestMatchAll(ctx, scanID, true); err != nil {
		t.Fatal(err)
	}
	if worked, err := service.ProcessWorkflow(ctx); err != nil || !worked {
		t.Fatalf("workflow %v %v", worked, err)
	}
	scan, err := service.Scan(ctx, scanID)
	if err != nil {
		t.Fatal(err)
	}
	if scan.MatchedCount != 1 || scan.UnresolvedCount != 0 {
		t.Fatalf("first run %+v", scan)
	}
	firstCalls := brainzCalls
	// A second request has nothing to do: no file returned to unresolved and
	// the catalog rows are all reused.
	if _, err := service.RequestMatchAll(ctx, scanID, true); err == nil {
		t.Fatal("a fully matched scan accepted a second match request")
	}
	scan, err = service.Scan(ctx, scanID)
	if err != nil {
		t.Fatal(err)
	}
	if scan.MatchedCount != 1 || scan.UnresolvedCount != 0 {
		t.Fatalf("second run %+v", scan)
	}
	var artists, albums, tracks int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM music_artists),(SELECT COUNT(*) FROM music_albums),(SELECT COUNT(*) FROM music_tracks)`).Scan(&artists, &albums, &tracks); err != nil {
		t.Fatal(err)
	}
	if artists != 1 || albums != 1 || tracks != 1 {
		t.Fatalf("rerun duplicated catalog rows %d/%d/%d", artists, albums, tracks)
	}
	if brainzCalls != firstCalls {
		t.Fatalf("rerun issued %d extra MusicBrainz requests", brainzCalls-firstCalls)
	}
}

// TestNamesFallbackGroupsNoAlbumFilesIntoSharedAlbum asserts the
// "Unknown" grouping behavior for files with no album evidence at all.
func TestNamesFallbackGroupsNoAlbumFilesIntoSharedAlbum(t *testing.T) {
	ctx := context.Background()
	service, db, scanID, _ := acoustidFixture(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }, nil, nil, false)
	insertLocalTagRows(t, db, scanID, []localTagRow{
		{path: "music/Bat Mane/Unknown/Bat Mane - Bluffin.mp3", artist: "Bat Mane", albumArtist: "Bat Mane", title: "Bluffin"},
		{path: "music/Bat Mane/Unknown/Bat Mane - Grave Digger.mp3", artist: "Bat Mane", albumArtist: "Bat Mane", title: "Grave Digger"},
	})
	if _, err := service.RequestMatchAll(ctx, scanID, true); err != nil {
		t.Fatal(err)
	}
	if worked, err := service.ProcessWorkflow(ctx); err != nil || !worked {
		t.Fatalf("workflow %v %v", worked, err)
	}
	scan, err := service.Scan(ctx, scanID)
	if err != nil {
		t.Fatal(err)
	}
	if scan.MatchedCount != 2 || scan.UnresolvedCount != 0 {
		t.Fatalf("scan %+v", scan)
	}
	var albums, tracks int
	var title string
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM music_albums),(SELECT COUNT(*) FROM music_tracks)`).Scan(&albums, &tracks); err != nil {
		t.Fatal(err)
	}
	if albums != 1 || tracks != 2 {
		t.Fatalf("album grouping albums=%d tracks=%d", albums, tracks)
	}
	if err := db.QueryRow(`SELECT title FROM music_albums`).Scan(&title); err != nil || title != unknownAlbumTitle {
		t.Fatal("album title", title, err)
	}
	var firstNumber, secondNumber int
	if err := db.QueryRow(`SELECT number FROM music_tracks WHERE title='Bluffin'`).Scan(&firstNumber); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT number FROM music_tracks WHERE title='Grave Digger'`).Scan(&secondNumber); err != nil {
		t.Fatal(err)
	}
	if firstNumber == secondNumber || firstNumber < 1 || secondNumber < 1 {
		t.Fatalf("numberless tracks collided %d/%d", firstNumber, secondNumber)
	}
}

var _ sql.DB
