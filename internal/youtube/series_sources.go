package youtube

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var episodeNumberRE = regexp.MustCompile(`(?i)\bS(\d{1,2})[ ._-]?E(\d{1,3})\b`)
var episodeCrossRE = regexp.MustCompile(`(?i)\b(\d{1,2})x(\d{1,3})\b`)

type SeriesSource struct {
	ID           string          `json:"id"`
	MediaID      int64           `json:"media_id"`
	URL          string          `json:"source_url"`
	Title        string          `json:"title"`
	Mapping      string          `json:"mapping"`
	Season       int             `json:"season_number"`
	StartEpisode int             `json:"start_episode"`
	Quality      string          `json:"quality"`
	Subtitles    DownloadOptions `json:"subtitles"`
	AutoImport   bool            `json:"auto_import"`
	Enabled      bool            `json:"enabled"`
	Interval     int             `json:"interval_hours"`
	Initialized  bool            `json:"initialized"`
	Revision     int             `json:"revision"`
	Requested    bool            `json:"requested"`
	NextRun      int64           `json:"next_run"`
	LastChecked  int64           `json:"last_checked"`
	State        string          `json:"state"`
	Message      string          `json:"message"`
	Created      int64           `json:"created_at"`
	snapshot     string
	owner        string
}

type SeriesSourceInput struct {
	URL          string          `json:"source_url"`
	Mapping      string          `json:"mapping"`
	Season       int             `json:"season_number"`
	StartEpisode int             `json:"start_episode"`
	Quality      string          `json:"quality"`
	Subtitles    DownloadOptions `json:"subtitles"`
	AutoImport   bool            `json:"auto_import"`
	Enabled      bool            `json:"enabled"`
	Interval     int             `json:"interval_hours"`
	Revision     int             `json:"revision"`
	Reviewed     bool            `json:"reviewed"`
}

type SeriesEntry struct {
	VideoID    string `json:"video_id"`
	Title      string `json:"title"`
	Channel    string `json:"channel"`
	Position   int    `json:"position"`
	State      string `json:"state"`
	EpisodeID  int64  `json:"episode_id,omitempty"`
	JobID      string `json:"job_id,omitempty"`
	Note       string `json:"note"`
	Discovered int64  `json:"discovered_at"`
}

type SeriesPreviewEntry struct {
	Video     Video  `json:"video"`
	Position  int    `json:"position"`
	Season    int    `json:"season_number"`
	Episode   int    `json:"episode_number"`
	EpisodeID int64  `json:"episode_id,omitempty"`
	Label     string `json:"label"`
	State     string `json:"state"`
	Note      string `json:"note"`
}

type SeriesPreview struct {
	Title     string               `json:"title"`
	Channel   string               `json:"channel"`
	Truncated bool                 `json:"truncated"`
	Entries   []SeriesPreviewEntry `json:"entries"`
}

const seriesSourceColumns = `id,media_id,source_url,title,mapping,season_number,start_episode,quality,subtitles_json,auto_import,enabled,interval_hours,initialized,revision,requested,next_run,last_checked,state,message,created_at,target_snapshot,lease_owner`

func scanSeriesSource(row interface{ Scan(...any) error }) (SeriesSource, error) {
	var s SeriesSource
	var options string
	e := row.Scan(&s.ID, &s.MediaID, &s.URL, &s.Title, &s.Mapping, &s.Season, &s.StartEpisode, &s.Quality, &options, &s.AutoImport, &s.Enabled, &s.Interval, &s.Initialized, &s.Revision, &s.Requested, &s.NextRun, &s.LastChecked, &s.State, &s.Message, &s.Created, &s.snapshot, &s.owner)
	if e == nil {
		e = json.Unmarshal([]byte(options), &s.Subtitles)
	}
	if s.Quality == "" {
		s.Quality = "best"
	}
	return s, e
}

