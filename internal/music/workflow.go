package music

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

const (
	// workflowLeaseSeconds bounds one worker claim. The worker renews the lease
	// after every bounded batch so a crashed or restarted process can resume.
	workflowLeaseSeconds = 300
	// workflowClaimInterval is how often the durable worker looks for work.
	workflowClaimInterval = time.Second
	// workflowFileBatch bounds one catalog-match/provider batch.
	workflowFileBatch = 400
	// workflowFingerprintBatch bounds one AcoustID pass before the lease is
	// renewed and progress is saved.
	workflowFingerprintBatch = 40
	// workflowFinalizeBatch bounds one registration or edit-plan batch.
	workflowFinalizeBatch = 50
	// workflowEditPoll is how often the finalize worker checks its sequential
	// edit job before starting the next one.
	workflowEditPoll = 150 * time.Millisecond
)

var errWorkLeaseLost = errors.New("The music workflow lease was lost. The work will resume from the saved state.")

// RequestMatchAll records the owner's reviewed consent to match every
// unresolved file against tags/MusicBrainz and then AcoustID. It never
// registers, moves or edits a file; the durable worker does the work.
func (s *Service) RequestMatchAll(ctx context.Context, scanID int64, reviewed bool) (Scan, error) {
	if scanID < 1 || !reviewed {
		return Scan{}, ErrInvalidInput
	}
	scan, err := s.Scan(ctx, scanID)
	if err != nil {
		return Scan{}, err
	}
	if err := workflowRequestable(scan); err != nil {
		return Scan{}, err
	}
	unresolved, err := s.unresolvedCount(ctx, scanID)
	if err != nil {
		return Scan{}, err
	}
	if unresolved == 0 {
		return Scan{}, &Error{Code: "music_nothing_to_do", Message: "Every file in this scan is already matched or registered."}
	}
	result, err := s.store.db.ExecContext(ctx, `UPDATE music_scans
		SET request='match',phase='queued',progress_total=?,progress_done=0,current_path='',message=?,error='',requested_at=unixepoch(),work_owner='',work_until=0,updated_at=unixepoch()
		WHERE id=? AND request='' AND state NOT IN ('scanning','cancelled')`,
		unresolved, "Waiting for the matching worker. It matches saved catalog entries and the files' own tags first, then uses audio fingerprints and MusicBrainz only for the rest.", scanID)
	if err != nil {
		return Scan{}, err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return Scan{}, &Error{Code: "music_scan_state", Message: "This scan already has a pending or running workflow."}
	}
	return s.Scan(ctx, scanID)
}

