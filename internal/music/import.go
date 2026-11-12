package music

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"

	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/faaaaq/ReelScarey/internal/filecheck"
	"github.com/faaaaq/ReelScarey/internal/organization"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type ImportJob struct {
	ID            int64        `json:"id"`
	AlbumID       int64        `json:"album_id"`
	Root          string       `json:"root"`
	SourcePath    string       `json:"source_path"`
	State         string       `json:"state"`
	FileCount     int          `json:"file_count"`
	ImportedCount int          `json:"imported_count"`
	Error         string       `json:"error"`
	CreatedAt     int64        `json:"created_at"`
	UpdatedAt     int64        `json:"updated_at"`
	Files         []ImportFile `json:"files,omitempty"`
}

type ImportFile struct {
	ID              int64  `json:"id"`
	SourcePath      string `json:"source_path"`
	TrackID         *int64 `json:"track_id"`
	DestinationPath string `json:"destination_path"`
	State           string `json:"state"`
	Error           string `json:"error"`
}

type ImportJobList struct {
	Items []ImportJob `json:"items"`
	Page  int         `json:"page"`
	Total int         `json:"total"`
	Limit int         `json:"limit"`
}

type importAlbum struct {
	ArtistID   int64
	AlbumID    int64
	ArtistName string
	ArtistMBID string
	AlbumTitle string
	AlbumMBID  string
	Year       int
	Tracks     []Track
	MaxDisc    int
}

type importPlan struct {
	Source string
	Track  Track
}

func (s *Service) StartImport(ctx context.Context, albumID int64, sourcePath, root string) (ImportJob, error) {
	return s.startImport(ctx, "", albumID, sourcePath, root, nil)
}

// StartImportMapped imports an album folder using an explicit file-index to
// track-ID map. It is used when tags are missing or ambiguous.
func (s *Service) StartImportMapped(ctx context.Context, albumID int64, sourcePath, root string, mapping map[int]int64) (ImportJob, error) {
	if len(mapping) == 0 {
		return ImportJob{}, ErrInvalidInput
	}
	return s.startImport(ctx, "", albumID, sourcePath, root, mapping)
}

// DownloadImport reattaches a transfer whose import was committed before the
// transfer worker saved its import ID.
func (s *Service) DownloadImport(ctx context.Context, downloadID string) (ImportJob, error) {
	var id int64
	if err := s.store.db.QueryRowContext(ctx, `SELECT id FROM music_import_jobs WHERE download_id=? AND download_id<>''`, downloadID).Scan(&id); err != nil {
		return ImportJob{}, err
	}
	return s.ImportJob(ctx, id)
}

func (s *Service) StartDownloadImport(ctx context.Context, downloadID string, albumID int64, sourcePath, root string, mapping map[int]int64) (ImportJob, error) {
	if downloadID == "" {
		return ImportJob{}, ErrInvalidInput
	}
	return s.startImport(ctx, downloadID, albumID, sourcePath, root, mapping)
}

