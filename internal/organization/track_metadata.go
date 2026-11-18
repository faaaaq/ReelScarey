package organization

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var trackMetadataLanguage = regexp.MustCompile(`(?i)^(?:und|[a-z]{2,3}(?:-[a-z0-9]{2,8})*)$`)

func validateTrackMetadataEdits(edits []TrackMetadataEdit) error {
	if len(edits) == 0 || len(edits) > 200 {
		return problem("invalid_selection", "Choose between 1 and 200 audio or subtitle track edits.")
	}
	seen := map[int]bool{}
	for _, edit := range edits {
		if edit.Index < 0 || seen[edit.Index] || edit.Title == nil && edit.Language == nil && edit.Default == nil && edit.Forced == nil {
			return problem("invalid_selection", "Each selected track needs at least one changed field.")
		}
		seen[edit.Index] = true
		if edit.Title != nil && (len(*edit.Title) > 1024 || strings.ContainsAny(*edit.Title, "\x00\r\n")) {
			return problem("invalid_selection", "Track titles must be at most 1024 bytes without control characters.")
		}
		if edit.Language != nil && !trackMetadataLanguage.MatchString(*edit.Language) {
			return problem("invalid_selection", "Use a BCP 47 language code such as eng, jpn, en-US, or und for an unknown language.")
		}
	}
	return nil
}

// PreviewTrackMetadata uses the existing replacement review and recovery
// machinery. Previewing reads and fingerprints the registered source but never
// invokes MKVToolNix or changes media bytes.
func (s *Service) PreviewTrackMetadata(ctx context.Context, fileID int64, edits []TrackMetadataEdit) (Plan, error) {
	if err := validateTrackMetadataEdits(edits); err != nil {
		return Plan{}, err
	}
	before, _, err := replacementMetadata(ctx, s.db, fileID)
	if err != nil {
		return Plan{}, err
	}
	if ext := strings.ToLower(path.Ext(before.Path)); !matroskaTitleExtension(ext) {
		return Plan{}, problem("unsupported_media", "In-place track field editing is supported only for Matroska (.mkv/.webm) files.")
	}
	root, _, err := s.openRoot("")
	if err != nil {
		return Plan{}, err
	}
	source, err := root.Open(filepath.FromSlash(before.Path))
	if err != nil {
		root.Close()
		return Plan{}, err
	}
	links, err := mediaLinkCount(source)
	source.Close()
	root.Close()
	if err != nil {
		return Plan{}, err
	}
	if links > 1 {
		return Plan{}, problem("linked_source", fmt.Sprintf("This file shares its bytes with another link, such as a seeding copy. Make an independent copy first; %d bytes of free space are required. No file was changed.", before.Size))
	}
	plan, err := s.PreviewEmbeddedTitle(ctx, fileID)
	if err != nil {
		return Plan{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Plan{}, err
	}
	defer tx.Rollback()
	var state, raw string
	if err = tx.QueryRowContext(ctx, `SELECT state,snapshot FROM organization_plans WHERE id=?`, plan.ID).Scan(&state, &raw); err != nil {
		return Plan{}, err
	}
	if state != "review" {
		return Plan{}, problem("revision_conflict", "The metadata review changed before track fields could be attached.")
	}
	var saved Plan
	if err = json.Unmarshal([]byte(raw), &saved); err != nil || saved.Replacement == nil {
		return Plan{}, problem("revision_conflict", "The saved metadata review could not be read.")
	}
	saved.Replacement.TrackEdits = append([]TrackMetadataEdit(nil), edits...)
	updated, err := json.Marshal(saved)
	if err != nil {
		return Plan{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE organization_plans SET snapshot=?,updated_at=? WHERE id=? AND state='review'`, updated, time.Now().Unix(), plan.ID); err != nil {
		return Plan{}, err
	}
	if err = tx.Commit(); err != nil {
		return Plan{}, err
	}
	return saved, nil
}
