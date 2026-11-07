package library_test

import (
	"context"
	"database/sql"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/automation"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"sync"
	"testing"
)

func expectations(items []library.MonitoringItem) []library.MonitoringExpectation {
	out := make([]library.MonitoringExpectation, 0, len(items))
	for _, v := range items {
		value := v.Monitored
		out = append(out, library.MonitoringExpectation{ID: v.ID, Monitored: &value, SeriesRevision: v.SeriesRevision, AutomationRevision: v.AutomationRevision})
	}
	return out
}
func monitoringFixture(t *testing.T) (*sql.DB, *library.Store, []int64, string) {
	t.Helper()
	dir := t.TempDir()
	db, e := database.Open(context.Background(), dir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	s := library.New(db)
	ids := []int64{}
	for i := range 3 {
		v, _, e := s.AddMovie(context.Background(), metadata.Movie{TMDbID: int64(i + 1), Title: "Saved movie"}, i != 1)
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, v.ID)
	}
	return db, s, ids, dir
}

func TestReviewedMovieMonitoringPreservesProfilesAndFencesStaleEdits(t *testing.T) {
	db, s, ids, dir := monitoringFixture(t)
	ctx := context.Background()
	a := automation.New(db, nil)
	profile := int64(2)
	if e := a.Assign(ctx, ids[0], automation.AssignmentUpdate{ProfileID: &profile}); e != nil {
		t.Fatal(e)
	}
	var before, after int
	db.QueryRow(`SELECT total_changes()`).Scan(&before)
	review, e := s.ReviewMonitoring(ctx, "movie", ids)
	if e != nil || len(review) != 3 {
		t.Fatal(review, e)
	}
	db.QueryRow(`SELECT total_changes()`).Scan(&after)
	if before != after {
		t.Fatal("preview wrote preferences")
	}
	original := expectations(review)
	out, e := s.ApplyMonitoring(ctx, "movie", original, false)
	if e != nil || out.Total != 3 || out.Changed != 2 || out.Unchanged != 1 {
		t.Fatal(out, e)
	}
	var saved int64
	if e = db.QueryRow(`SELECT profile_id FROM item_automation WHERE media_id=?`, ids[0]).Scan(&saved); e != nil || saved != profile {
		t.Fatal("profile was replaced", saved, e)
	}
	if e = a.Assign(ctx, ids[0], automation.AssignmentUpdate{Revision: 1, ProfileID: &profile}); e == nil {
		t.Fatal("stale single-item edit accepted after bulk")
	}
	if _, e = s.ApplyMonitoring(ctx, "movie", original, true); !errors.Is(e, library.ErrMonitoringConflict) {
		t.Fatal("stale bulk accepted", e)
	}
	review, e = s.ReviewMonitoring(ctx, "movie", ids)
	if e != nil {
		t.Fatal(e)
	}
	if out, e = s.ApplyMonitoring(ctx, "movie", expectations(review), true); e != nil || out.Changed != 3 {
		t.Fatal(out, e)
	}
	// Moving away and back must invalidate an earlier review even when its bool matches.
	review, e = s.ReviewMonitoring(ctx, "movie", ids)
	if e != nil {
		t.Fatal(e)
	}
	off, on := false, true
	rev := review[0].AutomationRevision
	if e = a.Assign(ctx, ids[0], automation.AssignmentUpdate{Revision: rev, ProfileID: &profile, Monitored: &off}); e != nil {
		t.Fatal(e)
	}
	if e = a.Assign(ctx, ids[0], automation.AssignmentUpdate{Revision: rev + 1, ProfileID: &profile, Monitored: &on}); e != nil {
		t.Fatal(e)
	}
	stale := expectations([]library.MonitoringItem{review[2], review[0]})
	if _, e = s.ApplyMonitoring(ctx, "movie", stale, false); !errors.Is(e, library.ErrMonitoringConflict) {
		t.Fatal(e)
	}
	var monitored int
	db.QueryRow(`SELECT SUM(monitored) FROM media_items`).Scan(&monitored)
	if monitored != 3 {
		t.Fatal("stale batch partially changed titles", monitored)
	}
	db.Close()
	db, e = database.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s = library.New(db)
	if _, e = s.ApplyMonitoring(ctx, "movie", stale, false); !errors.Is(e, library.ErrMonitoringConflict) {
		t.Fatal("reopen lost review fence", e)
	}
	fresh, e := s.ReviewMonitoring(ctx, "movie", ids)
	if e != nil {
		t.Fatal(e)
	}
	db.QueryRow(`SELECT total_changes()`).Scan(&before)
	out, e = s.ApplyMonitoring(ctx, "movie", expectations(fresh), true)
	db.QueryRow(`SELECT total_changes()`).Scan(&after)
	if e != nil || out.Changed != 0 || out.Unchanged != 3 || before != after {
		t.Fatal("no-op changed revisions", out, e)
	}
}

