package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/faaaaq/ReelScarey/internal/auth"
	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/organization"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"github.com/faaaaq/ReelScarey/internal/subtitle"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

type organizationReviewFixture struct {
	db      *sql.DB
	cfg     config.Config
	handler http.Handler
	session auth.Session
	s       *organization.Service
}

func newOrganizationReviewFixture(t *testing.T) organizationReviewFixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	cfg := config.Config{ConfigDir: filepath.Join(root, "config"), MediaDir: filepath.Join(root, "media"), DownloadsDir: filepath.Join(root, "downloads"), PublicURL: "http://example.com"}
	for _, dir := range []string{cfg.MediaDir, cfg.DownloadsDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	db, err := database.Open(ctx, cfg.ConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	accounts, err := auth.New(ctx, db, cfg.ConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	code, err := securestore.SecretFile(filepath.Join(cfg.ConfigDir, "setup-token"), 32, false)
	if err != nil {
		t.Fatal(err)
	}
	session, err := accounts.Setup(ctx, "reviewadmin", "test-only-review-password", base64.RawURLEncoding.EncodeToString(code))
	if err != nil {
		t.Fatal(err)
	}
	handler := New(db, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}}, "test", Dependencies{Auth: accounts})
	return organizationReviewFixture{db: db, cfg: cfg, handler: handler, session: session, s: organization.New(db, cfg)}
}

func (f organizationReviewFixture) movie(t *testing.T, id int, title, source string) {
	t.Helper()
	if _, err := f.db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,year,status) VALUES(?,'movie',?,?,2020,'available')`, id, id, title); err != nil {
		t.Fatal(err)
	}
	body := []byte("owned disposable review video " + title)
	dest := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(source))
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, body, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	if _, err := f.db.Exec(`INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name) VALUES(?,?,?,?,?,'{}',1,'existing','original.mkv')`, id, id, source, len(body), hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
}

func TestReviewOrganizationAPISecuritySettingsAndExactSelections(t *testing.T) {
	f := newOrganizationReviewFixture(t)
	f.movie(t, 1, "Review film", "Movies/Original/first.mkv")
	for _, endpoint := range []struct{ method, path, body string }{
		{"GET", "/api/v1/organization", ""},
		{"GET", "/api/v1/organization/files", ""},
		{"GET", "/api/v1/organization/plans", ""},
		{"GET", "/api/v1/library/items/1/organization", ""},
		{"PUT", "/api/v1/organization", `{}`},
		{"POST", "/api/v1/organization/plans", `{"file_ids":[1]}`},
		{"POST", "/api/v1/organization/removal", `{"media_id":1,"mode":"keep"}`},
		{"POST", "/api/v1/organization/acquisition", `{"kind":"download","job_id":"missing"}`},
		{"POST", "/api/v1/organization/replacements", `{"media_file_id":1,"scan_file_id":"candidate"}`},
		{"POST", "/api/v1/organization/embedded-title", `{"media_file_id":1}`},
		{"GET", "/api/v1/organization/replacements?media_file_id=1", ""},
		{"POST", "/api/v1/library/files/1/inspect", `{}`},
		{"POST", "/api/v1/organization/plans/missing/rollback", `{}`},
		{"POST", "/api/v1/organization/plans/missing/recover-replacement", `{}`},
		{"POST", "/api/v1/organization/plans/missing/apply", `{}`},
		{"POST", "/api/v1/organization/plans/missing/cancel", `{}`},
		{"PUT", "/api/v1/library/items/1/organization", `{"library_key":"anime","revision":0}`},
	} {
		if out := request(f.handler, endpoint.method, endpoint.path, endpoint.body, nil, false); out.Code != 401 {
			t.Fatal("organization endpoint unprotected", endpoint, out.Code, out.Body)
		}
		if endpoint.method != "GET" {
			if out := request(f.handler, endpoint.method, endpoint.path, endpoint.body, &f.session, false); out.Code != 403 {
				t.Fatal("organization CSRF missing", endpoint, out.Code, out.Body)
			}
		}
	}
	r := httptest.NewRequest("POST", "/api/v1/organization/plans", strings.NewReader(`{"file_ids":[1]}`))
	r.AddCookie(&http.Cookie{Name: cookieName, Value: f.session.Token})
	r.Header.Set("X-CSRF-Token", f.session.CSRFToken)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://other.invalid")
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, r)
	if rec.Code != 403 {
		t.Fatal("cross-origin organization accepted", rec.Code)
	}
	out := request(f.handler, "GET", "/api/v1/organization", "", &f.session, false)
	var settings organization.Config
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &settings) != nil || settings.MainRoot != f.cfg.MediaDir || len(settings.Libraries) != 3 {
		t.Fatal("invalid organization configuration", out.Code, out.Body)
	}
	settings.MainRoot = filepath.Join(t.TempDir(), "unconfigured-main-root")
	settings.Libraries[0].Name = "Films"
	body, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	out = request(f.handler, "PUT", "/api/v1/organization", string(body), &f.session, true)
	var changed organization.Config
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &changed) != nil || changed.MainRoot != f.cfg.MediaDir || changed.Revision != settings.Revision+1 {
		t.Fatal("server root or revision changed incorrectly", out.Code, out.Body)
	}
	if out = request(f.handler, "PUT", "/api/v1/organization", string(body), &f.session, true); out.Code != 409 {
		t.Fatal("stale organization config accepted", out.Code, out.Body)
	}
	for _, body := range []string{`null`, `{}`, `{"file_ids":[1],"all":true}`, `{"file_ids":[1,1]}`, `{"file_ids":[0]}`, `{"file_ids":[1]}{}`, `{"file_ids":[1],"target_library":"unknown"}`, `{"media_id":1}`, `{"media_id":1,"file_ids":[1],"target_library":"anime"}`} {
		if out = request(f.handler, "POST", "/api/v1/organization/plans", body, &f.session, true); out.Code != 400 {
			t.Fatal("invalid/non-explicit organization selection accepted", body, out.Code, out.Body)
		}
	}
	if out = request(f.handler, "POST", "/api/v1/organization/plans", `{"file_ids":[999]}`, &f.session, true); out.Code != 404 {
		t.Fatal("missing selected file not 404", out.Code, out.Body)
	}
	var plans int
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM organization_plans`).Scan(&plans); err != nil || plans != 0 {
		t.Fatal("invalid preview persisted a plan", plans, err)
	}
}

