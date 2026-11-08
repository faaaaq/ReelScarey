package transfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
)

// musicJobLimit matches the paged Activity/queue view used by movie and TV
// downloads.
const musicJobLimit = 24

// musicActiveStates are the states the worker owns. Completed, cancelled,
// blocked and failed jobs are never claimed until an explicit retry.
const musicActiveStates = `'queued','submitting','downloading','importing'`

// MusicJob is one album download. Music releases are stored by their
// "album:release" candidate key, so the row identity is a string even though
// the album itself is an integer foreign key.
type MusicJob struct {
	ID              string  `json:"id"`
	AlbumID         int64   `json:"album_id"`
	ReleaseID       string  `json:"release_id"`
	Title           string  `json:"title"`
	InfoHash        string  `json:"info_hash"`
	Protocol        string  `json:"protocol"`
	ClientKey       string  `json:"client_key,omitempty"`
	ClientKind      string  `json:"client_kind,omitempty"`
	ClientLabel     string  `json:"client_label,omitempty"`
	ClientRoot      string  `json:"client_download_root,omitempty"`
	ClientLocal     string  `json:"client_local_folder,omitempty"`
	SavePath        string  `json:"save_path"`
	Root            string  `json:"root"`
	IndexerName     string  `json:"indexer_name,omitempty"`
	State           string  `json:"state"`
	Progress        float64 `json:"progress"`
	TotalBytes      int64   `json:"total_bytes"`
	DownloadedBytes int64   `json:"downloaded_bytes"`
	Error           string  `json:"error"`
	ImportJobID     *int64  `json:"import_job_id,omitempty"`
	Attempts        int     `json:"attempts"`
	CreatedAt       int64   `json:"created_at"`
	UpdatedAt       int64   `json:"updated_at"`

	nextRun int64
	owner   string
}

type MusicJobPage struct {
	Items []MusicJob `json:"items"`
	Page  int        `json:"page"`
	Total int        `json:"total"`
	Limit int        `json:"limit"`
}

const musicJobColumns = `id,album_id,release_id,title,info_hash,protocol,client_key,client_kind,client_label,client_download_root,client_local_folder,save_path,root,indexer_name,state,progress,total_bytes,downloaded_bytes,error,import_job_id,attempts,next_run,created_at,updated_at`

func scanMusicJob(row scanner) (MusicJob, error) {
	var j MusicJob
	var importID sql.NullInt64
	e := row.Scan(&j.ID, &j.AlbumID, &j.ReleaseID, &j.Title, &j.InfoHash, &j.Protocol,
		&j.ClientKey, &j.ClientKind, &j.ClientLabel, &j.ClientRoot, &j.ClientLocal,
		&j.SavePath, &j.Root, &j.IndexerName, &j.State, &j.Progress, &j.TotalBytes,
		&j.DownloadedBytes, &j.Error, &importID, &j.Attempts, &j.nextRun, &j.CreatedAt, &j.UpdatedAt)
	if importID.Valid {
		id := importID.Int64
		j.ImportJobID = &id
	}
	return j, e
}

// MusicJob returns one music download by its string identity.
func (s *Service) MusicJob(ctx context.Context, id string) (MusicJob, error) {
	if !textOK(id, 100) {
		return MusicJob{}, problem("music_job_not_found", "This music download no longer exists.")
	}
	j, e := scanMusicJob(s.db.QueryRowContext(ctx, `SELECT `+musicJobColumns+` FROM music_downloads WHERE id=?`, id))
	if errors.Is(e, sql.ErrNoRows) {
		return MusicJob{}, problem("music_job_not_found", "This music download no longer exists.")
	}
	return j, e
}

