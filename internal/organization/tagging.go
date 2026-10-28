package organization

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
func (s *Service) previewEmbeddedTitle(ctx context.Context, fileID int64, parentID string) (Plan, error) {
	root, resolved, e := s.openRoot("")
	if e != nil {
		return Plan{}, e
	}
	defer root.Close()
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
	if ext != ".mkv" && ext != ".mp4" && ext != ".m4v" {
		return Plan{}, problem("invalid_selection", "Embedded title updates currently support MP4 and Matroska files.")
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
	value := embeddedTitle(title, episodes)
	if value == "" || len(value) > 1024 || strings.ContainsAny(value, "\x00\r\n") {
		return Plan{}, problem("invalid_selection", "The saved title cannot be safely embedded.")
	}
	now := time.Now().Unix()
	pid := id()
	r := &Replacement{FileID: fileID, MediaID: mediaID, Title: title, Before: before, BeforeYouTube: youtube, Original: old, Incoming: old, Backup: path.Join(".reelscarey-versions", ".reelscarey-version-"+pid), TagStage: path.Join(".reelscarey-versions", ".reelscarey-tag-"+pid), TagTitle: value, TagParent: parentID, Episodes: episodes}
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

func (s *Service) prepareEmbeddedTitle(ctx context.Context, root *os.Root, p *Plan) error {
	r := p.Replacement
	if s.tag == nil {
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
	probe, e := s.tag(ctx, source, stage, path.Ext(r.Original.Source), r.TagTitle)
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

// finishOrganization durably queues the single approved title update only after
// the move has completed. An existing child is reused after interruption.
func (s *Service) finishOrganization(ctx context.Context, p *Plan) error {
	if !p.UpdateEmbeddedTitle {
		_, e := s.db.ExecContext(ctx, `UPDATE organization_plans SET state='completed',message='',updated_at=unixepoch() WHERE id=?`, p.ID)
		return e
	}
	if len(p.Files) != 1 || p.EmbeddedTitle == "" {
		return problem("invalid_selection", "This saved embedded-title review is invalid.")
	}
	var childID string
	e := s.db.QueryRowContext(ctx, `SELECT id FROM organization_plans WHERE json_extract(snapshot,'$.replacement.tag_parent')=? ORDER BY rowid LIMIT 1`, p.ID).Scan(&childID)
	if errors.Is(e, sql.ErrNoRows) {
		var child Plan
		child, e = s.previewEmbeddedTitle(ctx, p.Files[0].FileID, p.ID)
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
	if child.Replacement == nil || child.Replacement.TagParent != p.ID || child.Replacement.TagTitle != p.EmbeddedTitle || child.State != "review" {
		_, err := s.db.ExecContext(ctx, `UPDATE organization_plans SET state='completed',message='File organization completed; the saved title changed. Review an embedded title update separately.',updated_at=unixepoch() WHERE id=?`, p.ID)
		return err
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
	result, e := tx.ExecContext(ctx, `UPDATE organization_plans SET state='queued',message='',updated_at=unixepoch() WHERE id=? AND state='review'`, child.ID)
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
	return tx.Commit()
}
