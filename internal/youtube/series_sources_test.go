package youtube

import (
	"context"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

type seriesInspector struct{ result Inspection }

func (p *seriesInspector) Inspect(context.Context, string) (Inspection, error) {
	return p.result, nil
}

func seriesFixture(t *testing.T) (*Store, *seriesInspector, int64) {
	t.Helper()
	ctx := context.Background()
	db, e := database.Open(ctx, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	queries := []string{
		`INSERT INTO media_items(id,kind,tmdb_id,title,year,status,monitored) VALUES(1,'series',1,'Fixture Show',2024,'missing',1)`,
		`INSERT INTO series_details(media_id) VALUES(1)`,
		`INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored) VALUES(1,1,1,1,'Season 1','','',3,1)`,
		`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(1,1,11,1,'One','','',1)`,
		`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(2,1,12,2,'Two','','',1)`,
		`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(3,1,13,3,'Three','','',0)`,
		`INSERT INTO media_files(id,series_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name) VALUES(1,1,'TV/Fixture/One.mkv',10,'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','{}',1,'existing','One.mkv')`,
		`INSERT INTO episode_files(episode_id,media_file_id) VALUES(1,1)`,
	}
	for _, q := range queries {
		if _, e = db.Exec(q); e != nil {
			t.Fatal(q, e)
		}
	}
	url := "https://www.youtube.com/playlist?list=PLabcdefghijk"
	source, _ := NormalizeURL(url)
	inspector := &seriesInspector{result: Inspection{Source: source, Entries: []Entry{
		{Kind: "video", ID: "aaaaaaaaaaa", Title: "Fixture Show S01E01", Channel: "Fixture"},
		{Kind: "video", ID: "bbbbbbbbbbb", Title: "Fixture Show S01E02", Channel: "Fixture"},
	}}}
	return NewStore(db), inspector, 1
}

func TestSeriesSourceBaselineThenExactEpisodeQueue(t *testing.T) {
	ctx := context.Background()
	s, inspector, mediaID := seriesFixture(t)
	source, e := s.SaveSeriesSource(ctx, mediaID, SeriesSourceInput{URL: inspector.result.Source.URL, Mapping: "episode", Season: 1, StartEpisode: 1, Quality: "1080", Interval: 6, AutoImport: true, Reviewed: true})
	if e != nil {
		t.Fatal(e)
	}
	if source.Enabled || source.Initialized {
		t.Fatal("new source must start paused and uninitialized")
	}
	if e = s.RequestSeriesCheck(ctx, mediaID, source.ID, source.Revision); e != nil {
		t.Fatal(e)
	}
	if worked, e := s.ProcessSeriesSource(ctx, inspector); e != nil || !worked {
		t.Fatal(worked, e)
	}
	entries, e := s.SeriesEntries(ctx, source.ID)
	if e != nil || len(entries) != 2 || entries[0].State != "baseline" {
		t.Fatal(entries, e)
	}
	sources, e := s.SeriesSources(ctx, mediaID)
	if e != nil || len(sources) != 1 {
		t.Fatal(sources, e)
	}
	source = sources[0]
	update := SeriesSourceInput{URL: source.URL, Mapping: source.Mapping, Season: source.Season, StartEpisode: source.StartEpisode, Quality: source.Quality, Subtitles: source.Subtitles, AutoImport: true, Enabled: true, Interval: source.Interval, Revision: source.Revision, Reviewed: true}
	source, e = s.SaveSeriesSource(ctx, mediaID, update)
	if e != nil || !source.Enabled {
		t.Fatal(source, e)
	}
	inspector.result.Entries = append(inspector.result.Entries, Entry{Kind: "video", ID: "ccccccccccc", Title: "Fixture Show S01E02", Channel: "Fixture"})
	if e = s.RequestSeriesCheck(ctx, mediaID, source.ID, source.Revision); e != nil {
		t.Fatal(e)
	}
	if worked, e := s.ProcessSeriesSource(ctx, inspector); e != nil || !worked {
		t.Fatal(worked, e)
	}
	jobs, e := s.Jobs(ctx)
	if e != nil || len(jobs) != 1 || jobs[0].Target.EpisodeID != 2 || !jobs[0].AutoImport || jobs[0].SeriesSourceID != source.ID || jobs[0].Quality != "1080" {
		t.Fatal(jobs, e)
	}
	entries, e = s.SeriesEntries(ctx, source.ID)
	if e != nil {
		t.Fatal(e)
	}
	states := map[string]string{}
	for _, entry := range entries {
		states[entry.VideoID] = entry.State
	}
	if states["ccccccccccc"] != "queued" {
		t.Fatal(states)
	}
	sources, e = s.SeriesSources(ctx, mediaID)
	if e != nil || len(sources) != 1 {
		t.Fatal(sources, e)
	}
	source = sources[0]
	// The same video and a new episode with an existing file must be skipped.
	inspector.result.Entries = append(inspector.result.Entries, Entry{Kind: "video", ID: "ddddddddddd", Title: "Fixture Show S01E01", Channel: "Fixture"})
	if e = s.RequestSeriesCheck(ctx, mediaID, source.ID, source.Revision); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ProcessSeriesSource(ctx, inspector); e != nil {
		t.Fatal(e)
	}
	jobs, e = s.Jobs(ctx)
	if e != nil || len(jobs) != 1 {
		t.Fatal("duplicate or existing episode queued", jobs, e)
	}
	entries, _ = s.SeriesEntries(ctx, source.ID)
	for _, entry := range entries {
		if entry.VideoID == "ddddddddddd" && entry.State != "skipped" {
			t.Fatal(entry)
		}
	}
}

func TestSeriesSourceOrderPreviewAndMonitoredFilter(t *testing.T) {
	ctx := context.Background()
	s, inspector, mediaID := seriesFixture(t)
	preview, e := s.PreviewSeriesSource(ctx, mediaID, SeriesSourceInput{URL: inspector.result.Source.URL, Mapping: "order", Season: 1, StartEpisode: 2, Quality: "best", Interval: 6}, inspector)
	if e != nil {
		t.Fatal(e)
	}
	if len(preview.Entries) != 2 || preview.Entries[0].State != "ready" || preview.Entries[0].Episode != 2 || preview.Entries[1].State != "skipped" {
		t.Fatal(preview)
	}
}
