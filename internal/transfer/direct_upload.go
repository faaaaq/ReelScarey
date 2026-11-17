package transfer

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/faaaaq/ReelScarey/internal/organization"
)

// A browser-picked import copies one file from the owner's computer into the
// title's library folder and registers it through the exact reviewed flow a
// discovered file uses. The browser cannot address server filesystem paths,
// so the copy is explicit: the destination folder is resolved (and created
// through the naming templates when the title has none) and shown for
// confirmation before the upload, the transfer is bounded, and nothing is
// ever overwritten. Until the reviewed registration succeeds, the incoming
// copy lives under a dot-prefixed staging name that every library walker and
// direct browse ignores, and a failed or abandoned upload removes it. The
// copy holds the media read fence for its duration, so a reviewed
// organization batch waits rather than racing the write.

const (
	// MaxDirectUploadBytes bounds one browser-picked copy; it is far above any
	// single remux and exists so a runaway transfer fails instead of filling
	// the media mount silently.
	MaxDirectUploadBytes = 512 << 30
	maxDirectUploadName  = 160
)

type DirectUploadRequest struct {
	Kind       string  `json:"kind"`
	Collection string  `json:"collection"`
	MediaID    int64   `json:"media_id"`
	Episodes   []int64 `json:"episode_ids"`
	Name       string  `json:"name"`
	Reviewed   bool    `json:"reviewed"`
}

// DirectUploadPrepare resolves and, when the title has no folder yet, creates
// the destination folder a browser upload will copy into. The returned path
// is media-root relative and slash separated; the interface shows it for the
// owner's confirmation before any bytes move.
func (s *Service) DirectUploadPrepare(ctx context.Context, kind, collection string, mediaID int64) (string, error) {
	defer organization.ReadMedia()()
	if e := organization.EnsureMediaAvailable(ctx, s.db); e != nil {
		return "", e
	}
	roots, e := s.directImportScope(ctx, kind, collection)
	if e != nil {
		return "", e
	}
	if e = s.directUploadTarget(ctx, kind, mediaID); e != nil {
		return "", e
	}
	return s.directUploadDir(ctx, mediaID, roots)
}

// DirectUpload streams one browser-picked file into the resolved destination
// folder and registers it in place for the selected title. Registration keeps
// the reviewed confirmation, the duplicate and target fences and the shared
// verification worker of every discovered file; the copy is never a
// replacement and never overwrites anything.
func (s *Service) DirectUpload(ctx context.Context, in DirectUploadRequest, src io.Reader) (LibraryScan, error) {
	defer organization.ReadMedia()()
	if e := organization.EnsureMediaAvailable(ctx, s.db); e != nil {
		return LibraryScan{}, e
	}
	roots, e := s.directImportScope(ctx, in.Kind, in.Collection)
	if e != nil {
		return LibraryScan{}, e
	}
	if !in.Reviewed || in.MediaID < 1 || len(in.Episodes) > 20 {
		return LibraryScan{}, problem("invalid_mapping", "Confirm the title and episode match before importing this file.")
	}
	name, e := directUploadName(in.Name)
	if e != nil {
		return LibraryScan{}, e
	}
	if e = s.directUploadTarget(ctx, in.Kind, in.MediaID); e != nil {
		return LibraryScan{}, e
	}
	dir, e := s.directUploadDir(ctx, in.MediaID, roots)
	if e != nil {
		return LibraryScan{}, e
	}
	root, e := s.existingRoot(ctx, nil)
	if e != nil {
		return LibraryScan{}, e
	}
	defer root.Close()
	if e = segments(root, dir, false); e != nil {
		return LibraryScan{}, problem("unsafe_path", "The destination folder is missing, inaccessible or contains a symbolic link.")
	}
	relative := dir + "/" + name
	if _, e = root.Lstat(filepath.FromSlash(relative)); e == nil {
		return LibraryScan{}, problem("destination_exists", "A file with this name is already in the destination folder. Rename the file and try again.")
	} else if !errors.Is(e, os.ErrNotExist) {
		return LibraryScan{}, problem("storage_unavailable", "The destination folder could not be checked.")
	}
	staging := dir + "/.uploading-" + ID() + "-" + name
	file, e := root.Create(filepath.FromSlash(staging))
	if e != nil {
		return LibraryScan{}, problem("storage_unavailable", "The destination folder could not be written to. Check media mount permissions and free space.")
	}
	written, writeErr := copyUpload(ctx, file, src)
	if closeErr := file.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr == nil && written <= 0 {
		writeErr = errors.New("the uploaded file is empty")
	}
	if writeErr != nil {
		root.Remove(filepath.FromSlash(staging))
		if errors.Is(writeErr, context.Canceled) || errors.Is(writeErr, context.DeadlineExceeded) {
			return LibraryScan{}, problem("upload_cancelled", "The upload was interrupted. Nothing was copied or registered.")
		}
		return LibraryScan{}, problem("upload_failed", "The copy failed before finishing. Nothing was registered.")
	}
	if e := root.Rename(filepath.FromSlash(staging), filepath.FromSlash(relative)); e != nil {
		root.Remove(filepath.FromSlash(staging))
		return LibraryScan{}, problem("destination_exists", "A file with this name appeared in the destination folder. Nothing was overwritten; rename the file and try again.")
	}
	// Registration reuses the identical reviewed-registration fence a
	// discovered file uses: loaded seasons, bounded consecutive targets,
	// reservation conflicts, duplicate paths and the durable verification
	// queue. A refusal leaves the copied file on disk honestly unregistered.
	out, e := s.QueueDirectImport(ctx, DirectImportRequest{
		Kind:       in.Kind,
		Collection: in.Collection,
		MediaID:    in.MediaID,
		Episodes:   in.Episodes,
		Root:       directImportContainingRoot(roots, dir),
		Relative:   relative,
		Reviewed:   true,
	})
	if e != nil {
		return out, problem("registration_refused", "The file was copied to "+relative+" but registration was refused: "+e.Error())
	}
	return out, nil
}

