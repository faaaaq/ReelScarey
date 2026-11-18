package organization

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/faaaaq/ReelScarey/internal/filecheck"
)

// Deletion reviews bind to a file identity and bounded content observation.
// Unlike a replacement or copy, unlinking does not need a full media checksum.
func observeDeletionAsset(ctx context.Context, root *os.Root, asset Companion) (filecheck.Snapshot, error) {
	var empty filecheck.Snapshot
	if err := checkSource(ctx, root, asset, false); err != nil {
		return empty, err
	}
	f, err := root.Open(filepath.FromSlash(asset.Source))
	if err != nil {
		return empty, err
	}
	defer f.Close()
	observation, err := filecheck.Capture(ctx, f)
	if err != nil {
		return empty, err
	}
	if err = checkSource(ctx, root, asset, false); err != nil {
		return empty, err
	}
	return observation, nil
}

func deletionObservation(removal *Removal, name string) *filecheck.Snapshot {
	observation, ok := removal.Observations[name]
	if !ok {
		return nil
	}
	return &observation
}

func verifyDeletionAsset(ctx context.Context, root *os.Root, asset Companion, expected *filecheck.Snapshot, missingOK bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkSource(ctx, root, asset, false); err != nil {
		if missingOK && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if expected == nil {
		if asset.Size == 0 {
			return nil
		}
		if asset.SHA == "" {
			return problem("invalid_selection", "The deletion review has no file verification. Create a new review.")
		}
		return checkSource(ctx, root, asset, true)
	}
	actual, err := observeDeletionAsset(ctx, root, asset)
	if err != nil {
		return err
	}
	if !filecheck.SameContent(actual, *expected) {
		return problem("source_changed", "A file changed after deletion was reviewed. It was not deleted.")
	}
	return nil
}

func removeDeletionAsset(ctx context.Context, root *os.Root, asset Companion, expected *filecheck.Snapshot) error {
	if err := verifyDeletionAsset(ctx, root, asset, expected, true); err != nil {
		return err
	}
	if err := root.Remove(filepath.FromSlash(asset.Source)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return problem("delete_failed", "A reviewed file could not be permanently deleted. Check its permissions, then resume this deletion.")
	}
	return nil
}

// Commit the reviewed catalog removal before unlinking. The committed snapshot
// remains the durable cleanup journal: a crash resumes only these exact files,
// and changed/replaced paths are refused. No retained copy is made.
func (s *Service) executePermanentDeletion(ctx context.Context, p *Plan) error {
	if p.Removal == nil || p.Removal.Selection == "" || p.Removal.Mode != "delete" {
		return problem("invalid_selection", "This is not a permanent deletion review.")
	}
	root, _, err := s.openRoot(p.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	if p.Phase != "committed" {
		if p.Phase != "planned" {
			return problem("plan_stale", "Create a new deletion review.")
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err = s.validateDeletion(ctx, root, tx, *p, false); err != nil {
			return err
		}
		if err = deleteSelectionRows(ctx, tx, p.Removal); err != nil {
			return err
		}
		committed := *p
		committed.Phase = "committed"
		raw, err := json.Marshal(committed)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE organization_plans SET snapshot=?,updated_at=unixepoch() WHERE id=?`, string(raw), p.ID); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		p.Phase = "committed"
	}
	for _, asset := range p.Removal.Assets {
		// The owner can stop a running deletion; the remaining files are left
		// in place and a fresh apply resumes the reviewed journal.
		if stopped, e := s.ownerStopped(ctx, p.ID); e != nil {
			return e
		} else if stopped {
			return errOwnerStopped
		}
		if err = removeDeletionAsset(ctx, root, asset, deletionObservation(p.Removal, asset.Source)); err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE organization_plans SET state='completed',message='',updated_at=unixepoch() WHERE id=?`, p.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'organization',?,?,'permanently deleted')`, p.ID, p.Removal.Title); err != nil {
		return err
	}
	return tx.Commit()
}
