package music

import (
	"context"
	"database/sql"
	"testing"
)

func seedScanRow(t *testing.T, db *sql.DB, state string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(`INSERT INTO music_scans(root,state,phase,message,progress_total,progress_done,created_at,updated_at)
		VALUES('Music',?,'scanning','',0,0,unixepoch(),unixepoch()) RETURNING id`, state).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// A cancel that was durably persisted and acknowledged must never be silently
// reverted by the scan run's completion path (CS-8): the transition into
// matching and the final state write both refuse a cancelled scan.
func TestScanCancelSurvivesRunCompletion(t *testing.T) {
	ctx := context.Background()
	s, db, _ := editFixture(t)
	id := seedScanRow(t, db, "scanning")
	if err := s.CancelScan(ctx, id); err != nil {
		t.Fatal(err)
	}
	// The run's completion path executes after the cancel landed: with no
	// roots left to walk it proceeds to write its final state.
	s.runScan(ctx, id, nil, false)
	var state, phase string
	if err := db.QueryRow(`SELECT state,phase FROM music_scans WHERE id=?`, id).Scan(&state, &phase); err != nil {
		t.Fatal(err)
	}
	if state != "cancelled" || phase != "cancelled" {
		t.Fatalf("persisted cancel was reverted: state=%q phase=%q", state, phase)
	}
}

// The same guard must not block a healthy run: a scan that was not cancelled
// still completes normally.
func TestScanCompletionStillAppliesWithoutCancel(t *testing.T) {
	ctx := context.Background()
	s, db, _ := editFixture(t)
	id := seedScanRow(t, db, "scanning")
	s.runScan(ctx, id, nil, false)
	var state string
	if err := db.QueryRow(`SELECT state FROM music_scans WHERE id=?`, id).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state == "scanning" {
		t.Fatalf("completion write never applied: %q", state)
	}
}
