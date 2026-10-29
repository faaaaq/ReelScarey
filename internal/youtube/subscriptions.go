package youtube

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/faaaaq/ReelScarey/internal/organization"
)

type SourceInspector interface {
	Inspect(context.Context, string) (Inspection, error)
}
type Subscription struct {
	ID              string          `json:"id"`
	URL             string          `json:"source_url"`
	Title           string          `json:"title"`
	CollectionID    int64           `json:"collection_id"`
	Enabled         bool            `json:"enabled"`
	Mode            string          `json:"mode"`
	Interval        int             `json:"interval_hours"`
	Subtitles       DownloadOptions `json:"subtitles"`
	Initialized     bool            `json:"initialized"`
	Revision        int             `json:"revision"`
	Requested       bool            `json:"requested"`
	NextRun         int64           `json:"next_run"`
	LastChecked     int64           `json:"last_checked"`
	State           string          `json:"state"`
	Message         string          `json:"message"`
	Created         int64           `json:"created_at"`
	snapshot, owner string
}
type SubscriptionUpdate struct {
	ID           string          `json:"id"`
	URL          string          `json:"source_url"`
	Title        string          `json:"title"`
	CollectionID int64           `json:"collection_id"`
	Enabled      bool            `json:"enabled"`
	Mode         string          `json:"mode"`
	Interval     int             `json:"interval_hours"`
	Subtitles    DownloadOptions `json:"subtitles"`
	Revision     int             `json:"revision"`
	Reviewed     bool            `json:"reviewed"`
}

const subscriptionColumns = `id,source_url,title,collection_id,enabled,mode,interval_hours,subtitles_json,initialized,revision,requested,next_run,last_checked,state,message,created_at,target_snapshot,lease_owner`