// RequestFinalize records reviewed consent to register the scan's matched files
// in place and then organize/tag them through the reviewed edit plan. It never
// starts while a scan or another workflow request is running.
func (s *Service) RequestFinalize(ctx context.Context, scanID int64, reviewed bool) (Scan, error) {
	if scanID < 1 || !reviewed {
		return Scan{}, ErrInvalidInput
	}
	scan, err := s.Scan(ctx, scanID)
	if err != nil {
		return Scan{}, err
	}
	if err := workflowRequestable(scan); err != nil {
		return Scan{}, err
	}
	matched, err := s.countScanFiles(ctx, scanID, "matched")
	if err != nil {
		return Scan{}, err
	}
	if matched == 0 {
		return Scan{}, &Error{Code: "music_nothing_to_do", Message: "This scan has no matched files to register. Match the unresolved files first."}
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return Scan{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE music_scan_files SET finalize_requested=0 WHERE scan_id=?`, scanID); err != nil {
		return Scan{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE music_scan_files SET finalize_requested=1 WHERE scan_id=? AND state='matched'`, scanID); err != nil {
		return Scan{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE music_scans
		SET request='finalize',phase='queued',progress_total=?,progress_done=0,current_path='',message=?,error='',requested_at=unixepoch(),work_owner='',work_until=0,updated_at=unixepoch()
		WHERE id=? AND request='' AND state NOT IN ('scanning','cancelled')`,
		matched, "Waiting for the finalize worker. It registers the matched files in place, then organizes and tags them.", scanID)
	if err != nil {
		return Scan{}, err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return Scan{}, &Error{Code: "music_scan_state", Message: "This scan already has a pending or running workflow."}
	}
	if err := tx.Commit(); err != nil {
		return Scan{}, err
	}
	return s.Scan(ctx, scanID)
}

func workflowRequestable(scan Scan) error {
	if scan.State == "scanning" || scan.Phase == "scanning" {
		return &Error{Code: "music_scan_state", Message: "Wait for the scan to finish before matching or finalizing it."}
	}
	if scan.State == "cancelled" || scan.Phase == "cancelled" {
		return &Error{Code: "music_scan_state", Message: "This scan was cancelled. Start a new scan."}
	}
	if scan.Request != "" || scan.Phase == "queued" || scan.Phase == "matching" ||
		scan.Phase == "fingerprinting" || scan.Phase == "organizing" || scan.WorkUntil > time.Now().Unix() {
		return &Error{Code: "music_scan_state", Message: "This scan already has a pending or running workflow."}
	}
	return nil
}

// RunWorkflow is the durable single-worker loop. It claims one queued scan at a
// time from the database and never relies on an in-memory queue.
func (s *Service) RunWorkflow(ctx context.Context, logger *slog.Logger) {
	ticker := time.NewTicker(workflowClaimInterval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		worked, err := s.ProcessWorkflow(ctx)
		if err != nil && ctx.Err() == nil {
			logger.Error("music workflow worker failed", "error", err)
		}
		if worked && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ProcessWorkflow claims and runs at most one saved request.
func (s *Service) ProcessWorkflow(ctx context.Context) (bool, error) {
	scan, owner, claimed, err := s.claimWorkflow(ctx)
	if err != nil || !claimed {
		return false, err
	}
	work, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	switch scan.Request {
	case "match":
		return true, s.runMatchWorkflow(work, scan, owner)
	case "finalize":
		return true, s.runFinalizeWorkflow(work, scan, owner)
	default:
		return true, s.finishWorkflow(context.WithoutCancel(ctx), scan.ID, owner, "failed", "failed", "The saved workflow request is not recognized.", "The saved workflow request is not recognized.")
	}
}

func (s *Service) claimWorkflow(ctx context.Context) (Scan, string, bool, error) {
	var id int64
	err := s.store.db.QueryRowContext(ctx, `SELECT id FROM music_scans
		WHERE request<>'' AND state IN ('review','completed','failed') AND work_until<=unixepoch()
		ORDER BY requested_at,id LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Scan{}, "", false, nil
	}
	if err != nil {
		return Scan{}, "", false, err
	}
	owner := workflowOwner()
	result, err := s.store.db.ExecContext(ctx, `UPDATE music_scans SET work_owner=?,work_until=unixepoch()+?,updated_at=unixepoch()
		WHERE id=? AND request<>'' AND work_until<=unixepoch()`, owner, workflowLeaseSeconds, id)
	if err != nil {
		return Scan{}, "", false, err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return Scan{}, "", false, nil
	}
	scan, err := s.Scan(ctx, id)
	if err != nil {
		return Scan{}, "", true, err
	}
	return scan, owner, true, nil
}

func workflowOwner() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buffer)
}

func (s *Service) renewWorkflow(ctx context.Context, id int64, owner string) error {
	result, err := s.store.db.ExecContext(ctx, `UPDATE music_scans SET work_until=unixepoch()+?,updated_at=unixepoch() WHERE id=? AND work_owner=?`,
		workflowLeaseSeconds, id, owner)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return errWorkLeaseLost
	}
	return nil
}

