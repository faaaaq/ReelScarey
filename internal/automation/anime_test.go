package automation

import (
	"context"
	"github.com/faaaaq/ReelScarey/internal/library"
	"testing"
)

func TestAnimeEpisodesRequireManualOrderReviewWhileFilmsRemainEligible(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	_, e := f.db.Exec(`UPDATE media_items SET category='anime' WHERE id=1;
INSERT INTO media_items(id,kind,tmdb_id,title,monitored) VALUES(2,'series',2,'Explicit anime show',1);
INSERT INTO series_details(media_id) VALUES(2);
INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,loaded_at) VALUES(1,2,20,1,'Season 1','','2000-01-01',1,'loaded');
INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date) VALUES(1,1,21,1,'Episode','','2000-01-01');`)
	if e != nil {
		t.Fatal(e)
	}
	before, e := f.s.targets(ctx, "", 10, "", "", 0, "")
	if e != nil || len(before) != 2 {
		t.Fatal(before, e)
	}
	s := library.New(f.db)
	review, e := s.Anime(ctx, 2)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveAnime(ctx, 2, library.AnimeUpdate{Category: "anime", Revision: review.Revision, NumberingRevision: review.NumberingRevision}); e != nil {
		t.Fatal(e)
	}
	after, e := f.s.targets(ctx, "", 10, "", "", 0, "")
	if e != nil || len(after) != 1 || after[0].MediaID != 1 || after[0].TV {
		t.Fatal("anime episode order was assumed or film was excluded", after, e)
	}
	show, e := s.TVSeason(ctx, 2, 1)
	if e != nil || len(show.Episodes) != 1 || !show.Episodes[0].EffectiveMonitored {
		t.Fatal("classification changed monitoring", show, e)
	}
}