func TestReviewOrganizationFileFiltersAreLiteralAndBounded(t *testing.T) {
	f := newOrganizationReviewFixture(t)
	f.movie(t, 1, "100%_literal", "Movies/Original/first.mkv")
	f.movie(t, 2, "100xyzOther", "Movies/Other/second.mkv")
	query := "/api/v1/organization/files?query=" + url.QueryEscape("100%_")
	out := request(f.handler, "GET", query, "", &f.session, false)
	var files struct {
		Items []organizationFile `json:"items"`
		Total int                `json:"total"`
		Limit int                `json:"limit"`
	}
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &files) != nil || files.Total != 1 || len(files.Items) != 1 || files.Items[0].ID != 1 || files.Limit != 50 {
		t.Fatal("file title search treated wildcards as patterns", out.Code, out.Body)
	}
	for _, suffix := range []string{"?page=0", "?page=-1", "?page=100001", "?page=NaN", "?kind=music", "?kind=movie&kind=anime", "?query=a&query=b", "?all=true", "?media_id=0", "?media_id=bad", "?query=%0A", "?query=%FF", "?query=%zz"} {
		if out = request(f.handler, "GET", "/api/v1/organization/files"+suffix, "", &f.session, false); out.Code != 400 {
			t.Fatal("invalid organization file filter accepted", suffix, out.Code, out.Body)
		}
	}
	out = request(f.handler, "GET", "/api/v1/organization/files?page=2", "", &f.session, false)
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &files) != nil || files.Total != 2 || len(files.Items) != 0 {
		t.Fatal("empty page changed collection count", out.Code, out.Body)
	}
}

