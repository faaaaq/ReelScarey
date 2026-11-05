package organization

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

// Replacement is a durable, reviewed swap. The original file is retained at
// Backup for media replacements and the discovered source is never removed.
// Metadata-only title updates keep no second copy: the verified stage replaces
// the original in place, and PreviousTitle records the prior container title.
type Replacement struct {
	AcquisitionSubtitles []AcquiredSubtitle  `json:"acquisition_subtitles,omitempty"`
	AcquisitionKind      string              `json:"acquisition_kind,omitempty"`
	AcquisitionJob       string              `json:"acquisition_job,omitempty"`
	FileID               int64               `json:"media_file_id"`
	MediaID              int64               `json:"media_id"`
	Title                string              `json:"title"`
	ScanFileID           string              `json:"scan_file_id,omitempty"`
	RestoresPlan         string              `json:"restores_plan,omitempty"`
	Recovering           bool                `json:"recovering,omitempty"`
	MetadataOnly         bool                `json:"metadata_only,omitempty"`
	PreviousTitle        *string             `json:"previous_title,omitempty"`
	TagTitle             string              `json:"tag_title,omitempty"`
	TagStage             string              `json:"tag_stage,omitempty"`
	TagParent            string              `json:"tag_parent,omitempty"`
	BeforeYouTube        *YouTubeVersion     `json:"before_youtube,omitempty"`
	AfterYouTube         *YouTubeVersion     `json:"after_youtube,omitempty"`
	Before               ReplacementMetadata `json:"before"`
	After                ReplacementMetadata `json:"after"`
	Original             Companion           `json:"original"`
	Incoming             Companion           `json:"incoming"`
	Backup               string              `json:"backup"`
	Episodes             []Episode           `json:"episodes"`
}
type ReplacementMetadata struct {
	Path       string  `json:"path"`
	Size       int64   `json:"size_bytes"`
	SHA        string  `json:"sha256"`
	Probe      string  `json:"probe_json"`
	DownloadID *string `json:"download_id"`
	FileIndex  int     `json:"file_index"`
	ImportedAt int64   `json:"imported_at"`
	Origin     string  `json:"origin"`
	SourceName string  `json:"source_name"`
	// Mtime and Identity describe a verified replacement output. They are not
	// stored in media_files, so Before records leave them zero.
	Mtime    int64  `json:"mtime,omitempty"`
	Identity string `json:"identity,omitempty"`
}

func (m ReplacementMetadata) companion(source string) Companion {
	return Companion{Source: source, Size: m.Size, Mtime: m.Mtime, SHA: m.SHA, Identity: m.Identity}
}

type YouTubeVersion struct {
	JobID      string `json:"job_id"`
	VideoID    string `json:"video_id"`
	Title      string `json:"title"`
	Path       string `json:"path"`
	Size       int64  `json:"size_bytes"`
	SHA        string `json:"sha256"`
	Probe      string `json:"probe_json"`
	ImportedAt int64  `json:"imported_at"`
}

