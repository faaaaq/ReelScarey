// Package artworkexport writes an opt-in movie poster.jpg beside registered
// movie files. It only reads catalog metadata, never changes media bytes or
// catalog rows, and never replaces an existing image.
package artworkexport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/organization"
)

const (
	DefaultInterval = 5 * time.Minute
	maxImageBytes   = 8 << 20
	maxPerPass      = 64
)

var (
	ErrConfig    = errors.New("Artwork export is not configured")
	ErrMediaRoot = errors.New("the media folder is not available")
)

type Worker struct {
	DB        *sql.DB
	MediaRoot string
	Interval  time.Duration

	mu        sync.Mutex
	attempted map[int64]struct{}
	cursor    int64
	client    *http.Client
}

type candidate struct {
	fileID   int64
	relative string
	poster   string
}

func (w *Worker) Run(ctx context.Context, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	interval := w.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	for {
		for ctx.Err() == nil {
			worked, err := w.ProcessOne(ctx)
			if err != nil && ctx.Err() == nil {
				logger.Warn("artwork export deferred", "error", err)
			}
			if !worked || err != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func (w *Worker) ProcessOne(ctx context.Context) (bool, error) {
	if w == nil || w.DB == nil || strings.TrimSpace(w.MediaRoot) == "" {
		return false, ErrConfig
	}
	defer organization.ReadMedia()()
	enabled, err := w.enabled(ctx)
	if err != nil {
		return false, err
	}
	if !enabled {
		return false, nil
	}
	root, err := os.OpenRoot(w.MediaRoot)
	if err != nil {
		return false, ErrMediaRoot
	}
	defer root.Close()
	if w.client == nil {
		w.client = &http.Client{
			Timeout:   20 * time.Second,
			Transport: &http.Transport{Proxy: nil},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("artwork redirects are disabled")
			},
		}
	}
	for checked := 0; checked < maxPerPass; checked++ {
		item, ok, err := w.nextCandidate(ctx)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
		if w.alreadyAttempted(item.fileID) {
			continue
		}
		posterRelative := path.Join(path.Dir(item.relative), "poster.jpg")
		if _, err := root.Stat(posterRelative); err == nil {
			w.markAttempted(item.fileID)
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			w.markAttempted(item.fileID)
			continue
		}
		data, err := w.download(ctx, item.poster)
		if err != nil {
			// Retry a transient provider failure on the next pass instead of
			// permanently skipping this file.
			w.mu.Lock()
			if item.fileID > 0 {
				w.cursor = item.fileID - 1
			}
			w.mu.Unlock()
			return true, err
		}
		file, err := root.OpenFile(posterRelative, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			w.markAttempted(item.fileID)
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return true, fmt.Errorf("create poster sidecar: %w", err)
		}
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			_ = root.Remove(posterRelative)
			w.markAttempted(item.fileID)
			return true, fmt.Errorf("write poster sidecar: %w", errors.Join(writeErr, closeErr))
		}
		w.markAttempted(item.fileID)
		return true, nil
	}
	return false, nil
}

func (w *Worker) download(ctx context.Context, posterPath string) ([]byte, error) {
	url := metadata.PosterURL(posterPath)
	if url == "" {
		return nil, errors.New("the saved poster path is unsafe")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := w.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("poster provider returned HTTP %d", response.StatusCode)
	}
	if !strings.HasPrefix(response.Header.Get("Content-Type"), "image/") {
		return nil, errors.New("the poster provider returned a non-image response")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > maxImageBytes {
		return nil, errors.New("the poster response is empty or too large")
	}
	return data, nil
}

func (w *Worker) enabled(ctx context.Context) (bool, error) {
	var enabled int
	err := w.DB.QueryRowContext(ctx, `SELECT artwork_export FROM settings WHERE id=1`).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("%w: the settings row is missing", ErrConfig)
	}
	return enabled == 1, err
}

func (w *Worker) nextCandidate(ctx context.Context) (candidate, bool, error) {
	w.mu.Lock()
	last := w.cursor
	w.mu.Unlock()
	var item candidate
	err := w.DB.QueryRowContext(ctx, `SELECT f.id,f.relative_path,m.poster_path
		FROM media_files f JOIN media_items m ON m.id=f.movie_id
		WHERE f.movie_id IS NOT NULL AND m.poster_path<>'' AND f.id>?
		ORDER BY f.id LIMIT 1`, last).Scan(&item.fileID, &item.relative, &item.poster)
	if errors.Is(err, sql.ErrNoRows) {
		return candidate{}, false, nil
	}
	if err != nil {
		return candidate{}, false, err
	}
	w.mu.Lock()
	w.cursor = item.fileID
	w.mu.Unlock()
	return item, true, nil
}

func (w *Worker) alreadyAttempted(id int64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.attempted == nil {
		w.attempted = map[int64]struct{}{}
	}
	_, ok := w.attempted[id]
	return ok
}

func (w *Worker) markAttempted(id int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.attempted == nil {
		w.attempted = map[int64]struct{}{}
	}
	w.attempted[id] = struct{}{}
}

// Ensure the media root is a directory without following an external link.
func ValidateRoot(mediaRoot string) bool {
	info, err := os.Stat(filepath.Clean(mediaRoot))
	return err == nil && info.IsDir()
}