func (s *Service) startWorkflowPhase(ctx context.Context, id int64, owner, phase string, total int, message string) error {
	result, err := s.store.db.ExecContext(ctx, `UPDATE music_scans
		SET phase=?,progress_total=?,progress_done=0,current_path='',message=?,work_until=unixepoch()+?,updated_at=unixepoch()
		WHERE id=? AND work_owner=?`, phase, total, message, workflowLeaseSeconds, id, owner)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return errWorkLeaseLost
	}
	return nil
}

// progressWorkflow records the live path/message and renews the lease. The
// done count is derived from the scan's remaining unresolved rows, so the bar
// never claims work that was not actually saved.
func (s *Service) progressWorkflow(ctx context.Context, id int64, owner, phase, currentPath, message string) error {
	result, err := s.store.db.ExecContext(ctx, `UPDATE music_scans
		SET phase=?,current_path=?,message=?,work_until=unixepoch()+?,updated_at=unixepoch(),
			progress_done=MAX(0,progress_total-(SELECT COUNT(*) FROM music_scan_files WHERE scan_id=? AND state='unresolved'))
		WHERE id=? AND work_owner=?`, phase, currentPath, message, workflowLeaseSeconds, id, id, owner)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return errWorkLeaseLost
	}
	return nil
}

func (s *Service) setWorkflowCounts(ctx context.Context, id int64, owner, phase string, total, done int, currentPath, message string) error {
	result, err := s.store.db.ExecContext(ctx, `UPDATE music_scans
		SET phase=?,progress_total=?,progress_done=?,current_path=?,message=?,work_until=unixepoch()+?,updated_at=unixepoch()
		WHERE id=? AND work_owner=?`, phase, total, done, currentPath, message, workflowLeaseSeconds, id, owner)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return errWorkLeaseLost
	}
	return nil
}

func (s *Service) finishWorkflow(ctx context.Context, id int64, owner, state, phase, message, errText string) error {
	_, _ = s.refreshScanCounts(ctx, id)
	result, err := s.store.db.ExecContext(ctx, `UPDATE music_scans
		SET state=?,phase=?,request='',work_owner='',work_until=0,current_path='',message=?,error=?,updated_at=unixepoch()
		WHERE id=? AND work_owner=?`, state, phase, message, errText, id, owner)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		var current string
		if err := s.store.db.QueryRowContext(ctx, `SELECT state FROM music_scans WHERE id=?`, id).Scan(&current); err == nil && current == "cancelled" {
			return nil
		}
		return errWorkLeaseLost
	}
	return nil
}

func (s *Service) requeueWorkflow(ctx context.Context, id int64, owner, message string) error {
	result, err := s.store.db.ExecContext(ctx, `UPDATE music_scans
		SET phase='queued',work_owner='',work_until=0,current_path='',message=?,updated_at=unixepoch()
		WHERE id=? AND work_owner=?`, message, id, owner)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return errWorkLeaseLost
	}
	return nil
}

func (s *Service) workflowCancelled(ctx context.Context, id int64) (bool, error) {
	var state string
	if err := s.store.db.QueryRowContext(ctx, `SELECT state FROM music_scans WHERE id=?`, id).Scan(&state); err != nil {
		return false, err
	}
	return state == "cancelled", nil
}

