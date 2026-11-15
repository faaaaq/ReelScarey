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
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
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
	// Resolution of a conflicting destination recorded when the publication
	// was prepared: identical content deduplicates, differing content was
	// renamed aside to this sibling. importFile reconciles the catalog rows
	// for the recorded (or freshly re-resolved) conflict before registering.
	Deduped bool   `json:"deduped,omitempty"`
	Aside   string `json:"aside,omitempty"`
	// Conversion journals what the effective quality profile did to this
	// file (migration 103 quality profiles). Old rows and profile-less
	// imports leave it empty, so the journal stays backward compatible.
	Conversion *importConversion `json:"conversion,omitempty"`
}

// importConversion is the journal entry for one profile-driven import: which
// profile applied, why conversion was or was not needed, the honest note
// shown on the import job, and where the staged original went under the
// effective retention policy.
type importConversion struct {
	Profile string `json:"profile"`
	// Reason is the ConversionNeeded verdict text for the applied profile.
	Reason string `json:"reason,omitempty"`
	// Note is an honest user-facing message recorded on the import job (a
	// failed conversion that fell back to the original, a lossless request
	// that was unavailable, or an "ask" retention pending review).
	Note string `json:"note,omitempty"`
	// Retention is the effective policy applied to the staged original when
	// a conversion happened: keep|delete|ask.
	Retention string `json:"retention,omitempty"`
	// OriginalStage is the staged original file when the retention policy is
	// delete: the publisher removes it only after the converted file is
	// verified in place, never before and never on failure.
	OriginalStage string `json:"original_stage,omitempty"`
	// Archive describes the retained original published alongside the
	// converted primary file as an archive variant (retention keep/ask).
	Archive *importArchive `json:"archive,omitempty"`
}

// importArchive describes the retained original of a converted import.
type importArchive struct {
	Stage       string    `json:"stage"`
	Destination string    `json:"destination"`
	SHA         string    `json:"sha256"`
	Size        int64     `json:"size"`
	Tags        AudioTags `json:"tags"`
}

// importProfilePlan carries the resolved quality profile into the staging
// step. nil means the source is imported unchanged.
type importProfilePlan struct {
	Profile   QualityProfile
	Retention string // keep|delete|ask
	Fallback  string // best|skip|approve
}

// resolveImportProfileKey maps a stored per-download profile value (the
// acquisition job's format column) onto a profile key, accepting the legacy
// yt-dlp values. Unknown values resolve to ” so a stale row never blocks an
// import; the per-album override or the global preference takes over.
func resolveImportProfileKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	if value == "source" {
		return "original"
	}
	if key, ok := legacyAcquisitionProfiles[value]; ok {
		return key
	}
	if _, ok := ProfileByKey(value); ok {
		return value
	}
	return ""
}

// importProfilePlan resolves the effective quality profile for one import
// job. The per-download key recorded on the linking acquisition job wins,
// then the per-album override, then the global download preference
// (music.ResolveProfile order). Transcoding disabled or an unresolvable
// profile yields nil, which imports the source unchanged.
func (s *Service) importProfilePlan(ctx context.Context, jobID, albumID int64) *importProfilePlan {
	prefs, err := s.GetPreferences(ctx)
	if err != nil || !prefs.TranscodingEnabled {
		return nil
	}
	explicit := ""
	if err := s.store.db.QueryRowContext(ctx, `SELECT j.format FROM music_acquisition_jobs j JOIN music_acquisition_tracks t ON t.job_id=j.id WHERE t.import_job_id=? LIMIT 1`, jobID).Scan(&explicit); err != nil && !errors.Is(err, sql.ErrNoRows) {
		explicit = ""
	}
	override, err := s.AlbumProfileOverride(ctx, albumID)
	if err != nil {
		override = ""
	}
	profile, err := s.resolveStoredProfile(ctx, prefs, resolveImportProfileKey(explicit), strings.TrimSpace(override))
	if err != nil || profile.Codec == "original" {
		return nil
	}
	retention := prefs.OriginalRetention
	if profile.KeepOriginal {
		retention = "keep"
	}
	if retention == "" {
		retention = defaultOriginalRetention
	}
	fallback := prefs.FallbackPolicy
	if fallback == "" {
		fallback = defaultFallbackPolicy
	}
	return &importProfilePlan{Profile: profile, Retention: retention, Fallback: fallback}
}

