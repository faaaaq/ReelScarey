package music

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
)

type localTagRow struct {
	path        string
	artist      string
	albumArtist string
	album       string
	title       string
	track       *int
	disc        *int
	artistMBID  string
	albumMBID   string
	trackMBID   string
	recording   string
}

func insertLocalTagRows(t *testing.T, db *sql.DB, scanID int64, rows []localTagRow) []int64 {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO music_scan_files(scan_id,relative_path,size_bytes,modified_at,sha256,format,duration_ms,bitrate,sample_rate,channels,
		tag_artist,tag_album_artist,tag_album,tag_title,tag_track,tag_disc,tag_year,
		tag_musicbrainz_artist,tag_musicbrainz_album,tag_musicbrainz_release_group,tag_musicbrainz_track,tag_musicbrainz_recording,
		tag_json,state,error,created_at,updated_at)
		VALUES(?,?,100,1,'','flac',1000,0,44100,2,?,?,?,?,?,?,NULL,?,'',?,?,?,'{}','unresolved','No catalog album matches this file.',1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		result, err := stmt.Exec(scanID, row.path, row.artist, row.albumArtist, row.album, row.title,
			row.track, row.disc, row.artistMBID, row.albumMBID, row.trackMBID, row.recording)
		if err != nil {
			t.Fatal(err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return ids
}

func newLocalTagScan(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	result, err := db.Exec(`INSERT INTO music_scans(root,state,created_at,updated_at) VALUES('Music','review',1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestMatchAllWellTaggedLibraryUsesNoProviders(t *testing.T) {
	ctx := context.Background()
	const fileCount = 1200
	const artists = 30
	const albums = 150
	var brainzCalls, acoustidCalls int32
	brainz := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&brainzCalls, 1)
		http.Error(w, "unexpected MusicBrainz call", http.StatusInternalServerError)
	}
	acoustid := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&acoustidCalls, 1)
		writeDiscoveryJSON(w, `{"status":"ok","results":[]}`)
	}
	fingerprinter := &fakeFingerprinter{available: true, fingerprint: Fingerprint{DurationSeconds: 200, Value: "AQADtE"}}
	service, db, scanID, _ := acoustidFixture(t, brainz, acoustid, fingerprinter, true)
	rows := make([]localTagRow, 0, fileCount)
	for i := 0; i < fileCount; i++ {
		artist := fmt.Sprintf("Artist %02d", i/40)
		album := fmt.Sprintf("Album %03d", i/8)
		number := i%8 + 1
		rows = append(rows, localTagRow{
			path:        fmt.Sprintf("Music/%s/%s/%02d Song %03d.flac", artist, album, number, i),
			artist:      artist,
			albumArtist: artist,
			album:       album,
			title:       fmt.Sprintf("Song %03d", i),
			track:       intPtr(number),
			disc:        intPtr(1),
		})
	}
	insertLocalTagRows(t, db, scanID, rows)
	if _, err := service.RequestMatchAll(ctx, scanID, true); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if worked, err := service.ProcessWorkflow(ctx); err != nil || !worked {
		t.Fatalf("workflow %v %v", worked, err)
	}
	elapsed := time.Since(started)
	scan, err := service.Scan(ctx, scanID)
	if err != nil {
		t.Fatal(err)
	}
	if scan.State != "review" || scan.Phase != "ready" || scan.MatchedCount != fileCount || scan.UnresolvedCount != 0 {
		t.Fatalf("scan %+v", scan)
	}
	if brainzCalls != 0 || acoustidCalls != 0 || fingerprinter.count() != 0 {
		t.Fatalf("providers were called: musicbrainz=%d acoustid=%d fingerprint=%d", brainzCalls, acoustidCalls, fingerprinter.count())
	}
	if !strings.Contains(scan.Message, fmt.Sprintf("Matched %d files", fileCount)) ||
		!strings.Contains(scan.Message, "without MusicBrainz or AcoustID calls") ||
		!strings.Contains(scan.Message, "0 files remain unresolved") {
		t.Fatalf("message %q", scan.Message)
	}
	var artistRows, albumRows, trackRows int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM music_artists),(SELECT COUNT(*) FROM music_albums),(SELECT COUNT(*) FROM music_tracks)`).
		Scan(&artistRows, &albumRows, &trackRows); err != nil {
		t.Fatal(err)
	}
	if artistRows != artists || albumRows != albums || trackRows != fileCount {
		t.Fatalf("catalog rows artists=%d albums=%d tracks=%d", artistRows, albumRows, trackRows)
	}
	var available int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_artists WHERE status<>'missing'`).Scan(&available); err != nil || available != 0 {
		t.Fatalf("local rows were not missing before registration: %d %v", available, err)
	}
	t.Logf("matched %d files locally in %s with zero provider calls", fileCount, elapsed)
}

func TestMatchAllUsesTaggedMusicBrainzIDsWithoutProvider(t *testing.T) {
	ctx := context.Background()
	var brainzCalls int32
	brainz := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&brainzCalls, 1)
		http.Error(w, "unexpected MusicBrainz call", http.StatusInternalServerError)
	}
	service, db, scanID, _ := acoustidFixture(t, brainz, nil, nil, false)
	rows := []localTagRow{
		{
			path: "Music/Radiohead/OK Computer/01 Airbag.flac", artist: "Radiohead", albumArtist: "Radiohead",
			album: "OK Computer", title: "Airbag", track: intPtr(1), disc: intPtr(1),
			artistMBID: testMBID, albumMBID: testRGID, trackMBID: testTrackID, recording: testRecordingID,
		},
		{
			path: "Music/Radiohead/OK Computer/02 Paranoid Android.flac", artist: "Radiohead", albumArtist: "Radiohead",
			album: "OK Computer", title: "Paranoid Android", track: intPtr(2), disc: intPtr(1),
			artistMBID: testMBID, albumMBID: testRGID, trackMBID: discoveryTrack2, recording: discoveryRecording2,
		},
	}
	insertLocalTagRows(t, db, scanID, rows)
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
	if brainzCalls != 0 {
		t.Fatalf("MusicBrainz was called %d times", brainzCalls)
	}
	var artistMBID, albumMBID string
	if err := db.QueryRow(`SELECT musicbrainz_id FROM music_artists`).Scan(&artistMBID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT musicbrainz_id FROM music_albums`).Scan(&albumMBID); err != nil {
		t.Fatal(err)
	}
	if artistMBID != testMBID || albumMBID != testRGID {
		t.Fatalf("genuine IDs were not used: artist=%q album=%q", artistMBID, albumMBID)
	}
	var trackMBID, recordingID string
	if err := db.QueryRow(`SELECT musicbrainz_id,recording_id FROM music_tracks WHERE number=2`).Scan(&trackMBID, &recordingID); err != nil {
		t.Fatal(err)
	}
	if trackMBID != discoveryTrack2 || recordingID != discoveryRecording2 {
		t.Fatalf("track IDs %q %q", trackMBID, recordingID)
	}
	var localRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_artists WHERE musicbrainz_id LIKE 'local-%'`).Scan(&localRows); err != nil || localRows != 0 {
		t.Fatalf("local artist rows %d %v", localRows, err)
	}
}

func TestLocalRowsAreEnrichedWithLaterGenuineIDs(t *testing.T) {
	ctx := context.Background()
	var brainzCalls int32
	brainz := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&brainzCalls, 1)
		http.Error(w, "unexpected MusicBrainz call", http.StatusInternalServerError)
	}
	service, db, _, _ := acoustidFixture(t, brainz, nil, nil, false)
	firstScan := newLocalTagScan(t, db)
	insertLocalTagRows(t, db, firstScan, []localTagRow{{
		path: "Music/Radiohead/OK Computer/01 Airbag.flac", artist: "Radiohead", albumArtist: "Radiohead",
		album: "OK Computer", title: "Airbag", track: intPtr(1), disc: intPtr(1),
	}})
	if _, err := service.RequestMatchAll(ctx, firstScan, true); err != nil {
		t.Fatal(err)
	}
	if worked, err := service.ProcessWorkflow(ctx); err != nil || !worked {
		t.Fatalf("first workflow %v %v", worked, err)
	}
	var localAlbumID string
	if err := db.QueryRow(`SELECT musicbrainz_id FROM music_albums`).Scan(&localAlbumID); err != nil || !IsLocalMusicID(localAlbumID) {
		t.Fatalf("first album %q %v", localAlbumID, err)
	}

	secondScan := newLocalTagScan(t, db)
	insertLocalTagRows(t, db, secondScan, []localTagRow{{
		path: "Music/Radiohead/OK Computer/01 Airbag.flac", artist: "Radiohead", albumArtist: "Radiohead",
		album: "OK Computer", title: "Airbag", track: intPtr(1), disc: intPtr(1),
		artistMBID: testMBID, albumMBID: testRGID, trackMBID: testTrackID, recording: testRecordingID,
	}})
	if _, err := service.RequestMatchAll(ctx, secondScan, true); err != nil {
		t.Fatal(err)
	}
	if worked, err := service.ProcessWorkflow(ctx); err != nil || !worked {
		t.Fatalf("second workflow %v %v", worked, err)
	}
	scan, err := service.Scan(ctx, secondScan)
	if err != nil || scan.MatchedCount != 1 {
		t.Fatalf("second scan %+v %v", scan, err)
	}
	if brainzCalls != 0 {
		t.Fatalf("enrichment called MusicBrainz %d times", brainzCalls)
	}
	var artistMBID, albumMBID, trackMBID, recordingID string
	if err := db.QueryRow(`SELECT musicbrainz_id FROM music_artists`).Scan(&artistMBID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT musicbrainz_id FROM music_albums`).Scan(&albumMBID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT musicbrainz_id,recording_id FROM music_tracks`).Scan(&trackMBID, &recordingID); err != nil {
		t.Fatal(err)
	}
	if artistMBID != testMBID || albumMBID != testRGID || trackMBID != testTrackID || recordingID != testRecordingID {
		t.Fatalf("local rows were not enriched: artist=%q album=%q track=%q recording=%q", artistMBID, albumMBID, trackMBID, recordingID)
	}
	var artists, albums, tracks int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM music_artists),(SELECT COUNT(*) FROM music_albums),(SELECT COUNT(*) FROM music_tracks)`).
		Scan(&artists, &albums, &tracks); err != nil {
		t.Fatal(err)
	}
	if artists != 1 || albums != 1 || tracks != 1 {
		t.Fatalf("enrichment duplicated rows artists=%d albums=%d tracks=%d", artists, albums, tracks)
	}
}

func TestLocalTagCatalogHandlesMultiDiscDefaultsAndNormalization(t *testing.T) {
	ctx := context.Background()
	var brainzCalls int32
	brainz := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&brainzCalls, 1)
		http.Error(w, "unexpected MusicBrainz call", http.StatusInternalServerError)
	}
	service, db, scanID, _ := acoustidFixture(t, brainz, nil, nil, false)
	rows := []localTagRow{
		{path: "Music/ACDC/Greatest Hits/01 One.flac", artist: "AC/DC", albumArtist: "AC/DC", album: "Greatest Hits", title: "One", track: intPtr(1), disc: intPtr(1)},
		{path: "Music/ACDC/Greatest Hits/02 Two.flac", artist: "AC/DC", albumArtist: "AC/DC", album: "Greatest Hits", title: "Two", track: intPtr(1), disc: intPtr(2)},
		{path: "Music/ACDC/Greatest Hits/03 Three.flac", artist: "AC/DC", albumArtist: "AC/DC", album: "Greatest Hits", title: "Three", track: intPtr(3), disc: nil},
		{path: "Music/ACDC/Greatest Hits/04 Four.flac", artist: "ACDC", albumArtist: "ACDC", album: "Greatest Hits!", title: "Four", track: intPtr(4), disc: nil},
	}
	insertLocalTagRows(t, db, scanID, rows)
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
	if scan.MatchedCount != 4 || scan.UnresolvedCount != 0 {
		t.Fatalf("scan %+v", scan)
	}
	if brainzCalls != 0 {
		t.Fatalf("MusicBrainz was called %d times", brainzCalls)
	}
	var artists, albums, tracks int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM music_artists),(SELECT COUNT(*) FROM music_albums),(SELECT COUNT(*) FROM music_tracks)`).
		Scan(&artists, &albums, &tracks); err != nil {
		t.Fatal(err)
	}
	if artists != 1 || albums != 1 || tracks != 4 {
		t.Fatalf("normalized catalog rows artists=%d albums=%d tracks=%d", artists, albums, tracks)
	}
	got := map[string]string{}
	trackRows, err := db.Query(`SELECT title,disc,number FROM music_tracks`)
	if err != nil {
		t.Fatal(err)
	}
	defer trackRows.Close()
	for trackRows.Next() {
		var title string
		var disc, number int
		if err := trackRows.Scan(&title, &disc, &number); err != nil {
			t.Fatal(err)
		}
		got[title] = fmt.Sprintf("%d/%d", disc, number)
	}
	want := map[string]string{"One": "1/1", "Two": "2/1", "Three": "1/3", "Four": "1/4"}
	for title, position := range want {
		if got[title] != position {
			t.Fatalf("track %q position %q want %q", title, got[title], position)
		}
	}

	// A missing disc tag with an ambiguous track number and a different title
	// is never guessed into an occupied position or renumbered.
	conflictScan := newLocalTagScan(t, db)
	insertLocalTagRows(t, db, conflictScan, []localTagRow{
		{path: "Music/ACDC/Greatest Hits/09 Conflict.flac", artist: "AC/DC", albumArtist: "AC/DC", album: "Greatest Hits", title: "Different", track: intPtr(1), disc: nil},
	})
	if _, err := service.RequestMatchAll(ctx, conflictScan, true); err != nil {
		t.Fatal(err)
	}
	if worked, err := service.ProcessWorkflow(ctx); err != nil || !worked {
		t.Fatalf("conflict workflow %v %v", worked, err)
	}
	conflict, err := service.Scan(ctx, conflictScan)
	if err != nil {
		t.Fatal(err)
	}
	if conflict.MatchedCount != 0 || conflict.UnresolvedCount != 1 || !strings.Contains(conflict.Message, "Audio fingerprint matching was skipped") {
		t.Fatalf("conflict scan %+v", conflict)
	}
	var conflictMessage string
	if err := db.QueryRow(`SELECT error FROM music_scan_files WHERE scan_id=?`, conflictScan).Scan(&conflictMessage); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(conflictMessage, "already uses disc 1 track 1") {
		t.Fatalf("conflict message %q", conflictMessage)
	}
	var afterTracks int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_tracks`).Scan(&afterTracks); err != nil || afterTracks != 4 {
		t.Fatalf("conflict changed tracks: %d %v", afterTracks, err)
	}

	// A rerun over the same tags reuses every catalog row.
	rerunScan := newLocalTagScan(t, db)
	insertLocalTagRows(t, db, rerunScan, rows)
	if _, err := service.RequestMatchAll(ctx, rerunScan, true); err != nil {
		t.Fatal(err)
	}
	if worked, err := service.ProcessWorkflow(ctx); err != nil || !worked {
		t.Fatalf("rerun workflow %v %v", worked, err)
	}
	rerun, err := service.Scan(ctx, rerunScan)
	if err != nil {
		t.Fatal(err)
	}
	if rerun.MatchedCount != 4 || rerun.UnresolvedCount != 0 {
		t.Fatalf("rerun scan %+v", rerun)
	}
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM music_artists),(SELECT COUNT(*) FROM music_albums),(SELECT COUNT(*) FROM music_tracks)`).
		Scan(&artists, &albums, &tracks); err != nil {
		t.Fatal(err)
	}
	if artists != 1 || albums != 1 || tracks != 4 {
		t.Fatalf("rerun duplicated catalog rows artists=%d albums=%d tracks=%d", artists, albums, tracks)
	}
	if brainzCalls != 0 {
		t.Fatalf("rerun called MusicBrainz %d times", brainzCalls)
	}
}

type pathFingerprinter struct {
	prints map[string]Fingerprint
	calls  int32
}

func (f *pathFingerprinter) Available(context.Context) error { return nil }

func (f *pathFingerprinter) Fingerprint(_ context.Context, filename string) (Fingerprint, error) {
	atomic.AddInt32(&f.calls, 1)
	if print, ok := f.prints[filepath.Base(filename)]; ok {
		return print, nil
	}
	return Fingerprint{}, &Error{Code: "music_fingerprint_failed", Message: "No fixture fingerprint."}
}

func TestMatchAllMixedTagsFallThroughToAcoustIDAndMusicBrainz(t *testing.T) {
	ctx := context.Background()
	var brainzCalls int32
	brainz := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&brainzCalls, 1)
		acoustidMusicBrainzHandler(t, testRecordingID)(w, r)
	}
	var acoustidCalls int32
	acoustid := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&acoustidCalls, 1)
		if r.URL.Query().Get("duration") == "284" {
			writeDiscoveryJSON(w, acoustidResultBody(0.97, testRecordingID, "Airbag", testRGID, "OK Computer"))
			return
		}
		writeDiscoveryJSON(w, `{"status":"ok","results":[]}`)
	}
	fingerprinter := &pathFingerprinter{prints: map[string]Fingerprint{
		"acoustid.flac": {DurationSeconds: 284, Value: "AQADtE"},
		"discover.flac": {DurationSeconds: 123, Value: "AQADtF"},
	}}
	service, db, scanID, media := acoustidFixture(t, brainz, acoustid, nil, true)
	service.SetFingerprinter(fingerprinter)

	insertLocalTagRows(t, db, scanID, []localTagRow{{
		path: "Music/Local Artist/Local Album/01 Local Song.flac", artist: "Local Artist", albumArtist: "Local Artist",
		album: "Local Album", title: "Local Song", track: intPtr(1), disc: intPtr(1),
	}})

	acoustidPath := "Music/Unknown/acoustid.flac"
	size, modified := writeFingerprintMedia(t, media, acoustidPath)
	insertFingerprintFile(t, db, scanID, acoustidPath, size, modified)

	discoverPath := "Music/Unknown/discover.flac"
	discoverSize, discoverModified := writeFingerprintMedia(t, media, discoverPath)
	discoverID := insertFingerprintFile(t, db, scanID, discoverPath, discoverSize, discoverModified)
	if _, err := db.Exec(`UPDATE music_scan_files SET tag_artist='Radiohead',tag_album_artist='Radiohead',tag_album='OK Computer',tag_title='',tag_track=1,tag_disc=1,tag_musicbrainz_release_group=? WHERE id=?`, testRGID, discoverID); err != nil {
		t.Fatal(err)
	}

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
	if scan.MatchedCount != 3 || scan.UnresolvedCount != 0 {
		t.Fatalf("mixed scan %+v", scan)
	}
	if !strings.Contains(scan.Message, "Matched 1 files") ||
		!strings.Contains(scan.Message, "1 more by audio fingerprint") ||
		!strings.Contains(scan.Message, "discovered 1 through MusicBrainz") {
		t.Fatalf("mixed message %q", scan.Message)
	}
	if brainzCalls == 0 || acoustidCalls != 2 {
		t.Fatalf("provider calls musicbrainz=%d acoustid=%d", brainzCalls, acoustidCalls)
	}
	var localAlbums int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_albums WHERE musicbrainz_id LIKE 'local-%'`).Scan(&localAlbums); err != nil || localAlbums != 1 {
		t.Fatalf("local albums %d %v", localAlbums, err)
	}
}

