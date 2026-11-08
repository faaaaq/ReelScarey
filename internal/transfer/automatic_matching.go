package transfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/instance"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/organization"
)

type AutomaticMetadata interface {
	Search(context.Context, string, int) (metadata.Results, error)
	Movie(context.Context, int64) (metadata.Movie, error)
	SearchSeries(context.Context, string, int) (metadata.SeriesResults, error)
	Series(context.Context, int64) (metadata.Series, error)
	Season(context.Context, int64, int) (metadata.Season, error)
}

// SetAutomaticMetadata is configured once, before starting the transfer workers.
func (s *Service) SetAutomaticMetadata(provider AutomaticMetadata) { s.automaticMetadata = provider }

type AutomaticMatchStatus struct {
	State    string `json:"state"`
	Phase    string `json:"phase"`
	Message  string `json:"message"`
	Resolved int    `json:"resolved"`
	Queued   int    `json:"queued"`
	Review   int    `json:"review"`
}
type automaticCandidate struct {
	Hint     automaticHint `json:"hint"`
	Snapshot reviewedMatch `json:"snapshot"`
	// Note records accepted cross-season/number evidence for the review UI.
	Note string `json:"note,omitempty"`
	// DuplicateKeys marks the canonical targets this file shares with other
	// resolved files. Such candidates stay in review until the owner chooses
	// one file for every group.
	DuplicateKeys []string `json:"duplicate_keys,omitempty"`
}