func youtubeVersion(ctx context.Context, q query, fileID int64) (*YouTubeVersion, error) {
	var v YouTubeVersion
	err := q.QueryRowContext(ctx, `SELECT job_id,video_id,title,relative_path,size_bytes,sha256,probe_json,imported_at FROM youtube_files WHERE media_file_id=?`, fileID).Scan(&v.JobID, &v.VideoID, &v.Title, &v.Path, &v.Size, &v.SHA, &v.Probe, &v.ImportedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func replacementMetadata(ctx context.Context, q query, id int64) (ReplacementMetadata, int64, error) {
	var v ReplacementMetadata
	var mediaID int64
	e := q.QueryRowContext(ctx, `SELECT COALESCE(movie_id,series_id),relative_path,size_bytes,sha256,probe_json,download_id,file_index,imported_at,origin,source_name FROM media_files WHERE id=?`, id).Scan(&mediaID, &v.Path, &v.Size, &v.SHA, &v.Probe, &v.DownloadID, &v.FileIndex, &v.ImportedAt, &v.Origin, &v.SourceName)
	return v, mediaID, e
}

// PreviewReplacement accepts only a discovered file ID, never a client path.
func (s *Service) PreviewReplacement(ctx context.Context, fileID int64, scanID string) (Plan, error) {
	unlock := ReadMedia()
	defer unlock()
	if e := EnsureMediaAvailable(ctx, s.db); e != nil {
		return Plan{}, e
	}
	root, resolved, e := s.openRoot("")
	if e != nil {
		return Plan{}, e
	}
	defer root.Close()
	if e = s.prepareChecksums(ctx, root, []int64{fileID}, 0); e != nil {
		return Plan{}, e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return Plan{}, e
	}
	defer tx.Rollback()
	before, mediaID, e := replacementMetadata(ctx, tx, fileID)
	if e != nil {
		return Plan{}, e
	}
	var scanPath, scanRoot, scanFolder, scanKind, state, scanIdentity string
	var scanSize, scanMtime int64
	e = tx.QueryRowContext(ctx, `SELECT f.relative_path,s.resolved_root,s.folder,s.kind,f.state,f.size_bytes,f.mtime,f.file_identity FROM library_scan_files f JOIN library_scans s ON s.id=f.scan_id WHERE f.id=? AND f.lease_until<unixepoch()`, scanID).Scan(&scanPath, &scanRoot, &scanFolder, &scanKind, &state, &scanSize, &scanMtime, &scanIdentity)
	if e != nil {
		return Plan{}, e
	}
	var title, kind string
	if e = tx.QueryRowContext(ctx, `SELECT title,kind FROM media_items WHERE id=?`, mediaID).Scan(&title, &kind); e != nil {
		return Plan{}, e
	}
	if state != "review" || scanRoot != resolved || scanKind != kind || !strings.HasPrefix(scanPath, scanFolder+"/") || strings.EqualFold(scanPath, before.Path) || !strings.EqualFold(path.Ext(scanPath), path.Ext(before.Path)) {
		return Plan{}, problem("invalid_selection", "Choose an unregistered discovered video of the same type and file extension.")
	}
	var count int
	if e = tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM media_files WHERE relative_path=? COLLATE NOCASE)+(SELECT COUNT(*) FROM youtube_files WHERE relative_path=? COLLATE NOCASE)`, scanPath, scanPath).Scan(&count); e != nil {
		return Plan{}, e
	}
	if count != 0 {
		return Plan{}, problem("invalid_selection", "The selected source is already registered.")
	}
	youtube, e := youtubeVersion(ctx, tx, fileID)
	if e != nil {
		return Plan{}, e
	}
	if youtube != nil && (youtube.Path != before.Path || youtube.Size != before.Size || youtube.SHA != before.SHA) {
		return Plan{}, problem("revision_conflict", "YouTube provenance does not match the registered file.")
	}
	if e = idle(ctx, tx, mediaID, fileID); e != nil {
		return Plan{}, e
	}
	old, e := inspect(root, before.Path)
	if e != nil {
		return Plan{}, e
	}
	if old.Size != before.Size {
		return Plan{}, problem("source_changed", "The registered file changed since import.")
	}
	old.SHA = before.SHA
	incoming, e := inspect(root, scanPath)
	if e != nil {
		return Plan{}, e
	}
	if incoming.Size != scanSize || incoming.Mtime != scanMtime || incoming.Identity != scanIdentity || incoming.Identity == old.Identity {
		return Plan{}, problem("source_changed", "The discovered file changed since the scan.")
	}
	eps, e := s.episodes(ctx, tx, fileID)
	if e != nil {
		return Plan{}, e
	}
	now := time.Now().Unix()
	pid := id()
	repl := &Replacement{FileID: fileID, MediaID: mediaID, Title: title, ScanFileID: scanID, Before: before, BeforeYouTube: youtube, Original: old, Incoming: incoming, Backup: path.Join(".reelscarey-versions", ".reelscarey-version-"+pid), Episodes: eps}
	p := Plan{ID: pid, State: "review", Root: resolved, Phase: "planned", Created: now, Updated: now, Files: []PlannedFile{}, Replacement: repl}
	raw, e := json.Marshal(p)
	if e != nil {
		return Plan{}, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO organization_plans(id,state,snapshot,created_at,updated_at) VALUES(?,'review',?,?,?)`, pid, string(raw), now, now); e != nil {
		return Plan{}, e
	}
	return p, tx.Commit()
}

