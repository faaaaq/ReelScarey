package transfer

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"strings"
	"time"
)

// The automatic seeding limit is an application-enforced policy for the
// supervised built-in client. qBittorrent pauses on its own share limits but
// never removes torrents or data, so a supervised ticker evaluates the client's
// reported ratio and seeding time itself.
const (
	shareActionPause           = "pause"
	shareActionRemove          = "remove"
	shareActionRemoveAndDelete = "remove_and_delete"

	minSharePolicyRatio   = 0.1
	maxSharePolicyRatio   = 1000
	maxSharePolicyMinutes = 525600
	shareEventStatePrefix = "seeding limit reached: "
	shareSeedingTick      = 60 * time.Second
	shareEventTitleMax    = 500
	shareHandledLimit     = 10000
)

// SharePolicy is the saved seeding limit. Zero disables an individual rule.
type SharePolicy struct {
	Enabled           bool    `json:"enabled"`
	Action            string  `json:"action"`
	MaxRatio          float64 `json:"max_ratio"`
	MaxSeedingMinutes int64   `json:"max_seeding_minutes"`
	IncludeReelscarey bool    `json:"include_reelscarey"`
	Revision          int     `json:"revision"`
	UpdatedAt         int64   `json:"updated_at"`
}

// SharePolicyUpdate replaces the saved policy. It uses the same shape as the
// read, and the revision fences concurrent editors. updated_at is reported by
// the server and ignored on write.
type SharePolicyUpdate struct {
	Enabled           bool    `json:"enabled"`
	Action            string  `json:"action"`
	MaxRatio          float64 `json:"max_ratio"`
	MaxSeedingMinutes int64   `json:"max_seeding_minutes"`
	IncludeReelscarey bool    `json:"include_reelscarey"`
	Revision          int     `json:"revision"`
	UpdatedAt         int64   `json:"updated_at"`
}

// SharePolicyRun reports one enforcement pass: how many completed torrents were
// evaluated against the rules, and how many were acted on. A disabled policy
// leaves both at zero.
type SharePolicyRun struct {
	Checked int `json:"checked"`
	Acted   int `json:"acted"`
}

func (s *Service) SharePolicy(ctx context.Context) (SharePolicy, error) {
	var out SharePolicy
	var enabled, include int
	err := s.db.QueryRowContext(ctx, `SELECT enabled,action,max_ratio,max_seeding_minutes,include_reelscarey,revision,updated_at FROM torrent_share_policy WHERE id=1`).
		Scan(&enabled, &out.Action, &out.MaxRatio, &out.MaxSeedingMinutes, &include, &out.Revision, &out.UpdatedAt)
	if err != nil {
		return out, err
	}
	out.Enabled, out.IncludeReelscarey = enabled == 1, include == 1
	return out, nil
}

func validateSharePolicy(in SharePolicyUpdate) error {
	if in.Action != shareActionPause && in.Action != shareActionRemove && in.Action != shareActionRemoveAndDelete {
		return problem("invalid_selection", "Choose pause, remove torrent or remove torrent and delete files.")
	}
	r := in.MaxRatio
	if math.IsNaN(r) || math.IsInf(r, 0) || (r != 0 && (r < minSharePolicyRatio || r > maxSharePolicyRatio)) {
		return problem("invalid_selection", "Enter a ratio between 0.1 and 1000, or 0 to disable the ratio rule.")
	}
	if in.MaxSeedingMinutes < 0 || in.MaxSeedingMinutes > maxSharePolicyMinutes {
		return problem("invalid_selection", "Enter a seeding time between 0 and 525600 minutes (0 disables the time rule).")
	}
	if in.Enabled && r == 0 && in.MaxSeedingMinutes == 0 {
		return problem("invalid_selection", "An enabled seeding limit needs a ratio, a seeding time, or both.")
	}
	if in.Revision < 1 {
		return problem("revision_conflict", "Reload the seeding policy before saving.")
	}
	return nil
}

