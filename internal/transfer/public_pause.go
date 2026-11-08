package transfer

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"time"
)

// The auto-pause is an application-enforced option for the supervised built-in
// client, like the seeding limit. When enabled, a torrent that finished
// downloading and came from a public source is paused shortly after completion
// instead of seeding indefinitely. Public sources are the public Nyaa indexer
// and manually uploaded torrents; private tracker and indexer downloads keep
// seeding, and torrents added to the client outside ReelScarey are left alone.
const publicPauseEventState = "public source finished: paused"

// PublicPause is the saved auto-pause option for the built-in client.
type PublicPause struct {
	Enabled   bool  `json:"enabled"`
	Revision  int   `json:"revision"`
	UpdatedAt int64 `json:"updated_at"`
}

// PublicPauseUpdate replaces the saved option. It uses the same shape as the
// read, and the revision fences concurrent editors. updated_at is reported by
// the server and ignored on write.
type PublicPauseUpdate struct {
	Enabled   bool  `json:"enabled"`
	Revision  int   `json:"revision"`
	UpdatedAt int64 `json:"updated_at"`
}

func (s *Service) PublicPause(ctx context.Context) (PublicPause, error) {
	var out PublicPause
	var enabled int
	err := s.db.QueryRowContext(ctx, `SELECT enabled,revision,updated_at FROM torrent_public_pause WHERE id=1`).
		Scan(&enabled, &out.Revision, &out.UpdatedAt)
	if err != nil {
		return out, err
	}
	out.Enabled = enabled == 1
	return out, nil
}

// SavePublicPause replaces the single saved option under a revision check.
func (s *Service) SavePublicPause(ctx context.Context, in PublicPauseUpdate) (PublicPause, error) {
	if in.Revision < 1 {
		return PublicPause{}, problem("revision_conflict", "Reload the auto-pause option before saving.")
	}
	out, err := s.db.ExecContext(ctx, `UPDATE torrent_public_pause SET enabled=?,revision=revision+1,updated_at=? WHERE id=1 AND revision=?`,
		in.Enabled, time.Now().Unix(), in.Revision)
	if err != nil {
		return PublicPause{}, err
	}
	if n, _ := out.RowsAffected(); n != 1 {
		return PublicPause{}, problem("revision_conflict", "The auto-pause option changed. Reload it before saving.")
	}
	return s.PublicPause(ctx)
}

// publicJobSource reports whether a finished torrent bound to a ReelScarey job
// came from a public source. The queue recorded the classification when the
// job was created; a hash without such a record is never auto-paused.
func (s *Service) publicJobSource(ctx context.Context, hash string) (bool, error) {
	var public int
	err := s.db.QueryRowContext(ctx, `SELECT indexer_public FROM downloads WHERE lower(info_hash)=?`, hash).Scan(&public)
	if err == nil {
		return public == 1, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT indexer_public FROM music_downloads WHERE lower(info_hash)=?`, hash).Scan(&public)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return public == 1, err
}

func (s *Service) recordPublicPauseEvent(ctx context.Context, hash, title string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'download',?,?,?)`,
		hash, cleanClientText(title, shareEventTitleMax), publicPauseEventState)
	return err
}

// RunPublicPause evaluates the saved option once and pauses every finished
// public-source torrent. It is the supervised pass; a failed evaluation is
// returned so a manual caller sees why nothing happened.
func (s *Service) RunPublicPause(ctx context.Context) (int, error) {
	return s.evaluatePublicPause(ctx, nil)
}

func (s *Service) tickPublicPause(ctx context.Context, logger *slog.Logger) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.Error("public torrent pause check panicked", "error", recovered)
		}
	}()
	paused, err := s.evaluatePublicPause(ctx, logger)
	if err != nil && ctx.Err() == nil {
		logger.Warn("public torrent pause check failed", "error", err)
		return
	}
	if paused > 0 && logger != nil {
		logger.Info("paused finished public torrents", "count", paused)
	}
}

func (s *Service) evaluatePublicPause(ctx context.Context, logger *slog.Logger) (int, error) {
	// shareMu serializes the seeding-limit and public-pause passes so their
	// dedupe cache stays consistent.
	s.shareMu.Lock()
	defer s.shareMu.Unlock()
	if s.shareHandledSet == nil {
		s.shareHandledSet = map[string]bool{}
	}
	policy, err := s.PublicPause(ctx)
	if err != nil || !policy.Enabled {
		return 0, err
	}
	manager, c, err := s.builtinTorrents(ctx)
	if err != nil {
		var clientErr *Error
		if errors.As(err, &clientErr) && clientErr.Code == "builtin_unavailable" {
			// The client is disabled or not configured. The supervised pass
			// stays quiet; a manual run tells the administrator why.
			if logger == nil {
				return 0, err
			}
			return 0, nil
		}
		return 0, err
	}
	items, err := manager.List(ctx, c)
	if err != nil {
		return 0, err
	}
	paused := 0
	var failures error
	for _, item := range items {
		if item.Progress < 1 || item.AmountLeft != 0 {
			continue
		}
		classifyTorrent(&item)
		hash := strings.ToLower(item.Hash)
		if !shareHashOK(hash) {
			continue
		}
		// qBittorrent names the finished-paused state pausedUP or stoppedUP
		// depending on the version. Either way the torrent is already stopped.
		if strings.HasPrefix(item.State, "paused") || strings.HasPrefix(item.State, "stopped") {
			continue
		}
		public := false
		switch item.Origin {
		case torrentOriginOwner:
			public = true
		case torrentOriginJob:
			public, err = s.publicJobSource(ctx, hash)
			if err != nil {
				failures = errors.Join(failures, err)
				continue
			}
		default:
			// A torrent added to the client outside ReelScarey has no recorded
			// source, so it is never auto-paused.
			continue
		}
		if !public {
			continue
		}
		// A torrent the owner resumed and that finished again produces a new
		// event; an already-handled hash is never paused twice in one pass.
		handled, err := s.shareHandled(ctx, hash, publicPauseEventState)
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		if handled {
			continue
		}
		if err = manager.Pause(ctx, c, []string{hash}); err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		paused++
		if err = s.recordPublicPauseEvent(ctx, hash, item.Name); err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		if len(s.shareHandledSet) >= shareHandledLimit {
			// The durable activity row remains the source of truth; the cache
			// only avoids repeated reads during one process lifetime.
			s.shareHandledSet = map[string]bool{}
		}
		s.shareHandledSet[hash+"|"+publicPauseEventState] = true
	}
	return paused, failures
}
