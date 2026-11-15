package youtube

import (
	"context"
	"errors"
	"fmt"
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

// twoShowsOneChannel builds two series that share one channel URL. XTRA XG has
// season-1 episodes 1..20 (episode 12 already carries a file), XG DAYS covers
// episode numbers 30..40 of its own season 1. The same channel publishes both
// shows' "#NN" titles plus an unnumbered extra.
func twoShowsOneChannel(t *testing.T) (*Store, *seriesInspector) {
	t.Helper()
	ctx := context.Background()
	db, e := database.Open(ctx, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	queries := []string{
		`INSERT INTO media_items(id,kind,tmdb_id,title,year,status,monitored) VALUES(1,'series',1,'XTRA XG',2024,'missing',1)`,
		`INSERT INTO series_details(media_id) VALUES(1)`,
		`INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored) VALUES(1,1,1,1,'Season 1','','',20,1)`,
		`INSERT INTO media_items(id,kind,tmdb_id,title,year,status,monitored) VALUES(2,'series',2,'XG DAYS',2024,'missing',1)`,
		`INSERT INTO series_details(media_id) VALUES(2)`,
		`INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored) VALUES(2,2,2,1,'Season 1','','',11,1)`,
	}
	for n := 1; n <= 20; n++ {
		queries = append(queries, fmt.Sprintf(`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(%d,1,%d,%d,'XTRA %d','','',1)`, n, n, n, n))
	}
	for n := 30; n <= 40; n++ {
		queries = append(queries, fmt.Sprintf(`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(%d,2,%d,%d,'DAYS %d','','',1)`, 100+n-30, n, n, n))
	}
	// Episode 12 of XTRA XG already carries a registered file.
	queries = append(queries,
		`INSERT INTO media_files(id,series_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name) VALUES(1,1,'TV/X/One.mkv',10,'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','{}',1,'existing','One.mkv')`,
		`INSERT INTO episode_files(episode_id,media_file_id) VALUES(12,1)`)
	for _, q := range queries {
		if _, e = db.Exec(q); e != nil {
			t.Fatal(q, e)
		}
	}
	source, _ := NormalizeURL("https://www.youtube.com/channel/UC1234567890123456789012")
	inspector := &seriesInspector{result: Inspection{Source: source, Entries: []Entry{
		{Kind: "video", ID: "xtraxg00001", Title: "XTRA XG #12", Channel: "XG"},
		{Kind: "video", ID: "xtraxg00002", Title: "XTRA XG #15", Channel: "XG"},
		{Kind: "video", ID: "xgdays00034", Title: "XG DAYS #34", Channel: "XG"},
		{Kind: "video", ID: "xgdays00089", Title: "XG DAYS #89", Channel: "XG"},
		{Kind: "video", ID: "trailer0001", Title: "Behind the scenes", Channel: "XG"},
	}}}
	return NewStore(db), inspector
}

func enableSeriesSource(ctx context.Context, s *Store, mediaID int64, in SeriesSourceInput) (SeriesSource, error) {
	source, e := s.SaveSeriesSource(ctx, mediaID, in)
	if e != nil {
		return source, e
	}
	return s.SaveSeriesSource(ctx, mediaID, SeriesSourceInput{URL: in.URL, Mapping: in.Mapping, Season: in.Season, StartEpisode: in.StartEpisode, Quality: in.Quality, Subtitles: source.Subtitles, AutoImport: in.AutoImport, Enabled: true, Interval: in.Interval, Revision: source.Revision, Reviewed: true})
}

// Two shows attach the same channel with mapping='episode'. The "#NN" hash
// suffix maps each video against that show's own catalog season, cross-show
// titles fail per-show episode lookup and land as honest skipped entries, and
// a repeat check never requeues anything.
func TestSeriesSourceHashEpisodeMapping(t *testing.T) {
	ctx := context.Background()
	if season, number, ok := mapEpisodeNumber(SeriesSourceInput{Mapping: "episode", Season: 1}, "XG DAYS #34", 0); !ok || season != 1 || number != 34 {
		t.Fatal("hash mapping", season, number, ok)
	}
	if season, number, ok := mapEpisodeNumber(SeriesSourceInput{Mapping: "episode"}, "XTRA XG #89", 0); !ok || season != 0 || number != 89 {
		t.Fatal("hash mapping without a season", season, number, ok)
	}
	if _, _, ok := mapEpisodeNumber(SeriesSourceInput{Mapping: "episode"}, "Behind the scenes", 0); ok {
		t.Fatal("unnumbered title mapped")
	}
	s, inspector := twoShowsOneChannel(t)
	first, e := enableSeriesSource(ctx, s, 1, SeriesSourceInput{URL: inspector.result.Source.URL, Mapping: "episode", Season: 1, StartEpisode: 1, Quality: "best", Interval: 6, Reviewed: true})
	if e != nil {
		t.Fatal(e)
	}
	second, e := enableSeriesSource(ctx, s, 2, SeriesSourceInput{URL: inspector.result.Source.URL, Mapping: "episode", Season: 1, StartEpisode: 1, Quality: "best", Interval: 6, Reviewed: true})
	if e != nil {
		t.Fatal("the same channel URL is allowed for two shows", e)
	}
	// Every check round must fence against the source's current revision, so
	// the source is refetched before its check is requested.
	runRound := func(backfill bool) {
		for _, media := range []int64{1, 2} {
			sources, e := s.SeriesSources(ctx, media)
			if e != nil || len(sources) != 1 {
				t.Fatal(sources, e)
			}
			if backfill {
				e = s.BackfillSeriesSource(ctx, media, sources[0].ID, sources[0].Revision)
			} else {
				e = s.RequestSeriesCheck(ctx, media, sources[0].ID, sources[0].Revision)
			}
			if e != nil {
				t.Fatal(e)
			}
		}
		for n := 0; n < 2; n++ {
			if worked, e := s.ProcessSeriesSource(ctx, inspector); e != nil || !worked {
				t.Fatal(worked, e)
			}
		}
	}
	// The first round is a baseline; the second backfills so the existing
	// "#NN" backlog is mapped against each show's own catalog.
	runRound(false)
	runRound(true)
	jobs, e := s.Jobs(ctx)
	if e != nil || len(jobs) != 2 {
		t.Fatal(jobs, e)
	}
	bySource := map[string]Job{}
	for _, job := range jobs {
		bySource[job.SeriesSourceID] = job
	}
	if bySource[first.ID].Target.EpisodeID != 15 || bySource[second.ID].Target.EpisodeID != 104 {
		t.Fatal("hash mapping queued the wrong per-show episodes", bySource)
	}
	entries, e := s.SeriesEntries(ctx, first.ID)
	if e != nil {
		t.Fatal(e)
	}
	states := map[string]string{}
	for _, entry := range entries {
		states[entry.VideoID] = entry.State
	}
	if states["xtraxg00002"] != "queued" {
		t.Fatal(states)
	}
	if states["xtraxg00001"] != "skipped" || states["xgdays00034"] != "skipped" || states["xgdays00089"] != "skipped" || states["trailer0001"] != "skipped" {
		t.Fatal("cross-show and unnumbered videos must skip honestly", states)
	}
	notes := map[string]string{}
	for _, entry := range entries {
		notes[entry.VideoID] = entry.Note
	}
	if !strings.Contains(notes["xgdays00034"], "not loaded") {
		t.Fatal("a foreign show title must fail catalog matching", notes)
	}
	entries, e = s.SeriesEntries(ctx, second.ID)
	if e != nil {
		t.Fatal(e)
	}
	states = map[string]string{}
	for _, entry := range entries {
		states[entry.VideoID] = entry.State
	}
	if states["xgdays00034"] != "queued" || states["xtraxg00001"] != "skipped" || states["xtraxg00002"] != "skipped" || states["xgdays00089"] != "skipped" || states["trailer0001"] != "skipped" {
		t.Fatal("per-show mapping is wrong for the second show", states)
	}
	// A repeat check refreshes entries and never requeues anything.
	runRound(false)
	jobs, e = s.Jobs(ctx)
	if e != nil || len(jobs) != 2 {
		t.Fatal("a repeat check requeued videos", jobs, e)
	}
}

func TestSeriesSourceSummariesCounts(t *testing.T) {
	ctx := context.Background()
	s, inspector, mediaID := seriesFixture(t)
	source, e := s.SaveSeriesSource(ctx, mediaID, SeriesSourceInput{URL: inspector.result.Source.URL, Mapping: "episode", Season: 1, StartEpisode: 1, Quality: "1080", Interval: 6, AutoImport: true, Reviewed: true})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SeriesSourceSummaries(ctx); e != nil {
		t.Fatal(e)
	}
	if e = s.RequestSeriesCheck(ctx, mediaID, source.ID, source.Revision); e != nil {
		t.Fatal(e)
	}
	if worked, e := s.ProcessSeriesSource(ctx, inspector); e != nil || !worked {
		t.Fatal(worked, e)
	}
	sources, e := s.SeriesSources(ctx, mediaID)
	if e != nil || len(sources) != 1 {
		t.Fatal(sources, e)
	}
	source = sources[0]
	source, e = enableSeriesSource(ctx, s, mediaID, SeriesSourceInput{URL: inspector.result.Source.URL, Mapping: "episode", Season: 1, StartEpisode: 1, Quality: "1080", Interval: 6, AutoImport: true, Revision: source.Revision, Reviewed: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.RequestSeriesCheck(ctx, mediaID, source.ID, source.Revision); e != nil {
		t.Fatal(e)
	}
	if worked, e := s.ProcessSeriesSource(ctx, inspector); e != nil || !worked {
		t.Fatal(worked, e)
	}
	sources, e = s.SeriesSources(ctx, mediaID)
	if e != nil || len(sources) != 1 {
		t.Fatal(sources, e)
	}
	source = sources[0]
	// Treat the backlog as new so the two baseline videos map: episode 1
	// already has a file (downloaded), episode 2 has an active job (missing).
	if e = s.BackfillSeriesSource(ctx, mediaID, source.ID, source.Revision); e != nil {
		t.Fatal(e)
	}
	if worked, e := s.ProcessSeriesSource(ctx, inspector); e != nil || !worked {
		t.Fatal(worked, e)
	}
	out, e := s.SeriesSourceSummaries(ctx)
	if e != nil || len(out) != 1 {
		t.Fatal(out, e)
	}
	summary := out[0]
	if summary.ID != source.ID || summary.MediaID != mediaID || summary.ShowTitle != "Fixture Show" || summary.Show.ID != mediaID || summary.Show.Title != "Fixture Show" {
		t.Fatal(summary)
	}
	if summary.Mapping != "episode" || summary.Season != 1 || summary.Quality != "1080" || !summary.AutoImport || !summary.Enabled || !summary.Initialized || summary.Revision < 1 {
		t.Fatal(summary)
	}
	if summary.Counts.Entries != 2 || summary.Counts.Queued != 1 || summary.Counts.Downloaded != 1 || summary.Counts.Missing != 1 || summary.Counts.Downloading != 0 || summary.Counts.AwaitingReview != 0 || summary.Counts.Completed != 0 {
		t.Fatal("counts are wrong", summary.Counts)
	}
	if _, e = s.db.Exec(`UPDATE youtube_jobs SET state='downloading',progress=0.5,downloaded_bytes=10,total_bytes=20 WHERE series_source_id=?`, source.ID); e != nil {
		t.Fatal(e)
	}
	out, e = s.SeriesSourceSummaries(ctx)
	if e != nil || out[0].Counts.Downloading != 1 || out[0].Counts.Queued != 0 {
		t.Fatal(out, e)
	}
	if _, e = s.db.Exec(`UPDATE youtube_jobs SET state='completed' WHERE series_source_id=?`, source.ID); e != nil {
		t.Fatal(e)
	}
	out, e = s.SeriesSourceSummaries(ctx)
	if e != nil || out[0].Counts.Completed != 1 || out[0].Counts.Downloading != 0 {
		t.Fatal(out, e)
	}
}

func TestSeriesEpisodeStatusesDeriveState(t *testing.T) {
	ctx := context.Background()
	s, inspector, mediaID := seriesFixture(t)
	source, e := s.SaveSeriesSource(ctx, mediaID, SeriesSourceInput{URL: inspector.result.Source.URL, Mapping: "episode", Season: 1, StartEpisode: 1, Quality: "best", Interval: 6, AutoImport: true, Reviewed: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.RequestSeriesCheck(ctx, mediaID, source.ID, source.Revision); e != nil {
		t.Fatal(e)
	}
	if worked, e := s.ProcessSeriesSource(ctx, inspector); e != nil || !worked {
		t.Fatal(worked, e)
	}
	sources, e := s.SeriesSources(ctx, mediaID)
	if e != nil || len(sources) != 1 {
		t.Fatal(sources, e)
	}
	source = sources[0]
	source, e = enableSeriesSource(ctx, s, mediaID, SeriesSourceInput{URL: inspector.result.Source.URL, Mapping: "episode", Season: 1, StartEpisode: 1, Quality: "best", Interval: 6, AutoImport: true, Revision: source.Revision, Reviewed: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.RequestSeriesCheck(ctx, mediaID, source.ID, source.Revision); e != nil {
		t.Fatal(e)
	}
	if worked, e := s.ProcessSeriesSource(ctx, inspector); e != nil || !worked {
		t.Fatal(worked, e)
	}
	sources, e = s.SeriesSources(ctx, mediaID)
	if e != nil || len(sources) != 1 {
		t.Fatal(sources, e)
	}
	source = sources[0]
	if e = s.BackfillSeriesSource(ctx, mediaID, source.ID, source.Revision); e != nil {
		t.Fatal(e)
	}
	if worked, e := s.ProcessSeriesSource(ctx, inspector); e != nil || !worked {
		t.Fatal(worked, e)
	}
	// Episode 3 is unmonitored and not bound; bind it through a match and a
	// failed job to exercise the failed state.
	if _, e = s.db.Exec(`INSERT INTO youtube_series_matches(source_id,video_id,episode_id,position,title,channel,created_at) VALUES(?,'eeeeeeeeeee',3,0,'X3','Fixture',unixepoch())`, source.ID); e != nil {
		t.Fatal(e)
	}
	review, e := s.Preview(ctx, Video{ID: "eeeeeeeeeee", Title: "X3", Channel: "Fixture"}, Target{Kind: "episode", MediaID: mediaID, EpisodeID: 3})
	if e != nil {
		t.Fatal(e)
	}
	job, e := s.Queue(ctx, review.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(`UPDATE youtube_jobs SET state='failed',message='Download failed',series_source_id=? WHERE id=?`, source.ID, job.ID); e != nil {
		t.Fatal(e)
	}
	var active string
	if e = s.db.QueryRowContext(ctx, `SELECT id FROM youtube_jobs WHERE series_source_id=? AND episode_id=2`, source.ID).Scan(&active); e != nil {
		t.Fatal(e)
	}
	items, e := s.SeriesEpisodeStatuses(ctx, source.ID, 500)
	if e != nil || len(items) != 3 {
		t.Fatal(items, e)
	}
	if items[0].EpisodeID != 1 || items[1].EpisodeID != 2 || items[2].EpisodeID != 3 {
		t.Fatal("episodes must be ordered by season and episode", items)
	}
	if items[0].State != "downloaded" || items[0].FileCount != 1 || items[0].EnglishSubtitles != nil {
		t.Fatal("an episode with a file is downloaded and unchecked", items[0])
	}
	if items[1].State != "queued" || items[1].EnglishSubtitles != nil || !items[1].Monitored || items[1].FileCount != 0 {
		t.Fatal("an active job derives the job state", items[1])
	}
	if len(items[1].Videos) != 1 || items[1].Videos[0].VideoID != "bbbbbbbbbbb" {
		t.Fatal("bound videos must be listed", items[1].Videos)
	}
	if items[2].State != "failed" || items[2].Note != "Download failed" || items[2].Monitored {
		t.Fatal("the latest failed job derives failed", items[2])
	}
	if _, e = s.db.Exec(`UPDATE youtube_jobs SET state='awaiting_review' WHERE id=?`, active); e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec(`INSERT INTO youtube_subtitle_sources(job_id,language,source_path,size_bytes,source_mtime,sha256) VALUES(?,'ja','p',10,1,'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')`, active); e != nil {
		t.Fatal(e)
	}
	items, e = s.SeriesEpisodeStatuses(ctx, source.ID, 500)
	if e != nil {
		t.Fatal(e)
	}
	if items[1].State != "awaiting_review" || items[1].EnglishSubtitles == nil || *items[1].EnglishSubtitles {
		t.Fatal("captions without an English track are a checked negative", items[1])
	}
	if _, e = s.db.Exec(`INSERT INTO youtube_subtitle_sources(job_id,language,source_path,size_bytes,source_mtime,sha256) VALUES(?,'en','p',10,1,'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb')`, active); e != nil {
		t.Fatal(e)
	}
	items, e = s.SeriesEpisodeStatuses(ctx, source.ID, 500)
	if e != nil {
		t.Fatal(e)
	}
	if items[1].EnglishSubtitles == nil || !*items[1].EnglishSubtitles {
		t.Fatal("an English evidence row must report true", items[1])
	}
}

func TestSeriesEpisodeBatchQueue(t *testing.T) {
	ctx := context.Background()
	s, inspector, mediaID := seriesFixture(t)
	source, e := s.SaveSeriesSource(ctx, mediaID, SeriesSourceInput{URL: inspector.result.Source.URL, Mapping: "episode", Season: 1, StartEpisode: 1, Quality: "best", Interval: 6, Subtitles: DownloadOptions{Languages: []string{"en"}}, Reviewed: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.RequestSeriesCheck(ctx, mediaID, source.ID, source.Revision); e != nil {
		t.Fatal(e)
	}
	if worked, e := s.ProcessSeriesSource(ctx, inspector); e != nil || !worked {
		t.Fatal(worked, e)
	}
	sources, e := s.SeriesSources(ctx, mediaID)
	if e != nil || len(sources) != 1 {
		t.Fatal(sources, e)
	}
	source = sources[0]
	if _, e = s.QueueSeriesEpisodes(ctx, source.ID, source.Revision+1, true, []SeriesEpisodeSelection{{VideoID: "bbbbbbbbbbb", EpisodeID: 2}}); !errors.Is(e, ErrChanged) {
		t.Fatal("stale revision accepted", e)
	}
	if _, e = s.QueueSeriesEpisodes(ctx, source.ID, source.Revision, false, []SeriesEpisodeSelection{{VideoID: "bbbbbbbbbbb", EpisodeID: 2}}); !errors.Is(e, ErrInput) {
		t.Fatal("unreviewed batch accepted", e)
	}
	if _, e = s.QueueSeriesEpisodes(ctx, source.ID, source.Revision, true, nil); !errors.Is(e, ErrInput) {
		t.Fatal("empty batch accepted", e)
	}
	tooMany := make([]SeriesEpisodeSelection, 0, 26)
	for n := 0; n < 26; n++ {
		tooMany = append(tooMany, SeriesEpisodeSelection{VideoID: fmt.Sprintf("%08dzzz", n), EpisodeID: 2})
	}
	if _, e = s.QueueSeriesEpisodes(ctx, source.ID, source.Revision, true, tooMany); !errors.Is(e, ErrInput) {
		t.Fatal("more than 25 selections accepted", e)
	}
	if _, e = s.QueueSeriesEpisodes(ctx, source.ID, source.Revision, true, []SeriesEpisodeSelection{{VideoID: "bbbbbbbbbbb", EpisodeID: 2}, {VideoID: "bbbbbbbbbbb", EpisodeID: 2}}); !errors.Is(e, ErrInput) {
		t.Fatal("duplicate video accepted", e)
	}
	// The batch is atomic: episode 1 already has a file, so the whole batch
	// must roll back and episode 2 must not be queued either.
	if _, e = s.QueueSeriesEpisodes(ctx, source.ID, source.Revision, true, []SeriesEpisodeSelection{{VideoID: "bbbbbbbbbbb", EpisodeID: 2}, {VideoID: "aaaaaaaaaaa", EpisodeID: 1}}); !errors.Is(e, ErrBusy) {
		t.Fatal("a filed episode must refuse the automatic pick", e)
	}
	jobs, e := s.Jobs(ctx)
	if e != nil || len(jobs) != 0 {
		t.Fatal("a failed batch left jobs behind", jobs, e)
	}
	out, e := s.QueueSeriesEpisodes(ctx, source.ID, source.Revision, true, []SeriesEpisodeSelection{{VideoID: "bbbbbbbbbbb", EpisodeID: 2}})
	if e != nil || len(out) != 1 || out[0].VideoID != "bbbbbbbbbbb" || out[0].EpisodeID != 2 || out[0].JobID == "" || out[0].State != "queued" {
		t.Fatal(out, e)
	}
	if _, e = s.QueueSeriesEpisodes(ctx, source.ID, source.Revision, true, []SeriesEpisodeSelection{{VideoID: "bbbbbbbbbbb", EpisodeID: 2}}); !errors.Is(e, ErrBusy) {
		t.Fatal("a duplicate selection was not refused", e)
	}
	jobs, e = s.Jobs(ctx)
	if e != nil || len(jobs) != 1 || jobs[0].Target.EpisodeID != 2 {
		t.Fatal(jobs, e)
	}
	if _, e = s.QueueSeriesEpisodes(ctx, source.ID, source.Revision, true, []SeriesEpisodeSelection{{VideoID: "aaaaaaaaaaa", EpisodeID: 2}}); !errors.Is(e, ErrInput) {
		t.Fatal("a selection contradicting the mapped episode was accepted", e)
	}
	if _, e = s.QueueSeriesEpisodes(ctx, source.ID, source.Revision, true, []SeriesEpisodeSelection{{VideoID: "nnnnnnnnnnn", EpisodeID: 2}}); !errors.Is(e, ErrInput) {
		t.Fatal("an automatic selection without an entry was accepted", e)
	}
	entries, e := s.SeriesEntries(ctx, source.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		if entry.VideoID == "bbbbbbbbbbb" && (entry.State != "queued" || entry.EpisodeID != 2) {
			t.Fatal("the queued entry was not updated", entry)
		}
	}

	// A manual source resolves through its bindings; a missing binding may be
	// supplied in the selection and is persisted with the ownership check.
	manual, e := s.SaveSeriesSource(ctx, mediaID, SeriesSourceInput{URL: "https://www.youtube.com/playlist?list=PLzzzzzzzzzz", Mapping: "manual", Season: 1, StartEpisode: 1, Quality: "best", Interval: 6, Subtitles: DownloadOptions{Languages: []string{"en"}}, Reviewed: true})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.QueueSeriesEpisodes(ctx, manual.ID, manual.Revision, true, []SeriesEpisodeSelection{{VideoID: "yyyyyyyyy01"}}); !errors.Is(e, ErrInput) {
		t.Fatal("an unbound manual selection without an episode was accepted", e)
	}
	if _, e = s.QueueSeriesEpisodes(ctx, manual.ID, manual.Revision, true, []SeriesEpisodeSelection{{VideoID: "yyyyyyyyy01", EpisodeID: 999}}); !errors.Is(e, ErrInput) {
		t.Fatal("a foreign episode was accepted", e)
	}
	out, e = s.QueueSeriesEpisodes(ctx, manual.ID, manual.Revision, true, []SeriesEpisodeSelection{{VideoID: "yyyyyyyyy01", EpisodeID: 3}})
	if e != nil || len(out) != 1 || out[0].EpisodeID != 3 {
		t.Fatal("an explicit pick of an unmonitored episode must queue", out, e)
	}
	matches, e := s.SeriesMatches(ctx, manual.ID)
	if e != nil || len(matches) != 1 || matches[0].VideoID != "yyyyyyyyy01" || matches[0].EpisodeID != 3 {
		t.Fatal("the supplied binding was not persisted", matches, e)
	}
	if _, e = s.QueueSeriesEpisodes(ctx, manual.ID, manual.Revision, true, []SeriesEpisodeSelection{{VideoID: "yyyyyyyyy01", EpisodeID: 2}}); !errors.Is(e, ErrInput) {
		t.Fatal("a selection contradicting the saved binding was accepted", e)
	}
	var languages string
	if e = s.db.QueryRowContext(ctx, `SELECT o.languages_json FROM youtube_subtitle_options o JOIN youtube_jobs j ON j.review_id=o.review_id WHERE j.id=?`, out[0].JobID).Scan(&languages); e != nil {
		t.Fatal(e)
	}
	if languages != `["en"]` {
		t.Fatal("the source's subtitle options must be carried over", languages)
	}
	entries, _ = s.SeriesEntries(ctx, manual.ID)
	found := false
	for _, entry := range entries {
		if entry.VideoID == "yyyyyyyyy01" && entry.State == "queued" && entry.JobID != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("the manual pick did not record an entry", entries)
	}
}
