package transfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/faaaaq/ReelScarey/internal/organization"
)

// Duplicate resolution is bounded so one very uniform library cannot return an
// unbounded review page. The overview reports when more groups remain.
const maxDuplicateGroups = 100

type DuplicateFile struct {
	ID       string     `json:"id"`
	Path     string     `json:"path"`
	FullPath string     `json:"full_path"`
	Hint     string     `json:"hint"`
	Size     int64      `json:"size_bytes"`
	Message  string     `json:"message"`
	Episodes []TVTarget `json:"episodes"`
}

type DuplicateGroup struct {
	Key         string          `json:"key"`
	SeriesTitle string          `json:"series_title"`
	Episode     TVTarget        `json:"episode"`
	Files       []DuplicateFile `json:"files"`
}

type DuplicateGroupPage struct {
	Items     []DuplicateGroup `json:"items"`
	Truncated bool             `json:"truncated"`
}

type DuplicateResolution struct {
	Key        string `json:"key"`
	KeepFileID string `json:"keep_file_id"`
	Reviewed   bool   `json:"reviewed"`
}

func duplicateEpisodeID(key string) (int64, bool) {
	raw, found := strings.CutPrefix(key, "episode:")
	if !found || raw == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, false
	}
	return id, true
}

func automaticDuplicateContains(keys []string, key string) bool {
	for _, saved := range keys {
		if saved == key {
			return true
		}
	}
	return false
}

