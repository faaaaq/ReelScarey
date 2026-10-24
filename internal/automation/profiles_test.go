package automation

import (
	"context"
	"database/sql"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/quality"
	"sync"
	"testing"
)

func applySnapshot(kind string, r ProfileReview) ProfileApply {
	in := ProfileApply{Kind: kind, Target: r.Target, TargetID: r.TargetProfile.ID, TargetRevision: r.TargetProfile.Revision, ScheduleRevision: r.ScheduleRevision}
	for _, v := range r.Items {
		in.Items = append(in.Items, ProfileExpectation{ID: v.ID, Revision: v.Revision, EffectiveID: v.Effective.ID, EffectiveRevision: v.Effective.Revision})
	}
	return in
}
func profileFixture(t *testing.T) (*sql.DB, *Service, *library.Store, []int64, string) {
	t.Helper()
	dir := t.TempDir()
	db, e := database.Open(context.Background(), dir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	catalog := library.New(db)
	ids := []int64{}
	for i, title := range []string{"Alpha", "Beta", "Gamma"} {
		v, _, e := catalog.AddMovie(context.Background(), metadata.Movie{TMDbID: int64(i + 1), Title: title}, i != 1)
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, v.ID)
	}
	return db, New(db, nil), catalog, ids, dir
}
func TestBulkProfilesReviewAtomicApplyAndDefaultInheritance(t *testing.T) {
	db, s, catalog, ids, dir := profileFixture(t)
	ctx := context.Background()
	one, two := int64(1), int64(2)
	if e := s.Assign(ctx, ids[1], AssignmentUpdate{ProfileID: &two}); e != nil {
		t.Fatal(e)
	}
	if e := s.Assign(ctx, ids[2], AssignmentUpdate{ProfileID: &one}); e != nil {
		t.Fatal(e)
	}
	oldMonitoring, e := catalog.ReviewMonitoring(ctx, "movie", ids)
	if e != nil {
		t.Fatal(e)
	}
	var before, after int
	db.QueryRow(`SELECT total_changes()`).Scan(&before)
	review, e := s.ReviewProfiles(ctx, "movie", ids, ProfileChoice{ProfileID: &two})
	db.QueryRow(`SELECT total_changes()`).Scan(&after)
	if e != nil || before != after || len(review.Items) != 3 || review.Items[0].ProfileID != nil || review.Items[0].Effective.ID != 1 || review.Items[1].Effective.ID != 2 || review.TargetProfile.Cutoff != 2160 {
		t.Fatal("review is not a coherent read", review, e)
	}
	out, e := s.ApplyProfiles(ctx, applySnapshot("movie", review))
	if e != nil || out.Changed != 2 || out.Unchanged != 1 {
		t.Fatal(out, e)
	}
	var n, rev int
	db.QueryRow(`SELECT SUM(monitored) FROM media_items`).Scan(&n)
	db.QueryRow(`SELECT revision FROM item_automation WHERE media_id=?`, ids[1]).Scan(&rev)
	if n != 2 || rev != 1 {
		t.Fatal("profile apply altered monitoring or no-op revision", n, rev)
	}
	expected := []library.MonitoringExpectation{}
	for _, item := range oldMonitoring {
		v := item.Monitored
		expected = append(expected, library.MonitoringExpectation{ID: item.ID, Monitored: &v, SeriesRevision: item.SeriesRevision, AutomationRevision: item.AutomationRevision})
	}
	if _, e = catalog.ApplyMonitoring(ctx, "movie", expected, false); !errors.Is(e, library.ErrMonitoringConflict) {
		t.Fatal("old monitoring editor not fenced", e)
	}
	if e = s.Assign(ctx, ids[0], AssignmentUpdate{ProfileID: &one, Revision: 0}); e == nil {
		t.Fatal("old assignment editor not fenced")
	}
	review, e = s.ReviewProfiles(ctx, "movie", ids, ProfileChoice{UseDefault: true})
	if e != nil {
		t.Fatal(e)
	}
	out, e = s.ApplyProfiles(ctx, applySnapshot("movie", review))
	if e != nil || out.Changed != 3 || !out.UseDefault {
		t.Fatal(out, e)
	}
	if _, e = db.Exec(`UPDATE search_schedule SET default_profile_id=2,revision=revision+1 WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	page, e := catalog.Browse(ctx, library.BrowseFilter{Kind: "movie", Page: 1, Profile: "2"})
	if e != nil || page.Total != 3 || page.ProfileContext.Default.ID != 2 {
		t.Fatal(page, e)
	}
	for _, v := range page.Items {
		if v.QualityProfile.ProfileID != nil || v.QualityProfile.Effective.ID != 2 {
			t.Fatal("default not inherited", v)
		}
	}
	db.Close()
	db, e = database.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s = New(db, nil)
	catalog = library.New(db)
	review, e = s.ReviewProfiles(ctx, "movie", ids, ProfileChoice{UseDefault: true})
	if e != nil {
		t.Fatal(e)
	}
	db.QueryRow(`SELECT total_changes()`).Scan(&before)
	out, e = s.ApplyProfiles(ctx, applySnapshot("movie", review))
	db.QueryRow(`SELECT total_changes()`).Scan(&after)
	if e != nil || out.Changed != 0 || out.Unchanged != 3 || before != after {
		t.Fatal("no-op or reopen changed state", out, e)
	}
	// Explicit assignment and inheritance differ even when their effective ID matches.
	review, e = s.ReviewProfiles(ctx, "movie", []int64{ids[0]}, ProfileChoice{ProfileID: &two})
	if e != nil {
		t.Fatal(e)
	}
	out, e = s.ApplyProfiles(ctx, applySnapshot("movie", review))
	if e != nil || out.Changed != 1 {
		t.Fatal("pinning default profile was treated as no-op", out, e)
	}
	page, e = catalog.Browse(ctx, library.BrowseFilter{Kind: "movie", Page: 1, Profile: "default"})
	if e != nil || page.Total != 2 {
		t.Fatal("inherited-only filter", page, e)
	}
	for _, value := range []string{"unknown", "0", "01", "-1", "999"} {
		if _, e = catalog.Browse(ctx, library.BrowseFilter{Kind: "movie", Page: 1, Profile: value}); !errors.Is(e, library.ErrBrowseInput) {
			t.Fatal("invalid profile filter accepted", value, e)
		}
	}
}
func TestBulkProfileConflictsRejectEntireBatch(t *testing.T) {
	for _, change := range []string{"target-rules", "current-rules", "schedule", "assignment-roundtrip", "missing-target", "missing-item"} {
		t.Run(change, func(t *testing.T) {
			db, s, _, ids, _ := profileFixture(t)
			ctx := context.Background()
			two := int64(2)
			review, e := s.ReviewProfiles(ctx, "movie", ids, ProfileChoice{ProfileID: &two})
			if e != nil {
				t.Fatal(e)
			}
			switch change {
			case "target-rules", "current-rules":
				id := int64(2)
				if change == "current-rules" {
					id = 1
				}
				p, _ := quality.New(db).Get(ctx, id)
				p.MaxSizeGiB++
				if _, e = quality.New(db).Save(ctx, p); e != nil {
					t.Fatal(e)
				}
			case "schedule":
				_, e = db.Exec(`UPDATE search_schedule SET default_profile_id=2,revision=revision+1 WHERE id=1`)
			case "assignment-roundtrip":
				if e = s.Assign(ctx, ids[2], AssignmentUpdate{ProfileID: &two}); e == nil {
					e = s.Assign(ctx, ids[2], AssignmentUpdate{Revision: 1})
				}
			case "missing-target":
				_, e = db.Exec(`DELETE FROM quality_profiles WHERE id=2`)
			case "missing-item":
				_, e = db.Exec(`DELETE FROM media_items WHERE id=?`, ids[2])
			}
			if e != nil {
				t.Fatal(e)
			}
			var before, after int
			db.QueryRow(`SELECT total_changes()`).Scan(&before)
			if _, e = s.ApplyProfiles(ctx, applySnapshot("movie", review)); !errors.Is(e, ErrProfileReview) {
				t.Fatal("stale batch accepted", e)
			}
			db.QueryRow(`SELECT total_changes()`).Scan(&after)
			if before != after {
				t.Fatal("stale batch partially wrote assignments")
			}
		})
	}
}
func TestBulkProfilesTVPreservationBoundsAndConcurrentApply(t *testing.T) {
	db, s, catalog, ids, _ := profileFixture(t)
	ctx := context.Background()
	two := int64(2)
	show, _, e := catalog.AddSeries(ctx, metadata.Series{Movie: metadata.Movie{TMDbID: 44, Title: "Saved show"}, Seasons: []metadata.SeasonSummary{{TMDbID: 45, Number: 0, Title: "Specials"}, {TMDbID: 46, Number: 1, Title: "Season 1"}}}, false)
	if e != nil {
		t.Fatal(e)
	}
	original, _ := catalog.TVShow(ctx, show.ID)
	review, e := s.ReviewProfiles(ctx, "series", []int64{show.ID}, ProfileChoice{ProfileID: &two})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ApplyProfiles(ctx, applySnapshot("series", review)); e != nil {
		t.Fatal(e)
	}
	after, e := catalog.TVShow(ctx, show.ID)
	if e != nil || after.Monitored || after.Revision != original.Revision || after.Seasons[0].Monitored || !after.Seasons[1].Monitored {
		t.Fatal("profile apply touched TV monitoring", after, e)
	}
	if _, e = s.ReviewProfiles(ctx, "movie", []int64{show.ID}, ProfileChoice{UseDefault: true}); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal("mixed type accepted", e)
	}
	for _, target := range []ProfileChoice{{}, {UseDefault: true, ProfileID: &two}, {ProfileID: new(int64)}} {
		if _, e = s.ReviewProfiles(ctx, "movie", ids, target); e == nil {
			t.Fatal("bad target accepted")
		}
	}
	for _, selected := range [][]int64{nil, {0}, {ids[0], ids[0]}, make([]int64, 101)} {
		if _, e = s.ReviewProfiles(ctx, "movie", selected, ProfileChoice{UseDefault: true}); e == nil {
			t.Fatal("bad selection accepted")
		}
	}
	review, e = s.ReviewProfiles(ctx, "movie", ids, ProfileChoice{ProfileID: &two})
	if e != nil {
		t.Fatal(e)
	}
	in := applySnapshot("movie", review)
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = s.ApplyProfiles(cancelCtx, in); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	var group sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		group.Go(func() { _, e := s.ApplyProfiles(ctx, in); results <- e })
	}
	group.Wait()
	close(results)
	success, conflicts := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if errors.Is(e, ErrProfileReview) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal(success, conflicts)
	}
	var jobs int
	db.QueryRow(`SELECT (SELECT COUNT(*) FROM downloads)+(SELECT COUNT(*) FROM automation_runs)+(SELECT COUNT(*) FROM library_scans)`).Scan(&jobs)
	if jobs != 0 {
		t.Fatal("profile edit started background work")
	}
}
