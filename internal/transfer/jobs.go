package transfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/organization"
	"time"
)

type FileChoice struct {
	Suggestions []int64 `json:"suggestions,omitempty"`
	Index       int     `json:"index"`
	Name        string  `json:"name"`
	Size        int64   `json:"size_bytes"`
}
type Job struct {
	MovieVersion               string `json:"movie_version,omitempty"`
	ClientKey                  string `json:"client_key,omitempty"`
	ClientKind                 string `json:"client_kind,omitempty"`
	ClientLabel                string `json:"client_label,omitempty"`
	Protocol                   string `json:"protocol"`
	ClientID                   string `json:"client_id,omitempty"`
	clientFiles, clientStorage string
	Kind                       string                      `json:"kind"`
	Targets                    []TVTarget                  `json:"targets,omitempty"`
	AnimeSearch                *AnimeSearchSnapshot        `json:"anime_search,omitempty"`
	AnimeOrder                 *library.AnimeOrderSnapshot `json:"anime_order,omitempty"`
	fileIndex                  *int
	fileTargets                []TVTarget
	ID                         string       `json:"id"`
	MovieID                    int64        `json:"movie_id"`
	MovieTitle                 string       `json:"movie_title"`
	Title                      string       `json:"title"`
	Hash                       string       `json:"info_hash"`
	State                      string       `json:"state"`
	Progress                   float64      `json:"progress"`
	Total                      int64        `json:"total_bytes"`
	Downloaded                 int64        `json:"downloaded_bytes"`
	ClientState                string       `json:"client_state"`
	Selected                   *int         `json:"selected_file"`
	Choices                    []FileChoice `json:"choices"`
	Attempts                   int          `json:"attempts"`
	ErrorCode                  string       `json:"error_code"`
	ErrorMessage               string       `json:"error_message"`
	ExtractPath                string       `json:"extract_path,omitempty"`
	Created                    int64        `json:"created_at"`
	Updated                    int64        `json:"updated_at"`
	owner                      string
	payload                    []byte
	clientURL, savePath        string
	clientRoot, clientLocal    string
	attempted                  int
	submittedAt                int64
	organizationSnapshot       string
}

const jobColumns = `d.kind,d.id,d.movie_id,m.title,d.title,d.info_hash,d.state,d.progress,d.total_bytes,d.downloaded_bytes,d.client_state,d.selected_file,d.choices,d.attempts,d.error_code,d.error_message,d.created_at,d.updated_at,d.lease_owner,d.payload,d.client_url,d.save_path,d.submission_attempted,d.submitted_at,d.protocol,d.client_id,d.client_files,d.client_storage,d.organization_snapshot,d.anime_search_snapshot,d.anime_order_snapshot,d.client_key,d.client_kind,d.client_label,d.client_download_root,d.client_local_folder,d.movie_version,d.extract_path`

type scanner interface{ Scan(...any) error }

