package music

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/filecheck"
	"github.com/faaaaq/ReelScarey/internal/organization"
)

const (
	MaxScanFiles = 20000
	MaxScanDepth = 24
)

var audioExtensions = map[string]bool{
	".mp3": true, ".flac": true, ".m4a": true, ".mp4": true, ".ogg": true,
	".opus": true, ".wav": true, ".aiff": true, ".aif": true, ".wma": true,
}

var (
	errScanLimit     = errors.New("The folder contains more than 20,000 audio files. Narrow the folder.")
	errScanCancelled = errors.New("The scan was cancelled.")
)

type Scan struct {
	ID              int64  `json:"id"`
	Root            string `json:"root"`
	State           string `json:"state"`
	FileCount       int    `json:"file_count"`
	MatchedCount    int    `json:"matched_count"`
	UnresolvedCount int    `json:"unresolved_count"`
	RegisteredCount int    `json:"registered_count"`
	Error           string `json:"error"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
}

type ScanFile struct {
	ID                        int64  `json:"id"`
	ScanID                    int64  `json:"scan_id"`
	RelativePath              string `json:"relative_path"`
	SizeBytes                 int64  `json:"size_bytes"`
	ModifiedAt                int64  `json:"modified_at"`
	SHA256                    string `json:"sha256"`
	Format                    string `json:"format"`
	DurationMS                int    `json:"duration_ms"`
	Bitrate                   int    `json:"bitrate"`
	SampleRate                int    `json:"sample_rate"`
	Channels                  int    `json:"channels"`
	Artist                    string `json:"tag_artist"`
	AlbumArtist               string `json:"tag_album_artist"`
	Album                     string `json:"tag_album"`
	Title                     string `json:"tag_title"`
	Track                     *int   `json:"tag_track"`
	Disc                      *int   `json:"tag_disc"`
	Year                      *int   `json:"tag_year"`
	MusicBrainzArtistID       string `json:"tag_musicbrainz_artist"`
	MusicBrainzAlbumID        string `json:"tag_musicbrainz_album"`
	MusicBrainzReleaseGroupID string `json:"tag_musicbrainz_release_group"`
	MusicBrainzTrackID        string `json:"tag_musicbrainz_track"`
	MusicBrainzRecordingID    string `json:"tag_musicbrainz_recording"`
	TagJSON                   string `json:"tag_json"`
	ArtistID                  *int64 `json:"artist_id"`
	AlbumID                   *int64 `json:"album_id"`
	TrackID                   *int64 `json:"track_id"`
	State                     string `json:"state"`
	Error                     string `json:"error"`
}

type ScanList struct {
	Items []Scan `json:"items"`
	Page  int    `json:"page"`
	Total int    `json:"total"`
	Limit int    `json:"limit"`
}

type ScanPage struct {
	Scan  Scan       `json:"scan"`
	Items []ScanFile `json:"items"`
	Page  int        `json:"page"`
	Total int        `json:"total"`
	Limit int        `json:"limit"`
}

const scanColumns = `id, root, state, file_count, matched_count, unresolved_count, registered_count, error, created_at, updated_at`
const scanFileColumns = `id, scan_id, relative_path, size_bytes, modified_at, sha256, format, duration_ms, bitrate, sample_rate, channels, tag_artist, tag_album_artist, tag_album, tag_title, tag_track, tag_disc, tag_year, tag_musicbrainz_artist, tag_musicbrainz_album, tag_musicbrainz_release_group, tag_musicbrainz_track, tag_musicbrainz_recording, tag_json, artist_id, album_id, track_id, state, error`

func scanScan(row interface{ Scan(...any) error }) (Scan, error) {
	var s Scan
	err := row.Scan(&s.ID, &s.Root, &s.State, &s.FileCount, &s.MatchedCount, &s.UnresolvedCount, &s.RegisteredCount, &s.Error, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

func scanScanFile(row interface{ Scan(...any) error }) (ScanFile, error) {
	var f ScanFile
	err := row.Scan(&f.ID, &f.ScanID, &f.RelativePath, &f.SizeBytes, &f.ModifiedAt, &f.SHA256, &f.Format, &f.DurationMS, &f.Bitrate, &f.SampleRate, &f.Channels, &f.Artist, &f.AlbumArtist, &f.Album, &f.Title, &f.Track, &f.Disc, &f.Year, &f.MusicBrainzArtistID, &f.MusicBrainzAlbumID, &f.MusicBrainzReleaseGroupID, &f.MusicBrainzTrackID, &f.MusicBrainzRecordingID, &f.TagJSON, &f.ArtistID, &f.AlbumID, &f.TrackID, &f.State, &f.Error)
	return f, err
}

func (s *Service) StartScan(ctx context.Context, root string) (Scan, error) {
	clean, err := s.cleanScanRoot(root)
	if err != nil {
		return Scan{}, err
	}
	now := time.Now().Unix()
	result, err := s.store.db.ExecContext(ctx, `INSERT INTO music_scans(root,state,created_at,updated_at) VALUES(?,'scanning',?,?)`, clean, now, now)
	if err != nil {
		return Scan{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Scan{}, err
	}
	go s.runScan(context.WithoutCancel(ctx), id, clean)
	return s.Scan(ctx, id)
}

func (s *Service) cleanScanRoot(root string) (string, error) {
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
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", &Error{Code: "music_scan_root", Message: "Choose an existing folder below the media root."}
	}
	return clean, nil
}

func (s *Service) runScan(ctx context.Context, id int64, root string) {
	defer organization.ReadMedia()()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	state, message := "review", ""
	if err := s.walkScan(ctx, id, root); err != nil {
		state = "failed"
		message = err.Error()
		if errors.Is(err, errScanCancelled) || errors.Is(err, context.Canceled) {
			state = "cancelled"
			message = ""
		}
	} else if err := s.matchScan(ctx, id); err != nil {
		state = "failed"
		message = err.Error()
	}
	var files, matched, unresolved int
	if err := s.store.db.QueryRowContext(context.WithoutCancel(ctx), `SELECT COUNT(*), COALESCE(SUM(state='matched'),0), COALESCE(SUM(state='unresolved'),0) FROM music_scan_files WHERE scan_id=?`, id).Scan(&files, &matched, &unresolved); err != nil && state == "review" {
		state, message = "failed", err.Error()
	}
	if state == "review" && files == 0 {
		state, message = "failed", "No audio files were found in this folder."
	}
	_, _ = s.store.db.ExecContext(context.WithoutCancel(ctx), `UPDATE music_scans SET state=?,file_count=?,matched_count=?,unresolved_count=?,error=?,updated_at=unixepoch() WHERE id=?`, state, files, matched, unresolved, message, id)
}

func (s *Service) walkScan(ctx context.Context, id int64, root string) error {
	absRoot := filepath.Join(s.mediaDir, filepath.FromSlash(root))
	media, err := os.OpenRoot(s.mediaDir)
	if err != nil {
		return err
	}
	defer media.Close()
	count := 0
	batch := make([]audioObservation, 0, 128)
	err = filepath.WalkDir(absRoot, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".reelscarey" {
				return filepath.SkipDir
			}
			rel, e := filepath.Rel(absRoot, name)
			if e == nil && rel != "." && strings.Count(rel, string(filepath.Separator)) >= MaxScanDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !audioExtensions[strings.ToLower(filepath.Ext(name))] {
			return nil
		}
		if count >= MaxScanFiles {
			return errScanLimit
		}
		if count%50 == 0 {
			cancelled, e := s.scanCancelled(ctx, id)
			if e != nil {
				return e
			}
			if cancelled {
				return errScanCancelled
			}
		}
		info, e := entry.Info()
		if e != nil {
			return nil
		}
		relative, e := filepath.Rel(s.mediaDir, name)
		if e != nil || !contained(s.mediaDir, name) {
			return nil
		}
		relative = filepath.ToSlash(relative)
		observation, err := s.observeAudio(ctx, media, relative)
		if err != nil {
			return err
		}
		observation.relative = relative
		if observation.size == 0 {
			observation.size, observation.modified = info.Size(), info.ModTime().Unix()
		}
		batch = append(batch, observation)
		count++
		if len(batch) == cap(batch) {
			if err := s.saveAudioObservations(ctx, id, batch); err != nil {
				return err
			}
			batch = batch[:0]
		}
		return nil
	})
	if err == nil || errors.Is(err, errScanLimit) {
		err = errors.Join(err, s.saveAudioObservations(ctx, id, batch))
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("The scan took too long. Narrow the folder and try again.")
	}
	return err
}

func (s *Service) matchScan(ctx context.Context, id int64) error {
	rows, err := s.store.db.QueryContext(ctx, `SELECT `+scanFileColumns+` FROM music_scan_files WHERE scan_id=? AND state='unresolved' AND error='' ORDER BY id`, id)
	if err != nil {
		return err
	}
	files := []ScanFile{}
	for rows.Next() {
		file, e := scanScanFile(rows)
		if e != nil {
			rows.Close()
			return e
		}
		files = append(files, file)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, file := range files {
		artistID, albumID, trackID, matchErr := s.matchFile(ctx, file)
		state, message := "matched", ""
		if matchErr != nil {
			state, message = "unresolved", matchErr.Error()
			artistID, albumID, trackID = 0, 0, 0
		}
		if _, err := s.store.db.ExecContext(ctx, `UPDATE music_scan_files SET artist_id=?,album_id=?,track_id=?,state=?,error=?,updated_at=unixepoch() WHERE id=?`,
			nullableID(artistID), nullableID(albumID), nullableID(trackID), state, message, file.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) matchFile(ctx context.Context, file ScanFile) (int64, int64, int64, error) {
	var artistID, albumID int64
	if file.MusicBrainzReleaseGroupID != "" {
		err := s.store.db.QueryRowContext(ctx, `SELECT artist_id,id FROM music_albums WHERE musicbrainz_id=?`, file.MusicBrainzReleaseGroupID).Scan(&artistID, &albumID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, 0, 0, err
		}
	}
	if albumID < 1 && file.Album != "" {
		artist := firstNonEmpty(file.AlbumArtist, file.Artist)
		if artist != "" {
			err := s.store.db.QueryRowContext(ctx, `SELECT a.artist_id,a.id FROM music_albums a JOIN music_artists ar ON ar.id=a.artist_id WHERE lower(trim(a.title))=lower(?) AND lower(trim(ar.name))=lower(?) ORDER BY a.id LIMIT 1`, file.Album, artist).Scan(&artistID, &albumID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return 0, 0, 0, err
			}
		}
	}
	if albumID < 1 {
		return 0, 0, 0, &Error{Code: "music_unmatched", Message: "No catalog album matches this file's tags. Add the artist first or tag the file with MusicBrainz IDs."}
	}
	var tracks int
	if err := s.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_tracks WHERE album_id=?`, albumID).Scan(&tracks); err != nil {
		return 0, 0, 0, err
	}
	if tracks == 0 {
		if _, err := s.Album(ctx, albumID); err != nil {
			return 0, 0, 0, err
		}
	}
	queries := []struct {
		query string
		args  []any
	}{}
	if file.MusicBrainzTrackID != "" {
		queries = append(queries, struct {
			query string
			args  []any
		}{`SELECT id FROM music_tracks WHERE album_id=? AND musicbrainz_id=?`, []any{albumID, file.MusicBrainzTrackID}})
	}
	if file.MusicBrainzRecordingID != "" {
		queries = append(queries, struct {
			query string
			args  []any
		}{`SELECT id FROM music_tracks WHERE album_id=? AND recording_id=?`, []any{albumID, file.MusicBrainzRecordingID}})
	}
	if file.Track != nil && file.Disc != nil {
		queries = append(queries, struct {
			query string
			args  []any
		}{`SELECT id FROM music_tracks WHERE album_id=? AND disc=? AND number=?`, []any{albumID, *file.Disc, *file.Track}})
	}
	if file.Title != "" {
		queries = append(queries, struct {
			query string
			args  []any
		}{`SELECT id FROM music_tracks WHERE album_id=? AND lower(trim(title))=lower(?) ORDER BY disc,number,id LIMIT 1`, []any{albumID, file.Title}})
	}
	var trackID int64
	for _, candidate := range queries {
		if err := s.store.db.QueryRowContext(ctx, candidate.query, candidate.args...).Scan(&trackID); err == nil && trackID > 0 {
			break
		}
		trackID = 0
	}
	if trackID < 1 {
		return 0, 0, 0, &Error{Code: "music_unmatched_track", Message: "The album matched but no track matches this file's number or title."}
	}
	return artistID, albumID, trackID, nil
}

