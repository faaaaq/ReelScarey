package music

import (
	"context"
	"testing"
	"time"
)

// reconcileAcquisitions must not fail silently when a state write fails
// (RS-0014): the failure is journaled as an activity event and reconciliation
// continues with the remaining stuck items.
func TestAcquisitionWriteFailureIsJournaledAndReconciliationContinues(t *testing.T) {
	ctx := context.Background()
	s, db, _ := editFixture(t)
	now := time.Now().Unix()
	stale := now - 7300
	seed := func(source string) int64 {
		var jobID int64
		if err := db.QueryRow(`INSERT INTO music_acquisition_jobs(source_url,source_kind,title,root,format,state,track_count,created_at,updated_at)
			VALUES('','album',?, 'Music','mp3','running',1,?,?) RETURNING id`, source, stale, stale).Scan(&jobID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO music_acquisition_tracks(job_id,position,title,artist,album,album_artist,year,track_number,disc_number,duration_ms,cover_url,source_url,search_query,state,created_at,updated_at)
			VALUES(?,1,'Track','Artist',?, 'Artist',2020,1,1,200000,'','','query','downloading',?,?)`, jobID, source, stale, stale); err != nil {
			t.Fatal(err)
		}
		return jobID
	}
	jobA, jobB := seed("Album A"), seed("Album B")
	// Inject failures only for the track state writes; everything else works.
	if _, err := db.Exec(`CREATE TRIGGER fail_track_state BEFORE UPDATE ON music_acquisition_tracks BEGIN SELECT RAISE(ABORT, 'injected acquisition write failure'); END;`); err != nil {
		t.Fatal(err)
	}
	s.reconcileAcquisitions(ctx)
	var events int
	if err := db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='music' AND state='acquisition state update failed'`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events == 0 {
		t.Fatal("failed acquisition state writes were not journaled")
	}
	if _, err := db.Exec(`DROP TRIGGER fail_track_state`); err != nil {
		t.Fatal(err)
	}
	// After the fault is removed, reconciliation recovers both stuck tracks
	// instead of having stopped at the first failure.
	s.reconcileAcquisitions(ctx)
	for _, jobID := range []int64{jobA, jobB} {
		var state string
		if err := db.QueryRow(`SELECT state FROM music_acquisition_tracks WHERE job_id=?`, jobID).Scan(&state); err != nil || state != "failed" {
			t.Fatalf("job %d track not reconciled: %q %v", jobID, state, err)
		}
		var jobState string
		if err := db.QueryRow(`SELECT state FROM music_acquisition_jobs WHERE id=?`, jobID).Scan(&jobState); err != nil || jobState != "failed" {
			t.Fatalf("job %d not reconciled: %q %v", jobID, jobState, err)
		}
	}
	var withCause int
	if err := db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='music' AND title LIKE '%could not be marked failed%'`).Scan(&withCause); err != nil {
		t.Fatal(err)
	}
	if withCause == 0 {
		t.Fatal("journal events lost their cause detail")
	}
}
