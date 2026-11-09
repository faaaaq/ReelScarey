// Package nfoexport writes opt-in Jellyfin-compatible NFO sidecars beside
// registered movie and episode media files.
//
// The worker only reads library metadata. It never changes media bytes,
// probe_json, analysis columns or library rows, and it refuses to replace an
// existing sidecar. All paths are anchored to MediaRoot so a registered path
// cannot escape the library or follow a symbolic link.
package nfoexport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/faaaaq/ReelScarey/internal/nfo"
	"github.com/faaaaq/ReelScarey/internal/organization"
)

const (
	// DefaultInterval is used when Worker.Interval is zero or negative.
	DefaultInterval = 5 * time.Minute
	// maxCandidatesPerPass bounds how many registered rows one ProcessOne call
	// inspects while looking for a file that still needs a sidecar.
	maxCandidatesPerPass = 256
	// maxEpisodeMappings bounds the work for one shared media file.
	maxEpisodeMappings = 100
)

var (
	// ErrConfig reports a worker without a database or media root.
	ErrConfig = errors.New("NFO export is not configured")
	// ErrMediaRoot reports an unavailable media folder.
	ErrMediaRoot = errors.New("the media folder is not available")
	// ErrMissingMedia reports a registered file that is no longer present.
	ErrMissingMedia = errors.New("the registered media file is missing from the media folder")
	// ErrUnsafePath reports a path that could leave the media root or follow a
	// symbolic link or special file.
	ErrUnsafePath = errors.New("the registered media path is not safe for NFO export")
	// ErrAmbiguousEpisode reports a shared media file that maps to several
	// episodes without one exact filename label.
	ErrAmbiguousEpisode = errors.New("the shared media file maps to multiple episodes without one exact label")
	// ErrNotEligible is internal: a candidate stopped being eligible between the
	// selection query and its metadata read.
	errNotEligible = errors.New("the registered media file is no longer eligible for NFO export")
)

// Worker writes sidecar NFO files for registered media below MediaRoot.
type Worker struct {
	DB        *sql.DB
	MediaRoot string
	Interval  time.Duration

	mu        sync.Mutex
	attempted map[string]struct{}
	lastID    int64
}

type candidate struct {
	id       int64
	relative string
	movieID  int64
}

type episodeRecord struct {
	id       int64
	title    string
	tmdbID   int64
	overview string
	airDate  string
	season   int
	number   int
	year     int
}

// ProcessOne writes at most one missing sidecar for an eligible registered
// media file. NFO export must be enabled in settings; when it is disabled the
// method returns (false, nil) without inspecting any media file.
//
// It returns true when a sidecar was written or a selected file raised an
// error. An existing sidecar is never replaced and does not count as work.
func (w *Worker) ProcessOne(ctx context.Context) (bool, error) {
	if w == nil || w.DB == nil || strings.TrimSpace(w.MediaRoot) == "" {
		return false, ErrConfig
	}
	// Sidecar writes share the media gate with organization and import so a
	// file cannot be moved while its NFO is being published.
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

	for checked := 0; checked < maxCandidatesPerPass; checked++ {
		item, ok, err := w.nextCandidate(ctx)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
		mediaRelative, sidecarRelative, err := sidecarPath(item.relative)
		if err != nil {
			w.markAttempted("unsafe\x00" + item.relative)
			return true, err
		}
		if w.attemptedAlready(sidecarRelative) {
			continue
		}
		exists, err := inspectDestination(root, sidecarRelative)
		if err != nil {
			w.markAttempted(sidecarRelative)
			return true, err
		}
		if exists {
			// An existing NFO, including one created after the previous tick,
			// is final. Never replace it with another episode's details.
			w.markAttempted(sidecarRelative)
			continue
		}
		if err = verifyMedia(root, mediaRelative); err != nil {
			w.markAttempted(sidecarRelative)
			return true, err
		}
		document, err := w.render(ctx, item)
		if err != nil {
			w.markAttempted(sidecarRelative)
			if errors.Is(err, errNotEligible) {
				continue
			}
			return true, err
		}
		absolute := filepath.Join(w.MediaRoot, filepath.FromSlash(sidecarRelative))
		if err = nfo.WriteFileAtomic(absolute, document, 0o644); err != nil {
			w.markAttempted(sidecarRelative)
			if errors.Is(err, nfo.ErrExists) {
				// Another process won the race. The file was not replaced.
				continue
			}
			return true, err
		}
		w.markAttempted(sidecarRelative)
		return true, nil
	}
	return false, nil
}

