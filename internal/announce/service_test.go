package announce

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSourceAutoGrabOptInPersists(t *testing.T) {
	s, done := announceFixture(t)
	defer done()
	ctx := context.Background()
	source, err := s.SaveSource(ctx, "", SourceUpdate{Tracker: "iptorrents", Name: "IPT", Nick: "tester", Enabled: true, Key: "private-passkey"})
	if err != nil {
		t.Fatal(err)
	}
	if source.AutoGrab {
		t.Fatal("new source opted into automatic grabbing")
	}
	yes, no := true, false
	source, err = s.SaveSource(ctx, source.ID, SourceUpdate{Tracker: source.Tracker, Name: source.Name, Nick: source.Nick, Enabled: true, Revision: source.Revision, AutoGrab: &yes})
	if err != nil {
		t.Fatal(err)
	}
	if !source.AutoGrab || !source.HasKey {
		t.Fatal("saved key was not used to enable automatic grabbing", source)
	}
	// Omitted opt-in and blank credential retain the saved values, including
	// across a new service instance using the same persisted database/key.
	s = New(s.db, s.secrets, s.logger)
	source, err = s.SaveSource(ctx, source.ID, SourceUpdate{Tracker: source.Tracker, Name: source.Name, Nick: source.Nick, Revision: source.Revision, Key: "   "})
	if err != nil {
		t.Fatal(err)
	}
	if !source.AutoGrab || !source.HasKey || source.Enabled {
		t.Fatal("unrelated update cleared saved opt-in/key", source)
	}
	var autoGrab bool
	if err = s.db.QueryRowContext(ctx, `SELECT auto_grab FROM announce_sources WHERE id=?`, source.ID).Scan(&autoGrab); err != nil || !autoGrab {
		t.Fatal("opt-in was not stored in the plaintext column", autoGrab, err)
	}
	source, err = s.SaveSource(ctx, source.ID, SourceUpdate{Tracker: source.Tracker, Name: source.Name, Nick: source.Nick, Revision: source.Revision, AutoGrab: &no})
	if err != nil {
		t.Fatal(err)
	}
	if source.AutoGrab || !source.HasKey {
		t.Fatal("explicit opt-out did not preserve credential", source)
	}
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private-passkey") {
		t.Fatal("source status exposed the credential")
	}
}

func TestSourceAutoGrabValidation(t *testing.T) {
	s, done := announceFixture(t)
	defer done()
	yes := true
	for _, tc := range []struct{ tracker, key, message string }{
		{"iptorrents", "", "passkey"},
		{"iptorrents", "   ", "passkey"},
		{"torrentleech", "saved-key", "only for IPTorrents"},
	} {
		t.Run(tc.tracker+tc.key, func(t *testing.T) {
			_, err := s.SaveSource(context.Background(), "", SourceUpdate{Tracker: tc.tracker, Name: "source", Nick: "tester", Key: tc.key, AutoGrab: &yes})
			var problem *Error
			if !errors.As(err, &problem) || problem.Code != "invalid_settings" || !strings.Contains(problem.Message, tc.message) {
				t.Fatalf("expected clear validation error, got %v", err)
			}
		})
	}
}

func TestRecordAtRevisionFencesAutoGrab(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		enabled, autoGrab       bool
		revision                int
		tracker, state, outcome string
	}{
		{"capture by default", true, false, 1, "iptorrents", "done", "capture_only"},
		{"paused opt-in", false, true, 1, "iptorrents", "done", "capture_only"},
		{"current opt-in", true, true, 1, "iptorrents", "pending", ""},
		{"stale connection", true, true, 0, "iptorrents", "done", "capture_only"},
		{"different tracker", true, true, 1, "torrentleech", "done", "capture_only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, done := announceFixture(t)
			defer done()
			ctx := context.Background()
			source, err := s.SaveSource(ctx, "", SourceUpdate{Tracker: "iptorrents", Name: "IPT", Nick: "tester", Enabled: tc.enabled, Key: "private-passkey", AutoGrab: &tc.autoGrab})
			if err != nil {
				t.Fatal(err)
			}
			p := Parsed{ReleaseName: "Owned.Movie.2026.1080p", TorrentID: "123", InfoURL: "https://iptorrents.com/details.php?id=123"}
			if err = s.RecordAtRevision(ctx, source.ID, tc.revision, tc.tracker, p, "announce with private-passkey"); err != nil {
				t.Fatal(err)
			}
			var revision int
			var state, outcome string
			var lastEventAt int64
			if err = s.db.QueryRowContext(ctx, `SELECT source_revision,processing_state,outcome FROM announce_events WHERE source_id=?`, source.ID).Scan(&revision, &state, &outcome); err != nil {
				t.Fatal(err)
			}
			if revision != source.Revision || state != tc.state || outcome != tc.outcome {
				t.Fatal(revision, state, outcome)
			}
			if err = s.db.QueryRowContext(ctx, `SELECT last_event_at FROM announce_sources WHERE id=?`, source.ID).Scan(&lastEventAt); err != nil || lastEventAt == 0 {
				t.Fatal(lastEventAt, err)
			}
			page, err := s.Events(ctx, 1)
			if err != nil || len(page.Items) != 1 {
				t.Fatal(page, err)
			}
			if page.Items[0].Outcome != tc.outcome || page.Items[0].Attempts != 0 || page.Items[0].DownloadID != "" {
				t.Fatal(page)
			}
			if (page.Items[0].ProcessedAt > 0) != (tc.state == "done") {
				t.Fatal("wrong terminal timestamp", page)
			}
			data, err := json.Marshal(page)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "private-passkey") {
				t.Fatal("event API exposed raw credentials")
			}
		})
	}
}

