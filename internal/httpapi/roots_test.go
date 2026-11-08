package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLibraryRootsAPIRequiresAuthCSRFAndPersists(t *testing.T) {
	f := newOrganizationReviewFixture(t)
	for _, folder := range []string{"Movies", "Movies 4K", "TV Shows", "Anime", "Music"} {
		if err := os.MkdirAll(filepath.Join(f.cfg.MediaDir, folder), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if out := request(f.handler, "GET", "/api/v1/library/roots", "", nil, false); out.Code != 401 {
		t.Fatalf("unauthenticated roots read: %d", out.Code)
	}
	var current struct {
		Revision  int `json:"revision"`
		Libraries []struct {
			Key   string `json:"key"`
			Roots []struct {
				Path string `json:"path"`
			} `json:"roots"`
		} `json:"libraries"`
	}
	out := request(f.handler, "GET", "/api/v1/library/roots", "", &f.session, false)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	if err := json.Unmarshal(out.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if current.Revision < 1 || len(current.Libraries) != 4 {
		t.Fatal(current)
	}
	body := `{"revision":1,"libraries":[{"key":"movies","roots":["Movies","Movies 4K"]}]}`
	if out = request(f.handler, "PUT", "/api/v1/library/roots", body, &f.session, false); out.Code != 403 {
		t.Fatalf("roots mutation without CSRF: %d %s", out.Code, out.Body)
	}
	body = `{"revision":1,"libraries":[{"key":"movies","roots":["Movies","Movies 4K"]},{"key":"music","roots":["Music"]}]}`
	out = request(f.handler, "PUT", "/api/v1/library/roots", body, &f.session, true)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	var updated struct {
		Revision  int `json:"revision"`
		Libraries []struct {
			Key        string `json:"key"`
			Configured bool   `json:"configured"`
			Roots      []struct {
				Path string `json:"path"`
			} `json:"roots"`
		} `json:"libraries"`
	}
	if err := json.Unmarshal(out.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 {
		t.Fatal(updated)
	}
	for _, library := range updated.Libraries {
		if library.Key == "movies" && (!library.Configured || len(library.Roots) != 2) {
			t.Fatal(library)
		}
	}
	// Unsafe and stale writes are rejected through the same authenticated endpoint.
	if out = request(f.handler, "PUT", "/api/v1/library/roots", `{"revision":2,"libraries":[{"key":"movies","roots":["/etc"]}]}`, &f.session, true); out.Code != 400 {
		t.Fatalf("unsafe roots accepted: %d %s", out.Code, out.Body)
	}
	if out = request(f.handler, "PUT", "/api/v1/library/roots", body, &f.session, true); out.Code != 409 {
		t.Fatalf("stale roots revision accepted: %d %s", out.Code, out.Body)
	}
}
