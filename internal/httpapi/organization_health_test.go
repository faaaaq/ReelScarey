package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFileHealthReportsSavedContentWithoutChangingIt(t *testing.T) {
	f := newOrganizationReviewFixture(t)
	f.movie(t, 1, "Health film", "Movies/Health/film.mkv")
	endpoint := "/api/v1/organization/files/1/health"
	if out := request(f.handler, "POST", endpoint, `{}`, nil, false); out.Code != 401 {
		t.Fatalf("anonymous: %d", out.Code)
	}
	if out := request(f.handler, "POST", endpoint, `{}`, &f.session, false); out.Code != 403 {
		t.Fatalf("missing CSRF: %d", out.Code)
	}
	check := func(want string) {
		t.Helper()
		out := request(f.handler, "POST", endpoint, `{}`, &f.session, true)
		if out.Code != 200 {
			t.Fatalf("health response: %d %s", out.Code, out.Body.String())
		}
		var result struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(out.Body.Bytes(), &result); err != nil || result.Status != want {
			t.Fatalf("got %s, want %s: %v", out.Body.String(), want, err)
		}
	}
	check("verified")
	name := filepath.Join(f.cfg.MediaDir, "Movies", "Health", "film.mkv")
	body, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	body[0] ^= 1
	if err = os.WriteFile(name, body, 0600); err != nil {
		t.Fatal(err)
	}
	check("changed")
	if err = os.Remove(name); err != nil {
		t.Fatal(err)
	}
	check("missing")
	var count int
	if err = f.db.QueryRow(`SELECT count(*) FROM media_files WHERE id=1`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("registration changed: %d %v", count, err)
	}
}
