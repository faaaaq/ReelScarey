package organization

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

func embeddedTitle(title string, episodes []Episode) string {
	title = strings.TrimSpace(title)
	if len(episodes) == 0 {
		return title
	}
	first := episodes[0]
	number := fmt.Sprintf("S%02dE%02d", first.Season, first.Number)
	if len(episodes) > 1 {
		number += fmt.Sprintf("-E%02d", episodes[len(episodes)-1].Number)
	}
	if len(episodes) == 1 && strings.TrimSpace(first.Title) != "" {
		return title + " - " + number + " - " + strings.TrimSpace(first.Title)
	}
	return title + " - " + number
}

// A single matched episode uses its exact catalog title. A shared file
// carries the show and ordered episode range because it represents several titles.
func registeredEmbeddedTitle(title string, episodes []Episode) string {
	if len(episodes) == 1 && strings.TrimSpace(episodes[0].Title) != "" {
		return strings.TrimSpace(episodes[0].Title)
	}
	return embeddedTitle(title, episodes)
}

func reviewedEmbeddedTitle(v PlannedFile) (string, error) {
	ext := strings.ToLower(path.Ext(v.Source))
	if ext != ".mkv" && ext != ".mp4" && ext != ".m4v" && ext != ".mov" && ext != ".avi" && ext != ".webm" {
		return "", problem("invalid_selection", "Embedded title updates support MP4, Matroska, MOV, AVI and WebM files.")
	}
	for i := 1; i < len(v.Episodes); i++ {
		if v.Episodes[i].Season != v.Episodes[0].Season || v.Episodes[i].Number != v.Episodes[i-1].Number+1 {
			return "", problem("invalid_selection", "Review the episode mapping before updating this file's embedded title.")
		}
	}
	value := embeddedTitle(v.Snapshot.Content.Title, v.Episodes)
	if value == "" || len(value) > 1024 || strings.ContainsAny(value, "\x00\r\n") {
		return "", problem("invalid_selection", "The saved title cannot be safely embedded.")
	}
	return value, nil
}

// readEmbeddedTitle returns the current container title when the probe service
// is available, falling back to the stored inspection. It never invents a
// value: a nil result means no title fact was recorded.
func (s *Service) readEmbeddedTitle(ctx context.Context, root *os.Root, name, stored string) *string {
	parse := func(raw string) (*string, bool) {
		var v struct {
			EmbeddedTitle *string `json:"embedded_title"`
		}
		if json.Unmarshal([]byte(raw), &v) != nil {
			return nil, false
		}
		return v.EmbeddedTitle, true
	}
	// A stored non-null title is a verified inspection fact; reuse it and
	// avoid another probe. Missing or null facts are refreshed when possible.
	if title, ok := parse(stored); ok && title != nil {
		return title
	}
	if s.probe != nil {
		if f, e := root.Open(filepath.FromSlash(name)); e == nil {
			raw, probeErr := s.probe(ctx, f)
			f.Close()
			if probeErr == nil {
				if title, ok := parse(raw); ok {
					return title
				}
			}
		}
	}
	title, _ := parse(stored)
	return title
}

// PreviewEmbeddedTitle creates a durable reviewed replacement using a verified
// metadata-only copy. It never accepts client-supplied paths or title text.
func (s *Service) PreviewEmbeddedTitle(ctx context.Context, fileID int64) (Plan, error) {
	unlock := ReadMedia()
	defer unlock()
	if e := EnsureMediaAvailable(ctx, s.db); e != nil {
		return Plan{}, e
	}
	return s.previewEmbeddedTitle(ctx, fileID, "")
}

