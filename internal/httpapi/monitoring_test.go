package httpapi

import (
	"context"
	"encoding/json"
	"github.com/faaaaq/ReelScarey/internal/library"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBulkMonitoringSecurityReviewAndAtomicConflict(t *testing.T) {
	handler, db, accounts, code := rawFixture(t, nil)
	session, e := accounts.Setup(context.Background(), "testadmin", "test-only-password", code)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO media_items(kind,tmdb_id,title,monitored) VALUES('movie',1,'First title',1),('movie',2,'Second title',1)`); e != nil {
		t.Fatal(e)
	}
	preview := `{"kind":"movie","ids":[1,2]}`
	path := "/api/v1/library/monitoring/preview"
	for _, endpoint := range []struct{ method, path, body string }{{"POST", path, preview}, {"PUT", "/api/v1/library/monitoring", `{}`}} {
		if out := request(handler, endpoint.method, endpoint.path, endpoint.body, nil, false); out.Code != 401 {
			t.Fatal(out.Code)
		}
		if out := request(handler, endpoint.method, endpoint.path, endpoint.body, &session, false); out.Code != 403 {
			t.Fatal("missing CSRF accepted", out.Code)
		}
	}
	r := httptest.NewRequest("POST", path, strings.NewReader(preview))
	r.Header.Set("Origin", "https://other.invalid")
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, r)
	if out.Code != 403 {
		t.Fatal("foreign origin accepted")
	}
	out = request(handler, "POST", path, preview, &session, true)
	var review struct {
		Items []library.MonitoringItem `json:"items"`
	}
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &review) != nil || len(review.Items) != 2 {
		t.Fatal(out.Code, out.Body)
	}
	body := `{"kind":"movie","monitored":false,"items":[{"id":1,"monitored":true,"series_revision":0,"automation_revision":0},{"id":2,"monitored":false,"series_revision":0,"automation_revision":0}]}`
	if out = request(handler, "PUT", "/api/v1/library/monitoring", body, &session, true); out.Code != 409 {
		t.Fatal(out.Code, out.Body)
	}
	var count int
	db.QueryRow(`SELECT SUM(monitored) FROM media_items`).Scan(&count)
	if count != 2 {
		t.Fatal("partial update")
	}
	body = strings.ReplaceAll(body, `"id":2,"monitored":false`, `"id":2,"monitored":true`)
	out = request(handler, "PUT", "/api/v1/library/monitoring", body, &session, true)
	var result library.MonitoringResult
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &result) != nil || result.Changed != 2 {
		t.Fatal(out.Code, out.Body)
	}
	for _, bad := range []string{`{}`, `{"kind":"movie","monitored":false,"items":[{"id":1}]}`, `{"kind":"movie","monitored":false,"items":[],"all":true}`} {
		if out = request(handler, "PUT", "/api/v1/library/monitoring", bad, &session, true); out.Code != 400 {
			t.Fatal("bad input accepted", bad, out.Code)
		}
	}
	if out = request(handler, "POST", path, `{"kind":"movie","ids":[1,1]}`, &session, true); out.Code != 400 {
		t.Fatal(out.Code)
	}
	if out = request(handler, "POST", path, `{"kind":"movie","ids":[999]}`, &session, true); out.Code != 404 {
		t.Fatal(out.Code)
	}
	if out = request(handler, "GET", path, "", &session, false); out.Code != 405 {
		t.Fatal(out.Code)
	}
	db.QueryRow(`SELECT (SELECT COUNT(*) FROM downloads)+(SELECT COUNT(*) FROM automation_runs)+(SELECT COUNT(*) FROM library_scans)`).Scan(&count)
	if count != 0 {
		t.Fatal("bulk started jobs")
	}
}
