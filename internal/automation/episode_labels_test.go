package automation

import (
	"context"
	"testing"
)

func TestExplicitEpisodeLabelsQueueOnlyTheCanonicalSingleEpisode(t *testing.T) {
	for _, tc := range []struct {
		label string
		want  int
	}{{"S01EP01", 1}, {"Season 1 Ep 1", 1}, {"Season 1 Episodes 1-2", 0}, {"S01E01E02", 0}, {"S01E01 and 2", 0}} {
		t.Run(tc.label, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			if _, e := f.db.Exec(`DELETE FROM media_items; INSERT INTO media_items(id,kind,tmdb_id,title,year,monitored,release_date) VALUES(5,'series',5,'Show',2020,1,'2020-01-01');INSERT INTO series_details(media_id) VALUES(5);INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored,loaded_at) VALUES(1,5,10,1,'Season','','2020-01-01',1,1,'loaded');INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,monitored,air_date) VALUES(1,1,11,1,'One','',1,'2020-01-01')`); e != nil {
				t.Fatal(e)
			}
			f.i.title = "Show.2020." + tc.label + ".1080p.English.WEB"
			configure(t, f.s, false, "download", 10)
			if e := f.s.Request(ctx); e != nil {
				t.Fatal(e)
			}
			if _, e := f.s.ProcessNext(ctx); e != nil {
				t.Fatal(e)
			}
			var count int
			if e := f.db.QueryRow(`SELECT COUNT(*) FROM downloads WHERE kind='series'`).Scan(&count); e != nil || count != tc.want {
				t.Fatal("wrong automatic reservation", count, e)
			}
			if count == 1 {
				var episode, tmdb int64
				var season, number int
				if e := f.db.QueryRow(`SELECT episode_id,tmdb_id,season_number,episode_number FROM download_targets`).Scan(&episode, &tmdb, &season, &number); e != nil || episode != 1 || tmdb != 11 || season != 1 || number != 1 {
					t.Fatal("canonical identity changed", e)
				}
			}
		})
	}
}