func (s *Service) AutomaticMatchStatus(ctx context.Context, scan string) (*AutomaticMatchStatus, error) {
	var out AutomaticMatchStatus
	err := s.db.QueryRowContext(ctx, `SELECT state,phase,message FROM library_match_jobs WHERE scan_id=?`, scan).Scan(&out.State, &out.Phase, &out.Message)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(state='queued'),0),COALESCE(SUM(state='review'),0) FROM library_match_candidates WHERE scan_id=?`, scan).Scan(&out.Resolved, &out.Queued, &out.Review)
	return &out, err
}
func (s *Service) StartAutomaticMatching(ctx context.Context, scanID string) error {
	defer organization.ReadMedia()()
	scan, err := s.LibraryScan(ctx, scanID)
	if err != nil {
		return err
	}
	if scan.State != "completed" {
		return problem("scan_busy", "Finish discovery before automatically matching this scan.")
	}
	root, err := s.existingRoot(ctx, &scan)
	if err != nil {
		return err
	}
	root.Close()
	category := "standard"
	prefs, err := organization.New(s.db, s.cfg).Get(ctx)
	if err != nil {
		return err
	}
	for _, collection := range prefs.Libraries {
		if collection.Key != "anime" || scan.Kind != "series" {
			continue
		}
		for _, root := range scan.Roots {
			if root == collection.Folder || root == collection.Folder+"/TV Shows" {
				category = "anime"
			}
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = organization.EnsureMediaAvailableTx(ctx, tx); err != nil {
		return err
	}
	if instance.RecoveryPending(s.cfg.ConfigDir) {
		return problem("recovery_pending", "Review recovery before matching files.")
	}
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM library_match_jobs WHERE scan_id=? AND (state IN ('pending','running','waiting') OR lease_until>=unixepoch())`, scanID).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return problem("scan_busy", "Automatic matching is already active.")
	}
	// Resume an interrupted run: ready and queued matches are already verified
	// snapshots and must not be thrown away. Only unresolved reviews are retried,
	// and the title cache is retained to avoid repeating provider searches.
	if _, err = tx.ExecContext(ctx, `DELETE FROM library_match_candidates WHERE scan_id=? AND state='review'`, scanID); err != nil {
		return err
	}
	// Only an explicit administrator POST reaches this method. Existing scans do
	// not acquire automatic authorization merely by opening or reading a page.
	if _, err = tx.ExecContext(ctx, `INSERT INTO library_match_jobs(scan_id,state,category,created_at,updated_at) VALUES(?,'pending',?,unixepoch(),unixepoch()) ON CONFLICT(scan_id) DO UPDATE SET state='pending',phase='resolve',category=excluded.category,message='Matching requested.',attempts=0,retry_at=0,lease_owner='',lease_until=0,updated_at=unixepoch()`, scanID, category); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) CancelAutomaticMatching(ctx context.Context, scan string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE library_match_jobs SET state='cancelled',message='Automatic matching stopped. Files already queued for verification continue.',lease_owner='',lease_until=0,updated_at=unixepoch() WHERE scan_id=? AND state IN ('pending','running','waiting','paused')`, scan)
	return err
}
func (s *Service) automaticGuard(ctx context.Context, tx *sql.Tx, scan, owner string) error {
	if instance.RecoveryPending(s.cfg.ConfigDir) {
		return ErrLease
	}
	if err := organization.EnsureMediaAvailableTx(ctx, tx); err != nil {
		return err
	}
	var n int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM library_match_jobs j JOIN library_scans s ON s.id=j.scan_id WHERE j.scan_id=? AND j.state='running' AND j.lease_owner=? AND j.lease_until>=unixepoch() AND s.state='completed'`, scan, owner).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrLease
	}
	return nil
}
func (s *Service) automaticTx(ctx context.Context, scan, owner string, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.automaticGuard(ctx, tx, scan, owner); err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) ProcessAutomaticMatchNext(ctx context.Context) (bool, error) {
	defer organization.ReadMedia()()
	if instance.RecoveryPending(s.cfg.ConfigDir) {
		return false, nil
	}
	if err := organization.EnsureMediaAvailable(ctx, s.db); err != nil {
		return false, err
	}
	// Interrupted discovery cannot leave a matching job permanently active.
	if _, err := s.db.ExecContext(ctx, `UPDATE library_match_jobs SET state='paused',lease_owner='',lease_until=0,message='Discovery did not complete. Start a new scan before automatic matching.',updated_at=unixepoch() WHERE state IN ('pending','running','waiting') AND EXISTS(SELECT 1 FROM library_scans s WHERE s.id=scan_id AND s.state IN ('failed','cancelled'))`); err != nil {
		return false, err
	}
	owner := ID()
	var scanID, phase string
	err := s.db.QueryRowContext(ctx, `UPDATE library_match_jobs SET state='running',lease_owner=?,lease_until=unixepoch()+60,updated_at=unixepoch() WHERE scan_id=(SELECT j.scan_id FROM library_match_jobs j JOIN library_scans s ON s.id=j.scan_id WHERE s.state='completed' AND j.state IN ('pending','running','waiting') AND j.lease_until<unixepoch() AND j.retry_at<=unixepoch() ORDER BY j.created_at,j.scan_id LIMIT 1) RETURNING scan_id,phase`, owner).Scan(&scanID, &phase)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	work, cancel := context.WithTimeout(ctx, 50*time.Second)
	if phase == "resolve" {
		err = s.resolveAutomaticNext(work, scanID, owner)
	} else {
		err = s.queueAutomaticNext(work, scanID, owner)
	}
	cancel()
	finish, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if err != nil && ctx.Err() == nil && !errors.Is(err, ErrLease) {
		reason, delay := automaticRetryReason(err)
		_, save := s.db.ExecContext(finish, `UPDATE library_match_jobs SET attempts=attempts+1,state=CASE WHEN attempts>=4 THEN 'paused' ELSE 'waiting' END,retry_at=unixepoch()+?,message=CASE WHEN attempts>=4 THEN ? ELSE ? END WHERE scan_id=? AND lease_owner=? AND state='running'`, delay, reason+" Matching paused after repeated failures. Start matching again after resolving the issue.", reason+" Matching will retry automatically.", scanID, owner)
		if save != nil {
			return true, save
		}
	}
	_, save := s.db.ExecContext(finish, `UPDATE library_match_jobs SET lease_owner='',lease_until=0,updated_at=unixepoch() WHERE scan_id=? AND lease_owner=?`, scanID, owner)
	return true, save
}