func (s *Service) validateReplacement(ctx context.Context, root *os.Root, q query, p Plan) error {
	r := p.Replacement
	if r.After.Path != "" && r.After.Path != r.Before.Path {
		exists, e := existsFolded(root, r.After.Path)
		if e != nil {
			return e
		}
		if exists {
			return problem("destination_exists", "The replacement's new extension would use an occupied path. No file was overwritten.")
		}
	}
	current, media, e := replacementMetadata(ctx, q, r.FileID)
	if e != nil {
		return e
	}
	if media != r.MediaID || !reflect.DeepEqual(current, r.Before) {
		return problem("revision_conflict", "The registered file changed after review.")
	}
	eps, e := s.episodes(ctx, q, r.FileID)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(eps, r.Episodes) {
		return problem("revision_conflict", "Episode mappings changed after review.")
	}
	if r.AcquisitionKind != "" {
		if e = acquisitionReady(ctx, q, r.AcquisitionKind, r.AcquisitionJob); e != nil {
			return e
		}
		if e = validateAcquisitionTarget(ctx, q, r); e != nil {
			return e
		}
	}
	if r.TagTitle != "" {
		var title string
		if e = q.QueryRowContext(ctx, `SELECT title FROM media_items WHERE id=?`, r.MediaID).Scan(&title); e != nil {
			return e
		}
		if embeddedTitle(title, eps) != r.TagTitle {
			return problem("revision_conflict", "The saved title changed after review.")
		}
	}
	if e = idleExcept(ctx, q, r.MediaID, r.FileID, r.AcquisitionKind, r.AcquisitionJob); e != nil {
		return e
	}
	youtube, e := youtubeVersion(ctx, q, r.FileID)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(youtube, r.BeforeYouTube) {
		return problem("revision_conflict", "YouTube provenance changed after review.")
	}
	if r.ScanFileID != "" {
		var state string
		var name string
		if e = q.QueryRowContext(ctx, `SELECT state,relative_path FROM library_scan_files WHERE id=? AND lease_until<unixepoch()`, r.ScanFileID).Scan(&state, &name); e != nil {
			return e
		}
		if state != "review" || name != r.Incoming.Source {
			return problem("revision_conflict", "The discovered file changed after review.")
		}
		var registered int
		if e = q.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM media_files WHERE relative_path=? COLLATE NOCASE)+(SELECT COUNT(*) FROM youtube_files WHERE relative_path=? COLLATE NOCASE)`, name, name).Scan(&registered); e != nil {
			return e
		}
		if registered != 0 {
			return problem("revision_conflict", "The discovered file was registered after review.")
		}
	}
	if e = checkSource(ctx, root, r.Original, false); e != nil {
		return e
	}
	return checkSource(ctx, root, r.Incoming, false)
}

// PreviewRollback creates a new reviewed swap, retaining the current version in
// its own backup. Only the still-current result of a completed swap can return.
func (s *Service) PreviewRollback(ctx context.Context, planID string) (Plan, error) {
	unlock := ReadMedia()
	defer unlock()
	if e := EnsureMediaAvailable(ctx, s.db); e != nil {
		return Plan{}, e
	}
	old, e := s.Plan(ctx, planID)
	if e != nil {
		return Plan{}, e
	}
	if old.Removal != nil {
		return Plan{}, problem("invalid_selection", "Removed catalog entries must be added and registered again. Retained trash is not deleted.")
	}
	if old.Replacement != nil && old.Replacement.MetadataOnly {
		return Plan{}, problem("invalid_selection", "This job only changed the embedded title and kept no video copy. Review a new title update to change it again.")
	}
	if old.State == "completed" && old.Replacement == nil {
		return s.previewMoveRollback(ctx, old)
	}
	if old.State != "completed" || old.Replacement == nil {
		return Plan{}, problem("invalid_selection", "Choose a completed replacement to restore.")
	}
	root, resolved, e := s.openRoot(old.Root)
	if e != nil {
		return Plan{}, e
	}
	defer root.Close()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return Plan{}, e
	}
	defer tx.Rollback()
	previous := old.Replacement
	before, mediaID, e := replacementMetadata(ctx, tx, previous.FileID)
	if e != nil {
		return Plan{}, e
	}
	expected := previous.After
	expected.Path = before.Path   // reviewed organization may have moved the file
	expected.Probe = before.Probe // a verified reinspection may refresh stream facts
	if mediaID != previous.MediaID || !reflect.DeepEqual(before, expected) {
		return Plan{}, problem("revision_conflict", "The current file no longer matches this replacement. Review the latest version instead.")
	}
	if e = idle(ctx, tx, mediaID, previous.FileID); e != nil {
		return Plan{}, e
	}
	currentYouTube, e := youtubeVersion(ctx, tx, previous.FileID)
	if e != nil {
		return Plan{}, e
	}
	var expectedYouTube *YouTubeVersion
	if previous.AfterYouTube != nil {
		copy := *previous.AfterYouTube
		copy.Path = before.Path
		if currentYouTube != nil {
			copy.Probe = currentYouTube.Probe
		}
		expectedYouTube = &copy
	}
	if !reflect.DeepEqual(currentYouTube, expectedYouTube) {
		return Plan{}, problem("revision_conflict", "The current YouTube provenance changed after replacement.")
	}
	original, e := inspect(root, before.Path)
	if e != nil {
		return Plan{}, e
	}
	original.SHA = before.SHA
	if original.Size != before.Size {
		return Plan{}, problem("source_changed", "The current file changed.")
	}
	incoming, e := inspect(root, previous.Backup)
	if e != nil {
		return Plan{}, e
	}
	incoming.SHA = previous.Before.SHA
	var savedIdentity string
	if e = tx.QueryRowContext(ctx, `SELECT identity FROM organization_outputs WHERE plan_id=? AND destination=?`, old.ID, previous.Backup).Scan(&savedIdentity); e != nil {
		return Plan{}, e
	}
	if savedIdentity == "" || incoming.Identity != savedIdentity || incoming.Size != previous.Before.Size {
		return Plan{}, problem("destination_changed", "The retained version changed and was left for review.")
	}
	eps, e := s.episodes(ctx, tx, previous.FileID)
	if e != nil {
		return Plan{}, e
	}
	if !reflect.DeepEqual(eps, previous.Episodes) {
		return Plan{}, problem("revision_conflict", "Episode mappings changed after the replacement.")
	}
	now := time.Now().Unix()
	pid := id()
	restored := previous.Before
	restored.Path = strings.TrimSuffix(before.Path, path.Ext(before.Path)) + path.Ext(previous.Before.Path)
	var restoredYouTube *YouTubeVersion
	if previous.BeforeYouTube != nil {
		copy := *previous.BeforeYouTube
		copy.Path = restored.Path
		restoredYouTube = &copy
	}
	r := &Replacement{FileID: previous.FileID, MediaID: mediaID, Title: previous.Title, RestoresPlan: old.ID, Before: before, After: restored, BeforeYouTube: currentYouTube, AfterYouTube: restoredYouTube, Original: original, Incoming: incoming, Backup: path.Join(".reelscarey-versions", ".reelscarey-version-"+pid), Episodes: eps}
	p := Plan{ID: pid, State: "review", Root: resolved, Phase: "planned", Created: now, Updated: now, Files: []PlannedFile{}, Replacement: r}
	raw, e := json.Marshal(p)
	if e != nil {
		return Plan{}, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO organization_plans(id,state,snapshot,created_at,updated_at) VALUES(?,'review',?,?,?)`, pid, string(raw), now, now); e != nil {
		return Plan{}, e
	}
	return p, tx.Commit()
}

