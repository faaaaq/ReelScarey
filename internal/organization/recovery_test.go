package organization

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// A crash between the staging rename and the removal commit leaves the
// catalog registered while the content lives under the staged name. The
// sweep must rename it back and journal the reconciliation (NEW-B1).
func TestRecoverStagedRemovalsRestoresRegisteredPath(t *testing.T) {
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/Film/film.mkv", []byte("video"))
	staged := filepath.Join(f.cfg.MediaDir, "Movies", "Film", "film.mkv"+stagedRemovalSuffix+"1000")
	if err := os.Rename(filepath.Join(f.cfg.MediaDir, "Movies", "Film", "film.mkv"), staged); err != nil {
		t.Fatal(err)
	}
	if err := f.s.RecoverStagedRemovals(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.cfg.MediaDir, "Movies", "Film", "film.mkv")); err != nil {
		t.Fatal("the staged copy was not restored to its registered path", err)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatal("the staged copy remained", err)
	}
	var events int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='title' AND state='` + stagedRemovalRepaired + `'`).Scan(&events); err != nil || events != 1 {
		t.Fatal("the reconciliation was not journaled", events, err)
	}
}

// A crash after the commit but before the cleanup leaves an orphaned staged
// copy the catalog no longer references. The sweep must journal it for
// manual deletion and never auto-delete content (NEW-B2).
func TestRecoverStagedRemovalsJournalsOrphanWithoutDeleting(t *testing.T) {
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/Film/film.mkv", []byte("video"))
	staged := filepath.Join(f.cfg.MediaDir, "Movies", "Film", "film.mkv"+stagedRemovalSuffix+"1000")
	if err := os.Rename(filepath.Join(f.cfg.MediaDir, "Movies", "Film", "film.mkv"), staged); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`DELETE FROM media_files WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err := f.s.RecoverStagedRemovals(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staged); err != nil {
		t.Fatal("the orphaned staged copy must be retained for manual review", err)
	}
	var events int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='title' AND state='` + stagedRemovalOrphan + `'`).Scan(&events); err != nil || events != 1 {
		t.Fatal("the orphan was not journaled", events, err)
	}
}

// A staged copy whose content does not match the registered checksum is
// never restored: the sweep journals it as a leftover for manual review and
// leaves the registered row untouched (VERIF-B-2).
func TestRecoverStagedRemovalsRefusesUnverifiedStagedContent(t *testing.T) {
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/Film/film.mkv", []byte("video"))
	staged := filepath.Join(f.cfg.MediaDir, "Movies", "Film", "film.mkv"+stagedRemovalSuffix+"1000")
	if err := os.Rename(filepath.Join(f.cfg.MediaDir, "Movies", "Film", "film.mkv"), staged); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("tampered content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.s.RecoverStagedRemovals(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staged); err != nil {
		t.Fatal("the unverified staged copy must be retained for manual review", err)
	}
	if _, err := os.Stat(filepath.Join(f.cfg.MediaDir, "Movies", "Film", "film.mkv")); !os.IsNotExist(err) {
		t.Fatal("the registered path must not be restored from unverified content", err)
	}
	var repaired int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='title' AND state='` + stagedRemovalRepaired + `'`).Scan(&repaired); err != nil || repaired != 0 {
		t.Fatal("an unverified restore was journaled as repaired", repaired, err)
	}
	var left int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='title' AND state='` + stagedRemovalLeft + `'`).Scan(&left); err != nil || left != 1 {
		t.Fatal("the unverified staged copy was not journaled as a leftover", left, err)
	}
}

// A staged copy beside a live file is a leftover, not a crashed removal:
// the sweep must journal its location and leave both files untouched.
func TestRecoverStagedRemovalsLeavesLeftoverAndJournals(t *testing.T) {
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/Film/film.mkv", []byte("video"))
	staged := filepath.Join(f.cfg.MediaDir, "Movies", "Film", "film.mkv"+stagedRemovalSuffix+"1000")
	if err := os.WriteFile(staged, []byte("leftover"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.s.RecoverStagedRemovals(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staged); err != nil {
		t.Fatal("the leftover copy was removed", err)
	}
	if _, err := os.Stat(filepath.Join(f.cfg.MediaDir, "Movies", "Film", "film.mkv")); err != nil {
		t.Fatal("the live file was disturbed", err)
	}
	var events int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='title' AND state='` + stagedRemovalLeft + `'`).Scan(&events); err != nil || events != 1 {
		t.Fatal("the leftover was not journaled", events, err)
	}
}

// A staged copy younger than the grace period is presumed in-flight and
// must be ignored entirely: no rename, no journal.
func TestRecoverStagedRemovalsIgnoresInFlightStaging(t *testing.T) {
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/Film/film.mkv", []byte("video"))
	// Use a real fresh unixnano staging name so the grace window itself is
	// what excludes the copy, not the suffix parse guard.
	fresh := filepath.Join(f.cfg.MediaDir, "Movies", "Film", "film.mkv"+stagedRemovalSuffix+strconv.FormatInt(time.Now().UnixNano(), 10))
	if err := os.Rename(filepath.Join(f.cfg.MediaDir, "Movies", "Film", "film.mkv"), fresh); err != nil {
		t.Fatal(err)
	}
	if err := f.s.RecoverStagedRemovals(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("the in-flight staged copy was disturbed", err)
	}
	var events int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='title' AND object_id LIKE '%reelscarey-removing%'`).Scan(&events); err != nil || events != 0 {
		t.Fatal("the in-flight staged copy was journaled", events, err)
	}
}

// Journaling is deduplicated per (object_id, state): a leftover the operator
// has not resolved produces one event, not one per startup.
func TestRecoverStagedRemovalsJournalsDeduplicated(t *testing.T) {
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/Film/film.mkv", []byte("video"))
	staged := filepath.Join(f.cfg.MediaDir, "Movies", "Film", "film.mkv"+stagedRemovalSuffix+"1000")
	if err := os.WriteFile(staged, []byte("leftover"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := f.s.RecoverStagedRemovals(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	var events int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='title' AND state='` + stagedRemovalLeft + `'`).Scan(&events); err != nil || events != 1 {
		t.Fatalf("expected one deduplicated event, got %d (%v)", events, err)
	}
	_ = time.Now
}

// A sweep that cannot run (media root unavailable) is journaled as a visible
// Activity event, deduplicated across startups, never silent (RS-0014).
func TestRecoverStagedRemovalsJournalsSkippedSweep(t *testing.T) {
	f := newReviewFixture(t)
	f.s.cfg.MediaDir = filepath.Join(f.cfg.MediaDir, "gone")
	if err := f.s.RecoverStagedRemovals(context.Background()); err == nil {
		t.Fatal("the sweep must fail when the media root is missing")
	}
	for i := 0; i < 2; i++ {
		f.s.journalRecoverySkipped(context.Background())
	}
	var events int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='organization' AND object_id='startup' AND state='staged-removal recovery could not run; check the media folder and restart'`).Scan(&events); err != nil || events != 1 {
		t.Fatalf("expected one deduplicated skip event, got %d (%v)", events, err)
	}
}