func (s *Service) runMatchWorkflow(ctx context.Context, scan Scan, owner string) error {
	id := scan.ID
	base := context.WithoutCancel(ctx)
	total, err := s.unresolvedCount(base, id)
	if err != nil {
		return err
	}
	if err := s.startWorkflowPhase(base, id, owner, "matching", total, "Matching saved catalog entries and the files' own tags first."); err != nil {
		if errors.Is(err, errWorkLeaseLost) {
			return nil
		}
		return err
	}
	catalog := newLocalCatalog(s)
	localMatched, warnings, err := s.localTagMatchFiles(ctx, id, owner, catalog)
	if err != nil {
		if isRateLimited(err) {
			warnings = append(warnings, matchErrorMessage(err))
			unresolved, countErr := s.unresolvedCount(base, id)
			if countErr != nil {
				return s.handleWorkflowError(ctx, id, owner, countErr, localMatched)
			}
			message := fmt.Sprintf("Matched %d files before MusicBrainz paused matching. %d files remain unresolved for review. %s", localMatched, unresolved, dedupeWarnings(warnings))
			return s.finishWorkflow(base, id, owner, "review", "ready", strings.TrimSpace(message), "")
		}
		return s.handleWorkflowError(ctx, id, owner, err, localMatched)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return s.handleWorkflowError(ctx, id, owner, ctxErr, localMatched)
	}
	if cancelled, err := s.workflowCancelled(base, id); err != nil {
		return err
	} else if cancelled {
		return nil
	}
	fingerprinted := 0
	unresolved, err := s.unresolvedCount(base, id)
	if err != nil {
		return err
	}
	if unresolved > 0 {
		key, warning := s.fingerprintAvailability(ctx)
		if warning != "" {
			warnings = append(warnings, warning)
		} else {
			if err := s.startWorkflowPhase(base, id, owner, "fingerprinting", unresolved, "Fingerprinting the remaining unresolved files through AcoustID."); err != nil {
				if errors.Is(err, errWorkLeaseLost) {
					return nil
				}
				return err
			}
			var matchedNow int
			var fpWarnings []string
			matchedNow, fpWarnings, err = s.fingerprintAllFiles(ctx, id, owner, key, catalog)
			fingerprinted += matchedNow
			warnings = append(warnings, fpWarnings...)
			if err != nil {
				// A provider/rate-limit error is a warning: matched rows are
				// preserved and the rest stay unresolved for review. A lease,
				// context or database error still uses the normal handling.
				var provider *Error
				if errors.As(err, &provider) {
					warnings = append(warnings, matchErrorMessage(err))
				} else {
					return s.handleWorkflowError(ctx, id, owner, err, localMatched+fingerprinted)
				}
			}
		}
	}
	unresolved, err = s.unresolvedCount(base, id)
	if err != nil {
		return err
	}
	// MusicBrainz discovery is the last step and only sees files whose tags
	// were not complete enough for a local identity. A well-tagged library
	// therefore never waits on the provider.
	discovered := 0
	if unresolved > 0 {
		if err := s.startWorkflowPhase(base, id, owner, "matching", unresolved, "Discovering MusicBrainz catalog entries for the remaining files."); err != nil {
			if errors.Is(err, errWorkLeaseLost) {
				return nil
			}
			return err
		}
		var discoverWarnings []string
		discovered, discoverWarnings, err = s.discoverAllFiles(ctx, id, owner)
		warnings = append(warnings, discoverWarnings...)
		if err != nil {
			var provider *Error
			if errors.As(err, &provider) {
				warnings = append(warnings, matchErrorMessage(err))
			} else {
				return s.handleWorkflowError(ctx, id, owner, err, localMatched+fingerprinted+discovered)
			}
		}
	}
	unresolved, err = s.unresolvedCount(base, id)
	if err != nil {
		return err
	}
	message := fmt.Sprintf("Matched %d files from the saved catalog and their own tags without MusicBrainz or AcoustID calls; matched %d more by audio fingerprint; discovered %d through MusicBrainz. %d files remain unresolved for review.",
		localMatched, fingerprinted, discovered, unresolved)
	if warningText := dedupeWarnings(warnings); warningText != "" {
		message += " " + warningText
	}
	return s.finishWorkflow(base, id, owner, "review", "ready", message, "")
}

func (s *Service) handleWorkflowError(ctx context.Context, id int64, owner string, err error, progress int) error {
	if errors.Is(err, errWorkLeaseLost) {
		return nil
	}
	base := context.WithoutCancel(ctx)
	if cancelled, cancelErr := s.workflowCancelled(base, id); cancelErr == nil && cancelled {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		if progress > 0 {
			return s.requeueWorkflow(base, id, owner, "The workflow continues with the remaining files.")
		}
		return s.finishWorkflow(base, id, owner, "review", "ready", "Matching stopped before it could process more files. The remaining files stay unresolved for review.", "")
	}
	return s.finishWorkflow(base, id, owner, "failed", "failed", "", err.Error())
}

