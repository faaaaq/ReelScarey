package automation

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"reflect"
	"time"

	"github.com/faaaaq/ReelScarey/internal/quality"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

// The announce processor turns captured IPTorrents announce events into ordinary
// downloads. internal/announce records every event and marks the ones a live
// IPTorrents source opted into (processing_state='pending'); this worker claims
// those events durably, matches them to the saved library with the same identity
// and quality rules the scheduled search uses, and hands the exact release to
// transfer.QueueAnnounceAfter. It never carries a download URL or credential and
// never bypasses the reservation/importer workflow it delegates to.

const (
	// announceBatch bounds one processing pass so a large backlog cannot stall
	// a single worker turn forever; later passes continue where it left off.
	announceBatch = 25
	// announceLease is the per-event lease a claim holds; the heartbeat renews
	// it every 15s, well inside the window, and a crashed worker's lease is
	// reclaimed by the next pass.
	announceLease = int64(60)
	// announceMaxAttempts and announceMaxAge bound transient retries. A capture
	// that keeps failing is surfaced for review instead of retrying forever.
	announceMaxAttempts = 8
	announceMaxAge      = int64(24 * 3600)
	// announceBackoff is the first retry delay; it doubles per attempt up to the
	// cap.
	announceBackoff    = int64(30)
	announceBackoffMax = int64(15 * 60)
)

// announceEvent is one claimed captured announce. Only the columns the
// processor needs for identity and honest reporting are loaded; the encrypted
// source settings stay in the database and reach transfer directly.
type announceEvent struct {
	ID             int64
	SourceID       string
	Tracker        string
	ReleaseName    string
	Category       string
	SizeBytes      int64
	Freeleech      string
	TorrentID      string
	InfoURL        string
	ReceivedAt     int64
	SourceRevision int
	Attempts       int
}

// RunAnnounces is the service-startup worker. It mirrors RunRSS/Run: it pumps
// ProcessAnnounces until the context is canceled, backing off five seconds
// between passes and logging deferred errors without spinning.
func (s *Service) RunAnnounces(ctx context.Context, logger *slog.Logger) {
	for ctx.Err() == nil {
		_, e := s.ProcessAnnounces(ctx)
		if e != nil && ctx.Err() == nil {
			logger.Warn("announce processing deferred", "error", e)
		}
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// ProcessAnnounces claims a bounded batch of pending/retry events and processes
// them under a set of 60s owner leases that a 15s heartbeat keeps alive. The
// completion of each event is fenced on the owner and a live lease, so a lease
// that expired mid-flight is reclaimed by the next pass rather than completed
// twice.
func (s *Service) ProcessAnnounces(ctx context.Context) (bool, error) {
	batch, owner, e := s.claimAnnounces(ctx)
	if e != nil {
		return false, e
	}
	if len(batch) == 0 {
		return false, nil
	}
	work, cancel := context.WithTimeout(ctx, 8*time.Minute)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-work.Done():
				return
			case <-ticker.C:
				if e := s.heartbeatAnnounces(work, owner); e != nil {
					cancel()
					return
				}
			}
		}
	}()
	e = s.announcePass(work, owner, batch)
	cancel()
	<-done
	if ctx.Err() != nil || errors.Is(e, ErrLease) || errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
		// Leases were lost or the application is restarting. Every claimed
		// event keeps its lease until it expires and is reclaimed, so nothing
		// needs to be unwound here and no terminal outcome was recorded.
		return true, nil
	}
	if e != nil {
		// A genuine storage or index error aborted the pass; surface it and let
		// RunAnnounces log and retry rather than silently dropping the batch.
		return true, e
	}
	return true, nil
}

// claimAnnounces reclaims expired leases and atomically claims a bounded batch
// of pending or retry events. The increment of attempts and the clearing of
// next_run happen under the same statement that takes the lease, so a retry is
// counted exactly once per claim and a claimed event cannot be re-claimed by a
// concurrent pass while its backoff window is still open.
func (s *Service) claimAnnounces(ctx context.Context) ([]announceEvent, string, error) {
	now := s.now().Unix()
	owner := transfer.ID()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, "", e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `UPDATE announce_events SET processing_state='retry',message='An interrupted announce was reclaimed after its lease expired.',lease_owner='',lease_until=0,next_run=? WHERE processing_state='processing' AND (lease_until IS NULL OR lease_until<=?)`, now, now); e != nil {
		return nil, "", e
	}
	rows, e := tx.QueryContext(ctx, `UPDATE announce_events SET processing_state='processing',lease_owner=?,lease_until=?,attempts=attempts+1,next_run=NULL WHERE id IN (
SELECT id FROM announce_events WHERE tracker='iptorrents' AND processing_state IN ('pending','retry') AND (next_run IS NULL OR next_run<=?) ORDER BY received_at,id LIMIT ?
) RETURNING id,source_id,tracker,release_name,category,size_bytes,freeleech,torrent_id,info_url,received_at,source_revision,attempts`, owner, now+announceLease, now, announceBatch)
	if e != nil {
		return nil, "", e
	}
	out := []announceEvent{}
	for rows.Next() {
		var ev announceEvent
		if e = rows.Scan(&ev.ID, &ev.SourceID, &ev.Tracker, &ev.ReleaseName, &ev.Category, &ev.SizeBytes, &ev.Freeleech, &ev.TorrentID, &ev.InfoURL, &ev.ReceivedAt, &ev.SourceRevision, &ev.Attempts); e != nil {
			rows.Close()
			return nil, "", e
		}
		out = append(out, ev)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, "", e
	}
	if e = tx.Commit(); e != nil {
		return nil, "", e
	}
	return out, owner, nil
}

