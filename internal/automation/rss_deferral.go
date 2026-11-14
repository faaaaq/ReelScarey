package automation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"time"

	"github.com/faaaaq/ReelScarey/internal/quality"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

// Durable feed deferral. A release for a monitored episode whose listed air
// date has not arrived is rejected by the documented on-or-before-today rule.
// The announce path defers such a release to the air date; the feed path does
// the same. A feed entry is seen once and feed pages rotate, so the deferred
// decision must live in the database: the rss_deferrals row keeps the sealed
// release payload captured from the live entry and is reconsidered at the
// episode's air-date local midnight, bounded exactly like the announce
// deferral (announceMaxAttempts claims, announceDeferHorizon days), with every
// terminal state recorded as an observable outcome.

// deferredFeeds is the optional workflow capability behind the durable feed
// deferral. Keeping it optional leaves existing workflow implementations and
// test doubles unchanged; the real transfer service always provides it.
type deferredFeeds interface {
	PersistDeferredFeed(context.Context, string) ([]byte, error)
	QueueDeferredFeed(context.Context, []byte, int64, int64, time.Duration, transfer.QueueGuard) (transfer.Job, bool, error)
}

const (
	// rssDeferBatch bounds one wake pass so a large backlog cannot stall a
	// single worker turn; later passes continue where it left off.
	rssDeferBatch = 10
	// rssDeferLease is the per-row wake lease; the heartbeat renews it every
	// 15s and an expired lease is reclaimed by the next pass.
	rssDeferLease = int64(60)
)

type rssDeferral struct {
	ID           int64
	IndexerID    string
	IndexerName  string
	ReleaseKey   string
	ReleaseTitle string
	MediaID      int64
	EpisodeID    int64
	TargetKey    string
	Payload      []byte
	Size         int64
	Seeders      *int
	Protocol     string
	PublishedAt  string
	Attempts     int
	CreatedAt    int64
}

// rssDeferLegacyMessage is the honest not_aired record used when the workflow
// does not provide the deferral capability, so the row never overstates what
// will happen.
const rssDeferLegacyMessage = "The release matches a monitored episode whose listed air date has not arrived. Automatic grabbing for that episode begins on its air date."

// deferRSSFeed keeps one eligible early feed release for reconsideration at
// its episode's air date and returns the not_aired message to record. The
// bounds match the announce deferral exactly: a wake beyond the horizon is
// never scheduled, and the message says what to do instead.
func (s *Service) deferRSSFeed(ctx context.Context, src transfer.RecentFeed, item transfer.RecentItem, t searchTarget) (string, error) {
	dq, ok := s.workflow.(deferredFeeds)
	if !ok {
		return rssDeferLegacyMessage, nil
	}
	var airDate string
	e := s.db.QueryRowContext(ctx, `SELECT air_date FROM episodes WHERE id=?`, t.EpisodeID).Scan(&airDate)
	if errors.Is(e, sql.ErrNoRows) {
		return rssDeferLegacyMessage, nil
	}
	if e != nil {
		return "", e
	}
	air, e := time.ParseInLocation("2006-01-02", airDate, s.now().Location())
	if e != nil {
		return rssDeferLegacyMessage, nil
	}
	until := int64(air.Sub(s.now()).Seconds())
	if until < 0 {
		until = 0
	}
	if until > announceDeferHorizon {
		return fmt.Sprintf("The episode airs %s, before which automatic grabbing waits. The release was not kept for a later automatic grab; use a manual or scheduled search on or after the air date.", airDate), nil
	}
	payload, e := dq.PersistDeferredFeed(ctx, item.Token)
	if e != nil {
		var typed *transfer.Error
		if errors.As(e, &typed) && (typed.Code == "release_expired" || typed.Code == "invalid_release") {
			return rssDeferLegacyMessage, nil
		}
		return "", e
	}
	now := s.now().Unix()
	var seeders any
	if item.Seeders != nil {
		seeders = *item.Seeders
	}
	if _, e = s.db.ExecContext(ctx, `INSERT INTO rss_deferrals(indexer_id,indexer_name,release_key,release_title,media_id,episode_id,target_key,air_date,size_bytes,seeders,protocol,published_at,payload,next_run,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(indexer_id,release_key) DO NOTHING`,
		src.IndexerID, src.IndexerName, item.Key, item.Title, t.MediaID, *t.EpisodeID, t.Key, airDate, item.Size, seeders, item.Protocol, item.Published, payload, air.Unix(), now, now); e != nil {
		return "", e
	}
	return fmt.Sprintf("The release matches a monitored episode whose listed air date has not arrived. The release was deferred to %s, when automatic grabbing for this episode begins.", airDate), nil
}