func (s *Service) executeReplacement(ctx context.Context, p *Plan) error {
	r := p.Replacement
	if r.MetadataOnly {
		return s.executeEmbeddedTitle(ctx, p)
	}
	root, _, e := s.openRoot(p.Root)
	if e != nil {
		return e
	}
	defer root.Close()
	if p.Phase == "planned" {
		if e = s.validateReplacement(ctx, root, s.db, *p); e != nil {
			return e
		}
		if e = checkSource(ctx, root, r.Original, true); e != nil {
			return e
		}
		if r.AcquisitionKind != "" {
			if e = acquisitionReady(ctx, s.db, r.AcquisitionKind, r.AcquisitionJob); e != nil {
				return e
			}
		}
		if r.TagTitle != "" {
			if e = s.prepareEmbeddedTitle(ctx, root, p); e != nil {
				return e
			}
		} else {
			file, e := root.Open(filepath.FromSlash(r.Incoming.Source))
			if e != nil {
				return e
			}
			defer file.Close()
			ident, e := identity(file)
			if e != nil || ident != r.Incoming.Identity {
				return problem("source_changed", "The incoming file changed during verification.")
			}
			digest, e := hash(ctx, file)
			if e != nil {
				return e
			}
			if r.RestoresPlan == "" {
				if s.probe == nil {
					return problem("ffprobe_missing", "The replacement verifier is unavailable. The original was not changed.")
				}
				probe, e := s.probe(ctx, file)
				if e != nil {
					return e
				}
				if r.AcquisitionKind == "" {
					r.After = ReplacementMetadata{Path: r.Before.Path, Size: r.Incoming.Size, SHA: digest, Probe: probe, FileIndex: -1, ImportedAt: time.Now().Unix(), Origin: "existing", SourceName: path.Base(r.Incoming.Source)}
				} else if digest != r.Incoming.SHA {
					return problem("source_changed", "The acquired source changed after review.")
				}
			} else if digest != r.After.SHA {
				return problem("source_changed", "The retained version no longer matches its saved checksum.")
			}
			r.Incoming.SHA = digest
			if digest == r.Before.SHA {
				return problem("invalid_selection", "The selected file has the same contents as the registered version.")
			}
			if e = checkSource(ctx, root, r.Incoming, true); e != nil {
				return e
			}
		}
		if r.Incoming.SHA == r.Before.SHA {
			return problem("invalid_selection", "The selected file has the same contents as the registered version.")
		}
		if e = s.savePhase(ctx, p, "prepared"); e != nil {
			return e
		}
	}
	backup := r.Original
	backup.Destination = r.Backup
	if p.Phase == "prepared" {
		if e = s.validateReplacement(ctx, root, s.db, *p); e != nil {
			return e
		}
		if e = checkSource(ctx, root, r.Original, true); e != nil {
			return e
		}
		if e = checkSource(ctx, root, r.Incoming, true); e != nil {
			return e
		}
		if e = s.publishReplacementCopy(ctx, root, *p, backup, 0); e != nil {
			return e
		}
		for n, sub := range r.AcquisitionSubtitles {
			if e = checkSource(ctx, root, sub.File, true); e != nil {
				return e
			}
			if e = s.publishReplacementCopy(ctx, root, *p, sub.File, n+10); e != nil {
				return e
			}
		}
		if e = s.savePhase(ctx, p, "archived"); e != nil {
			return e
		}
	}
	if p.Phase == "archived" {
		if e = s.checkOutput(ctx, root, s.db, p.ID, backup); e != nil {
			return e
		}
		if e = checkSource(ctx, root, r.Incoming, true); e != nil {
			return e
		}
		// If removal succeeded just before interruption, the verified backup makes
		// the missing path safe to resume. Never remove a changed or foreign path.
		e = checkSource(ctx, root, r.Original, true)
		if e == nil {
			if e = s.validateReplacement(ctx, root, s.db, *p); e != nil {
				return e
			}
			if e = root.Remove(filepath.FromSlash(r.Original.Source)); e != nil {
				return e
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if e = s.savePhase(ctx, p, "removed"); e != nil {
			return e
		}
	}
	incoming := r.Incoming
	incoming.Destination = r.After.Path
	if incoming.Destination == "" {
		incoming.Destination = r.Original.Source
	}
	stageNumber := 1
	if r.Recovering {
		stageNumber = 2
	}
	if p.Phase == "removed" {
		if e = s.checkOutput(ctx, root, s.db, p.ID, backup); e != nil {
			return e
		}
		if e = checkSource(ctx, root, r.Incoming, true); e != nil {
			return e
		}
		if e = s.publishReplacementCopy(ctx, root, *p, incoming, stageNumber); e != nil {
			return e
		}
		if e = s.savePhase(ctx, p, "published"); e != nil {
			return e
		}
	}
	if p.Phase == "published" {
		tx, e := s.db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		before, media, e := replacementMetadata(ctx, tx, r.FileID)
		if e != nil {
			return e
		}
		if media != r.MediaID || !reflect.DeepEqual(before, r.Before) {
			return problem("revision_conflict", "The registered file changed during replacement.")
		}
		eps, e := s.episodes(ctx, tx, r.FileID)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(eps, r.Episodes) {
			return problem("revision_conflict", "Episode mappings changed during replacement.")
		}
		if e = idleExcept(ctx, tx, r.MediaID, r.FileID, r.AcquisitionKind, r.AcquisitionJob); e != nil {
			return e
		}
		youtube, e := youtubeVersion(ctx, tx, r.FileID)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(youtube, r.BeforeYouTube) {
			return problem("revision_conflict", "YouTube provenance changed during replacement.")
		}
		if e = s.checkOutput(ctx, root, tx, p.ID, backup); e != nil {
			return e
		}
		if e = s.checkOutput(ctx, root, tx, p.ID, incoming); e != nil {
			return e
		}
		a := r.After
		if _, e = tx.ExecContext(ctx, `UPDATE media_files SET relative_path=?,size_bytes=?,sha256=?,probe_json=?,download_id=?,file_index=?,imported_at=?,origin=?,source_name=? WHERE id=?`, a.Path, a.Size, a.SHA, a.Probe, a.DownloadID, a.FileIndex, a.ImportedAt, a.Origin, a.SourceName, r.FileID); e != nil {
			return e
		}
		if youtube != nil {
			if _, e = tx.ExecContext(ctx, `DELETE FROM youtube_files WHERE media_file_id=?`, r.FileID); e != nil {
				return e
			}
		}
		if r.AfterYouTube != nil {
			y := r.AfterYouTube
			if y.Path != a.Path || y.Size != a.Size || y.SHA != a.SHA {
				return problem("revision_conflict", "The restored YouTube provenance does not match the video.")
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO youtube_files(job_id,media_file_id,video_id,title,relative_path,size_bytes,sha256,probe_json,imported_at) VALUES(?,?,?,?,?,?,?,?,?)`, y.JobID, r.FileID, y.VideoID, y.Title, y.Path, y.Size, y.SHA, y.Probe, y.ImportedAt); e != nil {
				return e
			}
		}
		for _, sub := range r.AcquisitionSubtitles {
			if e = s.checkOutput(ctx, root, tx, p.ID, sub.File); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO youtube_subtitle_files(job_id,language,relative_path,sha256,size_bytes) VALUES(?,?,?,?,?)`, r.AcquisitionJob, sub.Language, sub.File.Destination, sub.File.SHA, sub.File.Size); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM subtitle_inspections WHERE media_file_id=?`, r.FileID); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE subtitle_jobs SET requested=0,selected_candidate='',state='queued',next_run=0,attempts=0,message='Video changed; subtitle timing may require review.',updated_at=unixepoch() WHERE media_file_id=?`, r.FileID); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM subtitle_candidates WHERE media_file_id=?`, r.FileID); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM subtitles WHERE media_file_id=? AND source='embedded'`, r.FileID); e != nil {
			return e
		}
		activity := "file replaced"
		if r.TagTitle != "" && !r.Recovering {
			activity = "embedded title updated"
		}
		if r.Recovering {
			activity = "original file restored after interruption"
		} else if r.RestoresPlan != "" {
			activity = "previous file restored"
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'title',?,?,?)`, r.MediaID, r.Title, activity); e != nil {
			return e
		}
		if r.ScanFileID != "" {
			res, e := tx.ExecContext(ctx, `UPDATE library_scan_files SET state='skipped',message='Used as a replacement source; the original source was retained.',updated_at=unixepoch() WHERE id=? AND state='review'`, r.ScanFileID)
			if e != nil {
				return e
			}
			n, _ := res.RowsAffected()
			if n != 1 {
				return problem("revision_conflict", "The discovered source changed during replacement.")
			}
		}
		if e = completeAcquisition(ctx, tx, r); e != nil {
			return e
		}
		p.Phase = "committed"
		raw, e := json.Marshal(p)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE organization_plans SET snapshot=?,updated_at=unixepoch() WHERE id=?`, string(raw), p.ID); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
	}
	// Remove only our recorded staging links; retained version and source remain.
	for n, v := range []Companion{backup, incoming} {
		if n == 1 {
			n = stageNumber
		}
		stage := path.Join(path.Dir(v.Destination), ".reelscarey-organize-"+p.ID+"-"+string(rune('0'+n))+".part")
		var saved string
		if e = s.db.QueryRowContext(ctx, `SELECT identity FROM organization_outputs WHERE plan_id=? AND destination=?`, p.ID, v.Destination).Scan(&saved); e != nil {
			return e
		}
		if f, err := root.Open(filepath.FromSlash(stage)); err == nil {
			actual, _ := identity(f)
			f.Close()
			if actual == saved {
				_ = root.Remove(filepath.FromSlash(stage))
			}
		}
	}
	if r.TagStage != "" {
		var saved string
		if e = s.db.QueryRowContext(ctx, `SELECT identity FROM organization_outputs WHERE plan_id=? AND destination=?`, p.ID, r.TagStage).Scan(&saved); e != nil {
			return e
		}
		if f, err := root.Open(filepath.FromSlash(r.TagStage)); err == nil {
			actual, _ := identity(f)
			f.Close()
			if saved != "" && actual == saved {
				_ = root.Remove(filepath.FromSlash(r.TagStage))
			}
		}
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `UPDATE organization_plans SET state='completed',message='',updated_at=unixepoch() WHERE id=?`, p.ID); e != nil {
		return e
	}
	if r.TagParent != "" && !r.Recovering {
		if _, e = tx.ExecContext(ctx, `UPDATE organization_plans SET state='queued',message='',updated_at=unixepoch() WHERE id=(SELECT id FROM organization_plans WHERE state='review' AND json_extract(snapshot,'$.replacement.tag_parent')=? ORDER BY rowid LIMIT 1)`, r.TagParent); e != nil {
			return e
		}
	}
	return tx.Commit()
}