func (s *Store) SeriesSources(ctx context.Context, mediaID int64) ([]SeriesSource, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT `+seriesSourceColumns+` FROM youtube_series_sources WHERE media_id=? ORDER BY created_at,id`, mediaID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []SeriesSource{}
	for rows.Next() {
		v, e := scanSeriesSource(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) SeriesEntries(ctx context.Context, sourceID string) ([]SeriesEntry, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT video_id,title,channel,position,state,COALESCE(episode_id,0),COALESCE(job_id,''),note,discovered_at FROM youtube_series_entries WHERE source_id=? ORDER BY position,discovered_at,video_id LIMIT 500`, sourceID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []SeriesEntry{}
	for rows.Next() {
		var v SeriesEntry
		if e = rows.Scan(&v.VideoID, &v.Title, &v.Channel, &v.Position, &v.State, &v.EpisodeID, &v.JobID, &v.Note, &v.Discovered); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func seriesSnapshot(ctx context.Context, q queryer, mediaID int64) (string, string, error) {
	var title string
	var tmdb int64
	e := q.QueryRowContext(ctx, `SELECT title,tmdb_id FROM media_items WHERE id=? AND kind='series'`, mediaID).Scan(&title, &tmdb)
	if e != nil {
		return "", "", e
	}
	raw, _ := json.Marshal(struct {
		Title  string
		TMDbID int64
	}{title, tmdb})
	return string(raw), title, nil
}

func normalizedSeriesInput(in SeriesSourceInput) (SeriesSourceInput, Source, error) {
	source, e := NormalizeURL(in.URL)
	if e != nil || source.Kind == "video" {
		return in, Source{}, ErrInvalidURL
	}
	in.Mapping = strings.ToLower(strings.TrimSpace(in.Mapping))
	if in.Mapping != "episode" && in.Mapping != "order" {
		return in, Source{}, ErrInput
	}
	if in.Mapping == "order" && (source.Kind != "playlist" || in.Season < 1 || in.StartEpisode < 1) {
		return in, Source{}, ErrInput
	}
	if in.Season < 0 || in.Season > 100 || in.StartEpisode < 1 || in.StartEpisode > 5000 {
		return in, Source{}, ErrInput
	}
	quality, ok := normalizedQuality(in.Quality)
	if !ok {
		return in, Source{}, ErrInput
	}
	in.Quality = quality
	in.Subtitles.Quality = quality
	if !validSubtitleOptions(in.Subtitles) {
		return in, Source{}, ErrInput
	}
	if in.Interval < 1 || in.Interval > 168 {
		return in, Source{}, ErrInput
	}
	in.URL = source.URL
	return in, source, nil
}

func (s *Store) SaveSeriesSource(ctx context.Context, mediaID int64, in SeriesSourceInput) (SeriesSource, error) {
	in, _, e := normalizedSeriesInput(in)
	if e != nil || mediaID < 1 || !in.Reviewed {
		return SeriesSource{}, ErrInput
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return SeriesSource{}, e
	}
	defer tx.Rollback()
	snapshot, title, e := seriesSnapshot(ctx, tx, mediaID)
	if e != nil {
		return SeriesSource{}, ErrChanged
	}
	options, _ := json.Marshal(in.Subtitles)
	now := time.Now().Unix()
	var key string
	if in.Revision == 0 {
		// New schedules start paused. Enabling is a separate, explicit action.
		key, e = id()
		if e != nil {
			return SeriesSource{}, e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO youtube_series_sources(id,media_id,source_url,title,mapping,season_number,start_episode,quality,subtitles_json,auto_import,enabled,interval_hours,created_at,target_snapshot) VALUES(?,?,?,?,?,?,?,?,?,?,0,?,?,?)`, key, mediaID, in.URL, title, in.Mapping, in.Season, in.StartEpisode, in.Quality, string(options), in.AutoImport, in.Interval, now, snapshot)
	} else {
		var old SeriesSource
		old, e = scanSeriesSource(tx.QueryRowContext(ctx, `SELECT `+seriesSourceColumns+` FROM youtube_series_sources WHERE media_id=? AND revision=?`, mediaID, in.Revision))
		if e != nil {
			return SeriesSource{}, ErrChanged
		}
		key = old.ID
		if old.Enabled && !in.Enabled {
			// disabling is always allowed
		} else if !old.Enabled && in.Enabled && !in.Reviewed {
			return SeriesSource{}, ErrInput
		}
		_, e = tx.ExecContext(ctx, `UPDATE youtube_series_sources SET title=?,mapping=?,season_number=?,start_episode=?,quality=?,subtitles_json=?,auto_import=?,enabled=?,interval_hours=?,revision=revision+1,state='idle',message='',requested=0,next_run=0,lease_owner='',lease_until=0 WHERE id=? AND revision=?`, title, in.Mapping, in.Season, in.StartEpisode, in.Quality, string(options), in.AutoImport, in.Enabled, in.Interval, key, in.Revision)
	}
	if e != nil {
		return SeriesSource{}, ErrChanged
	}
	if _, e = tx.ExecContext(ctx, `UPDATE youtube_series_sources SET target_snapshot=? WHERE id=?`, snapshot, key); e != nil {
		return SeriesSource{}, e
	}
	out, e := scanSeriesSource(tx.QueryRowContext(ctx, `SELECT `+seriesSourceColumns+` FROM youtube_series_sources WHERE id=?`, key))
	if e != nil {
		return SeriesSource{}, e
	}
	if e = tx.Commit(); e != nil {
		return SeriesSource{}, e
	}
	return out, nil
}

func (s *Store) RemoveSeriesSource(ctx context.Context, mediaID int64, sourceID string, revision int) error {
	res, e := s.db.ExecContext(ctx, `DELETE FROM youtube_series_sources WHERE id=? AND media_id=? AND revision=?`, sourceID, mediaID, revision)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrChanged
	}
	return nil
}

func (s *Store) RequestSeriesCheck(ctx context.Context, mediaID int64, sourceID string, revision int) error {
	res, e := s.db.ExecContext(ctx, `UPDATE youtube_series_sources SET requested=1,next_run=0,revision=revision+1,state='idle',message='' WHERE id=? AND media_id=? AND revision=?`, sourceID, mediaID, revision)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrChanged
	}
	return nil
}

// BackfillSeriesSource forgets the initial baseline so the next check treats
// those videos as new and queues the ones that map to monitored missing
// episodes. It is an explicit action, never automatic.
func (s *Store) BackfillSeriesSource(ctx context.Context, mediaID int64, sourceID string, revision int) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	res, e := tx.ExecContext(ctx, `UPDATE youtube_series_sources SET initialized=1,requested=1,next_run=0,revision=revision+1,state='idle',message='' WHERE id=? AND media_id=? AND revision=?`, sourceID, mediaID, revision)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrChanged
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM youtube_series_entries WHERE source_id=? AND state='baseline'`, sourceID); e != nil {
		return e
	}
	return tx.Commit()
}

// PreviewSeriesSource maps the current source contents without saving or
// queueing anything. It makes the explicit mapping visible before the owner
// enables the source.
func (s *Store) PreviewSeriesSource(ctx context.Context, mediaID int64, in SeriesSourceInput, inspector SourceInspector) (SeriesPreview, error) {
	in, normalized, e := normalizedSeriesInput(in)
	if e != nil {
		return SeriesPreview{}, e
	}
	if inspector == nil {
		return SeriesPreview{}, ErrInspection
	}
	work, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	inspection, e := inspector.Inspect(work, normalized.URL)
	if e != nil {
		return SeriesPreview{}, e
	}
	if inspection.Source != normalized {
		return SeriesPreview{}, ErrInspection
	}
	out := SeriesPreview{Title: inspection.Title, Channel: inspection.Channel, Truncated: inspection.Truncated, Entries: []SeriesPreviewEntry{}}
	position := 0
	for _, entry := range inspection.Entries {
		if entry.Kind != "video" || !storeVideoID.MatchString(entry.ID) {
			continue
		}
		mapped, note, season, number, episodeID, label := s.matchPreview(work, s.db, mediaID, in, entry, position)
		out.Entries = append(out.Entries, SeriesPreviewEntry{Video: Video{ID: entry.ID, Title: entry.Title, Channel: entry.Channel}, Position: position, Season: season, Episode: number, EpisodeID: episodeID, Label: label, State: mapped, Note: note})
		position++
	}
	return out, nil
}

func (s *Store) matchPreview(ctx context.Context, q queryer, mediaID int64, in SeriesSourceInput, entry Entry, position int) (state, note string, season, number int, episodeID int64, label string) {
	season, number, ok := mapEpisodeNumber(in, entry.Title, position)
	if !ok {
		return "skipped", "No explicit episode number", season, number, 0, ""
	}
	if in.Season > 0 && season != in.Season {
		return "skipped", fmt.Sprintf("Season %d does not match %d", season, in.Season), season, number, 0, ""
	}
	var monitored, files, active int
	var epTitle string
	e := q.QueryRowContext(ctx, `SELECT e.id,e.title, (e.monitored=1 AND s.monitored=1 AND m.monitored=1), (SELECT COUNT(*) FROM episode_files f WHERE f.episode_id=e.id), (SELECT COUNT(*) FROM episode_downloads d WHERE d.episode_id=e.id)+(SELECT COUNT(*) FROM youtube_jobs y WHERE y.episode_id=e.id AND y.state NOT IN ('completed','cancelled','failed')) FROM episodes e JOIN seasons s ON s.id=e.season_id JOIN media_items m ON m.id=s.series_id WHERE s.series_id=? AND s.number=? AND e.number=?`, mediaID, season, number).Scan(&episodeID, &epTitle, &monitored, &files, &active)
	if errors.Is(e, sql.ErrNoRows) {
		return "skipped", "Episode is not loaded in the catalog", season, number, 0, ""
	}
	if e != nil {
		return "skipped", "Episode lookup failed", season, number, 0, ""
	}
	label = fmt.Sprintf("S%02dE%02d — %s", season, number, epTitle)
	if monitored != 1 {
		return "skipped", "Episode or season is not monitored", season, number, episodeID, label
	}
	if files > 0 {
		return "skipped", "Episode already has a file", season, number, episodeID, label
	}
	if active > 0 {
		return "skipped", "Another download is active", season, number, episodeID, label
	}
	return "ready", "", season, number, episodeID, label
}

func mapEpisodeNumber(in SeriesSourceInput, title string, position int) (int, int, bool) {
	if in.Mapping == "order" {
		if in.Season < 1 {
			return 0, 0, false
		}
		return in.Season, in.StartEpisode + position, true
	}
	if m := episodeNumberRE.FindStringSubmatch(title); len(m) == 3 {
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		return s, e, true
	}
	if m := episodeCrossRE.FindStringSubmatch(title); len(m) == 3 {
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		return s, e, true
	}
	return 0, 0, false
}

// RunSeriesSources advances due per-show sources in a bounded loop.
func (s *Store) RunSeriesSources(ctx context.Context, inspector SourceInspector, logger *slog.Logger) {
	for ctx.Err() == nil {
		worked, e := s.ProcessSeriesSource(ctx, inspector)
		if e != nil && ctx.Err() == nil {
			logger.Warn("YouTube series source deferred", "error", e)
		}
		if worked && e == nil {
			continue
		}
		timer := time.NewTimer(30 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *Store) ProcessSeriesSource(ctx context.Context, inspector SourceInspector) (processed bool, retErr error) {
	if inspector == nil {
		return false, nil
	}
	owner, e := id()
	if e != nil {
		return false, e
	}
	now := time.Now().Unix()
	p, e := scanSeriesSource(s.db.QueryRowContext(ctx, `UPDATE youtube_series_sources SET lease_owner=?,lease_until=?,state='checking' WHERE id=(SELECT id FROM youtube_series_sources WHERE lease_until<? AND (requested=1 OR (enabled=1 AND next_run<=?)) ORDER BY next_run,created_at,id LIMIT 1) RETURNING `+seriesSourceColumns, owner, now+120, now, now))
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	fail := func(message string) (bool, error) {
		_, _ = s.db.ExecContext(ctx, `UPDATE youtube_series_sources SET state='attention',message=?,next_run=?,requested=0,lease_owner='',lease_until=0 WHERE id=? AND revision=? AND lease_owner=?`, message, now+3600, p.ID, p.Revision, owner)
		return true, nil
	}
	work, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	inspection, e := inspector.Inspect(work, p.URL)
	if e != nil {
		return fail("Source check failed; the previous entries were preserved.")
	}
	normalized, e := NormalizeURL(p.URL)
	if e != nil || inspection.Source != normalized {
		return fail("The source URL changed; review the source.")
	}
	tx, e := s.db.BeginTx(work, nil)
	if e != nil {
		return true, e
	}
	defer tx.Rollback()
	current, _, e := seriesSnapshot(work, tx, p.MediaID)
	if e != nil || current != p.snapshot {
		return fail("This show changed; review the YouTube source.")
	}
	position := 0
	queued := 0
	for _, entry := range inspection.Entries {
		if entry.Kind != "video" || !storeVideoID.MatchString(entry.ID) || !textOK(entry.Title, 500) {
			continue
		}
		pos := position
		position++
		var existing string
		e = tx.QueryRowContext(work, `SELECT state FROM youtube_series_entries WHERE source_id=? AND video_id=?`, p.ID, entry.ID).Scan(&existing)
		if e == nil {
			if _, e = tx.ExecContext(work, `UPDATE youtube_series_entries SET title=?,channel=?,position=? WHERE source_id=? AND video_id=?`, entry.Title, entry.Channel, pos, p.ID, entry.ID); e != nil {
				return true, e
			}
			continue
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return true, e
		}
		state, note := "baseline", "Initial baseline; future videos are matched."
		var episodeID int64
		if p.Initialized && queued < 10 {
			mapped, reason, season, number, epID, label := s.matchPreview(work, tx, p.MediaID, SeriesSourceInput{Mapping: p.Mapping, Season: p.Season, StartEpisode: p.StartEpisode}, entry, pos)
			episodeID = epID
			if mapped == "ready" {
				jobID, queueErr := s.queueSeriesEntry(work, tx, p, entry, episodeID)
				if queueErr == nil {
					if _, e = tx.ExecContext(work, `INSERT INTO youtube_series_entries(source_id,video_id,position,title,channel,state,episode_id,job_id,note,discovered_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, p.ID, entry.ID, pos, entry.Title, entry.Channel, "queued", episodeID, jobID, label, now); e != nil {
						return true, e
					}
					queued++
					continue
				}
				state, note = "skipped", "Could not reserve this episode: "+queueErr.Error()
			} else {
				state, note = "skipped", reason
				if number > 0 {
					note = fmt.Sprintf("S%02dE%02d: %s", season, number, reason)
				}
			}
		}
		if _, e = tx.ExecContext(work, `INSERT INTO youtube_series_entries(source_id,video_id,position,title,channel,state,episode_id,note,discovered_at) VALUES(?,?,?,?,?,?,?,?,?)`, p.ID, entry.ID, pos, entry.Title, entry.Channel, state, nullable(episodeID), note, now); e != nil {
			return true, e
		}
	}
	message := "Source checked."
	if !p.Initialized {
		message = "Initial baseline recorded. New videos are matched from now on."
	} else if queued > 0 {
		message = fmt.Sprintf("Queued %d episode%s.", queued, map[bool]string{true: "", false: "s"}[queued == 1])
	}
	_, e = tx.ExecContext(work, `UPDATE youtube_series_sources SET initialized=1,last_checked=?,next_run=?,requested=0,state='idle',message=?,lease_owner='',lease_until=0 WHERE id=? AND revision=? AND lease_owner=?`, now, now+int64(p.Interval)*3600, message, p.ID, p.Revision, owner)
	if e != nil {
		return true, e
	}
	return true, tx.Commit()
}

func (s *Store) queueSeriesEntry(ctx context.Context, tx *sql.Tx, p SeriesSource, entry Entry, episodeID int64) (string, error) {
	var fileID int64
	if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM episode_files WHERE episode_id=?`, episodeID).Scan(&fileID); e != nil {
		return "", e
	}
	if fileID > 0 {
		return "", ErrBusy
	}
	snap, _, e := targetSnapshot(ctx, tx, Target{Kind: "episode", MediaID: p.MediaID, EpisodeID: episodeID})
	if e != nil {
		return "", e
	}
	key, e := id()
	if e != nil {
		return "", e
	}
	expiry := time.Now().Unix() + 1800
	if _, e = tx.ExecContext(ctx, `INSERT INTO youtube_reviews(id,video_id,video_title,channel,target_kind,media_id,episode_id,target_snapshot,expires_at) VALUES(?,?,?,?,?,?,?,?,?)`, key, entry.ID, entry.Title, entry.Channel, "episode", p.MediaID, episodeID, snap, expiry); e != nil {
		return "", e
	}
	options, _ := json.Marshal(p.Subtitles)
	if _, e = tx.ExecContext(ctx, `INSERT INTO youtube_subtitle_options(review_id,languages_json,automatic,quality) VALUES(?,?,?,?)`, key, string(options), p.Subtitles.Automatic, p.Quality); e != nil {
		return "", e
	}
	job, e := s.queueTx(ctx, tx, key)
	if e != nil {
		return "", e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE youtube_jobs SET auto_import=?,series_source_id=? WHERE id=?`, p.AutoImport, p.ID, job.ID); e != nil {
		return "", e
	}
	return job.ID, nil
}