func (s *Service) RegisterScan(ctx context.Context, scanID int64, fileIDs []int64) (int, error) {
	defer organization.ReadMedia()()
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	if scanID < 1 || len(fileIDs) == 0 || len(fileIDs) > 500 {
		return 0, ErrInvalidInput
	}
	seen := map[int64]bool{}
	ids := []int64{}
	for _, id := range fileIDs {
		if id > 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return 0, ErrInvalidInput
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var root string
	if err := tx.QueryRowContext(ctx, `SELECT root FROM music_scans WHERE id=?`, scanID).Scan(&root); err != nil {
		return 0, err
	}
	registered := 0
	for _, id := range ids {
		file, err := scanScanFile(tx.QueryRowContext(ctx, `SELECT `+scanFileColumns+` FROM music_scan_files WHERE id=? AND scan_id=?`, id, scanID))
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if file.State != "matched" || file.ArtistID == nil || file.AlbumID == nil || file.TrackID == nil {
			continue
		}
		abs := filepath.Join(s.mediaDir, filepath.FromSlash(file.RelativePath))
		if !contained(s.mediaDir, abs) {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil || info.Size() != file.SizeBytes || info.ModTime().Unix() != file.ModifiedAt {
			if _, err := tx.ExecContext(ctx, `UPDATE music_scan_files SET state='unresolved', error='The file changed after the scan. Rescan it.', updated_at=unixepoch() WHERE id=?`, id); err != nil {
				return 0, err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO music_files(artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,sample_rate,channels,duration_ms,tag_json,imported_at,origin,source_name)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,unixepoch(),'existing',?)
			ON CONFLICT(relative_path) DO UPDATE SET artist_id=excluded.artist_id,album_id=excluded.album_id,track_id=excluded.track_id,root=excluded.root,size_bytes=excluded.size_bytes,sha256=excluded.sha256,format=excluded.format,bitrate=excluded.bitrate,sample_rate=excluded.sample_rate,channels=excluded.channels,duration_ms=excluded.duration_ms,tag_json=excluded.tag_json,imported_at=excluded.imported_at`,
			*file.ArtistID, *file.AlbumID, *file.TrackID, root, file.RelativePath, file.SizeBytes, file.SHA256, file.Format, file.Bitrate, file.SampleRate, file.Channels, file.DurationMS, file.TagJSON, filepath.Base(file.RelativePath)); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE music_scan_files SET state='registered', updated_at=unixepoch() WHERE id=?`, id); err != nil {
			return 0, err
		}
		registered++
	}
	if _, err := tx.ExecContext(ctx, `UPDATE music_artists SET status='available', updated_at=unixepoch() WHERE id IN (SELECT DISTINCT artist_id FROM music_files)`); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE music_albums SET status='available', updated_at=unixepoch() WHERE id IN (SELECT DISTINCT album_id FROM music_files)`); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE music_scans SET registered_count=registered_count+?, updated_at=unixepoch() WHERE id=?`, registered, scanID); err != nil {
		return 0, err
	}
	return registered, tx.Commit()
}

func (s *Service) ListScans(ctx context.Context, page int) (ScanList, error) {
	if page < 1 || page > 100000 {
		return ScanList{}, ErrInvalidInput
	}
	const limit = 20
	out := ScanList{Items: []Scan{}, Page: page, Limit: limit}
	if err := s.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_scans`).Scan(&out.Total); err != nil {
		return ScanList{}, err
	}
	rows, err := s.store.db.QueryContext(ctx, `SELECT `+scanColumns+` FROM music_scans ORDER BY id DESC LIMIT ? OFFSET ?`, limit, (page-1)*limit)
	if err != nil {
		return ScanList{}, err
	}
	defer rows.Close()
	for rows.Next() {
		scan, err := scanScan(rows)
		if err != nil {
			return ScanList{}, err
		}
		out.Items = append(out.Items, scan)
	}
	return out, rows.Err()
}

func (s *Service) Scan(ctx context.Context, id int64) (Scan, error) {
	return scanScan(s.store.db.QueryRowContext(ctx, `SELECT `+scanColumns+` FROM music_scans WHERE id=?`, id))
}

func (s *Service) ScanPage(ctx context.Context, id int64, page int) (ScanPage, error) {
	if page < 1 || page > 100000 {
		return ScanPage{}, ErrInvalidInput
	}
	scan, err := s.Scan(ctx, id)
	if err != nil {
		return ScanPage{}, err
	}
	const limit = 100
	out := ScanPage{Scan: scan, Items: []ScanFile{}, Page: page, Limit: limit}
	if err := s.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_scan_files WHERE scan_id=?`, id).Scan(&out.Total); err != nil {
		return ScanPage{}, err
	}
	rows, err := s.store.db.QueryContext(ctx, `SELECT `+scanFileColumns+` FROM music_scan_files WHERE scan_id=? ORDER BY relative_path LIMIT ? OFFSET ?`, id, limit, (page-1)*limit)
	if err != nil {
		return ScanPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		file, err := scanScanFile(rows)
		if err != nil {
			return ScanPage{}, err
		}
		out.Items = append(out.Items, file)
	}
	return out, rows.Err()
}