// A retained version and a replacement must not share an inode with a download
// client or the independently retained discovered source.
func (s *Service) publishReplacementCopy(ctx context.Context, root *os.Root, p Plan, v Companion, n int) error {
	previous := s.link
	s.link = func(*os.Root, string, string) error { return errors.New("replacement requires an independent copy") }
	defer func() { s.link = previous }()
	return s.publish(ctx, root, p, v, n)
}

// RecoverReplacement requeues an interrupted swap to restore the retained
// original. It is allowed only when the old path is vacant; a foreign file is
// never overwritten or moved. Uncertain prior stages are left untouched.
func (s *Service) RecoverReplacement(ctx context.Context, planID string) (Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock := ReadMedia()
	defer unlock()
	p, e := s.Plan(ctx, planID)
	if e != nil {
		return p, e
	}
	r := p.Replacement
	if r != nil && r.MetadataOnly {
		return p, problem("invalid_selection", "Metadata-only title updates keep no video copy to restore.")
	}
	if p.State != "failed" || r == nil || r.Recovering || (p.Phase != "archived" && p.Phase != "removed") {
		return p, problem("invalid_selection", "This job cannot restore its original version from the current state.")
	}
	root, _, e := s.openRoot(p.Root)
	if e != nil {
		return p, e
	}
	defer root.Close()
	if e = segments(root, path.Dir(r.Original.Source), false); e != nil {
		return p, e
	}
	if _, e = root.Lstat(filepath.FromSlash(r.Original.Source)); !errors.Is(e, os.ErrNotExist) {
		if e != nil {
			return p, e
		}
		return p, problem("destination_exists", "The original path is occupied. It was left untouched; resolve it before restoring.")
	}
	backup, e := inspect(root, r.Backup)
	if e != nil {
		return p, e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return p, e
	}
	defer tx.Rollback()
	before, media, e := replacementMetadata(ctx, tx, r.FileID)
	if e != nil {
		return p, e
	}
	if media != r.MediaID || !reflect.DeepEqual(before, r.Before) {
		return p, problem("revision_conflict", "The registered file changed; the original was retained for review.")
	}
	youtube, e := youtubeVersion(ctx, tx, r.FileID)
	if e != nil {
		return p, e
	}
	if !reflect.DeepEqual(youtube, r.BeforeYouTube) {
		return p, problem("revision_conflict", "YouTube provenance changed; the original was retained for review.")
	}
	eps, e := s.episodes(ctx, tx, r.FileID)
	if e != nil {
		return p, e
	}
	if !reflect.DeepEqual(eps, r.Episodes) {
		return p, problem("revision_conflict", "The episode mapping changed; the original was retained for review.")
	}
	if e = idleExcept(ctx, tx, r.MediaID, r.FileID, r.AcquisitionKind, r.AcquisitionJob); e != nil {
		return p, e
	}
	var saved string
	if e = tx.QueryRowContext(ctx, `SELECT identity FROM organization_outputs WHERE plan_id=? AND destination=?`, p.ID, r.Backup).Scan(&saved); e != nil {
		return p, e
	}
	if saved == "" || saved != backup.Identity || backup.Size != r.Before.Size {
		return p, problem("destination_changed", "The retained version changed and was left for review.")
	}
	var active int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM organization_plans WHERE state IN ('queued','running') AND id<>?`, p.ID).Scan(&active); e != nil {
		return p, e
	}
	if active != 0 {
		return p, problem("organization_busy", "Wait for the current job to finish.")
	}
	// The destination is vacant. Forget only its old journal row; any stage is
	// deliberately left in place, and restoration uses a different stage name.
	if _, e = tx.ExecContext(ctx, `DELETE FROM organization_outputs WHERE plan_id=? AND destination=?`, p.ID, r.Original.Source); e != nil {
		return p, e
	}
	backup.SHA = r.Before.SHA
	r.Incoming = backup
	r.After = r.Before
	r.AfterYouTube = r.BeforeYouTube
	r.ScanFileID = ""
	r.Recovering = true
	p.Phase = "removed"
	raw, e := json.Marshal(p)
	if e != nil {
		return p, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE organization_plans SET snapshot=?,state='queued',message='',updated_at=unixepoch() WHERE id=? AND state='failed'`, string(raw), p.ID); e != nil {
		return p, e
	}
	if e = tx.Commit(); e != nil {
		return p, e
	}
	return s.Plan(ctx, p.ID)
}

// ReplacementVersions keeps the latest saved versions reachable even when
// unrelated organization jobs have pushed them out of the general history.
func (s *Service) ReplacementVersions(ctx context.Context, fileID int64) ([]Plan, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT id FROM organization_plans WHERE state='completed' AND json_extract(snapshot,'$.replacement.media_file_id')=? ORDER BY rowid DESC LIMIT 20`, fileID)
	if e != nil {
		return nil, e
	}
	var ids []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := make([]Plan, 0, len(ids))
	for _, id := range ids {
		p, e := s.Plan(ctx, id)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, nil
}
