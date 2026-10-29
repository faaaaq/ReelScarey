package subtitle

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/faaaaq/ReelScarey/internal/transfer"
)

type Job struct {
	ID          string `json:"id"`
	MediaFileID int64  `json:"media_file_id"`
	Mode        string `json:"mode"`
	Requested   bool   `json:"requested"`
	Selected    string `json:"-"`
	State       string `json:"state"`
	Message     string `json:"message"`
	Attempts    int    `json:"attempts"`
	NextRun     int64  `json:"next_run"`
	owner       string
	until       int64
	Revision    int   `json:"revision"`
	Created     int64 `json:"created_at"`
	Updated     int64 `json:"updated_at"`
}

const jobColumns = `id,media_file_id,mode,requested,selected_candidate,state,message,attempts,next_run,lease_owner,lease_until,revision,created_at,updated_at`

func scanJob(row interface{ Scan(...any) error }) (Job, error) {
	var j Job
	e := row.Scan(&j.ID, &j.MediaFileID, &j.Mode, &j.Requested, &j.Selected, &j.State, &j.Message, &j.Attempts, &j.NextRun, &j.owner, &j.until, &j.Revision, &j.Created, &j.Updated)
	return j, e
}

const monitoredSQL = `EXISTS(SELECT 1 FROM media_files f JOIN media_items m ON m.id=COALESCE(f.movie_id,f.series_id) WHERE f.id=subtitle_jobs.media_file_id AND m.monitored=1 AND (f.movie_id IS NOT NULL OR EXISTS(SELECT 1 FROM episode_files ef JOIN episodes e ON e.id=ef.episode_id JOIN seasons s ON s.id=e.season_id WHERE ef.media_file_id=f.id AND e.monitored=1 AND s.monitored=1)))`

