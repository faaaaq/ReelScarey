package transfer

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"math"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/faaaaq/ReelScarey/internal/music"
)

// MusicImporter is the narrow boundary the transfer worker uses to hand a
// completed album folder to the music module. internal/music owns matching,
// tagging, publication and its own journal; transfer only tracks the download.
type MusicImporter interface {
	StartImport(context.Context, int64, string, string) (music.ImportJob, error)
	StartImportMapped(context.Context, int64, string, string, map[int]int64) (music.ImportJob, error)
	ListFolder(context.Context, string) ([]music.FolderFile, error)
	ImportJob(context.Context, int64) (music.ImportJob, error)
}

// Music importers are registered per Service. Keeping this alongside the
// additive music worker files avoids widening the existing Service literal for
// an optional vertical-slice dependency.
var musicImporters sync.Map // map[*Service]MusicImporter

func (s *Service) SetMusicImporter(importer MusicImporter) {
	if importer == nil {
		musicImporters.Delete(s)
		return
	}
	musicImporters.Store(s, importer)
}

func (s *Service) musicImporter() MusicImporter {
	if v, ok := musicImporters.Load(s); ok {
		if importer, ok := v.(MusicImporter); ok {
			return importer
		}
	}
	return nil
}

// errMusicSettled means processMusic already wrote a terminal or review state
// (completed or blocked) and the generic success bookkeeping must not clear it.
var errMusicSettled = errors.New("music download settled")

// RunMusic is the music reconciliation loop. It is a second claimant over one
// shared worker goroutine, not a second daemon: Run must start exactly one.
func (s *Service) RunMusic(ctx context.Context, logger *slog.Logger) {
	for ctx.Err() == nil {
		processed, err := s.ProcessMusic(ctx)
		if err != nil && ctx.Err() == nil {
			logger.Error("music download worker failed", "operation", "reconcile")
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

// ProcessMusic claims and advances at most one music download.
func (s *Service) ProcessMusic(ctx context.Context) (bool, error) {
	j, err := s.claimMusic(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	work, cancel := context.WithCancel(ctx)
	defer cancel()
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
				if s.ownedMusic(work, j, `UPDATE music_downloads SET lease_until=unixepoch()+60`) != nil {
					cancel()
					return
				}
			}
		}
	}()
	err = s.processMusic(work, j)
	cancel()
	<-done

	finish, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	release := func() error {
		_, e := s.db.ExecContext(finish, `UPDATE music_downloads SET lease_owner='',lease_until=0 WHERE id=? AND lease_owner=?`, j.ID, j.owner)
		return e
	}
	if ctx.Err() != nil || errors.Is(err, ErrLease) || errors.Is(err, context.Canceled) {
		return true, release()
	}
	if errors.Is(err, errMusicSettled) {
		return true, release()
	}
	if err != nil {
		p := &Error{Code: "music_workflow_error", Message: "The music workflow could not finish this step. Check storage and integration settings, then retry."}
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
			err = s.ownedMusic(finish, j, `UPDATE music_downloads SET error=?,attempts=?,next_run=?,updated_at=unixepoch()`, musicErrorText(p.Message), attempts, next)
		} else {
			err = s.ownedMusic(finish, j, `UPDATE music_downloads SET state=?,error=?,attempts=?,next_run=?,updated_at=unixepoch()`, state, musicErrorText(p.Message), attempts, next)
		}
	} else {
		err = s.ownedMusic(finish, j, `UPDATE music_downloads SET attempts=0,error='',next_run=unixepoch()+5,updated_at=unixepoch()`)
	}
	releaseErr := release()
	if err != nil {
		return true, err
	}
	return true, releaseErr
}

func (s *Service) claimMusic(ctx context.Context) (MusicJob, error) {
	owner := ID()
	now := time.Now().Unix()
	var id string
	e := s.db.QueryRowContext(ctx, `UPDATE music_downloads SET lease_owner=?,lease_until=? WHERE id=(SELECT id FROM music_downloads WHERE state IN (`+musicActiveStates+`) AND next_run<=? AND lease_until<? ORDER BY next_run,created_at,id LIMIT 1) RETURNING id`, owner, now+60, now, now).Scan(&id)
	if e != nil {
		return MusicJob{}, e
	}
	j, e := s.MusicJob(ctx, id)
	if e == nil {
		j.owner = owner
	}
	return j, e
}

