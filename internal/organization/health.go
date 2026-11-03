package organization

import (
	"context"
	"errors"
	"os"
)

// FileHealth compares a registered file with its saved import checksum without
// changing the file. Fast registrations gain their first full checksum here.
// Results describe one point in time.
type FileHealth struct {
	FileID int64  `json:"media_file_id"`
	Path   string `json:"path"`
	Status string `json:"status"`
}

func (s *Service) CheckFileHealth(ctx context.Context, fileID int64) (FileHealth, error) {
	if !s.healthMu.TryLock() {
		return FileHealth{}, problem("media_busy", "Another file integrity check is running. Try again when it finishes.")
	}
	defer s.healthMu.Unlock()
	unlock := ReadMedia()
	defer unlock()
	var out FileHealth
	var size int64
	var checksum string
	out.FileID = fileID
	if err := EnsureMediaAvailable(ctx, s.db); err != nil {
		return out, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT relative_path,size_bytes,sha256 FROM media_files WHERE id=?`, fileID).Scan(&out.Path, &size, &checksum); err != nil {
		return out, err
	}
	root, _, err := s.openRoot("")
	if err != nil {
		return out, err
	}
	defer root.Close()
	info, err := inspect(root, out.Path)
	if errors.Is(err, os.ErrNotExist) {
		out.Status = "missing"
		return out, nil
	}
	if err != nil {
		var unsafe *Error
		if errors.As(err, &unsafe) {
			out.Status = "unsafe"
			return out, nil
		}
		return out, err
	}
	if info.Size != size {
		out.Status = "changed"
		return out, nil
	}
	if checksum == "" {
		if err = s.prepareChecksums(ctx, root, []int64{fileID}, 0); err != nil {
			var changed *Error
			if errors.As(err, &changed) {
				out.Status = "changed"
				return out, nil
			}
			return out, err
		}
		if err = s.db.QueryRowContext(ctx, `SELECT sha256 FROM media_files WHERE id=?`, fileID).Scan(&checksum); err != nil {
			return out, err
		}
	}
	info.SHA = checksum
	err = checkSource(ctx, root, info, true)
	if errors.Is(err, os.ErrNotExist) {
		out.Status = "missing"
		return out, nil
	}
	if err != nil {
		var changed *Error
		if errors.As(err, &changed) {
			out.Status = "changed"
			return out, nil
		}
		return out, err
	}
	// Recheck the path after hashing, including its identity, to detect a swap.
	if err = checkSource(ctx, root, info, false); errors.Is(err, os.ErrNotExist) {
		out.Status = "missing"
		return out, nil
	} else if err != nil {
		var changed *Error
		if errors.As(err, &changed) {
			out.Status = "changed"
			return out, nil
		}
		return out, err
	}
	// Another process might change the catalog even while the media gate is held.
	var path, saved string
	var savedSize int64
	if err = s.db.QueryRowContext(ctx, `SELECT relative_path,size_bytes,sha256 FROM media_files WHERE id=?`, fileID).Scan(&path, &savedSize, &saved); err != nil {
		return out, err
	}
	if path != out.Path || savedSize != size || saved != checksum {
		return out, problem("revision_conflict", "The saved file changed during inspection. Try again.")
	}
	out.Status = "verified"
	return out, nil
}
