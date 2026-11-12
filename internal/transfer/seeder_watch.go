package transfer

import (
	"context"
	"log/slog"
	"time"
)

// Seeder-assist tuning.
//
// Announce-delivered grabs are submitted the instant the tracker advertises
// them, which is often before the swarm has a reachable seeder. qBittorrent
// then sits in a zero-peer state until its own slow periodic re-announce (many
// minutes later). This watchdog polls shortly after submission and pushes an
// immediate re-announce while the swarm still reports no seeds, so the transfer
// starts as soon as a seeder appears instead of waiting for the client's own
// schedule. It is separate from RunStallWatch, which only observes stalled byte
// progress after the fact.
const (
	// seederWatchInterval is how often the freshly submitted torrents are
	// re-checked for seeders.
	seederWatchInterval = 8 * time.Second
	// seederWatchWindow bounds the assist to recent submissions. Older jobs
	// are handled by the normal observed stall watch and the ordinary worker.
	seederWatchWindow = 30 * time.Minute
	// seederAssistGap spaces the re-announces for one torrent so a public
	// tracker is never hammered.
	seederAssistGap = 20 * time.Second
	// seederAssistMaxTries caps the re-announces per job. It is derived from
	// the watch window and the polite gap so the assist keeps nudging across
	// the whole window instead of stopping after a minute or two and leaving
	// the transfer stalled until qBittorrent's own slow periodic re-announce
	// (usually ~30 minutes later). A release that still has no seeders is not
	// made downloadable by asking again, but a swarm that has not formed yet
	// often does so within the window, so the extra nudges are worth it.
	seederAssistMaxTries = int(seederWatchWindow / seederAssistGap)
	// seederWatchBatch bounds one pass over the candidate rows.
	seederWatchBatch = 50
)

// seederAssistState records how often one download has already been nudged and
// when, so the watchdog re-announces at a polite cadence rather than on every
// poll. It lives only in memory; see Service.seederHelp.
type seederAssistState struct {
	last  time.Time
	tries int
}

// RunSeederWatch polls recently submitted torrents and re-announces any whose
// swarm reports no seeds while the transfer has not begun.
func (s *Service) RunSeederWatch(ctx context.Context, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	timer := time.NewTimer(seederWatchInterval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if err := s.AssistSeederless(ctx, logger); err != nil && ctx.Err() == nil {
			logger.Warn("seeder watchdog deferred", "error", err)
		}
		timer.Reset(seederWatchInterval)
	}
}

// AssistSeederless re-announces every recently submitted torrent that still
// reports no seeds and has not started downloading. It is exported so tests and
// operators can run a single pass without the timer.
func (s *Service) AssistSeederless(ctx context.Context, logger *slog.Logger) error {
	cutoff := time.Now().Add(-seederWatchWindow).Unix()
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM downloads
WHERE state='downloading' AND protocol='torrent' AND progress<1 AND submission_attempted=1 AND submitted_at>=?
ORDER BY submitted_at DESC,id LIMIT ?`, cutoff, seederWatchBatch)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		s.assistOneSeederless(ctx, logger, id)
	}
	s.pruneSeederHelp()
	return nil
}

func (s *Service) assistOneSeederless(ctx context.Context, logger *slog.Logger, id string) {
	j, err := s.Job(ctx, id)
	if err != nil || j.Protocol != "torrent" {
		return
	}
	c, err := s.boundClient(ctx, j)
	if err != nil {
		return
	}
	client := s.torrentClient(c)
	manager, ok := client.(torrentManager)
	if !ok {
		// The classic Transmission adapter exposes no per-torrent re-announce
		// and no seeder count, so there is nothing to act on.
		return
	}
	torrent, err := client.Find(ctx, c, j.Hash)
	if err != nil || torrent == nil {
		return
	}
	if !containsPart(torrent.Tags, "reelscarey-"+j.ID) {
		return
	}
	if !seederAssistStateActive(torrent.State) {
		return
	}
	if torrent.Progress > 0 || torrent.NumSeeds > 0 || torrent.NumComplete > 0 {
		// The transfer started or the swarm has seeds; stop watching this job.
		s.forgetSeederHelp(id)
		return
	}
	if !s.seederHelpDue(id) {
		return
	}
	if err = manager.Command(ctx, c, "reannounce", []string{j.Hash}, ""); err != nil {
		if logger != nil {
			logger.Debug("seeder assist re-announce failed", "download", id, "error", err)
		}
		return
	}
	s.noteSeederHelp(id)
	if logger != nil {
		logger.Info("re-announced a torrent with no reported seeds", "download", id, "client_state", torrent.State)
	}
}

// seederAssistStateActive reports whether a qBittorrent state is one where the
// torrent is actively trying to reach peers. Paused, errored, verifying and
// seeding states are left alone.
func seederAssistStateActive(state string) bool {
	switch state {
	case "downloading", "metaDL", "forcedMetaDL", "stalledDL", "forcedDL", "allocating", "unknown":
		return true
	default:
		return false
	}
}

func (s *Service) seederHelpDue(id string) bool {
	s.seederMu.Lock()
	defer s.seederMu.Unlock()
	st, ok := s.seederHelp[id]
	if !ok {
		return true
	}
	if st.tries >= seederAssistMaxTries {
		return false
	}
	return time.Since(st.last) >= seederAssistGap
}

func (s *Service) noteSeederHelp(id string) {
	s.seederMu.Lock()
	defer s.seederMu.Unlock()
	if s.seederHelp == nil {
		s.seederHelp = map[string]seederAssistState{}
	}
	st := s.seederHelp[id]
	st.tries++
	st.last = time.Now()
	s.seederHelp[id] = st
}

func (s *Service) forgetSeederHelp(id string) {
	s.seederMu.Lock()
	defer s.seederMu.Unlock()
	delete(s.seederHelp, id)
}

func (s *Service) pruneSeederHelp() {
	cutoff := time.Now().Add(-seederWatchWindow)
	s.seederMu.Lock()
	defer s.seederMu.Unlock()
	for id, st := range s.seederHelp {
		if st.last.Before(cutoff) {
			delete(s.seederHelp, id)
		}
	}
}