// storedProfileKey is the value journaled for a profile: the stable builtin
// key, or the custom profile's name (custom rows have no key).
func storedProfileKey(profile QualityProfile) string {
	if profile.Key != "" {
		return profile.Key
	}
	return profile.Name
}

// profileFileExtension is the file extension a profile's output uses.
func profileFileExtension(profile QualityProfile) string {
	if profile.Container != "" {
		return "." + profile.Container
	}
	switch profile.Codec {
	case "flac":
		return ".flac"
	case "mp3":
		return ".mp3"
	case "aac":
		return ".m4a"
	case "opus":
		return ".opus"
	}
	return ""
}

func (s *Service) runImport(ctx context.Context, jobID, albumID int64, folder string) (int64, error) {
	album, err := s.loadImportAlbum(ctx, albumID)
	if err != nil {
		return 0, err
	}
	// The effective quality profile is resolved once per job: the per-download
	// key recorded on the linking acquisition job wins, then the per-album
	// override, then the global download preference. nil imports unchanged.
	plan := s.importProfilePlan(ctx, jobID, albumID)
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
		if err := s.importFile(ctx, media, album, *track, folder, source, jobID, fileID, raw, plan); err != nil {
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
	// Profile-driven imports record honest notes (a failed conversion that
	// fell back to the original, an unavailable lossless request, an "ask"
	// retention pending review) on their file rows; the job surfaces them.
	notes, err := s.importNotes(ctx, jobID)
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE music_import_jobs SET state='completed',error=?,updated_at=unixepoch() WHERE id=?`, notes, jobID); err != nil {
		return 0, err
	}
	return 0, tx.Commit()
}

// importNotes joins the per-file honest notes recorded during a profile-driven
// import so the completed job still reports what actually happened.
func (s *Service) importNotes(ctx context.Context, jobID int64) (string, error) {
	rows, err := s.store.db.QueryContext(ctx, `SELECT error FROM music_import_files WHERE job_id=? AND error<>'' ORDER BY id`, jobID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	notes := []string{}
	seen := map[string]bool{}
	for rows.Next() {
		var note string
		if err := rows.Scan(&note); err != nil {
			return "", err
		}
		if seen[note] {
			continue
		}
		seen[note] = true
		notes = append(notes, note)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return strings.Join(notes, " "), nil
}

func (s *Service) importFile(ctx context.Context, media *os.Root, album importAlbum, track Track, folder, source string, jobID, fileID int64, raw string, plan *importProfilePlan) error {
	var p importPublication
	if raw == "" {
		var err error
		p, err = s.prepareMusicImport(ctx, media, album, track, folder, source, plan)
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
	deduped, aside := p.Deduped, p.Aside
	if !sameMusicFile(media, p.Stage, p.Destination) {
		freshDeduped, freshAside, err := placeMusicStage(ctx, media, p.Destination, p.Stage, p.SHA)
		if err != nil {
			return err
		}
		deduped = deduped || freshDeduped
		if aside == "" {
			aside = freshAside
		}
	} else if aside == "" {
		// The destination already holds identical content and no occupant
		// was renamed aside; any registered row at that path is a duplicate
		// to reconcile below.
		deduped = true
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
	if deduped {
		// The destination already holds identical content that another
		// catalog row may still register. Drop that row so the import can
		// register the destination without violating UNIQUE(relative_path).
		if _, err := tx.ExecContext(ctx, `DELETE FROM music_files WHERE relative_path=?`, p.Destination); err != nil {
			return err
		}
	} else if aside != "" {
		// The previous occupant was renamed aside on disk; repoint its
		// catalog row before registering the destination, otherwise the
		// UNIQUE(relative_path) constraint fails the import and leaves the
		// occupant row pointing at a path that now holds the imported file.
		asideRoot := ""
		if slash := strings.IndexByte(aside, '/'); slash > 0 {
			asideRoot = aside[:slash]
		}
		if _, err := tx.ExecContext(ctx, `UPDATE music_files SET relative_path=?,root=? WHERE relative_path=?`, aside, asideRoot, p.Destination); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO music_files(artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,sample_rate,channels,duration_ms,tag_json,imported_at,origin,source_name)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,unixepoch(),'download',?)`,
		album.ArtistID, album.AlbumID, track.ID, folder, p.Destination, p.Size, p.SHA, p.Tags.Format, p.Tags.Bitrate, p.Tags.SampleRate, p.Tags.Channels, p.Tags.DurationMS, string(tagJSON), path.Base(source)); err != nil {
		return err
	}
	note := ""
	if p.Conversion != nil {
		note = p.Conversion.Note
	}
	if _, err := tx.ExecContext(ctx, `UPDATE music_import_files SET state='imported',destination_path=?,error=?,updated_at=unixepoch() WHERE id=?`, p.Destination, note, fileID); err != nil {
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
	if p.Conversion == nil {
		return nil
	}
	if p.Conversion.Archive != nil {
		// The retention policy kept the original: publish it as an archive
		// variant beside the converted primary file.
		return s.publishImportArchive(ctx, media, album, track, folder, source, p)
	}
	if p.Conversion.OriginalStage != "" {
		// Retention delete: the converted file is now fully imported and
		// verified at its destination, so the staged original may go. It is
		// never removed earlier and never on a failure path.
		_ = media.Remove(p.Conversion.OriginalStage)
	}
	return nil
}

func (s *Service) prepareMusicImport(ctx context.Context, media *os.Root, album importAlbum, track Track, folder, source string, plan *importProfilePlan) (importPublication, error) {
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
	stagedFiles := []string{p.Stage}
	keep := false
	defer func() {
		if !keep {
			for _, name := range stagedFiles {
				_ = media.Remove(name)
			}
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
	// The effective quality profile is applied while the staged copy is
	// still private: a needed conversion replaces the staged file, the
	// retention policy decides where the original goes, and everything is
	// journaled on the publication before it is persisted. A failed
	// conversion never aborts the import: the original is imported
	// unchanged with an honest note instead.
	if plan != nil {
		if err := s.applyImportProfile(ctx, media, &p, *plan); err != nil {
			return p, err
		}
		if p.Stage != stagedFiles[0] {
			stagedFiles = append(stagedFiles, p.Stage)
		}
	}
	p.SHA, p.Size, err = musicDigest(ctx, media, p.Stage)
	if err != nil {
		return p, err
	}
	// A conflicting destination is resolved here, once the staged content is
	// final: differing content is renamed aside under a versioned sibling,
	// identical content is left for deduplication at publish time. The
	// resolution is recorded on the publication so importFile can reconcile
	// the catalog rows even across a journal replay.
	p.Deduped, p.Aside, err = reconcileMusicDestination(ctx, media, p.Destination, p.SHA)
	if err != nil {
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

// applyImportProfile converts the staged original into the effective quality
// profile's target before the publication is finalized. The staged source is
// never rewritten or deleted here: on any conversion failure the original is
// imported unchanged and an honest note is recorded instead, so an existing
// download keeps working when transcoding is unavailable.
func (s *Service) applyImportProfile(ctx context.Context, media *os.Root, p *importPublication, plan importProfilePlan) error {
	profile := plan.Profile
	ensure := func() {
		if p.Conversion == nil {
			p.Conversion = &importConversion{}
		}
	}
	ensure()
	p.Conversion.Profile = storedProfileKey(profile)
	sourceFamily := probeFormatFamily(p.Tags.Format)
	// A lossless-only profile fails closed against a lossy source before any
	// conversion is attempted; the saved fallback policy decides the outcome.
	// The probed tags are the authoritative evidence here; release-name
	// metadata from the search path is only a ranking hint.
	if profile.RequireLossless && !sourceIsLossless(sourceFamily) {
		_, reason := ConversionNeeded(p.Tags, profile)
		p.Conversion.Reason = reason
		switch plan.Fallback {
		case "skip":
			return &Error{Code: "music_lossless_unavailable", Message: "This source is lossy and does not meet the lossless quality profile, so it was not imported."}
		case "approve":
			return &Error{Code: "music_lossless_review", Message: "This source is lossy and does not meet the lossless quality profile. The original download was retained for review; approve a lossy fallback or choose a lossless release, then retry."}
		}
		// best: keep the best available source and store it accurately (the
		// registered row carries the probed format and bitrate), saying
		// honestly that the requested lossless quality was unavailable.
		p.Conversion.Note = "The requested lossless quality was not available; the best available source was kept and stored exactly as probed."
		return nil
	}
	needed, reason := ConversionNeeded(p.Tags, profile)
	p.Conversion.Reason = reason
	if !needed {
		// The staged source already satisfies the profile, so it is imported
		// unchanged instead of being copied onto itself.
		return nil
	}
	if ffmpegPath() == "" {
		p.Conversion.Note = "FFmpeg is not installed on the server, so the original file was imported unchanged."
		return nil
	}
	origExt := path.Ext(p.Destination)
	convExt := profileFileExtension(profile)
	convRel := strings.TrimSuffix(p.Stage, path.Ext(p.Stage)) + "-converted" + convExt
	convAbs := filepath.Join(s.mediaDir, filepath.FromSlash(convRel))
	stageAbs := filepath.Join(s.mediaDir, filepath.FromSlash(p.Stage))
	result, err := s.ConvertFile(ctx, ConvertRequest{SourcePath: stageAbs, OutputPath: convAbs, Profile: profile})
	if err != nil {
		p.Conversion.Note = "The conversion to " + profile.Name + " failed (" + err.Error() + "), so the original file was imported unchanged."
		return nil
	}
	// The staged original is handled per the effective retention policy. The
	// keep and ask variants publish it as an archive variant beside the
	// converted primary; delete records its stage path so the publisher can
	// remove it only after the converted file is verified in place.
	base := strings.TrimSuffix(p.Destination, origExt)
	switch plan.Retention {
	case "keep", "ask":
		sha, size, err := musicDigest(ctx, media, p.Stage)
		if err != nil {
			return err
		}
		archiveDest := base + origExt
		if convExt == origExt {
			archiveDest = base + " (original)" + origExt
		}
		p.Conversion.Archive = &importArchive{Stage: p.Stage, Destination: archiveDest, SHA: sha, Size: size, Tags: p.Tags}
		p.Conversion.Retention = plan.Retention
		if plan.Retention == "ask" {
			p.Conversion.Note = "The original " + strings.TrimPrefix(origExt, ".") + " file was retained beside the converted " + profile.Name + " copy pending review."
		}
	default:
		p.Conversion.OriginalStage = p.Stage
		p.Conversion.Retention = plan.Retention
	}
	p.Stage = convRel
	p.Tags = result.Tags
	p.SHA = result.SHA256
	p.Size = result.SizeBytes
	p.Destination = base + convExt
	return nil
}

// publishImportArchive registers the retained original of a converted import
// as an archive variant beside the converted primary file. It follows the
// same no-overwrite verification and conflict reconciliation as the primary
// publication, and the journal (importConversion.Archive) makes a replay
// after a crash idempotent.
func (s *Service) publishImportArchive(ctx context.Context, media *os.Root, album importAlbum, track Track, folder, source string, p importPublication) error {
	archive := p.Conversion.Archive
	if archive.Stage == "" || archive.Destination == "" {
		return nil
	}
	if err := expectMusicDigest(ctx, media, archive.Stage, archive.SHA); err != nil {
		return errors.New("The retained original changed. Inspect it before retrying.")
	}
	deduped, aside := false, ""
	if !sameMusicFile(media, archive.Stage, archive.Destination) {
		var err error
		deduped, aside, err = placeMusicStage(ctx, media, archive.Destination, archive.Stage, archive.SHA)
		if err != nil {
			return err
		}
	} else {
		// The destination already holds identical content; any registered
		// row there is a duplicate to reconcile below.
		deduped = true
	}
	if err := expectMusicDigest(ctx, media, archive.Destination, archive.SHA); err != nil {
		return filecheck.ErrChanged
	}
	tagJSON, err := json.Marshal(archive.Tags)
	if err != nil {
		return err
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if deduped {
		// Identical content at the destination: drop the stale row so the
		// archive variant can register without violating
		// UNIQUE(relative_path).
		if _, err := tx.ExecContext(ctx, `DELETE FROM music_files WHERE relative_path=?`, archive.Destination); err != nil {
			return err
		}
	} else if aside != "" {
		asideRoot := ""
		if slash := strings.IndexByte(aside, '/'); slash > 0 {
			asideRoot = aside[:slash]
		}
		if _, err := tx.ExecContext(ctx, `UPDATE music_files SET relative_path=?,root=? WHERE relative_path=?`, aside, asideRoot, archive.Destination); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO music_files(artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,sample_rate,channels,duration_ms,tag_json,imported_at,origin,source_name,variant_role,variant_profile)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,unixepoch(),'download',?,'archive','')`,
		album.ArtistID, album.AlbumID, track.ID, folder, archive.Destination, archive.Size, archive.SHA, archive.Tags.Format, archive.Tags.Bitrate, archive.Tags.SampleRate, archive.Tags.Channels, archive.Tags.DurationMS, string(tagJSON), path.Base(source)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_ = media.Remove(archive.Stage)
	return nil
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
