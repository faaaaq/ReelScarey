package music

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/organization"
)

// The upgrade queue deliberately shares one durable work clock. Its rate is
// the application-owned maximum described in migration 114, never a client
// preference or a per-job allowance.
var (
	upgradeAttemptInterval = 18 * time.Second
	upgradeRetryBackoff    = 5 * time.Minute
	upgradeLeaseSeconds    = 300
	upgradePollInterval    = 2 * time.Second
)

var errUpgradeLeaseLost = errors.New("music upgrade lease lost")
var errUpgradeStageMissing = errors.New("music upgrade publication stage is missing")

// lostUpgradeLease reports whether the renewer gave up this claim. The renewer
// writes at most once, so the check never blocks.
func lostUpgradeLease(claimErr <-chan error) bool {
	select {
	case <-claimErr:
		return true
	default:
		return false
	}
}

type upgradePublication struct {
	Version     int       `json:"version"`
	Stage       string    `json:"stage"`
	Destination string    `json:"destination"`
	SHA256      string    `json:"sha256"`
	SizeBytes   int64     `json:"size_bytes"`
	Tags        AudioTags `json:"tags"`
}

type upgradeClaim struct {
	item       upgradeItem
	jobID      int64
	owner      string
	target     string
	maxBitrate int
}

// RecoverUpgradeJobs makes work interrupted by a restart claimable again. It
// does not reset the durable global rate clock or discard completed evidence.
func (s *Service) RecoverUpgradeJobs(ctx context.Context) error {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE music_upgrade_jobs SET state='queued',
			error='The upgrade was interrupted by a restart and will retry.',
			current_track='',lease_owner='',lease_until=0,updated_at=unixepoch()
		WHERE state='running'`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE music_upgrade_items SET state='pending',not_before=0,updated_at=unixepoch()
		WHERE state IN ('searching','downloading')`); err != nil {
		return err
	}
	return tx.Commit()
}

