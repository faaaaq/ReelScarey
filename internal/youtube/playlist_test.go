package youtube

import (
	"context"
	"fmt"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func playlistFixture(t *testing.T) (*Store, *seriesInspector) {
	t.Helper()
	ctx := context.Background()
	db, e := database.Open(ctx, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	queries := []string{
		`INSERT INTO media_items(id,kind,tmdb_id,title,year,status,monitored) VALUES(1,'series',242826,'X or Go',2024,'available',0)`,
		`INSERT INTO series_details(media_id) VALUES(1)`,
		`INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored,loaded_at) VALUES(1,1,1,1,'Season 1','','',6,1,'2024-01-01')`,
	}
	for n := 1; n <= 6; n++ {
		title := fmt.Sprintf("Episode %d", n)
		if n == 6 {
			title = "Pilot"
		}
		queries = append(queries, fmt.Sprintf(`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(%d,1,%d,%d,'%s','','',1)`, n, 100+n, n, title))
	}
	queries = append(queries, `INSERT INTO media_files(id,series_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name) VALUES(1,1,'TV/X or GO/Season 1/Episode 5.webm',10,'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','{}',1,'existing','Episode 5.webm')`)
	queries = append(queries, `INSERT INTO episode_files(episode_id,media_file_id) VALUES(5,1)`)
	for _, q := range queries {
		if _, e = db.Exec(q); e != nil {
			t.Fatal(q, e)
		}
	}
	url := "https://www.youtube.com/playlist?list=PLabcdefghijk"
	source, _ := NormalizeURL(url)
	inspector := &seriesInspector{result: Inspection{Source: source, Title: "X or GO Full Episodes", Channel: "Vaff angool", Entries: []Entry{
		{Kind: "video", ID: "aaaaaaaaaaa", Title: "[X or GO] Ep.1 (Full Ver.) (ENG/JPN)", Channel: "Vaff angool", DurationSeconds: 2876},
		{Kind: "video", ID: "bbbbbbbbbbb", Title: "[X or GO] Ep.2 (Full Ver.) (ENG/JPN)", Channel: "Vaff angool", DurationSeconds: 3077},
		{Kind: "video", ID: "ccccccccccc", Title: "[X or GO] clip | Ep.2 (ENG/JPN)", Channel: "Vaff angool", DurationSeconds: 632},
		{Kind: "video", ID: "ddddddddddd", Title: "[X or GO] behind the scenes | Ep.5 (ENG/JPN)", Channel: "Vaff angool", DurationSeconds: 902},
		{Kind: "video", ID: "fffffffffff", Title: "X or GO Pilot", Channel: "Vaff angool", DurationSeconds: 3000},
		{Kind: "video", ID: "eeeeeeeeeee", Title: "[Comeback Stage] Crayon Pop", Channel: "Other", DurationSeconds: 205},
	}}}
	return NewStore(db), inspector
}

func TestPlaylistPreviewIdentifiesShowAndPrefersFullEpisodes(t *testing.T) {
	ctx := context.Background()
	s, inspector := playlistFixture(t)
	candidates, e := s.IdentifySeries(ctx, "X or GO Full Episodes")
	if e != nil || len(candidates) == 0 || candidates[0].ID != 1 || candidates[0].Score < 0.8 {
		t.Fatal(candidates, e)
	}
	preview, e := s.PreviewPlaylist(ctx, inspector.result.Source.URL, 0, 1, inspector)
	if e != nil {
		t.Fatal(e)
	}
	if preview.NeedsShow || preview.Show == nil || preview.Show.ID != 1 || preview.Season != 1 {
		t.Fatal(preview)
	}
	byVideo := map[string]PlaylistEpisode{}
	for _, entry := range preview.Entries {
		byVideo[entry.VideoID] = entry
	}
	if !byVideo["aaaaaaaaaaa"].Selected || byVideo["aaaaaaaaaaa"].Status != "missing" {
		t.Fatal(byVideo["aaaaaaaaaaa"])
	}
	if !byVideo["bbbbbbbbbbb"].Selected || byVideo["ccccccccccc"].Selected || !byVideo["ccccccccccc"].Duplicate {
		t.Fatal("full episode not preferred over the clip", byVideo["bbbbbbbbbbb"], byVideo["ccccccccccc"])
	}
	if byVideo["ddddddddddd"].Status != "available" || byVideo["ddddddddddd"].Selected {
		t.Fatal("available episode preselected", byVideo["ddddddddddd"])
	}
	if byVideo["eeeeeeeeeee"].Status != "unmatched" {
		t.Fatal(byVideo["eeeeeeeeeee"])
	}
	if byVideo["fffffffffff"].EpisodeID != 6 || byVideo["fffffffffff"].Status != "missing" || !byVideo["fffffffffff"].Selected {
		t.Fatal("episode-title fallback did not match", byVideo["fffffffffff"])
	}
}

func TestPlaylistQueueKeepsOneSourcePerPlaylist(t *testing.T) {
	ctx := context.Background()
	s, inspector := playlistFixture(t)
	url := inspector.result.Source.URL
	if _, e := s.QueuePlaylist(ctx, PlaylistQueueInput{URL: url, ShowID: 1, Season: 1, Quality: "best", Entries: []string{"aaaaaaaaaaa"}, KeepSource: true}, inspector); e != nil {
		t.Fatal(e)
	}
	if _, e := s.QueuePlaylist(ctx, PlaylistQueueInput{URL: url, ShowID: 1, Season: 1, Quality: "best", Entries: []string{"bbbbbbbbbbb"}, KeepSource: true}, inspector); e != nil {
		t.Fatal(e)
	}
	var sources int
	if e := s.db.QueryRow(`SELECT COUNT(*) FROM youtube_series_sources WHERE media_id=1`).Scan(&sources); e != nil || sources != 1 {
		t.Fatal("duplicate source created", sources, e)
	}
}

func TestPlaylistQueueQueuesSelectedMissingAndRequiresReplacementConsent(t *testing.T) {
	ctx := context.Background()
	s, inspector := playlistFixture(t)
	url := inspector.result.Source.URL
	result, e := s.QueuePlaylist(ctx, PlaylistQueueInput{URL: url, ShowID: 1, Season: 1, Quality: "1080", Subtitles: DownloadOptions{Languages: []string{"en"}}, Entries: []string{"aaaaaaaaaaa", "bbbbbbbbbbb"}}, inspector)
	if e != nil || len(result) != 2 || result[0].State != "queued" || result[1].State != "queued" {
		t.Fatal(result, e)
	}
	jobs, e := s.Jobs(ctx)
	if e != nil || len(jobs) != 2 {
		t.Fatal(jobs, e)
	}
	for _, job := range jobs {
		if !job.AutoImport || job.Quality != "1080" || job.Target.Kind != "episode" {
			t.Fatal(job)
		}
		var languages string
		if e = s.db.QueryRow(`SELECT o.languages_json FROM youtube_jobs j JOIN youtube_subtitle_options o ON o.review_id=j.review_id WHERE j.id=?`, job.ID).Scan(&languages); e != nil || languages != `["en"]` {
			t.Fatal("languages column is not a language array", languages, e)
		}
	}
	available, e := s.QueuePlaylist(ctx, PlaylistQueueInput{URL: url, ShowID: 1, Season: 1, Quality: "best", Entries: []string{"ddddddddddd"}}, inspector)
	if e != nil || len(available) != 1 || available[0].State != "skipped" {
		t.Fatal("existing episode queued without consent", available, e)
	}
	replacement, e := s.QueuePlaylist(ctx, PlaylistQueueInput{URL: url, ShowID: 1, Season: 1, Quality: "best", Entries: []string{"ddddddddddd"}, Replace: true}, inspector)
	if e != nil || len(replacement) != 1 || replacement[0].State != "queued" {
		t.Fatal(replacement, e)
	}
	var reservations int
	if e = s.db.QueryRow(`SELECT COUNT(*) FROM acquisition_replacements WHERE source_kind='youtube'`).Scan(&reservations); e != nil || reservations == 0 {
		t.Fatal("replacement reservation missing", reservations, e)
	}
}