func TestAcoustIDReusesLocalAlbumInsteadOfDuplicatingIt(t *testing.T) {
	ctx := context.Background()
	brainz := acoustidMusicBrainzHandler(t, testRecordingID)
	acoustid := func(w http.ResponseWriter, r *http.Request) {
		writeDiscoveryJSON(w, acoustidResultBody(0.97, testRecordingID, "Airbag", testRGID, "OK Computer"))
	}
	fingerprinter := &fakeFingerprinter{available: true, fingerprint: Fingerprint{DurationSeconds: 284, Value: "AQADtE"}}
	service, db, scanID, media := acoustidFixture(t, brainz, acoustid, fingerprinter, true)
	insertLocalTagRows(t, db, scanID, []localTagRow{{
		path: "Music/Radiohead/OK Computer/01 Airbag.flac", artist: "Radiohead", albumArtist: "Radiohead",
		album: "OK Computer", title: "Airbag", track: intPtr(1), disc: intPtr(1),
	}})
	relative := "Music/Unknown/track.flac"
	size, modified := writeFingerprintMedia(t, media, relative)
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
	if scan.MatchedCount != 2 || scan.UnresolvedCount != 0 {
		t.Fatalf("scan %+v", scan)
	}
	var artists, albums, tracks int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM music_artists),(SELECT COUNT(*) FROM music_albums),(SELECT COUNT(*) FROM music_tracks)`).
		Scan(&artists, &albums, &tracks); err != nil {
		t.Fatal(err)
	}
	if artists != 1 || albums != 1 || tracks != 1 {
		t.Fatalf("provider discovery duplicated the local album: artists=%d albums=%d tracks=%d", artists, albums, tracks)
	}
}

func TestFingerprintScanUsesAcoustIDIDsAndTagsLocally(t *testing.T) {
	ctx := context.Background()
	var brainzCalls int32
	brainz := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&brainzCalls, 1)
		http.Error(w, "unexpected MusicBrainz call", http.StatusInternalServerError)
	}
	acoustid := func(w http.ResponseWriter, r *http.Request) {
		writeDiscoveryJSON(w, acoustidResultBody(0.97, testRecordingID, "Airbag", testRGID, "OK Computer"))
	}
	fingerprinter := &fakeFingerprinter{available: true, fingerprint: Fingerprint{DurationSeconds: 284, Value: "AQADtE"}}
	service, db, scanID, media := acoustidFixture(t, brainz, acoustid, fingerprinter, true)
	relative := "Music/Radiohead/OK Computer/01 Airbag.flac"
	size, modified := writeFingerprintMedia(t, media, relative)
	fileID := insertFingerprintFile(t, db, scanID, relative, size, modified)
	if _, err := db.Exec(`UPDATE music_scan_files SET tag_artist='Radiohead',tag_album_artist='Radiohead',tag_album='OK Computer',tag_title='Airbag',tag_track=1,tag_disc=1 WHERE id=?`, fileID); err != nil {
		t.Fatal(err)
	}
	result, err := service.FingerprintScan(ctx, scanID, []int64{fileID}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Matched != 1 || result.ProviderCalls != 1 || brainzCalls != 0 {
		t.Fatalf("fingerprint result %+v brainz=%d", result, brainzCalls)
	}
	var albumMBID, trackMBID, recordingID string
	if err := db.QueryRow(`SELECT musicbrainz_id FROM music_albums`).Scan(&albumMBID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT musicbrainz_id,recording_id FROM music_tracks`).Scan(&trackMBID, &recordingID); err != nil {
		t.Fatal(err)
	}
	if albumMBID != testRGID || recordingID != testRecordingID || !IsLocalMusicID(trackMBID) {
		t.Fatalf("catalog IDs album=%q track=%q recording=%q", albumMBID, trackMBID, recordingID)
	}
}

