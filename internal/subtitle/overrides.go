package subtitle

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

type ShowPreferences struct {
	Inherited      bool        `json:"inherited"`
	Preferences    Preferences `json:"preferences"`
	Revision       int         `json:"revision"`
	GlobalRevision int         `json:"global_revision"`
	Languages      []Language  `json:"languages"`
}
type ShowPreferenceUpdate struct {
	Enabled         bool    `json:"enabled"`
	Tracks          []Track `json:"tracks"`
	HearingImpaired string  `json:"hearing_impaired"`
	MatchPolicy     string  `json:"match_policy"`
	Revision        int     `json:"revision"`
	GlobalRevision  int     `json:"global_revision"`
}

func (s *Service) ShowPreferences(ctx context.Context, id int64) (ShowPreferences, error) {
	p, e := s.Preferences(ctx)
	if e != nil {
		return ShowPreferences{}, e
	}
	out := ShowPreferences{Inherited: true, Preferences: p, GlobalRevision: p.Revision, Languages: Languages}
	var kind string
	if e = s.db.QueryRowContext(ctx, `SELECT kind FROM media_items WHERE id=?`, id).Scan(&kind); e != nil {
		return out, e
	}
	if kind != "series" {
		return out, problem("invalid_preferences", "Choose a saved show for subtitle overrides.")
	}
	var tracks string
	e = s.db.QueryRowContext(ctx, `SELECT enabled,tracks,hearing_impaired,match_policy,revision FROM subtitle_overrides WHERE media_id=?`, id).Scan(&out.Preferences.Enabled, &tracks, &out.Preferences.HearingImpaired, &out.Preferences.MatchPolicy, &out.Revision)
	if errors.Is(e, sql.ErrNoRows) {
		return out, nil
	}
	if e != nil {
		return out, e
	}
	out.Inherited = false
	e = json.Unmarshal([]byte(tracks), &out.Preferences.Tracks)
	return out, e
}
func (s *Service) effectivePreferences(ctx context.Context, id int64) (Preferences, error) {
	p, e := s.Preferences(ctx)
	if e != nil {
		return p, e
	}
	global := p.Enabled
	var tracks string
	e = s.db.QueryRowContext(ctx, `SELECT enabled,tracks,hearing_impaired,match_policy FROM subtitle_overrides WHERE media_id=?`, id).Scan(&p.Enabled, &tracks, &p.HearingImpaired, &p.MatchPolicy)
	if errors.Is(e, sql.ErrNoRows) {
		var m media
		if err := s.db.QueryRowContext(ctx, `SELECT id,kind,category FROM media_items WHERE id=?`, id).Scan(&m.MediaID, &m.Kind, &m.Category); err != nil {
			return p, err
		}
		return s.preferencesFor(ctx, m, p), nil
	}
	if e != nil {
		return p, e
	}
	p.Enabled = p.Enabled && global
	if e = json.Unmarshal([]byte(tracks), &p.Tracks); e != nil {
		return p, e
	}
	var m media
	if e = s.db.QueryRowContext(ctx, `SELECT id,kind,category FROM media_items WHERE id=?`, id).Scan(&m.MediaID, &m.Kind, &m.Category); e != nil {
		return p, e
	}
	return s.preferencesFor(ctx, m, p), nil
}
func (s *Service) filePreferences(ctx context.Context, fileID int64) (Preferences, error) {
	var mediaID int64
	e := s.db.QueryRowContext(ctx, `SELECT COALESCE(movie_id,series_id) FROM media_files WHERE id=?`, fileID).Scan(&mediaID)
	if e != nil {
		return Preferences{}, e
	}
	return s.effectivePreferences(ctx, mediaID)
}
func (s *Service) SaveShowPreferences(ctx context.Context, id int64, in ShowPreferenceUpdate, inherit bool) (ShowPreferences, error) {
	old, e := s.ShowPreferences(ctx, id)
	if e != nil {
		return old, e
	}
	p := old.Preferences
	p.Enabled = in.Enabled
	p.Tracks = in.Tracks
	p.HearingImpaired = in.HearingImpaired
	p.MatchPolicy = in.MatchPolicy
	if !inherit {
		if e = validatePreferences(p); e != nil {
			return old, e
		}
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return old, e
	}
	defer tx.Rollback()
	var revision int
	if e = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT revision FROM subtitle_overrides WHERE media_id=?),0)`, id).Scan(&revision); e != nil {
		return old, e
	}
	if revision != in.Revision {
		return old, problem("revision_conflict", "Show subtitle preferences changed. Reload before saving.")
	}
	r, e := tx.ExecContext(ctx, `UPDATE subtitle_preferences SET revision=revision+1 WHERE id=1 AND revision=?`, in.GlobalRevision)
	if e != nil {
		return old, e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return old, problem("revision_conflict", "Global subtitle preferences changed. Reload before saving.")
	}
	if inherit {
		_, e = tx.ExecContext(ctx, `DELETE FROM subtitle_overrides WHERE media_id=?`, id)
	} else {
		tracks, _ := json.Marshal(in.Tracks)
		_, e = tx.ExecContext(ctx, `INSERT INTO subtitle_overrides(media_id,enabled,tracks,hearing_impaired,match_policy) VALUES(?,?,?,?,?) ON CONFLICT(media_id) DO UPDATE SET enabled=excluded.enabled,tracks=excluded.tracks,hearing_impaired=excluded.hearing_impaired,match_policy=excluded.match_policy,revision=subtitle_overrides.revision+1`, id, in.Enabled, string(tracks), in.HearingImpaired, in.MatchPolicy)
	}
	if e != nil {
		return old, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE subtitle_jobs SET requested=0,next_run=0,attempts=0,state='queued',message='Show subtitle preferences changed.',updated_at=? WHERE media_file_id IN (SELECT id FROM media_files WHERE series_id=?)`, s.now().Unix(), id); e != nil {
		return old, e
	}
	if e = tx.Commit(); e != nil {
		return old, e
	}
	return s.ShowPreferences(ctx, id)
}
