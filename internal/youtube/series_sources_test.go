package youtube

import (
	"context"
	"errors"
	"strings"
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

// An explicit manual pick overrides monitoring and replaces an episode that
// already has a file through the reviewed replacement path, so only a genuine
// write conflict still blocks it.
func TestSeriesManualMatchesOverrideMonitoringAndQueueExplicitEpisodes(t *testing.T) {
	ctx := context.Background()
	s, inspector, mediaID := seriesFixture(t)
	for _, q := range []string{
		`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(4,1,14,4,'Four','','',1)`,
		`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(5,1,15,5,'Five','','',1)`,
	} {
		if _, e := s.db.Exec(q); e != nil {
			t.Fatal(q, e)
		}
	}
	// None of these titles carry a usable episode number.
	inspector.result.Entries = []Entry{
		{Kind: "video", ID: "xxxxxxxxx01", Title: "Intro XX", Channel: "Fixture"},
		{Kind: "video", ID: "xxxxxxxxx02", Title: "X1", Channel: "Fixture"},
		{Kind: "video", ID: "xxxxxxxxx03", Title: "X2 infinity", Channel: "Fixture"},
		{Kind: "video", ID: "yyyyyyyyy01", Title: "Extra", Channel: "Fixture"},
	}
	source, e := s.SaveSeriesSource(ctx, mediaID, SeriesSourceInput{URL: inspector.result.Source.URL, Mapping: "manual", Season: 0, StartEpisode: 1, Quality: "best", Interval: 6, AutoImport: true, Reviewed: true})
	if e != nil || source.Mapping != "manual" {
		t.Fatal(source, e)
	}
	if source.Enabled || source.Initialized {
		t.Fatal("a manual source must still start paused", source)
	}

	preview, e := s.PreviewSeriesMatches(ctx, mediaID, source.ID, inspector)
	if e != nil {
		t.Fatal(e)
	}
	if len(preview.Videos) != 4 || len(preview.Episodes) != 5 {
		t.Fatal(preview.Videos, preview.Episodes)
	}
	if preview.Videos[0].State != "unassigned" || preview.Videos[0].EpisodeID != 0 {
		t.Fatal("unbound video was not reported as unassigned", preview.Videos[0])
	}

	// A stale revision, a foreign episode and an invalid video id are refused.
	if _, e = s.SaveSeriesMatches(ctx, mediaID, source.ID, source.Revision+7, true, nil); !errors.Is(e, ErrChanged) {
		t.Fatal("stale revision accepted", e)
	}
	if _, e = s.SaveSeriesMatches(ctx, mediaID, source.ID, source.Revision, false, []SeriesMatchInput{{VideoID: "xxxxxxxxx02", EpisodeID: 999, Title: "X1"}}); !errors.Is(e, ErrChanged) {
		t.Fatal("episode outside the show accepted", e)
	}
	if _, e = s.SaveSeriesMatches(ctx, mediaID, source.ID, source.Revision, false, []SeriesMatchInput{{VideoID: "not-valid", EpisodeID: 2, Title: "X1"}}); !errors.Is(e, ErrInput) {
		t.Fatal("invalid video id accepted", e)
	}

	// Episode 1 already has a file and episode 3 is not monitored. The explicit
	// pick overrides both.
	round := []SeriesMatchInput{
		{VideoID: "xxxxxxxxx01", EpisodeID: 1, Title: "Intro XX"},
		{VideoID: "xxxxxxxxx02", EpisodeID: 2, Title: "X1"},
		{VideoID: "xxxxxxxxx03", EpisodeID: 3, Title: "X2 infinity"},
	}
	n, e := s.SaveSeriesMatches(ctx, mediaID, source.ID, source.Revision, true, round)
	if e != nil || n != 3 {
		t.Fatal(n, e)
	}
	sources, e := s.SeriesSources(ctx, mediaID)
	if e != nil || len(sources) != 1 {
		t.Fatal(sources, e)
	}
	source = sources[0]
	if source.Revision == 1 || !source.Requested || !source.Initialized {
		t.Fatal("saving matches must bump the revision and request a run", source)
	}
	matches, e := s.SeriesMatches(ctx, source.ID)
	if e != nil || len(matches) != 3 || matches[0].VideoID != "xxxxxxxxx01" || matches[0].EpisodeID != 1 {
		t.Fatal(matches, e)
	}
	preview, e = s.PreviewSeriesMatches(ctx, mediaID, source.ID, inspector)
	if e != nil {
		t.Fatal(e)
	}
	byVideo := map[string]SeriesMatchVideo{}
	for _, v := range preview.Videos {
		byVideo[v.VideoID] = v
	}
	if byVideo["xxxxxxxxx01"].State != "ready" || !strings.Contains(byVideo["xxxxxxxxx01"].Label, "replacement") {
		t.Fatal("an episode with a file must be offered as a replacement", byVideo["xxxxxxxxx01"])
	}
	if byVideo["xxxxxxxxx03"].State != "ready" {
		t.Fatal("an explicit pick must override monitoring", byVideo["xxxxxxxxx03"])
	}

	if worked, e := s.ProcessSeriesSource(ctx, inspector); e != nil || !worked {
		t.Fatal(worked, e)
	}
	jobs, e := s.Jobs(ctx)
	if e != nil || len(jobs) != 3 {
		t.Fatal("manual source queued the wrong videos", jobs, e)
	}
	labels := map[int64]string{}
	for _, job := range jobs {
		labels[job.Target.EpisodeID] = job.TargetLabel
		if job.SeriesSourceID != source.ID {
			t.Fatal("job lost its source", job)
		}
	}
	if !strings.Contains(labels[1], "replacement") {
		t.Fatal("episode 1 must queue as a replacement", labels)
	}
	if _, ok := labels[2]; !ok {
		t.Fatal("episode 2 was not queued", labels)
	}
	if _, ok := labels[3]; !ok {
		t.Fatal("the unmonitored episode was not queued", labels)
	}
	states := map[string]string{}
	entries, _ := s.SeriesEntries(ctx, source.ID)
	for _, entry := range entries {
		states[entry.VideoID] = entry.State
	}
	if states["xxxxxxxxx01"] != "queued" || states["xxxxxxxxx02"] != "queued" || states["xxxxxxxxx03"] != "queued" {
		t.Fatal(states)
	}
	if states["yyyyyyyyy01"] != "skipped" {
		t.Fatal("an unassigned video must be skipped", states)
	}

	// A previously skipped video is queued once it gets a binding, while an
	// active job for an episode still blocks a second one.
	second := append(append([]SeriesMatchInput{}, round...), SeriesMatchInput{VideoID: "yyyyyyyyy01", EpisodeID: 4, Title: "Extra"})
	if n, e = s.SaveSeriesMatches(ctx, mediaID, source.ID, source.Revision, true, second); e != nil || n != 4 {
		t.Fatal(n, e)
	}
	inspector.result.Entries = append(inspector.result.Entries, Entry{Kind: "video", ID: "zzzzzzzzz01", Title: "Zzz", Channel: "Fixture"})
	conflict := append(append([]SeriesMatchInput{}, second...), SeriesMatchInput{VideoID: "zzzzzzzzz01", EpisodeID: 2, Title: "Zzz"})
	latest, e := s.SeriesSources(ctx, mediaID)
	if e != nil || len(latest) != 1 {
		t.Fatal(latest, e)
	}
	if _, e = s.SaveSeriesMatches(ctx, mediaID, source.ID, latest[0].Revision, true, conflict); e != nil {
		t.Fatal(e)
	}
	if worked, e := s.ProcessSeriesSource(ctx, inspector); e != nil || !worked {
		t.Fatal(worked, e)
	}
	jobs, e = s.Jobs(ctx)
	if e != nil || len(jobs) != 4 {
		t.Fatal("the reassigned video was not queued exactly once", jobs, e)
	}
	states = map[string]string{}
	episodes := map[string]int64{}
	entries, _ = s.SeriesEntries(ctx, source.ID)
	for _, entry := range entries {
		states[entry.VideoID] = entry.State
		episodes[entry.VideoID] = entry.EpisodeID
	}
	if states["yyyyyyyyy01"] != "queued" || episodes["yyyyyyyyy01"] != 4 {
		t.Fatal("the reassigned video entry is wrong", states, episodes)
	}
	if states["zzzzzzzzz01"] != "skipped" {
		t.Fatal("an active download must still block an explicit pick", states)
	}
}

func TestSeriesMatchesRefusedForAutomaticSources(t *testing.T) {
	ctx := context.Background()
	s, inspector, mediaID := seriesFixture(t)
	source, e := s.SaveSeriesSource(ctx, mediaID, SeriesSourceInput{URL: inspector.result.Source.URL, Mapping: "episode", Season: 1, StartEpisode: 1, Quality: "best", Interval: 6, Reviewed: true})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.PreviewSeriesMatches(ctx, mediaID, source.ID, inspector); !errors.Is(e, ErrInput) {
		t.Fatal("automatic source accepted a manual preview", e)
	}
	if _, e = s.SaveSeriesMatches(ctx, mediaID, source.ID, source.Revision, true, []SeriesMatchInput{{VideoID: "aaaaaaaaaaa", EpisodeID: 2, Title: "x"}}); !errors.Is(e, ErrInput) {
		t.Fatal("automatic source accepted manual matches", e)
	}
}
