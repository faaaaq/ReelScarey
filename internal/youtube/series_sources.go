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
	"unicode"
	"unicode/utf8"
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
	if in.Mapping != "episode" && in.Mapping != "order" && in.Mapping != "manual" {
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
	e := q.QueryRowContext(ctx, `SELECT e.id FROM episodes e JOIN seasons s ON s.id=e.season_id WHERE s.series_id=? AND s.number=? AND e.number=?`, mediaID, season, number).Scan(&episodeID)
	if errors.Is(e, sql.ErrNoRows) {
		return "skipped", "Episode is not loaded in the catalog", season, number, 0, ""
	}
	if e != nil {
		return "skipped", "Episode lookup failed", season, number, 0, ""
	}
	state, note, label = episodePreview(ctx, q, mediaID, episodeID)
	return state, note, season, number, episodeID, label
}

// episodePreview reports whether a chosen episode can accept a download. It
// backs both the automatic mapping and the explicit manual assignments.
func episodePreview(ctx context.Context, q queryer, mediaID, episodeID int64) (state, note, label string) {
	var monitored, files, active, season, number int
	var epTitle string
	e := q.QueryRowContext(ctx, `SELECT e.title,s.number,e.number,(e.monitored=1 AND s.monitored=1 AND m.monitored=1),(SELECT COUNT(*) FROM episode_files f WHERE f.episode_id=e.id),(SELECT COUNT(*) FROM episode_downloads d WHERE d.episode_id=e.id)+(SELECT COUNT(*) FROM youtube_jobs y WHERE y.episode_id=e.id AND y.state NOT IN ('completed','cancelled','failed')) FROM episodes e JOIN seasons s ON s.id=e.season_id JOIN media_items m ON m.id=s.series_id WHERE e.id=? AND s.series_id=?`, episodeID, mediaID).Scan(&epTitle, &season, &number, &monitored, &files, &active)
	if errors.Is(e, sql.ErrNoRows) {
		return "skipped", "Episode is not loaded in the catalog", ""
	}
	if e != nil {
		return "skipped", "Episode lookup failed", ""
	}
	label = fmt.Sprintf("S%02dE%02d — %s", season, number, epTitle)
	if monitored != 1 {
		return "skipped", "Episode or season is not monitored", label
	}
	if files > 0 {
		return "skipped", "Episode already has a file", label
	}
	if active > 0 {
		return "skipped", "Another download is active", label
	}
	return "ready", "", label
}

