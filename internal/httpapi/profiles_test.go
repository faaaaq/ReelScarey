package httpapi

import (
	"context"
	"encoding/json"
	"github.com/faaaaq/ReelScarey/internal/automation"
	"github.com/faaaaq/ReelScarey/internal/config"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestReviewedProfileAPISecurityAndCatalogContext(t *testing.T) {
	_, db, accounts, code := rawFixture(t, nil)
	ctx := context.Background()
	session, e := accounts.Setup(ctx, "testadmin", "test-only-password", code)
	if e != nil {
		t.Fatal(e)
	}
	handler := New(db, config.Config{PublicURL: "http://example.com"}, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{"index.html": {Data: []byte("test")}}, "test", Dependencies{Auth: accounts, Automation: automation.New(db, nil)})
	if _, e = db.Exec(`INSERT INTO media_items(kind,tmdb_id,title) VALUES('movie',1,'Saved title')`); e != nil {
		t.Fatal(e)
	}
	preview := `{"kind":"movie","ids":[1],"target":{"profile_id":2}}`
	path := "/api/v1/library/profiles/preview"
	for _, route := range []struct{ method, path, body string }{{"POST", path, preview}, {"PUT", "/api/v1/library/profiles", `{}`}} {
		if out := request(handler, route.method, route.path, route.body, nil, false); out.Code != 401 {
			t.Fatal(out.Code)
		}
		if out := request(handler, route.method, route.path, route.body, &session, false); out.Code != 403 {
			t.Fatal(out.Code)
		}
	}
	r := httptest.NewRequest("POST", path, strings.NewReader(preview))
	r.Header.Set("Origin", "https://other.invalid")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	if rec.Code != 403 {
		t.Fatal("cross-origin accepted")
	}
	out := request(handler, "POST", path, preview, &session, true)
	var review automation.ProfileReview
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &review) != nil || len(review.Items) != 1 || review.Items[0].Effective.ID != 1 || review.TargetProfile.ID != 2 {
		t.Fatal(out.Code, out.Body)
	}
	in := automation.ProfileApply{Kind: "movie", Target: review.Target, TargetID: review.TargetProfile.ID, TargetRevision: review.TargetProfile.Revision, ScheduleRevision: review.ScheduleRevision, Items: []automation.ProfileExpectation{{ID: 1, Revision: 0, EffectiveID: 1, EffectiveRevision: 1}}}
	body, _ := json.Marshal(in)
	out = request(handler, "PUT", "/api/v1/library/profiles", string(body), &session, true)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	if out = request(handler, "PUT", "/api/v1/library/profiles", string(body), &session, true); out.Code != 409 {
		t.Fatal("stale apply accepted", out.Code, out.Body)
	}
	out = request(handler, "GET", "/api/v1/library?kind=movie&profile=2", "", &session, false)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"profile_context"`) || !strings.Contains(out.Body.String(), `"quality_profile"`) || !strings.Contains(out.Body.String(), `"name":"Ultra HD"`) {
		t.Fatal(out.Code, out.Body)
	}
	for _, bad := range []string{`{}`, `{"kind":"movie","ids":[1,1],"target":{"use_default":true}}`, `{"kind":"movie","ids":[1],"target":{"profile_id":2},"all":true}`} {
		if out = request(handler, "POST", path, bad, &session, true); out.Code != 400 {
			t.Fatal("invalid preview accepted", out.Code, out.Body)
		}
	}
	if out = request(handler, "GET", path, "", &session, false); out.Code != 405 {
		t.Fatal(out.Code)
	}
	if out = request(handler, "GET", "/api/v1/library?kind=movie&profile=default&profile=2", "", &session, false); out.Code != 400 {
		t.Fatal(out.Code)
	}
}
