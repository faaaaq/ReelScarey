package music

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	return s.startImport(ctx, albumID, sourcePath, root, nil)
}

// StartImportMapped imports an album folder using an explicit file-index to
// track-ID map. It is used when tags are missing or ambiguous.
func (s *Service) StartImportMapped(ctx context.Context, albumID int64, sourcePath, root string, mapping map[int]int64) (ImportJob, error) {
	if len(mapping) == 0 {
		return ImportJob{}, ErrInvalidInput
	}
	return s.startImport(ctx, albumID, sourcePath, root, mapping)
}

func (s *Service) startImport(ctx context.Context, albumID int64, sourcePath, root string, mapping map[int]int64) (ImportJob, error) {
	if albumID < 1 {
		return ImportJob{}, ErrInvalidInput
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
	result, err := tx.ExecContext(ctx, `INSERT INTO music_import_jobs(album_id,root,source_path,state,file_count,created_at,updated_at) VALUES(?,?,?,'running',?,?,?)`, albumID, root, source, len(plans), now, now)
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
	go s.runImport(context.WithoutCancel(ctx), jobID)
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

func (s *Service) runImport(ctx context.Context, jobID int64) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	var albumID int64
	var root, source string
	if err := s.store.db.QueryRowContext(ctx, `SELECT album_id,root,source_path FROM music_import_jobs WHERE id=?`, jobID).Scan(&albumID, &root, &source); err != nil {
		s.finishImport(ctx, jobID, "failed", err.Error(), 0)
		return
	}
	album, err := s.loadImportAlbum(ctx, albumID)
	if err != nil {
		s.finishImport(ctx, jobID, "failed", err.Error(), 0)
		return
	}
	type item struct {
		id, trackID int64
		source      string
	}
	items := []item{}
	rows, err := s.store.db.QueryContext(ctx, `SELECT id,track_id,source_path FROM music_import_files WHERE job_id=? AND state='pending' ORDER BY id`, jobID)
	if err != nil {
		s.finishImport(ctx, jobID, "failed", err.Error(), 0)
		return
	}
	for rows.Next() {
		var it item
		if rows.Scan(&it.id, &it.trackID, &it.source) == nil {
			items = append(items, it)
		}
	}
	rows.Close()
	imported := 0
	for _, it := range items {
		track := trackByID(album.Tracks, it.trackID)
		if track == nil {
			s.finishImport(ctx, jobID, "failed", "A catalog track disappeared before import.", imported)
			return
		}
		destination, err := s.importFile(ctx, album, *track, root, it.source, jobID, it.id)
		if err != nil {
			_, _ = s.store.db.ExecContext(context.WithoutCancel(ctx), `UPDATE music_import_files SET state='failed',error=?,updated_at=unixepoch() WHERE id=?`, err.Error(), it.id)
			s.finishImport(ctx, jobID, "failed", err.Error(), imported)
			return
		}
		_, _ = s.store.db.ExecContext(context.WithoutCancel(ctx), `UPDATE music_import_files SET state='imported',destination_path=?,updated_at=unixepoch() WHERE id=?`, destination, it.id)
		imported++
	}
	_, _ = s.store.db.ExecContext(context.WithoutCancel(ctx), `UPDATE music_artists SET status='available',updated_at=unixepoch() WHERE id=?`, album.ArtistID)
	_, _ = s.store.db.ExecContext(context.WithoutCancel(ctx), `UPDATE music_albums SET status='available',updated_at=unixepoch() WHERE id=?`, album.AlbumID)
	s.finishImport(ctx, jobID, "completed", "", imported)
}

func (s *Service) importFile(ctx context.Context, album importAlbum, track Track, root, sourceRel string, jobID, fileID int64) (string, error) {
	source := filepath.Join(s.downloadsDir, filepath.FromSlash(sourceRel))
	if !contained(s.downloadsDir, source) {
		return "", ErrInvalidInput
	}
	if _, err := os.Stat(source); err != nil {
		return "", &Error{Code: "music_import_missing_source", Message: "A download file is no longer present. Nothing was imported."}
	}
	ext := filepath.Ext(source)
	prefix := fmt.Sprintf("%02d", track.Number)
	if album.MaxDisc > 1 {
		prefix = fmt.Sprintf("%d-%02d", track.Disc, track.Number)
	}
	destinationRel := path.Join(root, safeName(album.ArtistName), safeName(album.AlbumTitle), prefix+" "+safeName(track.Title)+ext)
	destination := filepath.Join(s.mediaDir, filepath.FromSlash(destinationRel))
	if !contained(s.mediaDir, destination) {
		return "", ErrInvalidInput
	}
	if _, err := os.Stat(destination); err == nil {
		return "", &Error{Code: "music_destination_exists", Message: "A file already exists at the destination. Nothing was overwritten."}
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return "", err
	}
	stage := filepath.Join(filepath.Dir(destination), fmt.Sprintf(".reelscarey-import-%d-%d%s", jobID, fileID, ext))
	tags := EditTags{
		Artist:                    album.ArtistName,
		AlbumArtist:               album.ArtistName,
		Album:                     album.AlbumTitle,
		Title:                     track.Title,
		Track:                     track.Number,
		TrackTotal:                len(album.Tracks),
		Disc:                      track.Disc,
		DiscTotal:                 album.MaxDisc,
		Year:                      album.Year,
		MusicBrainzArtistID:       album.ArtistMBID,
		MusicBrainzReleaseGroupID: album.AlbumMBID,
		MusicBrainzTrackID:        track.MusicBrainzID,
		MusicBrainzRecordingID:    track.RecordingID,
	}
	if err := writeTags(ctx, source, stage, tags, ext); err != nil {
		return "", err
	}
	actual, err := s.prober.ProbeAudio(ctx, stage)
	if err != nil {
		os.Remove(stage)
		return "", err
	}
	if err := verifyEditTags(actual, tags); err != nil {
		os.Remove(stage)
		return "", err
	}
	if err := os.Link(stage, destination); err != nil {
		if err := copyFileExclusive(ctx, stage, destination); err != nil {
			os.Remove(stage)
			return "", err
		}
	}
	os.Remove(stage)
	sha, _ := hashFile(ctx, destination)
	tagJSON, _ := json.Marshal(actual)
	info, err := os.Stat(destination)
	if err != nil {
		return "", err
	}
	if _, err := s.store.db.ExecContext(ctx, `INSERT INTO music_files(artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,sample_rate,channels,duration_ms,tag_json,imported_at,origin,source_name)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,unixepoch(),'download',?)`,
		album.ArtistID, album.AlbumID, track.ID, root, destinationRel, info.Size(), sha, actual.Format, actual.Bitrate, actual.SampleRate, actual.Channels, actual.DurationMS, string(tagJSON), filepath.Base(sourceRel)); err != nil {
		return "", err
	}
	return destinationRel, nil
}

func trackByID(tracks []Track, id int64) *Track {
	for i := range tracks {
		if tracks[i].ID == id {
			return &tracks[i]
		}
	}
	return nil
}

func (s *Service) finishImport(ctx context.Context, jobID int64, state, message string, imported int) {
	_, _ = s.store.db.ExecContext(context.WithoutCancel(ctx), `UPDATE music_import_jobs SET state=?,error=?,imported_count=?,updated_at=unixepoch() WHERE id=?`, state, message, imported, jobID)
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