// SaveSharePolicy replaces the single saved policy under a revision check.
func (s *Service) SaveSharePolicy(ctx context.Context, in SharePolicyUpdate) (SharePolicy, error) {
	s.shareMu.Lock()
	defer s.shareMu.Unlock()
	if err := validateSharePolicy(in); err != nil {
		return SharePolicy{}, err
	}
	out, err := s.db.ExecContext(ctx, `UPDATE torrent_share_policy SET enabled=?,action=?,max_ratio=?,max_seeding_minutes=?,include_reelscarey=?,revision=revision+1,updated_at=? WHERE id=1 AND revision=?`,
		in.Enabled, in.Action, in.MaxRatio, in.MaxSeedingMinutes, in.IncludeReelscarey, time.Now().Unix(), in.Revision)
	if err != nil {
		return SharePolicy{}, err
	}
	if n, _ := out.RowsAffected(); n != 1 {
		return SharePolicy{}, problem("revision_conflict", "The seeding policy changed. Reload it before saving.")
	}
	return s.SharePolicy(ctx)
}

// shareLimitReached applies the configured rules to one completed torrent. The
// client's reported ratio and seeding time are authoritative.
func shareLimitReached(t Torrent, p SharePolicy) bool {
	if !p.Enabled {
		return false
	}
	if p.MaxRatio > 0 && t.Ratio >= p.MaxRatio {
		return true
	}
	if p.MaxSeedingMinutes > 0 && t.SeedingTime >= p.MaxSeedingMinutes*60 {
		return true
	}
	return false
}

// effectiveShareAction never deletes data for a ReelScarey download: the
// reviewed import and the seeding source must survive, so remove-and-delete
// degrades to removing the torrent entry only.
func effectiveShareAction(t Torrent, action string) string {
	if action == shareActionRemoveAndDelete && t.Origin == torrentOriginJob {
		return shareActionRemove
	}
	return action
}

func shareEventState(action string) string { return shareEventStatePrefix + action }

