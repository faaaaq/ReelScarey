package nfoexport

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/faaaaq/ReelScarey/internal/nfo"
	"github.com/faaaaq/ReelScarey/internal/organization"
)

const (
	defaultSortTitleBatchSize = 128
	maxSortTitleDocumentBytes = 4 << 20
)

var (
	// ErrSortTitleCursor reports an invalid resumable batch position.
	ErrSortTitleCursor = errors.New("the sort-title cleanup cursor must not be negative")
	// ErrSidecarChanged reports an NFO that changed while it was being read.
	ErrSidecarChanged = errors.New("the episode NFO changed during sort-title cleanup")
)

// SortTitleIssue identifies one registered sidecar that could not be cleaned.
// Paths are library-relative, never host-absolute paths.
type SortTitleIssue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// SortTitleReport is one bounded, resumable cleanup batch. Missing means no
// sibling NFO exists; AlreadyBlank includes documents without a sorttitle.
type SortTitleReport struct {
	Scanned      int              `json:"scanned"`
	Changed      int              `json:"changed"`
	AlreadyBlank int              `json:"already_blank"`
	Missing      int              `json:"missing"`
	Failed       int              `json:"failed"`
	Issues       []SortTitleIssue `json:"issues"`
	NextAfterID  int64            `json:"next_after_id"`
	Done         bool             `json:"done"`
}

// CleanupOne clears at most one non-empty episode sort title. Unlike NFO
// generation this always runs, including when nfo_export is disabled, so both
// downloaded and manually registered TV/Anime files get the same treatment.
func (w *Worker) CleanupOne(ctx context.Context) (bool, error) {
	if w == nil || w.DB == nil || strings.TrimSpace(w.MediaRoot) == "" {
		return false, ErrConfig
	}
	w.cleanupMu.Lock()
	defer w.cleanupMu.Unlock()
	defer organization.WriteMedia()()
	if err := organization.EnsureMediaAvailable(ctx, w.DB); err != nil {
		return false, err
	}
	root, err := os.OpenRoot(w.MediaRoot)
	if err != nil {
		return false, ErrMediaRoot
	}
	defer root.Close()

	for checked := 0; checked < maxCandidatesPerPass; checked++ {
		item, ok, err := w.nextEpisodeCandidate(ctx)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
		mediaRelative, sidecarRelative, err := sidecarPath(item.relative)
		if err != nil {
			return true, err
		}
		exists, err := inspectDestination(root, sidecarRelative)
		if err != nil {
			return true, err
		}
		if !exists {
			continue
		}
		changed, err := clearEpisodeSidecar(ctx, root, mediaRelative, sidecarRelative)
		if err != nil {
			return true, err
		}
		if changed {
			return true, nil
		}
	}
	// A full page made progress even when every sidecar was already blank or
	// missing. Drain the next page immediately rather than delaying new imports
	// behind one Interval per page; the next call performs the end-of-list read.
	return true, nil
}

// nextEpisodeCandidate advances the automatic cleanup cursor. New imports and
// registrations receive new media_files IDs, so they are visited without a
// library scan or a filesystem walk.
func (w *Worker) nextEpisodeCandidate(ctx context.Context) (candidate, bool, error) {
	var item candidate
	err := w.DB.QueryRowContext(ctx, `
		SELECT id,relative_path,0
		FROM media_files
		WHERE id>? AND relative_path<>''
		  AND EXISTS(SELECT 1 FROM episode_files ef WHERE ef.media_file_id=media_files.id)
		ORDER BY id LIMIT 1`, w.cleanupLastID).Scan(&item.id, &item.relative, &item.movieID)
	if errors.Is(err, sql.ErrNoRows) {
		return candidate{}, false, nil
	}
	if err != nil {
		return candidate{}, false, err
	}
	w.cleanupLastID = item.id
	return item, true, nil
}

