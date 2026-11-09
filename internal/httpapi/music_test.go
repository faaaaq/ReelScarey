package httpapi

import (
	"context"
	"database/sql"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/faaaaq/ReelScarey/internal/auth"
	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/music"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

const musicTestMBID = "a74b1b7f-71a5-4011-9441-d0b5e4122711"

type fakeMusic struct {
	search            music.SearchResults
	artist            music.Artist
	created           bool
	err               error
	acoustidStatus    music.AcoustIDStatus
	savedKey          string
	deletedKey        bool
	musicbrainzStatus music.MusicBrainzStatus
	savedBaseURL      string
	resetBaseURL      bool
	fingerprintResult music.DiscoverResult
	audioPath         string
	serverTokenOK     bool
}

func (f *fakeMusic) SearchArtists(context.Context, string, int) (music.SearchResults, error) {
	return f.search, f.err
}
func (f *fakeMusic) AddArtist(context.Context, string, bool) (music.Artist, bool, error) {
	return f.artist, f.created, f.err
}
func (f *fakeMusic) ListArtists(context.Context, string, int) (music.ArtistPage, error) {
	return music.ArtistPage{Items: []music.Artist{}, Page: 1, Limit: 48}, f.err
}
func (f *fakeMusic) Artist(context.Context, int64) (music.ArtistDetails, error) {
	return music.ArtistDetails{Artist: music.Artist{ID: 1, Name: "Artist"}, Albums: []music.Album{}}, f.err
}
func (f *fakeMusic) ArtistTracks(context.Context, int64, int, int) (music.ArtistTrackPage, error) {
	return music.ArtistTrackPage{Items: []music.ArtistTrack{}, Page: 1, Total: 0, Limit: 15}, f.err
}
func (f *fakeMusic) ArtistTopTracks(context.Context, int64, int) (music.TopTrackPage, error) {
	return music.TopTrackPage{Source: "Deezer", Items: []music.TopTrack{}}, f.err
}
func (f *fakeMusic) ResolveLink(context.Context, string) (music.LinkPreview, error) {
	return music.LinkPreview{Source: "Spotify", Kind: "track", Title: "Track", Tracks: []music.LinkTrack{{
		Title: "Track", Artist: "Artist", Album: "Album", AlbumArtist: "Artist",
		TrackNumber: 1, DiscNumber: 1, DurationMS: 1000, SearchQuery: "Artist Track",
	}}}, f.err
}
func (f *fakeMusic) ChartTracks(context.Context, int) (music.TopTrackPage, error) {
	return music.TopTrackPage{Source: "Deezer charts", Items: []music.TopTrack{}}, f.err
}
func (f *fakeMusic) MonitorSettings(context.Context) (music.MusicMonitorSettings, error) {
	return music.MusicMonitorSettings{IntervalHours: 24, Revision: 1}, f.err
}
func (f *fakeMusic) SaveMonitorSettings(_ context.Context, enabled bool, interval, revision int) (music.MusicMonitorSettings, error) {
	return music.MusicMonitorSettings{Enabled: enabled, IntervalHours: interval, Revision: revision + 1}, f.err
}
func (f *fakeMusic) ListMonitorSources(context.Context) (music.MusicMonitorSourceList, error) {
	return music.MusicMonitorSourceList{Items: []music.MusicMonitorSource{}}, f.err
}
func (f *fakeMusic) AddMonitorSource(_ context.Context, rawURL string) (music.MusicMonitorSource, error) {
	return music.MusicMonitorSource{ID: 1, URL: rawURL, Title: "Playlist", Enabled: true}, f.err
}
func (f *fakeMusic) SetMonitorSourceEnabled(_ context.Context, id int64, enabled bool) (music.MusicMonitorSource, error) {
	return music.MusicMonitorSource{ID: id, Enabled: enabled}, f.err
}
func (f *fakeMusic) DeleteMonitorSource(context.Context, int64) error { return f.err }
func (f *fakeMusic) TriggerMonitor(context.Context) (music.MusicMonitorSettings, error) {
	return music.MusicMonitorSettings{IntervalHours: 24, Revision: 1}, f.err
}
func (f *fakeMusic) StartAcquisition(_ context.Context, _, _, _, _, _ string, _ []music.AcquisitionTrackInput) (music.AcquisitionJob, error) {
	return music.AcquisitionJob{ID: 1, State: "queued", TrackCount: 1}, f.err
}
func (f *fakeMusic) Acquisition(context.Context, int64) (music.AcquisitionJob, error) {
	return music.AcquisitionJob{ID: 1, State: "queued", Tracks: []music.AcquisitionTrack{}}, f.err
}
func (f *fakeMusic) ListAcquisitions(context.Context, int) (music.AcquisitionJobList, error) {
	return music.AcquisitionJobList{Items: []music.AcquisitionJob{}, Page: 1, Limit: 20}, f.err
}
func (f *fakeMusic) ServerAlbum(context.Context, int64) (music.AlbumDetails, error) {
	return music.AlbumDetails{Album: music.Album{ID: 1, Title: "Album"}, Tracks: []music.Track{}}, f.err
}
func (f *fakeMusic) ServerSong(context.Context, int64) (music.ServerSong, error) {
	return music.ServerSong{FileID: 1, Title: "Song", Artist: "Artist", Album: "Album"}, f.err
}
func (f *fakeMusic) ServerSongs(context.Context, string, int) ([]music.ServerSong, error) {
	return []music.ServerSong{}, f.err
}
func (f *fakeMusic) ServerRandomSongs(context.Context, int) ([]music.ServerSong, error) {
	return []music.ServerSong{}, f.err
}
func (f *fakeMusic) ServerAlbums(context.Context, string, int, int) ([]music.Album, error) {
	return []music.Album{}, f.err
}
func (f *fakeMusic) ServerSearchAlbums(context.Context, string, int) ([]music.Album, error) {
	return []music.Album{}, f.err
}
func (f *fakeMusic) MusicServer(context.Context) (music.MusicServerStatus, error) {
	return music.MusicServerStatus{Enabled: false, Revision: 1}, f.err
}
func (f *fakeMusic) SaveMusicServer(_ context.Context, enabled bool, revision int) (music.MusicServerStatus, error) {
	return music.MusicServerStatus{Enabled: enabled, Revision: revision + 1}, f.err
}
func (f *fakeMusic) GenerateMusicServerToken(context.Context) (string, music.MusicServerStatus, error) {
	return "test-token", music.MusicServerStatus{Configured: true, Revision: 1}, f.err
}
func (f *fakeMusic) DeleteMusicServerToken(context.Context) (music.MusicServerStatus, error) {
	return music.MusicServerStatus{Configured: false, Revision: 1}, f.err
}
func (f *fakeMusic) VerifyMusicServerToken(_ context.Context, _ string, password, token, salt string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	if !f.serverTokenOK {
		return false, nil
	}
	return password == "test-token" || (token != "" && salt != ""), nil
}
func (f *fakeMusic) OpenAudioFile(context.Context, int64) (music.AudioStream, error) {
	if f.err != nil {
		return music.AudioStream{}, f.err
	}
	file, err := os.Open(f.audioPath)
	if err != nil {
		return music.AudioStream{}, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return music.AudioStream{}, err
	}
	return music.AudioStream{File: file, Name: filepath.Base(f.audioPath), ContentType: "audio/mpeg", Size: info.Size(), ModTime: info.ModTime()}, nil
}
func (f *fakeMusic) Album(context.Context, int64) (music.AlbumDetails, error) {
	return music.AlbumDetails{Album: music.Album{ID: 1, Title: "Album"}, Tracks: []music.Track{}}, f.err
}
func (f *fakeMusic) StartScan(context.Context, string) (music.Scan, error) {
	return music.Scan{ID: 1, State: "scanning"}, f.err
}

func (f *fakeMusic) StartScanRoots(context.Context, []string) (music.Scan, error) {
	return music.Scan{ID: 1, State: "scanning"}, f.err
}
func (f *fakeMusic) StartScanRootsFull(context.Context, []string, bool) (music.Scan, error) {
	return music.Scan{ID: 1, State: "scanning"}, f.err
}
func (f *fakeMusic) RequestMatchAll(context.Context, int64, bool) (music.Scan, error) {
	return music.Scan{ID: 1, State: "review", Phase: "queued", Request: "match"}, f.err
}
func (f *fakeMusic) RequestFinalize(context.Context, int64, bool) (music.Scan, error) {
	return music.Scan{ID: 1, State: "review", Phase: "queued", Request: "finalize"}, f.err
}
func (f *fakeMusic) ListScans(context.Context, int) (music.ScanList, error) {
	return music.ScanList{Items: []music.Scan{}, Page: 1, Limit: 20}, f.err
}
func (f *fakeMusic) Scan(context.Context, int64) (music.Scan, error) {
	return music.Scan{ID: 1, State: "review"}, f.err
}
func (f *fakeMusic) ScanPage(context.Context, int64, int) (music.ScanPage, error) {
	return music.ScanPage{Scan: music.Scan{ID: 1, State: "review"}, Items: []music.ScanFile{}, Page: 1, Limit: 100}, f.err
}
func (f *fakeMusic) RegisterScan(context.Context, int64, []int64) (int, error) {
	return 1, f.err
}
func (f *fakeMusic) DiscoverScan(context.Context, int64, []int64, bool) (music.DiscoverResult, error) {
	return music.DiscoverResult{Selected: 1, Matched: 1, ArtistsAdded: 1, AlbumsAdded: 1, Remaining: 0, Warnings: []string{}}, f.err
}
func (f *fakeMusic) FingerprintScan(context.Context, int64, []int64, bool) (music.DiscoverResult, error) {
	return f.fingerprintResult, f.err
}
func (f *fakeMusic) AcoustIDStatus(context.Context) (music.AcoustIDStatus, error) {
	return f.acoustidStatus, f.err
}
func (f *fakeMusic) SaveAcoustIDKey(_ context.Context, key string) error {
	if f.err == nil {
		f.savedKey = key
		f.acoustidStatus.Configured = true
	}
	return f.err
}
func (f *fakeMusic) DeleteAcoustIDKey(context.Context) error {
	if f.err == nil {
		f.deletedKey = true
		f.acoustidStatus.Configured = false
	}
	return f.err
}
func (f *fakeMusic) MusicBrainzStatus(context.Context) (music.MusicBrainzStatus, error) {
	return f.musicbrainzStatus, f.err
}
func (f *fakeMusic) SaveMusicBrainzBaseURL(_ context.Context, base string) (music.MusicBrainzStatus, error) {
	if f.err == nil {
		f.savedBaseURL = base
		f.musicbrainzStatus.BaseURL = base
		f.musicbrainzStatus.UsingDefault = false
		f.musicbrainzStatus.Message = "Using a custom MusicBrainz base URL."
	}
	return f.musicbrainzStatus, f.err
}
func (f *fakeMusic) DeleteMusicBrainzBaseURL(context.Context) (music.MusicBrainzStatus, error) {
	if f.err == nil {
		f.resetBaseURL = true
		f.musicbrainzStatus.BaseURL = music.DefaultMusicBrainzBaseURL
		f.musicbrainzStatus.DefaultBaseURL = music.DefaultMusicBrainzBaseURL
		f.musicbrainzStatus.UsingDefault = true
		f.musicbrainzStatus.CooldownUntil = 0
		f.musicbrainzStatus.Message = "Using public MusicBrainz."
	}
	return f.musicbrainzStatus, f.err
}
func (f *fakeMusic) CancelScan(context.Context, int64) error { return f.err }
func (f *fakeMusic) SkipScanFile(context.Context, int64, int64) error {
	return f.err
}
func (f *fakeMusic) ListFiles(context.Context, int) (music.MusicFilePage, error) {
	return music.MusicFilePage{Items: []music.MusicFile{}, Page: 1, Limit: 100}, f.err
}
func (f *fakeMusic) PreviewEdits(context.Context, []int64) (music.EditPlan, error) {
	return music.EditPlan{Files: []music.EditFile{}, PlanHash: "hash"}, f.err
}
func (f *fakeMusic) ApplyEdits(context.Context, []int64, string, bool) (music.EditJob, error) {
	return music.EditJob{ID: 1, State: "running"}, f.err
}
func (f *fakeMusic) EditJob(context.Context, int64) (music.EditJob, error) {
	return music.EditJob{ID: 1, State: "completed", Files: []music.EditJobFile{}}, f.err
}
func (f *fakeMusic) ListEditJobs(context.Context, int) (music.EditJobList, error) {
	return music.EditJobList{Items: []music.EditJob{}, Page: 1, Limit: 20}, f.err
}
func (f *fakeMusic) RollbackEdits(context.Context, int64, bool) (music.EditJob, error) {
	return music.EditJob{ID: 1, State: "rolled_back"}, f.err
}
func (f *fakeMusic) StartImport(context.Context, int64, string, string) (music.ImportJob, error) {
	return music.ImportJob{ID: 1, State: "running"}, f.err
}
func (f *fakeMusic) ImportJob(context.Context, int64) (music.ImportJob, error) {
	return music.ImportJob{ID: 1, State: "completed"}, f.err
}
func (f *fakeMusic) ListImports(context.Context, int) (music.ImportJobList, error) {
	return music.ImportJobList{Items: []music.ImportJob{}, Page: 1, Limit: 20}, f.err
}
func (f *fakeMusic) GetPreferences(context.Context) (music.MusicPreferences, error) {
	return music.MusicPreferences{AllowLossless: true, PreferredFormats: []string{"FLAC"}, Revision: 1}, f.err
}
func (f *fakeMusic) SavePreferences(_ context.Context, in music.MusicPreferences) (music.MusicPreferences, error) {
	in.Revision++
	return in, f.err
}
func (f *fakeMusic) WantedAlbums(context.Context, int) (music.WantedAlbumPage, error) {
	return music.WantedAlbumPage{Items: []music.WantedAlbum{}, Page: 1, Limit: 48}, f.err
}
func (f *fakeMusic) GetSchedule(context.Context) (music.MusicSchedule, error) {
	return music.MusicSchedule{Enabled: false, IntervalHours: 24, Mode: "search", Revision: 1}, f.err
}
func (f *fakeMusic) SaveSchedule(_ context.Context, in music.MusicSchedule) (music.MusicSchedule, error) {
	in.Revision++
	return in, f.err
}
func (f *fakeMusic) RunSchedule(context.Context) error { return f.err }
func (f *fakeMusic) AlbumPreferences(context.Context, int64) (music.AlbumPreferences, error) {
	return music.AlbumPreferences{Preferences: music.MusicPreferences{AllowLossless: true, Revision: 1}, Inherited: true, Revision: 1}, f.err
}
func (f *fakeMusic) SaveAlbumPreferences(_ context.Context, _ int64, in music.MusicPreferences) (music.AlbumPreferences, error) {
	in.Revision++
	return music.AlbumPreferences{Preferences: in, Revision: 2}, f.err
}
func (f *fakeMusic) ResetAlbumPreferences(context.Context, int64) (music.AlbumPreferences, error) {
	return music.AlbumPreferences{Inherited: true, Revision: 2}, f.err
}
func (f *fakeMusic) UpgradeSuggestions(context.Context, int) ([]music.UpgradeSuggestion, error) {
	return []music.UpgradeSuggestion{}, f.err
}

func musicFixture(t *testing.T, service MusicService) (http.Handler, *auth.Session) {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	assets := fstest.MapFS{"index.html": {Data: []byte("app shell")}}
	authService, err := auth.New(context.Background(), db, dir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := securestore.New(context.Background(), db, dir)
	if err != nil {
		t.Fatal(err)
	}
	code, err := securestore.SecretFile(filepath.Join(dir, "setup-token"), 32, false)
	if err != nil {
		t.Fatal(err)
	}
	session, err := authService.Setup(context.Background(), "admin", "test-only-password", base64.RawURLEncoding.EncodeToString(code))
	if err != nil {
		t.Fatal(err)
	}
	handler := New(db, config.Config{ConfigDir: dir, MediaDir: "/media", DownloadsDir: "/downloads", PublicURL: "http://example.com"}, slog.New(slog.NewTextHandler(io.Discard, nil)), assets, "test", Dependencies{
		Auth:     authService,
		Metadata: metadata.New(store, metadata.NewTMDb()),
		Music:    service,
		Secrets:  store,
	})
	return handler, &session
}

func TestMusicMusicBrainzStatusAndBaseURLRoutes(t *testing.T) {
	service := &fakeMusic{musicbrainzStatus: music.MusicBrainzStatus{
		BaseURL:        music.DefaultMusicBrainzBaseURL,
		DefaultBaseURL: music.DefaultMusicBrainzBaseURL,
		UsingDefault:   true,
		Message:        "Using public MusicBrainz at one request per second.",
		CacheEntries:   12,
	}}
	handler, session := musicFixture(t, service)
	if out := request(handler, "GET", "/api/v1/library/music/musicbrainz", "", nil, false); out.Code != 401 {
		t.Fatal("unauthenticated status", out.Code)
	}
	out := request(handler, "GET", "/api/v1/library/music/musicbrainz", "", session, false)
	body := out.Body.String()
	if out.Code != 200 || !strings.Contains(body, `"using_default":true`) || !strings.Contains(body, `"default_base_url":"`+music.DefaultMusicBrainzBaseURL+`"`) || !strings.Contains(body, `"cache_entries":12`) || strings.Contains(body, "secret") {
		t.Fatal("status fields", out.Code, body)
	}
	if out := request(handler, "PUT", "/api/v1/library/music/musicbrainz", `{"base_url":"https://mb.example"}`, session, false); out.Code != 403 {
		t.Fatal("save without CSRF", out.Code)
	}
	out = request(handler, "PUT", "/api/v1/library/music/musicbrainz", `{"base_url":"https://mb.example"}`, session, true)
	if out.Code != 200 || service.savedBaseURL != "https://mb.example" || !strings.Contains(out.Body.String(), `"using_default":false`) {
		t.Fatal("save base url", out.Code, service.savedBaseURL, out.Body.String())
	}
	if out := request(handler, "PUT", "/api/v1/library/music/musicbrainz", `{"base_url":""}`, session, true); out.Code != 400 {
		t.Fatal("empty base url", out.Code)
	}
	out = request(handler, "DELETE", "/api/v1/library/music/musicbrainz", "", session, true)
	if out.Code != 200 || !service.resetBaseURL || !strings.Contains(out.Body.String(), `"using_default":true`) {
		t.Fatal("reset base url", out.Code, out.Body.String())
	}
	if out := request(handler, "POST", "/api/v1/library/music/musicbrainz", `{}`, session, true); out.Code != 405 {
		t.Fatal("method", out.Code)
	}
}

func TestMusicRoutesRequireAuthenticationAndCSRF(t *testing.T) {
	handler, session := musicFixture(t, &fakeMusic{})
	if out := request(handler, "GET", "/api/v1/library/music/artists", "", nil, false); out.Code != 401 {
		t.Fatal("unauthenticated music read", out.Code)
	}
	if out := request(handler, "POST", "/api/v1/library/music/artists", `{"musicbrainz_id":"`+musicTestMBID+`"}`, session, false); out.Code != 403 {
		t.Fatal("music add without CSRF", out.Code)
	}
	if out := request(handler, "POST", "/api/v1/library/music/scans/1/discover", `{"file_ids":[1],"reviewed":true}`, nil, false); out.Code != 401 {
		t.Fatal("unauthenticated music discovery", out.Code)
	}
}

func TestMusicSearchAndAdd(t *testing.T) {
	service := &fakeMusic{
		search:  music.SearchResults{Page: 1, TotalPages: 1, TotalResults: 1, Items: []music.Artist{{MusicBrainzID: musicTestMBID, Name: "Radiohead"}}},
		artist:  music.Artist{ID: 7, MusicBrainzID: musicTestMBID, Name: "Radiohead"},
		created: true,
	}
	handler, session := musicFixture(t, service)
	out := request(handler, "GET", "/api/v1/metadata/music/artists?query=radiohead", "", session, false)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"name":"Radiohead"`) {
		t.Fatal("search", out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/music/artists", `{"musicbrainz_id":"`+musicTestMBID+`"}`, session, true)
	if out.Code != 201 || !strings.Contains(out.Body.String(), `"created":true`) {
		t.Fatal("add", out.Code, out.Body.String())
	}
	service.created = false
	out = request(handler, "POST", "/api/v1/library/music/artists", `{"musicbrainz_id":"`+musicTestMBID+`"}`, session, true)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"created":false`) {
		t.Fatal("idempotent add", out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/music/artists", `{"musicbrainz_id":"not-a-uuid"}`, session, true)
	if out.Code != 400 {
		t.Fatal("invalid mbid", out.Code, out.Body.String())
	}
	out = request(handler, "GET", "/api/v1/metadata/music/artists?query=", "", session, false)
	if out.Code != 400 {
		t.Fatal("empty search", out.Code, out.Body.String())
	}
}

func TestMusicArtistAndAlbumRoutes(t *testing.T) {
	handler, session := musicFixture(t, &fakeMusic{})
	for _, path := range []string{"/api/v1/library/music/artists/1", "/api/v1/library/music/artists/1/tracks?limit=15", "/api/v1/library/music/albums/1"} {
		out := request(handler, "GET", path, "", session, false)
		if out.Code != 200 {
			t.Fatal(path, out.Code, out.Body.String())
		}
	}
	if out := request(handler, "GET", "/api/v1/library/music/albums/nope", "", session, false); out.Code != 400 {
		t.Fatal("invalid album id", out.Code)
	}
	if out := request(handler, "GET", "/api/v1/library/music/artists/1/tracks?limit=101", "", session, false); out.Code != 400 {
		t.Fatal("invalid track limit", out.Code)
	}
}

func TestMusicFileStreamSupportsRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, session := musicFixture(t, &fakeMusic{audioPath: path})
	if out := request(handler, "GET", "/api/v1/library/music/files/1/stream", "", nil, false); out.Code != 401 {
		t.Fatal("stream leaked to anonymous request", out.Code)
	}
	out := request(handler, "GET", "/api/v1/library/music/files/1/stream", "", session, false)
	if out.Code != 200 || out.Body.String() != "0123456789" || out.Header().Get("Content-Type") != "audio/mpeg" {
		t.Fatal("stream", out.Code, out.Header().Get("Content-Type"), out.Body.String())
	}
	r := httptest.NewRequest("GET", "/api/v1/library/music/files/1/stream", nil)
	r.AddCookie(&http.Cookie{Name: cookieName, Value: session.Token})
	r.Header.Set("Range", "bytes=2-4")
	rangeOut := httptest.NewRecorder()
	handler.ServeHTTP(rangeOut, r)
	if rangeOut.Code != http.StatusPartialContent || rangeOut.Body.String() != "234" || rangeOut.Header().Get("Content-Range") != "bytes 2-4/10" {
		t.Fatal("range stream", rangeOut.Code, rangeOut.Header().Get("Content-Range"), rangeOut.Body.String())
	}
	if out := request(handler, "GET", "/api/v1/library/music/files/nope/stream", "", session, false); out.Code != 400 {
		t.Fatal("invalid stream id", out.Code)
	}
}

func TestSubsonicPingAndStreamUseOptInToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, _ := musicFixture(t, &fakeMusic{serverTokenOK: true, audioPath: path})
	request := func(rawURL string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", rawURL, nil)
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, r)
		return out
	}
	out := request("/rest/ping.view?u=reelscarey&p=test-token&v=1.16.1&c=test")
	if out.Code != 200 || !strings.Contains(out.Body.String(), `status="ok"`) {
		t.Fatal("ping", out.Code, out.Body.String())
	}
	out = request("/rest/ping.view?u=reelscarey&p=test-token&v=1.16.1&c=test&f=json")
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"status":"ok"`) {
		t.Fatal("json ping", out.Code, out.Body.String())
	}
	out = request("/rest/ping.view?u=reelscarey&p=wrong&v=1.16.1&c=test")
	if out.Code != 200 || !strings.Contains(out.Body.String(), `status="failed"`) || !strings.Contains(out.Body.String(), `code="40"`) {
		t.Fatal("wrong token", out.Code, out.Body.String())
	}
	out = request("/rest/getMusicFolders.view?u=reelscarey&p=test-token&v=1.16.1&c=test")
	if out.Code != 200 || !strings.Contains(out.Body.String(), `name="Music"`) {
		t.Fatal("folders", out.Code, out.Body.String())
	}
	out = request("/rest/stream.view?u=reelscarey&p=test-token&v=1.16.1&c=test&id=1")
	if out.Code != 200 || out.Body.String() != "0123456789" || out.Header().Get("Content-Type") != "audio/mpeg" {
		t.Fatal("stream", out.Code, out.Header().Get("Content-Type"), out.Body.String())
	}
}

func TestMusicServerSettingsRoutes(t *testing.T) {
	handler, session := musicFixture(t, &fakeMusic{})
	if out := request(handler, "GET", "/api/v1/library/music/server", "", nil, false); out.Code != 401 {
		t.Fatal("server status leaked", out.Code)
	}
	if out := request(handler, "GET", "/api/v1/library/music/server", "", session, false); out.Code != 200 {
		t.Fatal("server status", out.Code, out.Body.String())
	}
	if out := request(handler, "PUT", "/api/v1/library/music/server", `{"enabled":true,"revision":1}`, session, false); out.Code != 403 {
		t.Fatal("server toggle without CSRF", out.Code)
	}
	if out := request(handler, "PUT", "/api/v1/library/music/server", `{"enabled":true,"revision":1}`, session, true); out.Code != 200 {
		t.Fatal("server toggle", out.Code, out.Body.String())
	}
	if out := request(handler, "POST", "/api/v1/library/music/server/token", `{}`, session, true); out.Code != 201 {
		t.Fatal("token create", out.Code, out.Body.String())
	}
	if out := request(handler, "DELETE", "/api/v1/library/music/server/token", "", session, true); out.Code != 200 {
		t.Fatal("token delete", out.Code, out.Body.String())
	}
}

func TestMusicLinkAndMonitorRoutes(t *testing.T) {
	handler, session := musicFixture(t, &fakeMusic{})
	if out := request(handler, "POST", "/api/v1/library/music/links/preview", `{"url":"https://open.spotify.com/track/abc"}`, session, true); out.Code != 200 {
		t.Fatal("link preview", out.Code, out.Body.String())
	}
	if out := request(handler, "POST", "/api/v1/library/music/links", `{"url":"https://open.spotify.com/track/abc","track_indices":[0],"root":"Music","format":"source"}`, session, true); out.Code != 400 {
		t.Fatal("link without review", out.Code)
	}
	if out := request(handler, "POST", "/api/v1/library/music/links", `{"url":"https://open.spotify.com/track/abc","track_indices":[0],"root":"Music","format":"source","reviewed":true}`, session, true); out.Code != 202 {
		t.Fatal("link acquisition", out.Code, out.Body.String())
	}
	for _, path := range []string{"/api/v1/library/music/charts", "/api/v1/library/music/monitor", "/api/v1/library/music/monitor/sources", "/api/v1/library/music/acquisitions"} {
		if out := request(handler, "GET", path, "", session, false); out.Code != 200 {
			t.Fatal(path, out.Code, out.Body.String())
		}
	}
	if out := request(handler, "PUT", "/api/v1/library/music/monitor", `{"enabled":false,"interval_hours":24,"revision":1}`, session, true); out.Code != 200 {
		t.Fatal("monitor settings", out.Code, out.Body.String())
	}
	if out := request(handler, "POST", "/api/v1/library/music/monitor/sources", `{"url":"https://open.spotify.com/playlist/abc"}`, session, true); out.Code != 201 {
		t.Fatal("monitor source", out.Code, out.Body.String())
	}
	if out := request(handler, "POST", "/api/v1/library/music/monitor/run", `{}`, session, true); out.Code != 202 {
		t.Fatal("monitor run", out.Code, out.Body.String())
	}
}

func TestMusicProviderErrorsMapToStatus(t *testing.T) {
	cases := []struct {
		err    error
		status int
		header string
	}{
		{&music.Error{Code: "music_rate_limited", Message: "slow down", RetryAfter: 3}, 429, "3"},
		{&music.Error{Code: "music_not_found", Message: "missing"}, 404, ""},
		{music.ErrInvalidInput, 400, ""},
		{sql.ErrNoRows, 404, ""},
	}
	for _, tc := range cases {
		handler, session := musicFixture(t, &fakeMusic{err: tc.err})
		out := request(handler, "GET", "/api/v1/library/music/artists/1", "", session, false)
		if out.Code != tc.status {
			t.Fatal(tc.err, out.Code, out.Body.String())
		}
		if tc.header != "" && out.Header().Get("Retry-After") != tc.header {
			t.Fatal("retry-after", out.Header().Get("Retry-After"))
		}
	}
}

func TestMusicScanRoutes(t *testing.T) {
	handler, session := musicFixture(t, &fakeMusic{})
	out := request(handler, "POST", "/api/v1/library/music/scans", `{"root":"Music"}`, session, true)
	if out.Code != 202 {
		t.Fatal("start scan", out.Code, out.Body.String())
	}
	out = request(handler, "GET", "/api/v1/library/music/scans/1", "", session, false)
	if out.Code != 200 {
		t.Fatal("scan status", out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/music/scans/1/register", `{"file_ids":[1]}`, session, true)
	if out.Code != 400 {
		t.Fatal("register without review", out.Code)
	}
	out = request(handler, "POST", "/api/v1/library/music/scans/1/register", `{"file_ids":[1],"reviewed":true}`, session, true)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"registered":1`) {
		t.Fatal("register", out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/music/scans/1/discover", `{"file_ids":[1],"reviewed":true}`, session, true)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"matched":1`) || !strings.Contains(out.Body.String(), `"artists_added":1`) {
		t.Fatal("discover", out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/music/scans/1/discover", `{"all":true,"reviewed":true}`, session, true)
	if out.Code != 200 {
		t.Fatal("discover all", out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/music/scans/1/discover", `{"file_ids":[1],"reviewed":true}`, session, false)
	if out.Code != 403 {
		t.Fatal("discover without CSRF", out.Code)
	}
	out = request(handler, "POST", "/api/v1/library/music/scans/1/discover", `{"file_ids":[1]}`, session, true)
	if out.Code != 400 {
		t.Fatal("discover without review", out.Code)
	}
	out = request(handler, "POST", "/api/v1/library/music/scans/1/discover", `{"file_ids":[],"reviewed":true}`, session, true)
	if out.Code != 400 {
		t.Fatal("discover empty selection", out.Code)
	}
	out = request(handler, "POST", "/api/v1/library/music/scans/1/discover", `{"file_ids":[1],"all":true,"reviewed":true}`, session, true)
	if out.Code != 400 {
		t.Fatal("discover conflicting selection", out.Code)
	}
	out = request(handler, "POST", "/api/v1/library/music/scans/1/skip", `{"file_id":1}`, session, true)
	if out.Code != 200 {
		t.Fatal("skip", out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/music/scans/1/cancel", `{}`, session, true)
	if out.Code != 200 {
		t.Fatal("cancel", out.Code, out.Body.String())
	}
	out = request(handler, "GET", "/api/v1/library/music/scans", "", session, false)
	if out.Code != 200 {
		t.Fatal("scan list", out.Code, out.Body.String())
	}
}

func TestMusicScanWorkflowRoutes(t *testing.T) {
	handler, session := musicFixture(t, &fakeMusic{})
	out := request(handler, "POST", "/api/v1/library/music/scans", `{"roots":["Music"],"full":true}`, session, true)
	if out.Code != 202 {
		t.Fatal("full scan", out.Code, out.Body.String())
	}
	out = request(handler, "GET", "/api/v1/library/music/scans/1", "", session, false)
	for _, field := range []string{"full_scan", "phase", "progress_total", "progress_done", "current_path", "message", "skipped_count", "request", "requested_at"} {
		if !strings.Contains(out.Body.String(), `"`+field+`"`) {
			t.Fatal("scan status is missing", field, out.Body.String())
		}
	}
	out = request(handler, "POST", "/api/v1/library/music/scans/1/match-all", `{"reviewed":true}`, nil, true)
	if out.Code != 401 {
		t.Fatal("match-all unauthenticated", out.Code)
	}
	out = request(handler, "POST", "/api/v1/library/music/scans/1/match-all", `{"reviewed":true}`, session, false)
	if out.Code != 403 {
		t.Fatal("match-all without CSRF", out.Code)
	}
	out = request(handler, "POST", "/api/v1/library/music/scans/1/match-all", `{}`, session, true)
	if out.Code != 400 {
		t.Fatal("match-all without review", out.Code)
	}
	out = request(handler, "POST", "/api/v1/library/music/scans/1/match-all", `{"reviewed":true}`, session, true)
	if out.Code != 202 || !strings.Contains(out.Body.String(), `"request":"match"`) || !strings.Contains(out.Body.String(), `"phase":"queued"`) {
		t.Fatal("match-all", out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/music/scans/1/finalize", `{"reviewed":true}`, nil, true)
	if out.Code != 401 {
		t.Fatal("finalize unauthenticated", out.Code)
	}
	out = request(handler, "POST", "/api/v1/library/music/scans/1/finalize", `{"reviewed":true}`, session, false)
	if out.Code != 403 {
		t.Fatal("finalize without CSRF", out.Code)
	}
	out = request(handler, "POST", "/api/v1/library/music/scans/1/finalize", `{"reviewed":false}`, session, true)
	if out.Code != 400 {
		t.Fatal("finalize without review", out.Code)
	}
	out = request(handler, "POST", "/api/v1/library/music/scans/1/finalize", `{"reviewed":true}`, session, true)
	if out.Code != 202 || !strings.Contains(out.Body.String(), `"request":"finalize"`) {
		t.Fatal("finalize", out.Code, out.Body.String())
	}
	out = request(handler, "GET", "/api/v1/library/music/scans/1/match-all", "", session, false)
	if out.Code != 405 {
		t.Fatal("match-all method", out.Code)
	}
}

func TestMusicEditRoutes(t *testing.T) {
	handler, session := musicFixture(t, &fakeMusic{})
	out := request(handler, "GET", "/api/v1/library/music/files", "", session, false)
	if out.Code != 200 {
		t.Fatal("files", out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/music/edits/preview", `{"file_ids":[1]}`, session, true)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"plan_hash":"hash"`) {
		t.Fatal("preview", out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/music/edits/apply", `{"file_ids":[1],"plan_hash":"hash"}`, session, true)
	if out.Code != 400 {
		t.Fatal("apply without review", out.Code)
	}
	out = request(handler, "POST", "/api/v1/library/music/edits/apply", `{"file_ids":[1],"plan_hash":"hash","reviewed":true}`, session, true)
	if out.Code != 202 {
		t.Fatal("apply", out.Code, out.Body.String())
	}
	out = request(handler, "GET", "/api/v1/library/music/edits/1", "", session, false)
	if out.Code != 200 {
		t.Fatal("edit job", out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/music/edits/1/rollback", `{"reviewed":true}`, session, true)
	if out.Code != 200 {
		t.Fatal("rollback", out.Code, out.Body.String())
	}
	out = request(handler, "GET", "/api/v1/library/music/edits", "", session, false)
	if out.Code != 200 {
		t.Fatal("edit list", out.Code, out.Body.String())
	}
}

func TestMusicImportRoutes(t *testing.T) {
	handler, session := musicFixture(t, &fakeMusic{})
	out := request(handler, "POST", "/api/v1/library/music/albums/1/import", `{"source_path":"Album","root":"Music"}`, session, true)
	if out.Code != 202 {
		t.Fatal("start import", out.Code, out.Body.String())
	}
	out = request(handler, "GET", "/api/v1/library/music/imports/1", "", session, false)
	if out.Code != 200 {
		t.Fatal("import job", out.Code, out.Body.String())
	}
	out = request(handler, "GET", "/api/v1/library/music/imports", "", session, false)
	if out.Code != 200 {
		t.Fatal("import list", out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/music/albums/1/import", `{"source_path":""}`, session, true)
	if out.Code != 400 {
		t.Fatal("empty source", out.Code)
	}
}

func TestMusicAutomationRoutes(t *testing.T) {
	handler, session := musicFixture(t, &fakeMusic{})
	for _, path := range []string{"/api/v1/library/music/preferences", "/api/v1/library/music/schedule"} {
		out := request(handler, "GET", path, "", session, false)
		if out.Code != 200 {
			t.Fatal(path, out.Code, out.Body.String())
		}
	}
	if out := request(handler, "GET", "/api/v1/library/music/wanted", "", session, false); out.Code != 200 {
		t.Fatal("wanted", out.Code, out.Body.String())
	}
	if out := request(handler, "POST", "/api/v1/library/music/schedule/run", "{}", session, true); out.Code != 202 {
		t.Fatal("run", out.Code, out.Body.String())
	}
	if out := request(handler, "PUT", "/api/v1/library/music/preferences", `{"allow_lossless":false,"min_bitrate":192,"preferred_formats":["MP3"],"revision":1}`, session, true); out.Code != 200 {
		t.Fatal("preferences", out.Code, out.Body.String())
	}
}

func TestMusicFrontendRoutes(t *testing.T) {
	handler := frontend(fstest.MapFS{"index.html": {Data: []byte("app shell")}})
	for _, path := range []string{"/music", "/music/artists/1", "/music/albums/1"} {
		out := request(handler, "GET", path, "", nil, false)
		if out.Code != 200 || out.Body.String() != "app shell" {
			t.Fatal(path, out.Code, out.Body.String())
		}
	}
}

func TestMusicAcoustIDSettingsNeverExposeKey(t *testing.T) {
	service := &fakeMusic{acoustidStatus: music.AcoustIDStatus{
		Configured:           true,
		UpdatedAt:            "2026-11-08T10:00:00.000Z",
		FingerprintAvailable: true,
		Message:              "Only the Chromaprint fingerprint and duration are sent; audio is never uploaded.",
	}}
	handler, session := musicFixture(t, service)
	if out := request(handler, "GET", "/api/v1/library/music/acoustid", "", nil, false); out.Code != 401 {
		t.Fatal("unauthenticated status", out.Code)
	}
	out := request(handler, "GET", "/api/v1/library/music/acoustid", "", session, false)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"configured":true`) ||
		!strings.Contains(out.Body.String(), `"fingerprint_available":true`) ||
		!strings.Contains(out.Body.String(), `"updated_at":"2026-11-08T10:00:00.000Z"`) {
		t.Fatal("status", out.Code, out.Body.String())
	}
	if strings.Contains(out.Body.String(), "api_key") || strings.Contains(out.Body.String(), "secret") {
		t.Fatalf("status exposed a key field: %s", out.Body.String())
	}
	if out := request(handler, "PUT", "/api/v1/library/music/acoustid", `{"api_key":"a-better-key-value"}`, session, false); out.Code != 403 {
		t.Fatal("save without CSRF", out.Code)
	}
	if out := request(handler, "PUT", "/api/v1/library/music/acoustid", `{"api_key":"short"}`, session, true); out.Code != 400 {
		t.Fatal("short key", out.Code, out.Body.String())
	}
	if out := request(handler, "PUT", "/api/v1/library/music/acoustid", `{"api_key":"bad\u0001key-value"}`, session, true); out.Code != 400 {
		t.Fatal("control key", out.Code, out.Body.String())
	}
	out = request(handler, "PUT", "/api/v1/library/music/acoustid", `{"api_key":"a-better-key-value"}`, session, true)
	if out.Code != 200 || service.savedKey != "a-better-key-value" || !strings.Contains(out.Body.String(), `"configured":true`) {
		t.Fatal("save", out.Code, out.Body.String(), service.savedKey)
	}
	out = request(handler, "DELETE", "/api/v1/library/music/acoustid", "", session, true)
	if out.Code != 200 || !service.deletedKey || !strings.Contains(out.Body.String(), `"configured":false`) {
		t.Fatal("delete", out.Code, out.Body.String())
	}
	if out := request(handler, "DELETE", "/api/v1/library/music/acoustid", "", session, false); out.Code != 403 {
		t.Fatal("delete without CSRF", out.Code)
	}
}

func TestMusicScanFingerprintRoutes(t *testing.T) {
	service := &fakeMusic{fingerprintResult: music.DiscoverResult{
		Selected: 1, Matched: 1, Fingerprinted: 1, ProviderCalls: 1, Warnings: []string{},
	}}
	handler, session := musicFixture(t, service)
	if out := request(handler, "POST", "/api/v1/library/music/scans/1/fingerprint", `{"file_ids":[1],"reviewed":true}`, nil, false); out.Code != 401 {
		t.Fatal("unauthenticated", out.Code)
	}
	out := request(handler, "POST", "/api/v1/library/music/scans/1/fingerprint", `{"file_ids":[1],"reviewed":true}`, session, true)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"fingerprinted":1`) || !strings.Contains(out.Body.String(), `"provider_calls":1`) {
		t.Fatal("fingerprint", out.Code, out.Body.String())
	}
	if out := request(handler, "POST", "/api/v1/library/music/scans/1/fingerprint", `{"file_ids":[1]}`, session, true); out.Code != 400 {
		t.Fatal("without review", out.Code)
	}
	if out := request(handler, "POST", "/api/v1/library/music/scans/1/fingerprint", `{"file_ids":[1],"all":true,"reviewed":true}`, session, true); out.Code != 400 {
		t.Fatal("conflicting selection", out.Code)
	}
	if out := request(handler, "POST", "/api/v1/library/music/scans/1/fingerprint", `{"file_ids":[],"reviewed":true}`, session, true); out.Code != 400 {
		t.Fatal("empty selection", out.Code)
	}
	ids := make([]string, 501)
	for i := range ids {
		ids[i] = "1"
	}
	if out := request(handler, "POST", "/api/v1/library/music/scans/1/fingerprint", `{"file_ids":[`+strings.Join(ids, ",")+`],"reviewed":true}`, session, true); out.Code != 400 {
		t.Fatal("oversized selection", out.Code)
	}
	if out := request(handler, "POST", "/api/v1/library/music/scans/1/fingerprint", `{"file_ids":[1],"reviewed":true}`, session, true); out.Code != 200 {
		t.Fatal("fingerprint after errors", out.Code, out.Body.String())
	}
	service.err = &music.Error{Code: "music_acoustid_invalid_key", Message: "AcoustID rejected the application key."}
	if out := request(handler, "POST", "/api/v1/library/music/scans/1/fingerprint", `{"file_ids":[1],"reviewed":true}`, session, true); out.Code != 400 {
		t.Fatal("invalid key mapping", out.Code, out.Body.String())
	}
	service.err = &music.Error{Code: "music_acoustid_rate_limited", Message: "AcoustID is limiting requests.", RetryAfter: 9}
	out = request(handler, "POST", "/api/v1/library/music/scans/1/fingerprint", `{"file_ids":[1],"reviewed":true}`, session, true)
	if out.Code != 429 || out.Header().Get("Retry-After") != "9" {
		t.Fatal("rate limit mapping", out.Code, out.Header().Get("Retry-After"))
	}
	service.err = nil
	if out := request(handler, "GET", "/api/v1/library/music/scans/1/fingerprint", "", session, false); out.Code != 405 {
		t.Fatal("method", out.Code)
	}
}
