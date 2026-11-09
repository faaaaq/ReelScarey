package music

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/organization"
)

var ErrPlanChanged = errors.New("The catalog changed after this review. Preview the files again.")

type EditTags struct {
	Artist                    string `json:"artist"`
	AlbumArtist               string `json:"album_artist"`
	Album                     string `json:"album"`
	Title                     string `json:"title"`
	Track                     int    `json:"track"`
	TrackTotal                int    `json:"track_total"`
	Disc                      int    `json:"disc"`
	DiscTotal                 int    `json:"disc_total"`
	Year                      int    `json:"year"`
	MusicBrainzArtistID       string `json:"musicbrainz_artist_id"`
	MusicBrainzAlbumID        string `json:"musicbrainz_album_id"`
	MusicBrainzReleaseGroupID string `json:"musicbrainz_release_group_id"`
	MusicBrainzTrackID        string `json:"musicbrainz_track_id"`
	MusicBrainzRecordingID    string `json:"musicbrainz_recording_id"`
}

type EditFile struct {
	MusicFileID     int64    `json:"music_file_id"`
	SourcePath      string   `json:"source_path"`
	DestinationPath string   `json:"destination_path"`
	Action          string   `json:"action"`
	Tags            EditTags `json:"tags"`
	PreviousTagJSON string   `json:"-"`
}

type EditPlan struct {
	Files    []EditFile `json:"files"`
	PlanHash string     `json:"plan_hash"`
}

type EditJob struct {
	ID        int64         `json:"id"`
	State     string        `json:"state"`
	FileCount int           `json:"file_count"`
	Error     string        `json:"error"`
	CreatedAt int64         `json:"created_at"`
	UpdatedAt int64         `json:"updated_at"`
	Files     []EditJobFile `json:"files,omitempty"`
}

type EditJobFile struct {
	MusicFileID     int64  `json:"music_file_id"`
	SourcePath      string `json:"source_path"`
	DestinationPath string `json:"destination_path"`
	Action          string `json:"action"`
	State           string `json:"state"`
	Error           string `json:"error"`
}

type EditJobList struct {
	Items []EditJob `json:"items"`
	Page  int       `json:"page"`
	Total int       `json:"total"`
	Limit int       `json:"limit"`
}

func (s *Service) PreviewEdits(ctx context.Context, fileIDs []int64) (EditPlan, error) {
	files, err := s.loadEditFiles(ctx, fileIDs)
	if err != nil {
		return EditPlan{}, err
	}
	return EditPlan{Files: files, PlanHash: editPlanHash(files)}, nil
}

