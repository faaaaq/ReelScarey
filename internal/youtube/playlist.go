package youtube

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

var videoEpisodeRE = regexp.MustCompile(`(?i)\b(?:ep|episode|e)\.?\s*(\d{1,3})\b`)
var videoSeasonEpisodeRE = regexp.MustCompile(`(?i)\bS(\d{1,2})[ ._-]?E(\d{1,3})\b`)
var videoCrossRE = regexp.MustCompile(`(?i)\b(\d{1,2})x(\d{1,3})\b`)
var videoHashRE = regexp.MustCompile(`(?i)(?:^|\s)#(\d{1,3})\b`)

var titleStopWords = map[string]bool{
	"a": true, "an": true, "and": true, "the": true, "of": true, "or": true,
	"full": true, "episode": true, "episodes": true, "ep": true, "ver": true,
	"official": true, "video": true, "videos": true, "series": true,
	"show": true, "channel": true, "eng": true, "jpn": true, "sub": true,
	"subs": true, "hd": true, "4k": true,
}

type SeriesCandidate struct {
	ID     int64   `json:"id"`
	Title  string  `json:"title"`
	Year   int     `json:"year,omitempty"`
	TMDbID int64   `json:"tmdb_id"`
	Score  float64 `json:"score"`
}

type SeasonSummary struct {
	Number   int  `json:"number"`
	Episodes int  `json:"episode_count"`
	Loaded   bool `json:"loaded"`
}

type PlaylistEpisode struct {
	VideoID      string `json:"video_id"`
	Title        string `json:"title"`
	Channel      string `json:"channel"`
	Duration     int64  `json:"duration_seconds"`
	Thumbnail    string `json:"thumbnail"`
	Season       int    `json:"season_number"`
	Episode      int    `json:"episode_number"`
	EpisodeID    int64  `json:"episode_id,omitempty"`
	EpisodeTitle string `json:"episode_title,omitempty"`
	Status       string `json:"status"`
	Note         string `json:"note"`
	Selected     bool   `json:"selected_default"`
	Duplicate    bool   `json:"duplicate"`
}

type PlaylistPreview struct {
	URL        string            `json:"source_url"`
	Title      string            `json:"title"`
	Channel    string            `json:"channel"`
	Truncated  bool              `json:"truncated"`
	NeedsShow  bool              `json:"needs_show"`
	Show       *SeriesCandidate  `json:"show,omitempty"`
	Candidates []SeriesCandidate `json:"candidates"`
	Season     int               `json:"season_number"`
	Seasons    []SeasonSummary   `json:"seasons"`
	Entries    []PlaylistEpisode `json:"entries"`
}

type PlaylistQueueInput struct {
	URL        string          `json:"source_url"`
	ShowID     int64           `json:"show_id"`
	Season     int             `json:"season_number"`
	Quality    string          `json:"quality"`
	Subtitles  DownloadOptions `json:"subtitles"`
	Entries    []string        `json:"entries"`
	Replace    bool            `json:"replace_existing"`
	KeepSource bool            `json:"keep_source"`
}

type PlaylistQueueResult struct {
	VideoID   string `json:"video_id"`
	EpisodeID int64  `json:"episode_id"`
	JobID     string `json:"job_id,omitempty"`
	State     string `json:"state"`
	Message   string `json:"message"`
}

func titleTokens(value string) []string {
	value = strings.ToLower(strings.ToValidUTF8(value, ""))
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if titleStopWords[f] || len(f) < 1 {
			continue
		}
		out = append(out, f)
	}
	return out
}

func tokenScore(left, right []string) float64 {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	seen := map[string]bool{}
	for _, v := range left {
		seen[v] = true
	}
	overlap := 0
	for _, v := range right {
		if seen[v] {
			overlap++
		}
	}
	min := len(left)
	if len(right) < min {
		min = len(right)
	}
	if min == 0 {
		return 0
	}
	return float64(overlap) / float64(min)
}