// localTagMatchFiles is the fast path. For each bounded batch it first matches
// saved catalog rows, then creates the artist/album/track rows the files' own
// tags prove. Neither step calls MusicBrainz or AcoustID.
func (s *Service) localTagMatchFiles(ctx context.Context, scanID int64, owner string, catalog *localCatalog) (int, []string, error) {
	warnings := []string{}
	matched := 0
	cursor := ""
	for {
		if err := s.renewWorkflow(ctx, scanID, owner); err != nil {
			return matched, warnings, err
		}
		files, err := s.unresolvedScanFilesAfter(ctx, scanID, cursor, workflowFileBatch)
		if err != nil {
			return matched, warnings, err
		}
		if len(files) == 0 {
			return matched, warnings, nil
		}
		if cancelled, err := s.workflowCancelled(ctx, scanID); err != nil {
			return matched, warnings, err
		} else if cancelled {
			return matched, warnings, nil
		}
		next := files[len(files)-1].RelativePath
		remaining := make([]ScanFile, 0, len(files))
		for _, file := range files {
			artistID, albumID, trackID, ok, matchErr := catalog.findExisting(ctx, file)
			if matchErr != nil {
				return matched, warnings, matchErr
			}
			if !ok {
				remaining = append(remaining, file)
				if isRateLimited(matchErr) {
					warnings = append(warnings, matchErrorMessage(matchErr))
					return matched, warnings, matchErr
				}
				continue
			}
			if err := s.updateScanFileState(ctx, file.ID, "matched", artistID, albumID, trackID, ""); err != nil {
				return matched, warnings, err
			}
			matched++
			if err := s.progressWorkflow(ctx, scanID, owner, "matching", file.RelativePath, "Matching saved catalog entries."); err != nil {
				return matched, warnings, err
			}
		}
		for _, file := range remaining {
			ev := tagIdentityFor(file)
			if !ev.canCreate() {
				// The file stays unresolved for the fingerprint and provider
				// fallbacks; it has no complete local tag identity.
				continue
			}
			artistID, albumID, trackID, matchErr := catalog.matchIdentity(ctx, file, ev)
			if matchErr != nil {
				var tagged *Error
				if !errors.As(matchErr, &tagged) {
					return matched, warnings, matchErr
				}
				if err := s.updateScanFileState(ctx, file.ID, "unresolved", artistID, albumID, 0, matchErrorMessage(matchErr)); err != nil {
					return matched, warnings, err
				}
				continue
			}
			if err := s.updateScanFileState(ctx, file.ID, "matched", artistID, albumID, trackID, ""); err != nil {
				return matched, warnings, err
			}
			matched++
			if err := s.progressWorkflow(ctx, scanID, owner, "matching", file.RelativePath, "Creating catalog entries from tags."); err != nil {
				return matched, warnings, err
			}
		}
		cursor = next
	}
}

