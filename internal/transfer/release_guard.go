package transfer

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/releaseguard"
)

func releaseGuardKey(protocol, hash, releaseID string) (string, bool) {
	if protocol == "usenet" {
		key, err := releaseguard.UsenetKey(releaseID)
		return key, err == nil
	}
	key, err := releaseguard.TorrentKey(hash)
	return key, err == nil
}

func (s *Service) blockedRelease(ctx context.Context, protocol, hash, releaseID, indexer string) (bool, error) {
	key, ok := releaseGuardKey(protocol, hash, releaseID)
	if !ok {
		return false, nil
	}
	return releaseguard.New(s.db).Blocked(ctx, protocol, key, indexer)
}

// BlockRelease records a failed or rejected release for this indexer. A zero
// TTL keeps it blocked until the owner removes it.
func (s *Service) BlockRelease(ctx context.Context, protocol, hash, releaseID, indexer, title, reason string, ttl time.Duration) error {
	key, ok := releaseGuardKey(protocol, hash, releaseID)
	if !ok {
		return errors.New("the release has no stable identity to block")
	}
	expires := int64(0)
	if ttl > 0 {
		expires = time.Now().Add(ttl).Unix()
	}
	return releaseguard.New(s.db).Block(ctx, releaseguard.Entry{
		Protocol: protocol, ReleaseKey: key, Indexer: indexer, Title: title,
		Reason: reason, ExpiresAt: expires,
	})
}

func (s *Service) Blacklist(ctx context.Context, limit int) ([]releaseguard.Entry, error) {
	return releaseguard.New(s.db).List(ctx, limit)
}

func (s *Service) RemoveBlacklist(ctx context.Context, id int64) error {
	return releaseguard.New(s.db).Remove(ctx, id)
}

func (s *Service) blockFailedJob(ctx context.Context, j Job, problem *Error) {
	if problem == nil {
		return
	}
	var indexer string
	_ = s.db.QueryRowContext(ctx, `SELECT indexer_name FROM downloads WHERE id=?`, j.ID).Scan(&indexer)
	reason := strings.TrimSpace(problem.Code + ": " + problem.Message)
	if err := s.BlockRelease(ctx, j.Protocol, j.Hash, j.ID, indexer, j.Title, reason, 7*24*time.Hour); err != nil && s.logger != nil {
		s.logger.Warn("release blacklist write failed", "download", j.ID, "error", err)
	}
}

// RunStallWatch records active downloads whose byte progress has not moved for
// thirty minutes. It only observes; the normal worker still owns retries and
// terminal state. Each stall is written once to the activity feed until new
// progress arrives.
func (s *Service) RunStallWatch(ctx context.Context, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		stalled, err := releaseguard.DetectStalled(ctx, s.db, time.Now(), 30*time.Minute)
		if err != nil && ctx.Err() == nil {
			logger.Warn("stalled download check deferred", "error", err)
		}
		for _, item := range stalled {
			marked, err := releaseguard.MarkStalled(ctx, s.db, item.ID, time.Now())
			if err != nil || !marked {
				continue
			}
			_, _ = s.db.ExecContext(ctx, `INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'download',?,?,?)`,
				item.ID, item.Title, "stalled")
		}
		timer.Reset(2 * time.Minute)
	}
}
