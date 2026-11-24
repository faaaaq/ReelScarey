package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/music"
)

type fakeMusicPlaylists struct {
	fakeMusic
	list         music.LibraryPlaylistList
	detail       music.LibraryPlaylistDetails
	err          error
	createdName  string
	createdTrack *int64
	renamedID    int64
	renamedName  string
	deletedID    int64
	trackID      int64
	playlistID   int64
	revision     int
	reordered    []int64
}

func (f *fakeMusicPlaylists) ListLibraryPlaylists(context.Context) (music.LibraryPlaylistList, error) {
	return f.list, f.err
}
func (f *fakeMusicPlaylists) LibraryPlaylist(_ context.Context, id int64) (music.LibraryPlaylistDetails, error) {
	f.playlistID = id
	return f.detail, f.err
}
func (f *fakeMusicPlaylists) CreateLibraryPlaylist(_ context.Context, name string, trackID *int64) (music.LibraryPlaylistDetails, error) {
	f.createdName = name
	if trackID != nil {
		value := *trackID
		f.createdTrack = &value
	}
	return f.detail, f.err
}
func (f *fakeMusicPlaylists) RenameLibraryPlaylist(_ context.Context, id int64, name string, revision int) (music.LibraryPlaylistDetails, error) {
	f.renamedID, f.renamedName, f.revision = id, name, revision
	return f.detail, f.err
}
func (f *fakeMusicPlaylists) DeleteLibraryPlaylist(_ context.Context, id int64, revision int) error {
	f.deletedID, f.revision = id, revision
	return f.err
}
func (f *fakeMusicPlaylists) AddLibraryPlaylistTrack(_ context.Context, id, trackID int64, revision int) (music.LibraryPlaylistDetails, error) {
	f.playlistID, f.trackID, f.revision = id, trackID, revision
	return f.detail, f.err
}
func (f *fakeMusicPlaylists) RemoveLibraryPlaylistTrack(_ context.Context, id, trackID int64, revision int) (music.LibraryPlaylistDetails, error) {
	f.playlistID, f.trackID, f.revision = id, trackID, revision
	return f.detail, f.err
}
func (f *fakeMusicPlaylists) ReorderLibraryPlaylist(_ context.Context, id int64, trackIDs []int64, revision int) (music.LibraryPlaylistDetails, error) {
	f.playlistID, f.reordered, f.revision = id, append([]int64(nil), trackIDs...), revision
	return f.detail, f.err
}