func (s *Service) startImport(ctx context.Context, downloadID string, albumID int64, sourcePath, root string, mapping map[int]int64) (ImportJob, error) {
	if albumID < 1 {
		return ImportJob{}, ErrInvalidInput
	}
	defer organization.ReadMedia()()
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	if downloadID != "" {
		job, err := s.DownloadImport(ctx, downloadID)
		if !errors.Is(err, sql.ErrNoRows) {
			return job, err
		}
	}
	source, err := s.cleanImportSource(sourcePath)
	if err != nil {
		return ImportJob{}, err
	}
	root, err = s.cleanImportRoot(root)
	if err != nil {
		return ImportJob{}, err
	}
	album, err := s.loadImportAlbum(ctx, albumID)
	if err != nil {
		return ImportJob{}, err
	}
	if len(album.Tracks) == 0 {
		return ImportJob{}, &Error{Code: "music_no_tracks", Message: "Open the album once so its tracks are loaded before importing."}
	}
	sourceAbs := filepath.Join(s.downloadsDir, filepath.FromSlash(source))
	plans, err := s.planImport(ctx, album, sourceAbs, mapping)
	if err != nil {
		return ImportJob{}, err
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return ImportJob{}, err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	result, err := tx.ExecContext(ctx, `INSERT INTO music_import_jobs(album_id,root,source_path,state,file_count,created_at,updated_at,download_id) VALUES(?,?,?,'running',?,?,?,?)`, albumID, root, source, len(plans), now, now, downloadID)
	if err != nil {
		return ImportJob{}, err
	}
	jobID, err := result.LastInsertId()
	if err != nil {
		return ImportJob{}, err
	}
	for _, plan := range plans {
		relative, err := filepath.Rel(s.downloadsDir, plan.Source)
		if err != nil {
			return ImportJob{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO music_import_files(job_id,source_path,track_id,state,created_at,updated_at) VALUES(?,?,?,'pending',?,?)`, jobID, filepath.ToSlash(relative), plan.Track.ID, now, now); err != nil {
			return ImportJob{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ImportJob{}, err
	}
	return s.ImportJob(ctx, jobID)
}

func (s *Service) cleanImportSource(source string) (string, error) {
	if s.downloadsDir == "" {
		return "", &Error{Code: "music_import_source", Message: "The downloads root is not configured."}
	}
	source = strings.TrimSpace(strings.ReplaceAll(source, "\\", "/"))
	if source == "" || source == "." || strings.HasPrefix(source, "/") || strings.Contains(source, "..") {
		return "", ErrInvalidInput
	}
	clean := path.Clean(source)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", ErrInvalidInput
	}
	abs := filepath.Join(s.downloadsDir, filepath.FromSlash(clean))
	if !contained(s.downloadsDir, abs) {
		return "", ErrInvalidInput
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", &Error{Code: "music_import_source", Message: "Choose an existing folder below the downloads root."}
	}
	return clean, nil
}

func (s *Service) cleanImportRoot(root string) (string, error) {
	if s.mediaDir == "" {
		return "", ErrInvalidInput
	}
	root = strings.TrimSpace(strings.ReplaceAll(root, "\\", "/"))
	if root == "" || root == "." || strings.HasPrefix(root, "/") || strings.Contains(root, "..") {
		return "", ErrInvalidInput
	}
	clean := path.Clean(root)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", ErrInvalidInput
	}
	abs := filepath.Join(s.mediaDir, filepath.FromSlash(clean))
	if !contained(s.mediaDir, abs) {
		return "", ErrInvalidInput
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", err
	}
	return clean, nil
}

func (s *Service) loadImportAlbum(ctx context.Context, albumID int64) (importAlbum, error) {
	var album importAlbum
	var year sql.NullInt64
	err := s.store.db.QueryRowContext(ctx, `SELECT ar.id,a.id,ar.name,ar.musicbrainz_id,a.title,a.musicbrainz_id,a.year
		FROM music_albums a JOIN music_artists ar ON ar.id=a.artist_id WHERE a.id=?`, albumID).
		Scan(&album.ArtistID, &album.AlbumID, &album.ArtistName, &album.ArtistMBID, &album.AlbumTitle, &album.AlbumMBID, &year)
	if err != nil {
		return importAlbum{}, err
	}
	album.Year = int(year.Int64)
	album.Tracks, err = s.store.Tracks(ctx, albumID)
	if err != nil {
		return importAlbum{}, err
	}
	for _, track := range album.Tracks {
		if track.Disc > album.MaxDisc {
			album.MaxDisc = track.Disc
		}
	}
	if album.MaxDisc < 1 {
		album.MaxDisc = 1
	}
	return album, nil
}

func (s *Service) planImport(ctx context.Context, album importAlbum, source string, mapping map[int]int64) ([]importPlan, error) {
	files, err := audioFiles(source)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, &Error{Code: "music_import_empty", Message: "No audio files were found in this folder."}
	}
	if mapping != nil {
		if len(mapping) != len(files) {
			return nil, &Error{Code: "music_import_mapping", Message: "Map every downloaded file to exactly one catalog track."}
		}
		used := map[int64]bool{}
		plans := []importPlan{}
		for i, file := range files {
			trackID, ok := mapping[i]
			if !ok {
				return nil, &Error{Code: "music_import_mapping", Message: "Map every downloaded file to exactly one catalog track."}
			}
			track := trackByID(album.Tracks, trackID)
			if track == nil {
				return nil, &Error{Code: "music_import_mapping", Message: "A selected track is not part of this album."}
			}
			if used[track.ID] {
				return nil, &Error{Code: "music_import_mapping", Message: "Two files map to the same catalog track."}
			}
			used[track.ID] = true
			plans = append(plans, importPlan{Source: file, Track: *track})
		}
		for _, track := range album.Tracks {
			if !used[track.ID] {
				return nil, &Error{Code: "music_import_mapping", Message: "Every catalog track must be mapped to one file."}
			}
		}
		return plans, nil
	}
	byKey := map[string]Track{}
	byTitle := map[string]Track{}
	for _, track := range album.Tracks {
		byKey[fmt.Sprintf("%d:%d", track.Disc, track.Number)] = track
		byTitle[strings.ToLower(strings.TrimSpace(track.Title))] = track
	}
	used := map[int64]bool{}
	plans := []importPlan{}
	for _, file := range files {
		tags, err := s.prober.ProbeAudio(ctx, file)
		if err != nil {
			return nil, err
		}
		var track Track
		var ok bool
		if tags.Disc != nil && tags.Track != nil {
			track, ok = byKey[fmt.Sprintf("%d:%d", *tags.Disc, *tags.Track)]
		}
		if !ok && tags.Title != "" {
			track, ok = byTitle[strings.ToLower(strings.TrimSpace(tags.Title))]
		}
		if !ok {
			return nil, &Error{Code: "music_import_unmatched", Message: "No catalog track matches " + filepath.Base(file) + ". Tag it with its disc and track number, then retry."}
		}
		if used[track.ID] {
			return nil, &Error{Code: "music_import_duplicate", Message: "Two files map to the same catalog track."}
		}
		used[track.ID] = true
		plans = append(plans, importPlan{Source: file, Track: track})
	}
	for _, track := range album.Tracks {
		if !used[track.ID] {
			return nil, &Error{Code: "music_import_missing", Message: "No file matches the catalog track " + track.Title + "."}
		}
	}
	return plans, nil
}

type FolderFile struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
	Title  string `json:"title"`
	Track  *int   `json:"track"`
	Disc   *int   `json:"disc"`
}

// ListFolder probes the audio files in a completed download folder so the UI can
// offer an explicit file-to-track mapping.
func (s *Service) ListFolder(ctx context.Context, source string) ([]FolderFile, error) {
	clean, err := s.cleanImportSource(source)
	if err != nil {
		return nil, err
	}
	abs := filepath.Join(s.downloadsDir, filepath.FromSlash(clean))
	files, err := audioFiles(abs)
	if err != nil {
		return nil, err
	}
	out := []FolderFile{}
	for i, file := range files {
		tags, err := s.prober.ProbeAudio(ctx, file)
		if err != nil {
			return nil, err
		}
		name, _ := filepath.Rel(abs, file)
		out = append(out, FolderFile{Index: i, Name: filepath.ToSlash(name), Artist: tags.Artist, Album: tags.Album, Title: tags.Title, Track: tags.Track, Disc: tags.Disc})
	}
	return out, nil
}

func audioFiles(root string) ([]string, error) {
	files := []string{}
	err := filepath.WalkDir(root, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !audioExtensions[strings.ToLower(filepath.Ext(name))] {
			return nil
		}
		if len(files) >= 500 {
			return errors.New("The folder contains more than 500 audio files.")
		}
		files = append(files, name)
		return nil
	})
	return files, err
}

// RunImports processes durable import rows, including work interrupted by
// shutdown. A running row is never dependent on the initiating HTTP request.
func (s *Service) RunImports(ctx context.Context, logger *slog.Logger) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		worked, err := s.ProcessImport(ctx)
		if err != nil && ctx.Err() == nil {
			logger.Error("music import worker failed", "error", err)
		}
		if worked && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) ProcessImport(ctx context.Context) (bool, error) {
	defer organization.ReadMedia()()
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	var id, albumID int64
	var root string
	err := s.store.db.QueryRowContext(ctx, `SELECT id,album_id,root FROM music_import_jobs WHERE state='running' ORDER BY id LIMIT 1`).Scan(&id, &albumID, &root)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	work, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	fileID, err := s.runImport(work, id, albumID, root)
	if err == nil || ctx.Err() != nil {
		return true, err
	}
	// Leave interrupted work pending on process shutdown. A real conflict
	// remains visible and requires review instead of overwriting the path.
	finish, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	tx, saveErr := s.store.db.BeginTx(finish, nil)
	if saveErr != nil {
		return true, saveErr
	}
	defer tx.Rollback()
	if fileID != 0 {
		if _, saveErr := tx.ExecContext(finish, `UPDATE music_import_files SET state='failed',error=?,updated_at=unixepoch() WHERE id=? AND state='pending'`, err.Error(), fileID); saveErr != nil {
			return true, saveErr
		}
	}
	_, saveErr = tx.ExecContext(finish, `UPDATE music_import_jobs SET state='failed',error=?,updated_at=unixepoch() WHERE id=?`, err.Error(), id)
	if saveErr != nil {
		return true, saveErr
	}
	return true, tx.Commit()
}

type importPublication struct {
	Destination string    `json:"destination"`
	Stage       string    `json:"stage"`
	SHA         string    `json:"sha256"`
	Size        int64     `json:"size"`
	Tags        AudioTags `json:"tags"`
}

func (s *Service) runImport(ctx context.Context, jobID, albumID int64, folder string) (int64, error) {
	album, err := s.loadImportAlbum(ctx, albumID)
	if err != nil {
		return 0, err
	}
	media, err := os.OpenRoot(s.mediaDir)
	if err != nil {
		return 0, err
	}
	defer media.Close()
	for {
		var fileID, trackID int64
		var source, raw string
		err := s.store.db.QueryRowContext(ctx, `SELECT id,track_id,source_path,publication FROM music_import_files WHERE job_id=? AND state='pending' ORDER BY id LIMIT 1`, jobID).
			Scan(&fileID, &trackID, &source, &raw)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return fileID, err
		}
		track := trackByID(album.Tracks, trackID)
		if track == nil {
			return fileID, errors.New("A catalog track disappeared before import.")
		}
		if err := s.importFile(ctx, media, album, *track, folder, source, jobID, fileID, raw); err != nil {
			return fileID, err
		}
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE music_artists SET status='available',updated_at=unixepoch() WHERE id=?`, album.ArtistID); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE music_albums SET status='available',updated_at=unixepoch() WHERE id=?`, album.AlbumID); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE music_import_jobs SET state='completed',error='',updated_at=unixepoch() WHERE id=?`, jobID); err != nil {
		return 0, err
	}
	return 0, tx.Commit()
}

func (s *Service) importFile(ctx context.Context, media *os.Root, album importAlbum, track Track, folder, source string, jobID, fileID int64, raw string) error {
	var p importPublication
	if raw == "" {
		var err error
		p, err = s.prepareMusicImport(ctx, media, album, track, folder, source)
		if err != nil {
			return err
		}
		data, err := json.Marshal(p)
		if err != nil {
			return err
		}
		if _, err := s.store.db.ExecContext(ctx, `UPDATE music_import_files SET publication=? WHERE id=? AND state='pending'`, string(data), fileID); err != nil {
			return err
		}
	} else if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return err
	}
	if err := expectMusicDigest(ctx, media, p.Stage, p.SHA); err != nil {
		return errors.New("The staged music file changed. Inspect it before retrying.")
	}
	if !sameMusicFile(media, p.Stage, p.Destination) {
		if _, _, err := placeMusicStage(ctx, media, p.Destination, p.Stage, p.SHA); err != nil {
			return err
		}
	}
	if err := expectMusicDigest(ctx, media, p.Destination, p.SHA); err != nil {
		return filecheck.ErrChanged
	}
	tagJSON, err := json.Marshal(p.Tags)
	if err != nil {
		return err
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO music_files(artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,sample_rate,channels,duration_ms,tag_json,imported_at,origin,source_name)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,unixepoch(),'download',?)`,
		album.ArtistID, album.AlbumID, track.ID, folder, p.Destination, p.Size, p.SHA, p.Tags.Format, p.Tags.Bitrate, p.Tags.SampleRate, p.Tags.Channels, p.Tags.DurationMS, string(tagJSON), path.Base(source)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE music_import_files SET state='imported',destination_path=?,updated_at=unixepoch() WHERE id=?`, p.Destination, fileID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE music_import_jobs SET imported_count=(SELECT COUNT(*) FROM music_import_files WHERE job_id=? AND state='imported'),updated_at=unixepoch() WHERE id=?`, jobID, jobID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// The destination content is verified by digest above, so the staged copy
	// is no longer needed whether it was hardlinked in, copied, or the
	// destination already held identical content.
	_ = media.Remove(p.Stage)
	return nil
}

func (s *Service) prepareMusicImport(ctx context.Context, media *os.Root, album importAlbum, track Track, folder, source string) (importPublication, error) {
	var p importPublication
	downloads, err := os.OpenRoot(s.downloadsDir)
	if err != nil {
		return p, err
	}
	defer downloads.Close()
	input, err := filecheck.Open(downloads, source)
	if err != nil {
		return p, err
	}
	defer input.Close()
	before, err := filecheck.Capture(ctx, input)
	if err != nil {
		return p, err
	}
	ext := path.Ext(source)
	prefix := fmt.Sprintf("%02d", track.Number)
	if album.MaxDisc > 1 {
		prefix = fmt.Sprintf("%d-%02d", track.Disc, track.Number)
	}
	p.Destination = path.Join(folder, safeName(album.ArtistName), safeName(album.AlbumTitle), prefix+" "+safeName(track.Title)+ext)
	dir := path.Dir(p.Destination)
	if err := media.MkdirAll(dir, 0o755); err != nil {
		return p, err
	}
	var token [16]byte
	rand.Read(token[:])
	p.Stage = path.Join(dir, ".reelscarey-import-"+hex.EncodeToString(token[:])+ext)
	stage, err := media.OpenFile(p.Stage, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return p, err
	}
	stage.Close()
	keep := false
	defer func() {
		if !keep {
			_ = media.Remove(p.Stage)
		}
	}()
	stagePath := filepath.Join(s.mediaDir, filepath.FromSlash(p.Stage))
	tags := EditTags{
		Artist: album.ArtistName, AlbumArtist: album.ArtistName, Album: album.AlbumTitle, Title: track.Title,
		Track: track.Number, TrackTotal: len(album.Tracks), Disc: track.Disc, DiscTotal: album.MaxDisc, Year: album.Year,
		MusicBrainzArtistID: album.ArtistMBID, MusicBrainzReleaseGroupID: album.AlbumMBID,
		MusicBrainzTrackID: track.MusicBrainzID, MusicBrainzRecordingID: track.RecordingID,
	}
	if err := writeTags(ctx, filepath.Join(s.downloadsDir, filepath.FromSlash(source)), stagePath, tags, ext); err != nil {
		return p, err
	}
	after, err := filecheck.Capture(ctx, input)
	if err != nil || !filecheck.SameContent(after, before) {
		return p, filecheck.ErrChanged
	}
	current, err := filecheck.Open(downloads, source)
	if err != nil {
		return p, err
	}
	observed, err := filecheck.Capture(ctx, current)
	current.Close()
	if err != nil || !filecheck.SameContent(observed, before) {
		return p, filecheck.ErrChanged
	}
	p.Tags, err = s.prober.ProbeAudio(ctx, stagePath)
	if err != nil {
		return p, err
	}
	if err := verifyEditTags(p.Tags, tags); err != nil {
		return p, err
	}
	p.SHA, p.Size, err = musicDigest(ctx, media, p.Stage)
	if err != nil {
		return p, err
	}
	// A conflicting destination is resolved here, once the staged content is
	// final: differing content is renamed aside under a versioned sibling,
	// identical content is left for deduplication at publish time.
	if err := reconcileMusicDestination(ctx, media, p.Destination, p.SHA); err != nil {
		return p, err
	}
	staged, err := media.OpenFile(p.Stage, os.O_RDWR, 0)
	if err != nil {
		return p, err
	}
	if err := errors.Join(staged.Sync(), staged.Close()); err != nil {
		return p, err
	}
	keep = true
	return p, nil
}

func trackByID(tracks []Track, id int64) *Track {
	for i := range tracks {
		if tracks[i].ID == id {
			return &tracks[i]
		}
	}
	return nil
}

func (s *Service) ImportJob(ctx context.Context, id int64) (ImportJob, error) {
	var job ImportJob
	err := s.store.db.QueryRowContext(ctx, `SELECT id,album_id,root,source_path,state,file_count,imported_count,error,created_at,updated_at FROM music_import_jobs WHERE id=?`, id).
		Scan(&job.ID, &job.AlbumID, &job.Root, &job.SourcePath, &job.State, &job.FileCount, &job.ImportedCount, &job.Error, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return ImportJob{}, err
	}
	rows, err := s.store.db.QueryContext(ctx, `SELECT id,source_path,track_id,destination_path,state,error FROM music_import_files WHERE job_id=? ORDER BY id`, id)
	if err != nil {
		return ImportJob{}, err
	}
	defer rows.Close()
	job.Files = []ImportFile{}
	for rows.Next() {
		var file ImportFile
		if err := rows.Scan(&file.ID, &file.SourcePath, &file.TrackID, &file.DestinationPath, &file.State, &file.Error); err != nil {
			return ImportJob{}, err
		}
		job.Files = append(job.Files, file)
	}
	return job, rows.Err()
}

func (s *Service) ListImports(ctx context.Context, page int) (ImportJobList, error) {
	if page < 1 || page > 100000 {
		return ImportJobList{}, ErrInvalidInput
	}
	const limit = 20
	out := ImportJobList{Items: []ImportJob{}, Page: page, Limit: limit}
	if err := s.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_import_jobs`).Scan(&out.Total); err != nil {
		return ImportJobList{}, err
	}
	rows, err := s.store.db.QueryContext(ctx, `SELECT id,album_id,root,source_path,state,file_count,imported_count,error,created_at,updated_at FROM music_import_jobs ORDER BY id DESC LIMIT ? OFFSET ?`, limit, (page-1)*limit)
	if err != nil {
		return ImportJobList{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var job ImportJob
		if err := rows.Scan(&job.ID, &job.AlbumID, &job.Root, &job.SourcePath, &job.State, &job.FileCount, &job.ImportedCount, &job.Error, &job.CreatedAt, &job.UpdatedAt); err != nil {
			return ImportJobList{}, err
		}
		out.Items = append(out.Items, job)
	}
	return out, rows.Err()
}
