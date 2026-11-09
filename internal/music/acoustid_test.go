package music

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

const acoustidTestKey = "test-acoustid-application-key"

type fakeFingerprinter struct {
	available   bool
	fingerprint Fingerprint
	err         error
	calls       int32
}

func (f *fakeFingerprinter) Available(context.Context) error {
	if !f.available {
		return &Error{Code: "music_fingerprint_unavailable", Message: "Chromaprint fpcalc is not available. Use the Docker image."}
	}
	return nil
}

func (f *fakeFingerprinter) Fingerprint(ctx context.Context, _ string) (Fingerprint, error) {
	atomic.AddInt32(&f.calls, 1)
	if err := f.Available(ctx); err != nil {
		return Fingerprint{}, err
	}
	if f.err != nil {
		return Fingerprint{}, f.err
	}
	return f.fingerprint, nil
}

func (f *fakeFingerprinter) count() int { return int(atomic.LoadInt32(&f.calls)) }

func acoustidFixture(t *testing.T, brainz http.HandlerFunc, acoustid http.HandlerFunc, fingerprinter *fakeFingerprinter, saveKey bool) (*Service, *sql.DB, int64, string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
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
	if brainz == nil {
		brainz = func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }
	}
	brainzServer := httptest.NewServer(brainz)
	t.Cleanup(brainzServer.Close)
	service := NewServiceWithClient(db, newClient(brainzServer.URL+"/ws/2", brainzServer.Client(), 0), dir)
	secrets, err := securestore.New(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	service.SetSecrets(secrets)
	service.SetFingerprinter(fingerprinter)
	if acoustid != nil {
		acoustidServer := httptest.NewServer(acoustid)
		t.Cleanup(acoustidServer.Close)
		service.setAcoustIDClient(newAcoustID(acoustidServer.URL+"/v2/lookup", acoustidServer.Client(), 0))
	}
	if saveKey {
		if err := service.SaveAcoustIDKey(ctx, acoustidTestKey); err != nil {
			t.Fatal(err)
		}
	}
	return service, db, scanID, dir
}