// Run processes eligible files until ctx is cancelled. It drains ready work
// immediately and then waits Interval between passes. Only warnings are logged.
func (w *Worker) Run(ctx context.Context, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	interval := w.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	for {
		worked, err := w.ProcessOne(ctx)
		if err != nil && ctx.Err() == nil {
			logger.Warn("NFO export deferred", "error", err)
		}
		if ctx.Err() != nil {
			return
		}
		if worked && err == nil {
			continue
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (w *Worker) enabled(ctx context.Context) (bool, error) {
	var enabled int
	err := w.DB.QueryRowContext(ctx, `SELECT nfo_export FROM settings WHERE id=1`).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("%w: the settings row is missing", ErrConfig)
	}
	if err != nil {
		return false, err
	}
	return enabled == 1, nil
}

// nextCandidate advances an in-process cursor. A restarted worker rescans from
// the beginning; correctness never depends on this cursor because the sidecar
// existence check is repeated before every write.
func (w *Worker) nextCandidate(ctx context.Context) (candidate, bool, error) {
	w.mu.Lock()
	last := w.lastID
	w.mu.Unlock()

	var item candidate
	err := w.DB.QueryRowContext(ctx, `
		SELECT id,relative_path,COALESCE(movie_id,0)
		FROM media_files
		WHERE id>? AND relative_path<>''
		  AND (movie_id IS NOT NULL OR EXISTS(SELECT 1 FROM episode_files ef WHERE ef.media_file_id=media_files.id))
		ORDER BY id LIMIT 1`, last).
		Scan(&item.id, &item.relative, &item.movieID)
	if errors.Is(err, sql.ErrNoRows) {
		return candidate{}, false, nil
	}
	if err != nil {
		return candidate{}, false, err
	}
	w.mu.Lock()
	if item.id > w.lastID {
		w.lastID = item.id
	}
	w.mu.Unlock()
	return item, true, nil
}

func (w *Worker) render(ctx context.Context, item candidate) ([]byte, error) {
	if item.movieID > 0 {
		return w.renderMovie(ctx, item.movieID)
	}
	return w.renderEpisode(ctx, item)
}

func (w *Worker) renderMovie(ctx context.Context, movieID int64) ([]byte, error) {
	var title, overview, releaseDate string
	var year sql.NullInt64
	var tmdbID int64
	err := w.DB.QueryRowContext(ctx, `
		SELECT title,year,tmdb_id,overview,release_date
		FROM media_items WHERE id=? AND kind='movie'`, movieID).
		Scan(&title, &year, &tmdbID, &overview, &releaseDate)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotEligible
	}
	if err != nil {
		return nil, err
	}
	return nfo.RenderMovie(nfo.Movie{
		Title:     title,
		Year:      usableYear(year),
		TMDbID:    tmdbID,
		Plot:      overview,
		Premiered: validDate(releaseDate),
	})
}

func (w *Worker) renderEpisode(ctx context.Context, item candidate) ([]byte, error) {
	mappings, err := w.episodeMappings(ctx, item.id)
	if err != nil {
		return nil, err
	}
	if len(mappings) == 0 {
		return nil, errNotEligible
	}
	selected := mappings[0]
	if len(mappings) > 1 {
		selected, err = labeledEpisode(item.relative, mappings)
		if err != nil {
			return nil, err
		}
	}
	return nfo.RenderEpisode(nfo.Episode{
		Title:     selected.title,
		Year:      selected.year,
		TMDbID:    selected.tmdbID,
		Plot:      selected.overview,
		Premiered: validDate(selected.airDate),
		Season:    selected.season,
		Episode:   selected.number,
	})
}

func (w *Worker) episodeMappings(ctx context.Context, mediaFileID int64) ([]episodeRecord, error) {
	rows, err := w.DB.QueryContext(ctx, `
		SELECT e.id,e.title,e.tmdb_id,e.overview,e.air_date,s.number,e.number,COALESCE(m.year,0)
		FROM episode_files ef
		JOIN episodes e ON e.id=ef.episode_id
		JOIN seasons s ON s.id=e.season_id
		JOIN media_items m ON m.id=s.series_id
		WHERE ef.media_file_id=?
		ORDER BY s.number,e.number,e.id
		LIMIT ?`, mediaFileID, maxEpisodeMappings+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []episodeRecord
	for rows.Next() {
		var episode episodeRecord
		if err = rows.Scan(&episode.id, &episode.title, &episode.tmdbID, &episode.overview, &episode.airDate, &episode.season, &episode.number, &episode.year); err != nil {
			return nil, err
		}
		out = append(out, episode)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(out) > maxEpisodeMappings {
		return nil, ErrAmbiguousEpisode
	}
	return out, nil
}

// labeledEpisode picks the one mapping named by an exact SxxExx or NxNN label.
// Shared files with no label, several labels or a range stay ambiguous.
func labeledEpisode(relative string, mappings []episodeRecord) (episodeRecord, error) {
	base := path.Base(relative)
	labels := episodeLabels(base)
	if len(labels) != 1 || multiEpisodePattern.MatchString(base) {
		return episodeRecord{}, ErrAmbiguousEpisode
	}
	label := labels[0]
	if label.season <= 0 && label.episode <= 0 {
		return episodeRecord{}, ErrAmbiguousEpisode
	}
	var match *episodeRecord
	for index := range mappings {
		episode := &mappings[index]
		if label.season > 0 && label.season != episode.season {
			continue
		}
		if label.episode != episode.number {
			continue
		}
		if match != nil {
			return episodeRecord{}, ErrAmbiguousEpisode
		}
		match = episode
	}
	if match == nil {
		return episodeRecord{}, ErrAmbiguousEpisode
	}
	return *match, nil
}

type episodeLabel struct {
	season  int
	episode int
}

var (
	// S01E02, S01.E02, S01_E02 and E02 when no season is present.
	episodeLabelPattern = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:s([0-9]{1,3})[ ._-]?)?e([0-9]{1,4})(?:[^0-9]|$)`)
	// 1x02.
	crossLabelPattern = regexp.MustCompile(`(?i)(?:^|[^0-9])([0-9]{1,3})x([0-9]{1,4})(?:[^0-9]|$)`)
	// S01E01-E02, E01-E02, S01E01E02 and similar ranges.
	multiEpisodePattern = regexp.MustCompile(`(?i)(?:e[0-9]{1,4}[ ._-]*(?:-|to|&|,)[ ._-]*e?[0-9]{1,4}|e[0-9]{1,4}e[0-9]{1,4})`)
)

func episodeLabels(base string) []episodeLabel {
	var out []episodeLabel
	for _, match := range episodeLabelPattern.FindAllStringSubmatch(base, -1) {
		label := episodeLabel{}
		if match[1] != "" {
			label.season = parseSmallInt(match[1])
		}
		label.episode = parseSmallInt(match[2])
		out = append(out, label)
	}
	for _, match := range crossLabelPattern.FindAllStringSubmatch(base, -1) {
		out = append(out, episodeLabel{season: parseSmallInt(match[1]), episode: parseSmallInt(match[2])})
	}
	return out
}

func parseSmallInt(raw string) int {
	value := 0
	for _, digit := range raw {
		value = value*10 + int(digit-'0')
		if value > 9999 {
			return 0
		}
	}
	return value
}

// sidecarPath validates a registered slash-separated path and returns the media
// path and the sibling .nfo path, both relative to MediaRoot.
func sidecarPath(relative string) (string, string, error) {
	if relative == "" || strings.ContainsAny(relative, "\\:\x00\r\n") {
		return "", "", ErrUnsafePath
	}
	clean := path.Clean(relative)
	if clean != relative || clean == "." || !fs.ValidPath(clean) {
		return "", "", ErrUnsafePath
	}
	base := path.Base(clean)
	extension := path.Ext(base)
	stem := strings.TrimSuffix(base, extension)
	if stem == "" || stem == "." || stem == ".." {
		return "", "", ErrUnsafePath
	}
	sidecar := path.Join(path.Dir(clean), stem+".nfo")
	if sidecar == clean || !fs.ValidPath(sidecar) {
		return "", "", ErrUnsafePath
	}
	return clean, sidecar, nil
}

// inspectDestination reports whether a regular NFO already exists. Symbolic
// links and special files are rejected instead of followed or replaced.
func inspectDestination(root *os.Root, relative string) (bool, error) {
	info, err := root.Lstat(filepath.FromSlash(relative))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, ErrUnsafePath
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, ErrUnsafePath
	}
	return true, nil
}

// verifyMedia checks every path component through the opened root. Missing
// components are reported separately from unsafe links and special files.
func verifyMedia(root *os.Root, relative string) error {
	parts := strings.Split(relative, string(filepath.Separator))
	for index := range parts {
		info, err := root.Lstat(filepath.Join(parts[:index+1]...))
		if errors.Is(err, fs.ErrNotExist) {
			return ErrMissingMedia
		}
		if err != nil {
			return ErrUnsafePath
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return ErrUnsafePath
		}
		last := index == len(parts)-1
		if last {
			if !info.Mode().IsRegular() {
				return ErrUnsafePath
			}
			continue
		}
		if !info.IsDir() {
			return ErrUnsafePath
		}
	}
	return nil
}

func (w *Worker) attemptedAlready(key string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, ok := w.attempted[key]
	return ok
}

func (w *Worker) markAttempted(key string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.attempted == nil {
		w.attempted = make(map[string]struct{})
	}
	w.attempted[key] = struct{}{}
}

func usableYear(year sql.NullInt64) int {
	if !year.Valid || year.Int64 < 1870 || year.Int64 > 2200 {
		return 0
	}
	return int(year.Int64)
}

func validDate(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := time.Parse(time.DateOnly, raw)
	if err != nil || parsed.Format(time.DateOnly) != raw {
		return ""
	}
	return raw
}
