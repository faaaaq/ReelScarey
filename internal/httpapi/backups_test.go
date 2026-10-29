package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/faaaaq/ReelScarey/internal/backups"
	"github.com/faaaaq/ReelScarey/internal/config"
)

func TestBackupAPIAuthorizationScheduleUploadAndDownload(t *testing.T) {
	_, db, a, code := rawFixture(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	admin, e := a.Setup(ctx, "admin", "fixture-only-password", code)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.CreateRequestAccount(ctx, "viewer", "fixture-viewer-password"); e != nil {
		t.Fatal(e)
	}
	viewer, e := a.Login(ctx, "viewer", "fixture-viewer-password")
	if e != nil {
		t.Fatal(e)
	}
	var dbPath string
	var seq int
	var dbName string
	if e = db.QueryRow(`PRAGMA database_list`).Scan(&seq, &dbName, &dbPath); e != nil {
		t.Fatal(e)
	}
	cfg := config.Config{ConfigDir: filepath.Dir(dbPath), BackupsDir: filepath.Join(t.TempDir(), "backups"), MediaDir: "/media", DownloadsDir: "/downloads", PublicURL: "http://example.com"}
	b, e := backups.New(ctx, db, cfg, "test", func() {})
	if e != nil {
		t.Fatal(e)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	done := make(chan struct{})
	go func() { defer close(done); b.Run(ctx, logger) }()
	defer func() { cancel(); <-done }()
	h := New(db, cfg, logger, fstest.MapFS{"index.html": {Data: []byte("backup-app-shell")}}, "test", Dependencies{Auth: a, Backups: b})
	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest("GET", "http://example.com/backups", nil))
	if page.Code != 200 || !strings.Contains(page.Body.String(), "backup-app-shell") {
		t.Fatal("backup deep link unavailable", page.Code)
	}
	request := func(method, path, body, token, csrf, origin, contentType string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://example.com/api/v1/system/backups"+path, strings.NewReader(body))
		if token != "" {
			r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		}
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", contentType)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, p := range []string{"", "/00000000000000000000000000000000/download"} {
		if w := request("GET", p, "", "", "", "", ""); w.Code != 401 {
			t.Fatal(w.Code, w.Body.String())
		}
		if w := request("GET", p, "", viewer.Token, "", "", ""); w.Code != 403 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := request("POST", "", "{}", admin.Token, "", "http://example.com", "application/json"); w.Code != 403 {
		t.Fatal("CSRF accepted", w.Code)
	}
	if w := request("POST", "", "{}", admin.Token, admin.CSRFToken, "http://other.example", "application/json"); w.Code != 403 {
		t.Fatal("cross origin accepted", w.Code)
	}
	if w := request("GET", "", "", admin.Token, "", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":false`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("PUT", "/schedule", `{"enabled":true,"interval_days":2,"keep_count":3,"keep_days":10,"revision":1}`, admin.Token, admin.CSRFToken, "http://example.com", "application/json"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("POST", "/upload", "bad", admin.Token, admin.CSRFToken, "http://example.com", "application/zip"); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("POST", "", "{}", admin.Token, admin.CSRFToken, "http://example.com", "application/json"); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var list []backups.Archive
	deadline := time.Now().Add(10 * time.Second)
	for {
		list, e = b.List()
		if e != nil {
			t.Fatal(e)
		}
		p, e := b.Schedule(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if len(list) == 1 && !p.Running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("backup did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	archiveID := list[0].ID
	download := request("GET", "/"+archiveID+"/download", "", admin.Token, "", "", "")
	if download.Code != 200 || download.Header().Get("Content-Type") != "application/zip" || !strings.HasPrefix(download.Body.String(), "PK") || !strings.Contains(download.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal(download.Code, download.Header())
	}
	upload := request("POST", "/upload", download.Body.String(), admin.Token, admin.CSRFToken, "http://example.com", "application/zip")
	if upload.Code != 201 {
		t.Fatal(upload.Code, upload.Body.String())
	}
	preview := request("POST", "/"+archiveID+"/preview", "{}", admin.Token, admin.CSRFToken, "http://example.com", "application/json")
	if preview.Code != 200 || !strings.Contains(preview.Body.String(), `"administrator":"admin"`) {
		t.Fatal(preview.Code, preview.Body.String())
	}
	if w := request("POST", "/"+archiveID+"/restore", `{"sha256":"wrong","confirmation":"RESTORE"}`, admin.Token, admin.CSRFToken, "http://example.com", "application/json"); w.Code != 409 || b.Restoring() {
		t.Fatal(w.Code, w.Body.String())
	}
	var review struct {
		Archive backups.Archive `json:"archive"`
	}
	if e = json.Unmarshal(preview.Body.Bytes(), &review); e != nil {
		t.Fatal(e)
	}
	if w := request("DELETE", "/"+archiveID, "", admin.Token, admin.CSRFToken, "http://example.com", ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}