func scanJob(row scanner) (Job, error) {
	var j Job
	var choices, anime, order string
	e := row.Scan(&j.Kind, &j.ID, &j.MovieID, &j.MovieTitle, &j.Title, &j.Hash, &j.State, &j.Progress, &j.Total, &j.Downloaded, &j.ClientState, &j.Selected, &choices, &j.Attempts, &j.ErrorCode, &j.ErrorMessage, &j.Created, &j.Updated, &j.owner, &j.payload, &j.clientURL, &j.savePath, &j.attempted, &j.submittedAt, &j.Protocol, &j.ClientID, &j.clientFiles, &j.clientStorage, &j.organizationSnapshot, &anime, &order, &j.ClientKey, &j.ClientKind, &j.ClientLabel, &j.clientRoot, &j.clientLocal, &j.MovieVersion, &j.ExtractPath)
	if e == nil {
		e = json.Unmarshal([]byte(choices), &j.Choices)
	}
	if e == nil && anime != "" {
		e = json.Unmarshal([]byte(anime), &j.AnimeSearch)
	}
	if e == nil && order != "" {
		e = json.Unmarshal([]byte(order), &j.AnimeOrder)
	}
	if j.Choices == nil {
		j.Choices = []FileChoice{}
	}
	return j, e
}
func (s *Service) jobBy(ctx context.Context, column string, value any) (Job, error) {
	j, e := scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM downloads d JOIN media_items m ON m.id=d.movie_id WHERE `+column+`=?`, value))
	if e == nil && j.Kind == "series" {
		j.Targets, e = s.targets(ctx, j.ID)
	}
	return j, e
}
func (s *Service) Job(ctx context.Context, id string) (Job, error) { return s.jobBy(ctx, "d.id", id) }

type JobPage struct {
	Items []Job `json:"items"`
	Page  int   `json:"page"`
	Total int   `json:"total"`
	Limit int   `json:"limit"`
}

func (s *Service) List(ctx context.Context, page int) (JobPage, error) {
	// Completed jobs belong to History. The direct job endpoint still returns
	// them so item history can link to an old download.
	if page < 1 {
		page = 1
	}
	out := JobPage{Items: []Job{}, Page: page, Limit: 24}
	if e := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM downloads WHERE state<>'completed'").Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := s.db.QueryContext(ctx, `SELECT `+jobColumns+` FROM downloads d JOIN media_items m ON m.id=d.movie_id WHERE d.state<>'completed' ORDER BY d.created_at DESC,d.id LIMIT 24 OFFSET ?`, (page-1)*24)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		j, e := scanJob(rows)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, j)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	for n := range out.Items {
		if out.Items[n].Kind == "series" {
			out.Items[n].Targets, e = s.targets(ctx, out.Items[n].ID)
			if e != nil {
				return out, e
			}
		}
	}
	return out, nil
}
func (s *Service) owned(ctx context.Context, j Job, query string, args ...any) error {
	args = append(args, j.ID, j.owner)
	result, e := s.db.ExecContext(ctx, query+` WHERE id=? AND lease_owner=? AND lease_until>=unixepoch()`, args...)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e == nil && n != 1 {
		return ErrLease
	}
	return e
}
func (s *Service) claim(ctx context.Context) (Job, error) {
	if e := organization.EnsureMediaAvailable(ctx, s.db); e != nil {
		return Job{}, e
	}
	owner := ID()
	now := time.Now().Unix()
	var id string
	e := s.db.QueryRowContext(ctx, `UPDATE downloads SET lease_owner=?,lease_until=? WHERE id=(SELECT id FROM downloads WHERE state IN ('queued','submitting','downloading','importing') AND next_run<=? AND lease_until<? ORDER BY next_run,created_at,id LIMIT 1) RETURNING id`, owner, now+60, now, now).Scan(&id)
	if e != nil {
		return Job{}, e
	}
	return s.Job(ctx, id)
}
func (s *Service) Retry(ctx context.Context, id string) error {
	if err := s.ensureReplacementMutation(ctx, id); err != nil {
		return err
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	j, e := s.Job(ctx, id)
	if e != nil {
		return e
	}
	if j.State != "blocked" && j.State != "failed" && j.State != "cancelled" {
		return problem("download_busy", "Only downloads needing attention or stopped tracking can be retried.")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if j.Kind != "series" {
		var replacementID int64
		if j.MovieVersion == "replacement" {
			if e = tx.QueryRowContext(ctx, `SELECT media_file_id FROM acquisition_replacements WHERE source_kind='download' AND job_id=?`, j.ID).Scan(&replacementID); e != nil {
				return e
			}
		}
		if e = reserveMovieVersion(ctx, tx, j.MovieID, j.MovieVersion, j.Title, j.ID, replacementID); e != nil {
			return e
		}
	}
	if j.Kind == "series" {
		if e = reserveTV(ctx, tx, id, j.MovieID, j.Targets); e != nil {
			return e
		}
	}
	result, e := tx.ExecContext(ctx, `UPDATE downloads SET state=CASE WHEN EXISTS(SELECT 1 FROM import_operations WHERE download_id=downloads.id) OR EXISTS(SELECT 1 FROM download_tv WHERE download_id=downloads.id AND mapping_confirmed=1) THEN 'importing' ELSE 'queued' END,submission_attempted=CASE WHEN error_code IN ('submission_unknown','client_add_rejected') THEN 0 ELSE submission_attempted END,attempts=0,next_run=0,lease_owner='',lease_until=0,error_code='',error_message='',updated_at=unixepoch() WHERE id=? AND state IN ('blocked','failed','cancelled') AND lease_until<unixepoch()`, id)
	if e != nil {
		return e
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return problem("download_busy", "This download is still finishing a step. Try again shortly.")
	}
	if _, e = tx.ExecContext(ctx, `UPDATE media_items SET status='downloading' WHERE id=? AND status<>'available'`, j.MovieID); e != nil {
		return e
	}
	if j.Kind == "series" {
		if e = refreshTVStatus(ctx, tx, j.MovieID); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Service) SelectFile(ctx context.Context, id string, index int) error {
	j, e := s.Job(ctx, id)
	if e != nil {
		return e
	}
	if j.State != "blocked" || j.ErrorCode != "choose_file" {
		return problem("download_busy", "This download is not waiting for a file choice.")
	}
	found := false
	for _, v := range j.Choices {
		if v.Index == index {
			found = true
		}
	}
	if !found {
		return problem("invalid_file", "Choose one of this download's movie files.")
	}
	result, e := s.db.ExecContext(ctx, `UPDATE downloads SET selected_file=?,state='downloading',next_run=0,attempts=0,error_code='',error_message='',updated_at=unixepoch() WHERE id=? AND state='blocked' AND lease_until<unixepoch()`, index, id)
	if e != nil {
		return e
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return problem("download_busy", "The download changed. Refresh Activity and try again.")
	}
	return nil
}
func (s *Service) Cancel(ctx context.Context, id string) error {
	if err := s.ensureReplacementMutation(ctx, id); err != nil {
		return err
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	j, e := s.Job(ctx, id)
	if e != nil {
		return e
	}
	if j.State != "blocked" && j.State != "failed" {
		return problem("download_busy", "Stop tracking is available only when a download needs attention.")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	result, e := tx.ExecContext(ctx, `UPDATE downloads SET state='cancelled',error_code='',error_message='Tracking stopped. The client job and its source files were left unchanged.',updated_at=unixepoch() WHERE id=? AND state IN ('blocked','failed') AND lease_until<unixepoch()`, id)
	if e != nil {
		return e
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return problem("download_busy", "The download is still finishing a step. Try again shortly.")
	}
	if j.Kind == "series" {
		if _, e = tx.ExecContext(ctx, `DELETE FROM episode_downloads WHERE download_id=?`, j.ID); e != nil {
			return e
		}
		if e = refreshTVStatus(ctx, tx, j.MovieID); e != nil {
			return e
		}
		return tx.Commit()
	}
	if _, e = tx.ExecContext(ctx, `UPDATE media_items SET status='missing' WHERE id=? AND NOT EXISTS(SELECT 1 FROM media_files WHERE movie_id=?) AND NOT EXISTS(SELECT 1 FROM downloads WHERE movie_id=? AND state NOT IN ('completed','cancelled'))`, j.MovieID, j.MovieID, j.MovieID); e != nil {
		return e
	}
	return tx.Commit()
}
func noRow(err error) bool { return errors.Is(err, sql.ErrNoRows) }
