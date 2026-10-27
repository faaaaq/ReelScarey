package httpapi

import (
	"context"
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
