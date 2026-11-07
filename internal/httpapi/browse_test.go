package httpapi

import (
	"context"
	"encoding/json"
	"github.com/faaaaq/ReelScarey/internal/library"
	"strings"
	"testing"
)

func TestBrowseAuthenticatedStrictFiltersAndReadOnly(t *testing.T) {
	handler, db, accounts, code := rawFixture(t, nil)
	session, e := accounts.Setup(context.Background(), "testadmin", "test-only-password", code)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO media_items(kind,tmdb_id,title,monitored,status) VALUES('movie',1,'Saved title',0,'available'),('movie',2,'Other title',1,'missing')`); e != nil {
		t.Fatal(e)
	}
	path := "/api/v1/library?kind=movie&page=1&query=Saved&monitored=paused&status=available&sort=added"
	if out := request(handler, "GET", path, "", nil, false); out.Code != 401 {
		t.Fatal("anonymous catalog exposed", out.Code)
	}
	out := request(handler, "GET", path, "", &session, false)
	var page library.Page
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil || page.Total != 1 || page.CollectionTotal != 2 || page.Items[0].Title != "Saved title" {
		t.Fatal(out.Code, out.Body)
	}
	out = request(handler, "GET", "/api/v1/library?kind=movie&page=all&sort=title", "", &session, false)
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil || page.Page != 1 || page.Limit != 2 || page.Total != 2 || len(page.Items) != 2 || page.CollectionTotal != 2 {
		t.Fatal("page=all shape", out.Code, out.Body)
	}
	if _, e = db.Exec(`WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n<5001) INSERT INTO media_items(kind,tmdb_id,title) SELECT 'movie',10000+n,'Bulk '||n FROM seq`); e != nil {
		t.Fatal(e)
	}
	out = request(handler, "GET", "/api/v1/library?kind=movie&page=all", "", &session, false)
	if out.Code != 400 || !strings.Contains(out.Body.String(), `"code":"library_too_large"`) {
		t.Fatal("unbounded page=all was not rejected clearly", out.Code, out.Body)
	}
	for _, suffix := range []string{"&sort=unknown", "&status=failed", "&monitored=no", "&query=%0A", "&query=%", "&sort=title&sort=added", "&unexpected=x"} {
		if out = request(handler, "GET", "/api/v1/library?kind=movie"+suffix, "", &session, false); out.Code != 400 {
			t.Fatal("invalid browse accepted", suffix, out.Code)
		}
	}
	var n int
	if e = db.QueryRow(`SELECT (SELECT COUNT(*) FROM downloads)+(SELECT COUNT(*) FROM library_scans)+(SELECT COUNT(*) FROM automation_runs)`).Scan(&n); e != nil || n != 0 {
		t.Fatal("browse started work", n, e)
	}
}