func TestMusicLibraryPlaylistRoutesAndJSONContract(t *testing.T) {
	fileID := int64(101)
	year := 2024
	summary := music.LibraryPlaylist{ID: 7, Name: "Focus", TrackCount: 2, DurationMS: 123456, Revision: 4, CreatedAt: 10, UpdatedAt: 20}
	service := &fakeMusicPlaylists{
		list: music.LibraryPlaylistList{Items: []music.LibraryPlaylist{summary}},
		detail: music.LibraryPlaylistDetails{Playlist: summary, Tracks: []music.ArtistTrack{{
			Track:      music.Track{ID: 11, AlbumID: 2, MusicBrainzID: "track-mbid", RecordingID: "recording-mbid", Disc: 1, Number: 3, Title: "One", DurationMS: 1000, FileID: &fileID, Bitrate: 900, SizeBytes: 1234, Format: "flac", RelativePath: "Music/one.flac"},
			ArtistName: "Artist", AlbumTitle: "Album", AlbumYear: &year, CoverURL: "cover.jpg",
		}, {
			Track: music.Track{ID: 12, AlbumID: 2, Disc: 1, Number: 4, Title: "Two", DurationMS: 2000}, ArtistName: "Artist", AlbumTitle: "Album", AlbumYear: &year,
		}}},
	}
	handler, session := musicFixture(t, service)

	out := request(handler, "GET", "/api/v1/library/music/playlists", "", session, false)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"items":[{"id":7,"name":"Focus","track_count":2,"duration_ms":123456,"revision":4,"created_at":10,"updated_at":20}]`) {
		t.Fatalf("list contract: %d %s", out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/music/playlists", `{"name":"  Focus  ","track_id":11}`, session, true)
	if out.Code != 201 || service.createdName != "  Focus  " || service.createdTrack == nil || *service.createdTrack != 11 {
		t.Fatalf("create: %d %s track=%v name=%q", out.Code, out.Body.String(), service.createdTrack, service.createdName)
	}
	for _, fragment := range []string{`"playlist":{"id":7`, `"tracks":[{"id":11`, `"file_id":101`, `"relative_path":"Music/one.flac"`, `"artist_name":"Artist"`, `"album_title":"Album"`, `"album_year":2024`, `"cover_url":"cover.jpg"`, `"title":"Two","duration_ms":2000,"artist_name":"Artist","album_title":"Album"`} {
		if !strings.Contains(out.Body.String(), fragment) {
			t.Fatalf("detail JSON missing %s: %s", fragment, out.Body.String())
		}
	}
	if strings.Contains(out.Body.String(), `"title":"Two","duration_ms":2000,"file_id"`) {
		t.Fatalf("unplayable track fabricated a file: %s", out.Body.String())
	}

	out = request(handler, "GET", "/api/v1/library/music/playlists/7", "", session, false)
	if out.Code != 200 || service.playlistID != 7 {
		t.Fatalf("detail: %d %s", out.Code, out.Body.String())
	}
	out = request(handler, "PUT", "/api/v1/library/music/playlists/7", `{"name":"Renamed","revision":4}`, session, true)
	if out.Code != 200 || service.renamedID != 7 || service.renamedName != "Renamed" || service.revision != 4 {
		t.Fatalf("rename: %d %s", out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/music/playlists/7/tracks", `{"track_id":12,"revision":4}`, session, true)
	if out.Code != 200 || service.playlistID != 7 || service.trackID != 12 || service.revision != 4 {
		t.Fatalf("add: %d %s", out.Code, out.Body.String())
	}
	out = request(handler, "PUT", "/api/v1/library/music/playlists/7/tracks", `{"track_ids":[12,11],"revision":5}`, session, true)
	if out.Code != 200 || service.revision != 5 || len(service.reordered) != 2 || service.reordered[0] != 12 || service.reordered[1] != 11 {
		t.Fatalf("reorder: %d %s ids=%v", out.Code, out.Body.String(), service.reordered)
	}
	out = request(handler, "DELETE", "/api/v1/library/music/playlists/7/tracks", `{"track_id":12,"revision":6}`, session, true)
	if out.Code != 200 || service.trackID != 12 || service.revision != 6 {
		t.Fatalf("remove: %d %s", out.Code, out.Body.String())
	}
	out = request(handler, "DELETE", "/api/v1/library/music/playlists/7", `{"revision":7}`, session, true)
	if out.Code != 200 || service.deletedID != 7 || service.revision != 7 || strings.TrimSpace(out.Body.String()) != `{"deleted":true}` {
		t.Fatalf("delete: %d %s", out.Code, out.Body.String())
	}

	service.err = music.ErrRevisionConflict
	out = request(handler, "PUT", "/api/v1/library/music/playlists/7", `{"name":"Stale","revision":3}`, session, true)
	if out.Code != 409 || !strings.Contains(out.Body.String(), `"code":"music_revision_conflict"`) {
		t.Fatalf("stale revision: %d %s", out.Code, out.Body.String())
	}
	service.err = music.ErrLibraryPlaylistTrackExists
	out = request(handler, "POST", "/api/v1/library/music/playlists/7/tracks", `{"track_id":11,"revision":4}`, session, true)
	if out.Code != 409 || !strings.Contains(out.Body.String(), `"code":"music_playlist_track_exists"`) {
		t.Fatalf("duplicate: %d %s", out.Code, out.Body.String())
	}
	service.err = music.ErrNotFound
	for _, route := range []struct{ method, path, body string }{
		{"GET", "/api/v1/library/music/playlists/999", ""},
		{"POST", "/api/v1/library/music/playlists/7/tracks", `{"track_id":999,"revision":4}`},
	} {
		out = request(handler, route.method, route.path, route.body, session, route.method != "GET")
		if out.Code != 404 {
			t.Fatalf("not found %s: %d %s", route.path, out.Code, out.Body.String())
		}
	}
}

func TestMusicLibraryPlaylistEmptyArraysAndMethodBehavior(t *testing.T) {
	service := &fakeMusicPlaylists{list: music.LibraryPlaylistList{Items: []music.LibraryPlaylist{}}, detail: music.LibraryPlaylistDetails{Playlist: music.LibraryPlaylist{ID: 1, Name: "Empty", Revision: 1}, Tracks: []music.ArtistTrack{}}}
	handler, session := musicFixture(t, service)
	if out := request(handler, "GET", "/api/v1/library/music/playlists", "", session, false); out.Code != 200 || strings.TrimSpace(out.Body.String()) != `{"items":[]}` {
		t.Fatalf("empty list: %d %s", out.Code, out.Body.String())
	}
	if out := request(handler, "GET", "/api/v1/library/music/playlists/1", "", session, false); out.Code != 200 || !strings.Contains(out.Body.String(), `"tracks":[]`) {
		t.Fatalf("empty detail: %d %s", out.Code, out.Body.String())
	}
	for _, route := range []struct{ method, path, allow string }{
		{"PATCH", "/api/v1/library/music/playlists", "GET, POST"},
		{"POST", "/api/v1/library/music/playlists/1", "GET, PUT, DELETE"},
		{"GET", "/api/v1/library/music/playlists/1/tracks", "POST, PUT, DELETE"},
	} {
		out := request(handler, route.method, route.path, `{}`, session, true)
		if out.Code != 405 || out.Header().Get("Allow") != route.allow {
			t.Fatalf("method %s %s: %d Allow=%q body=%s", route.method, route.path, out.Code, out.Header().Get("Allow"), out.Body.String())
		}
	}
}

func TestMusicLibraryPlaylistAuthenticationRequesterAndCSRF(t *testing.T) {
	handler, session := musicFixture(t, &fakeMusicPlaylists{list: music.LibraryPlaylistList{Items: []music.LibraryPlaylist{}}})
	if out := request(handler, "GET", "/api/v1/library/music/playlists", "", nil, false); out.Code != 401 {
		t.Fatalf("anonymous read = %d", out.Code)
	}
	if out := request(handler, "POST", "/api/v1/library/music/playlists", `{"name":"No CSRF"}`, session, false); out.Code != 403 {
		t.Fatalf("mutation without CSRF = %d", out.Code)
	}
	if out := request(handler, "PUT", "/api/v1/library/music/playlists/1/tracks", `{"track_ids":[],"revision":1}`, session, false); out.Code != 403 {
		t.Fatalf("reorder without CSRF = %d", out.Code)
	}

	h, _, authService, code := rawFixture(t, nil)
	ctx := context.Background()
	if _, err := authService.Setup(ctx, "admin", "admin-test-password", code); err != nil {
		t.Fatal(err)
	}
	if _, err := authService.CreateRequestAccount(ctx, "viewer", "viewer-test-password"); err != nil {
		t.Fatal(err)
	}
	requester, err := authService.Login(ctx, "viewer", "viewer-test-password")
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct{ method, body string }{{"GET", ""}, {"POST", `{"name":"Requester"}`}} {
		req := httptest.NewRequest(route.method, "/api/v1/library/music/playlists", strings.NewReader(route.body))
		req.AddCookie(&http.Cookie{Name: cookieName, Value: requester.Token})
		req.Header.Set("X-CSRF-Token", requester.CSRFToken)
		req.Header.Set("Origin", "http://example.com")
		req.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		h.ServeHTTP(out, req)
		if out.Code != 403 {
			t.Fatalf("requester %s playlists = %d %s", route.method, out.Code, out.Body.String())
		}
	}
}
