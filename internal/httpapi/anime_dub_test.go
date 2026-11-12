package httpapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/quality"
)

func TestAnimeDubPreferencesAndBulkRoutes(t *testing.T) {
	handler, _ := fixture(t)
	out := request(handler, "GET", "/api/v1/library/anime/preferences", "", nil, false)
	var current struct {
		Revision int `json:"revision"`
	}
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &current) != nil || !strings.Contains(out.Body.String(), `"default_rule"`) {
		t.Fatal("preferences", out.Code, out.Body.String())
	}
	// The rule is saved against the revision the page just read, so a
	// migration that bumps the seeded row cannot wedge the editor.
	out = request(handler, "PUT", "/api/v1/library/anime/preferences", fmt.Sprintf(`{"dub_rule":"/[bad/","revision":%d}`, current.Revision), nil, false)
	if out.Code != 400 {
		t.Fatal("invalid rule", out.Code, out.Body.String())
	}
	out = request(handler, "PUT", "/api/v1/library/anime/preferences", fmt.Sprintf(`{"dub_rule":"Custom Dub","revision":%d}`, current.Revision), nil, false)
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

func TestAnimeDubTestRouteDefaultsAndExclusions(t *testing.T) {
	handler, _ := fixture(t)
	// The preferences response reports the exclusion-aware default rule.
	out := request(handler, "GET", "/api/v1/library/anime/preferences", "", nil, false)
	var prefs struct {
		DefaultRule string `json:"default_rule"`
	}
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &prefs) != nil || prefs.DefaultRule != quality.DefaultDubRule {
		t.Fatal("preferences default rule", out.Code, out.Body.String())
	}
	// Without an ad-hoc rule the saved default rule decides the release.
	out = request(handler, "POST", "/api/v1/library/anime/dub-test", `{"title":"Show Y S01E01 - Dual Audio"}`, nil, false)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"matches":true`) {
		t.Fatal("default rule lost the Dual Audio match", out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/anime/dub-test", `{"title":"Show Y S01E01 - Dual Audio - French Japanese"}`, nil, false)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"matches":false`) {
		t.Fatal("default rule did not exclude the French release", out.Code, out.Body.String())
	}
	// An ad-hoc rule with its own exclusion line rejects that language while
	// keeping every other positive match.
	out = request(handler, "POST", "/api/v1/library/anime/dub-test", `{"rule":"Dual Audio\n!french","title":"Show S01E01 Dual Audio French"}`, nil, false)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"matches":false`) {
		t.Fatal("ad-hoc exclusion did not reject the release", out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/anime/dub-test", `{"rule":"Dual Audio\n!french","title":"Show S01E01 Dual Audio"}`, nil, false)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"matches":true`) {
		t.Fatal("ad-hoc exclusion lost the positive match", out.Code, out.Body.String())
	}
}