// RunUpgradeJobs is the durable single-worker loop. Like the conversion
// worker, it claims work from SQLite and never keeps an in-memory queue.
func (s *Service) RunUpgradeJobs(ctx context.Context, logger *slog.Logger) {
	ticker := time.NewTicker(upgradePollInterval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		worked, err := s.processUpgrade(ctx)
		if err != nil && ctx.Err() == nil {
			logger.Error("music upgrade worker failed", "error", err)
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

func (s *Service) processUpgrade(ctx context.Context) (bool, error) {
	claim, claimed, err := s.claimUpgrade(ctx)
	if err != nil || !claimed {
		return false, err
	}
	return true, s.runUpgrade(ctx, claim)
}

func (s *Service) claimUpgrade(ctx context.Context) (upgradeClaim, bool, error) {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return upgradeClaim{}, false, err
	}
	defer tx.Rollback()
	now := time.Now()
	nowMS := now.UnixMilli()
	// An expired worker has the same durable treatment as a restart. Its old
	// owner cannot publish because the promotion write requires this lease.
	if _, err := tx.ExecContext(ctx, `UPDATE music_upgrade_items SET state='pending',not_before=0,updated_at=unixepoch()
		WHERE state IN ('searching','downloading') AND job_id IN
		(SELECT id FROM music_upgrade_jobs WHERE state='running' AND lease_until<=unixepoch())`); err != nil {
		return upgradeClaim{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE music_upgrade_jobs SET state='queued',
			error='The upgrade lease expired and will retry.',current_track='',lease_owner='',lease_until=0,updated_at=unixepoch()
		WHERE state='running' AND lease_until<=unixepoch()`); err != nil {
		return upgradeClaim{}, false, err
	}
	var itemID int64
	err = tx.QueryRowContext(ctx, `SELECT i.id FROM music_upgrade_items i
		JOIN music_upgrade_jobs j ON j.id=i.job_id
		JOIN music_upgrade_rate r ON r.id=1
		WHERE i.state='pending' AND j.state='queued' AND i.not_before<=? AND r.next_attempt_ms<=?
		ORDER BY i.id LIMIT 1`, nowMS, nowMS).Scan(&itemID)
	if errors.Is(err, sql.ErrNoRows) {
		return upgradeClaim{}, false, nil
	}
	if err != nil {
		return upgradeClaim{}, false, err
	}
	owner := workflowOwner()
	if _, err := tx.ExecContext(ctx, `UPDATE music_upgrade_rate SET next_attempt_ms=? WHERE id=1`,
		now.Add(upgradeAttemptInterval).UnixMilli()); err != nil {
		return upgradeClaim{}, false, err
	}
	item, err := scanUpgradeItem(tx.QueryRowContext(ctx, `SELECT `+upgradeItemColumns+` FROM music_upgrade_items WHERE id=?`, itemID))
	if err != nil {
		return upgradeClaim{}, false, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE music_upgrade_items
		SET state='searching',detail='',attempts=attempts+1,last_attempt_at=?,not_before=0,updated_at=unixepoch()
		WHERE id=? AND state='pending'`, nowMS, itemID)
	if err != nil {
		return upgradeClaim{}, false, err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return upgradeClaim{}, false, nil
	}
	result, err = tx.ExecContext(ctx, `UPDATE music_upgrade_jobs
		SET state='running',current_track=?,lease_owner=?,lease_until=unixepoch()+?,updated_at=unixepoch()
		WHERE id=? AND state='queued'`, item.Artist+" — "+item.Title, owner, upgradeLeaseSeconds, item.JobID)
	if err != nil {
		return upgradeClaim{}, false, err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return upgradeClaim{}, false, nil
	}
	var target string
	var capBitrate int
	if err := tx.QueryRowContext(ctx, `SELECT target_quality,max_bitrate_kbps FROM music_upgrade_jobs WHERE id=?`, item.JobID).
		Scan(&target, &capBitrate); err != nil {
		return upgradeClaim{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return upgradeClaim{}, false, err
	}
	item.State = "searching"
	item.Attempts++
	item.NotBefore = 0
	return upgradeClaim{item: item, jobID: item.JobID, owner: owner, target: target, maxBitrate: capBitrate}, true, nil
}

func (s *Service) renewUpgrade(ctx context.Context, claim upgradeClaim) error {
	result, err := s.store.db.ExecContext(ctx, `UPDATE music_upgrade_jobs
		SET lease_until=unixepoch()+?,updated_at=unixepoch()
		WHERE id=? AND lease_owner=? AND state='running'`, upgradeLeaseSeconds, claim.jobID, claim.owner)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return errUpgradeLeaseLost
	}
	return nil
}

func (s *Service) upgradeStillClaimed(ctx context.Context, claim upgradeClaim) (bool, error) {
	var state, owner string
	err := s.store.db.QueryRowContext(ctx, `SELECT state,lease_owner FROM music_upgrade_jobs WHERE id=?`, claim.jobID).
		Scan(&state, &owner)
	if err != nil {
		return false, err
	}
	return state == "running" && owner == claim.owner, nil
}

// runUpgrade searches, verifies, stages and publishes one item. Errors are
// written back to durable state; only database failures return to the worker.
func (s *Service) runUpgrade(ctx context.Context, claim upgradeClaim) error {
	base := context.WithoutCancel(ctx)
	release := organization.ReadMedia()
	defer release()
	if err := s.upgradeMkdir(claim); err != nil {
		return s.finishUpgradeItem(base, claim, "failed", err)
	}
	defer s.removeUpgradeAttempt(claim)

	// The renewer extends the job lease while the item runs and stops when
	// runUpgrade returns. A lost lease cancels leaseCtx, which stops an
	// in-flight search or download; writes to durable state also require the
	// lease, so a stale owner can neither publish nor record an outcome.
	leaseCtx, cancelLease := context.WithCancel(base)
	claimErr := make(chan error, 1)
	renewerDone := make(chan struct{})
	go func() {
		defer close(renewerDone)
		ticker := time.NewTicker(time.Duration(upgradeLeaseSeconds) * time.Second / 3)
		defer ticker.Stop()
		for {
			select {
			case <-leaseCtx.Done():
				return
			case <-ticker.C:
				if err := s.renewUpgrade(base, claim); err != nil {
					claimErr <- err
					cancelLease()
					return
				}
			}
		}
	}()
	defer func() {
		cancelLease()
		<-renewerDone
	}()
	if lostUpgradeLease(claimErr) {
		return nil
	}

	// A publication saved before the transaction completed is the only safe
	// crash resume path. If neither its stage nor destination exists, a fresh
	// attempt redownloads rather than trusting a lost staging path.
	if strings.TrimSpace(claim.item.Publication) != "" {
		var pub upgradePublication
		if err := json.Unmarshal([]byte(claim.item.Publication), &pub); err == nil {
			err = s.publishUpgrade(base, claim, pub, "")
			if err == nil {
				return nil
			}
			if !errors.Is(err, errUpgradeStageMissing) {
				if ctx.Err() != nil || errors.Is(err, errUpgradeLeaseLost) {
					return nil
				}
				return s.finishUpgradeItem(base, claim, "failed", err)
			}
			if _, err := s.store.db.ExecContext(base, `UPDATE music_upgrade_items SET publication='',updated_at=unixepoch()
				WHERE id=? AND state IN ('searching','downloading') AND EXISTS(
					SELECT 1 FROM music_upgrade_jobs WHERE id=? AND lease_owner=? AND state='running')`,
				claim.item.ID, claim.jobID, claim.owner); err != nil {
				return err
			}
		}
	}

	source, err := s.selectAudioSource("automatic")
	if err != nil {
		return s.finishUpgradeItem(base, claim, "failed", err)
	}
	query := strings.TrimSpace(claim.item.Artist + " " + claim.item.Title)
	candidates, err := source.SearchAudio(leaseCtx, query, 10)
	if err != nil {
		return s.finishUpgradeItem(base, claim, "failed", &Error{Code: "music_upgrade_source", Message: audioSourceFailure(source, "search", err)})
	}
	decision := evaluateAudioCandidates(candidates, AcquisitionTrackInput{
		Title: claim.item.Title, Artist: claim.item.Artist, DurationMS: claim.item.DurationMS,
	})
	evidence, marshalErr := json.Marshal(decision.Candidates)
	if marshalErr != nil || len(evidence) > 256<<10 {
		return s.finishUpgradeItem(base, claim, "failed", &Error{Code: "music_upgrade_evidence", Message: "The audio candidate evidence could not be saved safely."})
	}
	if ok, err := s.guardedUpgradeItemWrite(base, claim, `UPDATE music_upgrade_items
		SET candidate_evidence_json=?,state='downloading',updated_at=unixepoch() WHERE id=?`, string(evidence)); err != nil || !ok {
		return err
	}
	if decision.Chosen == nil {
		return s.finishUpgradeItem(base, claim, "skipped", &Error{Code: "music_upgrade_skipped", Message: decision.Reason})
	}
	videoID := decision.Chosen.ID
	selectedURL := videoID
	if !strings.Contains(videoID, "://") {
		selectedURL = "https://www.youtube.com/watch?v=" + videoID
	}
	if ok, err := s.guardedUpgradeItemWrite(base, claim, `UPDATE music_upgrade_items
		SET selected_source_id=?,selected_source_url=?,updated_at=unixepoch() WHERE id=?`, videoID, selectedURL); err != nil || !ok {
		return err
	}

	stageDir := s.upgradeAttemptDir(claim)
	audio, err := source.DownloadAudio(leaseCtx, videoID, stageDir, "source")
	if err != nil {
		return s.finishUpgradeItem(base, claim, "failed", &Error{Code: "music_upgrade_source", Message: audioSourceFailure(source, "download", err)})
	}
	if err := verifyDownloadedAudio(stageDir, audio.Path); err != nil {
		return s.finishUpgradeItem(base, claim, "failed", err)
	}
	newTags, err := s.prober.ProbeAudio(base, audio.Path)
	if err != nil {
		return s.finishUpgradeItem(base, claim, "failed", err)
	}
	if ok, reason := evaluateUpgradeQuality(claim.item, newTags, claim.target, claim.maxBitrate); !ok {
		return s.finishUpgradeItem(base, claim, "skipped", &Error{Code: "music_upgrade_skipped", Message: reason})
	}
	sha, size, err := stageDigest(base, audio.Path)
	if err != nil {
		return s.finishUpgradeItem(base, claim, "failed", err)
	}
	ext, err := upgradeAudioExtension(audio.Path, newTags.Format)
	if err != nil {
		return s.finishUpgradeItem(base, claim, "failed", err)
	}
	pub := upgradePublication{
		Version: 1, SHA256: sha, SizeBytes: size, Tags: newTags,
	}
	pub.Destination = upgradeDestinationPath(claim.item.RelativePath, int64(claim.item.ID), ext)
	pub.Stage = upgradeStagePath(claim.item.RelativePath, claim.jobID, int64(claim.item.ID), ext)
	pubJSON, err := json.Marshal(pub)
	if err != nil || len(pubJSON) > 64<<10 {
		return s.finishUpgradeItem(base, claim, "failed", &Error{Code: "music_upgrade_journal", Message: "The upgrade publication could not be journaled safely."})
	}
	if ok, err := s.guardedUpgradeItemWrite(base, claim, `UPDATE music_upgrade_items SET publication=?,updated_at=unixepoch() WHERE id=?`, string(pubJSON)); err != nil || !ok {
		return err
	}
	if err := s.publishUpgrade(base, claim, pub, audio.Path); err != nil {
		if ctx.Err() != nil || errors.Is(err, errUpgradeLeaseLost) {
			return nil
		}
		return s.finishUpgradeItem(base, claim, "failed", err)
	}
	return nil
}

// guardedUpgradeItemWrite records item progress only while this worker still
// owns the job lease. It supplies the item ID and the lease guard itself, so
// `args` must cover only the statement's own placeholders. ok is false when
// the lease is gone and nothing was written; a database error is returned so
// the worker can log it instead of stalling on an invisible failure.
func (s *Service) guardedUpgradeItemWrite(ctx context.Context, claim upgradeClaim, statement string, args ...any) (bool, error) {
	args = append(args, claim.item.ID, claim.jobID, claim.owner)
	result, err := s.store.db.ExecContext(ctx, statement+`
		AND EXISTS(SELECT 1 FROM music_upgrade_jobs WHERE id=? AND lease_owner=? AND state='running')`, args...)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// publishUpgrade stages the verified source below the original's folder, then
// swaps roles in one transaction. The old row and bytes remain at the old path
// as the archive; the new sibling becomes the primary.
func (s *Service) publishUpgrade(ctx context.Context, claim upgradeClaim, pub upgradePublication, downloadedPath string) error {
	if pub.Version != 1 || pub.SHA256 == "" || pub.Destination == "" || pub.Stage == "" {
		return errUpgradeStageMissing
	}
	destination, err := cleanMediaRelative(pub.Destination)
	if err != nil {
		return errUpgradeStageMissing
	}
	stage, err := cleanMediaRelative(pub.Stage)
	if err != nil {
		return errUpgradeStageMissing
	}
	expectedDest := upgradeDestinationPath(claim.item.RelativePath, int64(claim.item.ID), path.Ext(destination))
	expectedStage := upgradeStagePath(claim.item.RelativePath, claim.jobID, int64(claim.item.ID), path.Ext(destination))
	if destination != expectedDest || stage != expectedStage {
		return &Error{Code: "music_upgrade_changed", Message: "The saved upgrade publication does not match its item."}
	}
	if ok, reason := evaluateUpgradeQuality(claim.item, pub.Tags, claim.target, claim.maxBitrate); !ok {
		return &Error{Code: "music_upgrade_quality", Message: reason}
	}

	if _, _, err := s.safeMediaPath(claim.item.RelativePath); err != nil {
		return err
	}
	media, err := os.OpenRoot(s.mediaDir)
	if err != nil {
		return err
	}
	defer media.Close()
	if err := expectMusicDigest(ctx, media, claim.item.RelativePath, claim.item.SHA256); err != nil {
		return &Error{Code: "music_upgrade_changed", Message: "The original music file changed before the upgrade could be published."}
	}
	stagePresent := false
	if info, statErr := media.Lstat(stage); statErr == nil {
		if !info.Mode().IsRegular() {
			return occupiedDestinationError()
		}
		stagePresent = true
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	destInfo, statErr := media.Lstat(destination)
	switch {
	case statErr == nil:
		if !destInfo.Mode().IsRegular() {
			return occupiedDestinationError()
		}
		if err := expectMusicDigest(ctx, media, destination, pub.SHA256); err != nil {
			return occupiedDestinationError()
		}
	case errors.Is(statErr, os.ErrNotExist):
		if !stagePresent {
			if downloadedPath == "" || !sameDownloadedBytes(ctx, downloadedPath, pub.SHA256) {
				return errUpgradeStageMissing
			}
			if err := media.MkdirAll(path.Dir(stage), 0o700); err != nil {
				return err
			}
			if err := copyIntoMediaRoot(ctx, media, downloadedPath, stage); err != nil {
				return err
			}
			stagePresent = true
		}
		if err := expectMusicDigest(ctx, media, stage, pub.SHA256); err != nil {
			return err
		}
		if err := linkMusicStage(ctx, media, destination, stage); err != nil {
			return err
		}
	default:
		return statErr
	}
	if err := expectMusicDigest(ctx, media, destination, pub.SHA256); err != nil {
		return err
	}
	if err := expectMusicDigest(ctx, media, claim.item.RelativePath, claim.item.SHA256); err != nil {
		return &Error{Code: "music_upgrade_changed", Message: "The original music file changed while the replacement was staged."}
	}
	tagJSON, err := json.Marshal(pub.Tags)
	if err != nil {
		return err
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state, owner string
	if err := tx.QueryRowContext(ctx, `SELECT state,lease_owner FROM music_upgrade_jobs WHERE id=?`, claim.jobID).
		Scan(&state, &owner); err != nil {
		return err
	}
	if state != "running" || owner != claim.owner {
		return errUpgradeLeaseLost
	}
	var competing int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_files
		WHERE track_id=? AND variant_role='primary' AND id<>?`, claim.item.TrackID, claim.item.MusicFileID).Scan(&competing); err != nil {
		return err
	}
	if competing != 0 {
		return &Error{Code: "music_upgrade_changed", Message: "The track gained another primary file during the upgrade. Nothing was replaced."}
	}
	var occupant int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM music_files WHERE relative_path=?`, destination).Scan(&occupant)
	if err == nil {
		return &Error{Code: "music_upgrade_changed", Message: "The replacement path is already registered. Nothing was replaced."}
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE music_files
		SET variant_role='archive',variant_profile='',sha256=?
		WHERE id=? AND relative_path=? AND variant_role='primary' AND track_id=? AND (sha256=? OR sha256='')`,
		claim.item.SHA256, claim.item.MusicFileID, claim.item.RelativePath, claim.item.TrackID, claim.item.SHA256)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return &Error{Code: "music_upgrade_changed", Message: "The original registration changed during the upgrade. Nothing was replaced."}
	}
	insert, err := tx.ExecContext(ctx, `INSERT INTO music_files
		(artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,sample_rate,channels,duration_ms,
		 tag_json,imported_at,origin,source_name,variant_role,variant_profile)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,unixepoch(),'upgrade',?,'primary','')`,
		claim.item.ArtistID, claim.item.AlbumID, claim.item.TrackID, claim.item.Root, destination, pub.SizeBytes, pub.SHA256,
		pub.Tags.Format, pub.Tags.Bitrate, pub.Tags.SampleRate, pub.Tags.Channels, pub.Tags.DurationMS, string(tagJSON),
		path.Base(claim.item.RelativePath))
	if err != nil {
		return err
	}
	newFileID, err := insert.LastInsertId()
	if err != nil {
		return err
	}
	detail := "The verified source copy replaced the original; the original remains as an archive variant."
	if _, err := tx.ExecContext(ctx, `UPDATE music_upgrade_items
		SET state='upgraded',detail=?,new_bitrate=?,new_format=?,new_sample_rate=?,new_music_file_id=?,updated_at=unixepoch()
		WHERE id=? AND state IN ('searching','downloading')`,
		detail, pub.Tags.Bitrate, pub.Tags.Format, pub.Tags.SampleRate, newFileID, claim.item.ID); err != nil {
		return err
	}
	if err := refreshUpgradeJobTx(ctx, tx, claim.jobID, claim.owner, claim.item.ID, true); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_ = media.Remove(stage)
	return nil
}

// finishUpgradeItem writes an item outcome and refreshes its job. A skipped
// item is final. A failed item returns to pending with a durable bounded
// backoff only while its cause may clear by itself and attempts remain.
func (s *Service) finishUpgradeItem(ctx context.Context, claim upgradeClaim, state string, cause error) error {
	detail := upgradeFailureMessage(cause)
	if errors.Is(ctx.Err(), context.Canceled) {
		detail = "The upgrade was interrupted and will retry."
	}
	itemState := state
	terminal := true
	notBefore := int64(0)
	if state == "failed" && upgradeFailureRetryable(cause) && claim.item.Attempts < upgradeMaxAttempts {
		itemState = "pending"
		terminal = false
		notBefore = time.Now().Add(time.Duration(claim.item.Attempts) * upgradeRetryBackoff).UnixMilli()
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE music_upgrade_items SET state=?,detail=?,not_before=?,updated_at=unixepoch()
		WHERE id=? AND state IN ('searching','downloading') AND EXISTS(
			SELECT 1 FROM music_upgrade_jobs WHERE id=? AND lease_owner=? AND state='running')`,
		itemState, detail, notBefore, claim.item.ID, claim.jobID, claim.owner); err != nil {
		return err
	}
	if err := refreshUpgradeJobTx(ctx, tx, claim.jobID, claim.owner, claim.item.ID, terminal); err != nil {
		if errors.Is(err, errUpgradeLeaseLost) {
			// The guarded item write above was a no-op too: the job was
			// cancelled or taken over, so there is nothing left to record.
			return nil
		}
		return err
	}
	return tx.Commit()
}

// refreshUpgradeJobTx derives the public job state and message from item
// facts inside the same transaction that changed an item, so counts can
// never diverge. The message is the newest failure detail; a skip reason
// belongs to its item and is not a problem with the job. The item's active
// guard is released once the item reaches a terminal state.
func refreshUpgradeJobTx(ctx context.Context, tx *sql.Tx, jobID int64, owner string, itemID int64, itemTerminal bool) error {
	var total, pending, upgraded, skipped, failed int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*),
			COALESCE(SUM(CASE WHEN state IN ('pending','searching','downloading') THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN state='upgraded' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN state='skipped' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN state='failed' THEN 1 ELSE 0 END),0)
		FROM music_upgrade_items WHERE job_id=?`, jobID).Scan(&total, &pending, &upgraded, &skipped, &failed)
	if err != nil {
		return err
	}
	next := "completed"
	if pending > 0 {
		next = "queued"
	} else if failed > 0 {
		next = "failed"
	}
	jobError := ""
	if next != "completed" {
		// A failed item or one waiting to retry after a failed attempt.
		err := tx.QueryRowContext(ctx, `SELECT detail FROM music_upgrade_items
			WHERE job_id=? AND detail<>'' AND (state='failed' OR (state='pending' AND attempts>0))
			ORDER BY updated_at DESC, id DESC LIMIT 1`, jobID).Scan(&jobError)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE music_upgrade_jobs
		SET state=?,total_count=?,processed_count=?,upgraded_count=?,skipped_count=?,failed_count=?,
			current_track='',error=?,lease_owner='',lease_until=0,updated_at=unixepoch()
		WHERE id=? AND lease_owner=? AND state='running'`,
		next, total, upgraded+skipped+failed, upgraded, skipped, failed, jobError, jobID, owner)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return errUpgradeLeaseLost
	}
	if itemTerminal {
		if _, err := tx.ExecContext(ctx, `DELETE FROM music_upgrade_active WHERE item_id=?`, itemID); err != nil {
			return err
		}
	}
	if next != "queued" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM music_upgrade_active WHERE job_id=?`, jobID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) upgradeMkdir(claim upgradeClaim) error {
	if s.downloadsDir == "" {
		return &Error{Code: "music_import_source", Message: "The downloads root is not configured."}
	}
	return os.MkdirAll(s.upgradeAttemptDir(claim), 0o700)
}

func (s *Service) upgradeAttemptDir(claim upgradeClaim) string {
	return filepath.Join(s.downloadsDir, "music-upgrades",
		fmt.Sprintf("%d", claim.jobID), fmt.Sprintf("%d", claim.item.ID), claim.owner)
}

func (s *Service) removeUpgradeAttempt(claim upgradeClaim) {
	dir := s.upgradeAttemptDir(claim)
	if s.downloadsDir == "" || !contained(s.downloadsDir, dir) {
		return
	}
	_ = os.RemoveAll(dir)
}

// upgradeFailureRetryable reports whether another attempt could plausibly
// succeed. Typed failures describe the original file, the catalog or the
// requested upgrade, so they are final on the first attempt; only a provider
// failure is typed and still worth retrying. Untyped errors are disk or
// database trouble, which may clear on its own, except for invalid input.
func upgradeFailureRetryable(err error) bool {
	var musicErr *Error
	if errors.As(err, &musicErr) {
		return musicErr.Code == "music_upgrade_source"
	}
	return !errors.Is(err, ErrInvalidInput)
}

func upgradeFailureMessage(err error) string {
	var musicErr *Error
	if errors.As(err, &musicErr) && strings.TrimSpace(musicErr.Message) != "" {
		return musicErr.Message
	}
	if err == nil {
		return "The upgrade stopped unexpectedly."
	}
	message := redactPaths(err.Error())
	if len(message) > 600 {
		message = message[:600]
	}
	if strings.TrimSpace(message) == "" {
		return "The upgrade stopped unexpectedly."
	}
	return message
}

func verifyDownloadedAudio(stageDir, downloadedPath string) error {
	info, err := os.Lstat(downloadedPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return &Error{Code: "music_upgrade_source", Message: "The downloaded audio source is missing or is not a regular file."}
	}
	if !contained(stageDir, downloadedPath) {
		return ErrInvalidInput
	}
	resolvedStage, err := filepath.EvalSymlinks(stageDir)
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(downloadedPath)
	if err != nil || !contained(resolvedStage, resolved) {
		return ErrInvalidInput
	}
	return nil
}

func upgradeAudioExtension(sourcePath, format string) (string, error) {
	ext := strings.ToLower(path.Ext(sourcePath))
	if ext != "" {
		valid := len(ext) <= 10
		for _, r := range ext[1:] {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
				valid = false
				break
			}
		}
		if valid {
			return ext, nil
		}
	}
	switch probeFormatFamily(format) {
	case "flac":
		return ".flac", nil
	case "wav":
		return ".wav", nil
	case "mp3":
		return ".mp3", nil
	case "m4a", "aac", "adts":
		return ".m4a", nil
	case "opus":
		return ".opus", nil
	default:
		return "", &Error{Code: "music_upgrade_quality", Message: "The downloaded audio source has an unrecognized audio format."}
	}
}

func upgradeDestinationPath(original string, itemID int64, ext string) string {
	name := path.Clean(strings.ReplaceAll(original, "\\", "/"))
	dir, base := path.Split(name)
	stem := strings.TrimSuffix(base, path.Ext(base))
	return cleanMediaRelativeString(dir + stem + " [upgrade-" + fmt.Sprint(itemID) + "]" + ext)
}

func upgradeStagePath(original string, jobID, itemID int64, ext string) string {
	name := path.Clean(strings.ReplaceAll(original, "\\", "/"))
	dir, base := path.Split(name)
	stem := strings.TrimSuffix(base, path.Ext(base))
	return cleanMediaRelativeString(dir + ".reelscarey-upgrade-" + fmt.Sprint(jobID) + "-" + fmt.Sprint(itemID) + "-" + stem + ext)
}

func cleanMediaRelativeString(value string) string {
	clean, err := cleanMediaRelative(value)
	if err != nil {
		return ""
	}
	return clean
}

func sameDownloadedBytes(ctx context.Context, downloadedPath, sha string) bool {
	actual, _, err := stageDigest(ctx, downloadedPath)
	return err == nil && actual == sha
}

// upgradeClass is intentionally conservative. The available probe has no
// codec/bit-depth field, so ALAC cannot be proven lossless and Hi-Res means
// proven-lossless FLAC/WAV above 48 kHz, not a bit-depth certification.
func upgradeClass(tags AudioTags) int {
	family := probeFormatFamily(tags.Format)
	if sourceIsLossless(family) {
		if tags.SampleRate > 48000 {
			return 4
		}
		return 3
	}
	if family == "" {
		return 0
	}
	if tags.Bitrate >= 256000 {
		return 2
	}
	return 1
}

// evaluateUpgradeQuality requires an exact or near duration identity and a
// strictly higher measured class. The positive cap applies only when the
// downloaded source has a known bitrate.
func evaluateUpgradeQuality(item upgradeItem, downloaded AudioTags, target string, maxBitrateKbps int) (bool, string) {
	oldClass := upgradeClass(AudioTags{
		Format: item.OldFormat, Bitrate: item.OldBitrate, SampleRate: item.OldSampleRate,
	})
	newClass := upgradeClass(downloaded)
	if oldClass == 0 {
		return false, "The current file's audio format is not recognized, so a measured improvement cannot be proven."
	}
	if newClass == 0 {
		return false, "The downloaded source's audio format is not recognized, so it was not used as an upgrade."
	}
	if maxBitrateKbps > 0 && downloaded.Bitrate > 0 && downloaded.Bitrate > maxBitrateKbps*1000 {
		return false, "The downloaded source exceeds the requested maximum bitrate."
	}
	required := 2
	switch target {
	case "lossless":
		required = 3
	case "hires":
		required = 4
	}
	if newClass < required {
		return false, "The downloaded source does not meet the requested quality."
	}
	if newClass <= oldClass {
		return false, "The downloaded source is not a measured improvement over the current file."
	}
	if item.DurationMS > 0 {
		if downloaded.DurationMS <= 0 {
			return false, "The downloaded source's duration could not be verified."
		}
		diff := downloaded.DurationMS - item.DurationMS
		if diff < 0 {
			diff = -diff
		}
		limit := item.DurationMS / 50
		if limit > 10000 {
			limit = 10000
		}
		if limit < 1000 {
			limit = 1000
		}
		if diff > limit {
			return false, "The downloaded source duration does not match the requested recording."
		}
	}
	return true, ""
}