// RunRSSDeferrals is the service-startup worker for due feed deferrals. It
// mirrors RunAnnounces: pump ProcessRSSDeferrals until the context is
// canceled, backing off between passes and logging deferred errors.
func (s *Service) RunRSSDeferrals(ctx context.Context, logger *slog.Logger) {
	for ctx.Err() == nil {
		_, e := s.ProcessRSSDeferrals(ctx)
		if e != nil && ctx.Err() == nil {
			logger.Warn("feed deferral processing deferred", "error", e)
		}
		timer := time.NewTimer(30 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// ProcessRSSDeferrals claims a bounded batch of due deferrals and wakes each
// under a 60s lease that a 15s heartbeat keeps alive, mirroring the announce
// processor. A lost lease leaves the row for the next pass; nothing is
// completed twice because every resolution is fenced on the live lease.
func (s *Service) ProcessRSSDeferrals(ctx context.Context) (bool, error) {
	batch, owner, e := s.claimRSSDeferrals(ctx)
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
				if e := s.heartbeatRSSDeferrals(work, owner); e != nil {
					cancel()
					return
				}
			}
		}
	}()
	// One saved-library index serves the whole batch; the guard revalidates
	// every decision inside the reservation transaction regardless.
	idx, e := s.rssTargets(work)
	if e != nil {
		cancel()
		<-done
		return true, e
	}
	for _, d := range batch {
		if ctx.Err() != nil {
			break
		}
		if e := s.handleRSSDeferral(work, owner, d, idx); e != nil {
			if errors.Is(e, ErrLease) || ctx.Err() != nil {
				continue
			}
			cancel()
			<-done
			return true, e
		}
	}
	cancel()
	<-done
	if ctx.Err() == nil {
		if e := s.pruneRSSDeferrals(context.Background()); e != nil {
			return true, e
		}
	}
	return true, nil
}

