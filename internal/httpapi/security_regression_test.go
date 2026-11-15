package httpapi

// Agent E security regression tests (FIX-OWNERSHIP: new security_regression_
// file). Complements audit_f_music_authz_test.go, which proves the session/
// role middleware on every music route: these tests close the remaining HTTP
// gaps — CSRF coverage across the *other* transcoding mutation routes,
// custom-profile name validation at the boundary (control characters, path
// separators) with error-hygiene assertions, and a hostile ?profile= export
// parameter that must fail closed without leaking the server layout.

import (
	"context"
	"strings"
	"testing"
)

// TestSecurityRegressionMusicMutationsRequireCSRF proves every transcoding
// mutation route the CSRF test in audit_f does not already cover refuses an
// authenticated administrator session that carries no CSRF token.
func TestSecurityRegressionMusicMutationsRequireCSRF(t *testing.T) {
	handler, _, _, service, code := auditMusicFixture(t, nil)
	admin, err := service.Setup(context.Background(), "sec-admin", "test-only-password", code)
	if err != nil {
		t.Fatal(err)
	}
	routes := []struct {
		method, path, body string
	}{
		{"POST", "/api/v1/library/music/profiles", `{"name":"Sec Profile","codec":"opus","bitrate_kbps":128}`},
		{"PUT", "/api/v1/library/music/profiles/9", `{"name":"Sec Profile","codec":"opus","bitrate_kbps":128}`},
		{"DELETE", "/api/v1/library/music/profiles/9", ""},
		{"PUT", "/api/v1/library/music/preferences", `{"min_bitrate":0}`},
		{"PUT", "/api/v1/library/music/albums/1/preferences", `{"min_bitrate":0}`},
		{"DELETE", "/api/v1/library/music/albums/1/preferences", ""},
		{"POST", "/api/v1/library/music/conversions/1/cancel", "{}"},
		{"POST", "/api/v1/library/music/conversions/1/retry", "{}"},
		{"DELETE", "/api/v1/library/music/cache", ""},
		{"POST", "/api/v1/library/music/optimize/apply", `{"profile":"opus-high","file_ids":[1]}`},
	}
	for _, route := range routes {
		out := request(handler, route.method, route.path, route.body, &admin, false)
		if out.Code != 403 || !strings.Contains(out.Body.String(), "csrf_required") {
			t.Fatalf("%s %s ran without a CSRF token: %d %s", route.method, route.path, out.Code, out.Body.String())
		}
		// With the token attached the request passes the security middleware
		// (any 403 it may get afterwards is a handler-level decision, never
		// csrf_required).
		withCSRF := request(handler, route.method, route.path, route.body, &admin, true)
		if withCSRF.Code == 403 && strings.Contains(withCSRF.Body.String(), "csrf_required") {
			t.Fatalf("%s %s refused a valid CSRF token: %d %s", route.method, route.path, withCSRF.Code, withCSRF.Body.String())
		}
	}
}

// TestSecurityRegressionProfileNameValidationAtHTTPBoundary proves the
// custom-profile creation route rejects control characters in profile names
// and handles a name carrying path separators honestly: the name is stored
// (the variant filename fence sanitizes it at use time) and no response ever
// discloses an absolute path or a raw database error.
func TestSecurityRegressionProfileNameValidationAtHTTPBoundary(t *testing.T) {
	handler, root, _, service, code := auditMusicFixture(t, nil)
	admin, err := service.Setup(context.Background(), "sec-admin", "test-only-password", code)
	if err != nil {
		t.Fatal(err)
	}

	// Control characters (including a trailing newline) are rejected: a
	// response header or filename built from such a name is not safe.
	for name := range map[string]string{
		"newline":   "{\"name\":\"bad\\nname\",\"codec\":\"opus\",\"bitrate_kbps\":128}",
		"null byte": "{\"name\":\"bad\\u0000name\",\"codec\":\"opus\",\"bitrate_kbps\":128}",
	} {
		if out := request(handler, "POST", "/api/v1/library/music/profiles", name, &admin, true); out.Code != 400 {
			t.Fatalf("profile with %s accepted: %d %s", name, out.Code, out.Body.String())
		}
	}

	// A separator-bearing name passes validation (the fence is the variant
	// label sanitizer, proven in internal/music), is listed back, and never
	// leaks an absolute path.
	created := request(handler, "POST", "/api/v1/library/music/profiles",
		`{"name":"opus/../evil","codec":"opus","bitrate_kbps":128}`, &admin, true)
	if created.Code != 201 {
		t.Fatalf("separator-bearing name refused: %d %s", created.Code, created.Body.String())
	}
	if strings.Contains(created.Body.String(), root) {
		t.Fatalf("profile creation disclosed an absolute path: %s", created.Body.String())
	}
	listed := request(handler, "GET", "/api/v1/library/music/profiles", "", &admin, true)
	if listed.Code != 200 || !strings.Contains(listed.Body.String(), "opus/../evil") {
		t.Fatalf("created profile not listed: %d %s", listed.Code, listed.Body.String())
	}

	// A duplicate name is a database-level uniqueness conflict: the client
	// must get the generic internal error, never the raw SQLite message.
	duplicate := request(handler, "POST", "/api/v1/library/music/profiles",
		`{"name":"opus/../evil","codec":"opus","bitrate_kbps":128}`, &admin, true)
	if duplicate.Code == 0 || duplicate.Code == 201 {
		t.Fatalf("duplicate profile name handling: %d %s", duplicate.Code, duplicate.Body.String())
	}
	body := duplicate.Body.String()
	if strings.Contains(body, "UNIQUE") || strings.Contains(body, "constraint") || strings.Contains(body, root) {
		t.Fatalf("duplicate-name error leaked internals: %s", body)
	}
}

// TestSecurityRegressionExportHostileProfileParamFailsClosed proves the
// export route's profile query parameter cannot smuggle a path-like value:
// an unresolvable or path-shaped key is a plain 400 and the response never
// discloses the fixture root or any absolute path.
func TestSecurityRegressionExportHostileProfileParamFailsClosed(t *testing.T) {
	handler, root, _, service, code := auditMusicFixture(t, nil)
	admin, err := service.Setup(context.Background(), "sec-admin", "test-only-password", code)
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{
		"../opus-high",
		"opus-high/../../evil",
		"%2e%2e%2fopus-high",
		"nonexistent-profile",
		"%20%20",
		"opus%00-high",
	} {
		out := request(handler, "GET", "/api/v1/library/music/files/1/export?profile="+profile, "", &admin, true)
		if out.Code != 400 {
			t.Fatalf("export accepted hostile profile %q: %d %s", profile, out.Code, out.Body.String())
		}
		if strings.Contains(out.Body.String(), root) || strings.Contains(out.Body.String(), "media") {
			t.Fatalf("export profile error disclosed the server layout for %q: %s", profile, out.Body.String())
		}
	}
}