func TestBulkTVMonitoringKeepsChildSelectionsAndInvalidatesTVRevision(t *testing.T) {
	db, s, _, _ := monitoringFixture(t)
	ctx := context.Background()
	summary := metadata.SeasonSummary{TMDbID: 31, Number: 1, Title: "Season 1", EpisodeCount: 2}
	show, _, e := s.AddSeries(ctx, metadata.Series{Movie: metadata.Movie{TMDbID: 30, Title: "Saved show"}, Seasons: []metadata.SeasonSummary{{TMDbID: 32, Number: 0, Title: "Specials"}, summary}}, true)
	if e != nil {
		t.Fatal(e)
	}
	details, _ := s.TVShow(ctx, show.ID)
	if e = s.SaveSeason(ctx, show.ID, metadata.Season{SeasonSummary: summary, Episodes: []metadata.Episode{{TMDbID: 33, Number: 1, Title: "First"}, {TMDbID: 34, Number: 2, Title: "Second"}}}, details.Revision); e != nil {
		t.Fatal(e)
	}
	details, _ = s.TVShow(ctx, show.ID)
	season, episode := 1, 2
	if e = s.MonitorTV(ctx, show.ID, details.Revision, &season, &episode, false); e != nil {
		t.Fatal(e)
	}
	review, e := s.ReviewMonitoring(ctx, "series", []int64{show.ID})
	if e != nil {
		t.Fatal(e)
	}
	out, e := s.ApplyMonitoring(ctx, "series", expectations(review), false)
	if e != nil || out.Changed != 1 {
		t.Fatal(out, e)
	}
	if e = s.MonitorTV(ctx, show.ID, review[0].SeriesRevision, nil, nil, true); !errors.Is(e, library.ErrRevision) {
		t.Fatal("bulk did not fence old TV editor", e)
	}
	loaded, e := s.TVSeason(ctx, show.ID, 1)
	if e != nil || !loaded.Season.Monitored || !loaded.Episodes[0].Monitored || loaded.Episodes[0].EffectiveMonitored || loaded.Episodes[1].Monitored {
		t.Fatal("pause overwrote child selections", loaded, e)
	}
	review, _ = s.ReviewMonitoring(ctx, "series", []int64{show.ID})
	if _, e = s.ApplyMonitoring(ctx, "series", expectations(review), true); e != nil {
		t.Fatal(e)
	}
	loaded, e = s.TVSeason(ctx, show.ID, 1)
	if e != nil || !loaded.Episodes[0].EffectiveMonitored || loaded.Episodes[1].EffectiveMonitored {
		t.Fatal("resume did not retain episode choices", loaded, e)
	}
	details, _ = s.TVShow(ctx, show.ID)
	if details.Seasons[0].Monitored {
		t.Fatal("specials were selected")
	}
	review, _ = s.ReviewMonitoring(ctx, "series", []int64{show.ID})
	if e = s.MonitorTV(ctx, show.ID, details.Revision, &season, nil, false); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ApplyMonitoring(ctx, "series", expectations(review), false); !errors.Is(e, library.ErrMonitoringConflict) {
		t.Fatal("stale TV preview accepted", e)
	}
	var jobs int
	db.QueryRow(`SELECT (SELECT COUNT(*) FROM downloads)+(SELECT COUNT(*) FROM automation_runs)+(SELECT COUNT(*) FROM library_scans)`).Scan(&jobs)
	if jobs != 0 {
		t.Fatal("bulk started work")
	}
}

