package httpapi

import (
	"context"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/music"
)

// upgradeRouteMusic adds only the optional source-upgrade capability to the
// existing full MusicService fake. This keeps the route tests independent of
// provider downloads and verifies the exact HTTP contract.
type upgradeRouteMusic struct {
	*fakeMusic
	input    music.UpgradeJobInput
	job      music.UpgradeJob
	page     music.UpgradeJobPage
	action   string
	actionID int64
}

func (f *upgradeRouteMusic) EnqueueUpgradeJob(_ context.Context, input music.UpgradeJobInput) (music.UpgradeJob, error) {
	if f.err != nil {
		return music.UpgradeJob{}, f.err
	}
	f.input = input
	f.job.ID = 9
	f.job.State = "queued"
	return f.job, nil
}

func (f *upgradeRouteMusic) ListUpgradeJobs(context.Context, int) (music.UpgradeJobPage, error) {
	return f.page, f.err
}

func (f *upgradeRouteMusic) CancelUpgradeJob(_ context.Context, id int64) (music.UpgradeJob, error) {
	if f.err != nil {
		return music.UpgradeJob{}, f.err
	}
	f.action, f.actionID = "cancel", id
	f.job.ID, f.job.State = id, "cancelled"
	return f.job, nil
}

func (f *upgradeRouteMusic) RetryUpgradeJob(_ context.Context, id int64) (music.UpgradeJob, error) {
	if f.err != nil {
		return music.UpgradeJob{}, f.err
	}
	f.action, f.actionID = "retry", id
	f.job.ID, f.job.State = id, "queued"
	return f.job, nil
}

func TestMusicUpgradeJobRoutes(t *testing.T) {
	service := &upgradeRouteMusic{
		fakeMusic: &fakeMusic{},
		page: music.UpgradeJobPage{
			Items: []music.UpgradeJob{{ID: 9, Scope: "album", ScopeID: 4, ScopeName: "Album", TargetQuality: "lossless", State: "running", TotalCount: 8, ProcessedCount: 3}},
			Page:  1,
			Total: 1,
			Limit: 100,
		},
	}
	handler, session := musicFixture(t, service)
	if out := request(handler, "GET", "/api/v1/library/music/upgrade-jobs", "", nil, false); out.Code != 401 {
		t.Fatal("unauthenticated queue read", out.Code)
	}
	out := request(handler, "GET", "/api/v1/library/music/upgrade-jobs?page=1", "", session, false)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"scope_name":"Album"`) || !strings.Contains(out.Body.String(), `"processed_count":3`) {
		t.Fatal("queue list", out.Code, out.Body.String())
	}
	if out := request(handler, "GET", "/api/v1/library/music/upgrade-jobs?page=0", "", session, false); out.Code != 400 {
		t.Fatal("invalid queue page", out.Code)
	}
	if out := request(handler, "PUT", "/api/v1/library/music/upgrade-jobs", "{}", session, true); out.Code != 405 {
		t.Fatal("queue method", out.Code)
	}
	if out := request(handler, "POST", "/api/v1/library/music/upgrade-jobs", `{"scope":"track","scope_id":7,"target_quality":"hires","max_bitrate_kbps":0}`, session, false); out.Code != 403 {
		t.Fatal("enqueue without CSRF", out.Code)
	}
	out = request(handler, "POST", "/api/v1/library/music/upgrade-jobs", `{"scope":"track","scope_id":7,"target_quality":"hires","max_bitrate_kbps":0}`, session, true)
	if out.Code != 202 || service.input.Scope != "track" || service.input.ScopeID != 7 || service.input.TargetQuality != "hires" || !strings.Contains(out.Body.String(), `"id":9`) {
		t.Fatal("enqueue", out.Code, out.Body.String(), service.input)
	}
	out = request(handler, "POST", "/api/v1/library/music/upgrade-jobs/9/cancel", `{}`, session, true)
	if out.Code != 200 || service.action != "cancel" || service.actionID != 9 || !strings.Contains(out.Body.String(), `"state":"cancelled"`) {
		t.Fatal("cancel", out.Code, out.Body.String(), service.action, service.actionID)
	}
	out = request(handler, "POST", "/api/v1/library/music/upgrade-jobs/9/retry", `{}`, session, true)
	if out.Code != 200 || service.action != "retry" || service.actionID != 9 || !strings.Contains(out.Body.String(), `"state":"queued"`) {
		t.Fatal("retry", out.Code, out.Body.String(), service.action, service.actionID)
	}
	if out := request(handler, "POST", "/api/v1/library/music/upgrade-jobs/9/pause", `{}`, session, true); out.Code != 404 {
		t.Fatal("unknown action", out.Code)
	}
}

func TestMusicUpgradeJobRouteErrors(t *testing.T) {
	service := &upgradeRouteMusic{fakeMusic: &fakeMusic{err: &music.Error{Code: "music_upgrade_state", Message: "The upgrade state changed."}}}
	handler, session := musicFixture(t, service)
	out := request(handler, "POST", "/api/v1/library/music/upgrade-jobs/9/cancel", `{}`, session, true)
	if out.Code != 409 || !strings.Contains(out.Body.String(), `"music_upgrade_state"`) {
		t.Fatal("state error mapping", out.Code, out.Body.String())
	}

	plainHandler, plainSession := musicFixture(t, &fakeMusic{})
	out = request(plainHandler, "GET", "/api/v1/library/music/upgrade-jobs", "", plainSession, false)
	if out.Code != 503 || !strings.Contains(out.Body.String(), `"music_unavailable"`) {
		t.Fatal("missing optional capability", out.Code, out.Body.String())
	}
}