func TestRecordCurrentRevisionAndDuplicatePreserveState(t *testing.T) {
	s, done := announceFixture(t)
	defer done()
	ctx := context.Background()
	yes := true
	source, err := s.SaveSource(ctx, "", SourceUpdate{Tracker: "iptorrents", Name: "IPT", Nick: "tester", Enabled: true, Key: "private-passkey", AutoGrab: &yes})
	if err != nil {
		t.Fatal(err)
	}
	oldRevision := source.Revision
	source, err = s.SaveSource(ctx, source.ID, SourceUpdate{Tracker: source.Tracker, Name: source.Name, Nick: source.Nick, Enabled: true, Revision: source.Revision})
	if err != nil {
		t.Fatal(err)
	}
	p := Parsed{ReleaseName: "Owned.Movie.2026"}
	if err = s.RecordAtRevision(ctx, source.ID, oldRevision, source.Tracker, p, "stale announce"); err != nil {
		t.Fatal(err)
	}
	if err = s.Record(ctx, source.ID, source.Tracker, p, "current announce"); err != nil {
		t.Fatal(err)
	}
	var state string
	var revision int
	if err = s.db.QueryRowContext(ctx, `SELECT processing_state,source_revision FROM announce_events ORDER BY id DESC LIMIT 1`).Scan(&state, &revision); err != nil || state != "pending" || revision != source.Revision {
		t.Fatal(state, revision, err)
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE announce_events SET processing_state='retry',outcome='waiting',message='Client unavailable',attempts=3,next_run=123 WHERE processing_state='pending'`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE announce_sources SET last_event_at=123 WHERE id=?`, source.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Record(ctx, source.ID, source.Tracker, p, "current announce"); err != nil {
		t.Fatal(err)
	}
	var attempts int
	var outcome, message string
	var lastEventAt int64
	if err = s.db.QueryRowContext(ctx, `SELECT processing_state,outcome,message,attempts FROM announce_events ORDER BY id DESC LIMIT 1`).Scan(&state, &outcome, &message, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "retry" || outcome != "waiting" || message != "Client unavailable" || attempts != 3 {
		t.Fatal("duplicate reset work", state, outcome, message, attempts)
	}
	if err = s.db.QueryRowContext(ctx, `SELECT last_event_at FROM announce_sources WHERE id=?`, source.ID).Scan(&lastEventAt); err != nil || lastEventAt != 123 {
		t.Fatal("duplicate changed last_event_at", lastEventAt, err)
	}
}

func TestPruneRetainsUnfinishedEvents(t *testing.T) {
	s, done := announceFixture(t)
	defer done()
	ctx := context.Background()
	source, err := s.SaveSource(ctx, "", SourceUpdate{Tracker: "iptorrents", Name: "IPT", Nick: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"pending", "retry", "processing", "done"} {
		if err = s.Record(ctx, source.ID, source.Tracker, Parsed{ReleaseName: state}, state); err != nil {
			t.Fatal(err)
		}
		if _, err = s.db.ExecContext(ctx, `UPDATE announce_events SET processing_state=?,received_at=1 WHERE release_name=?`, state, state); err != nil {
			t.Fatal(err)
		}
	}
	s.Prune(ctx)
	page, err := s.Events(ctx, 1)
	if err != nil || page.Total != 3 {
		t.Fatal(page, err)
	}
	for _, event := range page.Items {
		if event.ReleaseName == "done" {
			t.Fatal("terminal old event retained")
		}
	}
}