func TestFolderReviewAPIRequiresExplicitTitleAndShowsExtras(t *testing.T) {
	f := newOrganizationReviewFixture(t)
	f.movie(t, 1, "Review film", "Movies/Film/film.mkv")
	if err := os.WriteFile(filepath.Join(f.cfg.MediaDir, "Movies", "Film", "poster.jpg"), []byte("art"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"whole_folder":true}`, `{"whole_folder":true,"media_id":1,"file_ids":[1],"target_library":"anime"}`} {
		out := request(f.handler, "POST", "/api/v1/organization/plans", body, &f.session, true)
		if out.Code != 400 {
			t.Fatal("invalid folder selection accepted", out.Code, out.Body)
		}
	}
	out := request(f.handler, "POST", "/api/v1/organization/plans", `{"whole_folder":true,"media_id":1,"target_library":"anime"}`, &f.session, true)
	var p organization.Plan
	if out.Code != 201 || json.Unmarshal(out.Body.Bytes(), &p) != nil || p.FolderMove == nil || !strings.Contains(out.Body.String(), "poster.jpg") {
		t.Fatal("folder review omitted extras", out.Code, out.Body)
	}
	if _, err := os.Stat(filepath.Join(f.cfg.MediaDir, "Movies", "Film", "poster.jpg")); err != nil {
		t.Fatal("review moved files", err)
	}
}

func TestReviewOrganizationPreviewQueuesBeforeChangingFilesAndRetainsHistory(t *testing.T) {
	f := newOrganizationReviewFixture(t)
	source := "Movies/Original/first.mkv"
	f.movie(t, 1, "Review film", source)
	out := request(f.handler, "POST", "/api/v1/organization/plans", `{"media_id":1,"target_library":"anime"}`, &f.session, true)
	var plan organization.Plan
	if out.Code != 201 || json.Unmarshal(out.Body.Bytes(), &plan) != nil || plan.State != "review" || plan.EntireMediaID != 1 || len(plan.Files) != 1 || plan.Files[0].FileID != 1 || plan.Files[0].Source != source {
		t.Fatal("preview did not retain exact selected file", out.Code, out.Body)
	}
	old := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(source))
	before, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(plan.Files[0].Destination))); !os.IsNotExist(err) {
		t.Fatal("preview published destination", err)
	}
	apply := "/api/v1/organization/plans/" + plan.ID + "/apply"
	if out = request(f.handler, "POST", apply, `{"all":true}`, &f.session, true); out.Code != 400 {
		t.Fatal("unknown apply field accepted", out.Code, out.Body)
	}
	out = request(f.handler, "POST", apply, `{}`, &f.session, true)
	var queued organization.Plan
	if out.Code != 202 || json.Unmarshal(out.Body.Bytes(), &queued) != nil || queued.State != "queued" {
		t.Fatal("apply did not queue reviewed work", out.Code, out.Body)
	}
	after, err := os.ReadFile(old)
	if err != nil || string(after) != string(before) {
		t.Fatal("queueing changed original bytes", err)
	}
	if err = f.s.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	out = request(f.handler, "GET", "/api/v1/organization/plans/"+plan.ID, "", &f.session, false)
	var finished organization.Plan
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &finished) != nil || finished.State != "completed" {
		t.Fatal("completed plan not readable", out.Code, out.Body)
	}
	out = request(f.handler, "GET", "/api/v1/organization/plans", "", &f.session, false)
	if out.Code != 200 || !strings.Contains(out.Body.String(), plan.ID) || !strings.Contains(out.Body.String(), `"state":"completed"`) {
		t.Fatal("completed plan missing from history", out.Code, out.Body)
	}
	var downloads int
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&downloads); err != nil || downloads != 0 {
		t.Fatal("organization fabricated a download", downloads, err)
	}
}

func TestReviewDurableOrganizationReservationHoldsRestartedMediaWorkers(t *testing.T) {
	f := newOrganizationReviewFixture(t)
	f.movie(t, 1, "Review film", "Movies/Original/first.mkv")
	ctx := context.Background()
	plan, err := f.s.Preview(ctx, []int64{1}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	// Fresh service instances model a process restart: an in-memory mutex has
	// no queued owner, so the persisted batch must stop workers before claims/IO.
	subs := subtitle.New(f.db, nil, f.cfg, nil)
	if worked, err := subs.ProcessNext(ctx); worked || err == nil {
		t.Fatal("restarted subtitle worker bypassed durable organization reservation", worked, err)
	}
	workflow := transfer.New(f.db, nil, f.cfg, nil, nil)
	if worked, err := workflow.ProcessLibraryNext(ctx); worked || err == nil {
		t.Fatal("restarted discovery worker bypassed durable organization reservation", worked, err)
	}
	err = transfer.NewImporter(workflow).Execute(ctx, transfer.Job{ID: "not-a-real-download"}, "unopened-source.mkv", 1)
	var failure *organization.Error
	if !errors.As(err, &failure) || failure.Code != "organization_busy" {
		t.Fatal("restarted importer reached file IO before durable reservation", err)
	}
	var jobs, scans int
	if err = f.db.QueryRow(`SELECT (SELECT COUNT(*) FROM subtitle_jobs),(SELECT COUNT(*) FROM library_scans)`).Scan(&jobs, &scans); err != nil || jobs != 0 || scans != 0 {
		t.Fatal("held workers created competing work", jobs, scans, err)
	}
	if err = organization.New(f.db, f.cfg).Tick(ctx); err != nil {
		t.Fatal("organization recovery did not resume its durable batch", err)
	}
	if err = organization.EnsureMediaAvailable(ctx, f.db); err != nil {
		t.Fatal("completed organization did not release media workers", err)
	}
}
