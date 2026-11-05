package organization

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"

	"github.com/faaaaq/ReelScarey/internal/filecheck"
)

// Call before opening a review transaction. The caller holds the media gate;
// actual file operations still recheck these full digests through their journals.
func (s *Service) prepareChecksums(ctx context.Context, root *os.Root, ids []int64, mediaID int64) error {
	if mediaID != 0 {
		var err error
		ids, err = titleFiles(ctx, s.db, mediaID)
		if err != nil {
			return err
		}
	}
	if len(ids) > 100 {
		return problem("invalid_selection", "Choose at most 100 registered files for one review.")
	}
	for _, id := range ids {
		if _, err := filecheck.EnsureSHA(ctx, s.db, root, id); err != nil {
			if errors.Is(err, filecheck.ErrChanged) {
				return problem("source_changed", "The registered file changed since discovery. No files were modified.")
			}
			return err
		}
	}
	return nil
}

// prepareQuality fills HDR/SDR facts that predate the current probe schema.
// Only files with a measured resolution are refreshed, so explicitly
// unverified files keep that status. A failed probe leaves the transfer fact
// unknown and the reviewed filename omits the HDR/SDR tag.
func (s *Service) prepareQuality(ctx context.Context, root *os.Root, ids []int64, mediaID int64) error {
	if s.probe == nil {
		return nil
	}
	if mediaID != 0 {
		var err error
		ids, err = titleFiles(ctx, s.db, mediaID)
		if err != nil {
			return err
		}
	}
	if len(ids) > 100 {
		return problem("invalid_selection", "Choose at most 100 registered files for one review.")
	}
	for _, id := range ids {
		var rel string
		var resolution int
		var hdr sql.NullInt64
		if err := s.db.QueryRowContext(ctx, `SELECT relative_path,video_resolution,video_hdr FROM media_files WHERE id=?`, id).Scan(&rel, &resolution, &hdr); err != nil {
			return err
		}
		if resolution <= 0 || hdr.Valid {
			continue
		}
		f, err := root.Open(filepath.FromSlash(rel))
		if err != nil {
			continue
		}
		raw, probeErr := s.probe(ctx, f)
		f.Close()
		if probeErr != nil {
			continue
		}
		if _, err = s.db.ExecContext(ctx, `UPDATE media_files SET probe_json=? WHERE id=? AND relative_path=?`, raw, id, rel); err != nil {
			return err
		}
	}
	return nil
}