func TestMatchWorkflowDiscoversMusicBrainzWhenAcoustIDUnavailable(t *testing.T) {
	ctx := context.Background()
	var brainzCalls int32
	brainz := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&brainzCalls, 1)
		acoustidMusicBrainzHandler(t, testRecordingID)(w, r)
	}
	service, db, scanID, _ := acoustidFixture(t, brainz, nil, nil, false)
	insertLocalTagRows(t, db, scanID, []localTagRow{{
		path: "Music/Radiohead/OK Computer/01 Airbag.flac", artist: "Radiohead", albumArtist: "Radiohead",
		album: "OK Computer", title: "", track: intPtr(1), disc: intPtr(1), albumMBID: testRGID,
	}})
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
		t.Fatalf("scan %+v", scan)
	}
	if !strings.Contains(scan.Message, "Audio fingerprint matching was skipped") ||
		!strings.Contains(scan.Message, "discovered 1 through MusicBrainz") {
		t.Fatalf("message %q", scan.Message)
	}
	if brainzCalls == 0 {
		t.Fatal("MusicBrainz discovery did not run after AcoustID was skipped")
	}
}

func TestFinalizeLocalTagRowsMovesAndNeverWritesSyntheticIDs(t *testing.T) {
	ctx := context.Background()
	service, db, media := editFixture(t)
	// Replace the canonical fixture rows so the whole path is tag-derived.
	if _, err := db.Exec(`DELETE FROM music_tracks; DELETE FROM music_albums; DELETE FROM music_artists`); err != nil {
		t.Fatal(err)
	}
	service.SetProber(&mapCountingProber{tags: map[string]AudioTags{
		"Song.flac": {
			Artist: "Local Artist", AlbumArtist: "Local Artist", Album: "Local Album", Title: "Song",
			Track: intPtr(1), Disc: intPtr(1), Format: "flac", DurationMS: 1000,
		},
	}})
	writeScanFixture(t, media, "Music/Local Artist/Local Album/Song.flac")
	scan, err := service.StartScan(ctx, "Music")
	if err != nil {
		t.Fatal(err)
	}
	scan = waitForScan(t, ctx, service, scan.ID)
	if scan.UnresolvedCount != 1 {
		t.Fatalf("scan did not start unresolved: %+v", scan)
	}
	if _, err := service.RequestMatchAll(ctx, scan.ID, true); err != nil {
		t.Fatal(err)
	}
	if worked, err := service.ProcessWorkflow(ctx); err != nil || !worked {
		t.Fatalf("match workflow %v %v", worked, err)
	}
	scan, err = service.Scan(ctx, scan.ID)
	if err != nil || scan.MatchedCount != 1 {
		t.Fatalf("local match %+v %v", scan, err)
	}
	var artistMBID, albumMBID, trackMBID string
	if err := db.QueryRow(`SELECT musicbrainz_id FROM music_artists`).Scan(&artistMBID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT musicbrainz_id FROM music_albums`).Scan(&albumMBID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT musicbrainz_id FROM music_tracks`).Scan(&trackMBID); err != nil {
		t.Fatal(err)
	}
	if !IsLocalMusicID(artistMBID) || !IsLocalMusicID(albumMBID) || !IsLocalMusicID(trackMBID) {
		t.Fatalf("expected local identities: %q %q %q", artistMBID, albumMBID, trackMBID)
	}
	if _, err := service.RequestFinalize(ctx, scan.ID, true); err != nil {
		t.Fatal(err)
	}
	if worked, err := service.ProcessWorkflow(ctx); err != nil || !worked {
		t.Fatalf("finalize workflow %v %v", worked, err)
	}
	scan, err = service.Scan(ctx, scan.ID)
	if err != nil || scan.State != "completed" || scan.Phase != "completed" || scan.RegisteredCount != 1 {
		t.Fatalf("finalize %+v %v", scan, err)
	}
	var relative, origin, tagJSON string
	if err := db.QueryRow(`SELECT relative_path,origin,tag_json FROM music_files`).Scan(&relative, &origin, &tagJSON); err != nil {
		t.Fatal(err)
	}
	if relative != "Music/Local Artist/Local Album/01 Song.flac" || origin != "existing" {
		t.Fatalf("registered file %q %q", relative, origin)
	}
	if strings.Contains(strings.ToLower(tagJSON), "local-") {
		t.Fatalf("synthetic identity reached the file tags: %s", tagJSON)
	}
	var editTags string
	if err := db.QueryRow(`SELECT tags_json FROM music_edit_files`).Scan(&editTags); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(editTags), "local-") {
		t.Fatalf("synthetic identity reached an edit tag plan: %s", editTags)
	}
	var tags EditTags
	if err := json.Unmarshal([]byte(editTags), &tags); err != nil {
		t.Fatal(err)
	}
	if tags.MusicBrainzArtistID != "" || tags.MusicBrainzReleaseGroupID != "" ||
		tags.MusicBrainzTrackID != "" || tags.MusicBrainzRecordingID != "" {
		t.Fatalf("synthetic IDs were planned as MusicBrainz tags: %+v", tags)
	}
	if tags.Artist != "Local Artist" || tags.Album != "Local Album" || tags.Title != "Song" || tags.Track != 1 || tags.Disc != 1 {
		t.Fatalf("tagged names were not used: %+v", tags)
	}
	if _, err := os.Stat(filepath.Join(media, filepath.FromSlash(relative))); err != nil {
		t.Fatal("organized destination missing", err)
	}
	var artistStatus, albumStatus string
	if err := db.QueryRow(`SELECT status FROM music_artists`).Scan(&artistStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status FROM music_albums`).Scan(&albumStatus); err != nil {
		t.Fatal(err)
	}
	if artistStatus != "available" || albumStatus != "available" {
		t.Fatalf("registration did not mark local rows available: %s %s", artistStatus, albumStatus)
	}
}

func TestLocalTagRetagWritesOnlyGenuineMusicBrainzIDs(t *testing.T) {
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " is not installed")
		}
	}
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	media := t.TempDir()
	service := NewService(db, media)
	service.SetProber(FFprobeTagger{})

	file := ScanFile{
		Artist: "Local Artist", AlbumArtist: "Local Artist", Album: "Local Album", Title: "Song",
		Track: intPtr(1), Disc: intPtr(1), Format: "flac", DurationMS: 1000,
	}
	catalog := newLocalCatalog(service)
	artistID, albumID, trackID, err := catalog.matchIdentity(ctx, file, tagIdentityFor(file))
	if err != nil {
		t.Fatal(err)
	}
	sourceRel := "Music/Local Artist/Local Album/Song.flac"
	source := filepath.Join(media, filepath.FromSlash(sourceRel))
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-c:a", "flac", "-y", source)
	if err := cmd.Run(); err != nil {
		t.Skip("could not generate a FLAC fixture", err)
	}
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_files(id,artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,duration_ms,tag_json,imported_at,origin,source_name)
		VALUES(1,?,?,?,'Music',?,?,'sha','flac',1000,'{}',1,'existing','Song.flac')`, artistID, albumID, trackID, sourceRel, info.Size()); err != nil {
		t.Fatal(err)
	}
	plan, err := service.PreviewEdits(ctx, []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Files) != 1 || plan.Files[0].Action != "move_retag" {
		t.Fatalf("plan %+v", plan)
	}
	tags := plan.Files[0].Tags
	if tags.MusicBrainzArtistID != "" || tags.MusicBrainzReleaseGroupID != "" ||
		tags.MusicBrainzTrackID != "" || tags.MusicBrainzRecordingID != "" {
		t.Fatalf("preview planned synthetic MBIDs: %+v", tags)
	}
	job, err := service.ApplyEdits(ctx, []int64{1}, plan.PlanHash, true)
	if err != nil {
		t.Fatal(err)
	}
	job = waitForEditJob(t, ctx, service, job.ID)
	if job.State != "completed" {
		t.Fatalf("edit job %+v", job)
	}
	destination := filepath.Join(media, filepath.FromSlash(plan.Files[0].DestinationPath))
	actual, err := (FFprobeTagger{}).ProbeAudio(ctx, destination)
	if err != nil {
		t.Fatal(err)
	}
	if actual.Artist != "Local Artist" || actual.Album != "Local Album" || actual.Title != "Song" ||
		actual.Track == nil || *actual.Track != 1 || actual.Disc == nil || *actual.Disc != 1 {
		t.Fatalf("written tags %+v", actual)
	}
	if actual.MusicBrainzArtistID != "" || actual.MusicBrainzAlbumID != "" ||
		actual.MusicBrainzReleaseGroupID != "" || actual.MusicBrainzTrackID != "" || actual.MusicBrainzRecordingID != "" {
		t.Fatalf("synthetic or unexpected MBIDs were written: %+v", actual)
	}
	if _, err := os.Stat(source + ".reelscarey-backup"); err != nil {
		t.Fatal("retained backup missing", err)
	}
}
