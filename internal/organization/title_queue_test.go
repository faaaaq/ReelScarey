package organization

import (
	"context"
	"os"
	"testing"
)

// The durable title queue reports how much work waits, a pause stops the worker
// from claiming new files, and discarding drops the pending work without
// touching any media.
func TestTitleQueuePauseBlocksClaimingAndDiscardEmptiesIt(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/One/one.mkv", []byte("one"))
	f.movie(t, 2, "Movies/Two/two.mkv", []byte("two"))

	q, err := f.s.TitleQueue(ctx)
	if err != nil || q.Pending != 2 || q.Paused {
		t.Fatalf("initial queue: %+v err=%v", q, err)
	}

	// The inspectors the worker needs are present, so only the pause flag can
	// keep it from claiming a file.
	f.s.SetProbe(func(context.Context, *os.File) (string, error) { return `{"embedded_title":""}`, nil })
	f.s.SetTagger(func(context.Context, *os.File, *os.File, string, string) (string, error) {
		t.Fatal("a paused queue claimed a file")
		return "", nil
	})

	if q, err = f.s.SetTitleQueuePaused(ctx, true); err != nil || !q.Paused || q.Pending != 2 {
		t.Fatalf("pause: %+v err=%v", q, err)
	}
	if err = f.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	var active int
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM organization_plans WHERE state IN ('queued','running')`).Scan(&active); err != nil || active != 0 {
		t.Fatalf("a paused queue created work: %d err=%v", active, err)
	}

	if q, err = f.s.DiscardPendingTitles(ctx); err != nil || q.Pending != 0 {
		t.Fatalf("discard: %+v err=%v", q, err)
	}
	var events int
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='title' AND state LIKE '%discarded%'`).Scan(&events); err != nil || events != 1 {
		t.Fatalf("discard activity: %d err=%v", events, err)
	}

	// Resuming lets the worker claim and finish the queued file in one tick.
	if q, err = f.s.SetTitleQueuePaused(ctx, false); err != nil || q.Paused {
		t.Fatalf("resume: %+v err=%v", q, err)
	}
	if _, err = f.db.Exec(`INSERT INTO video_title_updates(media_file_id,next_attempt) VALUES(1,0)`); err != nil {
		t.Fatal(err)
	}
	f.s.SetInPlaceTagger(func(ctx context.Context, path, extension, beforeProbe, title string, beforeSize int64) (string, error) {
		if e := os.WriteFile(path, []byte("edited-in-place"), 0600); e != nil {
			return "", e
		}
		return `{"embedded_title":"Review movie"}`, nil
	})
	if err = f.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if q, err = f.s.TitleQueue(ctx); err != nil || q.Pending != 0 {
		t.Fatalf("queue after resume: %+v err=%v", q, err)
	}
	var completed int
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM organization_plans WHERE state='completed'`).Scan(&completed); err != nil || completed != 1 {
		t.Fatalf("completed title update: %d err=%v", completed, err)
	}
}