func TestMonitoringBoundsMissingTargetsAndCancellation(t *testing.T) {
	db, s, ids, _ := monitoringFixture(t)
	ctx := context.Background()
	tooMany := make([]int64, 5001)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	for _, tc := range []struct {
		kind string
		ids  []int64
	}{{"movie", nil}, {"all", ids}, {"movie", []int64{ids[0], ids[0]}}, {"movie", []int64{0}}, {"movie", tooMany}} {
		if _, e := s.ReviewMonitoring(ctx, tc.kind, tc.ids); !errors.Is(e, library.ErrMonitoringInput) {
			t.Fatal(tc, e)
		}
	}
	if _, e := s.ReviewMonitoring(ctx, "movie", []int64{999}); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal(e)
	}
	review, _ := s.ReviewMonitoring(ctx, "movie", ids)
	expected := expectations(review)
	expected[2].ID = 999
	if _, e := s.ApplyMonitoring(ctx, "movie", expected, false); !errors.Is(e, library.ErrMonitoringConflict) {
		t.Fatal(e)
	}
	expected = expectations(review)
	expected[0].Monitored = nil
	if _, e := s.ApplyMonitoring(ctx, "movie", expected, false); !errors.Is(e, library.ErrMonitoringInput) {
		t.Fatal(e)
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, e := s.ApplyMonitoring(cancelCtx, "movie", expectations(review), false); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	var n int
	db.QueryRow(`SELECT SUM(monitored) FROM media_items`).Scan(&n)
	if n != 2 {
		t.Fatal("invalid request changed preferences")
	}
}

func TestMonitoringAcceptsLargeExplicitSelectionsAndStillRefusesMixedReviews(t *testing.T) {
	db, s, ids, _ := monitoringFixture(t)
	ctx := context.Background()
	many := append([]int64{}, ids...)
	for i := len(many) + 1; i <= 101; i++ {
		v, _, e := s.AddMovie(ctx, metadata.Movie{TMDbID: int64(i), Title: "Large selection movie"}, false)
		if e != nil {
			t.Fatal(e)
		}
		many = append(many, v.ID)
	}
	review, e := s.ReviewMonitoring(ctx, "movie", many)
	if e != nil || len(review) != 101 {
		t.Fatal("101 explicit IDs were not accepted", len(review), e)
	}
	var before int
	if e = db.QueryRow(`SELECT SUM(monitored) FROM media_items`).Scan(&before); e != nil {
		t.Fatal(e)
	}
	mixed := expectations(review)
	mixed[len(mixed)-1].AutomationRevision++
	if _, e = s.ApplyMonitoring(ctx, "movie", mixed, false); !errors.Is(e, library.ErrMonitoringConflict) {
		t.Fatal("mixed revision was accepted", e)
	}
	var after int
	if e = db.QueryRow(`SELECT SUM(monitored) FROM media_items`).Scan(&after); e != nil || after != before {
		t.Fatal("mixed revision partially changed titles", before, after, e)
	}
	if _, e = s.ApplyMonitoring(ctx, "movie", expectations(review), false); e != nil {
		t.Fatal(e)
	}
}

func TestConcurrentBulkApplyHasOneWinnerAndKeepsTrackedJobs(t *testing.T) {
	db, s, ids, _ := monitoringFixture(t)
	ctx := context.Background()
	// Controlled job state only; this test starts no client or provider.
	if _, e := db.Exec(`INSERT INTO release_candidates(id,movie_id,title,size_bytes,published_at,payload,expires_at) VALUES('selected',?,'Controlled release',1,'',X'00',9999999999)`, ids[0]); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(`INSERT INTO downloads(id,movie_id,release_id,title,info_hash,payload,client_url,save_path,state,progress,created_at,updated_at) VALUES('tracked',?,'selected','Controlled release','hash',X'00','unused','unused','downloading',0.25,1,1)`, ids[0]); e != nil {
		t.Fatal(e)
	}
	review, e := s.ReviewMonitoring(ctx, "movie", ids)
	if e != nil {
		t.Fatal(e)
	}
	expected := expectations(review)
	results := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Go(func() { _, e := s.ApplyMonitoring(ctx, "movie", expected, false); results <- e })
	}
	group.Wait()
	close(results)
	success, conflicts := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if errors.Is(e, library.ErrMonitoringConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal("concurrent edit was not fenced", success, conflicts)
	}
	var state string
	var progress float64
	if e = db.QueryRow(`SELECT state,progress FROM downloads WHERE id='tracked'`).Scan(&state, &progress); e != nil || state != "downloading" || progress != 0.25 {
		t.Fatal("monitoring touched tracked download", state, progress, e)
	}
}
