package transfer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// importTestSecondJob queues a second, distinct download for the fixture movie
// by giving the fixture indexer a different info hash and clearing the first
// torrent from the fixture client.
func importTestSecondJob(t *testing.T, f *workflowFixture) Job {
	t.Helper()
	f.indexer.hash = strings.Repeat("a", 40)
	f.client.torrent = nil
	f.client.files = nil
	f.client.adds = 0
	out, e := f.s.Search(context.Background(), f.movie.ID, "")
	if e != nil || len(out) != 1 {
		t.Fatal(out, e)
	}
	j, created, e := f.s.Queue(context.Background(), out[0].ID)
	if e != nil || !created {
		t.Fatal(j, e)
	}
	return j
}

func importTestAwait(t *testing.T, f *workflowFixture, id string) Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		j, e := f.s.Job(context.Background(), id)
		if e != nil {
			t.Fatal(e)
		}
		if j.State != "downloading" && j.State != "submitting" && j.State != "importing" {
			return j
		}
		if time.Now().After(deadline) {
			t.Fatalf("download never settled: %+v", j)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A committed import journal row keeps its destination claim forever, but the
// library file it describes can disappear outside the app. Re-acquiring the
// same title must reclaim the stale claim instead of failing with a raw
// UNIQUE-constraint error behind the generic workflow notice.
func TestStaleImportJournalClaimAllowsReimport(t *testing.T) {
	f := setupWorkflow(t)
	j1 := f.queue(t)
	f.tick(t)
	data := []byte(strings.Repeat("stale claim fixture video ", 10000))
	f.complete(t, j1, data)
	f.tick(t)
	settled := importTestAwait(t, f, j1.ID)
	if settled.State != "completed" {
		t.Fatal(settled.State, settled.ErrorCode)
	}
	var dest string
	if e := f.db.QueryRow(`SELECT destination_path FROM import_operations WHERE download_id=?`, j1.ID).Scan(&dest); e != nil {
		t.Fatal(e)
	}
	// The library file and its registration disappear outside the app.
	if e := os.Remove(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(dest))); e != nil {
		t.Fatal(e)
	}
	if _, e := f.db.Exec(`DELETE FROM media_files WHERE download_id=?`, j1.ID); e != nil {
		t.Fatal(e)
	}
	j2 := importTestSecondJob(t, f)
	f.tick(t)
	f.complete(t, j2, data)
	f.tick(t)
	settled = importTestAwait(t, f, j2.ID)
	if settled.State != "completed" {
		t.Fatalf("a stale journal claim blocked re-import: %+v", settled)
	}
	var stale int
	if e := f.db.QueryRow(`SELECT COUNT(*) FROM import_operations WHERE download_id=?`, j1.ID).Scan(&stale); e != nil || stale != 0 {
		t.Fatal("the stale journal claim was not reclaimed", stale, e)
	}
	var active int
	if e := f.db.QueryRow(`SELECT COUNT(*) FROM import_operations WHERE download_id=? AND phase='committed'`, j2.ID).Scan(&active); e != nil || active != 1 {
		t.Fatal("the new import was not journaled", active, e)
	}
	if _, e := os.Stat(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(dest))); e != nil {
		t.Fatal("the re-imported file is missing", e)
	}
}

// While the destination file still exists, a second import into the same path
// is a real conflict: the job must stop with an actionable message, never the
// generic workflow notice and never overwrite the file.
func TestLiveDestinationClaimFailsWithUsefulMessage(t *testing.T) {
	f := setupWorkflow(t)
	j1 := f.queue(t)
	f.tick(t)
	data := []byte(strings.Repeat("destination claim fixture video ", 10000))
	f.complete(t, j1, data)
	f.tick(t)
	settled := importTestAwait(t, f, j1.ID)
	if settled.State != "completed" {
		t.Fatal(settled.State, settled.ErrorCode)
	}
	var dest string
	if e := f.db.QueryRow(`SELECT destination_path FROM import_operations WHERE download_id=?`, j1.ID).Scan(&dest); e != nil {
		t.Fatal(e)
	}
	// Only the registration was removed; the file itself is still on disk.
	if _, e := f.db.Exec(`DELETE FROM media_files WHERE download_id=?`, j1.ID); e != nil {
		t.Fatal(e)
	}
	j2 := importTestSecondJob(t, f)
	f.tick(t)
	f.complete(t, j2, data)
	f.tick(t)
	settled = importTestAwait(t, f, j2.ID)
	if settled.State != "blocked" || settled.ErrorCode != "destination_claimed" {
		t.Fatalf("a live destination claim did not stop the job honestly: %+v", settled)
	}
	if strings.Contains(settled.ErrorMessage, "workflow could not finish") {
		t.Fatal("the blocked message is the generic workflow notice", settled.ErrorMessage)
	}
	// The existing library file was left untouched.
	if _, e := os.Stat(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(dest))); e != nil {
		t.Fatal("the live library file disappeared", e)
	}
}