// ClearSortTitles processes one bounded batch of all registered episode media
// in stable ID order. The caller resumes with NextAfterID until Done is true.
// Individual malformed or unsafe NFOs are reported and do not stop other files.
func (w *Worker) ClearSortTitles(ctx context.Context, afterID int64) (SortTitleReport, error) {
	report := SortTitleReport{Issues: []SortTitleIssue{}, NextAfterID: afterID}
	if w == nil || w.DB == nil || strings.TrimSpace(w.MediaRoot) == "" {
		return report, ErrConfig
	}
	if afterID < 0 {
		return report, ErrSortTitleCursor
	}
	limit := w.batchLimit
	if limit <= 0 {
		limit = defaultSortTitleBatchSize
	}
	w.cleanupMu.Lock()
	defer w.cleanupMu.Unlock()
	defer organization.WriteMedia()()
	if err := organization.EnsureMediaAvailable(ctx, w.DB); err != nil {
		return report, err
	}
	root, err := os.OpenRoot(w.MediaRoot)
	if err != nil {
		return report, ErrMediaRoot
	}
	defer root.Close()

	rows, err := w.DB.QueryContext(ctx, `
		SELECT id,relative_path
		FROM media_files
		WHERE id>? AND relative_path<>''
		  AND EXISTS(SELECT 1 FROM episode_files ef WHERE ef.media_file_id=media_files.id)
		ORDER BY id LIMIT ?`, afterID, limit+1)
	if err != nil {
		return report, err
	}
	items := []candidate{}
	for rows.Next() {
		var item candidate
		if err = rows.Scan(&item.id, &item.relative); err != nil {
			rows.Close()
			return report, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return report, err
	}
	if err = rows.Close(); err != nil {
		return report, err
	}
	report.Done = len(items) <= limit
	if len(items) > limit {
		items = items[:limit]
	}
	for _, item := range items {
		if err = ctx.Err(); err != nil {
			return report, err
		}
		report.Scanned++
		report.NextAfterID = item.id
		mediaRelative, sidecarRelative, pathErr := sidecarPath(item.relative)
		if pathErr != nil {
			report.addIssue(item.relative, pathErr)
			continue
		}
		exists, inspectErr := inspectDestination(root, sidecarRelative)
		if inspectErr != nil {
			report.addIssue(sidecarRelative, inspectErr)
			continue
		}
		if !exists {
			report.Missing++
			continue
		}
		changed, clearErr := clearEpisodeSidecar(ctx, root, mediaRelative, sidecarRelative)
		if clearErr != nil {
			report.addIssue(sidecarRelative, clearErr)
			continue
		}
		if changed {
			report.Changed++
		} else {
			report.AlreadyBlank++
		}
	}
	return report, nil
}

func (r *SortTitleReport) addIssue(path string, err error) {
	r.Failed++
	r.Issues = append(r.Issues, SortTitleIssue{Path: path, Message: err.Error()})
}

// clearEpisodeSidecar verifies the registered media and sidecar before making
// a same-directory atomic replacement. Only the sorttitle spans change.
func clearEpisodeSidecar(ctx context.Context, root *os.Root, mediaRelative, sidecarRelative string) (bool, error) {
	if err := verifyMedia(root, mediaRelative); err != nil {
		return false, err
	}
	expected, err := root.Lstat(filepath.FromSlash(sidecarRelative))
	if err != nil {
		return false, err
	}
	if expected.Mode()&os.ModeSymlink != 0 || !expected.Mode().IsRegular() {
		return false, ErrUnsafePath
	}
	if expected.Size() > maxSortTitleDocumentBytes {
		return false, fmt.Errorf("%w: episode NFO exceeds %d bytes", ErrInvalidSortTitleDocument, maxSortTitleDocumentBytes)
	}
	file, err := root.Open(filepath.FromSlash(sidecarRelative))
	if err != nil {
		return false, err
	}
	opened, statErr := file.Stat()
	if statErr != nil || !os.SameFile(expected, opened) || !opened.Mode().IsRegular() {
		file.Close()
		return false, ErrSidecarChanged
	}
	document, readErr := io.ReadAll(io.LimitReader(file, maxSortTitleDocumentBytes+1))
	closeErr := file.Close()
	if readErr != nil {
		return false, readErr
	}
	if closeErr != nil {
		return false, closeErr
	}
	if len(document) > maxSortTitleDocumentBytes {
		return false, fmt.Errorf("%w: episode NFO exceeds %d bytes", ErrInvalidSortTitleDocument, maxSortTitleDocumentBytes)
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	updated, changed, err := nfo.ClearEpisodeSortTitle(document)
	if err != nil || !changed {
		return false, err
	}
	if err = replaceEpisodeSidecar(root, sidecarRelative, updated, expected); err != nil {
		return false, err
	}
	return true, nil
}

// ErrInvalidSortTitleDocument reports a sidecar too large to inspect safely.
var ErrInvalidSortTitleDocument = errors.New("episode NFO is not safe for sort-title cleanup")

func replaceEpisodeSidecar(root *os.Root, relative string, document []byte, expected os.FileInfo) error {
	directory, err := root.OpenRoot(filepath.FromSlash(path.Dir(relative)))
	if err != nil {
		return err
	}
	defer directory.Close()
	base := path.Base(relative)
	current, err := directory.Lstat(base)
	if err != nil || !sameSidecar(expected, current) {
		return ErrSidecarChanged
	}
	random := make([]byte, 12)
	if _, err = rand.Read(random); err != nil {
		return err
	}
	stage := ".reelscarey-sorttitle-" + hex.EncodeToString(random) + ".tmp"
	file, err := directory.OpenFile(stage, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	stageExists := true
	defer func() {
		if stageExists {
			_ = directory.Remove(stage)
		}
	}()
	closeWithError := func(err error) error {
		if closeErr := file.Close(); err == nil {
			return closeErr
		}
		return err
	}
	if err = file.Chmod(expected.Mode().Perm()); err != nil {
		return closeWithError(err)
	}
	if _, err = file.Write(document); err != nil {
		return closeWithError(err)
	}
	if err = file.Sync(); err != nil {
		return closeWithError(err)
	}
	if err = file.Close(); err != nil {
		return err
	}
	// Do not replace a sidecar that was changed or swapped while staging.
	current, err = directory.Lstat(base)
	if err != nil || !sameSidecar(expected, current) {
		return ErrSidecarChanged
	}
	if err = directory.Rename(stage, base); err != nil {
		return err
	}
	stageExists = false
	if runtime.GOOS != "windows" {
		parent, openErr := directory.Open(".")
		if openErr == nil {
			syncErr := parent.Sync()
			closeErr := parent.Close()
			if syncErr != nil {
				return syncErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}
	return nil
}

func sameSidecar(expected, current os.FileInfo) bool {
	return current != nil && current.Mode().IsRegular() && os.SameFile(expected, current) &&
		current.Size() == expected.Size() && current.ModTime().Equal(expected.ModTime())
}