func (s *Service) discover(ctx context.Context, p Preferences) error {
	if !p.Enabled {
		return nil
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var cursor int64
	if e = tx.QueryRowContext(ctx, `SELECT discovery_cursor FROM subtitle_preferences WHERE id=1`).Scan(&cursor); e != nil {
		return e
	}
	rows, e := tx.QueryContext(ctx, `SELECT id FROM media_files WHERE id>? ORDER BY id LIMIT 50`, cursor)
	if e != nil {
		return e
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, id := range ids {
		if _, e = tx.ExecContext(ctx, `INSERT INTO subtitle_jobs(id,media_file_id,created_at,updated_at) VALUES(?,?,?,?) ON CONFLICT(media_file_id) DO NOTHING`, transfer.ID(), id, s.now().Unix(), s.now().Unix()); e != nil {
			return e
		}
		cursor = id
	}
	if len(ids) < 50 {
		cursor = 0
	}
	if _, e = tx.ExecContext(ctx, `UPDATE subtitle_preferences SET discovery_cursor=? WHERE id=1 AND revision=?`, cursor, p.Revision); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Service) claim(ctx context.Context) (Job, error) {
	now := s.now().Unix()
	owner := transfer.ID()
	row := s.db.QueryRowContext(ctx, `UPDATE subtitle_jobs SET lease_owner=?,lease_until=?,revision=(SELECT revision FROM subtitle_preferences WHERE id=1),mode=CASE WHEN requested=1 THEN mode ELSE 'automatic' END,selected_candidate=CASE WHEN requested=1 THEN selected_candidate ELSE '' END,state='running',message='Checking subtitle tracks.',updated_at=? WHERE id=(SELECT j.id FROM subtitle_jobs j CROSS JOIN subtitle_preferences p WHERE j.state NOT IN ('blocked','cancelled','paused') AND j.next_run<=? AND j.lease_until<? AND (j.requested=1 OR (p.enabled=1 AND COALESCE((SELECT o.enabled FROM subtitle_overrides o JOIN media_files mf ON o.media_id=COALESCE(mf.movie_id,mf.series_id) WHERE mf.id=j.media_file_id),1)=1 AND (p.only_monitored=0 OR `+stringsForAlias(monitoredSQL)+`))) ORDER BY j.next_run,j.created_at,j.id LIMIT 1) RETURNING `+jobColumns, owner, now+60, now, now, now)
	return scanJob(row)
}
func stringsForAlias(query string) string {
	return strings.ReplaceAll(query, "subtitle_jobs.media_file_id", "j.media_file_id")
}
func (s *Service) request(ctx context.Context, fileID int64, mode, selected string) error {
	if _, e := s.media(ctx, fileID); e != nil {
		return e
	}
	p, e := s.filePreferences(ctx, fileID)
	if e != nil {
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var busy int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM subtitle_jobs WHERE media_file_id=? AND (lease_until>=? OR (requested=1 AND state IN ('queued','running','retrying')))`, fileID, s.now().Unix()).Scan(&busy); e != nil {
		return e
	}
	if busy > 0 {
		return problem("subtitle_busy", "A subtitle job is already requested or running for this file.")
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO subtitle_jobs(id,media_file_id,mode,requested,selected_candidate,state,message,revision,created_at,updated_at) VALUES(?,?,?,1,?,'queued','Subtitle job requested.',?,?,?) ON CONFLICT(media_file_id) DO UPDATE SET mode=excluded.mode,requested=1,selected_candidate=excluded.selected_candidate,state='queued',message=excluded.message,attempts=0,next_run=0,revision=excluded.revision,updated_at=excluded.updated_at`, transfer.ID(), fileID, mode, selected, p.Revision, s.now().Unix(), s.now().Unix()); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Service) Search(ctx context.Context, fileID int64) error {
	return s.request(ctx, fileID, "review", "")
}
func (s *Service) Retry(ctx context.Context, fileID int64) error {
	j, e := scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM subtitle_jobs WHERE media_file_id=?`, fileID))
	if e != nil {
		return e
	}
	if !slices.Contains([]string{"blocked", "waiting_provider", "retrying", "needs_review", "no_match", "completed", "paused", "cancelled"}, j.State) {
		return problem("subtitle_busy", "This subtitle job is still running.")
	}
	return s.request(ctx, fileID, j.Mode, j.Selected)
}
func (s *Service) Select(ctx context.Context, id string, reviewed bool) error {
	if !reviewed {
		return problem("review_required", "Review the subtitle identity, language, track type and timing match before downloading.")
	}
	var fileID int64
	var payload string
	e := s.db.QueryRowContext(ctx, `SELECT media_file_id,payload FROM subtitle_candidates WHERE id=?`, id).Scan(&fileID, &payload)
	if errors.Is(e, sql.ErrNoRows) {
		return problem("candidate_expired", "This subtitle choice expired. Search again.")
	}
	if e != nil {
		return e
	}
	m, e := s.media(ctx, fileID)
	if e != nil {
		return e
	}
	p, e := s.filePreferences(ctx, fileID)
	if e != nil {
		return e
	}
	c, e := s.credentials(ctx)
	if e != nil {
		return e
	}
	candidate, e := s.selected(ctx, Job{Selected: id}, p, c, m)
	if e != nil {
		return e
	}
	var pending int64
	e = s.db.QueryRowContext(ctx, `SELECT provider_file_id FROM subtitle_imports WHERE media_file_id=? AND language=? AND variant=? AND phase<>'committed'`, fileID, candidate.Language, candidate.Variant).Scan(&pending)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if pending != 0 && pending != candidate.FileID {
		return problem("pending_import", "Finish the pending subtitle import before choosing a different subtitle for this track.")
	}
	return s.request(ctx, fileID, "download", id)
}
func (s *Service) fence(ctx context.Context, tx *sql.Tx, j Job, generation string, requireProvider bool) error {
	row := s.db.QueryRowContext
	if tx != nil {
		row = tx.QueryRowContext
	}
	var valid int
	e := row(ctx, `SELECT COUNT(*) FROM subtitle_jobs j CROSS JOIN subtitle_preferences p WHERE j.id=? AND j.lease_owner=? AND j.lease_until>=? AND j.revision=? AND p.revision=? AND (j.requested=1 OR (p.enabled=1 AND COALESCE((SELECT o.enabled FROM subtitle_overrides o JOIN media_files mf ON o.media_id=COALESCE(mf.movie_id,mf.series_id) WHERE mf.id=j.media_file_id),1)=1 AND (p.only_monitored=0 OR `+stringsForAlias(monitoredSQL)+`)))`, j.ID, j.owner, s.now().Unix(), j.Revision, j.Revision).Scan(&valid)
	if e != nil {
		return e
	}
	if valid != 1 {
		return ErrLease
	}
	if requireProvider {
		var actual string
		if e = row(ctx, `SELECT generation FROM subtitle_provider_state WHERE id=1`).Scan(&actual); e != nil {
			return e
		}
		if generation == "" || generation != actual {
			return ErrLease
		}
	}
	return nil
}