// shareHashOK accepts the v1 (40 hex) and v2 (64 hex) info hashes that the
// client reports. The value is sent back to the client, so separators and other
// unexpected characters are refused instead of being forwarded.
func shareHashOK(hash string) bool {
	if len(hash) != 40 && len(hash) != 64 {
		return false
	}
	for _, c := range hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// shareHandled reports whether this exact hash/action pair already produced a
// History entry. The in-memory set bounds repeated database reads during one
// process lifetime; the durable row survives restarts.
func (s *Service) shareHandled(ctx context.Context, hash, state string) (bool, error) {
	key := hash + "|" + state
	if s.shareHandledSet[key] {
		return true, nil
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM activity_events WHERE kind='download' AND object_id=? AND state=?)`, hash, state).Scan(&exists); err != nil {
		return false, err
	}
	if exists == 1 {
		s.shareHandledSet[key] = true
		return true, nil
	}
	return false, nil
}

func (s *Service) recordShareEvent(ctx context.Context, hash, title, action string) error {
	state := shareEventState(action)
	_, err := s.db.ExecContext(ctx, `INSERT INTO activity_events(occurred_at,kind,object_id,title,state)
SELECT unixepoch(),'download',?,?,?
WHERE NOT EXISTS (SELECT 1 FROM activity_events WHERE kind='download' AND object_id=? AND state=?)`,
		hash, cleanClientText(title, shareEventTitleMax), state, hash, state)
	return err
}

// activeDownloadHashes lists torrents bound to a ReelScarey job that has not
// finished. Those jobs still need their client data, so the policy leaves them
// alone entirely.
func (s *Service) activeDownloadHashes(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT lower(info_hash) FROM downloads WHERE protocol='torrent' AND state NOT IN ('completed','cancelled')
		UNION SELECT lower(info_hash) FROM music_downloads WHERE protocol='torrent' AND state NOT IN ('completed','cancelled')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, err
		}
		out[hash] = true
	}
	return out, rows.Err()
}

// RunSharePolicy evaluates the saved policy once and applies it. It is the
// manual "Run now" action; the supervised ticker uses the same evaluation but
// logs per-torrent failures instead of returning them.
func (s *Service) RunSharePolicy(ctx context.Context) (SharePolicyRun, error) {
	return s.evaluateSharePolicy(ctx, nil)
}

// SuperviseTorrentPolicies enforces the saved seeding limit and the
// public-source auto-pause at most once a minute while the built-in client is
// enabled. A failed pass is logged and retried; nothing in the loop can stop
// the application.
func (s *Service) SuperviseTorrentPolicies(ctx context.Context, logger *slog.Logger) {
	ticker := time.NewTicker(shareSeedingTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		s.tickSharePolicy(ctx, logger)
		s.tickPublicPause(ctx, logger)
	}
}

func (s *Service) tickSharePolicy(ctx context.Context, logger *slog.Logger) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.Error("seeding limit check panicked", "error", recovered)
		}
	}()
	if _, err := s.evaluateSharePolicy(ctx, logger); err != nil && ctx.Err() == nil {
		logger.Warn("seeding limit check failed", "error", err)
	}
}

func (s *Service) evaluateSharePolicy(ctx context.Context, logger *slog.Logger) (SharePolicyRun, error) {
	s.shareMu.Lock()
	defer s.shareMu.Unlock()
	if s.shareHandledSet == nil {
		s.shareHandledSet = map[string]bool{}
	}
	var out SharePolicyRun
	policy, err := s.SharePolicy(ctx)
	if err != nil {
		return out, err
	}
	if !policy.Enabled {
		return out, nil
	}
	manager, c, err := s.builtinTorrents(ctx)
	if err != nil {
		var clientErr *Error
		if errors.As(err, &clientErr) && clientErr.Code == "builtin_unavailable" {
			// The client is disabled or not configured. The supervised pass
			// stays quiet; a manual run tells the administrator why.
			if logger == nil {
				return out, err
			}
			return out, nil
		}
		return out, err
	}
	items, err := manager.List(ctx, c)
	if err != nil {
		return out, err
	}
	active, err := s.activeDownloadHashes(ctx)
	if err != nil {
		return out, err
	}
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
		if active[hash] {
			continue
		}
		if item.Origin == torrentOriginJob && !policy.IncludeReelscarey {
			continue
		}
		// A paused torrent is already stopped; never delete data behind an
		// owner's explicit pause.
		if strings.HasPrefix(item.State, "paused") || strings.HasPrefix(item.State, "stopped") {
			continue
		}
		if !shareLimitReached(item, policy) {
			continue
		}
		out.Checked++
		action := effectiveShareAction(item, policy.Action)
		handled, err := s.shareHandled(ctx, hash, shareEventState(action))
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		if handled {
			continue
		}
		var applyErr error
		switch action {
		case shareActionPause:
			applyErr = manager.Pause(ctx, c, []string{hash})
		case shareActionRemove, shareActionRemoveAndDelete:
			applyErr = s.removeSeededTorrent(ctx, manager, c, item, policy, action == shareActionRemoveAndDelete)
			if errors.Is(applyErr, errShareChanged) {
				continue
			}
		default:
			applyErr = problem("invalid_selection", "The saved seeding policy action is not supported.")
		}
		if applyErr != nil {
			failures = errors.Join(failures, applyErr)
			continue
		}
		out.Acted++
		if err := s.recordShareEvent(ctx, hash, item.Name, action); err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		if len(s.shareHandledSet) >= shareHandledLimit {
			// The durable activity row remains the source of truth; the cache
			// only avoids repeated reads during one process lifetime.
			s.shareHandledSet = map[string]bool{}
		}
		s.shareHandledSet[hash+"|"+shareEventState(action)] = true
	}
	if failures != nil {
		if logger != nil {
			logger.Warn("some torrents did not accept the seeding limit", "error", failures)
			return out, nil
		}
		return out, failures
	}
	return out, nil
}

var errShareChanged = errors.New("torrent changed before seeding policy removal")

func (s *Service) removeSeededTorrent(ctx context.Context, manager torrentManager, c ClientConfig, item Torrent, policy SharePolicy, deleteFiles bool) error {
	// Both movie/TV and music reservations use configMu. Recheck with that
	// gate held so a newly queued job cannot lose its client entry.
	s.configMu.Lock()
	defer s.configMu.Unlock()
	hash := strings.ToLower(item.Hash)
	var active bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM downloads WHERE info_hash=? COLLATE NOCASE AND protocol='torrent' AND state NOT IN ('completed','cancelled')
		UNION ALL SELECT 1 FROM music_downloads WHERE info_hash=? COLLATE NOCASE AND protocol='torrent' AND state NOT IN ('completed','cancelled')
	)`, hash, hash).Scan(&active)
	if err != nil {
		return err
	}
	if active {
		return errShareChanged
	}
	current, err := manager.Find(ctx, c, hash)
	if err != nil {
		return err
	}
	if current == nil || current.Tags != item.Tags || current.AddedOn != item.AddedOn ||
		current.Progress < 1 || current.AmountLeft != 0 || !shareLimitReached(*current, policy) ||
		strings.HasPrefix(current.State, "paused") || strings.HasPrefix(current.State, "stopped") {
		return errShareChanged
	}
	return manager.Delete(ctx, c, []string{hash}, deleteFiles)
}
