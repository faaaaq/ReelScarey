package releaseguard

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestDetectStalledUsesOnlyActiveUnchangedProgress(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	now := time.Unix(1_800_000_000, 0)
	threshold := time.Hour
	old := now.Add(-2 * time.Hour).Unix()

	rows := []struct {
		id             string
		state          string
		progress       float64
		lastProgressAt int64
		stalledAt      int64
	}{
		{id: "old-downloading", state: "downloading", progress: 0.4, lastProgressAt: old},
		{id: "resumed", state: "downloading", progress: 0.4, lastProgressAt: old, stalledAt: now.Add(-3 * time.Hour).Unix()},
		{id: "boundary", state: "downloading", progress: 0.4, lastProgressAt: now.Add(-threshold).Unix()},
		{id: "recent", state: "downloading", progress: 0.4, lastProgressAt: now.Add(-30 * time.Minute).Unix()},
		{id: "future", state: "downloading", progress: 0.4, lastProgressAt: now.Add(time.Hour).Unix()},
		{id: "already-stalled", state: "downloading", progress: 0.4, lastProgressAt: old, stalledAt: now.Add(-time.Hour).Unix()},
		{id: "no-progress-sample", state: "downloading", progress: 0.4},
		{id: "at-completion", state: "downloading", progress: 1},
		{id: "queued", state: "queued", progress: 0, lastProgressAt: old},
		{id: "submitting", state: "submitting", progress: 0, lastProgressAt: old},
		{id: "importing", state: "importing", progress: 1, lastProgressAt: old},
		{id: "blocked", state: "blocked", progress: 0.4, lastProgressAt: old},
		{id: "failed", state: "failed", progress: 0.4, lastProgressAt: old},
		{id: "completed", state: "completed", progress: 1, lastProgressAt: old},
		{id: "cancelled", state: "cancelled", progress: 0.4, lastProgressAt: old},
	}
	for _, row := range rows {
		insertDownload(t, db, row.id, row.state, row.progress, row.lastProgressAt, row.stalledAt)
	}
	if _, err := db.Exec(`UPDATE downloads SET protocol = ? WHERE id = 'old-downloading'`, ProtocolUsenet); err != nil {
		t.Fatal(err)
	}

	got, err := DetectStalled(ctx, db, now, threshold)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(got))
	for _, item := range got {
		ids = append(ids, item.ID)
	}
	want := []string{"old-downloading", "resumed", "boundary"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("DetectStalled() ids = %v, want %v", ids, want)
	}
	if got[0].Protocol != ProtocolUsenet {
		t.Fatalf("usenet protocol was not returned: %+v", got[0])
	}
	if got[0].Title != "Release old-downloading" || got[0].LastProgressAt != old {
		t.Fatalf("unexpected stalled row: %+v", got[0])
	}
}

func TestDetectStalledRejectsInvalidThreshold(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	now := time.Unix(1_800_000_000, 0)
	for _, threshold := range []time.Duration{0, -time.Second} {
		if _, err := DetectStalled(ctx, db, now, threshold); err == nil {
			t.Fatalf("threshold %s was accepted", threshold)
		}
	}
}

func TestMarkStalledOnlyUpdatesActiveDownloadingJobs(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	now := time.Unix(1_800_000_000, 0)
	old := now.Add(-2 * time.Hour).Unix()
	insertDownload(t, db, "active", "downloading", 0.4, old, 0)
	insertDownload(t, db, "completed", "completed", 1, old, 0)
	insertDownload(t, db, "cancelled", "cancelled", 0.4, old, 0)
	insertDownload(t, db, "finished-progress", "downloading", 1, old, 0)

	updated, err := MarkStalled(ctx, db, "active", now)
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Fatal("active downloading job was not marked")
	}
	if got := downloadStalledAt(t, db, "active"); got != now.Unix() {
		t.Fatalf("stalled_at = %d, want %d", got, now.Unix())
	}
	updated, err = MarkStalled(ctx, db, "active", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if updated {
		t.Fatal("an already marked job was marked again without new progress")
	}
	if got := downloadStalledAt(t, db, "active"); got != now.Unix() {
		t.Fatalf("idempotent mark changed stalled_at to %d, want %d", got, now.Unix())
	}

	for _, id := range []string{"completed", "cancelled", "finished-progress", "missing"} {
		updated, err := MarkStalled(ctx, db, id, now)
		if err != nil {
			t.Fatal(err)
		}
		if updated {
			t.Fatalf("%s was incorrectly marked stalled", id)
		}
	}
	if got := downloadStalledAt(t, db, "completed"); got != 0 {
		t.Fatalf("completed job stalled_at = %d, want 0", got)
	}
	if got := downloadStalledAt(t, db, "cancelled"); got != 0 {
		t.Fatalf("cancelled job stalled_at = %d, want 0", got)
	}

	remaining, err := DetectStalled(ctx, db, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range remaining {
		if item.ID == "active" {
			t.Fatal("an already marked job was reported again without new progress")
		}
	}
}