// discoverAllFiles is the last-resort provider step. It only runs for files
// whose tags were not complete enough for a local identity, so a well-tagged
// library never reaches it. Files are processed in bounded batches and each
// provider result is saved as it completes.
func (s *Service) discoverAllFiles(ctx context.Context, scanID int64, owner string) (int, []string, error) {
	warnings := []string{}
	matched := 0
	cursor := ""
	total, err := s.unresolvedCount(ctx, scanID)
	if err != nil {
		return matched, warnings, err
	}
	processed := 0
	for {
		if err := s.renewWorkflow(ctx, scanID, owner); err != nil {
			return matched, warnings, err
		}
		files, err := s.unresolvedScanFilesAfter(ctx, scanID, cursor, workflowFileBatch)
		if err != nil {
			return matched, warnings, err
		}
		if len(files) == 0 {
			return matched, warnings, nil
		}
		if cancelled, err := s.workflowCancelled(ctx, scanID); err != nil {
			return matched, warnings, err
		} else if cancelled {
			return matched, warnings, nil
		}
		next := files[len(files)-1].RelativePath
		candidates := make([]ScanFile, 0, len(files))
		for _, file := range files {
			if tagIdentityFor(file).canCreate() {
				// A complete tag identity was already handled locally. It is
				// left for manual review if the tags conflicted; provider
				// discovery must not create a second catalog album for it.
				processed++
				continue
			}
			candidates = append(candidates, file)
		}
		if len(candidates) > 0 {
			result, stopped, err := s.runDiscovery(ctx, scanID, candidates, discoveryRun{
				onFile: func(file ScanFile) {
					processed++
					_ = s.setWorkflowCounts(ctx, scanID, owner, "matching", total, processed, file.RelativePath, "Discovering MusicBrainz catalog entries.")
				},
			})
			if err != nil {
				return matched, warnings, err
			}
			matched += result.Matched
			warnings = append(warnings, result.Warnings...)
			if stopped && ctx.Err() != nil {
				return matched, warnings, ctx.Err()
			}
		}
		cursor = next
	}
}

func (s *Service) fingerprintAllFiles(ctx context.Context, scanID int64, owner, key string, catalog *localCatalog) (int, []string, error) {
	warnings := []string{}
	matched := 0
	cursor := ""
	total, err := s.unresolvedCount(ctx, scanID)
	if err != nil {
		return matched, warnings, err
	}
	processed := 0
	for {
		if err := s.renewWorkflow(ctx, scanID, owner); err != nil {
			return matched, warnings, err
		}
		files, err := s.unresolvedScanFilesAfter(ctx, scanID, cursor, workflowFingerprintBatch)
		if err != nil {
			return matched, warnings, err
		}
		if len(files) == 0 {
			return matched, warnings, nil
		}
		if cancelled, err := s.workflowCancelled(ctx, scanID); err != nil {
			return matched, warnings, err
		} else if cancelled {
			return matched, warnings, nil
		}
		next := files[len(files)-1].RelativePath
		result, err := s.runFingerprint(ctx, scanID, files, key, fingerprintRun{
			catalog: catalog,
			onFile: func(file ScanFile) {
				processed++
				_ = s.setWorkflowCounts(ctx, scanID, owner, "fingerprinting", total, processed, file.RelativePath, "Fingerprinting and looking up AcoustID matches.")
			},
		})
		matched += result.Matched
		warnings = append(warnings, result.Warnings...)
		if err != nil {
			return matched, warnings, err
		}
		cursor = next
	}
}

func (s *Service) fingerprintAvailability(ctx context.Context) (string, string) {
	key, err := s.acoustidKey(ctx)
	if err != nil {
		return "", "Audio fingerprint matching was skipped: " + matchErrorMessage(err)
	}
	if s.fingerprinter == nil {
		return "", "Audio fingerprint matching was skipped: Chromaprint is not available in this deployment."
	}
	if s.acoustid == nil {
		return "", "Audio fingerprint matching was skipped: the AcoustID provider is unavailable."
	}
	if err := s.fingerprinter.Available(ctx); err != nil {
		return "", "Audio fingerprint matching was skipped: " + matchErrorMessage(err)
	}
	return key, ""
}

