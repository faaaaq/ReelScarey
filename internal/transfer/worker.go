package transfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"time"
)

func (s *Service) Run(ctx context.Context, logger *slog.Logger) {
	libraryCtx, libraryCancel := context.WithCancel(ctx)
	libraryDone := make(chan struct{})
	go func() { defer close(libraryDone); s.runLibrary(libraryCtx, logger) }()
	matchingDone := make(chan struct{})
	go func() { defer close(matchingDone); s.runAutomaticMatching(libraryCtx, logger) }()
	musicDone := make(chan struct{})
	go func() { defer close(musicDone); s.RunMusic(libraryCtx, logger) }()
	stallDone := make(chan struct{})
	go func() { defer close(stallDone); s.RunStallWatch(libraryCtx, logger) }()
	seederDone := make(chan struct{})
	go func() { defer close(seederDone); s.RunSeederWatch(libraryCtx, logger) }()
	defer func() {
		libraryCancel()
		<-libraryDone
		<-matchingDone
		<-musicDone
		<-stallDone
		<-seederDone
	}()
	if err := s.cleanupCommitted(ctx); err != nil && ctx.Err() == nil {
		logger.Warn("committed import staging cleanup deferred")
	}
	for ctx.Err() == nil {
		processed, err := s.ProcessNext(ctx)
		if err != nil && ctx.Err() == nil {
			logger.Error("download worker failed", "operation", "reconcile", "error", err)
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
			if state == "failed" {
				s.blockFailedJob(finish, j, p)
			}
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
		if j.Kind == "series" {
			return s.processTVImports(ctx, j)
		}
		var source string
		var size int64
		err := s.db.QueryRowContext(ctx, `SELECT source_path,size_bytes FROM import_operations WHERE download_id=? AND file_index=-1`, j.ID).Scan(&source, &size)
		if err == nil {
			if err = s.importer.Execute(ctx, j, source, size); err != nil {
				return err
			}
			_ = s.cleanupExtract(ctx, j)
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if j.Protocol == "usenet" {
		return s.processUsenet(ctx, j)
	}
	c, err := s.boundClient(ctx, j)
	if err != nil {
		return err
	}
	if c.URL != j.clientURL {
		return problem("client_changed", "Restore the torrent client URL used by this download before resuming tracking.")
	}
	client := s.torrentClient(c)
	torrent, err := client.Find(ctx, c, j.Hash)
	if err != nil {
		return err
	}
	tag := "reelscarey-" + j.ID
	if torrent == nil {
		if j.attempted != 0 {
			if time.Now().Unix()-j.submittedAt > 90 {
				return problem("submission_unknown", "The torrent is not visible in its client. Check the client, then retry this job if it was not accepted. The same info hash will be reused.")
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
		var actualHash string
		if len(payload.NZB) > 0 {
			return problem("payload_mismatch", "A torrent job cannot submit an NZB.")
		}
		if payload.Magnet != "" && len(payload.Torrent) == 0 {
			actualHash, err = MagnetHash(payload.Magnet)
		} else if payload.Magnet == "" && len(payload.Torrent) > 0 {
			actualHash, err = TorrentHash(payload.Torrent)
		} else {
			return problem("payload_mismatch", "The saved torrent payload is invalid. No submission was made.")
		}
		if err != nil || actualHash != j.Hash {
			return problem("payload_mismatch", "The saved torrent payload does not match this job's info hash. No submission was made.")
		}
		if err = s.owned(ctx, j, `UPDATE downloads SET state='submitting',submission_attempted=1,submitted_at=unixepoch(),updated_at=unixepoch()`); err != nil {
			return err
		}
		if err = client.Add(ctx, c, tag, j.savePath, payload); err != nil {
			return err
		}
		return s.owned(ctx, j, `UPDATE downloads SET state='downloading',updated_at=unixepoch()`)
	}
	if !containsPart(torrent.Tags, tag) {
		return problem("torrent_exists", "This torrent already exists outside this ReelScarey job. It was left unchanged; choose another release or stop tracking this job.")
	}
	if math.IsNaN(torrent.Progress) || math.IsInf(torrent.Progress, 0) || torrent.Progress < 0 || torrent.Progress > 1 || torrent.Size < 0 || torrent.Completed < 0 {
		return problem("client_response", "The torrent client returned invalid progress information.")
	}
	state := torrent.State
	if !textOK(state, 50) {
		state = "unknown"
	}
	if err = s.owned(ctx, j, `UPDATE downloads SET state='downloading',progress=?,total_bytes=?,downloaded_bytes=?,client_state=?,updated_at=unixepoch()`, torrent.Progress, torrent.Size, torrent.Completed, state); err != nil {
		return err
	}
	if state == "error" || state == "missingFiles" {
		return problem("client_download_error", "The torrent client reports an error or missing files. Fix the torrent in the client, then retry tracking.")
	}
	complete := state == "uploading" || state == "stalledUP" || state == "queuedUP" || state == "pausedUP" || state == "stoppedUP" || state == "forcedUP"
	if !complete || torrent.Progress < 1 || torrent.AmountLeft != 0 {
		return nil
	}
	files, err := client.Files(ctx, c, j.Hash)
	if err != nil {
		return err
	}
	return s.processCompleted(ctx, j, torrent.SavePath, files, c.DownloadPath)
}
func completedVideoChoices(files []TorrentFile) ([]FileChoice, error) {
	choices := make([]FileChoice, 0)
	seen := map[int]bool{}
	for n := range files {
		f := &files[n]
		if seen[f.Index] || f.Index < 0 {
			return nil, problem("client_response", "The completed download returned duplicate or invalid file indexes.")
		}
		seen[f.Index] = true
		if !video(f.Name) || extraVideo.MatchString(f.Name) || f.Priority <= 0 {
			continue
		}
		if _, err := safeRelative(f.Name); err != nil {
			return nil, err
		}
		if f.Size <= 0 || math.IsNaN(f.Progress) || math.IsInf(f.Progress, 0) || f.Progress != 1 {
			continue
		}
		choices = append(choices, FileChoice{Index: f.Index, Name: f.Name, Size: f.Size})
	}
	return choices, nil
}

func (s *Service) processCompleted(ctx context.Context, j Job, savePath string, files []TorrentFile, remoteRoot string) error {
	choices, err := completedVideoChoices(files)
	if err != nil {
		return err
	}
	if len(choices) == 0 {
		extracted, extractErr := s.extractArchiveMedia(ctx, j, files, savePath, remoteRoot)
		if extractErr != nil {
			return extractErr
		}
		if len(extracted) > 0 {
			files = append(files, extracted...)
			choices, err = completedVideoChoices(files)
			if err != nil {
				return err
			}
		}
	}
	if j.Kind == "series" {
		return s.prepareTVMapping(ctx, j, choices, savePath, remoteRoot)
	}
	var chosen *TorrentFile
	if j.Selected != nil {
		for n := range files {
			if files[n].Index == *j.Selected {
				chosen = &files[n]
				break
			}
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
			return problem("no_movie_file", "The completed download has no complete, supported movie file. Archives, samples, trailers, and unselected files are not imported.")
		}
		b, _ := json.Marshal(choices)
		if err := s.owned(ctx, j, `UPDATE downloads SET choices=?,updated_at=unixepoch()`, string(b)); err != nil {
			return err
		}
		return problem("choose_file", "This download contains more than one movie file. Choose the correct file to import.")
	}
	source, err := mapJobSource(j, savePath, chosen.Name, remoteRoot)
	if err != nil {
		return err
	}
	if !sourceInJob(j, source) {
		return problem("path_mapping", "The torrent's save location changed outside this job's dedicated download directory. Restore its location in qBittorrent before retrying.")
	}
	if err = s.owned(ctx, j, `UPDATE downloads SET state='importing',selected_file=?,choices='[]',updated_at=unixepoch()`, chosen.Index); err != nil {
		return err
	}
	if err = s.importer.Execute(ctx, j, source, chosen.Size); err != nil {
		return err
	}
	_ = s.cleanupExtract(ctx, j)
	return nil
}
