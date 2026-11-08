package httpapi

import (
	"strings"
	"testing"
)

func TestAnimeDubPreferencesAndBulkRoutes(t *testing.T) {
	handler, _ := fixture(t)
	out := request(handler, "GET", "/api/v1/library/anime/preferences", "", nil, false)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"default_rule"`) {
		t.Fatal("preferences", out.Code, out.Body.String())
	}
	out = request(handler, "PUT", "/api/v1/library/anime/preferences", `{"dub_rule":"/[bad/","revision":1}`, nil, false)
	if out.Code != 400 {
		t.Fatal("invalid rule", out.Code, out.Body.String())
	}
	out = request(handler, "PUT", "/api/v1/library/anime/preferences", `{"dub_rule":"Custom Dub","revision":1}`, nil, false)
	if out.Code != 200 {
		t.Fatal("save rule", out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/anime/dub-test", `{"title":"Show Custom Dub 01"}`, nil, false)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"matches":true`) {
		t.Fatal("test rule", out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/anime/english-dub", `{"enabled":true}`, nil, false)
	if out.Code != 400 {
		t.Fatal("bulk without review", out.Code)
	}
	out = request(handler, "POST", "/api/v1/library/anime/english-dub", `{"enabled":true,"reviewed":true}`, nil, false)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"shows":0`) {
		t.Fatal("bulk enable", out.Code, out.Body.String())
	}
}