func (s *Service) CancelScan(ctx context.Context, id int64) error {
	result, err := s.store.db.ExecContext(ctx, `UPDATE music_scans SET state='cancelled', updated_at=unixepoch() WHERE id=? AND state IN ('scanning','review')`, id)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Service) SkipScanFile(ctx context.Context, scanID, fileID int64) error {
	result, err := s.store.db.ExecContext(ctx, `UPDATE music_scan_files SET state='skipped', updated_at=unixepoch() WHERE id=? AND scan_id=? AND state IN ('matched','unresolved')`, fileID, scanID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Service) scanCancelled(ctx context.Context, id int64) (bool, error) {
	var state string
	if err := s.store.db.QueryRowContext(ctx, `SELECT state FROM music_scans WHERE id=?`, id).Scan(&state); err != nil {
		return false, err
	}
	return state == "cancelled", nil
}

type audioObservation struct {
	relative, sha, stamp, tagJSON, message string
	size, modified                         int64
	tags                                   AudioTags
}

func (s *Service) observeAudio(ctx context.Context, media *os.Root, name string) (audioObservation, error) {
	out := audioObservation{tagJSON: "{}"}
	file, err := filecheck.Open(media, name)
	if err != nil {
		out.message = "The audio file could not be opened safely."
		return out, nil
	}
	defer file.Close()
	before, err := filecheck.Capture(ctx, file)
	if err != nil {
		out.message = "The audio file changed while scanning."
		return out, nil
	}
	out.size, out.modified = before.Size, before.Mtime/int64(time.Second)
	data, err := json.Marshal(before)
	if err != nil {
		return out, err
	}
	var previous, previousSHA, previousTags string
	err = s.store.db.QueryRowContext(ctx, `SELECT verification_json,sha256,tag_json FROM music_scan_files
		WHERE relative_path=? AND verification_json<>'' AND sha256<>'' ORDER BY id DESC LIMIT 1`, name).
		Scan(&previous, &previousSHA, &previousTags)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	out.stamp = string(data)
	if previous == out.stamp && len(previousSHA) == 64 && json.Unmarshal([]byte(previousTags), &out.tags) == nil {
		out.sha, out.tagJSON = previousSHA, previousTags
		return out, nil
	}
	out.tags, err = s.prober.ProbeAudio(ctx, filepath.Join(s.mediaDir, filepath.FromSlash(name)))
	if err != nil {
		out.tags = AudioTags{}
		out.message = err.Error()
		return out, nil
	}
	out.sha, err = filecheck.Digest(ctx, file)
	if err != nil {
		return out, err
	}
	after, err := filecheck.Capture(ctx, file)
	visible, statErr := media.Lstat(name)
	opened, openErr := file.Stat()
	if err != nil || statErr != nil || openErr != nil || !os.SameFile(visible, opened) || after != before {
		return audioObservation{tagJSON: "{}", message: "The audio file changed while scanning."}, nil
	}
	tags, err := json.Marshal(out.tags)
	out.tagJSON = string(tags)
	return out, err
}

func (s *Service) saveAudioObservations(ctx context.Context, id int64, batch []audioObservation) error {
	if len(batch) == 0 {
		return nil
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO music_scan_files(scan_id,relative_path,size_bytes,modified_at,sha256,format,duration_ms,bitrate,sample_rate,channels,tag_artist,tag_album_artist,tag_album,tag_title,tag_track,tag_disc,tag_year,tag_musicbrainz_artist,tag_musicbrainz_album,tag_musicbrainz_release_group,tag_musicbrainz_track,tag_musicbrainz_recording,tag_json,state,error,verification_json,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'unresolved',?,?,unixepoch(),unixepoch())
		ON CONFLICT(scan_id,relative_path) DO NOTHING`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, f := range batch {
		t := f.tags
		if _, err := stmt.ExecContext(ctx, id, f.relative, f.size, f.modified, f.sha, t.Format, t.DurationMS, t.Bitrate, t.SampleRate, t.Channels,
			t.Artist, t.AlbumArtist, t.Album, t.Title, t.Track, t.Disc, t.Year, t.MusicBrainzArtistID, t.MusicBrainzAlbumID,
			t.MusicBrainzReleaseGroupID, t.MusicBrainzTrackID, t.MusicBrainzRecordingID, f.tagJSON, f.message, f.stamp); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func nullableID(value int64) any {
	if value < 1 {
		return nil
	}
	return value
}

func contained(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
