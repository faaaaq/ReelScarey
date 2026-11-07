package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/faaaaq/ReelScarey/internal/auth"
	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

func sharePolicyHandler(t *testing.T) (http.Handler, *auth.Session, *auth.Session) {
	t.Helper()
	_, db, accounts, code := rawFixture(t, nil)
	ctx := context.Background()
	admin, err := accounts.Setup(ctx, "policyadmin", "fixture-policy-password", code)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = accounts.CreateRequestAccount(ctx, "policyviewer", "fixture-viewer-password"); err != nil {
		t.Fatal(err)
	}
	viewer, err := accounts.Login(ctx, "policyviewer", "fixture-viewer-password")
	if err != nil {
		t.Fatal(err)
	}
	var sequence int
	var dbName, filename string
	if err = db.QueryRow(`PRAGMA database_list`).Scan(&sequence, &dbName, &filename); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{ConfigDir: filepath.Dir(filename), MediaDir: "/media", DownloadsDir: "/downloads", PublicURL: "http://example.com"}
	secrets, err := securestore.New(ctx, db, cfg.ConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	service := transfer.New(db, secrets, cfg, transfer.NewQBittorrent(), transfer.NewTorznab())
	handler := New(db, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{"index.html": {Data: []byte("policy app")}}, "test", Dependencies{Auth: accounts, Transfer: service})
	return handler, &admin, &viewer
}

func TestTorrentSharePolicyRoutes(t *testing.T) {
	handler, admin, viewer := sharePolicyHandler(t)
	path := "/api/v1/torrents/share-policy"
	if r := request(handler, "GET", path, "", nil, false); r.Code != 401 {
		t.Fatal(r.Code)
	}
	if r := request(handler, "GET", path, "", viewer, true); r.Code != 403 || !strings.Contains(r.Body.String(), "administrator_required") {
		t.Fatal("requester reached", r.Code)
	}

	// Defaults are readable and honest.
	r := request(handler, "GET", path, "", admin, true)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var policy transfer.SharePolicy
	if err := json.Unmarshal(r.Body.Bytes(), &policy); err != nil {
		t.Fatal(err)
	}
	if policy.Enabled || policy.Action != "pause" || policy.MaxRatio != 2 || policy.MaxSeedingMinutes != 0 || policy.IncludeReelscarey || policy.Revision != 1 || policy.UpdatedAt < 1 {
		t.Fatal(policy)
	}

	// A disabled policy never contacts the client and reports zero work.
	if r = request(handler, "POST", path+"/run", "", admin, true); r.Code != 200 || !strings.Contains(r.Body.String(), `"checked":0`) || !strings.Contains(r.Body.String(), `"acted":0`) {
		t.Fatal(r.Code, r.Body.String())
	}

	valid := `{"enabled":true,"action":"pause","max_ratio":2.0,"max_seeding_minutes":0,"include_reelscarey":false,"revision":1,"updated_at":0}`
	if r = request(handler, "PUT", path, valid, admin, false); r.Code != 403 || !strings.Contains(r.Body.String(), "csrf_required") {
		t.Fatal("missing CSRF", r.Code)
	}
	r = request(handler, "PUT", path, valid, admin, true)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if err := json.Unmarshal(r.Body.Bytes(), &policy); err != nil || !policy.Enabled || policy.Revision != 2 {
		t.Fatal(policy, err)
	}
	if r = request(handler, "PUT", path, valid, admin, true); r.Code != 409 || !strings.Contains(r.Body.String(), "revision_conflict") {
		t.Fatal("stale revision", r.Code, r.Body.String())
	}

	for _, test := range []struct {
		name, body string
		status     int
		code       string
	}{
		{"no rule", `{"enabled":true,"action":"pause","max_ratio":0,"max_seeding_minutes":0,"revision":2}`, 400, "invalid_selection"},
		{"low ratio", `{"enabled":true,"action":"pause","max_ratio":0.05,"revision":2}`, 400, "invalid_selection"},
		{"high ratio", `{"enabled":true,"action":"pause","max_ratio":1000.5,"revision":2}`, 400, "invalid_selection"},
		{"negative time", `{"enabled":true,"action":"pause","max_seeding_minutes":-1,"revision":2}`, 400, "invalid_selection"},
		{"too much time", `{"enabled":true,"action":"pause","max_seeding_minutes":525601,"revision":2}`, 400, "invalid_selection"},
		{"bad action", `{"enabled":true,"action":"delete","max_ratio":1,"revision":2}`, 400, "invalid_selection"},
		{"unknown field", `{"enabled":true,"action":"pause","max_ratio":1,"revision":2,"delete_files":true}`, 400, "invalid_json"},
		{"missing revision", `{"enabled":false,"action":"pause","max_ratio":2}`, 409, "revision_conflict"},
	} {
		r = request(handler, "PUT", path, test.body, admin, true)
		if r.Code != test.status || !strings.Contains(r.Body.String(), `"code":"`+test.code+`"`) {
			t.Fatalf("%s: %d %s", test.name, r.Code, r.Body.String())
		}
	}

	// Run now reaches the client only when the policy is enabled and the
	// built-in connection exists.
	if r = request(handler, "POST", path+"/run", "", admin, true); r.Code != 409 || !strings.Contains(r.Body.String(), "builtin_unavailable") {
		t.Fatal(r.Code, r.Body.String())
	}
	if r = request(handler, "PATCH", path, valid, admin, true); r.Code != 405 {
		t.Fatal("method", r.Code)
	}
	if r = request(handler, "GET", path+"/run", "", admin, true); r.Code != 405 {
		t.Fatal("run method", r.Code)
	}
}
