package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/library"
)

func TestPlanningAPIAuthenticationFiltersAndReadOnlyBehavior(t *testing.T) {
	handler, db, accounts, code := rawFixture(t, nil)
	session, e := accounts.Setup(context.Background(), "testadmin", "test-only-password", code)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO media_items(kind,tmdb_id,title,monitored,release_date) VALUES('movie',1,'Saved wanted title',1,'2026-10-01'),('movie',2,'Saved upcoming title',0,'2026-11-01')`); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"/api/v1/wanted", "/api/v1/calendar?month=2026-10"} {
		if out := request(handler, "GET", path, "", nil, false); out.Code != 401 {
			t.Fatal("planning leaked to anonymous request", out.Code)
		}
		out := request(handler, "GET", path, "", &session, false)
		var page library.PlanningPage
		if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil || page.Total != 1 || page.Limit != 50 || !page.OnlyMonitored || page.Items[0].Title != "Saved wanted title" {
			t.Fatal("planning contract", out.Code, out.Body)
		}
		for _, secret := range []string{"payload", "client_url", "secrets.key", "source_path", "info_hash"} {
			if strings.Contains(out.Body.String(), secret) {
				t.Fatal("planning exposed private fields", secret)
			}
		}
		if out = request(handler, "POST", path, "{}", &session, true); out.Code != 405 {
			t.Fatal("planning mutation accepted", out.Code)
		}
	}
	for _, path := range []string{"/api/v1/wanted?page=0", "/api/v1/wanted?page=100001", "/api/v1/wanted?status=available", "/api/v1/wanted?month=2026-10", "/api/v1/wanted?kind=episode", "/api/v1/wanted?monitored=no", "/api/v1/wanted?kind=movie&kind=series", "/api/v1/wanted?query=%", "/api/v1/wanted?query=%0A", "/api/v1/wanted?release=all", "/api/v1/calendar?month=2026-13", "/api/v1/calendar?month=2026-1", "/api/v1/calendar?release=premiere", "/api/v1/calendar?release=", "/api/v1/calendar?release=digital&release=physical", "/api/v1/calendar?unexpected=value"} {
		if out := request(handler, "GET", path, "", &session, false); out.Code != 400 {
			t.Fatal("invalid planning input accepted", path, out.Code, out.Body)
		}
	}
	out := request(handler, "GET", "/api/v1/calendar?month=2026-10&release=digital", "", &session, false)
	if out.Code != 200 {
		t.Fatal("calendar release filter rejected", out.Code, out.Body)
	}
	out = request(handler, "GET", "/api/v1/calendar?month=2026-11&monitored=all&kind=movie&query=upcoming", "", &session, false)
	var page library.PlanningPage
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil || page.Total != 1 || page.Items[0].Monitored || page.Items[0].MonitoringReason == "" {
		t.Fatal("bookmark/paused context lost", out.Body)
	}
	for _, path := range []string{"/wanted", "/calendar"} {
		if out = request(handler, "GET", path, "", nil, false); out.Code != 200 {
			t.Fatal("planning frontend route unavailable", path, out.Code)
		}
	}
	var n int
	if e = db.QueryRow(`SELECT (SELECT COUNT(*) FROM downloads)+(SELECT COUNT(*) FROM library_scans)+(SELECT COUNT(*) FROM automation_runs)`).Scan(&n); e != nil || n != 0 {
		t.Fatal("planning read started work", n, e)
	}
}
