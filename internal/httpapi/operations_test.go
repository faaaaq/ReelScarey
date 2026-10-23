package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInstallationChecksRequireAuthenticationCSRFAndExplicitRequest(t *testing.T) {
	raw, _, accounts, code := rawFixture(t, nil)
	for _, path := range []string{"/api/v1/setup", "/api/v1/system/checks", "/api/v1/system/recovery/resume"} {
		rec := httptest.NewRecorder()
		raw.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != 401 {
			t.Fatalf("%s: %d", path, rec.Code)
		}
	}
	h, _ := fixture(t)
	run := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/setup", "/system"} {
		if w := run("GET", path, ""); w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	w := run("GET", "/api/v1/system/checks", "")
	var before struct {
		Report any `json:"report"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &before); e != nil || before.Report != nil {
		t.Fatal("GET unexpectedly ran checks", w.Body.String())
	}
	if w = run("POST", "/api/v1/system/checks", `{"unexpected":true}`); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = run("POST", "/api/v1/system/recovery/resume", `{"reviewed":false}`); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w = run("POST", "/api/v1/system/checks", `{}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"database"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = run("GET", "/api/v1/system/checks", ""); !strings.Contains(w.Body.String(), `"checked_at"`) {
		t.Fatal("last report not cached")
	}
	session, e := accounts.Setup(context.Background(), "testadmin", "test-only-password", code)
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"/api/v1/system/checks", "/api/v1/system/recovery/resume"} {
		r := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: cookieName, Value: session.Token})
		w := httptest.NewRecorder()
		raw.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("CSRF missing", w.Code)
		}
	}
}
