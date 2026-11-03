package organization

import (
	"context"
	"errors"
	"os"

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
