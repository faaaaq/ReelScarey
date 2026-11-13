package httpapi

import (
	"context"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/music"
)

// RS-0023: subsonicAllSongs built the query bound as size+offset where the
// client-supplied offset is capped at 1,000,000, so a paged client sync over a
// large catalog could materialize up to ~1,000,500 rows per request. The
// offset must be clamped against the actual catalog size so the scan bound
// stays near the result-set size. These tests use a fake service that
// captures the limit argument ServerSongs is called with.
type offsetProbeMusic struct {
	*fakeMusic
	songs  []music.ServerSong
	limits []int
}

func (f *offsetProbeMusic) ServerSongs(ctx context.Context, query string, limit int) ([]music.ServerSong, error) {
	f.limits = append(f.limits, limit)
	if len(f.songs) > limit {
		return f.songs[:limit], nil
	}
	return f.songs, nil
}

func (f *offsetProbeMusic) ServerSongCount(ctx context.Context) (int, error) {
	return len(f.songs), nil
}

type albumProbeMusic struct {
	*fakeMusic
	offsets []int
}

func (f *albumProbeMusic) ServerAlbums(ctx context.Context, order string, size, offset int) ([]music.Album, error) {
	f.offsets = append(f.offsets, offset)
	return []music.Album{}, nil
}

func offsetTestSongs(n int) []music.ServerSong {
	songs := make([]music.ServerSong, n)
	for i := range songs {
		songs[i] = music.ServerSong{FileID: int64(i + 1), Title: "Song " + strconv.Itoa(i+1)}
	}
	return songs
}

func offsetTestSongsIn(body string) int {
	return strings.Count(body, "<song ")
}

func TestSubsonicAllSongsClampsOffsetToCatalogSize(t *testing.T) {
	// RS-0023: subsonicAllSongs fetched size+offset rows (limit up to 1,000,500)
	// per request; the offset must be clamped against the catalog size so the
	// scan bound stays near the result-set size.
	songs := offsetTestSongs(1000)
	probe := &offsetProbeMusic{fakeMusic: &fakeMusic{serverTokenOK: true, serverSongCount: 1000}, songs: songs}
	handler, _ := musicFixture(t, probe)
	request := func(rawURL string) *httptest.ResponseRecorder {
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, httptest.NewRequest("GET", rawURL, nil))
		return out
	}
	const auth = "u=kid&p=test-token&v=1.16.1&c=test"

	// (1) Offset beyond catalog end: an empty song list, no error, and the
	// query bound must not exceed the catalog size. Subsonic clients page, so
	// the empty response is the honest end-of-catalog answer.
	probe.limits = nil
	out := request("/rest/getSongs.view?" + auth + "&offset=5000&size=500")
	if out.Code != 200 || !strings.Contains(out.Body.String(), `status="ok"`) {
		t.Fatalf("offset beyond catalog end must return an ok empty response: %d %s", out.Code, out.Body.String())
	}
	if strings.Contains(out.Body.String(), "<song ") || strings.Contains(out.Body.String(), `"song"`) {
		t.Fatalf("offset beyond catalog end must not return songs: %s", out.Body.String())
	}
	if len(probe.limits) != 0 {
		t.Fatalf("offset beyond catalog end must not query songs with a large limit: %d", probe.limits)
	}

	// (2) Offset within catalog: the same rows as before the change, with the
	// query bound clamped to the catalog size.
	probe.limits = nil
	out = request("/rest/getSongs.view?" + auth + "&offset=700&size=500")
	if out.Code != 200 || !strings.Contains(out.Body.String(), `status="ok"`) {
		t.Fatalf("offset within catalog must return an ok response: %d %s", out.Code, out.Body.String())
	}
	if len(probe.limits) != 1 || probe.limits[0] != 1000 {
		t.Fatalf("offset within catalog must clamp the limit to the catalog size: %d", probe.limits)
	}
	if got := offsetTestSongsIn(out.Body.String()); got != 300 {
		t.Fatalf("offset within catalog must return the same rows as before the change: %d", got)
	}

	// (3) Combined with size bounds: size is capped at 500 and the limit at
	// size+offset clamped to the catalog size.
	probe.limits = nil
	out = request("/rest/getSongs.view?" + auth + "&offset=500&size=1000")
	if out.Code != 200 || !strings.Contains(out.Body.String(), `status="ok"`) {
		t.Fatalf("size above the cap must return an ok response: %d %s", out.Code, out.Body.String())
	}
	if len(probe.limits) != 1 || probe.limits[0] != 1000 {
		t.Fatalf("size must be clamped to 500 and the limit to size+offset: %d", probe.limits)
	}
	if got := offsetTestSongsIn(out.Body.String()); got != 500 {
		t.Fatalf("size above the cap must return the same rows as before the change: %d", got)
	}

	// (4) First page (offset=0, size=500): the limit and the returned rows are
	// byte-compatible with the pre-fix behavior.
	probe.limits = nil
	out = request("/rest/getSongs.view?" + auth + "&offset=0&size=500")
	if out.Code != 200 || !strings.Contains(out.Body.String(), `status="ok"`) {
		t.Fatalf("first page must return an ok response: %d %s", out.Code, out.Body.String())
	}
	if len(probe.limits) != 1 || probe.limits[0] != 500 {
		t.Fatalf("first page must not change the limit: %d", probe.limits)
	}
	if got := offsetTestSongsIn(out.Body.String()); got != 500 {
		t.Fatalf("first page must return the same rows as before the change: %d", got)
	}
}

func TestSubsonicAlbumListClampsOffsetToCap(t *testing.T) {
	// RS-0023 (related): getAlbumList2 parsed offset without the subsonicCount
	// clamp, so a huge offset went straight to SQL OFFSET. The offset must
	// share the same bound so the scan cannot walk past the catalog bound.
	probe := &albumProbeMusic{fakeMusic: &fakeMusic{serverTokenOK: true}}
	handler, _ := musicFixture(t, probe)
	request := func(rawURL string) *httptest.ResponseRecorder {
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, httptest.NewRequest("GET", rawURL, nil))
		return out
	}
	const auth = "u=kid&p=test-token&v=1.16.1&c=test"
	_ = request("/rest/getAlbumList2.view?" + auth + "&offset=2000000000&type=alphabeticalByName")
	if len(probe.offsets) != 1 || probe.offsets[0] != subsonicAllSongsOffsetCap {
		t.Fatalf("getAlbumList2 offset must be clamped to the catalog bound: %d", probe.offsets)
	}
}
