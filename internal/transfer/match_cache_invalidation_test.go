package transfer

import (
	"context"
	"testing"
)

// Configuration changes must invalidate the per-scan title cache so the next
// matching run re-reads the provider instead of replaying stale results
// (CS-3). A cached transient failure reason must not survive a TMDb re-key.
func TestInvalidateAutomaticMatchCacheDropsScanEntries(t *testing.T) {
	f := setupWorkflow(t)
	scan := automaticMovieScan(t, f,
		"Movies/Test Movie (2026)/Test.Movie.2026.720p.mp4",
		"Movies/Test Movie (2026)/Test.Movie.2026.1080p.mp4",
	)
	ctx := context.Background()
	if _, err := f.s.ProcessAutomaticMatchNext(ctx); err != nil {
		t.Fatal(err)
	}
	var cache int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM library_match_cache WHERE scan_id=?`, scan.ID).Scan(&cache); err != nil || cache == 0 {
		t.Fatal("title cache was not written", cache, err)
	}
	if err := f.s.InvalidateAutomaticMatchCache(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM library_match_cache`).Scan(&cache); err != nil || cache != 0 {
		t.Fatal("configuration change did not invalidate the title cache", cache, err)
	}
	// Matching still works afterwards: the verified candidate is unaffected
	// and re-resolution (which repopulates the cache) stays fail-closed.
	if _, err := f.db.Exec(`UPDATE library_match_jobs SET state='paused',lease_owner='',lease_until=0 WHERE scan_id=?`, scan.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.StartAutomaticMatching(ctx, scan.ID); err != nil {
		t.Fatal(err)
	}
	var ready int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM library_match_candidates WHERE scan_id=? AND state='ready'`, scan.ID).Scan(&ready); err != nil || ready != 1 {
		t.Fatal("matching did not recover after cache invalidation", ready, err)
	}
}
