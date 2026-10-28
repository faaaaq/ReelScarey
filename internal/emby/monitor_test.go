package emby

import (
	"context"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPlaybackMonitoringRecordsObservedSessionsAndGaps(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, e := database.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	secrets, e := securestore.New(ctx, db, dir)
	if e != nil {
		t.Fatal(e)
	}
	svc := New(secrets)
	if e = svc.Save(ctx, Config{URL: "http://emby.invalid", APIKey: "fixture"}); e != nil {
		t.Fatal(e)
	}
	m := NewMonitor(db, svc)
	status, e := m.Status(ctx)
	if e != nil || status.Enabled {
		t.Fatal(status, e)
	}
	status, e = m.Set(ctx, true, status.Revision)
	if e != nil {
		t.Fatal(e)
	}
	body := `[{"Id":"session1","UserName":"Alex","NowPlayingItem":{"Id":"movie1","Name":"Fixture","Type":"Movie"},"PlayState":{"PositionTicks":10}}]`
	code := 200
	svc.client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	if e = m.Poll(ctx); e != nil {
		t.Fatal(e)
	}
	history, e := m.History(ctx)
	if e != nil || len(history) != 1 || history[0].State != "active" {
		t.Fatal(history, e)
	}
	body = `null`
	if e = m.Poll(ctx); e == nil {
		t.Fatal("null response accepted as an empty session list")
	}
	history, e = m.History(ctx)
	if e != nil || history[0].State != "active" {
		t.Fatal("invalid response ended a session", history, e)
	}
	code = 500
	if e = m.Poll(ctx); e == nil {
		t.Fatal("failed upstream accepted")
	}
	code = 200
	body = `[]`
	if e = m.Poll(ctx); e != nil {
		t.Fatal(e)
	}
	history, e = m.History(ctx)
	if e != nil || history[0].State != "uncertain" {
		t.Fatal("gap presented as certain stop", history, e)
	}
	body = `[{"Id":"session1","UserName":"Alex","NowPlayingItem":{"Id":"movie1","Name":"Fixture","Type":"Movie"},"PlayState":{"IsPaused":true}}]`
	if e = m.Poll(ctx); e != nil {
		t.Fatal(e)
	}
	history, e = m.History(ctx)
	if e != nil || len(history) != 2 || history[0].State != "active" || !history[0].Paused {
		t.Fatal(history, e)
	}
	status, e = m.Status(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Set(ctx, false, status.Revision); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Set(ctx, true, status.Revision); e == nil {
		t.Fatal("stale revision accepted")
	}
	history, e = m.History(ctx)
	if e != nil || history[0].State != "uncertain" {
		t.Fatal(history, e)
	}
	var events int
	if e = db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='playback' AND state IN ('active','uncertain')`).Scan(&events); e != nil || events != 4 {
		t.Fatal("playback timeline missed state transitions", events, e)
	}
}
func TestPlaybackPollDoesNotApplyAfterConnectionChanges(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, e := database.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	secrets, e := securestore.New(ctx, db, dir)
	if e != nil {
		t.Fatal(e)
	}
	svc := New(secrets)
	if e = svc.Save(ctx, Config{URL: "http://old.invalid", APIKey: "old"}); e != nil {
		t.Fatal(e)
	}
	m := NewMonitor(db, svc)
	status, e := m.Status(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Set(ctx, true, status.Revision); e != nil {
		t.Fatal(e)
	}
	changed := false
	svc.client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		if !changed {
			changed = true
			if e = svc.Save(ctx, Config{URL: "http://new.invalid", APIKey: "new"}); e != nil {
				t.Fatal(e)
			}
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[{"Id":"s1","UserName":"Fixture","NowPlayingItem":{"Id":"m1","Name":"Film","Type":"Movie"}}]`)), Header: make(http.Header)}, nil
	})
	if e = m.Poll(ctx); e != nil {
		t.Fatal(e)
	}
	items, e := m.History(ctx)
	if e != nil || len(items) != 0 {
		t.Fatal("stale server response saved", items, e)
	}
	if e = m.Poll(ctx); e != nil {
		t.Fatal(e)
	}
	items, e = m.History(ctx)
	if e != nil || len(items) != 1 {
		t.Fatal(items, e)
	}
}