// episodeManual resolves an explicit manual pick. Monitoring never blocks a
// chosen episode, and an episode that already has a file is queued through the
// existing reviewed replacement flow, where the original file is retained and
// the swap needs an Organize-library review. Only a real write conflict — an
// active transfer download or another active YouTube job for the same episode —
// is refused, because the pipeline cannot own two writes for one episode at
// once (a unique index enforces it).
func episodeManual(ctx context.Context, q queryer, mediaID, episodeID int64) (state, note, label string) {
	var season, number, downloads, active, files int
	var epTitle string
	e := q.QueryRowContext(ctx, `SELECT e.title,s.number,e.number,(SELECT COUNT(*) FROM episode_downloads d WHERE d.episode_id=e.id),(SELECT COUNT(*) FROM youtube_jobs y WHERE y.episode_id=e.id AND y.state NOT IN ('completed','cancelled','failed')),(SELECT COUNT(*) FROM episode_files f WHERE f.episode_id=e.id) FROM episodes e JOIN seasons s ON s.id=e.season_id WHERE e.id=? AND s.series_id=?`, episodeID, mediaID).Scan(&epTitle, &season, &number, &downloads, &active, &files)
	if errors.Is(e, sql.ErrNoRows) {
		return "skipped", "Episode is not loaded in the catalog", ""
	}
	if e != nil {
		return "skipped", "Episode lookup failed", ""
	}
	label = fmt.Sprintf("S%02dE%02d — %s", season, number, epTitle)
	if downloads > 0 || active > 0 {
		return "skipped", "Another download is already active for this episode.", label
	}
	if files > 0 {
		return "ready", "", label + " (replacement)"
	}
	return "ready", "", label
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
	var manual map[string]int64
	if p.Mapping == "manual" {
		manual, e = seriesMatchIndex(work, tx, p.ID)
		if e != nil {
			return true, e
		}
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
			mapped, reason, label := "", "", ""
			if p.Mapping == "manual" {
				assigned, ok := manual[entry.ID]
				if !ok {
					mapped, reason = "skipped", "No episode is assigned to this video."
				} else {
					episodeID = assigned
					mapped, reason, label = episodeManual(work, tx, p.MediaID, assigned)
				}
			} else {
				var season, number int
				var epID int64
				mapped, reason, season, number, epID, label = s.matchPreview(work, tx, p.MediaID, SeriesSourceInput{Mapping: p.Mapping, Season: p.Season, StartEpisode: p.StartEpisode}, entry, pos)
				episodeID = epID
				if mapped != "ready" && number > 0 {
					reason = fmt.Sprintf("S%02dE%02d: %s", season, number, reason)
				}
			}
			if mapped == "ready" {
				jobID, queueErr := s.queueSeriesEntry(work, tx, p, entry, episodeID, p.Mapping == "manual")
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
	res, e := tx.ExecContext(work, `UPDATE youtube_series_sources SET initialized=1,last_checked=?,next_run=?,requested=0,state='idle',message=?,lease_owner='',lease_until=0 WHERE id=? AND revision=? AND lease_owner=?`, now, now+int64(p.Interval)*3600, message, p.ID, p.Revision, owner)
	if e != nil {
		return true, e
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return true, ErrChanged
	}
	return true, tx.Commit()
}

func (s *Store) queueSeriesEntry(ctx context.Context, tx *sql.Tx, p SeriesSource, entry Entry, episodeID int64, override bool) (string, error) {
	var monitored int
	if e := tx.QueryRowContext(ctx, `SELECT (e.monitored=1 AND s.monitored=1 AND m.monitored=1) FROM episodes e JOIN seasons s ON s.id=e.season_id JOIN media_items m ON m.id=s.series_id WHERE e.id=? AND s.series_id=? AND m.kind='series'`, episodeID, p.MediaID).Scan(&monitored); e != nil {
		return "", ErrChanged
	}
	if monitored != 1 && !override {
		return "", ErrBusy
	}
	if !override {
		// Automatic mappings never queue an episode that already has a file. An
		// explicit manual pick may: targetSnapshot then records it as a
		// reviewed replacement.
		var fileID int64
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM episode_files WHERE episode_id=?`, episodeID).Scan(&fileID); e != nil {
			return "", e
		}
		if fileID > 0 {
			return "", ErrBusy
		}
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
	languages, _ := json.Marshal(p.Subtitles.Languages)
	if _, e = tx.ExecContext(ctx, `INSERT INTO youtube_subtitle_options(review_id,languages_json,automatic,quality) VALUES(?,?,?,?)`, key, string(languages), p.Subtitles.Automatic, p.Quality); e != nil {
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

// ----- Manual playlist matching -------------------------------------------
//
// A manual source carries no title or position heuristic: every video the owner
// wants must be bound to a specific episode of the show. The bindings live in
// youtube_series_matches, keyed by (source_id, video_id).

type SeriesMatch struct {
	VideoID      string `json:"video_id"`
	Title        string `json:"title"`
	Channel      string `json:"channel"`
	Position     int    `json:"position"`
	EpisodeID    int64  `json:"episode_id"`
	SeasonNumber int    `json:"season_number"`
	EpisodeNum   int    `json:"episode_number"`
	EpisodeTitle string `json:"episode_title"`
}

type SeriesMatchVideo struct {
	VideoID   string `json:"video_id"`
	Title     string `json:"title"`
	Channel   string `json:"channel"`
	Position  int    `json:"position"`
	EpisodeID int64  `json:"episode_id,omitempty"`
	Label     string `json:"label"`
	State     string `json:"state"`
	Note      string `json:"note"`
}

type SeriesEpisodeOption struct {
	ID            int64  `json:"id"`
	SeasonNumber  int    `json:"season_number"`
	EpisodeNumber int    `json:"episode_number"`
	Title         string `json:"title"`
	Label         string `json:"label"`
	Monitored     bool   `json:"monitored"`
	Files         int    `json:"files"`
	Active        int    `json:"active"`
}

type SeriesMatchPreview struct {
	Title     string                `json:"title"`
	Channel   string                `json:"channel"`
	Truncated bool                  `json:"truncated"`
	Videos    []SeriesMatchVideo    `json:"videos"`
	Episodes  []SeriesEpisodeOption `json:"episodes"`
}

type SeriesMatchInput struct {
	VideoID   string `json:"video_id"`
	EpisodeID int64  `json:"episode_id"`
	Title     string `json:"title"`
	Channel   string `json:"channel"`
}

type rowsQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (s *Store) SeriesMatches(ctx context.Context, sourceID string) ([]SeriesMatch, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT m.video_id,m.title,m.channel,m.position,m.episode_id,COALESCE(se.number,0),COALESCE(e.number,0),COALESCE(e.title,'') FROM youtube_series_matches m LEFT JOIN episodes e ON e.id=m.episode_id LEFT JOIN seasons se ON se.id=e.season_id WHERE m.source_id=? ORDER BY m.position,m.video_id LIMIT 500`, sourceID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []SeriesMatch{}
	for rows.Next() {
		var v SeriesMatch
		if e = rows.Scan(&v.VideoID, &v.Title, &v.Channel, &v.Position, &v.EpisodeID, &v.SeasonNumber, &v.EpisodeNum, &v.EpisodeTitle); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func seriesMatchIndex(ctx context.Context, q rowsQueryer, sourceID string) (map[string]int64, error) {
	rows, e := q.QueryContext(ctx, `SELECT video_id,episode_id FROM youtube_series_matches WHERE source_id=?`, sourceID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var videoID string
		var episodeID int64
		if e = rows.Scan(&videoID, &episodeID); e != nil {
			return nil, e
		}
		out[videoID] = episodeID
	}
	return out, rows.Err()
}

func (s *Store) seriesEpisodeOptions(ctx context.Context, mediaID int64) ([]SeriesEpisodeOption, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT e.id,s.number,e.number,e.title,(e.monitored=1 AND s.monitored=1 AND m.monitored=1),(SELECT COUNT(*) FROM episode_files f WHERE f.episode_id=e.id),(SELECT COUNT(*) FROM episode_downloads d WHERE d.episode_id=e.id)+(SELECT COUNT(*) FROM youtube_jobs y WHERE y.episode_id=e.id AND y.state NOT IN ('completed','cancelled','failed')) FROM episodes e JOIN seasons s ON s.id=e.season_id JOIN media_items m ON m.id=s.series_id WHERE s.series_id=? ORDER BY s.number,e.number LIMIT 5001`, mediaID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []SeriesEpisodeOption{}
	for rows.Next() {
		var v SeriesEpisodeOption
		if e = rows.Scan(&v.ID, &v.SeasonNumber, &v.EpisodeNumber, &v.Title, &v.Monitored, &v.Files, &v.Active); e != nil {
			return nil, e
		}
		v.Label = fmt.Sprintf("S%02dE%02d — %s", v.SeasonNumber, v.EpisodeNumber, v.Title)
		out = append(out, v)
	}
	return out, rows.Err()
}

// PreviewSeriesMatches lists the source's videos next to the show's loaded
// episodes so the owner can bind each video to one episode. It saves nothing.
func (s *Store) PreviewSeriesMatches(ctx context.Context, mediaID int64, sourceID string, inspector SourceInspector) (SeriesMatchPreview, error) {
	src, e := scanSeriesSource(s.db.QueryRowContext(ctx, `SELECT `+seriesSourceColumns+` FROM youtube_series_sources WHERE id=? AND media_id=?`, sourceID, mediaID))
	if errors.Is(e, sql.ErrNoRows) {
		return SeriesMatchPreview{}, ErrChanged
	}
	if e != nil {
		return SeriesMatchPreview{}, e
	}
	if src.Mapping != "manual" {
		return SeriesMatchPreview{}, ErrInput
	}
	if inspector == nil {
		return SeriesMatchPreview{}, ErrInspection
	}
	normalized, e := NormalizeURL(src.URL)
	if e != nil {
		return SeriesMatchPreview{}, ErrInvalidURL
	}
	work, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	inspection, e := inspector.Inspect(work, normalized.URL)
	if e != nil {
		return SeriesMatchPreview{}, e
	}
	if inspection.Source != normalized {
		return SeriesMatchPreview{}, ErrInspection
	}
	assigned, e := seriesMatchIndex(work, s.db, sourceID)
	if e != nil {
		return SeriesMatchPreview{}, e
	}
	episodes, e := s.seriesEpisodeOptions(work, mediaID)
	if e != nil {
		return SeriesMatchPreview{}, e
	}
	out := SeriesMatchPreview{Title: inspection.Title, Channel: inspection.Channel, Truncated: inspection.Truncated, Videos: []SeriesMatchVideo{}, Episodes: episodes}
	position := 0
	for _, entry := range inspection.Entries {
		if entry.Kind != "video" || !storeVideoID.MatchString(entry.ID) || !textOK(entry.Title, 500) {
			continue
		}
		v := SeriesMatchVideo{VideoID: entry.ID, Title: entry.Title, Channel: entry.Channel, Position: position, State: "unassigned"}
		if episodeID, ok := assigned[entry.ID]; ok && episodeID > 0 {
			v.EpisodeID = episodeID
			state, note, label := episodeManual(work, s.db, mediaID, episodeID)
			v.Label = label
			if state == "ready" {
				v.State = "ready"
			} else {
				v.State, v.Note = "blocked", note
			}
		}
		out.Videos = append(out.Videos, v)
		position++
	}
	return out, nil
}

// SaveSeriesMatches replaces the explicit bindings for a manual source. When
// queue is set the source is asked to run once so the assigned, monitored,
// missing episodes are queued. Existing "queued" entries keep their job.
func (s *Store) SaveSeriesMatches(ctx context.Context, mediaID int64, sourceID string, revision int, queue bool, matches []SeriesMatchInput) (int, error) {
	if mediaID < 1 || revision < 1 || len(matches) > 200 {
		return 0, ErrInput
	}
	seen := map[string]bool{}
	clean := make([]SeriesMatchInput, 0, len(matches))
	for _, m := range matches {
		if !storeVideoID.MatchString(m.VideoID) || m.EpisodeID < 1 || seen[m.VideoID] {
			return 0, ErrInput
		}
		if m.Title != "" && !textOK(m.Title, 500) {
			return 0, ErrInput
		}
		if len(m.Channel) > 500 || !utf8.ValidString(m.Channel) || strings.ContainsFunc(m.Channel, unicode.IsControl) {
			return 0, ErrInput
		}
		seen[m.VideoID] = true
		clean = append(clean, m)
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	var mapping string
	if e = tx.QueryRowContext(ctx, `SELECT mapping FROM youtube_series_sources WHERE id=? AND media_id=? AND revision=?`, sourceID, mediaID, revision).Scan(&mapping); e != nil {
		return 0, ErrChanged
	}
	if mapping != "manual" {
		return 0, ErrInput
	}
	for _, m := range clean {
		var owned int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM episodes e JOIN seasons s ON s.id=e.season_id WHERE e.id=? AND s.series_id=?`, m.EpisodeID, mediaID).Scan(&owned); e != nil {
			return 0, e
		}
		if owned != 1 {
			return 0, ErrChanged
		}
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM youtube_series_matches WHERE source_id=?`, sourceID); e != nil {
		return 0, e
	}
	now := time.Now().Unix()
	for position, m := range clean {
		if _, e = tx.ExecContext(ctx, `INSERT INTO youtube_series_matches(source_id,video_id,episode_id,position,title,channel,created_at) VALUES(?,?,?,?,?,?,?)`, sourceID, m.VideoID, m.EpisodeID, position, m.Title, m.Channel, now); e != nil {
			return 0, e
		}
	}
	// Forget prior automatic decisions so a changed binding is re-evaluated.
	// Queued entries stay put so an active job is never duplicated.
	if _, e = tx.ExecContext(ctx, `DELETE FROM youtube_series_entries WHERE source_id=? AND state IN ('baseline','skipped')`, sourceID); e != nil {
		return 0, e
	}
	requested := 0
	if queue {
		requested = 1
	}
	res, e := tx.ExecContext(ctx, `UPDATE youtube_series_sources SET initialized=1,requested=?,next_run=0,state='idle',message='',revision=revision+1 WHERE id=? AND media_id=? AND revision=?`, requested, sourceID, mediaID, revision)
	if e != nil {
		return 0, e
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return 0, ErrChanged
	}
	if e = tx.Commit(); e != nil {
		return 0, e
	}
	return len(clean), nil
}