func (s *Service) ownedMusic(ctx context.Context, j MusicJob, query string, args ...any) error {
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

func (s *Service) processMusic(ctx context.Context, j MusicJob) error {
	if j.State == "importing" {
		if j.ImportJobID == nil {
			return problem("music_import_missing", "This music download has no import job. Retry it or stop tracking.")
		}
		importer := s.musicImporter()
		if importer == nil {
			return problem("music_importer_unavailable", "Music importing is not available. Enable the music library first.")
		}
		job, e := importer.ImportJob(ctx, *j.ImportJobID)
		if e != nil {
			return e
		}
		switch job.State {
		case "completed":
			if e = s.ownedMusic(ctx, j, `UPDATE music_downloads SET state='completed',progress=1,error='',updated_at=unixepoch()`); e != nil {
				return e
			}
			return errMusicSettled
		case "failed":
			message := job.Error
			if message == "" {
				message = "The music import failed. Review the downloaded folder and retry."
			}
			if e = s.ownedMusic(ctx, j, `UPDATE music_downloads SET state='blocked',error=?,updated_at=unixepoch()`, musicErrorText(message)); e != nil {
				return e
			}
			return errMusicSettled
		}
		return nil
	}
	c, e := s.boundMusicClient(ctx, j)
	if e != nil {
		return e
	}
	if j.Protocol == "usenet" {
		return s.processMusicUsenet(ctx, j, c)
	}
	return s.processMusicTorrent(ctx, j, c)
}

// boundMusicClient reopens the client snapshot saved when the job was queued.
// A changed client, download root or local-folder mapping fails the job rather
// than rerouting its existing output.
func (s *Service) boundMusicClient(ctx context.Context, j MusicJob) (ClientConfig, error) {
	var c ClientConfig
	var e error
	if j.ClientKey != "" {
		c, e = s.managedClientConfig(ctx, j.ClientKey)
	} else if j.Protocol == "usenet" {
		c, e = s.sabConfig(ctx)
		c.kind = "sabnzbd"
	} else {
		c, e = s.clientConfig(ctx)
		c.kind = "qbittorrent"
	}
	if e != nil {
		return c, e
	}
	if (j.ClientKind != "" && c.kind != j.ClientKind) || (j.ClientRoot != "" && c.DownloadPath != j.ClientRoot) || c.LocalFolder != j.ClientLocal {
		return c, problem("client_changed", "Restore this music download's original download client before resuming tracking.")
	}
	return c, nil
}

func (s *Service) processMusicTorrent(ctx context.Context, j MusicJob, c ClientConfig) error {
	client := s.torrentClient(c)
	torrent, e := client.Find(ctx, c, j.InfoHash)
	if e != nil {
		return e
	}
	tag := "reelscarey-" + j.ID
	if torrent == nil {
		if time.Now().Unix()-j.CreatedAt > 90 {
			return problem("submission_unknown", "The music torrent is not visible in its client. Check the client, then retry only if it was not accepted. The same info hash is never resubmitted automatically.")
		}
		return nil
	}
	if !containsPart(torrent.Tags, tag) {
		return problem("torrent_exists", "This torrent already exists outside this music download. It was left unchanged; choose another release or stop tracking this job.")
	}
	if math.IsNaN(torrent.Progress) || math.IsInf(torrent.Progress, 0) || torrent.Progress < 0 || torrent.Progress > 1 || torrent.Size < 0 || torrent.Completed < 0 {
		return problem("client_response", "The torrent client returned invalid progress information.")
	}
	state := torrent.State
	if !textOK(state, 50) {
		state = "unknown"
	}
	if e = s.ownedMusic(ctx, j, `UPDATE music_downloads SET state='downloading',progress=?,total_bytes=?,downloaded_bytes=?,updated_at=unixepoch()`, torrent.Progress, torrent.Size, torrent.Completed); e != nil {
		return e
	}
	if state == "error" || state == "missingFiles" {
		return problem("client_download_error", "The torrent client reports an error or missing files. Fix the torrent in the client, then retry tracking.")
	}
	complete := state == "uploading" || state == "stalledUP" || state == "queuedUP" || state == "pausedUP" || state == "stoppedUP" || state == "forcedUP"
	if !complete || torrent.Progress < 1 || torrent.AmountLeft != 0 {
		return nil
	}
	files, e := client.Files(ctx, c, j.InfoHash)
	if e != nil {
		return e
	}
	if !musicFilesComplete(files) {
		return problem("no_music_file", "The completed music download has no complete, supported audio file. Archives and unselected files are not imported.")
	}
	folder, e := musicCompletedFolder(j, torrent.SavePath, c.DownloadPath, "ReelScarey/"+j.ID)
	if e != nil {
		return e
	}
	return s.startMusicImport(ctx, j, folder)
}

func (s *Service) processMusicUsenet(ctx context.Context, j MusicJob, c ClientConfig) error {
	tag := "reelscarey-" + j.ID
	var adapter interface {
		Find(context.Context, ClientConfig, string, string) (*SABJob, error)
		Add(context.Context, ClientConfig, string, []byte) (string, error)
	} = s.sab
	if c.kind == "nzbget" {
		adapter = s.nzbget
	}
	remote, e := adapter.Find(ctx, c, "", tag)
	if e != nil {
		return e
	}
	if remote == nil {
		if time.Now().Unix()-j.CreatedAt > 90 {
			return problem("usenet_submission_unknown", "The music Usenet job is not visible. Restore its original name/category or history, then retry tracking. An uncertain NZB is never resubmitted automatically.")
		}
		return nil
	}
	if e = s.ownedMusic(ctx, j, `UPDATE music_downloads SET state='downloading',progress=?,total_bytes=?,downloaded_bytes=?,updated_at=unixepoch()`, remote.Progress, remote.Total, remote.Downloaded); e != nil {
		return e
	}
	if remote.Failed {
		return problem("client_download_error", "The Usenet client reports a failed music download. Repair or retry it there, retain its history, then retry tracking here.")
	}
	if !remote.Complete {
		return nil
	}
	folder, e := musicCompletedFolder(j, remote.Storage, c.DownloadPath, "ReelScarey/reelscarey-"+j.ID)
	if e != nil {
		return e
	}
	return s.startMusicImport(ctx, j, folder)
}

func (s *Service) startMusicImport(ctx context.Context, j MusicJob, folder string) error {
	importer := s.musicImporter()
	if importer == nil {
		return problem("music_importer_unavailable", "Music importing is not available. Enable the music library first.")
	}
	job, e := importer.StartImport(ctx, j.AlbumID, folder, j.Root)
	if e != nil {
		return e
	}
	if job.ID < 1 {
		return problem("music_import_missing", "The music importer did not create an import job. Nothing was imported.")
	}
	return s.ownedMusic(ctx, j, `UPDATE music_downloads SET state='importing',import_job_id=?,progress=1,error='',updated_at=unixepoch()`, job.ID)
}

// musicCompletedFolder turns a client-reported completed path into the folder
// relative to the downloads mount, keeping the job's dedicated folder and the
// client's local-folder mapping intact.
func musicCompletedFolder(j MusicJob, clientPath, remoteRoot, jobRoot string) (string, error) {
	mapped, e := MapSource(clientPath, ".reelscarey-check", remoteRoot)
	if e != nil {
		return "", e
	}
	relative := strings.TrimSuffix(mapped, "/.reelscarey-check")
	switch {
	case relative == jobRoot:
	case path.Dir(relative) == jobRoot:
		// A single-file Usenet job reports the file itself.
		relative = jobRoot
	default:
		return "", problem("path_mapping", "The completed music download must finish in its dedicated ReelScarey folder. Restore its location in the client before retrying.")
	}
	if j.ClientLocal != "" {
		relative = j.ClientLocal + "/" + relative
	}
	if _, e = safeRelative(relative); e != nil {
		return "", problem("path_mapping", "The completed music folder is outside the configured download root.")
	}
	return relative, nil
}

func musicFilesComplete(files []TorrentFile) bool {
	for _, f := range files {
		if f.Priority > 0 && f.Progress == 1 && f.Size > 0 && musicAudioFile(f.Name) {
			return true
		}
	}
	return false
}

func musicAudioFile(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".flac", ".mp3", ".m4a", ".aac", ".ogg", ".opus", ".wav", ".alac", ".aiff", ".aif", ".ape", ".wv", ".wma":
		return true
	}
	return false
}

func musicErrorText(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "The music workflow could not finish this step."
	}
	if len(message) > 1000 {
		message = message[:1000]
	}
	return message
}