// Caller holds the media gate and arranges any durable queue reservation.
func (s *Service) previewEmbeddedTitle(ctx context.Context, fileID int64, parentID string, automatic ...bool) (Plan, error) {
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
	ext := strings.ToLower(path.Ext(before.Path))
	if ext != ".mkv" && ext != ".mp4" && ext != ".m4v" && ext != ".mov" && ext != ".avi" && ext != ".webm" {
		return Plan{}, problem("invalid_selection", "Embedded title updates currently support MP4, Matroska, MOV, AVI and WebM files.")
	}
	var title string
	if e = tx.QueryRowContext(ctx, `SELECT title FROM media_items WHERE id=?`, mediaID).Scan(&title); e != nil {
		return Plan{}, e
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
	episodes, e := s.episodes(ctx, tx, fileID)
	if e != nil {
		return Plan{}, e
	}
	for i := 1; i < len(episodes); i++ {
		if episodes[i].Season != episodes[0].Season || episodes[i].Number != episodes[i-1].Number+1 {
			return Plan{}, problem("invalid_selection", "Review the episode mapping before updating this file's embedded title.")
		}
	}
	auto := len(automatic) > 0 && automatic[0]
	value := embeddedTitle(title, episodes)
	if auto {
		value = registeredEmbeddedTitle(title, episodes)
	}
	if value == "" || len(value) > 1024 || strings.ContainsAny(value, "\x00\r\n") {
		return Plan{}, problem("invalid_selection", "The saved title cannot be safely embedded.")
	}
	// Record the previous container title. A missing field means the file was
	// never inspected for a title; a pointer to an empty string means
	// inspection found none. The plan snapshot is the durable change record
	// for an update that keeps no copy of the video.
	previousTitle := s.readEmbeddedTitle(ctx, root, before.Path, before.Probe)
	state := "review"
	if auto {
		if _, err := tx.ExecContext(ctx, `DELETE FROM video_title_updates WHERE media_file_id=?`, fileID); err != nil {
			return Plan{}, err
		}
		if previousTitle != nil && *previousTitle == value {
			return Plan{}, tx.Commit()
		}
		state = "queued"
	}
	now := time.Now().Unix()
	pid := id()
	// Keep the verified stage on the same filesystem as the video so the final
	// replacement is a same-device rename. Media libraries may be separate
	// mounts, so a state-volume staging file cannot be renamed into them.
	r := &Replacement{FileID: fileID, MediaID: mediaID, Title: title, Before: before, BeforeYouTube: youtube, Original: old, Incoming: old, MetadataOnly: true, AutomaticTitle: auto, PreviousTitle: previousTitle, TagStage: path.Join(path.Dir(before.Path), ".reelscarey-tag-"+pid), TagTitle: value, TagParent: parentID, Episodes: episodes}
	p := Plan{ID: pid, State: state, Root: resolved, Phase: "planned", Created: now, Updated: now, Files: []PlannedFile{}, Replacement: r}
	raw, e := json.Marshal(p)
	if e != nil {
		return Plan{}, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO organization_plans(id,state,snapshot,created_at,updated_at) VALUES(?,?,?,?,?)`, pid, state, string(raw), now, now); e != nil {
		return Plan{}, e
	}
	return p, tx.Commit()
}

// titleHeaderBackupBytes bounds the header backup kept in the journal stage
// for an in-place title edit. Matroska keeps the segment header, seek head and
// segment info within the first few megabytes of well-formed files; beyond
// this bound the fast path is refused and the remux stage is used instead.
const titleHeaderBackupBytes = 16 << 20

func matroskaTitleExtension(extension string) bool {
	switch strings.ToLower(extension) {
	case ".mkv", ".webm":
		return true
	}
	return false
}

// backupTitleHeader copies the leading bytes of the source into the journal
// stage. Re-entrant prepare runs keep the existing backup: the source header
// may be mid-edit, so its current bytes are not a valid recovery copy.
func (s *Service) backupTitleHeader(ctx context.Context, stage, source *os.File, r *Replacement) error {
	limit := r.Original.Size
	if limit > titleHeaderBackupBytes {
		limit = titleHeaderBackupBytes
	}
	if limit <= 0 {
		return problem("unsafe_path", "The registered file has no content to back up.")
	}
	if info, e := stage.Stat(); e != nil {
		return e
	} else if info.Size() != limit {
		// A fresh prepare leaves the stage empty; a re-entrant prepare keeps a
		// completed backup. Otherwise the backup is rewritten from the source,
		// which is the verified original at this point in the review.
		if _, e := source.Seek(0, io.SeekStart); e != nil {
			return e
		}
		if _, e := stage.Seek(0, io.SeekStart); e != nil {
			return e
		}
		if e := stage.Truncate(0); e != nil {
			return e
		}
		n, e := io.CopyN(stage, source, limit)
		if e != nil {
			return e
		}
		if n != limit {
			return problem("source_changed", "The registered file ended before its header backup completed.")
		}
		if e = stage.Sync(); e != nil {
			return e
		}
	}
	id, e := identity(stage)
	if e != nil {
		return e
	}
	r.BackupIdentity = id
	return nil
}

// restoreTitleHeader repairs a file whose in-place edit was interrupted or
// refused: the backup bytes go back to offset zero, the file is truncated to
// its reviewed size, and the modification time is restored so the reviewed
// facts match again.
func (s *Service) restoreTitleHeader(root *os.Root, stage *os.File, r *Replacement) error {
	if _, e := stage.Seek(0, io.SeekStart); e != nil {
		return e
	}
	data, e := io.ReadAll(io.LimitReader(stage, titleHeaderBackupBytes))
	if e != nil {
		return e
	}
	if int64(len(data)) < r.Original.Size && int64(len(data)) < titleHeaderBackupBytes {
		return problem("staging_conflict", "The saved header backup is shorter than the reviewed file size.")
	}
	f, e := root.OpenFile(filepath.FromSlash(r.Original.Source), os.O_RDWR, 0)
	if e != nil {
		return e
	}
	defer f.Close()
	if _, e = f.WriteAt(data, 0); e != nil {
		return e
	}
	if e = f.Truncate(r.Original.Size); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	f.Close()
	full := filepath.Join(root.Name(), filepath.FromSlash(r.Original.Source))
	mtime := time.Unix(0, r.Original.Mtime)
	return os.Chtimes(full, mtime, mtime)
}

// fileDigest hashes a registered file once for the catalog's change detection.
func (s *Service) fileDigest(ctx context.Context, root *os.Root, name string) (string, error) {
	f, e := root.Open(filepath.FromSlash(name))
	if e != nil {
		return "", e
	}
	defer f.Close()
	return hash(ctx, f)
}

func (s *Service) prepareEmbeddedTitle(ctx context.Context, root *os.Root, p *Plan) error {
	r := p.Replacement
	disableExt := matroskaTitleExtension(strings.ToLower(path.Ext(r.Original.Source)))
	switch {
	case len(r.TrackEdits) > 0 && (len(r.DropStreams) > 0 || len(r.DisableStreams) > 0):
		return problem("invalid_selection", "Review track field edits separately from removing or disabling tracks.")
	case len(r.TrackEdits) > 0 && s.matroskaTrackEditor == nil:
		return problem("ffprobe_missing", "Editing Matroska track fields in place is unavailable on this server.")
	case len(r.TrackEdits) > 0 && !disableExt:
		return problem("unsupported_media", "In-place track field editing is supported only for Matroska (.mkv/.webm) files.")
	case len(r.DropStreams) > 0 && s.rewriteTracks == nil:
		return problem("ffprobe_missing", "The embedded metadata verifier is unavailable.")
	case len(r.DropStreams) == 0 && len(r.DisableStreams) > 0 && s.matroskaDisabler == nil:
		return problem("ffprobe_missing", "Disabling tracks in place is unavailable on this server.")
	case len(r.DropStreams) == 0 && len(r.DisableStreams) > 0 && !disableExt:
		return problem("unsupported_media", "Disabling tracks in place is supported only for Matroska (.mkv/.webm) files; choose Remove for this file.")
	case len(r.DropStreams) == 0 && len(r.DisableStreams) == 0 && s.tag == nil:
		return problem("ffprobe_missing", "The embedded metadata verifier is unavailable.")
	}
	dir := path.Dir(r.TagStage)
	if e := segments(root, dir, true); e != nil {
		return e
	}
	if e := root.MkdirAll(filepath.FromSlash(dir), 0755); e != nil {
		return e
	}
	if e := segments(root, dir, false); e != nil {
		return e
	}
	if _, e := s.db.ExecContext(ctx, `INSERT INTO organization_outputs(plan_id,destination,stage) VALUES(?,?,?) ON CONFLICT(plan_id,destination) DO NOTHING`, p.ID, r.TagStage, r.TagStage); e != nil {
		return e
	}
	var saved string
	if e := s.db.QueryRowContext(ctx, `SELECT identity FROM organization_outputs WHERE plan_id=? AND destination=?`, p.ID, r.TagStage).Scan(&saved); e != nil {
		return e
	}
	stage, e := root.OpenFile(filepath.FromSlash(r.TagStage), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if errors.Is(e, os.ErrExist) {
		if saved == "" {
			return problem("staging_conflict", "An unverified metadata staging file was retained for review.")
		}
		if e = segments(root, r.TagStage, false); e != nil {
			return e
		}
		stage, e = root.OpenFile(filepath.FromSlash(r.TagStage), os.O_RDWR, 0)
	}
	if e != nil {
		return e
	}
	defer stage.Close()
	actual, e := identity(stage)
	if e != nil {
		return e
	}
	if saved == "" {
		saved = actual
		if _, e = s.db.ExecContext(ctx, `UPDATE organization_outputs SET identity=?,phase='copying' WHERE plan_id=? AND destination=?`, saved, p.ID, r.TagStage); e != nil {
			return e
		}
	}
	if saved != actual {
		return problem("staging_conflict", "The metadata stage was replaced; it was retained for review.")
	}
	source, e := root.Open(filepath.FromSlash(r.Original.Source))
	if e != nil {
		return e
	}
	defer source.Close()
	sourceID, e := identity(source)
	if e != nil || sourceID != r.Original.Identity {
		return problem("source_changed", "The original changed before metadata verification.")
	}
	links, e := mediaLinkCount(source)
	if e != nil {
		return e
	}
	if links > 1 && (len(r.TrackEdits) > 0 || len(r.DisableStreams) > 0) {
		return problem("linked_source", fmt.Sprintf("This file gained another hardlink after review. Make an independent copy first; %d bytes of free space are required. No file was changed.", r.Original.Size))
	}
	// Matroska title edits go through mkvpropedit directly on the registered
	// file: seconds per file instead of a full demux, remux and re-verify of
	// every packet. The journal stage holds a bounded backup of the header so
	// an interrupted edit can be repaired.
	ext := strings.ToLower(path.Ext(r.Original.Source))
	if len(r.TrackEdits) > 0 {
		return s.editEmbeddedTrackMetadata(ctx, root, stage, source, p, saved)
	}
	// Disabling Matroska tracks in place: header-only flag edits, no remux.
	if len(r.DropStreams) == 0 && len(r.DisableStreams) > 0 {
		return s.disableEmbeddedTracks(ctx, root, stage, source, p, saved)
	}
	if len(r.DropStreams) == 0 && matroskaTitleExtension(ext) && s.inPlaceTagger != nil && links == 1 {
		if e = s.backupTitleHeader(ctx, stage, source, r); e != nil {
			return e
		}
		fullPath := filepath.Join(root.Name(), filepath.FromSlash(r.Original.Source))
		// mkvpropedit opens its own handle on the path.
		source.Close()
		probe, err := s.inPlaceTagger(ctx, fullPath, ext, r.Before.Probe, r.TagTitle, r.Original.Size)
		if err != nil {
			if restoreErr := s.restoreTitleHeader(root, stage, r); restoreErr != nil {
				return problem("staging_conflict", "The in-place title edit failed and the saved header backup could not be restored: "+restoreErr.Error())
			}
			// Fall back to the verified remux stage for this file; the slow
			// path below reopens the restored source and rewrites the stage.
			if source, e = root.Open(filepath.FromSlash(r.Original.Source)); e != nil {
				return e
			}
			defer source.Close()
		} else {
			edited, e := inspect(root, r.Original.Source)
			if e != nil {
				return e
			}
			digest, e := s.fileDigest(ctx, root, r.Original.Source)
			if e != nil {
				return e
			}
			r.InPlace = true
			r.Incoming = edited
			r.Incoming.SHA = digest
			r.After = r.Before
			r.After.Size = edited.Size
			r.After.SHA = digest
			r.After.Probe = probe
			r.After.Mtime = edited.Mtime
			r.After.Identity = edited.Identity
			if _, e = s.db.ExecContext(ctx, `UPDATE organization_outputs SET phase='published' WHERE plan_id=? AND destination=? AND identity=?`, p.ID, r.TagStage, saved); e != nil {
				return e
			}
			return nil
		}
	}
	var probe string
	if len(r.DropStreams) > 0 {
		probe, e = s.rewriteTracks(ctx, source, stage, ext, r.DropStreams)
	} else {
		probe, e = s.tag(ctx, source, stage, ext, r.TagTitle)
	}
	if e != nil {
		return e
	}
	if e = checkSource(ctx, root, r.Original, true); e != nil {
		return e
	}
	stageInfo, e := inspect(root, r.TagStage)
	if e != nil {
		return e
	}
	if stageInfo.Identity != saved {
		return problem("staging_conflict", "The metadata stage changed during verification.")
	}
	digest, e := hash(ctx, stage)
	if e != nil {
		return e
	}
	stageInfo.SHA = digest
	if e = checkSource(ctx, root, stageInfo, true); e != nil {
		return e
	}
	r.Incoming = stageInfo
	r.After = r.Before
	r.After.Size = stageInfo.Size
	r.After.SHA = digest
	r.After.Probe = probe
	r.After.Mtime = stageInfo.Mtime
	r.After.Identity = stageInfo.Identity
	if r.BeforeYouTube != nil {
		copy := *r.BeforeYouTube
		copy.Size = stageInfo.Size
		copy.SHA = digest
		copy.Probe = probe
		r.AfterYouTube = &copy
	}
	if _, e = s.db.ExecContext(ctx, `UPDATE organization_outputs SET phase='published' WHERE plan_id=? AND destination=? AND identity=?`, p.ID, r.TagStage, saved); e != nil {
		return e
	}
	return nil
}

// editEmbeddedTrackMetadata applies reviewed audio/subtitle fields in place.
// The caller holds the media gate and the shared stage owns header recovery.
func (s *Service) editEmbeddedTrackMetadata(ctx context.Context, root *os.Root, stage, source *os.File, p *Plan, saved string) error {
	r := p.Replacement
	if e := s.backupTitleHeader(ctx, stage, source, r); e != nil {
		return e
	}
	fullPath := filepath.Join(root.Name(), filepath.FromSlash(r.Original.Source))
	source.Close()
	probe, err := s.matroskaTrackEditor(ctx, fullPath, r.Before.Probe, r.TrackEdits, r.Original.Size)
	if err != nil {
		if restoreErr := s.restoreTitleHeader(root, stage, r); restoreErr != nil {
			return problem("staging_conflict", "The in-place track edit failed and the saved header backup could not be restored: "+restoreErr.Error())
		}
		return err
	}
	edited, e := inspect(root, r.Original.Source)
	if e != nil {
		return e
	}
	digest, e := s.fileDigest(ctx, root, r.Original.Source)
	if e != nil {
		return e
	}
	r.InPlace = true
	r.Incoming = edited
	r.Incoming.SHA = digest
	r.After = r.Before
	r.After.Size = edited.Size
	r.After.SHA = digest
	r.After.Probe = probe
	r.After.Mtime = edited.Mtime
	r.After.Identity = edited.Identity
	_, e = s.db.ExecContext(ctx, `UPDATE organization_outputs SET phase='published' WHERE plan_id=? AND destination=? AND identity=?`, p.ID, r.TagStage, saved)
	return e
}

// disableEmbeddedTracks clears the Matroska enabled flag of the reviewed tracks
// in place. It mirrors the in-place title edit: the journal stage holds a
// bounded header backup that repairs an interrupted edit, and the plan keeps no
// second copy of the video. Caller holds the media gate.
func (s *Service) disableEmbeddedTracks(ctx context.Context, root *os.Root, stage, source *os.File, p *Plan, saved string) error {
	r := p.Replacement
	if e := s.backupTitleHeader(ctx, stage, source, r); e != nil {
		return e
	}
	fullPath := filepath.Join(root.Name(), filepath.FromSlash(r.Original.Source))
	// mkvpropedit opens its own handle on the path.
	source.Close()
	probe, err := s.matroskaDisabler(ctx, fullPath, r.Before.Probe, r.DisableStreams, r.Original.Size)
	if err != nil {
		if restoreErr := s.restoreTitleHeader(root, stage, r); restoreErr != nil {
			return problem("staging_conflict", "The in-place track edit failed and the saved header backup could not be restored: "+restoreErr.Error())
		}
		return err
	}
	edited, e := inspect(root, r.Original.Source)
	if e != nil {
		return e
	}
	digest, e := s.fileDigest(ctx, root, r.Original.Source)
	if e != nil {
		return e
	}
	r.InPlace = true
	r.Incoming = edited
	r.Incoming.SHA = digest
	r.After = r.Before
	r.After.Size = edited.Size
	r.After.SHA = digest
	r.After.Probe = probe
	r.After.Mtime = edited.Mtime
	r.After.Identity = edited.Identity
	_, e = s.db.ExecContext(ctx, `UPDATE organization_outputs SET phase='published' WHERE plan_id=? AND destination=? AND identity=?`, p.ID, r.TagStage, saved)
	return e
}

// executeEmbeddedTitle applies a reviewed metadata-only title rewrite. It keeps
// no second copy of the video: the fully verified stage atomically replaces the
// original, and the plan snapshot records the prior title, size and checksums.
// Because no bytes are retained, a completed update cannot restore the old
// container bytes; review a new title update instead.
func (s *Service) executeEmbeddedTitle(ctx context.Context, p *Plan) error {
	if p.Replacement == nil || !p.Replacement.MetadataOnly {
		return problem("invalid_selection", "This job is not a metadata-only title update.")
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
		if e = checkSource(ctx, root, p.Replacement.Original, true); e != nil {
			return e
		}
		if e = s.prepareEmbeddedTitle(ctx, root, p); e != nil {
			return e
		}
		if e = s.savePhase(ctx, p, "prepared"); e != nil {
			return e
		}
	}
	if p.Phase == "prepared" {
		published, e := s.embeddedTitleAlreadyPublished(ctx, root, p)
		if e != nil {
			return e
		}
		if !published {
			if e = s.validateReplacement(ctx, root, s.db, *p); e != nil {
				return e
			}
			// An in-place edit has already changed the file, so comparing it
			// against the pre-edit checksum would always fail; the plan's
			// verified Incoming facts are the post-edit record instead.
			if !p.Replacement.InPlace {
				if e = checkSource(ctx, root, p.Replacement.Original, true); e != nil {
					return e
				}
			}
			if e = checkSource(ctx, root, p.Replacement.Incoming, true); e != nil {
				return e
			}
			if e = s.publishEmbeddedTitle(ctx, root, p); e != nil {
				return e
			}
		}
		if e = s.savePhase(ctx, p, "published"); e != nil {
			return e
		}
	}
	if p.Phase == "published" {
		if e = s.commitEmbeddedTitle(ctx, root, p); e != nil {
			return e
		}
	}
	if p.Phase != "committed" {
		return problem("revision_conflict", "The embedded title update has an unexpected saved phase.")
	}
	r := p.Replacement
	// The rename normally consumes the stage. An in-place edit keeps only a
	// header backup there, so it is removed by its own recorded identity.
	if r.TagStage != "" {
		if f, err := root.Open(filepath.FromSlash(r.TagStage)); err == nil {
			actual, _ := identity(f)
			f.Close()
			if actual != "" && (actual == r.Incoming.Identity || actual == r.BackupIdentity) {
				_ = root.Remove(filepath.FromSlash(r.TagStage))
			}
		}
	}
	if p.TrackCleanup != nil {
		return nil
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `UPDATE organization_plans SET state='completed',message='',updated_at=unixepoch() WHERE id=?`, p.ID); e != nil {
		return e
	}
	if r.TagParent != "" {
		if _, e = tx.ExecContext(ctx, `UPDATE organization_plans SET state='queued',message='',updated_at=unixepoch() WHERE id=(SELECT id FROM organization_plans WHERE state='review' AND json_extract(snapshot,'$.replacement.tag_parent')=? ORDER BY rowid LIMIT 1)`, r.TagParent); e != nil {
			return e
		}
	}
	return tx.Commit()
}

// embeddedTitleAlreadyPublished recognizes a rename that completed before the
// saved phase advanced. It never guesses: the original path must match the
// prepared output's recorded identity, size, mtime and checksum exactly.
func (s *Service) embeddedTitleAlreadyPublished(ctx context.Context, root *os.Root, p *Plan) (bool, error) {
	r := p.Replacement
	_, e := root.Lstat(filepath.FromSlash(r.TagStage))
	if e == nil {
		return false, nil
	}
	if !errors.Is(e, os.ErrNotExist) {
		return false, e
	}
	after := r.After.companion(r.Original.Source)
	if e = checkSource(ctx, root, after, true); e != nil {
		return false, problem("source_changed", "The verified title stage is missing and the published file no longer matches it. Review the file before retrying.")
	}
	return true, nil
}

// publishEmbeddedTitle replaces the original path with the verified stage.
// Rename is the only write: on Linux the original either remains or is fully
// replaced, and a torrent seeding hardlink continues to reference old bytes.
// An in-place edit has no stage to swap: the registered file already carries
// the verified title, so publish only confirms its recorded facts.
func (s *Service) publishEmbeddedTitle(ctx context.Context, root *os.Root, p *Plan) error {
	r := p.Replacement
	if r.InPlace {
		published := r.Incoming
		published.Source = r.Original.Source
		if e := checkSource(ctx, root, published, true); e != nil {
			return problem("destination_changed", "The edited title file changed during verification. Review the file before retrying.")
		}
		return nil
	}
	if e := checkSource(ctx, root, r.Incoming, true); e != nil {
		return e
	}
	if e := checkSource(ctx, root, r.Original, true); e != nil {
		return e
	}
	if e := root.Rename(filepath.FromSlash(r.TagStage), filepath.FromSlash(r.Original.Source)); e != nil {
		return problem("publish_failed", "The verified title update could not be published. The original was left unchanged.")
	}
	published := r.Incoming
	published.Source = r.Original.Source
	if e := checkSource(ctx, root, published, true); e != nil {
		return problem("destination_changed", "The published title update changed during verification. Review the file before retrying.")
	}
	return nil
}

// commitEmbeddedTitle records the verified replacement facts in one transaction.
// The previous title stays in the plan snapshot as the durable change record.
func (s *Service) commitEmbeddedTitle(ctx context.Context, root *os.Root, p *Plan) error {
	r := p.Replacement
	after := r.After.companion(r.Original.Source)
	if e := checkSource(ctx, root, after, true); e != nil {
		return problem("destination_changed", "The published title update changed before registration. Review the file before retrying.")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	before, mediaID, e := replacementMetadata(ctx, tx, r.FileID)
	if e != nil {
		return e
	}
	if mediaID != r.MediaID || !reflect.DeepEqual(before, r.Before) {
		return problem("revision_conflict", "The registered file changed during the title update.")
	}
	eps, e := s.episodes(ctx, tx, r.FileID)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(eps, r.Episodes) {
		return problem("revision_conflict", "Episode mappings changed during the title update.")
	}
	if e = idleExcept(ctx, tx, r.MediaID, r.FileID, "", ""); e != nil {
		return e
	}
	youtube, e := youtubeVersion(ctx, tx, r.FileID)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(youtube, r.BeforeYouTube) {
		return problem("revision_conflict", "YouTube provenance changed during the title update.")
	}
	a := r.After
	result, e := tx.ExecContext(ctx, `UPDATE media_files SET relative_path=?,size_bytes=?,sha256=?,probe_json=?,verification_json='{}' WHERE id=? AND relative_path=? AND sha256=?`, a.Path, a.Size, a.SHA, a.Probe, r.FileID, r.Before.Path, r.Before.SHA)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return problem("revision_conflict", "The registered file changed during the title update.")
	}
	if r.AfterYouTube != nil {
		y := r.AfterYouTube
		if _, e = tx.ExecContext(ctx, `UPDATE youtube_files SET size_bytes=?,sha256=?,probe_json=? WHERE media_file_id=?`, y.Size, y.SHA, y.Probe, r.FileID); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM subtitle_inspections WHERE media_file_id=?`, r.FileID); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE subtitle_jobs SET requested=0,selected_candidate='',state='queued',next_run=0,attempts=0,message='Video metadata changed; subtitle timing may require review.',updated_at=unixepoch() WHERE media_file_id=?`, r.FileID); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM subtitle_candidates WHERE media_file_id=?`, r.FileID); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM subtitles WHERE media_file_id=? AND source='embedded'`, r.FileID); e != nil {
		return e
	}
	activity := "embedded title updated"
	if len(r.DropStreams) > 0 {
		activity = "embedded tracks removed"
	} else if r.PreviousTitle != nil {
		activity = "embedded title updated; previous title recorded"
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'title',?,?,?)`, r.MediaID, r.Title, activity); e != nil {
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
	return tx.Commit()
}

