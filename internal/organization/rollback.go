package organization

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"time"
)

// previewMoveRollback builds a new inverse job from saved paths, never from
// browser-supplied paths. The caller holds ReadMedia. Current naming/assignment
// preferences remain in force for future imports; only reviewed paths return.
func (s *Service) previewMoveRollback(ctx context.Context, old Plan) (Plan, error) {
	if len(old.Files) == 0 || len(old.Files) > 100 {
		return Plan{}, problem("invalid_selection", "Choose a completed move with 1–100 registered files.")
	}
	root, resolved, err := s.openRoot(old.Root)
	if err != nil {
		return Plan{}, err
	}
	defer root.Close()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Plan{}, err
	}
	defer tx.Rollback()
	config, err := s.config(ctx, tx)
	if err != nil {
		return Plan{}, err
	}
	now := time.Now().Unix()
	p := Plan{ID: id(), RestoresPlan: old.ID, State: "review", Root: resolved, ConfigRevision: config.Revision, EntireMediaID: old.EntireMediaID, Phase: "planned", Created: now, Updated: now, Files: []PlannedFile{}}
	destinations := map[string]bool{}
	for _, previous := range old.Files {
		current, mediaID, e := replacementMetadata(ctx, tx, previous.FileID)
		if e != nil {
			return Plan{}, e
		}
		if mediaID != previous.MediaID || current.Path != previous.Destination {
			return Plan{}, problem("revision_conflict", "A moved file no longer matches this move. Review its latest location before restoring.")
		}
		if current.SHA != previous.SHA || current.Size != previous.Size {
			// A metadata-only title update changes bytes without keeping a copy.
			// Undoing the move restores the path and keeps the current verified
			// title. Any other content change still requires a reviewed restore.
			changed, e := metadataOnlyChangeRecorded(ctx, tx, previous.FileID, current.SHA, current.Size)
			if e != nil {
				return Plan{}, e
			}
			if !changed {
				return Plan{}, problem("revision_conflict", "A moved file has since changed. Restore its latest version before reviewing this move.")
			}
		}
		if e = idle(ctx, tx, mediaID, previous.FileID); e != nil {
			return Plan{}, e
		}
		info, e := inspect(root, current.Path)
		if e != nil {
			return Plan{}, e
		}
		info.SHA = current.SHA
		if info.Size != current.Size {
			return Plan{}, problem("source_changed", "A moved file changed since registration.")
		}
		snapshot, e := s.captureFile(ctx, tx, mediaID, previous.FileID, "")
		if e != nil {
			return Plan{}, e
		}
		episodes, e := s.episodes(ctx, tx, previous.FileID)
		if e != nil {
			return Plan{}, e
		}
		if !reflect.DeepEqual(episodes, previous.Episodes) {
			return Plan{}, problem("revision_conflict", "Episode mappings changed since this move.")
		}
		companions, e := companions(ctx, root, current.Path, previous.Source)
		if e != nil {
			return Plan{}, e
		}
		if old.FolderMove != nil {
			companions = []Companion{}
			for _, prior := range previous.Companions {
				c, e := inspect(root, prior.Destination)
				if e != nil {
					return Plan{}, e
				}
				c.Destination = prior.Source
				companions = append(companions, c)
			}
		}
		rows, e := tx.QueryContext(ctx, `SELECT relative_path FROM subtitles WHERE media_file_id=? AND relative_path<>'' UNION SELECT destination FROM subtitle_imports WHERE media_file_id=? AND phase='committed'`, previous.FileID, previous.FileID)
		if e != nil {
			return Plan{}, e
		}
		missing := false
		for rows.Next() {
			var name string
			if e = rows.Scan(&name); e != nil {
				rows.Close()
				return Plan{}, e
			}
			found := false
			for _, c := range companions {
				if c.Source == name {
					found = true
					break
				}
			}
			if !found {
				missing = true
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return Plan{}, e
		}
		if missing {
			return Plan{}, problem("unsupported_companion", "A registered subtitle has an unrelated location. Review it before undoing this move.")
		}
		v := PlannedFile{FileID: previous.FileID, MediaID: mediaID, Title: snapshot.Content.Title, Source: current.Path, Destination: previous.Source, Size: current.Size, SHA: current.SHA, Mtime: info.Mtime, Identity: info.Identity, Snapshot: snapshot, Episodes: episodes, Companions: companions, Unchanged: current.Path == previous.Source}
		if e = checkRollbackDestinations(root, v, destinations); e != nil {
			return Plan{}, e
		}
		var count int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_files WHERE relative_path=? COLLATE NOCASE AND id<>?`, v.Destination, v.FileID).Scan(&count); e != nil {
			return Plan{}, e
		}
		if count != 0 {
			return Plan{}, problem("destination_exists", "The original path belongs to another registered file.")
		}
		p.Files = append(p.Files, v)
	}
	if old.FolderMove != nil {
		inventory, e := inventoryFolder(ctx, root, tx, old.FolderMove.Destination, old.EntireMediaID)
		if e != nil {
			return Plan{}, e
		}
		exists, e := existsFolded(root, old.FolderMove.Source)
		if e != nil {
			return Plan{}, e
		}
		if exists {
			return Plan{}, problem("destination_exists", "The original title folder is occupied. It was left unchanged.")
		}
		p.FolderMove = &FolderMove{Source: old.FolderMove.Destination, Destination: old.FolderMove.Source, Inventory: inventory}
	}
	if p.FolderMove != nil {
		err = s.validateFolder(ctx, root, tx, p, false)
	} else {
		err = s.validate(ctx, root, tx, p, false)
	}
	if err != nil {
		return Plan{}, err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return Plan{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO organization_plans(id,state,snapshot,created_at,updated_at) VALUES(?,'review',?,?,?)`, p.ID, string(raw), now, now); err != nil {
		return Plan{}, err
	}
	return p, tx.Commit()
}

// metadataOnlyChangeRecorded accepts a current file only when its exact bytes
// are the verified output of a completed metadata-only title update. The update
// journal is the proof; an arbitrary external edit is not accepted.
func metadataOnlyChangeRecorded(ctx context.Context, q query, fileID int64, sha string, size int64) (bool, error) {
	var count int
	e := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM organization_plans WHERE state='completed' AND json_extract(snapshot,'$.replacement.media_file_id')=? AND json_extract(snapshot,'$.replacement.metadata_only')=1 AND json_extract(snapshot,'$.replacement.after.sha256')=? AND json_extract(snapshot,'$.replacement.after.size_bytes')=?`, fileID, sha, size).Scan(&count)
	return count > 0, e
}

func checkRollbackDestinations(root *os.Root, v PlannedFile, seen map[string]bool) error {
	all := append([]Companion{{Source: v.Source, Destination: v.Destination}}, v.Companions...)
	for _, c := range all {
		if !validManagedRelative(c.Source) || !validManagedRelative(c.Destination) {
			return problem("unsafe_path", "This saved move has an unsafe path.")
		}
		key := strings.ToLower(c.Destination)
		if seen[key] {
			return problem("destination_exists", "Two restored files would share a destination.")
		}
		seen[key] = true
		if c.Source == c.Destination {
			continue
		}
		exists, err := existsFolded(root, c.Destination)
		if err != nil {
			return err
		}
		if exists {
			return problem("destination_exists", "An original path is occupied. It was left unchanged.")
		}
	}
	return nil
}