func scanSubscription(row interface{ Scan(...any) error }) (Subscription, error) {
	var s Subscription
	var options string
	e := row.Scan(&s.ID, &s.URL, &s.Title, &s.CollectionID, &s.Enabled, &s.Mode, &s.Interval, &options, &s.Initialized, &s.Revision, &s.Requested, &s.NextRun, &s.LastChecked, &s.State, &s.Message, &s.Created, &s.snapshot, &s.owner)
	if e == nil {
		e = json.Unmarshal([]byte(options), &s.Subtitles)
	}
	return s, e
}
func (s *Store) Subscription(ctx context.Context, key string) (Subscription, error) {
	return scanSubscription(s.db.QueryRowContext(ctx, `SELECT `+subscriptionColumns+` FROM youtube_subscriptions WHERE id=?`, key))
}
func (s *Store) Subscriptions(ctx context.Context) ([]Subscription, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT `+subscriptionColumns+` FROM youtube_subscriptions ORDER BY created_at,id LIMIT 100`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Subscription{}
	for rows.Next() {
		v, e := scanSubscription(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) SaveSubscription(ctx context.Context, in SubscriptionUpdate) (Subscription, error) {
	source, e := NormalizeURL(in.URL)
	if e != nil || (source.Kind != "channel" && source.Kind != "playlist") || !textOK(in.Title, 200) || in.Interval < 1 || in.Interval > 168 || (in.Mode != "review" && in.Mode != "acquire") || !validSubtitleOptions(in.Subtitles) {
		return Subscription{}, ErrInput
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return Subscription{}, e
	}
	defer tx.Rollback()
	snap, _, e := targetSnapshot(ctx, tx, Target{Kind: "custom", CollectionID: in.CollectionID})
	if e != nil {
		return Subscription{}, e
	}
	options, _ := json.Marshal(in.Subtitles)
	if in.ID == "" {
		if in.Enabled || in.Mode != "review" {
			return Subscription{}, ErrInput
		}
		var count int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM youtube_subscriptions`).Scan(&count); e != nil {
			return Subscription{}, e
		}
		if count >= 100 {
			return Subscription{}, ErrBusy
		}
		in.ID, e = id()
		if e != nil {
			return Subscription{}, e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO youtube_subscriptions(id,source_url,title,collection_id,interval_hours,subtitles_json,target_snapshot,created_at) VALUES(?,?,?,?,?,?,?,?)`, in.ID, source.URL, in.Title, in.CollectionID, in.Interval, string(options), snap, time.Now().Unix())
	} else {
		old, e := scanSubscription(tx.QueryRowContext(ctx, `SELECT `+subscriptionColumns+` FROM youtube_subscriptions WHERE id=?`, in.ID))
		if e != nil {
			return Subscription{}, e
		}
		if old.Revision != in.Revision || old.URL != source.URL || old.CollectionID != in.CollectionID {
			return Subscription{}, ErrChanged
		}
		if in.Mode == "acquire" && (!old.Initialized || !in.Reviewed) {
			return Subscription{}, ErrChanged
		}
		if in.Mode == "acquire" && (old.Mode != "acquire" || (!old.Enabled && in.Enabled)) {
			if _, e = tx.ExecContext(ctx, `UPDATE youtube_subscription_entries SET state='baseline' WHERE subscription_id=? AND state='new'`, in.ID); e != nil {
				return Subscription{}, e
			}
		}
		r, err := tx.ExecContext(ctx, `UPDATE youtube_subscriptions SET title=?,enabled=?,mode=?,interval_hours=?,subtitles_json=?,target_snapshot=?,revision=revision+1,next_run=0,requested=0,lease_owner='',lease_until=0,state=CASE WHEN ? THEN 'scheduled' ELSE 'paused' END,message='Subscription settings saved.' WHERE id=? AND revision=?`, in.Title, in.Enabled, in.Mode, in.Interval, string(options), snap, in.Enabled, in.ID, in.Revision)
		e = err
		if e == nil {
			n, _ := r.RowsAffected()
			if n != 1 {
				e = ErrChanged
			}
		}
	}
	if e != nil {
		return Subscription{}, e
	}
	if e = tx.Commit(); e != nil {
		return Subscription{}, e
	}
	return s.Subscription(ctx, in.ID)
}
func (s *Store) RequestSubscription(ctx context.Context, key string, revision int) error {
	r, e := s.db.ExecContext(ctx, `UPDATE youtube_subscriptions SET requested=1,next_run=0,state='queued',message='Source check requested.' WHERE id=? AND revision=?`, key, revision)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrChanged
	}
	return nil
}

func (s *Store) DeleteSubscription(ctx context.Context, key string, revision int) error {
	r, e := s.db.ExecContext(ctx, `DELETE FROM youtube_subscriptions WHERE id=? AND revision=?`, key, revision)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrChanged
	}
	return nil
}

type SubscriptionEntry struct {
	SubscriptionID string `json:"subscription_id"`
	Video
	State      string `json:"state"`
	ReviewID   string `json:"review_id"`
	JobID      string `json:"job_id"`
	Discovered int64  `json:"discovered_at"`
}

func (s *Store) SubscriptionEntries(ctx context.Context, key string) ([]SubscriptionEntry, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT subscription_id,video_id,title,channel,state,COALESCE(review_id,''),COALESCE(job_id,''),discovered_at FROM youtube_subscription_entries WHERE subscription_id=? ORDER BY discovered_at DESC,video_id LIMIT 200`, key)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []SubscriptionEntry{}
	for rows.Next() {
		var v SubscriptionEntry
		if e = rows.Scan(&v.SubscriptionID, &v.Video.ID, &v.Title, &v.Channel, &v.State, &v.ReviewID, &v.JobID, &v.Discovered); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) RunSubscriptions(ctx context.Context, inspector SourceInspector, logger *slog.Logger) {
	for ctx.Err() == nil {
		worked, e := s.ProcessSubscription(ctx, inspector)
		if e != nil && ctx.Err() == nil {
			logger.Warn("YouTube subscription deferred", "error", e)
		}
		if worked && e == nil {
			continue
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
func (s *Store) ProcessSubscription(ctx context.Context, inspector SourceInspector) (processed bool, retErr error) {
	if inspector == nil {
		return false, nil
	}
	owner, e := id()
	if e != nil {
		return false, e
	}
	now := time.Now().Unix()
	p, e := scanSubscription(s.db.QueryRowContext(ctx, `UPDATE youtube_subscriptions SET lease_owner=?,lease_until=?,state='checking' WHERE id=(SELECT id FROM youtube_subscriptions WHERE lease_until<? AND (requested=1 OR (enabled=1 AND next_run<=?)) ORDER BY next_run,created_at,id LIMIT 1) RETURNING `+subscriptionColumns, owner, now+90, now, now))
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	defer func() {
		if retErr != nil {
			finish, c := context.WithTimeout(context.Background(), 3*time.Second)
			defer c()
			_, _ = s.db.ExecContext(finish, `UPDATE youtube_subscriptions SET state='attention',message='Source or target changed, or the check failed. Review the subscription before retrying.',next_run=?,requested=0,lease_owner='',lease_until=0 WHERE id=? AND revision=? AND lease_owner=?`, time.Now().Unix()+3600, p.ID, p.Revision, owner)
		}
	}()
	work, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	inspection, e := inspector.Inspect(work, p.URL)
	if e != nil {
		finish, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		_, _ = s.db.ExecContext(finish, `UPDATE youtube_subscriptions SET state='attention',message='Source check failed; previous entries were preserved.',next_run=?,requested=0,lease_owner='',lease_until=0 WHERE id=? AND revision=? AND lease_owner=?`, now+3600, p.ID, p.Revision, owner)
		return true, e
	}
	normalized, e := NormalizeURL(p.URL)
	if e != nil || inspection.Source != normalized {
		return true, ErrInspection
	}
	tx, e := s.db.BeginTx(work, nil)
	if e != nil {
		return true, e
	}
	defer tx.Rollback()
	var valid int
	if e = tx.QueryRowContext(work, `SELECT COUNT(*) FROM youtube_subscriptions WHERE id=? AND revision=? AND lease_owner=? AND (enabled=1 OR requested=1)`, p.ID, p.Revision, owner).Scan(&valid); e != nil {
		return true, e
	}
	if valid != 1 {
		return true, ErrChanged
	}
	current, _, e := targetSnapshot(work, tx, Target{Kind: "custom", CollectionID: p.CollectionID})
	if e != nil {
		return true, e
	}
	if current != p.snapshot {
		return true, ErrChanged
	}
	seen := map[string]bool{}
	for _, entry := range inspection.Entries {
		if entry.Kind != "video" || seen[entry.ID] || !storeVideoID.MatchString(entry.ID) || !textOK(entry.Title, 500) {
			continue
		}
		seen[entry.ID] = true
		state := "new"
		if !p.Initialized {
			state = "baseline"
		}
		if _, e = tx.ExecContext(work, `INSERT INTO youtube_subscription_entries(subscription_id,video_id,title,channel,state,discovered_at) VALUES(?,?,?,?,?,?) ON CONFLICT(subscription_id,video_id) DO UPDATE SET title=excluded.title,channel=excluded.channel`, p.ID, entry.ID, entry.Title, entry.Channel, state, now); e != nil {
			return true, e
		}
	}
	message := "Source checked; new videos await review. Existing videos were recorded as the initial baseline."
	if p.Initialized && p.Mode == "acquire" && p.Enabled {
		if e = organization.EnsureMediaAvailableTx(work, tx); e != nil {
			return true, e
		}
		rows, e := tx.QueryContext(work, `SELECT video_id,title,channel FROM youtube_subscription_entries WHERE subscription_id=? AND state='new' ORDER BY discovered_at,video_id LIMIT 25`, p.ID)
		if e != nil {
			return true, e
		}
		videos := []Video{}
		for rows.Next() {
			var v Video
			if e = rows.Scan(&v.ID, &v.Title, &v.Channel); e != nil {
				break
			}
			videos = append(videos, v)
		}
		e = errors.Join(e, rows.Err(), rows.Close())
		if e != nil {
			return true, e
		}
		for _, v := range videos {
			var existing string
			e = tx.QueryRowContext(work, `SELECT id FROM youtube_jobs WHERE collection_id=? AND video_id=? AND state NOT IN ('cancelled','failed') LIMIT 1`, p.CollectionID, v.ID).Scan(&existing)
			if e == nil {
				_, e = tx.ExecContext(work, `UPDATE youtube_subscription_entries SET state='queued',job_id=? WHERE subscription_id=? AND video_id=?`, existing, p.ID, v.ID)
				if e != nil {
					return true, e
				}
				continue
			}
			if e != sql.ErrNoRows {
				return true, e
			}
			reviewID, e := id()
			if e != nil {
				return true, e
			}
			_, e = tx.ExecContext(work, `INSERT INTO youtube_reviews(id,video_id,video_title,channel,target_kind,collection_id,target_snapshot,expires_at) VALUES(?,?,?,?,'custom',?,?,?)`, reviewID, v.ID, v.Title, v.Channel, p.CollectionID, current, now+1800)
			if e != nil {
				return true, e
			}
			langs, _ := json.Marshal(p.Subtitles.Languages)
			if _, e = tx.ExecContext(work, `INSERT INTO youtube_subtitle_options(review_id,languages_json,automatic) VALUES(?,?,?)`, reviewID, string(langs), p.Subtitles.Automatic); e != nil {
				return true, e
			}
			job, e := s.queueTx(work, tx, reviewID)
			if e != nil {
				return true, e
			}
			if _, e = tx.ExecContext(work, `UPDATE youtube_subscription_entries SET state='queued',review_id=?,job_id=? WHERE subscription_id=? AND video_id=?`, reviewID, job.ID, p.ID, v.ID); e != nil {
				return true, e
			}
		}
		message = "New videos acquired in batches of 25; verified output awaits import review."
	}
	if inspection.Truncated {
		message += " Only the latest 100 source entries were inspected; older uploads may be outside this window."
	}
	next := now + int64(p.Interval)*3600
	if p.Initialized && p.Mode == "acquire" && p.Enabled {
		var pending int
		if e = tx.QueryRowContext(work, `SELECT COUNT(*) FROM youtube_subscription_entries WHERE subscription_id=? AND state='new'`, p.ID).Scan(&pending); e != nil {
			return true, e
		}
		if pending > 0 {
			next = now + 30
		}
	}
	_, e = tx.ExecContext(work, `UPDATE youtube_subscriptions SET initialized=1,requested=0,next_run=?,last_checked=?,state='ready',message=?,lease_owner='',lease_until=0 WHERE id=? AND revision=? AND lease_owner=?`, next, now, message, p.ID, p.Revision, owner)
	if e != nil {
		return true, e
	}
	return true, tx.Commit()
}
