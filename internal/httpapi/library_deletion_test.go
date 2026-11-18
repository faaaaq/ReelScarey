package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/organization"
)

func TestLibraryDeletionAPIPermanentlyDeletesAfterExplicitReview(t *testing.T) {
	f := newOrganizationReviewFixture(t)
	f.movie(t, 1, "Permanent fixture", "Movies/Fixture/file.mkv")
	for endpoint, body := range map[string]string{
		"/api/v1/library/deletion":     `{"selection":"video_files","ids":[1],"mode":"trash"}`,
		"/api/v1/organization/removal": `{"media_id":1,"mode":"trash"}`,
	} {
		if out := request(f.handler, "POST", endpoint, body, nil, false); out.Code != 401 {
			t.Fatal("unauthenticated deletion", out.Code)
		}
		if out := request(f.handler, "POST", endpoint, body, &f.session, false); out.Code != 403 {
			t.Fatal("CSRF bypass", out.Code)
		}
		out := request(f.handler, "POST", endpoint, body, &f.session, true)
		if out.Code != 409 || !strings.Contains(out.Body.String(), "reload_required") {
			t.Fatalf("stale UI was accepted: %d %s", out.Code, out.Body)
		}
	}
	var count int
	if err := f.db.QueryRow(`SELECT count(*) FROM organization_plans`).Scan(&count); err != nil || count != 0 {
		t.Fatal("stale UI created a review", count, err)
	}
	out := request(f.handler, "POST", "/api/v1/library/deletion", `{"selection":"video_files","ids":[1],"mode":"delete"}`, &f.session, true)
	var plan organization.Plan
	if out.Code != 201 || json.Unmarshal(out.Body.Bytes(), &plan) != nil || plan.Removal == nil || plan.Removal.Mode != "delete" {
		t.Fatalf("review: %d %s", out.Code, out.Body)
	}
	name := filepath.Join(f.cfg.MediaDir, "Movies/Fixture/file.mkv")
	if _, err := os.Stat(name); err != nil {
		t.Fatal("review deleted the file", err)
	}
	out = request(f.handler, "POST", "/api/v1/organization/plans/"+plan.ID+"/apply", `{}`, &f.session, true)
	if out.Code != 202 {
		t.Fatal("apply", out.Code, out.Body)
	}
	if err := f.s.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatal("file not permanently deleted", err)
	}
	if _, err := os.Stat(filepath.Join(f.cfg.MediaDir, ".reelscarey-trash")); !os.IsNotExist(err) {
		t.Fatal("trash retained", err)
	}
}

func TestUnconfirmedLegacyTrashReviewRequiresReload(t *testing.T) {
	f := newOrganizationReviewFixture(t)
	f.movie(t, 1, "Legacy fixture", "Movies/Legacy/file.mkv")
	p, err := f.s.PreviewRemoval(context.Background(), 1, "trash")
	if err != nil {
		t.Fatal(err)
	}
	out := request(f.handler, "POST", "/api/v1/organization/plans/"+p.ID+"/apply", `{}`, &f.session, true)
	if out.Code != 409 || !strings.Contains(out.Body.String(), "reload_required") {
		t.Fatalf("old trash review accepted: %d %s", out.Code, out.Body)
	}
	if _, err := os.Stat(filepath.Join(f.cfg.MediaDir, "Movies/Legacy/file.mkv")); err != nil {
		t.Fatal("unconfirmed file changed", err)
	}
}