// IdentifySeries ranks saved shows against a playlist title. It never adds a
// show; the caller must confirm the chosen identity.
func (s *Store) IdentifySeries(ctx context.Context, title string) ([]SeriesCandidate, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT id,title,COALESCE(year,0),tmdb_id FROM media_items WHERE kind='series' ORDER BY title COLLATE NOCASE LIMIT 5000`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	want := titleTokens(title)
	out := []SeriesCandidate{}
	for rows.Next() {
		var c SeriesCandidate
		if e = rows.Scan(&c.ID, &c.Title, &c.Year, &c.TMDbID); e != nil {
			return nil, e
		}
		score := tokenScore(want, titleTokens(c.Title))
		if score >= 0.6 {
			c.Score = score
			out = append(out, c)
		}
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Title < out[j].Title
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > 5 {
		out = out[:5]
	}
	return out, nil
}

func (s *Store) seriesSeasons(ctx context.Context, mediaID int64) ([]SeasonSummary, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT number,episode_count,(loaded_at<>'') FROM seasons WHERE series_id=? ORDER BY number`, mediaID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []SeasonSummary{}
	for rows.Next() {
		var v SeasonSummary
		if e = rows.Scan(&v.Number, &v.Episodes, &v.Loaded); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type episodeRow struct {
	ID        int64
	Number    int
	Title     string
	Monitored bool
	Files     int
	Active    int
}

func (s *Store) seasonEpisodes(ctx context.Context, mediaID int64, season int) ([]episodeRow, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT e.id,e.number,e.title,(e.monitored=1 AND s.monitored=1 AND m.monitored=1),(SELECT COUNT(*) FROM episode_files f WHERE f.episode_id=e.id),(SELECT COUNT(*) FROM episode_downloads d WHERE d.episode_id=e.id)+(SELECT COUNT(*) FROM youtube_jobs y WHERE y.episode_id=e.id AND y.state NOT IN ('completed','cancelled','failed')) FROM episodes e JOIN seasons s ON s.id=e.season_id JOIN media_items m ON m.id=s.series_id WHERE s.series_id=? AND s.number=? ORDER BY e.number`, mediaID, season)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []episodeRow{}
	for rows.Next() {
		var v episodeRow
		if e = rows.Scan(&v.ID, &v.Number, &v.Title, &v.Monitored, &v.Files, &v.Active); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func parseVideoEpisode(title string) (season, number int, ok bool) {
	if m := videoSeasonEpisodeRE.FindStringSubmatch(title); len(m) == 3 {
		season, _ = atoi(m[1])
		number, _ = atoi(m[2])
		return season, number, true
	}
	if m := videoCrossRE.FindStringSubmatch(title); len(m) == 3 {
		season, _ = atoi(m[1])
		number, _ = atoi(m[2])
		return season, number, true
	}
	if m := videoEpisodeRE.FindStringSubmatch(title); len(m) == 2 {
		number, _ = atoi(m[1])
		return 0, number, true
	}
	if m := videoHashRE.FindStringSubmatch(title); len(m) == 2 {
		number, _ = atoi(m[1])
		return 0, number, true
	}
	return 0, 0, false
}

func atoi(value string) (int, bool) {
	n := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, value != ""
}

func matchEpisodeByTitle(title string, episodes []episodeRow) *episodeRow {
	want := titleTokens(title)
	if len(want) == 0 {
		return nil
	}
	var best *episodeRow
	bestScore := 0.0
	for i := range episodes {
		if episodes[i].Title == "" {
			continue
		}
		score := tokenScore(want, titleTokens(episodes[i].Title))
		if score > bestScore {
			best, bestScore = &episodes[i], score
		}
	}
	if bestScore >= 0.8 {
		return best
	}
	return nil
}

func (s *Store) PreviewPlaylist(ctx context.Context, rawURL string, showID int64, season int, inspector SourceInspector) (PlaylistPreview, error) {
	out := PlaylistPreview{Entries: []PlaylistEpisode{}, Candidates: []SeriesCandidate{}, Seasons: []SeasonSummary{}}
	source, e := NormalizeURL(rawURL)
	if e != nil || source.Kind != "playlist" {
		return out, ErrInvalidURL
	}
	if inspector == nil {
		return out, ErrInspection
	}
	work, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	inspection, e := inspector.Inspect(work, source.URL)
	if e != nil {
		return out, e
	}
	if inspection.Source != source {
		return out, ErrInspection
	}
	out.URL, out.Title, out.Channel, out.Truncated = source.URL, inspection.Title, inspection.Channel, inspection.Truncated

	showID, candidates, show, e := s.resolveSeries(work, inspection.Title, showID)
	if e != nil {
		return out, e
	}
	out.Candidates = candidates
	if showID == 0 {
		out.NeedsShow = true
		return out, nil
	}
	out.Show = show
	seasons, e := s.seriesSeasons(work, showID)
	if e != nil {
		return out, e
	}
	out.Seasons = seasons
	if season < 1 {
		loaded := []SeasonSummary{}
		for _, v := range seasons {
			if v.Loaded {
				loaded = append(loaded, v)
			}
		}
		if len(loaded) == 1 {
			season = loaded[0].Number
		}
	}
	out.Season = season
	if season < 1 {
		for _, entry := range inspection.Entries {
			if entry.Kind != "video" {
				continue
			}
			s, n, _ := parseVideoEpisode(entry.Title)
			out.Entries = append(out.Entries, PlaylistEpisode{VideoID: entry.ID, Title: entry.Title, Channel: entry.Channel, Duration: entry.DurationSeconds, Thumbnail: entry.Thumbnail, Season: s, Episode: n, Status: "unmatched", Note: "Choose a season first."})
		}
		return out, nil
	}
	episodes, e := s.seasonEpisodes(work, showID, season)
	if e != nil {
		return out, e
	}
	byNumber := map[int]*episodeRow{}
	for i := range episodes {
		byNumber[episodes[i].Number] = &episodes[i]
	}
	groups := map[int64][]PlaylistEpisode{}
	for _, entry := range inspection.Entries {
		if entry.Kind != "video" || !storeVideoID.MatchString(entry.ID) || !textOK(entry.Title, 500) {
			continue
		}
		pe := PlaylistEpisode{VideoID: entry.ID, Title: entry.Title, Channel: entry.Channel, Duration: entry.DurationSeconds, Thumbnail: entry.Thumbnail}
		s, n, ok := parseVideoEpisode(entry.Title)
		pe.Season, pe.Episode = s, n
		if !ok {
			if match := matchEpisodeByTitle(entry.Title, episodes); match != nil {
				pe.EpisodeID, pe.EpisodeTitle, pe.Episode = match.ID, match.Title, match.Number
				pe.Season, pe.Status = season, statusForEpisode(match)
			} else {
				pe.Status, pe.Note = "unmatched", "No episode number or matching episode title."
				out.Entries = append(out.Entries, pe)
				continue
			}
		} else {
			if s > 0 && s != season {
				pe.Status, pe.Note = "unmatched", fmt.Sprintf("Video says season %d, not %d.", s, season)
				out.Entries = append(out.Entries, pe)
				continue
			}
			ep := byNumber[n]
			if ep == nil {
				pe.Status, pe.Note = "unmatched", fmt.Sprintf("Episode %d is not loaded for this season.", n)
				out.Entries = append(out.Entries, pe)
				continue
			}
			pe.EpisodeID, pe.EpisodeTitle, pe.Episode = ep.ID, ep.Title, ep.Number
			pe.Season, pe.Status = season, statusForEpisode(ep)
		}
		groups[pe.EpisodeID] = append(groups[pe.EpisodeID], pe)
	}
	for episodeID, items := range groups {
		preferred := items[0]
		for _, v := range items[1:] {
			if v.Duration > preferred.Duration {
				preferred = v
			}
		}
		for _, v := range items {
			v.Duplicate = v.VideoID != preferred.VideoID
			if v.Duplicate {
				v.Note = "Another selected video maps to this episode; the longest is preselected."
			}
			v.Selected = !v.Duplicate && v.Status == "missing"
			out.Entries = append(out.Entries, v)
		}
		_ = episodeID
	}
	sort.SliceStable(out.Entries, func(i, j int) bool {
		if out.Entries[i].Episode != out.Entries[j].Episode {
			return out.Entries[i].Episode < out.Entries[j].Episode
		}
		return out.Entries[i].Duration > out.Entries[j].Duration
	})
	return out, nil
}

func statusForEpisode(e *episodeRow) string {
	if e.Files > 0 {
		return "available"
	}
	if e.Active > 0 {
		return "active"
	}
	return "missing"
}

func (s *Store) resolveSeries(ctx context.Context, title string, showID int64) (int64, []SeriesCandidate, *SeriesCandidate, error) {
	candidates, e := s.IdentifySeries(ctx, title)
	if e != nil {
		return 0, nil, nil, e
	}
	if showID > 0 {
		for _, c := range candidates {
			if c.ID == showID {
				return showID, candidates, &c, nil
			}
		}
		var c SeriesCandidate
		e = s.db.QueryRowContext(ctx, `SELECT id,title,COALESCE(year,0),tmdb_id FROM media_items WHERE id=? AND kind='series'`, showID).Scan(&c.ID, &c.Title, &c.Year, &c.TMDbID)
		if e != nil {
			return 0, candidates, nil, ErrChanged
		}
		c.Score = 1
		return showID, candidates, &c, nil
	}
	if len(candidates) > 0 && candidates[0].Score >= 0.8 {
		return candidates[0].ID, candidates, &candidates[0], nil
	}
	return 0, candidates, nil, nil
}

func (s *Store) QueuePlaylist(ctx context.Context, in PlaylistQueueInput, inspector SourceInspector) ([]PlaylistQueueResult, error) {
	quality, ok := normalizedQuality(in.Quality)
	if !ok || in.ShowID < 1 || in.Season < 1 || len(in.Entries) < 1 || len(in.Entries) > 50 {
		return nil, ErrInput
	}
	in.Subtitles.Quality = quality
	if !validSubtitleOptions(in.Subtitles) {
		return nil, ErrInput
	}
	source, e := NormalizeURL(in.URL)
	if e != nil || source.Kind != "playlist" {
		return nil, ErrInvalidURL
	}
	if inspector == nil {
		return nil, ErrInspection
	}
	work, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	inspection, e := inspector.Inspect(work, source.URL)
	if e != nil {
		return nil, e
	}
	if inspection.Source != source {
		return nil, ErrInspection
	}
	episodes, e := s.seasonEpisodes(work, in.ShowID, in.Season)
	if e != nil {
		return nil, e
	}
	byNumber := map[int]*episodeRow{}
	for i := range episodes {
		byNumber[episodes[i].Number] = &episodes[i]
	}
	selected := map[string]bool{}
	for _, id := range in.Entries {
		if !storeVideoID.MatchString(id) || selected[id] {
			return nil, ErrInput
		}
		selected[id] = true
	}
	var sourceID string
	if in.KeepSource {
		if e = s.db.QueryRowContext(work, `SELECT id FROM youtube_series_sources WHERE media_id=? AND source_url=?`, in.ShowID, source.URL).Scan(&sourceID); errors.Is(e, sql.ErrNoRows) {
			saved, err := s.SaveSeriesSource(work, in.ShowID, SeriesSourceInput{URL: source.URL, Mapping: "episode", Season: in.Season, StartEpisode: 1, Quality: quality, Subtitles: in.Subtitles, AutoImport: true, Enabled: false, Interval: 6, Reviewed: true})
			if err != nil {
				return nil, err
			}
			sourceID = saved.ID
		} else if e != nil {
			return nil, e
		}
	}
	results := []PlaylistQueueResult{}
	for _, entry := range inspection.Entries {
		if entry.Kind != "video" || !selected[entry.ID] {
			continue
		}
		result := PlaylistQueueResult{VideoID: entry.ID}
		season, number, ok := parseVideoEpisode(entry.Title)
		var ep *episodeRow
		if ok {
			if season > 0 && season != in.Season {
				result.State, result.Message = "skipped", "The video names a different season."
				results = append(results, result)
				continue
			}
			ep = byNumber[number]
			if ep == nil {
				result.State, result.Message = "skipped", "That episode is not loaded."
				results = append(results, result)
				continue
			}
		} else {
			ep = matchEpisodeByTitle(entry.Title, episodes)
			if ep == nil {
				result.State, result.Message = "skipped", "No episode number or matching episode title."
				results = append(results, result)
				continue
			}
		}
		result.EpisodeID = ep.ID
		if ep.Files > 0 && !in.Replace {
			result.State, result.Message = "skipped", "Episode already has a file."
			results = append(results, result)
			continue
		}
		if ep.Active > 0 {
			result.State, result.Message = "skipped", "Another download is active for this episode."
			results = append(results, result)
			continue
		}
		job, err := s.queueExplicit(work, in.ShowID, in.Season, ep, Video{ID: entry.ID, Title: entry.Title, Channel: entry.Channel}, quality, in.Subtitles, sourceID)
		if err != nil {
			result.State = "skipped"
			result.Message = "Could not queue this episode."
			if errors.Is(err, ErrBusy) {
				result.Message = "Episode is no longer available to queue."
			}
			results = append(results, result)
			continue
		}
		result.JobID, result.State, result.Message = job.ID, "queued", "Download queued."
		results = append(results, result)
	}
	return results, nil
}

func (s *Store) queueExplicit(ctx context.Context, mediaID int64, season int, ep *episodeRow, video Video, quality string, subtitles DownloadOptions, sourceID string) (Job, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return Job{}, e
	}
	defer tx.Rollback()
	var target SeasonSummary
	if e = tx.QueryRowContext(ctx, `SELECT number,episode_count,(loaded_at<>'') FROM seasons WHERE series_id=? AND number=?`, mediaID, season).Scan(&target.Number, &target.Episodes, &target.Loaded); e != nil {
		return Job{}, ErrChanged
	}
	if !target.Loaded {
		return Job{}, ErrChanged
	}
	snap, label, e := targetSnapshot(ctx, tx, Target{Kind: "episode", MediaID: mediaID, EpisodeID: ep.ID})
	if e != nil {
		return Job{}, e
	}
	key, e := id()
	if e != nil {
		return Job{}, e
	}
	now := time.Now().Unix()
	if _, e = tx.ExecContext(ctx, `INSERT INTO youtube_reviews(id,video_id,video_title,channel,target_kind,media_id,episode_id,target_snapshot,expires_at) VALUES(?,?,?,?,?,?,?,?,?)`, key, video.ID, video.Title, video.Channel, "episode", mediaID, ep.ID, snap, now+1800); e != nil {
		return Job{}, e
	}
	options, _ := json.Marshal(subtitles)
	if _, e = tx.ExecContext(ctx, `INSERT INTO youtube_subtitle_options(review_id,languages_json,automatic,quality) VALUES(?,?,?,?)`, key, string(options), subtitles.Automatic, quality); e != nil {
		return Job{}, e
	}
	job, e := s.queueTx(ctx, tx, key)
	if e != nil {
		return Job{}, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE youtube_jobs SET auto_import=1,series_source_id=? WHERE id=?`, sourceID, job.ID); e != nil {
		return Job{}, e
	}
	_ = label
	if e = tx.Commit(); e != nil {
		return Job{}, e
	}
	return job, nil
}
