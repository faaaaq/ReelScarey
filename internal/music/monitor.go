package music

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

type MusicMonitorSettings struct {
	Enabled       bool   `json:"enabled"`
	IntervalHours int    `json:"interval_hours"`
	NextRun       int64  `json:"next_run"`
	LastRun       int64  `json:"last_run"`
	LastError     string `json:"last_error"`
	Revision      int    `json:"revision"`
	UpdatedAt     int64  `json:"updated_at"`
}

type MusicMonitorSource struct {
	ID          int64  `json:"id"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	Kind        string `json:"kind"`
	Provider    string `json:"provider"`
	Enabled     bool   `json:"enabled"`
	LastChecked int64  `json:"last_checked"`
	LastError   string `json:"last_error"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

type MusicMonitorSourceList struct {
	Items []MusicMonitorSource `json:"items"`
}

func (s *Service) MonitorSettings(ctx context.Context) (MusicMonitorSettings, error) {
	var out MusicMonitorSettings
	var enabled int
	err := s.store.db.QueryRowContext(ctx, `SELECT enabled,interval_hours,next_run,last_run,last_error,revision,updated_at FROM music_monitor_settings WHERE id=1`).
		Scan(&enabled, &out.IntervalHours, &out.NextRun, &out.LastRun, &out.LastError, &out.Revision, &out.UpdatedAt)
	out.Enabled = enabled == 1
	return out, err
}

func (s *Service) SaveMonitorSettings(ctx context.Context, enabled bool, intervalHours, revision int) (MusicMonitorSettings, error) {
	if revision < 1 || intervalHours < 1 || intervalHours > 168 {
		return MusicMonitorSettings{}, ErrInvalidInput
	}
	result, err := s.store.db.ExecContext(ctx, `UPDATE music_monitor_settings SET enabled=?,interval_hours=?,next_run=CASE WHEN ?=1 THEN 0 ELSE next_run END,revision=revision+1,updated_at=unixepoch() WHERE id=1 AND revision=?`, boolInt(enabled), intervalHours, boolInt(enabled), revision)
	if err != nil {
		return MusicMonitorSettings{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return MusicMonitorSettings{}, err
	}
	if changed != 1 {
		return MusicMonitorSettings{}, ErrPlanChanged
	}
	return s.MonitorSettings(ctx)
}

func (s *Service) ListMonitorSources(ctx context.Context) (MusicMonitorSourceList, error) {
	rows, err := s.store.db.QueryContext(ctx, `SELECT id,url,title,kind,provider,enabled,last_checked,last_error,created_at,updated_at FROM music_monitor_sources ORDER BY id`)
	if err != nil {
		return MusicMonitorSourceList{}, err
	}
	defer rows.Close()
	out := MusicMonitorSourceList{Items: []MusicMonitorSource{}}
	for rows.Next() {
		var source MusicMonitorSource
		var enabled int
		if err := rows.Scan(&source.ID, &source.URL, &source.Title, &source.Kind, &source.Provider, &enabled, &source.LastChecked, &source.LastError, &source.CreatedAt, &source.UpdatedAt); err != nil {
			return MusicMonitorSourceList{}, err
		}
		source.Enabled = enabled == 1
		out.Items = append(out.Items, source)
	}
	return out, rows.Err()
}

func (s *Service) AddMonitorSource(ctx context.Context, rawURL string) (MusicMonitorSource, error) {
	preview, err := s.ResolveLink(ctx, rawURL)
	if err != nil {
		return MusicMonitorSource{}, err
	}
	if preview.Kind != "playlist" && preview.Kind != "artist" {
		return MusicMonitorSource{}, &Error{Code: "music_monitor_kind", Message: "Only playlist and artist links can be monitored."}
	}
	now := time.Now().Unix()
	if _, err := s.store.db.ExecContext(ctx, `INSERT INTO music_monitor_sources(url,title,kind,provider,enabled,created_at,updated_at) VALUES(?,?,?,?,1,?,?)
		ON CONFLICT(url) DO UPDATE SET title=excluded.title,kind=excluded.kind,provider=excluded.provider,updated_at=excluded.updated_at`,
		strings.TrimSpace(rawURL), preview.Title, preview.Kind, preview.Source, now, now); err != nil {
		return MusicMonitorSource{}, err
	}
	var source MusicMonitorSource
	var enabled int
	if err := s.store.db.QueryRowContext(ctx, `SELECT id,url,title,kind,provider,enabled,last_checked,last_error,created_at,updated_at FROM music_monitor_sources WHERE url=?`, strings.TrimSpace(rawURL)).
		Scan(&source.ID, &source.URL, &source.Title, &source.Kind, &source.Provider, &enabled, &source.LastChecked, &source.LastError, &source.CreatedAt, &source.UpdatedAt); err != nil {
		return MusicMonitorSource{}, err
	}
	source.Enabled = enabled == 1
	return source, nil
}

func (s *Service) SetMonitorSourceEnabled(ctx context.Context, id int64, enabled bool) (MusicMonitorSource, error) {
	if id < 1 {
		return MusicMonitorSource{}, ErrInvalidInput
	}
	if _, err := s.store.db.ExecContext(ctx, `UPDATE music_monitor_sources SET enabled=?,updated_at=unixepoch() WHERE id=?`, boolInt(enabled), id); err != nil {
		return MusicMonitorSource{}, err
	}
	var source MusicMonitorSource
	var flag int
	if err := s.store.db.QueryRowContext(ctx, `SELECT id,url,title,kind,provider,enabled,last_checked,last_error,created_at,updated_at FROM music_monitor_sources WHERE id=?`, id).
		Scan(&source.ID, &source.URL, &source.Title, &source.Kind, &source.Provider, &flag, &source.LastChecked, &source.LastError, &source.CreatedAt, &source.UpdatedAt); err != nil {
		return MusicMonitorSource{}, err
	}
	source.Enabled = flag == 1
	return source, nil
}

func (s *Service) DeleteMonitorSource(ctx context.Context, id int64) error {
	_, err := s.store.db.ExecContext(ctx, `DELETE FROM music_monitor_sources WHERE id=?`, id)
	return err
}

func (s *Service) TriggerMonitor(ctx context.Context) (MusicMonitorSettings, error) {
	if _, err := s.store.db.ExecContext(ctx, `UPDATE music_monitor_settings SET next_run=0,updated_at=unixepoch() WHERE id=1`); err != nil {
		return MusicMonitorSettings{}, err
	}
	return s.MonitorSettings(ctx)
}

// RunMonitorLoop starts paused: the settings row defaults to disabled.
func (s *Service) RunMonitorLoop(ctx context.Context, logger *slog.Logger) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if _, err := s.ProcessMonitor(ctx); err != nil && ctx.Err() == nil {
			logger.Error("music monitor failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) ProcessMonitor(ctx context.Context) (bool, error) {
	settings, err := s.MonitorSettings(ctx)
	if err != nil || !settings.Enabled {
		return false, err
	}
	now := time.Now().Unix()
	if settings.NextRun > now {
		return false, nil
	}
	result, err := s.store.db.ExecContext(ctx, `UPDATE music_monitor_settings SET lease_until=? WHERE id=1 AND enabled=1 AND lease_until<? AND next_run<=?`, now+600, now, now)
	if err != nil {
		return false, err
	}
	claimed, err := result.RowsAffected()
	if err != nil || claimed != 1 {
		return false, err
	}
	sources, err := s.ListMonitorSources(ctx)
	if err != nil {
		return false, err
	}
	failures := []string{}
	queued := 0
	for _, source := range sources.Items {
		if !source.Enabled {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		count, err := s.processMonitorSource(ctx, source)
		queued += count
		message := ""
		if err != nil {
			message = err.Error()
			failures = append(failures, source.Title+": "+message)
		}
		_, _ = s.store.db.ExecContext(ctx, `UPDATE music_monitor_sources SET last_checked=?,last_error=?,updated_at=unixepoch() WHERE id=?`, time.Now().Unix(), message, source.ID)
	}
	lastError := strings.Join(failures, " ")
	_, _ = s.store.db.ExecContext(ctx, `UPDATE music_monitor_settings SET last_run=?,next_run=?,last_error=?,lease_until=0,updated_at=unixepoch() WHERE id=1`,
		time.Now().Unix(), time.Now().Unix()+int64(settings.IntervalHours*3600), lastError)
	if queued > 0 {
		return true, nil
	}
	return false, nil
}

func (s *Service) processMonitorSource(ctx context.Context, source MusicMonitorSource) (int, error) {
	preview, err := s.ResolveLink(ctx, source.URL)
	if err != nil {
		return 0, err
	}
	seen := map[string]bool{}
	rows, err := s.store.db.QueryContext(ctx, `SELECT track_key FROM music_monitor_seen WHERE source_id=?`, source.ID)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var key string
		if rows.Scan(&key) == nil {
			seen[key] = true
		}
	}
	rows.Close()
	inputs := []AcquisitionTrackInput{}
	keys := []string{}
	for _, track := range preview.Tracks {
		key := monitorTrackKey(track)
		if key == "" || seen[key] {
			continue
		}
		keys = append(keys, key)
		inputs = append(inputs, AcquisitionTrackInput{
			Title: track.Title, Artist: track.Artist, Album: track.Album, AlbumArtist: track.AlbumArtist,
			Year: track.Year, TrackNumber: track.TrackNumber, DiscNumber: track.DiscNumber,
			DurationMS: track.DurationMS, CoverURL: track.CoverURL, SourceURL: track.SourceURL, SearchQuery: track.SearchQuery,
		})
		if len(inputs) >= 100 {
			break
		}
	}
	if len(inputs) == 0 {
		return 0, nil
	}
	if _, err := s.StartAcquisition(ctx, source.URL, "monitor", preview.Title, "Music", "source", inputs); err != nil {
		return 0, err
	}
	now := time.Now().Unix()
	for _, key := range keys[:len(inputs)] {
		_, _ = s.store.db.ExecContext(ctx, `INSERT OR IGNORE INTO music_monitor_seen(source_id,track_key,first_seen) VALUES(?,?,?)`, source.ID, key, now)
	}
	return len(inputs), nil
}

func monitorTrackKey(track LinkTrack) string {
	if strings.TrimSpace(track.SourceURL) != "" {
		return "source:" + strings.ToLower(strings.TrimSpace(track.SourceURL))
	}
	artist := compactMatchName(track.Artist)
	title := compactMatchName(track.Title)
	album := compactMatchName(track.Album)
	if artist == "" || title == "" {
		return ""
	}
	return "tag:" + artist + "|" + title + "|" + album
}