// finishOrganization durably queues the single approved title update only after
// the move has completed. An existing child is reused after interruption.
func (s *Service) finishOrganization(ctx context.Context, p *Plan) error {
	if !p.UpdateEmbeddedTitle {
		_, e := s.db.ExecContext(ctx, `UPDATE organization_plans SET state='completed',message='',updated_at=unixepoch() WHERE id=?`, p.ID)
		return e
	}
	if len(p.Files) < 1 || len(p.Files) > 100 {
		return problem("invalid_selection", "This saved embedded-title review is invalid.")
	}
	children := make([]string, 0, len(p.Files))
	for _, file := range p.Files {
		approvedTitle := file.EmbeddedTitle
		if approvedTitle == "" && len(p.Files) == 1 {
			approvedTitle = p.EmbeddedTitle
		}
		if approvedTitle == "" {
			return problem("invalid_selection", "The approved embedded title is missing.")
		}
		var childID string
		e := s.db.QueryRowContext(ctx, `SELECT id FROM organization_plans WHERE json_extract(snapshot,'$.replacement.tag_parent')=? AND json_extract(snapshot,'$.replacement.media_file_id')=? ORDER BY rowid LIMIT 1`, p.ID, file.FileID).Scan(&childID)
		if errors.Is(e, sql.ErrNoRows) {
			var child Plan
			child, e = s.previewEmbeddedTitle(ctx, file.FileID, p.ID)
			if e == nil {
				childID = child.ID
			}
		}
		if e != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var typed *Error
			if !errors.As(e, &typed) && !errors.Is(e, sql.ErrNoRows) {
				return e
			}
			_, err := s.db.ExecContext(ctx, `UPDATE organization_plans SET state='completed',message='File organization completed; the embedded title update could not be queued. Review it separately.',updated_at=unixepoch() WHERE id=?`, p.ID)
			return err
		}
		child, e := s.Plan(ctx, childID)
		if e != nil {
			return e
		}
		if child.Replacement == nil || child.Replacement.TagParent != p.ID || child.Replacement.TagTitle != approvedTitle || child.State != "review" {
			_, err := s.db.ExecContext(ctx, `UPDATE organization_plans SET state='completed',message='File organization completed; the saved title changed. Review an embedded title update separately.',updated_at=unixepoch() WHERE id=?`, p.ID)
			return err
		}
		children = append(children, child.ID)
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	parentResult, e := tx.ExecContext(ctx, `UPDATE organization_plans SET state='completed',message='File organization completed; the reviewed embedded title update is queued.',updated_at=unixepoch() WHERE id=? AND state='running'`, p.ID)
	if e != nil {
		return e
	}
	parentCount, e := parentResult.RowsAffected()
	if e != nil {
		return e
	}
	if parentCount != 1 {
		return problem("revision_conflict", "The organization job changed before the title update was queued.")
	}
	if len(children) > 0 {
		result, e := tx.ExecContext(ctx, `UPDATE organization_plans SET state='queued',message='',updated_at=unixepoch() WHERE id=? AND state='review'`, children[0])
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return problem("revision_conflict", "The embedded title review changed before queueing.")
		}
	}
	return tx.Commit()
}
