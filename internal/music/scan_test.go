package music

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
)

type fakeProber map[string]AudioTags

type countingProber struct {
	calls atomic.Int32
}

func (p *countingProber) ProbeAudio(context.Context, string) (AudioTags, error) {
	p.calls.Add(1)
	return AudioTags{Artist: "Artist", Album: "Album", Title: "Song", Track: intPtr(1), Disc: intPtr(1), Format: "flac"}, nil
}

func TestScanReusesOnlyUnchangedAudioObservations(t *testing.T) {
	ctx := context.Background()
	s, _, media := editFixture(t)
	prober := &countingProber{}
	s.SetProber(prober)
	folder := filepath.Join(media, "Music")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(folder, "Song.flac")
	if err := os.WriteFile(filename, []byte("fixture 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	scan := func() {
		t.Helper()
		result, err := s.StartScan(ctx, "Music")
		if err != nil {
			t.Fatal(err)
		}
		if result = waitForScan(t, ctx, s, result.ID); result.State != "review" || result.MatchedCount != 1 {
			t.Fatal(result)
		}
	}
	scan()
	scan()
	if prober.calls.Load() != 1 {
		t.Fatalf("unchanged audio was probed again: %d calls", prober.calls.Load())
	}
	if err := os.WriteFile(filename, []byte("fixture 2"), 0o644); err != nil {
		t.Fatal(err)
	}
	scan()
	if prober.calls.Load() != 2 {
		t.Fatalf("changed audio reused its previous observation: %d calls", prober.calls.Load())
	}
}

func (p fakeProber) ProbeAudio(_ context.Context, path string) (AudioTags, error) {
	tags, ok := p[filepath.Base(path)]
	if !ok {
		return AudioTags{}, errors.New("unreadable fixture")
	}
	return tags, nil
}

func TestScanMatchesRegistersWithoutMoving(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	media := t.TempDir()
	dir := filepath.Join(media, "Music", "Artist", "Album")
	if err = os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	matchedPath := filepath.Join(dir, "01 - Song.flac")
	unknownPath := filepath.Join(dir, "02 - Other.mp3")
	for _, name := range []string{matchedPath, unknownPath} {
		if err = os.WriteFile(name, []byte("fixture audio bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES(1,?, 'Artist',1,'missing',1,1)`, testMBID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(2,1,?, 'Album',1,'missing',1,1)`, testRGID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO music_tracks(id,album_id,musicbrainz_id,disc,number,title,created_at,updated_at) VALUES(3,2,?,1,1,'Song',1,1)`, testTrackID); err != nil {
		t.Fatal(err)
	}

	service := NewService(db, media)
	service.SetProber(fakeProber{
		"01 - Song.flac": {Artist: "Artist", AlbumArtist: "Artist", Album: "Album", Title: "Song", Track: intPtr(1), Disc: intPtr(1), Format: "flac", DurationMS: 1000},
		"02 - Other.mp3": {Artist: "Unknown", AlbumArtist: "Unknown", Album: "Unknown", Title: "Other", Track: intPtr(2), Disc: intPtr(1), Format: "mp3"},
	})
	scan, err := service.StartScan(ctx, "Music")
	if err != nil {
		t.Fatal(err)
	}
	scan = waitForScan(t, ctx, service, scan.ID)
	if scan.State != "review" || scan.FileCount != 2 || scan.MatchedCount != 1 || scan.UnresolvedCount != 1 {
		t.Fatal("scan counts", scan)
	}
	page, err := service.ScanPage(ctx, scan.ID, 1)
	if err != nil || len(page.Items) != 2 {
		t.Fatal("scan page", page, err)
	}
	var matched, unresolved *ScanFile
	for i := range page.Items {
		switch page.Items[i].RelativePath {
		case "Music/Artist/Album/01 - Song.flac":
			matched = &page.Items[i]
		case "Music/Artist/Album/02 - Other.mp3":
			unresolved = &page.Items[i]
		}
	}
	if matched == nil || matched.State != "matched" || matched.AlbumID == nil || *matched.AlbumID != 2 || matched.TrackID == nil || *matched.TrackID != 3 {
		t.Fatal("matched file", matched)
	}
	if unresolved == nil || unresolved.State != "unresolved" || unresolved.Error == "" {
		t.Fatal("unresolved file", unresolved)
	}
	registered, err := service.RegisterScan(ctx, scan.ID, []int64{matched.ID, unresolved.ID})
	if err != nil || registered != 1 {
		t.Fatal("register", registered, err)
	}
	var relative, origin, sha string
	if err = db.QueryRow(`SELECT relative_path,origin,sha256 FROM music_files`).Scan(&relative, &origin, &sha); err != nil {
		t.Fatal(err)
	}
	if relative != "Music/Artist/Album/01 - Song.flac" || origin != "existing" || sha == "" {
		t.Fatal("registered file", relative, origin, sha)
	}
	if _, err = os.Stat(matchedPath); err != nil {
		t.Fatal("source file was moved", err)
	}
	var artistStatus, albumStatus string
	if err = db.QueryRow(`SELECT status FROM music_artists WHERE id=1`).Scan(&artistStatus); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT status FROM music_albums WHERE id=2`).Scan(&albumStatus); err != nil {
		t.Fatal(err)
	}
	if artistStatus != "available" || albumStatus != "available" {
		t.Fatal("availability", artistStatus, albumStatus)
	}
}

func TestScanRejectsEscapingRoots(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := NewService(db, t.TempDir())
	for _, root := range []string{"", ".", "..", "../outside", "/etc", "Music/../../outside"} {
		if _, err := service.StartScan(ctx, root); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("escaping root accepted", root, err)
		}
	}
}

func waitForScan(t *testing.T, ctx context.Context, service *Service, id int64) Scan {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		scan, err := service.Scan(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if scan.State != "scanning" {
			return scan
		}
		if time.Now().After(deadline) {
			t.Fatal("scan timed out", scan)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func intPtr(value int) *int { return &value }

func TestMatchAlbumTrackUsesProviderIDsBeforeNumbersAndTitles(t *testing.T) {
	tracks := []Track{
		{ID: 10, MusicBrainzID: testTrackID, RecordingID: testRecordingID, Disc: 1, Number: 1, Title: "Intro"},
		{ID: 11, MusicBrainzID: discoveryTrack2, RecordingID: discoveryRecording2, Disc: 2, Number: 1, Title: "Don’t Stop Me Now"},
	}
	byTrackID := ScanFile{MusicBrainzTrackID: testTrackID, Track: intPtr(1), Disc: intPtr(2), Title: "Song"}
	if id, err := matchAlbumTrack(byTrackID, tracks); err != nil || id != 10 {
		t.Fatal("track id", id, err)
	}
	byRecording := ScanFile{MusicBrainzTrackID: "99999999-9999-4999-8999-999999999999", MusicBrainzRecordingID: discoveryRecording2, Track: intPtr(1), Disc: intPtr(1)}
	if id, err := matchAlbumTrack(byRecording, tracks); err != nil || id != 11 {
		t.Fatal("recording id", id, err)
	}
	byNumber := ScanFile{Track: intPtr(1), Disc: intPtr(2)}
	if id, err := matchAlbumTrack(byNumber, tracks); err != nil || id != 11 {
		t.Fatal("disc and number", id, err)
	}
	byTitle := ScanFile{Title: "Dont Stop Me Now"}
	if id, err := matchAlbumTrack(byTitle, tracks); err != nil || id != 11 {
		t.Fatal("normalized title", id, err)
	}
	byFeatured := ScanFile{Title: "Don’t Stop Me Now (feat. Rihanna)"}
	if id, err := matchAlbumTrack(byFeatured, tracks); err != nil || id != 11 {
		t.Fatal("featured title", id, err)
	}
}

func TestMatchAlbumTrackRejectsAmbiguousTitle(t *testing.T) {
	tracks := []Track{
		{ID: 10, Disc: 1, Number: 1, Title: "Song"},
		{ID: 11, Disc: 2, Number: 9, Title: "Song (feat. Guest)"},
	}
	_, err := matchAlbumTrack(ScanFile{Title: "Song"}, tracks)
	var provider *Error
	if !errors.As(err, &provider) || provider.Code != "music_unmatched_track" || !strings.Contains(provider.Message, "More than one track") {
		t.Fatal("ambiguous title", err)
	}
}

func TestTrackMatchContradictsDifferentTitleAtSameNumber(t *testing.T) {
	depends := Track{ID: 10, Disc: 1, Number: 1, Title: "DEPENDS"}
	// Same disc/number but a clearly different song is not the same track; the
	// tag-authoritative caller must treat the slot as occupied, not overwrite it.
	if !trackMatchContradicts(ScanFile{Title: "S.A.D", Track: intPtr(1), Disc: intPtr(1)}, depends) {
		t.Fatal("different title at same number was accepted")
	}
	// A title that agrees after normalization is the same track.
	if trackMatchContradicts(ScanFile{Title: "S.A.D.", Track: intPtr(1), Disc: intPtr(1)}, Track{Disc: 1, Number: 1, Title: "S.A.D"}) {
		t.Fatal("normalized agreeing title was contradicted")
	}
	// A genuine provider ID is authoritative even when the title differs.
	if trackMatchContradicts(ScanFile{Title: "S.A.D", MusicBrainzTrackID: testTrackID, Track: intPtr(1), Disc: intPtr(1)}, Track{MusicBrainzID: testTrackID, Disc: 1, Number: 1, Title: "DEPENDS"}) {
		t.Fatal("provider ID match was contradicted")
	}
}

func TestFindCatalogAlbumIgnoresArticleAndUnicodeForm(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES(1,?,'Beatles',1,'missing',1,1)`, testMBID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(2,1,?,'Björk Live',1,'missing',1,1)`, testRGID); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, t.TempDir())
	artistID, albumID, err := service.findCatalogAlbum(ctx, "Bjo\u0308rk Live", "The Beatles")
	if err != nil || artistID != 1 || albumID != 2 {
		t.Fatal("normalized album lookup", artistID, albumID, err)
	}
}

func TestScanReprobesLegacyTagDocumentOnce(t *testing.T) {
	ctx := context.Background()
	s, db, media := editFixture(t)
	prober := &countingProber{}
	s.SetProber(prober)
	folder := filepath.Join(media, "Music")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "Song.flac"), []byte("fixture 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	scan := func() {
		t.Helper()
		result, err := s.StartScan(ctx, "Music")
		if err != nil {
			t.Fatal(err)
		}
		if result = waitForScan(t, ctx, s, result.ID); result.State != "review" || result.MatchedCount != 1 {
			t.Fatal(result)
		}
	}
	scan()
	if prober.calls.Load() != 1 {
		t.Fatalf("first scan probed %d times", prober.calls.Load())
	}
	// Simulate a tag document written before the album-artist MBID field was
	// added. The file bytes are unchanged, but the parser must run again.
	if _, err := db.Exec(`UPDATE music_scan_files SET tag_json='{"artist":"Artist","album":"Album","title":"Song"}' WHERE id=(SELECT MAX(id) FROM music_scan_files)`); err != nil {
		t.Fatal(err)
	}
	scan()
	if prober.calls.Load() != 2 {
		t.Fatalf("legacy tag document was reused: %d probes", prober.calls.Load())
	}
	scan()
	if prober.calls.Load() != 2 {
		t.Fatalf("versioned tag document was not reused: %d probes", prober.calls.Load())
	}
	var version int
	if err := db.QueryRow(`SELECT json_extract(tag_json,'$.tag_version') FROM music_scan_files ORDER BY id DESC LIMIT 1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != audioTagVersion {
		t.Fatal("tag version", version)
	}
}