func (s *Service) runFinalizeWorkflow(ctx context.Context, scan Scan, owner string) error {
	id := scan.ID
	base := context.WithoutCancel(ctx)
	matchedIDs, err := s.matchedScanFileIDs(base, id)
	if err != nil {
		return err
	}
	if err := s.startWorkflowPhase(base, id, owner, "organizing", len(matchedIDs), "Registering matched files in place."); err != nil {
		if errors.Is(err, errWorkLeaseLost) {
			return nil
		}
		return err
	}
	registered, failed := 0, 0
	firstFailure := ""
	registrationAborted := false
	for {
		ids, err := s.matchedScanFileIDs(base, id)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			break
		}
		if len(ids) > workflowFinalizeBatch {
			ids = ids[:workflowFinalizeBatch]
		}
		if cancelled, err := s.workflowCancelled(base, id); err != nil {
			return err
		} else if cancelled {
			return nil
		}
		count, err := s.RegisterScan(base, id, ids)
		if err != nil {
			failed += len(ids)
			registrationAborted = true
			if firstFailure == "" {
				firstFailure = "Registration failed: " + err.Error()
			}
			break
		}
		if count == 0 {
			failed += len(ids)
			registrationAborted = true
			if firstFailure == "" {
				firstFailure = "Some matched files could not be registered."
			}
			break
		}
		registered += count
		if err := s.setWorkflowCounts(base, id, owner, "organizing", len(matchedIDs), registered, "", "Registering matched files in place."); err != nil {
			if errors.Is(err, errWorkLeaseLost) {
				return nil
			}
			return err
		}
	}
	if !registrationAborted && registered < len(matchedIDs) {
		failed += len(matchedIDs) - registered
		if firstFailure == "" {
			firstFailure = "Some matched files changed after the scan or could not be verified, so they were not registered."
		}
	}
	fileIDs, err := s.registeredMusicFileIDs(base, id)
	if err != nil {
		return err
	}
	if len(fileIDs) > 0 {
		if err := s.setWorkflowCounts(base, id, owner, "organizing", len(fileIDs), 0, "", "Organizing and tagging registered files."); err != nil {
			if errors.Is(err, errWorkLeaseLost) {
				return nil
			}
			return err
		}
	}
	organized := 0
	for index, fileID := range fileIDs {
		if cancelled, err := s.workflowCancelled(base, id); err != nil {
			return err
		} else if cancelled {
			return nil
		}
		if err := s.renewWorkflow(base, id, owner); err != nil {
			if errors.Is(err, errWorkLeaseLost) {
				return nil
			}
			return err
		}
		if err := s.setWorkflowCounts(base, id, owner, "organizing", len(fileIDs), index, "", fmt.Sprintf("Organizing and tagging file %d of %d.", index+1, len(fileIDs))); err != nil {
			if errors.Is(err, errWorkLeaseLost) {
				return nil
			}
			return err
		}
		plan, err := s.PreviewEdits(base, []int64{fileID})
		if err != nil {
			failed++
			if firstFailure == "" {
				firstFailure = "Organization preview failed: " + err.Error()
			}
			continue
		}
		if len(plan.Files) == 0 {
			organized++
			_ = s.setWorkflowCounts(base, id, owner, "organizing", len(fileIDs), index+1, "", fmt.Sprintf("Organizing and tagging file %d of %d.", index+1, len(fileIDs)))
			continue
		}
		_ = s.setWorkflowCounts(base, id, owner, "organizing", len(fileIDs), index, plan.Files[0].SourcePath, fmt.Sprintf("Organizing and tagging file %d of %d.", index+1, len(fileIDs)))
		job, err := s.ApplyEdits(base, []int64{plan.Files[0].MusicFileID}, plan.PlanHash, true)
		if err != nil {
			failed++
			if firstFailure == "" {
				firstFailure = "Organization failed: " + err.Error()
			}
			continue
		}
		outcome, err := s.waitEditJob(ctx, job.ID)
		if err != nil {
			failed++
			if firstFailure == "" {
				firstFailure = "The organization job did not finish in time; inspect the edit job and retained files before retrying."
			}
			break
		}
		if outcome.State != "completed" {
			failed++
			if firstFailure == "" {
				firstFailure = firstNonEmpty(outcome.Error, "An organization job did not complete.")
			}
			continue
		}
		organized++
		if err := s.setWorkflowCounts(base, id, owner, "organizing", len(fileIDs), index+1, "", "Organizing and tagging registered files."); err != nil {
			if errors.Is(err, errWorkLeaseLost) {
				return nil
			}
			return err
		}
	}
	unresolved, err := s.unresolvedCount(base, id)
	if err != nil {
		return err
	}
	if len(fileIDs) > 0 {
		_ = s.setWorkflowCounts(base, id, owner, "organizing", len(fileIDs), len(fileIDs), "", "Finalizing organization results.")
	}
	message := fmt.Sprintf("Registered %d files; organized %d files; %d failed.", registered, organized, failed)
	if unresolved > 0 {
		message += fmt.Sprintf(" %d files remain unresolved for review.", unresolved)
	}
	if failed > 0 || firstFailure != "" {
		message += " Failed and unmatched files stay available for manual review."
		return s.finishWorkflow(base, id, owner, "review", "ready", message, firstFailure)
	}
	return s.finishWorkflow(base, id, owner, "completed", "completed", message, "")
}