func writeFingerprintMedia(t *testing.T, dir, relative string) (int64, int64) {
	t.Helper()
	absolute := filepath.Join(dir, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(strings.Repeat("audio", 512)), 0o644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000000, 0)
	if err := os.Chtimes(absolute, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size(), info.ModTime().Unix()
}

func insertFingerprintFile(t *testing.T, db *sql.DB, scanID int64, relative string, size, modified int64) int64 {
	t.Helper()
	result, err := db.Exec(`INSERT INTO music_scan_files(scan_id,relative_path,size_bytes,modified_at,sha256,format,duration_ms,bitrate,sample_rate,channels,
		tag_artist,tag_album_artist,tag_album,tag_title,tag_track,tag_disc,tag_year,
		tag_musicbrainz_artist,tag_musicbrainz_album,tag_musicbrainz_release_group,tag_musicbrainz_track,tag_musicbrainz_recording,
		tag_json,state,error,created_at,updated_at)
		VALUES(?,?,?,?,'','flac',284000,0,44100,2,'','','','',NULL,NULL,NULL,'','','','','','{}','unresolved','No catalog album matches this file.',1,1)`,
		scanID, relative, size, modified)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func acoustidResultBody(score float64, recordingID, recordingTitle, groupID, groupTitle string) string {
	return fmt.Sprintf(`{"status":"ok","results":[{"id":"result-1","score":%g,"recordings":[{"id":%q,"title":%q,"artists":[{"id":%q,"name":"Radiohead"}],"releasegroups":[{"id":%q,"title":%q,"type":"Album","artists":[{"id":%q,"name":"Radiohead"}]}]}]}]}`,
		score, recordingID, recordingTitle, testMBID, groupID, groupTitle, testMBID)
}

func acoustidMusicBrainzHandler(t *testing.T, recordingID string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/2/release-group/"+testRGID:
			writeDiscoveryJSON(w, releaseLookupBody(testReleaseID, testRGID, "OK Computer", testMBID, "Radiohead"))
		case r.URL.Path == "/ws/2/artist/"+testMBID:
			writeDiscoveryJSON(w, artistLookupBody(testMBID, "Radiohead"))
		case r.URL.Path == "/ws/2/release" && r.URL.Query().Get("release-group") == testRGID:
			writeDiscoveryJSON(w, releaseListBody(testReleaseID, "OK Computer"))
		case r.URL.Path == "/ws/2/release/"+testReleaseID:
			writeDiscoveryJSON(w, tracksBody(testReleaseID, "Airbag", testTrackID, recordingID))
		default:
			t.Errorf("unexpected MusicBrainz request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}
}

func TestFingerprintScanMatchesStrongUniqueReleaseGroup(t *testing.T) {
	fingerprinter := &fakeFingerprinter{available: true, fingerprint: Fingerprint{DurationSeconds: 284, Value: "AQADtE"}}
	var calls int32
	acoustid := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		query := r.URL.Query()
		if query.Get("client") != acoustidTestKey || query.Get("duration") != "284" ||
			query.Get("fingerprint") != "AQADtE" || query.Get("meta") != "recordings+releasegroups" {
			t.Errorf("AcoustID query %s", r.URL.RawQuery)
		}
		writeDiscoveryJSON(w, acoustidResultBody(0.99, testRecordingID, "Airbag", testRGID, "OK Computer"))
	}
	service, db, scanID, dir := acoustidFixture(t, acoustidMusicBrainzHandler(t, testRecordingID), acoustid, fingerprinter, true)
	relative := "Music/Radiohead/OK Computer/01 Airbag.flac"
	size, modified := writeFingerprintMedia(t, dir, relative)
	fileID := insertFingerprintFile(t, db, scanID, relative, size, modified)

	result, err := service.FingerprintScan(context.Background(), scanID, []int64{fileID}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Selected != 1 || result.Matched != 1 || result.Fingerprinted != 1 || result.ProviderCalls != 1 ||
		result.ArtistsAdded != 1 || result.AlbumsAdded != 1 || result.Unresolved != 0 || result.Remaining != 0 || result.Deferred != 0 || len(result.Warnings) != 0 {
		t.Fatalf("fingerprint result %+v", result)
	}
	scan, err := service.Scan(context.Background(), scanID)
	if err != nil {
		t.Fatal(err)
	}
	if scan.FileCount != 1 || scan.MatchedCount != 1 || scan.UnresolvedCount != 0 {
		t.Fatalf("scan counts %+v", scan)
	}
	var artistID, albumID, trackID sql.NullInt64
	var state, message string
	if err := db.QueryRow(`SELECT artist_id,album_id,track_id,state,error FROM music_scan_files WHERE id=?`, fileID).
		Scan(&artistID, &albumID, &trackID, &state, &message); err != nil {
		t.Fatal(err)
	}
	if !artistID.Valid || !albumID.Valid || !trackID.Valid || state != "matched" || message != "" {
		t.Fatalf("scan file %v %v %v %q %q", artistID, albumID, trackID, state, message)
	}
	var artists, albums, tracks, files int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM music_artists),(SELECT COUNT(*) FROM music_albums),(SELECT COUNT(*) FROM music_tracks),(SELECT COUNT(*) FROM music_files)`).
		Scan(&artists, &albums, &tracks, &files); err != nil {
		t.Fatal(err)
	}
	if artists != 1 || albums != 1 || tracks != 1 || files != 0 {
		t.Fatalf("catalog counts %d %d %d %d", artists, albums, tracks, files)
	}
	again, err := service.FingerprintScan(context.Background(), scanID, []int64{fileID}, false)
	if err != nil || again.Selected != 0 || again.Remaining != 0 {
		t.Fatalf("rerun %+v %v", again, err)
	}
}

func TestResolveAcoustIDPolicy(t *testing.T) {
	groupA := AcoustIDReleaseGroup{ID: testRGID, Title: "OK Computer", Type: "Album"}
	groupB := AcoustIDReleaseGroup{ID: discoveryRG2, Title: "The Bends", Type: "Album"}
	recordingA := testRecordingID
	recordingB := discoveryRecording2

	tests := []struct {
		name     string
		input    []AcoustIDCandidate
		code     string
		recordID string
	}{
		{
			name:  "low score stays unresolved",
			input: []AcoustIDCandidate{{Score: 0.84, RecordingID: recordingA, ReleaseGroups: []AcoustIDReleaseGroup{groupA}}},
			code:  "music_acoustid_no_confident_match",
		},
		{
			name: "multiple release groups stay unresolved",
			input: []AcoustIDCandidate{
				{Score: 0.99, RecordingID: recordingA, ReleaseGroups: []AcoustIDReleaseGroup{groupA}},
				{Score: 0.99, RecordingID: recordingB, ReleaseGroups: []AcoustIDReleaseGroup{groupB}},
			},
			code: "music_acoustid_ambiguous_release_group",
		},
		{
			name:  "no release group stays unresolved",
			input: []AcoustIDCandidate{{Score: 0.99, RecordingID: recordingA}},
			code:  "music_acoustid_no_release_group",
		},
		{
			name: "highest score recording wins",
			input: []AcoustIDCandidate{
				{Score: 0.9, RecordingID: recordingB, ReleaseGroups: []AcoustIDReleaseGroup{groupA}},
				{Score: 0.99, RecordingID: recordingA, ReleaseGroups: []AcoustIDReleaseGroup{groupA}},
			},
			recordID: recordingA,
		},
		{
			name: "equal scores fall back to tag evidence",
			input: []AcoustIDCandidate{
				{Score: 0.99, RecordingID: recordingA, ReleaseGroups: []AcoustIDReleaseGroup{groupA}},
				{Score: 0.99, RecordingID: recordingB, ReleaseGroups: []AcoustIDReleaseGroup{groupA}},
			},
			recordID: "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			match, err := ResolveAcoustID(test.input)
			if test.code != "" {
				var provider *Error
				if !errors.As(err, &provider) || provider.Code != test.code {
					t.Fatalf("error %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if match.ReleaseGroupID != strings.ToLower(testRGID) || match.RecordingID != test.recordID {
				t.Fatalf("match %+v", match)
			}
		})
	}
}

func TestResolveAcoustIDUsesTaggedReleaseGroupOrExactAlbum(t *testing.T) {
	groupA := AcoustIDReleaseGroup{ID: testRGID, Title: "OK Computer", Type: "Album"}
	groupB := AcoustIDReleaseGroup{ID: discoveryRG2, Title: "The Bends", Type: "Album"}
	input := []AcoustIDCandidate{
		{Score: 0.99, RecordingID: testRecordingID, ReleaseGroups: []AcoustIDReleaseGroup{groupA}},
		{Score: 0.98, RecordingID: discoveryRecording2, ReleaseGroups: []AcoustIDReleaseGroup{groupB}},
	}
	if _, err := ResolveAcoustID(input); err == nil {
		t.Fatal("multiple release groups resolved without a hint")
	}
	byAlbum, err := ResolveAcoustIDForAlbum(input, "", "OK Computer")
	if err != nil || byAlbum.ReleaseGroupID != strings.ToLower(testRGID) || byAlbum.RecordingID != testRecordingID {
		t.Fatalf("exact album hint did not disambiguate: %+v %v", byAlbum, err)
	}
	byTag, err := ResolveAcoustIDForAlbum(input, discoveryRG2, "")
	if err != nil || byTag.ReleaseGroupID != strings.ToLower(discoveryRG2) || byTag.RecordingID != discoveryRecording2 {
		t.Fatalf("tagged release-group hint did not disambiguate: %+v %v", byTag, err)
	}
}

func TestAcoustIDClientErrors(t *testing.T) {
	t.Run("invalid key", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			writeDiscoveryJSON(w, `{"status":"error","error":{"code":2,"message":"invalid API key"}}`)
		}))
		defer server.Close()
		client := newAcoustID(server.URL, server.Client(), 0)
		_, err := client.Lookup(context.Background(), acoustidTestKey, 100, "AQADtE")
		var provider *Error
		if !errors.As(err, &provider) || provider.Code != "music_acoustid_invalid_key" {
			t.Fatal(err)
		}
	})
	t.Run("rate limit", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer server.Close()
		client := newAcoustID(server.URL, server.Client(), 0)
		_, err := client.Lookup(context.Background(), acoustidTestKey, 100, "AQADtE")
		var provider *Error
		if !errors.As(err, &provider) || provider.Code != "music_acoustid_rate_limited" || provider.RetryAfter != 7 {
			t.Fatalf("error %v", err)
		}
	})
	t.Run("no match", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeDiscoveryJSON(w, `{"status":"ok","results":[]}`)
		}))
		defer server.Close()
		client := newAcoustID(server.URL, server.Client(), 0)
		_, err := client.Lookup(context.Background(), acoustidTestKey, 100, "AQADtE")
		var provider *Error
		if !errors.As(err, &provider) || provider.Code != "music_acoustid_no_match" {
			t.Fatal(err)
		}
	})
	t.Run("parses candidates", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeDiscoveryJSON(w, acoustidResultBody(0.97, testRecordingID, "Airbag", testRGID, "OK Computer"))
		}))
		defer server.Close()
		client := newAcoustID(server.URL, server.Client(), 0)
		candidates, err := client.Lookup(context.Background(), acoustidTestKey, 284, "AQADtE")
		if err != nil {
			t.Fatal(err)
		}
		if len(candidates) != 1 || candidates[0].Score != 0.97 || candidates[0].RecordingID != testRecordingID ||
			len(candidates[0].ReleaseGroups) != 1 || candidates[0].ReleaseGroups[0].ID != testRGID ||
			candidates[0].ReleaseGroups[0].Title != "OK Computer" || candidates[0].ReleaseGroups[0].Type != "Album" {
			t.Fatalf("candidates %+v", candidates)
		}
	})
}

func TestFingerprintScanDefersBeyondTwentyFiles(t *testing.T) {
	fingerprinter := &fakeFingerprinter{available: true, fingerprint: Fingerprint{DurationSeconds: 180, Value: "AQADtE"}}
	var calls int32
	acoustid := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		writeDiscoveryJSON(w, acoustidResultBody(0.5, testRecordingID, "Airbag", testRGID, "OK Computer"))
	}
	service, db, scanID, dir := acoustidFixture(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected MusicBrainz request %s", r.URL.String())
		http.NotFound(w, r)
	}, acoustid, fingerprinter, true)
	for i := 0; i < 25; i++ {
		relative := fmt.Sprintf("Music/Artist/Album/%02d Track.flac", i+1)
		size, modified := writeFingerprintMedia(t, dir, relative)
		insertFingerprintFile(t, db, scanID, relative, size, modified)
	}
	result, err := service.FingerprintScan(context.Background(), scanID, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Selected != 25 || result.Deferred != 5 || result.Fingerprinted != 20 || result.ProviderCalls != 20 ||
		result.Matched != 0 || result.Unresolved != 25 || result.Remaining != 25 {
		t.Fatalf("result %+v", result)
	}
	if fingerprinter.count() != 20 || atomic.LoadInt32(&calls) != 20 {
		t.Fatalf("provider calls fp=%d acoustid=%d", fingerprinter.count(), calls)
	}
}

func TestFingerprintScanReusesAndInvalidatesCache(t *testing.T) {
	fingerprinter := &fakeFingerprinter{available: true, fingerprint: Fingerprint{DurationSeconds: 180, Value: "AQADtE"}}
	acoustid := func(w http.ResponseWriter, r *http.Request) {
		writeDiscoveryJSON(w, acoustidResultBody(0.5, testRecordingID, "Airbag", testRGID, "OK Computer"))
	}
	service, db, scanID, dir := acoustidFixture(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected MusicBrainz request %s", r.URL.String())
		http.NotFound(w, r)
	}, acoustid, fingerprinter, true)
	relative := "Music/Radiohead/OK Computer/01 Airbag.flac"
	size, modified := writeFingerprintMedia(t, dir, relative)
	fileID := insertFingerprintFile(t, db, scanID, relative, size, modified)

	first, err := service.FingerprintScan(context.Background(), scanID, []int64{fileID}, false)
	if err != nil || first.Fingerprinted != 1 || first.ProviderCalls != 1 || first.Remaining != 1 {
		t.Fatalf("first run %+v %v", first, err)
	}
	second, err := service.FingerprintScan(context.Background(), scanID, []int64{fileID}, false)
	if err != nil || second.Fingerprinted != 1 || second.ProviderCalls != 1 {
		t.Fatalf("second run %+v %v", second, err)
	}
	if fingerprinter.count() != 1 {
		t.Fatalf("cached fingerprint reran fpcalc: %d", fingerprinter.count())
	}
	// Cached evidence is usable even when fpcalc is temporarily unavailable.
	fingerprinter.available = false
	if _, err := service.FingerprintScan(context.Background(), scanID, []int64{fileID}, false); err != nil {
		t.Fatalf("cached run needed fpcalc: %v", err)
	}
	if fingerprinter.count() != 1 {
		t.Fatalf("cached fingerprint reran fpcalc while unavailable: %d", fingerprinter.count())
	}
	fingerprinter.available = true
	// A changed file replaces the cached row instead of reusing stale evidence.
	absolute := filepath.Join(dir, filepath.FromSlash(relative))
	if err := os.WriteFile(absolute, []byte(strings.Repeat("changed-audio", 700)), 0o644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000100, 0)
	if err := os.Chtimes(absolute, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE music_scan_files SET size_bytes=?,modified_at=? WHERE id=?`, info.Size(), info.ModTime().Unix(), fileID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.FingerprintScan(context.Background(), scanID, []int64{fileID}, false); err != nil {
		t.Fatal(err)
	}
	if fingerprinter.count() != 2 {
		t.Fatalf("changed file reused cache: %d", fingerprinter.count())
	}
}

func TestFingerprintScanRequiresKeyAndFpcalc(t *testing.T) {
	fingerprinter := &fakeFingerprinter{available: true, fingerprint: Fingerprint{DurationSeconds: 180, Value: "AQADtE"}}
	service, db, scanID, dir := acoustidFixture(t, nil, nil, fingerprinter, false)
	relative := "Music/Artist/Album/01 Track.flac"
	size, modified := writeFingerprintMedia(t, dir, relative)
	fileID := insertFingerprintFile(t, db, scanID, relative, size, modified)
	_, err := service.FingerprintScan(context.Background(), scanID, []int64{fileID}, false)
	var provider *Error
	if !errors.As(err, &provider) || provider.Code != "music_acoustid_not_configured" {
		t.Fatalf("missing key error %v", err)
	}

	fingerprinter.available = false
	service, db, scanID, dir = acoustidFixture(t, nil, nil, fingerprinter, true)
	size, modified = writeFingerprintMedia(t, dir, relative)
	insertFingerprintFile(t, db, scanID, relative, size, modified)
	_, err = service.FingerprintScan(context.Background(), scanID, nil, true)
	if !errors.As(err, &provider) || provider.Code != "music_fingerprint_unavailable" {
		t.Fatalf("missing fpcalc error %v", err)
	}
}

func TestAcoustIDStatusAndKeyLifecycle(t *testing.T) {
	fingerprinter := &fakeFingerprinter{available: true, fingerprint: Fingerprint{DurationSeconds: 180, Value: "AQADtE"}}
	service, _, _, _ := acoustidFixture(t, nil, nil, fingerprinter, false)
	ctx := context.Background()
	status, err := service.AcoustIDStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Configured || !status.FingerprintAvailable || !strings.Contains(status.Message, "AcoustID application key") {
		t.Fatalf("status %+v", status)
	}
	if err := service.SaveAcoustIDKey(ctx, "short"); err == nil {
		t.Fatal("short key accepted")
	}
	if err := service.SaveAcoustIDKey(ctx, "bad\x01key-value"); err == nil {
		t.Fatal("control key accepted")
	}
	if err := service.SaveAcoustIDKey(ctx, acoustidTestKey); err != nil {
		t.Fatal(err)
	}
	status, err = service.AcoustIDStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Configured || !status.FingerprintAvailable || status.UpdatedAt == "" || strings.Contains(status.Message, acoustidTestKey) {
		t.Fatalf("configured status %+v", status)
	}
	if err := service.DeleteAcoustIDKey(ctx); err != nil {
		t.Fatal(err)
	}
	status, err = service.AcoustIDStatus(ctx)
	if err != nil || status.Configured {
		t.Fatalf("deleted status %+v %v", status, err)
	}
	fingerprinter.available = false
	status, err = service.AcoustIDStatus(ctx)
	if err != nil || status.FingerprintAvailable || !strings.Contains(status.Message, "libchromaprint-tools") {
		t.Fatalf("missing fpcalc status %+v %v", status, err)
	}
}

func TestParseFPCalcOutput(t *testing.T) {
	valid, err := parseFPCalcOutput([]byte(`{"duration":284.4,"fingerprint":"AQADtE"}`))
	if err != nil || valid.DurationSeconds != 284 || valid.Value != "AQADtE" {
		t.Fatalf("valid %+v %v", valid, err)
	}
	for _, body := range []string{
		`{"duration":0,"fingerprint":"AQADtE"}`,
		`{"duration":-1,"fingerprint":"AQADtE"}`,
		`{"duration":284,"fingerprint":""}`,
		`{"duration":284,"fingerprint":"AQ\nAD"}`,
		`not json`,
	} {
		if _, err := parseFPCalcOutput([]byte(body)); err == nil {
			t.Fatalf("accepted %q", body)
		}
	}
}