// DuplicateGroups lists the automatic candidates that share one canonical
// episode. Candidates stay in review until one file is chosen explicitly;
// nothing here queues or registers media.
func (s *Service) DuplicateGroups(ctx context.Context, scanID string) (DuplicateGroupPage, error) {
	out := DuplicateGroupPage{Items: []DuplicateGroup{}}
	scan, err := s.LibraryScan(ctx, scanID)
	if err != nil {
		return out, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.file_id,c.payload,c.message,f.relative_path,f.size_bytes FROM library_match_candidates c JOIN library_scan_files f ON f.id=c.file_id WHERE c.scan_id=? AND c.state='review' AND f.state='review' AND c.payload<>'{}' ORDER BY f.relative_path`, scanID)
	if err != nil {
		return out, err
	}
	groups := map[string][]DuplicateFile{}
	for rows.Next() {
		var id, raw, message, relative string
		var size int64
		if err = rows.Scan(&id, &raw, &message, &relative, &size); err != nil {
			rows.Close()
			return out, err
		}
		var candidate automaticCandidate
		if json.Unmarshal([]byte(raw), &candidate) != nil || len(candidate.DuplicateKeys) == 0 {
			continue
		}
		file := DuplicateFile{
			ID:       id,
			Path:     relative,
			FullPath: filepath.Join(scan.MediaRoot, filepath.FromSlash(relative)),
			Hint:     discoveryHint(relative, scan.Kind),
			Size:     size,
			Message:  message,
			Episodes: candidate.Snapshot.EpisodeSnapshot,
		}
		if file.Episodes == nil {
			file.Episodes = []TVTarget{}
		}
		for _, key := range candidate.DuplicateKeys {
			if _, found := duplicateEpisodeID(key); !found {
				continue
			}
			groups[key] = append(groups[key], file)
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()
	keys := make([]string, 0, len(groups))
	for key, files := range groups {
		if len(files) < 2 {
			continue
		}
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		first, _ := duplicateEpisodeID(keys[i])
		second, _ := duplicateEpisodeID(keys[j])
		return first < second
	})
	if len(keys) > maxDuplicateGroups {
		keys = keys[:maxDuplicateGroups]
		out.Truncated = true
	}
	for _, key := range keys {
		group, err := s.duplicateGroup(ctx, key, groups[key])
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, group)
	}
	sort.SliceStable(out.Items, func(i, j int) bool {
		if out.Items[i].Episode.Season != out.Items[j].Episode.Season {
			return out.Items[i].Episode.Season < out.Items[j].Episode.Season
		}
		return out.Items[i].Episode.Number < out.Items[j].Episode.Number
	})
	return out, nil
}

func (s *Service) duplicateGroup(ctx context.Context, key string, files []DuplicateFile) (DuplicateGroup, error) {
	id, _ := duplicateEpisodeID(key)
	group := DuplicateGroup{Key: key, Files: files}
	err := s.db.QueryRowContext(ctx, `SELECT e.id,e.tmdb_id,s.number,e.number,e.title,m.title FROM episodes e JOIN seasons s ON s.id=e.season_id JOIN media_items m ON m.id=s.series_id WHERE e.id=?`, id).Scan(&group.Episode.EpisodeID, &group.Episode.TMDbID, &group.Episode.Season, &group.Episode.Number, &group.Episode.Title, &group.SeriesTitle)
	return group, err
}

// ResolveDuplicate records an explicit owner choice for one duplicate episode
// group. The chosen file continues through ordinary verification/registration;
// the other variants remain unregistered and are marked skipped. A variant that
// claims an episode outside the chosen file's target set is refused, because it
// may be the only source for that episode.
func (s *Service) ResolveDuplicate(ctx context.Context, scanID string, in DuplicateResolution) (int, error) {
	defer organization.ReadMedia()()
	episodeID, ok := duplicateEpisodeID(in.Key)
	if !ok || !in.Reviewed || len(in.KeepFileID) != 32 {
		return 0, problem("review_required", "Choose one file to register for this episode and confirm the review.")
	}
	if e := organization.EnsureMediaAvailable(ctx, s.db); e != nil {
		return 0, e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	if e = organization.EnsureMediaAvailableTx(ctx, tx); e != nil {
		return 0, e
	}
	if _, e := scanLibraryScan(tx.QueryRowContext(ctx, `SELECT `+libraryScanColumns+` FROM library_scans WHERE id=?`, scanID)); e != nil {
		return 0, e
	}
	var active int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM library_match_jobs WHERE scan_id=? AND (state IN ('pending','running','waiting') OR lease_until>=unixepoch())`, scanID).Scan(&active); e != nil {
		return 0, e
	}
	if active > 0 {
		return 0, problem("scan_busy", "Wait for automatic matching to finish before resolving duplicate variants.")
	}
	var raw string
	e = tx.QueryRowContext(ctx, `SELECT c.payload FROM library_match_candidates c JOIN library_scan_files f ON f.id=c.file_id WHERE c.file_id=? AND c.scan_id=? AND c.state='review' AND f.state='review'`, in.KeepFileID, scanID).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return 0, problem("scan_busy", "This duplicate review changed. Reload the scan.")
	}
	if e != nil {
		return 0, e
	}
	var keep automaticCandidate
	if e = json.Unmarshal([]byte(raw), &keep); e != nil || keep.Snapshot.Choice.FileID != in.KeepFileID {
		return 0, mappingError()
	}
	if !automaticDuplicateContains(keep.DuplicateKeys, in.Key) {
		return 0, problem("review_required", "The selected file no longer belongs to this duplicate group. Reload the scan.")
	}
	keepTargets := map[int64]bool{}
	for _, id := range keep.Snapshot.Choice.Episodes {
		keepTargets[id] = true
	}
	if !keepTargets[episodeID] {
		return 0, mappingError()
	}
	type variant struct {
		ID        string
		Candidate automaticCandidate
	}
	variants := []string{}
	rows, e := tx.QueryContext(ctx, `SELECT c.file_id,c.payload FROM library_match_candidates c JOIN library_scan_files f ON f.id=c.file_id WHERE c.scan_id=? AND c.state='review' AND f.state='review' AND c.payload<>'{}'`, scanID)
	if e != nil {
		return 0, e
	}
	for rows.Next() {
		var id, body string
		if e = rows.Scan(&id, &body); e != nil {
			rows.Close()
			return 0, e
		}
		if id == in.KeepFileID {
			continue
		}
		var candidate automaticCandidate
		if json.Unmarshal([]byte(body), &candidate) != nil || !automaticDuplicateContains(candidate.DuplicateKeys, in.Key) {
			continue
		}
		for _, target := range candidate.Snapshot.Choice.Episodes {
			if !keepTargets[target] {
				rows.Close()
				return 0, problem("invalid_mapping", "Another duplicate variant covers an episode the selected file does not. Review those files separately.")
			}
		}
		variants = append(variants, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return 0, e
	}
	if len(variants) == 0 {
		return 0, problem("scan_busy", "This duplicate review changed. Reload the scan.")
	}
	var keepPath, episodeTitle string
	var season, number int
	if e = tx.QueryRowContext(ctx, `SELECT relative_path FROM library_scan_files WHERE id=? AND scan_id=?`, in.KeepFileID, scanID).Scan(&keepPath); e != nil {
		return 0, e
	}
	if e = tx.QueryRowContext(ctx, `SELECT s.number,e.number,e.title FROM episodes e JOIN seasons s ON s.id=e.season_id WHERE e.id=?`, episodeID).Scan(&season, &number, &episodeTitle); e != nil {
		return 0, e
	}
	message := fmt.Sprintf("Skipped duplicate variant. %s was chosen to register S%02dE%02d %s.", path.Base(keepPath), season, number, strings.TrimSpace(episodeTitle))
	for _, id := range variants {
		res, err := tx.ExecContext(ctx, `UPDATE library_scan_files SET state='skipped',message=?,updated_at=unixepoch() WHERE id=? AND scan_id=? AND state='review' AND lease_until<unixepoch()`, strings.TrimSpace(message), id, scanID)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return 0, problem("scan_busy", "This duplicate review changed. Reload the scan.")
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM library_match_candidates WHERE file_id=?`, id); err != nil {
			return 0, err
		}
	}
	if e = s.queueExistingRegistrationTx(ctx, tx, in.KeepFileID, ExistingRegistration{MediaID: keep.Snapshot.Choice.MediaID, Episodes: keep.Snapshot.Choice.Episodes}, false); e != nil {
		return 0, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE library_match_candidates SET state='queued',message='Duplicate variant resolved; waiting for file verification.' WHERE file_id=?`, in.KeepFileID); e != nil {
		return 0, e
	}
	if e = tx.Commit(); e != nil {
		return 0, e
	}
	return len(variants), nil
}