func (s *Service) waitEditJob(ctx context.Context, id int64) (EditJob, error) {
	// An edit job runs in its own bounded goroutine. The finalize worker waits
	// for it independently of the claim deadline so a slow claim can never
	// start a second edit job on the same files.
	waitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Minute)
	defer cancel()
	for {
		job, err := s.EditJob(waitCtx, id)
		if err != nil {
			return EditJob{}, err
		}
		if job.State != "running" && job.State != "pending" {
			return job, nil
		}
		timer := time.NewTimer(workflowEditPoll)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return job, waitCtx.Err()
		case <-timer.C:
		}
	}
}

func (s *Service) unresolvedCount(ctx context.Context, scanID int64) (int, error) {
	return s.countScanFiles(ctx, scanID, "unresolved")
}

func (s *Service) countScanFiles(ctx context.Context, scanID int64, state string) (int, error) {
	var count int
	err := s.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_scan_files WHERE scan_id=? AND state=?`, scanID, state).Scan(&count)
	return count, err
}

func (s *Service) matchedScanFileIDs(ctx context.Context, scanID int64) ([]int64, error) {
	rows, err := s.store.db.QueryContext(ctx, `SELECT id FROM music_scan_files WHERE scan_id=? AND finalize_requested=1 AND state='matched' ORDER BY relative_path,id`, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Service) registeredMusicFileIDs(ctx context.Context, scanID int64) ([]int64, error) {
	rows, err := s.store.db.QueryContext(ctx, `SELECT f.id FROM music_files f
		JOIN music_scan_files s ON s.relative_path=f.relative_path
		WHERE s.scan_id=? AND s.finalize_requested=1 AND s.state='registered' ORDER BY f.id`, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// unresolvedScanFilesAfter pages through unresolved files by relative path so a
// bounded worker batch does not skip or repeat files in one claim.
func (s *Service) unresolvedScanFilesAfter(ctx context.Context, scanID int64, cursor string, limit int) ([]ScanFile, error) {
	rows, err := s.store.db.QueryContext(ctx, `SELECT `+scanFileColumns+` FROM music_scan_files
		WHERE scan_id=? AND state='unresolved' AND relative_path>? ORDER BY relative_path,id LIMIT ?`, scanID, cursor, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := []ScanFile{}
	for rows.Next() {
		file, err := scanScanFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, rows.Err()
}

func matchErrorMessage(err error) string {
	var provider *Error
	if errors.As(err, &provider) && strings.TrimSpace(provider.Message) != "" {
		return provider.Message
	}
	return err.Error()
}

func isRateLimited(err error) bool {
	var provider *Error
	return errors.As(err, &provider) && provider.Code == "music_rate_limited"
}

func dedupeWarnings(warnings []string) string {
	seen := map[string]bool{}
	out := []string{}
	for _, warning := range warnings {
		warning = strings.TrimSpace(warning)
		if warning == "" || seen[warning] {
			continue
		}
		seen[warning] = true
		out = append(out, warning)
	}
	return strings.Join(out, " ")
}