func (s *Service) claimRSSDeferrals(ctx context.Context) ([]rssDeferral, string, error) {
	now := s.now().Unix()
	owner := transfer.ID()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, "", e
	}
	defer tx.Rollback()
	// An interrupted wake (crashed worker, lost lease) is reclaimed by
	// clearing its lease; the row stays deferred and the next pass re-claims
	// it. The attempt was already counted by the claim that was interrupted.
	if _, e = tx.ExecContext(ctx, `UPDATE rss_deferrals SET lease_owner='',lease_until=0 WHERE state='deferred' AND lease_until>0 AND lease_until<=?`, now); e != nil {
		return nil, "", e
	}
	rows, e := tx.QueryContext(ctx, `UPDATE rss_deferrals SET lease_owner=?,lease_until=?,attempts=attempts+1 WHERE id IN (
SELECT id FROM rss_deferrals WHERE state='deferred' AND next_run<=? AND lease_until<=? ORDER BY next_run,id LIMIT ?
) RETURNING id,indexer_id,indexer_name,release_key,release_title,media_id,episode_id,target_key,payload,size_bytes,seeders,protocol,published_at,attempts,created_at`, owner, now+rssDeferLease, now, now, rssDeferBatch)
	if e != nil {
		return nil, "", e
	}
	out := []rssDeferral{}
	for rows.Next() {
		var d rssDeferral
		if e = rows.Scan(&d.ID, &d.IndexerID, &d.IndexerName, &d.ReleaseKey, &d.ReleaseTitle, &d.MediaID, &d.EpisodeID, &d.TargetKey, &d.Payload, &d.Size, &d.Seeders, &d.Protocol, &d.PublishedAt, &d.Attempts, &d.CreatedAt); e != nil {
			rows.Close()
			return nil, "", e
		}
		out = append(out, d)
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

func (s *Service) heartbeatRSSDeferrals(ctx context.Context, owner string) error {
	_, e := s.db.ExecContext(ctx, `UPDATE rss_deferrals SET lease_until=? WHERE lease_owner=? AND state='deferred' AND lease_until>=?`, s.now().Unix()+rssDeferLease, owner, s.now().Unix())
	return e
}

func (s *Service) rssDeferralFence(ctx context.Context, owner string, id int64) error {
	var count int
	e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM rss_deferrals WHERE id=? AND state='deferred' AND lease_owner=? AND lease_until>=?`, id, owner, s.now().Unix()).Scan(&count)
	if e != nil {
		return e
	}
	if count != 1 {
		return ErrLease
	}
	return nil
}

// handleRSSDeferral wakes one deferred release. The row always resolves: the
// ordinary feed decision runs again against the saved library, and every
// outcome — queued, rejected, stale, expired or still-not-aired — is recorded
// with its reason.
func (s *Service) handleRSSDeferral(ctx context.Context, owner string, d rssDeferral, idx rssIndex) error {
	if e := s.rssDeferralFence(ctx, owner, d.ID); e != nil {
		// One deferral whose lease was reclaimed must not stall the batch.
		if errors.Is(e, ErrLease) {
			return nil
		}
		return e
	}
	var airDate string
	e := s.db.QueryRowContext(ctx, `SELECT air_date FROM episodes WHERE id=?`, d.EpisodeID).Scan(&airDate)
	if errors.Is(e, sql.ErrNoRows) {
		return s.completeRSSDeferral(ctx, owner, d, "stale", "stale_target", "The episode is no longer in the saved show, so the deferred release was not grabbed.", "")
	} else if e != nil {
		return e
	}
	air, e := time.ParseInLocation("2006-01-02", airDate, s.now().Location())
	if e != nil {
		// Undated or invalid dates never authorize automatic work and never
		// invent an eligibility date.
		return s.completeRSSDeferral(ctx, owner, d, "stale", "stale_target", "The episode no longer has a valid air date, so the deferred release was not grabbed automatically.", "")
	}
	today, e := time.ParseInLocation("2006-01-02", s.now().Format("2006-01-02"), s.now().Location())
	if e != nil {
		return e
	}
	if air.After(today) {
		// The air date moved after the deferral was recorded. Re-defer to the
		// new local midnight — never grab early, never silently drop.
		until := int64(air.Sub(s.now()).Seconds())
		if d.Attempts >= announceMaxAttempts || until > announceDeferHorizon {
			return s.completeRSSDeferral(ctx, owner, d, "expired", "not_aired",
				fmt.Sprintf("The episode airs %s, before which automatic grabbing waits. The deferred release was not kept for a later automatic grab; use a manual or scheduled search on or after the air date.", airDate), "")
		}
		if _, e := s.db.ExecContext(ctx, `UPDATE rss_deferrals SET next_run=?,air_date=?,message=?,lease_owner='',lease_until=0,updated_at=? WHERE id=? AND state='deferred' AND lease_owner=? AND lease_until>=?`,
			air.Unix(), airDate, fmt.Sprintf("The episode now airs %s. The deferred release was rescheduled to that date.", airDate), s.now().Unix(), d.ID, owner, s.now().Unix()); e != nil {
			return e
		}
		return nil
	}
	// The episode is eligible today. Re-run the whole feed decision against
	// the saved library before any queueing, exactly like the live check: the
	// same identity filters, the same registered-file protection and the same
	// saved profile.
	var t searchTarget
	matched := false
	for _, m := range idx.match(d.ReleaseTitle) {
		if m.EpisodeID != nil && *m.EpisodeID == d.EpisodeID && !m.Anime && m.Season > 0 && !idx.hasFile(m) {
			t = m
			matched = true
			break
		}
	}
	if !matched {
		return s.completeRSSDeferral(ctx, owner, d, "stale", "stale_target", "The saved target changed before the deferred release became eligible, so it was not grabbed.", "")
	}
	p, e := quality.New(s.db).Get(ctx, t.ProfileID)
	if e != nil {
		return e
	}
	dv := quality.Evaluate(p, t.Target, quality.Release{ID: d.ReleaseKey, Title: d.ReleaseTitle, Size: d.Size, Seeders: d.Seeders, Protocol: d.Protocol, Published: d.PublishedAt, Now: s.now().Unix()})
	if !dv.Eligible {
		return s.completeRSSDeferral(ctx, owner, d, "rejected", "rejected", "The deferred release no longer satisfies this title's quality profile, seed, size or identity rules.", "")
	}
	dq, ok := s.workflow.(deferredFeeds)
	if !ok {
		return s.completeRSSDeferral(ctx, owner, d, "stale", "stale_target", "Download queueing is unavailable, so the deferred release was not grabbed.", "")
	}
	job, created, e := dq.QueueDeferredFeed(ctx, d.Payload, d.MediaID, d.EpisodeID, p.GrabDelay(), s.rssDeferralGuard(d.ID, owner, t, p))
	if e != nil {
		// A canceled work context means the heartbeat lost the lease or the
		// application is shutting down: leave the row leased so a later pass
		// reclaims and retries it.
		if ctx.Err() != nil || errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
			return e
		}
		var typed *transfer.Error
		switch {
		case errors.Is(e, ErrLease) || errors.Is(e, transfer.ErrLease):
			return s.completeRSSDeferral(ctx, owner, d, "stale", "stale_policy", "The saved target, profile or monitoring changed before the deferred release was grabbed.", "")
		case errors.As(e, &typed) && typed.Temporary:
			// A transient indexer, client or network failure: retry with the
			// same bounded backoff as the announce path.
			if d.Attempts >= announceMaxAttempts {
				return s.completeRSSDeferral(ctx, owner, d, "expired", "needs_attention", transferMessage(e)+" Automatic grabbing stopped after repeated failures; review the source and download client.", "")
			}
			shift := d.Attempts - 1
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
			if _, e := s.db.ExecContext(ctx, `UPDATE rss_deferrals SET next_run=?,message=?,lease_owner='',lease_until=0,updated_at=? WHERE id=? AND state='deferred' AND lease_owner=? AND lease_until>=?`,
				s.now().Unix()+delay, transferMessage(e)+" The grab will be retried.", s.now().Unix(), d.ID, owner, s.now().Unix()); e != nil {
				return e
			}
			return nil
		case errors.As(e, &typed) && (typed.Code == "release_expired" || typed.Code == "invalid_release"):
			return s.completeRSSDeferral(ctx, owner, d, "expired", "stale_source", "The release source changed or its saved selection expired before the deferred release was grabbed.", "")
		default:
			return s.completeRSSDeferral(ctx, owner, d, "expired", "needs_attention", transferMessage(e), "")
		}
	}
	outcome, message := "queued", "The deferred release was grabbed automatically on its air date."
	if !created {
		outcome, message = "already_tracked", "This deferred release already has a download record. Review its state in Activity."
	}
	return s.completeRSSDeferral(ctx, owner, d, "queued", outcome, message, job.ID)
}

// completeRSSDeferral resolves one leased deferral. The update is fenced on
// the owner and a live lease, so a lease that expired mid-flight is left for
// the next claim instead of being completed twice.
func (s *Service) completeRSSDeferral(ctx context.Context, owner string, d rssDeferral, state, outcome, message, downloadID string) error {
	var dl any
	if downloadID != "" {
		dl = downloadID
	}
	res, e := s.db.ExecContext(ctx, `UPDATE rss_deferrals SET state=?,outcome=?,message=?,download_id=?,lease_owner='',lease_until=0,updated_at=? WHERE id=? AND state='deferred' AND lease_owner=? AND lease_until>=?`,
		state, outcome, message, dl, s.now().Unix(), d.ID, owner, s.now().Unix())
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return ErrLease
	}
	// Surface the wake outcome in the same feed log the live check writes, so
	// the deferral's fate stays observable next to the original not_aired row.
	var media, episode, download any
	media = d.MediaID
	episode = d.EpisodeID
	if downloadID != "" {
		download = downloadID
	}
	_, e = s.db.ExecContext(ctx, `INSERT INTO rss_results(run_id,indexer_id,indexer_name,media_id,episode_id,target,release_title,outcome,message,download_id,created_at) VALUES('',?,?,?,?,'',?,?,?,?,?)`,
		d.IndexerID, d.IndexerName, media, episode, d.ReleaseTitle, outcome, message, download, s.now().Unix())
	return e
}

// pruneRSSDeferrals bounds the deferral table: resolved rows are kept for a
// week of observability, and the table itself can never grow past 500 rows.
func (s *Service) pruneRSSDeferrals(ctx context.Context) error {
	if _, e := s.db.ExecContext(ctx, `DELETE FROM rss_deferrals WHERE state<>'deferred' AND updated_at<?`, s.now().Unix()-7*86400); e != nil {
		return e
	}
	_, e := s.db.ExecContext(ctx, `DELETE FROM rss_deferrals WHERE id NOT IN (SELECT id FROM rss_deferrals ORDER BY id DESC LIMIT 500)`)
	return e
}

// rssDeferralGuard revalidates every policy fact inside the reservation
// transaction, immediately before transfer reserves the download — the same
// fence the live feed queue uses, with the deferral row's live lease replacing
// the feed-check lease. A change made after the wake match cannot slip a
// mis-scoped download through.
func (s *Service) rssDeferralGuard(id int64, owner string, t searchTarget, p quality.Profile) transfer.QueueGuard {
	return func(ctx context.Context, tx *sql.Tx, v transfer.Candidate) error {
		now := s.now().Unix()
		var count int
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM rss_deferrals WHERE id=? AND state='deferred' AND lease_owner=? AND lease_until>=?`, id, owner, now).Scan(&count); e != nil {
			return e
		}
		if count != 1 {
			return ErrLease
		}
		// Every target revision the wake match depended on is unchanged, and
		// the episode is still eligible on today's date.
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
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM quality_profiles WHERE id=? AND revision=?`, p.ID, p.Revision).Scan(&count); e != nil {
			return e
		}
		if count != 1 {
			return ErrLease
		}
		if v.MovieID != t.MediaID || v.Kind != "series" || len(v.Targets) != 1 || v.Targets[0].EpisodeID != *t.EpisodeID {
			return ErrLease
		}
		// Registered media is never replaced automatically, at deferral time
		// or at wake time.
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM episode_files WHERE episode_id=?`, *t.EpisodeID).Scan(&count); e != nil {
			return e
		}
		if count != 0 {
			return ErrLease
		}
		d := quality.Evaluate(p, current.Target, quality.Release{ID: v.ID, Title: v.Title, Size: v.Size, Seeders: v.Seeders, Protocol: v.Protocol, Published: v.Published, Now: s.now().Unix()})
		if !d.Eligible {
			return ErrLease
		}
		return nil
	}
}
