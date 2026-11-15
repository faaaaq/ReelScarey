package httpapi

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/youtube"
)

func TestYouTubeRoutesRequireSessionAndMutationOrigin(t *testing.T) {
	raw, _, _, _ := rawFixture(t, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/youtube/jobs", nil)
	out := httptest.NewRecorder()
	raw.ServeHTTP(out, request)
	if out.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous jobs: %d", out.Code)
	}
	h, _ := fixture(t)
	request = httptest.NewRequest(http.MethodPost, "/api/v1/youtube/collections", strings.NewReader(`{"title":"XG Days"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://evil.test")
	out = httptest.NewRecorder()
	h.ServeHTTP(out, request)
	if out.Code != http.StatusForbidden {
		t.Fatalf("wrong origin: %d", out.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/youtube/collections", strings.NewReader(`{"title":"XG Days"}`))
	request.Header.Set("Content-Type", "application/json")
	out = httptest.NewRecorder()
	h.ServeHTTP(out, request)
	if out.Code != http.StatusCreated {
		t.Fatalf("collection: %d %s", out.Code, out.Body.String())
	}
}

type fakeYouTubeInspector struct{}

func (fakeYouTubeInspector) Inspect(_ context.Context, input string) (youtube.Inspection, error) {
	return youtube.Inspection{Source: youtube.Source{Kind: "video", ID: "boKAdg0BS-c", URL: input}, Entries: []youtube.Entry{{Kind: "video", ID: "boKAdg0BS-c", Title: "Verified title", Channel: "XG"}}}, nil
}

// seriesSeed inserts one monitored show with two episodes and one paused
// YouTube source, enough for the read/write route contract checks.
func seriesSeed(t *testing.T, db *sql.DB) (sourceID string, revision int) {
	t.Helper()
	for _, query := range []string{
		`INSERT INTO media_items(id,kind,tmdb_id,title,year,status,monitored) VALUES(1,'series',1,'XTRA XG',2024,'missing',1)`,
		`INSERT INTO series_details(media_id) VALUES(1)`,
		`INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored) VALUES(1,1,1,1,'Season 1','','',2,1)`,
		`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(1,1,11,1,'One','','',1)`,
		`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(2,1,12,2,'Two','','',1)`,
		`INSERT INTO youtube_series_sources(id,media_id,source_url,title,mapping,season_number,start_episode,quality,subtitles_json,auto_import,enabled,interval_hours,created_at) VALUES('sourcesrc00001',1,'https://www.youtube.com/channel/UC1234567890123456789012','XTRA XG','episode',1,1,'best','{"languages":[],"automatic":false,"quality":"best"}',1,0,6,unixepoch())`,
		`INSERT INTO youtube_series_entries(source_id,video_id,position,title,channel,state,note,discovered_at) VALUES('sourcesrc00001','aaaaaaaaaaa',0,'XTRA XG S01E02','XG','baseline','',unixepoch())`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(query, err)
		}
	}
	return "sourcesrc00001", 1
}

func TestYouTubeSourcesRoutesContract(t *testing.T) {
	raw, _, _, _ := rawFixture(t, nil)
	// The overview, episode table and queue action are administrator-scoped.
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1/youtube/sources", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/youtube/sources/sourcesrc00001/episodes", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/youtube/sources/sourcesrc00001/episodes/queue", strings.NewReader(`{"revision":1,"reviewed":true,"selections":[]}`)),
	} {
		out := httptest.NewRecorder()
		raw.ServeHTTP(out, request)
		if out.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous %s: %d", request.URL.Path, out.Code)
		}
	}
	h, db := fixture(t)
	sourceID, revision := seriesSeed(t, db)
	// Method guards.
	out := httptest.NewRecorder()
	h.ServeHTTP(out, httptest.NewRequest(http.MethodPost, "/api/v1/youtube/sources", nil))
	if out.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post overview: %d", out.Code)
	}
	// The overview is a pure read: an empty library answers 200 even though
	// no yt-dlp inspector is configured on this server.
	out = httptest.NewRecorder()
	h.ServeHTTP(out, httptest.NewRequest(http.MethodGet, "/api/v1/youtube/sources", nil))
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"show_title":"XTRA XG"`) || !strings.Contains(out.Body.String(), `"counts"`) {
		t.Fatalf("overview: %d %s", out.Code, out.Body.String())
	}
	// Episode table and 404 on an unknown source.
	out = httptest.NewRecorder()
	h.ServeHTTP(out, httptest.NewRequest(http.MethodGet, "/api/v1/youtube/sources/"+sourceID+"/episodes", nil))
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"items":[]`) {
		t.Fatalf("episodes: %d %s", out.Code, out.Body.String())
	}
	out = httptest.NewRecorder()
	h.ServeHTTP(out, httptest.NewRequest(http.MethodGet, "/api/v1/youtube/sources/unknownsrc01/episodes", nil))
	if out.Code != http.StatusNotFound {
		t.Fatalf("unknown source: %d", out.Code)
	}
	// Queue action: explicit review required, revision-fenced.
	unreviewed := httptest.NewRequest(http.MethodPost, "/api/v1/youtube/sources/"+sourceID+"/episodes/queue", strings.NewReader(`{"revision":`+strconv.Itoa(revision)+`,"reviewed":false,"selections":[{"video_id":"aaaaaaaaaaa","episode_id":2}]}`))
	unreviewed.Header.Set("Content-Type", "application/json")
	out = httptest.NewRecorder()
	h.ServeHTTP(out, unreviewed)
	if out.Code != 400 || !strings.Contains(out.Body.String(), "review_required") {
		t.Fatalf("unreviewed queue: %d %s", out.Code, out.Body.String())
	}
	stale := httptest.NewRequest(http.MethodPost, "/api/v1/youtube/sources/"+sourceID+"/episodes/queue", strings.NewReader(`{"revision":`+strconv.Itoa(revision+99)+`,"reviewed":true,"selections":[{"video_id":"aaaaaaaaaaa","episode_id":2}]}`))
	stale.Header.Set("Content-Type", "application/json")
	out = httptest.NewRecorder()
	h.ServeHTTP(out, stale)
	if out.Code != http.StatusConflict || !strings.Contains(out.Body.String(), "youtube_changed") {
		t.Fatalf("stale revision: %d %s", out.Code, out.Body.String())
	}
	queued := httptest.NewRequest(http.MethodPost, "/api/v1/youtube/sources/"+sourceID+"/episodes/queue", strings.NewReader(`{"revision":`+strconv.Itoa(revision)+`,"reviewed":true,"selections":[{"video_id":"aaaaaaaaaaa","episode_id":2}]}`))
	queued.Header.Set("Content-Type", "application/json")
	out = httptest.NewRecorder()
	h.ServeHTTP(out, queued)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"job_id"`) || !strings.Contains(out.Body.String(), `"state":"queued"`) {
		t.Fatalf("queue: %d %s", out.Code, out.Body.String())
	}
	out = httptest.NewRecorder()
	h.ServeHTTP(out, httptest.NewRequest(http.MethodGet, "/api/v1/youtube/sources/"+sourceID+"/episodes", nil))
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"state":"queued"`) || !strings.Contains(out.Body.String(), `"english_subtitles":null`) {
		t.Fatalf("queued episode: %d %s", out.Code, out.Body.String())
	}
	// The jobs feed answers counts and filters.
	out = httptest.NewRecorder()
	h.ServeHTTP(out, httptest.NewRequest(http.MethodGet, "/api/v1/youtube/jobs?state=queued&series_source_id="+sourceID, nil))
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"counts"`) || !strings.Contains(out.Body.String(), `"queued":1`) {
		t.Fatalf("jobs: %d %s", out.Code, out.Body.String())
	}
	out = httptest.NewRecorder()
	h.ServeHTTP(out, httptest.NewRequest(http.MethodGet, "/api/v1/youtube/jobs?state=bogus", nil))
	if out.Code != 400 {
		t.Fatalf("bogus state: %d", out.Code)
	}
	out = httptest.NewRecorder()
	h.ServeHTTP(out, httptest.NewRequest(http.MethodGet, "/api/v1/youtube/jobs?media_id=nope", nil))
	if out.Code != 400 {
		t.Fatalf("bogus media: %d", out.Code)
	}
}

func TestYouTubeReviewUsesInspectedMetadata(t *testing.T) {
	_, db := fixture(t)
	store := youtube.NewStore(db)
	collection, err := store.CreateCollection(context.Background(), "Xtra XG")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{youtube: store, youtubeInspector: fakeYouTubeInspector{}}
	body := `{"video_id":"boKAdg0BS-c","target":{"kind":"custom","collection_id":` + strconv.FormatInt(collection.ID, 10) + `},"subtitles":{"languages":["en"],"automatic":false}}`
	r := httptest.NewRequest(http.MethodPost, "/api/v1/youtube/reviews", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	out := httptest.NewRecorder()
	s.youtubeReview(out, r)
	if out.Code != 200 || !strings.Contains(out.Body.String(), "Verified title") || !strings.Contains(out.Body.String(), `"languages":["en"]`) {
		t.Fatalf("review: %d %s", out.Code, out.Body.String())
	}
}
