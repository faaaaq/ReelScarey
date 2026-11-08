package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/transfer"
)

func TestTorrentPublicPauseRoutes(t *testing.T) {
	handler, admin, viewer := sharePolicyHandler(t)
	path := "/api/v1/torrents/public-pause"
	if r := request(handler, "GET", path, "", nil, false); r.Code != 401 {
		t.Fatal(r.Code)
	}
	if r := request(handler, "GET", path, "", viewer, true); r.Code != 403 || !strings.Contains(r.Body.String(), "administrator_required") {
		t.Fatal("requester reached", r.Code)
	}

	// The option starts off.
	r := request(handler, "GET", path, "", admin, true)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var option transfer.PublicPause
	if err := json.Unmarshal(r.Body.Bytes(), &option); err != nil {
		t.Fatal(err)
	}
	if option.Enabled || option.Revision != 1 || option.UpdatedAt < 1 {
		t.Fatal(option)
	}

	valid := `{"enabled":true,"revision":1,"updated_at":0}`
	if r = request(handler, "PUT", path, valid, admin, false); r.Code != 403 || !strings.Contains(r.Body.String(), "csrf_required") {
		t.Fatal("missing CSRF", r.Code)
	}
	r = request(handler, "PUT", path, valid, admin, true)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if err := json.Unmarshal(r.Body.Bytes(), &option); err != nil || !option.Enabled || option.Revision != 2 {
		t.Fatal(option, err)
	}
	if r = request(handler, "PUT", path, valid, admin, true); r.Code != 409 || !strings.Contains(r.Body.String(), "revision_conflict") {
		t.Fatal("stale revision", r.Code, r.Body.String())
	}
	if r = request(handler, "PATCH", path, valid, admin, true); r.Code != 405 {
		t.Fatal("method", r.Code)
	}
	if r = request(handler, "POST", path, valid, admin, true); r.Code != 405 {
		t.Fatal("method", r.Code)
	}
}