// MusicJobs lists music downloads, newest first, 24 per page.
func (s *Service) MusicJobs(ctx context.Context, page int) (MusicJobPage, error) {
	if page < 1 {
		page = 1
	}
	if page > 100000 {
		page = 100000
	}
	out := MusicJobPage{Items: []MusicJob{}, Page: page, Limit: musicJobLimit}
	if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_downloads`).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := s.db.QueryContext(ctx, `SELECT `+musicJobColumns+` FROM music_downloads ORDER BY created_at DESC,id LIMIT ? OFFSET ?`, musicJobLimit, (page-1)*musicJobLimit)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		j, e := scanMusicJob(rows)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, j)
	}
	return out, rows.Err()
}

// QueueMusic resolves a sealed music release candidate, submits its payload
// through the same download client selection as movie/TV jobs and records the
// tracked download. The payload is submitted here, not by the worker, because
// music_downloads deliberately stores no payload or client job identity: an
// uncertain submission is recorded as blocked for review and is never
// resubmitted automatically.
func (s *Service) QueueMusic(ctx context.Context, albumID int64, releaseID, root string) (MusicJob, error) {
	if albumID < 1 {
		return MusicJob{}, problem("invalid_music_album", "Choose a valid album.")
	}
	releaseID = strings.TrimSpace(releaseID)
	if !textOK(releaseID, 500) {
		return MusicJob{}, problem("invalid_release", "Search again to get a fresh release selection.")
	}
	root = strings.TrimSpace(strings.ReplaceAll(root, "\\", "/"))
	if _, e := safeRelative(root); e != nil {
		return MusicJob{}, problem("invalid_music_root", "Choose a relative media folder below the library root.")
	}
	var albumExists int
	if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_albums WHERE id=?`, albumID).Scan(&albumExists); e != nil {
		return MusicJob{}, e
	}
	if albumExists == 0 {
		return MusicJob{}, problem("invalid_music_album", "Choose an album already in your music library.")
	}

	s.configMu.Lock()
	defer s.configMu.Unlock()

	// Refuse a second active download for the album before contacting a
	// client, so a duplicate selection cannot submit the payload twice.
	if active, e := s.activeMusicForAlbum(ctx, albumID); e != nil {
		return MusicJob{}, e
	} else if active {
		return MusicJob{}, problem("duplicate_music_download", "This album already has an active music download. Finish or cancel it before choosing another release.")
	}

	key := fmt.Sprintf("%d:%s", albumID, releaseID)
	var sealed []byte
	var expires int64
	e := s.db.QueryRowContext(ctx, `SELECT payload,expires_at FROM music_release_candidates WHERE id=? AND album_id=?`, key, albumID).Scan(&sealed, &expires)
	if errors.Is(e, sql.ErrNoRows) {
		return MusicJob{}, problem("release_expired", "Search again to get a fresh music release selection.")
	}
	if e != nil {
		return MusicJob{}, e
	}
	if expires < time.Now().Unix() {
		return MusicJob{}, problem("release_expired", "This music release selection expired. Search again.")
	}
	plain, e := s.secrets.Open("music-release", sealed)
	if e != nil {
		return MusicJob{}, e
	}
	var v Candidate
	if e = json.Unmarshal(plain, &v); e != nil {
		return MusicJob{}, e
	}
	if v.ID != releaseID {
		return MusicJob{}, problem("invalid_release", "The encrypted music release selection does not match its record. Search again.")
	}

	indexer, e := s.candidateIndexer(ctx, v)
	if e != nil {
		return MusicJob{}, e
	}
	if v.Generation != indexer.Generation {
		return MusicJob{}, problem("release_expired", "The indexer settings changed. Search again before downloading.")
	}
	protocol := "torrent"
	if indexer.Kind == "newznab" {
		protocol = "usenet"
	}
	if v.Protocol != "" && v.Protocol != protocol {
		return MusicJob{}, problem("invalid_release", "The music release protocol does not match its indexer. Search again.")
	}
	client, e := s.downloadConfig(ctx, protocol)
	if e != nil {
		return MusicJob{}, e
	}
	adapter := s.adapter(indexer)
	if adapter == nil {
		return MusicJob{}, problem("release_expired", "The source adapter is unavailable. Search again.")
	}
	payload, hash, e := adapter.Fetch(ctx, indexer, v)
	if e != nil {
		return MusicJob{}, e
	}
	if e = validateMusicPayload(protocol, payload, hash); e != nil {
		return MusicJob{}, e
	}

	id := ID()
	save := path.Join(client.DownloadPath, "ReelScarey", id)
	tag := "reelscarey-" + id
	if protocol == "usenet" {
		var usenet interface {
			Add(context.Context, ClientConfig, string, []byte) (string, error)
		} = s.sab
		if client.kind == "nzbget" {
			usenet = s.nzbget
		}
		if _, e = usenet.Add(ctx, client, tag, payload.NZB); e != nil {
			return s.musicSubmissionFailure(ctx, albumID, releaseID, v.Title, hash, protocol, client, save, root, v.IndexerName, id, e)
		}
	} else {
		if e = s.torrentClient(client).Add(ctx, client, tag, save, payload); e != nil {
			return s.musicSubmissionFailure(ctx, albumID, releaseID, v.Title, hash, protocol, client, save, root, v.IndexerName, id, e)
		}
	}

	now := time.Now().Unix()
	if _, e = s.db.ExecContext(ctx, `INSERT INTO music_downloads(id,album_id,release_id,title,info_hash,protocol,client_key,client_kind,client_label,client_download_root,client_local_folder,save_path,root,indexer_name,state,progress,total_bytes,downloaded_bytes,error,attempts,next_run,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,'downloading',0,0,0,'',0,?,?,?)`,
		id, albumID, releaseID, v.Title, hash, protocol, client.profileKey, client.kind, client.label,
		client.DownloadPath, client.LocalFolder, save, root, v.IndexerName, now, now, now); e != nil {
		return MusicJob{}, e
	}
	return s.MusicJob(ctx, id)
}

