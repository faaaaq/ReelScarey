package transfer

import (
	"context"
	"testing"
	"time"
)

// TestMusicResubmitSurvivesLeaseExpiryDuringAdd proves the fenced pre-submit:
// when a claimant's lease is stolen while the client Add is in flight, its
// finalize is refused and the next claimant reconciles the visible torrent
// instead of resubmitting it.
func TestMusicResubmitSurvivesLeaseExpiryDuringAdd(t *testing.T) {
	f := setupWorkflow(t)
	albumID := musicTestAlbum(t, f)
	musicTestCandidate(t, f, albumID, "release-1", time.Now().Unix()+600)
	// A definitive rejection lets an explicit retry resubmit once.
	f.client.addErr = problem("client_add_rejected", "test rejection")
	job, e := f.s.QueueMusic(context.Background(), albumID, "release-1", "Music")
	if e != nil || job.SubmissionAttempted != 0 {
		t.Fatalf("the rejection was not recorded: %+v %v", job, e)
	}
	f.client.addErr = nil
	stolen := false
	f.client.beforeAdd = func() {
		if stolen {
			return
		}
		stolen = true
		if _, e := f.db.Exec(`UPDATE music_downloads SET lease_owner='stale-claimant',lease_until=unixepoch()+60 WHERE id=?`, job.ID); e != nil {
			t.Fatal(e)
		}
	}
	if e := f.s.RetryMusic(context.Background(), job.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := musicTestProcess(t, f); e != nil {
		t.Fatal(e)
	}
	if f.client.adds != 2 {
		t.Fatalf("expected exactly one resubmission, got %d adds", f.client.adds)
	}
	got := musicTestState(t, f, job.ID)
	if got.SubmissionAttempted != 1 || got.State != "submitting" {
		t.Fatalf("the resubmitted job was not durably marked as submitted: %+v", got)
	}
	// The stale lease expires; a fresh claimant finds the visible torrent and
	// resumes tracking without ever calling Add again.
	if _, e := f.db.Exec(`UPDATE music_downloads SET lease_owner='',lease_until=0 WHERE id=?`, job.ID); e != nil {
		t.Fatal(e)
	}
	if processed, e := musicTestProcess(t, f); e != nil || !processed {
		t.Fatal("the reconciling claimant did not resume the job", processed, e)
	}
	if got = musicTestState(t, f, job.ID); got.State != "downloading" {
		t.Fatalf("the visible music torrent did not resume tracking: %+v", got)
	}
	if f.client.adds != 2 {
		t.Fatalf("the lease handover produced a duplicate submission: %d adds", f.client.adds)
	}
}