// Report only known error categories so internal details never reach scan status.
func automaticRetryReason(err error) (string, int) {
	var provider *metadata.Error
	if errors.As(err, &provider) {
		switch provider.Code {
		case "tmdb_not_configured":
			return "TMDb is not connected. Connect it in Settings.", 60
		case "tmdb_unauthorized":
			return "TMDb rejected the saved token. Replace and test it in Settings.", 60
		case "tmdb_rate_limited":
			return "TMDb is limiting requests.", max(60, min(300, provider.RetryAfter))
		case "tmdb_unavailable":
			return "TMDb could not be reached. Test the saved connection in Settings.", 60
		case "tmdb_invalid_response":
			return "TMDb returned an unreadable response.", 60
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "A matching step timed out.", 60
	}
	return "Automatic matching could not finish the current step.", 60
}

func (s *Service) saveAutomaticReview(ctx context.Context, scan, owner, file, message string) error {
	return s.automaticTx(ctx, scan, owner, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO library_match_candidates(file_id,scan_id,state,message) VALUES(?,?,'review',?) ON CONFLICT(file_id) DO UPDATE SET state='review',message=excluded.message`, file, scan, message); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE library_scan_files SET message=? WHERE id=? AND scan_id=? AND state='review'`, message, file, scan); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE library_match_jobs SET attempts=0,retry_at=0,message='Matching titles and episodes.' WHERE scan_id=?`, scan)
		return err
	})
}
func automaticTargetKeys(v automaticCandidate) []string {
	if v.Snapshot.Kind == "movie" {
		return []string{"movie-file:" + strings.ToLower(v.Snapshot.File.Path)}
	}
	out := []string{}
	for _, id := range v.Snapshot.Choice.Episodes {
		out = append(out, fmt.Sprintf("episode:%d", id))
	}
	return out
}
func (s *Service) finishAutomaticResolution(ctx context.Context, scan, owner string) error {
	return s.automaticTx(ctx, scan, owner, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT file_id,payload FROM library_match_candidates WHERE scan_id=? AND state='ready'`, scan)
		if err != nil {
			return err
		}
		targets := map[string][]string{}
		payloads := map[string]string{}
		for rows.Next() {
			var id, raw string
			var v automaticCandidate
			if err = rows.Scan(&id, &raw); err != nil {
				rows.Close()
				return err
			}
			if err = json.Unmarshal([]byte(raw), &v); err != nil {
				rows.Close()
				return err
			}
			payloads[id] = raw
			for _, key := range automaticTargetKeys(v) {
				targets[key] = append(targets[key], id)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		// Resolve duplicates across the entire completed scan before queuing any file.
		duplicates := map[string][]string{}
		for key, files := range targets {
			if len(files) < 2 {
				continue
			}
			for _, id := range files {
				duplicates[id] = append(duplicates[id], key)
			}
		}
		for id, keys := range duplicates {
			sort.Strings(keys)
			message := "Multiple files map to the same episode. Choose one file to register for this episode and skip the duplicate variants."
			raw := payloads[id]
			var candidate automaticCandidate
			if json.Unmarshal([]byte(raw), &candidate) == nil {
				candidate.DuplicateKeys = keys
				if encoded, err := json.Marshal(candidate); err == nil {
					raw = string(encoded)
				}
			}
			if _, err = tx.ExecContext(ctx, `UPDATE library_match_candidates SET state='review',payload=?,message=? WHERE file_id=?`, raw, message, id); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE library_scan_files SET message=? WHERE id=? AND state='review'`, message, id); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE library_match_jobs SET phase='queue',message='Registering clear matches; uncertain matches stay in review.',attempts=0,retry_at=0 WHERE scan_id=?`, scan)
		return err
	})
}
func (s *Service) queueAutomaticNext(ctx context.Context, scanID, owner string) error {
	var file, raw string
	err := s.db.QueryRowContext(ctx, `SELECT file_id,payload FROM library_match_candidates WHERE scan_id=? AND state='ready' ORDER BY file_id LIMIT 1`, scanID).Scan(&file, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return s.automaticTx(ctx, scanID, owner, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `UPDATE library_match_jobs SET state='completed',message='Automatic matching finished. Clear matches are queued for verification; unresolved files need review.',attempts=0,retry_at=0 WHERE scan_id=?`, scanID)
			return err
		})
	}
	if err != nil {
		return err
	}
	var candidate automaticCandidate
	if err = json.Unmarshal([]byte(raw), &candidate); err != nil {
		return err
	}
	err = s.registerAutomaticCandidate(ctx, scanID, owner, file, candidate)
	if err == nil || errors.Is(err, ErrLease) || ctx.Err() != nil {
		return err
	}
	return s.saveAutomaticReview(ctx, scanID, owner, file, "This match changed or conflicts with another import. Review it before registration.")
}

