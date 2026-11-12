package automation

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// A season search requested from a library page scopes the run to that one
// season's monitored episodes, continues across batches until the season is
// done, never touches other seasons, and then clears its focus.
func TestSeasonSearchScopesTheRunToOneSeason(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, e := f.db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,year,category,monitored) VALUES(5,'series',5,'Season Show',2020,'standard',1);INSERT INTO series_details(media_id) VALUES(5);INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored,loaded_at) VALUES(1,5,10,1,'Season 1','','2020-01-01',2,1,'loaded'),(2,5,11,2,'Season 2','','2020-01-01',2,1,'loaded');INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(1,1,11,1,'One','','2020-01-01',1),(2,1,12,2,'Two','','2020-01-02',1),(3,2,13,1,'Three','','2020-01-01',1),(4,2,14,2,'Four','','2020-01-02',1)`); e != nil {
		t.Fatal(e)
	}
	f.i.titleFor = func(query string) string { return strings.ReplaceAll(query, " ", ".") + ".1080p.English.WEB" }
	// A batch of one proves the season focus continues across batches exactly
	// like the show focus, then clears once the season pass is complete.
	configure(t, f.s, true, "search", 1)
	season := 1
	if e := f.s.RequestItem(ctx, 5, nil, &season); e != nil {
		t.Fatal(e)
	}
	schedule, e := f.s.Get(ctx)
	if e != nil || !schedule.Requested || schedule.FocusMedia != "5" || schedule.FocusSeason != 1 {
		t.Fatal("the season request was not recorded", schedule, e)
	}
	if _, e = f.s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	schedule, e = f.s.Get(ctx)
	if e != nil || !schedule.Requested || schedule.FocusSeason != 1 {
		t.Fatal("the season search stopped before finishing its batch", schedule, e)
	}
	if _, e = f.s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	runs, _ := f.s.Runs(ctx)
	visited := map[int64]bool{}
	for _, run := range runs {
		results, e := f.s.Results(ctx, run.ID)
		if e != nil {
			t.Fatal(e)
		}
		for _, r := range results {
			if r.EpisodeID == nil || *r.EpisodeID > 2 {
				t.Fatal("the season search visited a target outside season 1", r)
			}
			visited[*r.EpisodeID] = true
		}
	}
	if len(visited) != 2 {
		t.Fatal("the season search did not visit both season 1 episodes", visited)
	}
	schedule, e = f.s.Get(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if schedule.Requested || schedule.FocusMedia != "" || schedule.FocusEpisode != "" || schedule.FocusSeason != 0 {
		t.Fatal("the finished season search kept its focus", schedule)
	}
}

// Invalid season scopes are rejected without touching the saved schedule:
// a season outside the show and a combined episode+season request never
// start a run.
func TestSeasonSearchRejectsInvalidScopes(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, e := f.db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,year,category,monitored) VALUES(5,'series',5,'Season Show',2020,'standard',1);INSERT INTO series_details(media_id) VALUES(5);INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored,loaded_at) VALUES(1,5,10,1,'Season 1','','2020-01-01',2,1,'loaded');INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(1,1,11,1,'One','','2020-01-01',1)`); e != nil {
		t.Fatal(e)
	}
	before, e := f.s.Get(ctx)
	if e != nil {
		t.Fatal(e)
	}
	foreign := 9
	err := f.s.RequestItem(ctx, 5, nil, &foreign)
	if err == nil {
		t.Fatal("a season outside the show was accepted")
	}
	var p *Error
	if !errors.As(err, &p) || p.Code != "invalid_selection" {
		t.Fatal("unexpected error for a foreign season", err)
	}
	episode := int64(1)
	season := 1
	err = f.s.RequestItem(ctx, 5, &episode, &season)
	if err == nil {
		t.Fatal("episode and season scopes were accepted together")
	}
	if !errors.As(err, &p) || p.Code != "invalid_selection" {
		t.Fatal("unexpected error for combined scopes", err)
	}
	after, e := f.s.Get(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if after.Requested || after.FocusMedia != "" || after.FocusSeason != 0 {
		t.Fatal("a rejected season request changed the schedule", before, after)
	}
}

// A season focus restricts the season-pack pass to that one season while an
// ordinary show focus still considers every packable season.
func TestPackSeasonsRespectSeasonFocus(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, e := f.db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,year,category,monitored) VALUES(5,'series',5,'Pack Show',2020,'standard',1);INSERT INTO series_details(media_id) VALUES(5);INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored,loaded_at) VALUES(1,5,10,1,'Season 1','','2020-01-01',2,1,'loaded'),(2,5,11,2,'Season 2','','2020-01-01',2,1,'loaded');INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(1,1,11,1,'One','','2020-01-01',1),(2,1,12,2,'Two','','2020-01-02',1),(3,2,13,1,'Three','','2020-01-01',1),(4,2,14,2,'Four','','2020-01-02',1)`); e != nil {
		t.Fatal(e)
	}
	all, e := f.s.packSeasons(ctx, 5, 0)
	if e != nil || len(all) != 2 || all[0] != 1 || all[1] != 2 {
		t.Fatal("the ordinary show focus must consider both seasons", all, e)
	}
	scoped, e := f.s.packSeasons(ctx, 5, 2)
	if e != nil || len(scoped) != 1 || scoped[0] != 2 {
		t.Fatal("the season focus must restrict packs to its season", scoped, e)
	}
	none, e := f.s.packSeasons(ctx, 5, 3)
	if e != nil || len(none) != 0 {
		t.Fatal("a season without packable episodes must stay empty", none, e)
	}
}
