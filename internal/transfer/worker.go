package transfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"strings"
	"time"
)

func (s *Service) Run(ctx context.Context, logger *slog.Logger) {
	if err := s.cleanupCommitted(ctx); err != nil && ctx.Err() == nil {
		logger.Warn("committed import staging cleanup deferred")
	}
	for ctx.Err() == nil {
		processed, err := s.ProcessNext(ctx)
		if err != nil && ctx.Err() == nil {
			logger.Error("download worker failed", "operation", "reconcile")
		}
		if processed && err == nil {
			continue
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (s *Service) ProcessNext(ctx context.Context) (bool, error) {
	j, err := s.claim(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	work, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-work.Done():
				return
			case <-ticker.C:
				if s.owned(work, j, `UPDATE downloads SET lease_until=unixepoch()+60`) != nil {
					cancel()
					return
				}
			}
		}
	}()
	err = s.process(work, j)
	cancel()
	<-done
	finish, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if ctx.Err() != nil || errors.Is(err, ErrLease) || errors.Is(err, context.Canceled) {
		_, e := s.db.ExecContext(finish, `UPDATE downloads SET lease_owner='',lease_until=0 WHERE id=? AND lease_owner=?`, j.ID, j.owner)
		return true, e
	}
	if err != nil {
		p := &Error{Code: "workflow_error", Message: "The workflow could not finish this step. Check storage and integration settings, then retry."}
		var typed *Error
		if errors.As(err, &typed) {
			p = typed
		}
		state := "blocked"
		next := time.Now().Unix()
		attempts := j.Attempts + 1
		if p.Temporary && attempts < 8 {
			state = ""
			delay := int64(5 * math.Pow(2, float64(attempts-1)))
			if delay > 300 {
				delay = 300
			}
			next += delay
		} else if p.Temporary {
			state = "failed"
		}
		if state == "" {
			err = s.owned(finish, j, `UPDATE downloads SET error_code=?,error_message=?,attempts=?,next_run=?,updated_at=unixepoch()`, p.Code, p.Message, attempts, next)
		} else {
			err = s.owned(finish, j, `UPDATE downloads SET state=?,error_code=?,error_message=?,attempts=?,next_run=?,updated_at=unixepoch()`, state, p.Code, p.Message, attempts, next)
		}
	} else {
		err = s.owned(finish, j, `UPDATE downloads SET attempts=0,error_code='',error_message='',next_run=unixepoch()+5,updated_at=unixepoch()`)
	}
	_, releaseErr := s.db.ExecContext(finish, `UPDATE downloads SET lease_owner='',lease_until=0 WHERE id=? AND lease_owner=?`, j.ID, j.owner)
	if err != nil {
		return true, err
	}
	return true, releaseErr
}
func (s *Service) process(ctx context.Context, j Job) error {
	if j.State == "importing" {
		var source string
		var size int64
		err := s.db.QueryRowContext(ctx, `SELECT source_path,size_bytes FROM import_operations WHERE download_id=?`, j.ID).Scan(&source, &size)
		if err == nil {
			return s.importer.Execute(ctx, j, source, size)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	c, err := s.clientConfig(ctx)
	if err != nil {
		return err
	}
	if c.URL != j.clientURL {
		return problem("client_changed", "Restore the qBittorrent URL used by this download before resuming tracking.")
	}
	torrent, err := s.client.Find(ctx, c, j.Hash)
	if err != nil {
		return err
	}
	tag := "reelscarey-" + j.ID
	if torrent == nil {
		if j.attempted != 0 {
			if time.Now().Unix()-j.submittedAt > 90 {
				return problem("submission_unknown", "The torrent is not visible in qBittorrent. Check the client, then retry this job if it was not accepted. The same info hash will be reused.")
			}
			return nil
		}
		plain, err := s.secrets.Open("download", j.payload)
		if err != nil {
			return err
		}
		var payload Payload
		if err = json.Unmarshal(plain, &payload); err != nil {
			return err
		}
		if err = s.owned(ctx, j, `UPDATE downloads SET state='submitting',submission_attempted=1,submitted_at=unixepoch(),updated_at=unixepoch()`); err != nil {
			return err
		}
		if err = s.client.Add(ctx, c, tag, j.savePath, payload); err != nil {
			return err
		}
		return s.owned(ctx, j, `UPDATE downloads SET state='downloading',updated_at=unixepoch()`)
	}
	if !containsPart(torrent.Tags, tag) {
		return problem("torrent_exists", "This torrent already exists outside this ReelScarey job. It was left unchanged; choose another release or stop tracking this job.")
	}
	if math.IsNaN(torrent.Progress) || math.IsInf(torrent.Progress, 0) || torrent.Progress < 0 || torrent.Progress > 1 || torrent.Size < 0 || torrent.Completed < 0 {
		return problem("client_response", "qBittorrent returned invalid progress information.")
	}
	state := torrent.State
	if !textOK(state, 50) {
		state = "unknown"
	}
	if err = s.owned(ctx, j, `UPDATE downloads SET state='downloading',progress=?,total_bytes=?,downloaded_bytes=?,client_state=?,updated_at=unixepoch()`, torrent.Progress, torrent.Size, torrent.Completed, state); err != nil {
		return err
	}
	if state == "error" || state == "missingFiles" {
		return problem("client_download_error", "qBittorrent reports an error or missing files. Fix the torrent in qBittorrent, then retry tracking.")
	}
	complete := state == "uploading" || state == "stalledUP" || state == "queuedUP" || state == "pausedUP" || state == "stoppedUP" || state == "forcedUP"
	if !complete || torrent.Progress < 1 || torrent.AmountLeft != 0 {
		return nil
	}
	files, err := s.client.Files(ctx, c, j.Hash)
	if err != nil {
		return err
	}
	choices := make([]FileChoice, 0)
	seen := map[int]bool{}
	var chosen *TorrentFile
	for n := range files {
		f := &files[n]
		if seen[f.Index] || f.Index < 0 {
			return problem("client_response", "qBittorrent returned duplicate or invalid file indexes.")
		}
		seen[f.Index] = true
		if !video(f.Name) || extraVideo.MatchString(f.Name) || f.Priority == 0 {
			continue
		}
		if _, err := safeRelative(f.Name); err != nil {
			return err
		}
		if f.Size <= 0 || math.IsNaN(f.Progress) || f.Progress < 1 {
			continue
		}
		choices = append(choices, FileChoice{Index: f.Index, Name: f.Name, Size: f.Size})
		if j.Selected != nil && f.Index == *j.Selected {
			chosen = f
		}
	}
	if j.Selected == nil && len(choices) == 1 {
		for n := range files {
			if files[n].Index == choices[0].Index {
				chosen = &files[n]
			}
		}
	}
	if chosen == nil {
		if len(choices) == 0 {
			return problem("no_movie_file", "The completed torrent has no complete, supported movie file. Archives, samples, trailers, and unselected files are not imported.")
		}
		b, _ := json.Marshal(choices)
		if err = s.owned(ctx, j, `UPDATE downloads SET choices=?,updated_at=unixepoch()`, string(b)); err != nil {
			return err
		}
		return problem("choose_file", "This torrent contains more than one movie file. Choose the correct file to import.")
	}
	source, err := MapSource(torrent.SavePath, chosen.Name, c.DownloadPath)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(source, "ReelScarey/"+j.ID+"/") {
		return problem("path_mapping", "The torrent's save location changed outside this job's dedicated download directory. Restore its location in qBittorrent before retrying.")
	}
	if err = s.owned(ctx, j, `UPDATE downloads SET state='importing',selected_file=?,choices='[]',updated_at=unixepoch()`, chosen.Index); err != nil {
		return err
	}
	return s.importer.Execute(ctx, j, source, chosen.Size)
}