// copyUpload streams the request body into the staging file, stopping at the
// size bound or when the owner's browser disconnects.
func copyUpload(ctx context.Context, file *os.File, src io.Reader) (int64, error) {
	buf := make([]byte, 256<<10)
	var written int64
	for {
		if e := ctx.Err(); e != nil {
			return written, e
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			if written+int64(n) > MaxDirectUploadBytes {
				return written, errors.New("the uploaded file is too large")
			}
			if _, writeErr := file.Write(buf[:n]); writeErr != nil {
				return written, writeErr
			}
			written += int64(n)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				if syncErr := file.Sync(); syncErr != nil {
					return written, syncErr
				}
				return written, nil
			}
			return written, readErr
		}
	}
}

// directUploadName accepts a plain file name only: no path segments, no dot
// prefix, no control characters, and it must be a complete supported video.
func directUploadName(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !utf8.ValidString(raw) || len(raw) > maxDirectUploadName {
		return "", problem("invalid_file", "Choose a video file with a usable name.")
	}
	if strings.ContainsAny(raw, `/\`) || strings.HasPrefix(raw, ".") || strings.ContainsFunc(raw, unicode.IsControl) {
		return "", problem("invalid_file", "Choose a video file with a simple name.")
	}
	if !video(raw) || extraVideo.MatchString(raw) {
		return "", problem("invalid_file", "Choose a complete, supported video file. Samples and trailers are not imported.")
	}
	return raw, nil
}

// directUploadTarget confirms the selected catalog title exists and matches
// the import kind before any folder is created or byte is copied.
func (s *Service) directUploadTarget(ctx context.Context, kind string, mediaID int64) error {
	var targetKind string
	e := s.db.QueryRowContext(ctx, `SELECT kind FROM media_items WHERE id=?`, mediaID).Scan(&targetKind)
	if errors.Is(e, sql.ErrNoRows) {
		return problem("invalid_mapping", "Choose a catalog title for this file.")
	}
	if e != nil {
		return e
	}
	if targetKind != kind {
		return problem("invalid_mapping", "Choose a catalog title of the same type as this import.")
	}
	return nil
}

// directUploadDir resolves the destination folder: the folder of the title's
// first registered file when one exists, otherwise the naming-template title
// folder (created when missing), otherwise the first configured root. Every
// outcome must live inside a configured root so registration can proceed.
func (s *Service) directUploadDir(ctx context.Context, mediaID int64, roots []string) (string, error) {
	var relative string
	e := s.db.QueryRowContext(ctx, `SELECT relative_path FROM media_files WHERE COALESCE(movie_id,series_id)=? ORDER BY id LIMIT 1`, mediaID).Scan(&relative)
	switch {
	case e == nil:
		if _, e = safeRelative(relative); e != nil {
			return "", e
		}
		dir := path.Dir(strings.ReplaceAll(relative, "\\", "/"))
		if directImportContainingRoot(roots, dir) == "" {
			return "", problem("unsafe_path", "The title's existing files live outside the configured roots.")
		}
		return dir, nil
	case errors.Is(e, sql.ErrNoRows):
		abs, createErr := organization.New(s.db, s.cfg).CreateTitleFolder(ctx, mediaID)
		if createErr != nil && mediaBusy(createErr) {
			return "", createErr
		}
		if createErr == nil {
			if dir, ok := s.mediaRelative(abs); ok && directImportContainingRoot(roots, dir) != "" {
				return dir, nil
			}
		}
		if len(roots) == 0 {
			return "", problem("storage_unavailable", "This library has no configured roots.")
		}
		return roots[0], nil
	default:
		return "", e
	}
}

func mediaBusy(err error) bool {
	var p *organization.Error
	if errors.As(err, &p) {
		return p.Code == "organization_busy" || p.Code == "subtitle_busy"
	}
	return false
}

// mediaRelative converts an absolute path inside the media root into a
// media-root relative slash path.
func (s *Service) mediaRelative(abs string) (string, bool) {
	resolved, e := filepath.EvalSymlinks(s.cfg.MediaDir)
	if e != nil {
		return "", false
	}
	rel, e := filepath.Rel(filepath.Clean(resolved), filepath.Clean(abs))
	if e != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}