func validateMusicPayload(protocol string, payload Payload, hash string) error {
	if protocol == "usenet" {
		identity, e := NZBIdentity(payload.NZB)
		if e != nil || identity != hash || payload.Magnet != "" || len(payload.Torrent) > 0 {
			return problem("payload_mismatch", "The saved NZB does not match this music release. No submission was made.")
		}
		return nil
	}
	var actual string
	var e error
	switch {
	case payload.Magnet != "" && len(payload.Torrent) == 0:
		actual, e = MagnetHash(payload.Magnet)
	case payload.Magnet == "" && len(payload.Torrent) > 0:
		actual, e = TorrentHash(payload.Torrent)
	default:
		return problem("payload_mismatch", "The saved torrent payload is invalid. No submission was made.")
	}
	if e != nil || actual != hash {
		return problem("payload_mismatch", "The saved torrent payload does not match this music release. No submission was made.")
	}
	return nil
}

// musicSubmissionFailure records an uncertain client submission so it is
// visible for review and is never silently retried. Definitive rejections are
// returned as errors without a row: nothing was accepted to reconcile.
func (s *Service) musicSubmissionFailure(ctx context.Context, albumID int64, releaseID, title, hash, protocol string, client ClientConfig, save, root, indexerName, id string, submitErr error) (MusicJob, error) {
	var p *Error
	uncertain := errors.As(submitErr, &p) && (p.Temporary || p.Code == "submission_unknown" || p.Code == "usenet_submission_unknown")
	if !uncertain {
		return MusicJob{}, submitErr
	}
	message := "The download client did not confirm the music submission. Check the client before retrying; the same job is never resubmitted automatically."
	if p.Message != "" {
		message = p.Message
	}
	now := time.Now().Unix()
	if _, e := s.db.ExecContext(ctx, `INSERT INTO music_downloads(id,album_id,release_id,title,info_hash,protocol,client_key,client_kind,client_label,client_download_root,client_local_folder,save_path,root,indexer_name,state,progress,total_bytes,downloaded_bytes,error,attempts,next_run,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,'blocked',0,0,0,?,0,?,?,?)`,
		id, albumID, releaseID, title, hash, protocol, client.profileKey, client.kind, client.label,
		client.DownloadPath, client.LocalFolder, save, root, indexerName, message, now, now, now); e != nil {
		return MusicJob{}, e
	}
	return s.MusicJob(ctx, id)
}

func (s *Service) activeMusicForAlbum(ctx context.Context, albumID int64) (bool, error) {
	var count int
	e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_downloads WHERE album_id=? AND state IN (`+musicActiveStates+`)`, albumID).Scan(&count)
	return count > 0, e
}

// CancelMusic stops tracking an active music download. The client job and its
// source files are left untouched.
func (s *Service) CancelMusic(ctx context.Context, id string) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if _, e := s.MusicJob(ctx, id); e != nil {
		return e
	}
	result, e := s.db.ExecContext(ctx, `UPDATE music_downloads SET state='cancelled',error='',lease_owner='',lease_until=0,updated_at=unixepoch() WHERE id=? AND state IN (`+musicActiveStates+`) AND lease_until<unixepoch()`, id)
	if e != nil {
		return e
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return problem("music_download_busy", "This music download is no longer active or is still finishing a step. Refresh and try again.")
	}
	return nil
}

// RetryMusic re-arms a blocked or failed music download. Submission state is
// never fabricated here: a retry only makes the worker reconcile the client
// again or resume polling its existing import job.
func (s *Service) RetryMusic(ctx context.Context, id string) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if _, e := s.MusicJob(ctx, id); e != nil {
		return e
	}
	result, e := s.db.ExecContext(ctx, `UPDATE music_downloads SET state=CASE WHEN import_job_id IS NULL THEN 'downloading' ELSE 'importing' END,attempts=0,next_run=0,error='',lease_owner='',lease_until=0,updated_at=unixepoch() WHERE id=? AND state IN ('blocked','failed') AND lease_until<unixepoch()`, id)
	if e != nil {
		return e
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return problem("music_download_busy", "Only a music download waiting for attention can be retried.")
	}
	return nil
}