func (s *Service) loadEditFiles(ctx context.Context, fileIDs []int64) ([]EditFile, error) {
	if len(fileIDs) == 0 || len(fileIDs) > 500 {
		return nil, ErrInvalidInput
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
		return nil, ErrInvalidInput
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.store.db.QueryContext(ctx, `SELECT f.id, f.root, f.relative_path, f.tag_json,
		ar.name, ar.musicbrainz_id, a.title, a.year, a.musicbrainz_id,
		t.title, t.disc, t.number, t.musicbrainz_id, t.recording_id,
		(SELECT COUNT(*) FROM music_tracks x WHERE x.album_id=f.album_id),
		(SELECT COALESCE(MAX(x.disc),1) FROM music_tracks x WHERE x.album_id=f.album_id)
		FROM music_files f
		JOIN music_artists ar ON ar.id=f.artist_id
		JOIN music_albums a ON a.id=f.album_id
		LEFT JOIN music_tracks t ON t.id=f.track_id
		WHERE f.id IN (`+placeholders+`)
		ORDER BY f.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := []EditFile{}
	for rows.Next() {
		var id int64
		var root, relative, tagJSON, artistName, artistMBID, albumTitle, albumMBID string
		var trackTitle, trackMBID, recordingID sql.NullString
		var year, disc, number, trackTotal, discTotal sql.NullInt64
		if err := rows.Scan(&id, &root, &relative, &tagJSON, &artistName, &artistMBID, &albumTitle, &year, &albumMBID, &trackTitle, &disc, &number, &trackMBID, &recordingID, &trackTotal, &discTotal); err != nil {
			return nil, err
		}
		if !trackTitle.Valid || !number.Valid || number.Int64 < 1 {
			return nil, &Error{Code: "music_no_track", Message: "A selected file has no matched catalog track. Rescan and match it first."}
		}
		discN := int(disc.Int64)
		if discN < 1 {
			discN = 1
		}
		root = strings.Trim(strings.ReplaceAll(root, "\\", "/"), "/")
		if root == "" {
			if slash := strings.IndexByte(relative, '/'); slash > 0 {
				root = relative[:slash]
			} else {
				root = "Music"
			}
		}
		ext := filepath.Ext(relative)
		prefix := fmt.Sprintf("%02d", number.Int64)
		if discTotal.Int64 > 1 {
			prefix = fmt.Sprintf("%d-%02d", discN, number.Int64)
		}
		destination := path.Join(root, safeName(artistName), safeName(albumTitle), prefix+" "+safeName(trackTitle.String)+ext)
		tags := EditTags{
			Artist:      artistName,
			AlbumArtist: artistName,
			Album:       albumTitle,
			Title:       trackTitle.String,
			Track:       int(number.Int64),
			TrackTotal:  int(trackTotal.Int64),
			Disc:        discN,
			DiscTotal:   int(discTotal.Int64),
			Year:        int(year.Int64),
			// Local tag identities are catalog keys, not MusicBrainz IDs.
			// Only genuine values may be written back to a file's tags.
			MusicBrainzArtistID:       genuineMBID(artistMBID),
			MusicBrainzReleaseGroupID: genuineMBID(albumMBID),
			MusicBrainzTrackID:        genuineMBID(trackMBID.String),
			MusicBrainzRecordingID:    genuineMBID(recordingID.String),
		}
		retag := needsRetag(tagJSON, tags)
		move := destination != relative
		action := ""
		switch {
		case move && retag:
			action = "move_retag"
		case move:
			action = "move"
		case retag:
			action = "retag"
		default:
			continue
		}
		files = append(files, EditFile{
			MusicFileID:     id,
			SourcePath:      relative,
			DestinationPath: destination,
			Action:          action,
			Tags:            tags,
			PreviousTagJSON: tagJSON,
		})
	}
	return files, rows.Err()
}

func needsRetag(currentJSON string, target EditTags) bool {
	var current AudioTags
	if json.Unmarshal([]byte(currentJSON), &current) != nil {
		return true
	}
	if !equalFold(current.Artist, target.Artist) || !equalFold(current.AlbumArtist, target.AlbumArtist) ||
		!equalFold(current.Album, target.Album) || !equalFold(current.Title, target.Title) {
		return true
	}
	if current.Track == nil || *current.Track != target.Track || current.Disc == nil || *current.Disc != target.Disc {
		return true
	}
	if target.Year > 0 && (current.Year == nil || *current.Year != target.Year) {
		return true
	}
	for _, pair := range [][2]string{
		{current.MusicBrainzArtistID, target.MusicBrainzArtistID},
		{current.MusicBrainzReleaseGroupID, target.MusicBrainzReleaseGroupID},
		{current.MusicBrainzTrackID, target.MusicBrainzTrackID},
		{current.MusicBrainzRecordingID, target.MusicBrainzRecordingID},
	} {
		if pair[1] != "" && !equalFold(pair[0], pair[1]) {
			return true
		}
	}
	return false
}

func editPlanHash(files []EditFile) string {
	data, _ := json.Marshal(files)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func safeName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Unknown"
	}
	value = strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "", "?", "", "\"", "", "<", "", ">", "", "|", "-").Replace(value)
	value = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, value)
	value = strings.Trim(value, ". ")
	if value == "" {
		return "Unknown"
	}
	runes := []rune(value)
	if len(runes) > 120 {
		value = string(runes[:120])
	}
	return value
}

func (s *Service) ApplyEdits(ctx context.Context, fileIDs []int64, planHash string, reviewed bool) (EditJob, error) {
	if !reviewed {
		return EditJob{}, ErrInvalidInput
	}
	plan, err := s.PreviewEdits(ctx, fileIDs)
	if err != nil {
		return EditJob{}, err
	}
	if plan.PlanHash != planHash {
		return EditJob{}, ErrPlanChanged
	}
	if len(plan.Files) == 0 {
		return EditJob{}, &Error{Code: "music_nothing_to_do", Message: "These files are already organized and tagged."}
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return EditJob{}, err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	result, err := tx.ExecContext(ctx, `INSERT INTO music_edit_jobs(state,file_count,created_at,updated_at) VALUES('running',?,?,?)`, len(plan.Files), now, now)
	if err != nil {
		return EditJob{}, err
	}
	jobID, err := result.LastInsertId()
	if err != nil {
		return EditJob{}, err
	}
	for _, file := range plan.Files {
		tagsJSON, _ := json.Marshal(file.Tags)
		if _, err := tx.ExecContext(ctx, `INSERT INTO music_edit_files(job_id,music_file_id,source_path,destination_path,action,tags_json,previous_tag_json,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,'pending',?,?)`,
			jobID, file.MusicFileID, file.SourcePath, file.DestinationPath, file.Action, string(tagsJSON), file.PreviousTagJSON, now, now); err != nil {
			return EditJob{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return EditJob{}, err
	}
	go s.runEditJob(context.WithoutCancel(ctx), jobID)
	return s.EditJob(ctx, jobID)
}

func (s *Service) runEditJob(ctx context.Context, jobID int64) {
	defer organization.ReadMedia()()
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	type item struct {
		id, musicFileID int64
		source, dest    string
		action, tags    string
	}
	items := []item{}
	rows, err := s.store.db.QueryContext(ctx, `SELECT id,music_file_id,source_path,destination_path,action,tags_json FROM music_edit_files WHERE job_id=? AND state='pending' ORDER BY id`, jobID)
	if err == nil {
		for rows.Next() {
			var it item
			if rows.Scan(&it.id, &it.musicFileID, &it.source, &it.dest, &it.action, &it.tags) == nil {
				items = append(items, it)
			}
		}
		rows.Close()
	}
	if err != nil {
		s.finishEditJob(ctx, jobID, "failed", err.Error())
		return
	}
	for _, it := range items {
		if err := s.applyEdit(ctx, jobID, it.id, it.musicFileID, it.source, it.dest, it.action, it.tags); err != nil {
			_, _ = s.store.db.ExecContext(context.WithoutCancel(ctx), `UPDATE music_edit_files SET state='failed',error=?,updated_at=unixepoch() WHERE id=?`, err.Error(), it.id)
			s.finishEditJob(ctx, jobID, "failed", err.Error())
			return
		}
		_, _ = s.store.db.ExecContext(context.WithoutCancel(ctx), `UPDATE music_edit_files SET state='completed',updated_at=unixepoch() WHERE id=?`, it.id)
	}
	// A completed edit whose original copy could not be removed leaves a
	// duplicate on disk. Keep the job completed so rollback stays available, but
	// surface the leftover file in the durable job error instead of only on the
	// file row.
	var leftovers int
	if err := s.store.db.QueryRowContext(context.WithoutCancel(ctx), `SELECT COUNT(*) FROM music_edit_files WHERE job_id=? AND state='completed' AND error<>''`, jobID).Scan(&leftovers); err == nil && leftovers > 0 {
		s.finishEditJob(ctx, jobID, "completed", fmt.Sprintf("%d original file(s) could not be removed; duplicates remain on disk. Review the file list before rolling back.", leftovers))
		return
	}
	s.finishEditJob(ctx, jobID, "completed", "")
}

func (s *Service) finishEditJob(ctx context.Context, jobID int64, state, message string) {
	_, _ = s.store.db.ExecContext(context.WithoutCancel(ctx), `UPDATE music_edit_jobs SET state=?,error=?,updated_at=unixepoch() WHERE id=?`, state, message, jobID)
}

func (s *Service) applyEdit(ctx context.Context, jobID, editID, musicFileID int64, sourceRel, destRel, action, tagsJSON string) error {
	var tags EditTags
	if json.Unmarshal([]byte(tagsJSON), &tags) != nil {
		return ErrInvalidInput
	}
	sourceRel = filepath.ToSlash(sourceRel)
	destRel = filepath.ToSlash(destRel)
	source := filepath.Join(s.mediaDir, filepath.FromSlash(sourceRel))
	dest := filepath.Join(s.mediaDir, filepath.FromSlash(destRel))
	if !contained(s.mediaDir, source) || !contained(s.mediaDir, dest) {
		return ErrInvalidInput
	}
	media, err := os.OpenRoot(s.mediaDir)
	if err != nil {
		return err
	}
	defer media.Close()
	originalSHA, _, err := musicDigest(ctx, media, sourceRel)
	if err != nil {
		return &Error{Code: "music_source_missing", Message: "The source file is no longer present. Nothing was changed."}
	}
	var originalRoot string
	if err := s.store.db.QueryRowContext(ctx, `SELECT root FROM music_files WHERE id=? AND relative_path=?`, musicFileID, sourceRel).Scan(&originalRoot); err != nil {
		return err
	}
	if dest != source {
		if _, err := os.Stat(dest); err == nil {
			return &Error{Code: "music_destination_exists", Message: "A file already exists at the destination. Nothing was overwritten."}
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	ext := filepath.Ext(source)
	stage := filepath.Join(filepath.Dir(dest), fmt.Sprintf(".reelscarey-stage-%d-%d%s", jobID, editID, ext))
	stageRel, err := filepath.Rel(s.mediaDir, stage)
	if err != nil {
		return err
	}
	stageRel = filepath.ToSlash(stageRel)
	retag := action == "retag" || action == "move_retag"
	backup := ""
	var verified AudioTags
	if retag {
		backup = source + ".reelscarey-backup"
		if _, err := os.Stat(backup); err == nil {
			return &Error{Code: "music_backup_exists", Message: "A previous retained copy exists. Review it before retagging this file again."}
		}
		if err := copyFileExclusive(ctx, source, backup); err != nil {
			return err
		}
		stageFile, err := media.OpenFile(stageRel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		if err := stageFile.Close(); err != nil {
			return err
		}
		if err := writeTags(ctx, source, stage, tags, ext); err != nil {
			os.Remove(stage)
			os.Remove(backup)
			return err
		}
		actual, err := s.prober.ProbeAudio(ctx, stage)
		if err != nil {
			os.Remove(backup)
			os.Remove(stage)
			return err
		}
		if err := verifyEditTags(actual, tags); err != nil {
			os.Remove(backup)
			os.Remove(stage)
			return err
		}
		verified = actual
	} else if err := linkOrCopy(ctx, source, stage); err != nil {
		return err
	}
	editedSHA, editedSize, err := musicDigest(ctx, media, stageRel)
	if err != nil {
		return err
	}
	if retag {
		if err := expectMusicDigest(ctx, media, sourceRel+".reelscarey-backup", originalSHA); err != nil {
			return err
		}
	}
	if err := expectMusicDigest(ctx, media, sourceRel, originalSHA); err != nil {
		return err
	}
	if _, err := s.store.db.ExecContext(ctx, `UPDATE music_edit_files SET stage_path=?,backup_path=?,original_sha=?,edited_sha=?,original_root=? WHERE id=?`,
		stage, backup, originalSHA, editedSHA, originalRoot, editID); err != nil {
		return err
	}
	replaced := ""
	if dest == source {
		replaced = sourceRel + ".reelscarey-replaced"
		if err := media.Link(sourceRel, replaced); err != nil {
			return &Error{Code: "music_replace_in_progress", Message: "The original could not be retained without overwriting. Review the retained files before retrying."}
		}
		if !sameMusicFile(media, sourceRel, replaced) {
			return ErrPlanChanged
		}
		if err := media.Remove(sourceRel); err != nil {
			return err
		}
		if err := media.Link(stageRel, destRel); err != nil {
			// Restore only into a vacant name and keep the retained original
			// even if another process occupied the path.
			_ = media.Link(replaced, sourceRel)
			return err
		}
		os.Remove(stage)
	} else {
		if err := os.Link(stage, dest); err != nil {
			if err := copyFileExclusive(ctx, stage, dest); err != nil {
				os.Remove(stage)
				if backup != "" {
					os.Remove(backup)
				}
				return err
			}
		}
		os.Remove(stage)
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	root := destRel
	if slash := strings.IndexByte(root, '/'); slash > 0 {
		root = root[:slash]
	} else {
		root = ""
	}
	if retag {
		tagJSON, _ := json.Marshal(verified)
		if _, err := tx.ExecContext(ctx, `UPDATE music_files SET relative_path=?,root=?,sha256=?,size_bytes=?,tag_json=? WHERE id=?`, destRel, root, editedSHA, editedSize, string(tagJSON), musicFileID); err != nil {
			return err
		}
	} else if _, err := tx.ExecContext(ctx, `UPDATE music_files SET relative_path=?,root=?,sha256=?,size_bytes=? WHERE id=?`, destRel, root, editedSHA, editedSize, musicFileID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE music_edit_files SET state='completed' WHERE id=?`, editID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if replaced != "" && expectMusicDigest(ctx, media, replaced, originalSHA) == nil {
		_ = media.Remove(replaced)
	}
	if dest != source {
		if err := expectMusicDigest(ctx, media, sourceRel, originalSHA); err != nil {
			return err
		}
		if err := os.Remove(source); err != nil {
			_, _ = s.store.db.ExecContext(context.WithoutCancel(ctx), `UPDATE music_edit_files SET error=? WHERE id=?`, "The original file could not be removed and remains on disk: "+err.Error()+" The edit is completed; roll back to compare the two copies.", editID)
		}
	}
	return nil
}

func writeTags(ctx context.Context, source, stage string, tags EditTags, ext string) error {
	args := []string{"-y", "-v", "error", "-i", source, "-c", "copy", "-map_metadata", "0", "-fflags", "+bitexact",
		"-metadata", "artist=" + tags.Artist,
		"-metadata", "album_artist=" + tags.AlbumArtist,
		"-metadata", "album=" + tags.Album,
		"-metadata", "title=" + tags.Title,
		"-metadata", "track=" + trackValue(tags.Track, tags.TrackTotal),
		"-metadata", "disc=" + trackValue(tags.Disc, tags.DiscTotal),
	}
	if tags.Year > 0 {
		args = append(args, "-metadata", fmt.Sprintf("date=%d", tags.Year))
	}
	for _, pair := range [][2]string{
		{"musicbrainz_artistid", tags.MusicBrainzArtistID},
		{"musicbrainz_albumid", tags.MusicBrainzAlbumID},
		{"musicbrainz_releasegroupid", tags.MusicBrainzReleaseGroupID},
		{"musicbrainz_trackid", tags.MusicBrainzTrackID},
		{"musicbrainz_recordingid", tags.MusicBrainzRecordingID},
	} {
		if pair[1] != "" {
			args = append(args, "-metadata", pair[0]+"="+pair[1])
		}
	}
	if strings.EqualFold(ext, ".mp3") {
		args = append(args, "-id3v2_version", "3")
	}
	if strings.EqualFold(ext, ".m4a") || strings.EqualFold(ext, ".mp4") {
		args = append(args, "-movflags", "use_metadata_tags")
	}
	args = append(args, stage)
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stderr boundedBuffer
	stderr.max = 16 << 10
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return &Error{Code: "music_tag_failed", Message: "FFmpeg could not write tags to this file. The original was retained."}
	}
	return nil
}

func verifyEditTags(actual AudioTags, tags EditTags) error {
	if !equalFold(actual.Artist, tags.Artist) || !equalFold(actual.Album, tags.Album) || !equalFold(actual.Title, tags.Title) {
		return &Error{Code: "music_tag_verify", Message: "The written tags did not match the catalog. The original was retained."}
	}
	if actual.Track == nil || *actual.Track != tags.Track || actual.Disc == nil || *actual.Disc != tags.Disc {
		return &Error{Code: "music_tag_verify", Message: "The written track or disc number did not match the catalog. The original was retained."}
	}
	return nil
}

func trackValue(number, total int) string {
	if total > 0 && number > 0 {
		return fmt.Sprintf("%d/%d", number, total)
	}
	return fmt.Sprintf("%d", number)
}

func equalFold(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func linkOrCopy(ctx context.Context, source, destination string) error {
	if err := os.Link(source, destination); err == nil {
		return nil
	}
	return copyFileExclusive(ctx, source, destination)
}

func copyFileExclusive(ctx context.Context, source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(destination)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(destination)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(destination)
		return err
	}
	if err := ctx.Err(); err != nil {
		os.Remove(destination)
		return err
	}
	return nil
}

func (s *Service) EditJob(ctx context.Context, id int64) (EditJob, error) {
	var job EditJob
	err := s.store.db.QueryRowContext(ctx, `SELECT id,state,file_count,error,created_at,updated_at FROM music_edit_jobs WHERE id=?`, id).
		Scan(&job.ID, &job.State, &job.FileCount, &job.Error, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return EditJob{}, err
	}
	rows, err := s.store.db.QueryContext(ctx, `SELECT music_file_id,source_path,destination_path,action,state,error FROM music_edit_files WHERE job_id=? ORDER BY id`, id)
	if err != nil {
		return EditJob{}, err
	}
	defer rows.Close()
	job.Files = []EditJobFile{}
	for rows.Next() {
		var file EditJobFile
		if err := rows.Scan(&file.MusicFileID, &file.SourcePath, &file.DestinationPath, &file.Action, &file.State, &file.Error); err != nil {
			return EditJob{}, err
		}
		job.Files = append(job.Files, file)
	}
	return job, rows.Err()
}

func (s *Service) ListEditJobs(ctx context.Context, page int) (EditJobList, error) {
	if page < 1 || page > 100000 {
		return EditJobList{}, ErrInvalidInput
	}
	const limit = 20
	out := EditJobList{Items: []EditJob{}, Page: page, Limit: limit}
	if err := s.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_edit_jobs`).Scan(&out.Total); err != nil {
		return EditJobList{}, err
	}
	rows, err := s.store.db.QueryContext(ctx, `SELECT id,state,file_count,error,created_at,updated_at FROM music_edit_jobs ORDER BY id DESC LIMIT ? OFFSET ?`, limit, (page-1)*limit)
	if err != nil {
		return EditJobList{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var job EditJob
		if err := rows.Scan(&job.ID, &job.State, &job.FileCount, &job.Error, &job.CreatedAt, &job.UpdatedAt); err != nil {
			return EditJobList{}, err
		}
		out.Items = append(out.Items, job)
	}
	return out, rows.Err()
}