// Workers only need the saved scan scope, not UI progress counts.
func (s *Service) automaticScan(ctx context.Context, id string) (LibraryScan, error) {
	return scanLibraryScan(s.db.QueryRowContext(ctx, `SELECT `+libraryScanColumns+` FROM library_scans WHERE id=?`, id))
}

// This is the only automatic entry into the shared registration implementation.
// It requires an explicit matching job, a live lease, exact current identity,
// a whole-scan duplicate check, unchanged file evidence and current episode IDs.
// The public/manual registration entry retains its separate reviewed requirement.
func (s *Service) registerAutomaticCandidate(ctx context.Context, scanID, owner, file string, expected automaticCandidate) error {
	scan, err := s.automaticScan(ctx, scanID)
	if err != nil {
		return err
	}
	root, err := s.existingRoot(ctx, &scan)
	if err != nil {
		return err
	}
	defer root.Close()
	return s.automaticTx(ctx, scanID, owner, func(tx *sql.Tx) error {
		var phase, state, raw string
		if err := tx.QueryRowContext(ctx, `SELECT j.phase,c.state,c.payload FROM library_match_jobs j JOIN library_match_candidates c ON c.scan_id=j.scan_id WHERE j.scan_id=? AND c.file_id=?`, scanID, file).Scan(&phase, &state, &raw); err != nil {
			return err
		}
		if phase != "queue" || state != "ready" {
			return ErrLease
		}
		var saved automaticCandidate
		if err := json.Unmarshal([]byte(raw), &saved); err != nil {
			return err
		}
		if !reflect.DeepEqual(saved, expected) || saved.Snapshot.Choice.FileID != file {
			return ErrLease
		}
		current, err := s.matchSnapshot(ctx, tx, scan, []BulkMatchChoice{saved.Snapshot.Choice}, root)
		if err != nil {
			return err
		}
		now := current.Items[0]
		old := saved.Snapshot
		freshHint, err := automaticIdentity(scan, now.File)
		if err != nil {
			return err
		}
		year := now.Year
		if !reflect.DeepEqual(freshHint, saved.Hint) || !freshHint.matches(now.Title, &year, now.ProviderID) || now.Kind != old.Kind || now.ProviderID != old.ProviderID || now.CategoryRevision != old.CategoryRevision || !reflect.DeepEqual(now.EpisodeSnapshot, old.EpisodeSnapshot) {
			return problem("scan_busy", "The automatic identity changed; review this match.")
		}
		if err = s.queueExistingRegistrationTx(ctx, tx, file, ExistingRegistration{MediaID: now.Choice.MediaID, Episodes: now.Choice.Episodes}, false); err != nil {
			return err
		}
		message := "Automatically matched using exact title and episode evidence."
		scanMessage := "Automatically matched; waiting for file verification."
		if saved.Note != "" {
			message = "Automatically matched. " + saved.Note
			scanMessage = message + " Waiting for file verification."
		}
		if _, err = tx.ExecContext(ctx, `UPDATE library_match_candidates SET state='queued',message=? WHERE file_id=?`, message, file); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE library_scan_files SET message=? WHERE id=?`, scanMessage, file)
		return err
	})
}

// Metadata resolution and queueing must not wait behind disk inspection. There
// remains just one disk worker, avoiding seek contention on a library drive.
func (s *Service) runAutomaticMatching(ctx context.Context, logger *slog.Logger) {
	for ctx.Err() == nil {
		processed, err := s.ProcessAutomaticMatchNext(ctx)
		if err != nil && ctx.Err() == nil {
			logger.Error("automatic library matching step failed")
		}
		if processed && err == nil {
			continue
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