// heartbeatAnnounces renews the lease on every event the owner still holds.
// Completed events are excluded because completion clears the lease.
func (s *Service) heartbeatAnnounces(ctx context.Context, owner string) error {
	_, e := s.db.ExecContext(ctx, `UPDATE announce_events SET lease_until=? WHERE lease_owner=? AND processing_state='processing' AND lease_until>=?`, s.now().Unix()+announceLease, owner, s.now().Unix())
	return e
}

// announceFence confirms the owner still holds a live lease on one event before
// the worker does expensive work for it.
func (s *Service) announceFence(ctx context.Context, owner string, id int64) error {
	var count int
	e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM announce_events WHERE id=? AND processing_state='processing' AND lease_owner=? AND lease_until>=?`, id, owner, s.now().Unix()).Scan(&count)
	if e != nil {
		return e
	}
	if count != 1 {
		return ErrLease
	}
	return nil
}

func (s *Service) announcePass(ctx context.Context, owner string, batch []announceEvent) error {
	if s.workflow == nil {
		return nil
	}
	idx, e := s.rssTargets(ctx)
	if e != nil {
		return e
	}
	store := quality.New(s.db)
	profiles := map[int64]quality.Profile{}
	for _, ev := range batch {
		// A canceled work context means the heartbeat lost the lease or the
		// application is shutting down; stop the pass without recording any
		// terminal outcome so the events are reclaimed and retried.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e := s.announceFence(ctx, owner, ev.ID); e != nil {
			// One event whose lease was reclaimed must not stall the rest of
			// the batch.
			if errors.Is(e, ErrLease) {
				continue
			}
			return e
		}
		if _, e := s.handleAnnounce(ctx, owner, ev, idx, store, profiles); e != nil {
			if errors.Is(e, ErrLease) {
				continue
			}
			return e
		}
	}
	return nil
}

// handleAnnounce matches one captured announce and completes it with an honest
// outcome. The event's processing_state is always resolved: a queueable match
// becomes 'done' with the download recorded, a transient failure becomes
// 'retry' with a bounded backoff, and every terminal rejection or staleness
// becomes 'done' with the reason encoded in outcome and message.
func (s *Service) handleAnnounce(ctx context.Context, owner string, ev announceEvent, idx rssIndex, store *quality.Store, profiles map[int64]quality.Profile) (bool, error) {
	// Matching uses the same identity rules as the scheduled search: the feed
	// title is mapped to saved targets by episode label or year, then filtered.
	// Automatic grabbing is deliberately narrow here.
	matches := idx.match(ev.ReleaseName)
	eligible := []searchTarget{}
	distinct := map[int64]bool{}
	for _, t := range matches {
		// Reject Anime outright: only IPTorrents movies and ordinary episodic TV
		// are auto-grabbed. Season packs and ranges never reach this list
		// because idx.match returns none for them.
		if t.Anime {
			continue
		}
		// Reject specials (season zero) and any non-positive season, which the
		// parser cannot treat as an ordinary episode.
		if t.TV && t.Season <= 0 {
			continue
		}
		// Never replace registered media or episode files.
		if idx.hasFile(t) {
			continue
		}
		eligible = append(eligible, t)
		distinct[t.MediaID] = true
	}
	if len(eligible) == 0 {
		return false, s.completeAnnounce(ctx, owner, ev, "done", "unmatched", "No monitored missing movie or ordinary single episode matched this announce title.", "", nil)
	}
	if len(distinct) > 1 {
		// A title that resolves to more than one canonical title is ambiguous
		// even when one candidate would score higher: guessing risks the wrong
		// download, so it stays for manual review.
		return false, s.completeAnnounce(ctx, owner, ev, "done", "ambiguous", "This announce title matches more than one saved title, so it was not grabbed automatically.", "", nil)
	}

	// Unknown seeder counts are the norm for an announce. AllowUnknownSeeders
	// lets an announce satisfy a profile that sets a minimum seeder count,
	// because the announce carries no count to check. Scheduled searches always
	// report a real count, so their minimum stays enforced; a count that is
	// reported below the minimum is still rejected.
	release := quality.Release{ID: ev.ReleaseName, Title: ev.ReleaseName, Size: ev.SizeBytes, Protocol: "torrent", Now: s.now().Unix()}
	if ev.ReceivedAt > 0 {
		release.Published = time.Unix(ev.ReceivedAt, 0).UTC().Format(time.RFC3339)
	}
	var best *searchTarget
	var bestProfile quality.Profile
	bestScore, bestQuality := -1, 0
	for i := range eligible {
		t := eligible[i]
		p, ok := profiles[t.ProfileID]
		if !ok {
			var e error
			p, e = store.Get(ctx, t.ProfileID)
			if e != nil {
				return false, e
			}
			profiles[t.ProfileID] = p
		}
		d := quality.EvaluateWithOptions(p, t.Target, release, quality.EvaluateOptions{AllowUnknownSeeders: true})
		if !d.Eligible {
			continue
		}
		if best == nil || d.Score > bestScore || (d.Score == bestScore && d.Quality > bestQuality) {
			copy := t
			best = &copy
			bestProfile = p
			bestScore, bestQuality = d.Score, d.Quality
		}
	}
	if best == nil {
		return false, s.completeAnnounce(ctx, owner, ev, "done", "rejected", "No announce candidate satisfies this title's quality profile, seed, size or identity rules.", "", nil)
	}

	job, created, e := s.workflow.QueueAnnounceAfter(ctx, ev.ID, ev.SourceID, ev.SourceRevision, best.MediaID, best.EpisodeID, best.MovieVersion, bestProfile.GrabDelay(), s.announceGuard(owner, ev, *best, bestProfile))
	if e != nil {
		// A canceled or expired work context means the heartbeat lost the
		// lease, or the application is shutting down. That is never a terminal
		// outcome: leave the event processing so its lease expires and a later
		// pass reclaims and retries it.
		if ctx.Err() != nil || errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
			return false, e
		}
		var typed *transfer.Error
		switch {
		case errors.Is(e, ErrLease) || errors.Is(e, transfer.ErrLease):
			// The guard fenced a policy, source or target change made after the
			// match; finish honestly rather than retrying a stale decision.
			return false, s.completeAnnounce(ctx, owner, ev, "done", "stale_policy", "The saved target, profile or source changed before this announce was grabbed.", "", nil)
		case errors.As(e, &typed) && typed.Temporary:
			// A transient indexer, client or network failure: retry with
			// backoff rather than surfacing a false failure.
			return false, s.retryAnnounce(ctx, owner, ev, transferMessage(e))
		case errors.As(e, &typed) && (typed.Code == "release_expired" || typed.Code == "invalid_release"):
			return false, s.completeAnnounce(ctx, owner, ev, "done", "stale_source", "The announce source changed or automatic grabbing is no longer enabled.", "", nil)
		default:
			return false, s.completeAnnounce(ctx, owner, ev, "done", "needs_attention", transferMessage(e), "", nil)
		}
	}
	outcome := "queued"
	message := "Matching announce queued automatically."
	if !created {
		outcome = "already_tracked"
		message = "This announce already has a download record. Review its state in Activity."
	}
	return created, s.completeAnnounce(ctx, owner, ev, "done", outcome, message, job.ID, nil)
}

// retryAnnounce reschedules a transient failure with bounded exponential
// backoff, or surfaces it for review once the attempt or age bound is reached.
func (s *Service) retryAnnounce(ctx context.Context, owner string, ev announceEvent, message string) error {
	now := s.now().Unix()
	if ev.Attempts >= announceMaxAttempts || (ev.ReceivedAt > 0 && ev.ReceivedAt+announceMaxAge < now) {
		return s.completeAnnounce(ctx, owner, ev, "done", "needs_attention", message+" Automatic grabbing stopped after repeated failures; review the source and download client.", "", nil)
	}
	shift := ev.Attempts - 1
	if shift < 0 {
		shift = 0
	}
	if shift > 6 {
		shift = 6
	}
	delay := announceBackoff * (1 << shift)
	if delay > announceBackoffMax {
		delay = announceBackoffMax
	}
	next := now + delay
	return s.completeAnnounce(ctx, owner, ev, "retry", "retry", message, "", &next)
}

// completeAnnounce resolves one leased event. The update is fenced on the owner
// and a live lease, so a lease that expired while the event was in flight is
// left for the next claim to reclaim instead of being completed here.
func (s *Service) completeAnnounce(ctx context.Context, owner string, ev announceEvent, state, outcome, message, downloadID string, nextRun *int64) error {
	var download any
	if downloadID != "" {
		download = downloadID
	}
	var next any
	if nextRun != nil {
		next = *nextRun
	}
	var processed any
	if state == "done" {
		processed = s.now().Unix()
	}
	res, e := s.db.ExecContext(ctx, `UPDATE announce_events SET processing_state=?,outcome=?,message=?,download_id=?,next_run=?,processed_at=?,lease_owner='',lease_until=0 WHERE id=? AND processing_state='processing' AND lease_owner=? AND lease_until>=?`, state, outcome, message, download, next, processed, ev.ID, owner, s.now().Unix())
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		// The lease expired or was reclaimed by another pass while this event
		// was in flight. Report the lost lease so the caller leaves the event
		// for the new owner instead of assuming a completion that never landed.
		return ErrLease
	}
	return nil
}

// announceGuard revalidates every policy fact inside the reservation
// transaction, immediately before transfer reserves the download. It repeats
// the whole decision the matcher made, on the same connection and in the same
// transaction that writes the reservation, so a change made after the match
// cannot slip a mis-scoped download through.
func (s *Service) announceGuard(owner string, ev announceEvent, t searchTarget, p quality.Profile) transfer.QueueGuard {
	return func(ctx context.Context, tx *sql.Tx, v transfer.Candidate) error {
		now := s.now().Unix()
		var count int
		// The event is still ours and is the exact captured revision.
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM announce_events WHERE id=? AND source_id=? AND source_revision=? AND tracker='iptorrents' AND torrent_id=? AND processing_state='processing' AND lease_owner=? AND lease_until>=?`, ev.ID, ev.SourceID, ev.SourceRevision, ev.TorrentID, owner, now).Scan(&count); e != nil {
			return e
		}
		if count != 1 {
			return ErrLease
		}
		// The source is still enabled, opted in and at the captured revision.
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM announce_sources WHERE id=? AND tracker='iptorrents' AND enabled=1 AND auto_grab=1 AND revision=?`, ev.SourceID, ev.SourceRevision).Scan(&count); e != nil {
			return e
		}
		if count != 1 {
			return ErrLease
		}
		// The release transfer built is the one this event describes.
		if v.Announce == nil || v.Announce.EventID != ev.ID || v.Announce.SourceID != ev.SourceID || v.Announce.SourceRevision != ev.SourceRevision || v.Announce.TorrentID != ev.TorrentID {
			return ErrLease
		}
		// The candidate targets exactly the matched title, never anything else.
		if v.MovieID != t.MediaID {
			return ErrLease
		}
		if t.TV {
			if t.EpisodeID == nil || v.Kind != "series" || len(v.Targets) != 1 || v.Targets[0].EpisodeID != *t.EpisodeID {
				return ErrLease
			}
		} else if v.Kind == "series" {
			return ErrLease
		}
		// Every target revision the match depended on is unchanged.
		date := s.now().Format("2006-01-02")
		current, e := scanTarget(tx.QueryRowContext(ctx, targetSQL+` WHERE key=?`, date, 1, t.Key))
		if errors.Is(e, sql.ErrNoRows) {
			return ErrLease
		}
		if e != nil {
			return e
		}
		if current.VersionRevision != t.VersionRevision || current.ProfileID != t.ProfileID || current.AssignmentRevision != t.AssignmentRevision || current.AnimeRevision != t.AnimeRevision || current.CategoryRevision != t.CategoryRevision || current.NumberingRevision != t.NumberingRevision || current.AnimeSearchTitle != t.AnimeSearchTitle || !reflect.DeepEqual(current.Target, t.Target) {
			return ErrLease
		}
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM quality_profiles WHERE id=? AND revision=?`, p.ID, p.Revision).Scan(&count); e != nil {
			return e
		}
		if count != 1 {
			return ErrLease
		}
		// No registered file appeared for the target while the download was
		// being prepared: registered media is never replaced automatically.
		if t.TV {
			if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM episode_files WHERE episode_id=?`, *t.EpisodeID).Scan(&count); e != nil {
				return e
			}
		} else {
			if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_files WHERE movie_id=? AND (?='' OR video_resolution=0 OR (?='hd' AND video_resolution BETWEEN 1 AND 1080) OR (?='uhd' AND video_resolution=2160))`, t.MediaID, t.MovieVersion, t.MovieVersion, t.MovieVersion).Scan(&count); e != nil {
				return e
			}
		}
		if count != 0 {
			return ErrLease
		}
		// The release still satisfies the profile, with a missing seed count
		// allowed to satisfy the minimum for an announce.
		d := quality.EvaluateWithOptions(p, current.Target, quality.Release{ID: v.ID, Title: v.Title, Size: v.Size, Seeders: v.Seeders, Protocol: v.Protocol, Published: v.Published, Now: s.now().Unix()}, quality.EvaluateOptions{AllowUnknownSeeders: true})
		if !d.Eligible {
			return ErrLease
		}
		return nil
	}
}
