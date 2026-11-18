package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/duplicates"
	"github.com/faaaaq/ReelScarey/internal/organization"
)

func TestDuplicateDismissalAPIRoundTrip(t *testing.T) {
	f := newOrganizationReviewFixture(t)
	f.movie(t, 1, "Duplicated film", "Movies/Dup/a.mkv")
	// A second registered copy of the same movie forms one review group.
	f.movie(t, 2, "Other title", "Movies/Other/solo.mkv")
	if _, err := f.db.Exec(`INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name)
		VALUES(3,1,'Movies/Dup/b.mkv',30,'copy-sha','{}',1,'existing','b.mkv')`); err != nil {
		t.Fatal(err)
	}

	for _, endpoint := range []struct{ method, path, body string }{
		{"GET", "/api/v1/duplicates", ""},
		{"POST", "/api/v1/duplicates/dismiss", `{"kind":"movie","media_id":1}`},
		{"POST", "/api/v1/duplicates/dismiss/restore", `{"kind":"movie","media_id":1}`},
	} {
		if out := request(f.handler, endpoint.method, endpoint.path, endpoint.body, nil, false); out.Code != 401 {
			t.Fatal("duplicates endpoint unprotected", endpoint, out.Code)
		}
		if endpoint.method != "GET" {
			if out := request(f.handler, endpoint.method, endpoint.path, endpoint.body, &f.session, false); out.Code != 403 {
				t.Fatal("duplicates CSRF missing", endpoint, out.Code)
			}
		}
	}
	if out := request(f.handler, "POST", "/api/v1/duplicates/dismiss", `{"kind":"episode","media_id":1}`, &f.session, true); out.Code != 400 {
		t.Fatal("episode dismissal accepted for a movie group", out.Code, out.Body)
	}
	if out := request(f.handler, "GET", "/api/v1/duplicates?include_dismissed=maybe", "", &f.session, false); out.Code != 400 {
		t.Fatal("invalid include_dismissed accepted", out.Code)
	}

	out := request(f.handler, "GET", "/api/v1/duplicates", "", &f.session, false)
	var report duplicates.Report
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &report) != nil || len(report.Groups) != 1 {
		t.Fatalf("review: %d %s", out.Code, out.Body)
	}

	if out := request(f.handler, "POST", "/api/v1/duplicates/dismiss", `{"kind":"movie","media_id":1}`, &f.session, true); out.Code != 200 {
		t.Fatal("dismiss", out.Code, out.Body)
	}
	out = request(f.handler, "GET", "/api/v1/duplicates", "", &f.session, false)
	if json.Unmarshal(out.Body.Bytes(), &report) != nil || len(report.Groups) != 0 || report.Total != 0 {
		t.Fatalf("dismissed group still listed: %s", out.Body)
	}
	out = request(f.handler, "GET", "/api/v1/duplicates?include_dismissed=1", "", &f.session, false)
	if json.Unmarshal(out.Body.Bytes(), &report) != nil || len(report.Groups) != 1 || !report.Groups[0].Dismissed {
		t.Fatalf("inclusive report lost the dismissed group: %s", out.Body)
	}
	if out := request(f.handler, "POST", "/api/v1/duplicates/dismiss/restore", `{"kind":"movie","media_id":1}`, &f.session, true); out.Code != 200 {
		t.Fatal("restore", out.Code, out.Body)
	}
	out = request(f.handler, "GET", "/api/v1/duplicates", "", &f.session, false)
	var restored duplicates.Report
	if json.Unmarshal(out.Body.Bytes(), &restored) != nil || len(restored.Groups) != 1 || restored.Groups[0].Dismissed {
		t.Fatalf("restored group still hidden: %s", out.Body)
	}
}

func TestOrganizationActiveReportsTheRunningPlan(t *testing.T) {
	f := newOrganizationReviewFixture(t)
	f.movie(t, 1, "Active film", "Movies/Active/only.mkv")

	out := request(f.handler, "GET", "/api/v1/organization/active", "", &f.session, false)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"active":false`) {
		t.Fatalf("idle active report: %d %s", out.Code, out.Body)
	}
	if _, err := f.db.Exec(`INSERT INTO organization_plans(id,state,snapshot,created_at,updated_at) VALUES('active-1','running',?,1,2)`,
		`{"id":"active-1","phase":"planned","track_cleanup":{"request":{"mode":"all_subtitles"},"targets":[{"id":1,"path":"Movies/Active/only.mkv"}],"processed":3,"changed":2,"skipped":1,"dropped":4,"issues":["Movies/Active/only.mkv: skipped"]}}`); err != nil {
		t.Fatal(err)
	}
	out = request(f.handler, "GET", "/api/v1/organization/active", "", &f.session, false)
	var payload struct {
		Active bool                    `json:"active"`
		Plan   organization.ActivePlan `json:"plan"`
	}
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &payload) != nil {
		t.Fatalf("active report: %d %s", out.Code, out.Body)
	}
	if !payload.Active || payload.Plan.Kind != "track_cleanup" || payload.Plan.Processed != 3 ||
		payload.Plan.Total != 1 || payload.Plan.Changed != 2 || payload.Plan.Skipped != 1 || payload.Plan.Dropped != 4 ||
		len(payload.Plan.Issues) != 1 {
		t.Fatalf("unexpected active plan: %+v", payload.Plan)
	}
}

func TestApplyDuringARunningPlanFailsFastInsteadOfBlocking(t *testing.T) {
	f := newOrganizationReviewFixture(t)
	f.movie(t, 1, "Busy film", "Movies/Busy/first.mkv")

	makePlan := func() organization.Plan {
		t.Helper()
		out := request(f.handler, "POST", "/api/v1/library/deletion", `{"selection":"video_files","ids":[1],"mode":"keep"}`, &f.session, true)
		var plan organization.Plan
		if out.Code != 201 || json.Unmarshal(out.Body.Bytes(), &plan) != nil {
			t.Fatalf("review: %d %s", out.Code, out.Body)
		}
		return plan
	}
	first := makePlan()
	second := makePlan()
	// Simulate a plan the worker is currently executing.
	if _, err := f.db.Exec(`UPDATE organization_plans SET state='running' WHERE id=?`, first.ID); err != nil {
		t.Fatal(err)
	}
	out := request(f.handler, "POST", "/api/v1/organization/plans/"+second.ID+"/apply", `{}`, &f.session, true)
	if out.Code != 409 || !strings.Contains(out.Body.String(), "organization_busy") {
		t.Fatalf("apply during a running plan: %d %s", out.Code, out.Body)
	}
	var state string
	if err := f.db.QueryRow(`SELECT state FROM organization_plans WHERE id=?`, second.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "review" {
		t.Fatalf("busy apply queued the plan anyway: %s", state)
	}
	// Once the other plan finishes, the same review applies normally.
	if _, err := f.db.Exec(`UPDATE organization_plans SET state='completed' WHERE id=?`, first.ID); err != nil {
		t.Fatal(err)
	}
	out = request(f.handler, "POST", "/api/v1/organization/plans/"+second.ID+"/apply", `{}`, &f.session, true)
	if out.Code != 202 {
		t.Fatalf("apply after the run finished: %d %s", out.Code, out.Body)
	}
}
