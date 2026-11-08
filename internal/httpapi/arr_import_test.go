package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func radarrBackupZip(t *testing.T) []byte {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "radarr.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE Movies(Id INTEGER PRIMARY KEY, TmdbId INTEGER, Title TEXT, Year INTEGER, Monitored INTEGER);
		INSERT INTO Movies(Id,TmdbId,Title,Year,Monitored) VALUES(1,11,'Canonical provider title',2020,1)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	body, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	w, err := zw.Create("radarr.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write(body); err != nil {
		t.Fatal(err)
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func rawRequest(handler http.Handler, path string, body []byte, contentType, token, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, bytes.NewReader(body))
	r.Header.Set("Content-Type", contentType)
	r.Header.Set("X-CSRF-Token", csrf)
	r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestArrBackupPreviewAndApply(t *testing.T) {
	provider := &testProvider{}
	handler, db, accounts, code := rawFixture(t, provider)
	session, err := accounts.Setup(context.Background(), "testadmin", "test-only-password", code)
	if err != nil {
		t.Fatal(err)
	}
	if out := request(handler, "PUT", "/api/v1/providers/tmdb", `{"token":"test-only-valid-provider-token"}`, &session, true); out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	backup := radarrBackupZip(t)
	out := rawRequest(handler, "/api/v1/library/import/backup/preview", backup, "application/zip", session.Token, session.CSRFToken)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	var preview struct {
		Source string `json:"source"`
		Items  []struct {
			Kind      string `json:"kind"`
			TMDbID    int64  `json:"tmdb_id"`
			Monitored bool   `json:"monitored"`
			InLibrary bool   `json:"in_library"`
		} `json:"items"`
	}
	if err = json.Unmarshal(out.Body.Bytes(), &preview); err != nil || preview.Source != "radarr" || len(preview.Items) != 1 || preview.Items[0].TMDbID != 11 || preview.Items[0].InLibrary {
		t.Fatal(out.Body.String(), err)
	}
	out = request(handler, "POST", "/api/v1/library/import/backup/apply", `{"items":[{"kind":"movie","tmdb_id":11,"monitored":true}]}`, &session, true)
	if out.Code != 200 || !bytes.Contains(out.Body.Bytes(), []byte(`"added":1`)) {
		t.Fatal(out.Code, out.Body.String())
	}
	var monitored int
	if err = db.QueryRow(`SELECT monitored FROM media_items WHERE kind='movie' AND tmdb_id=11`).Scan(&monitored); err != nil || monitored != 1 {
		t.Fatal("the backup movie was not added", monitored, err)
	}
	out = request(handler, "POST", "/api/v1/library/import/backup/apply", `{"items":[{"kind":"movie","tmdb_id":11,"monitored":true}]}`, &session, true)
	if out.Code != 200 || !bytes.Contains(out.Body.Bytes(), []byte(`"skipped":1`)) {
		t.Fatal(out.Code, out.Body.String())
	}
}

func sonarrBackupZip(t *testing.T) []byte {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sonarr.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE Series(Id INTEGER PRIMARY KEY, TvdbId INTEGER, Title TEXT, Year INTEGER, Monitored INTEGER);
		INSERT INTO Series(Id,TvdbId,Title,Year,Monitored) VALUES(1,100,'Fixture show',2020,1)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	body, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	w, err := zw.Create("sonarr.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write(body); err != nil {
		t.Fatal(err)
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestArrSonarrBackupPreviewAndApply(t *testing.T) {
	handler, db, accounts, code := rawFixture(t, &tvProvider{})
	session, err := accounts.Setup(context.Background(), "testadmin", "test-only-password", code)
	if err != nil {
		t.Fatal(err)
	}
	if out := request(handler, "PUT", "/api/v1/providers/tmdb", `{"token":"test-only-valid-provider-token"}`, &session, true); out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	backup := sonarrBackupZip(t)
	out := rawRequest(handler, "/api/v1/library/import/backup/preview", backup, "application/zip", session.Token, session.CSRFToken)
	if out.Code != 200 || !bytes.Contains(out.Body.Bytes(), []byte(`"tmdb_id":42`)) {
		t.Fatal(out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/import/backup/apply", `{"items":[{"kind":"series","tvdb_id":100,"monitored":true}]}`, &session, true)
	if out.Code != 200 || !bytes.Contains(out.Body.Bytes(), []byte(`"added":1`)) {
		t.Fatal(out.Code, out.Body.String())
	}
	var id int64
	if err = db.QueryRow(`SELECT id FROM media_items WHERE kind='series' AND tmdb_id=42`).Scan(&id); err != nil {
		t.Fatal("the Sonarr show was not added", err)
	}
}

func TestArrBackupRejectsInvalidZip(t *testing.T) {
	handler, _, accounts, code := rawFixture(t, &testProvider{})
	session, err := accounts.Setup(context.Background(), "testadmin", "test-only-password", code)
	if err != nil {
		t.Fatal(err)
	}
	out := rawRequest(handler, "/api/v1/library/import/backup/preview", []byte("not a zip"), "application/zip", session.Token, session.CSRFToken)
	if out.Code != 400 {
		t.Fatal(out.Code, out.Body.String())
	}
}
