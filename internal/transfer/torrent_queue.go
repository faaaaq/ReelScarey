package transfer

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// TorrentQueueLimits are the built-in qBittorrent queueing preferences. They
// are persisted separately from the generated qBittorrent.conf so a supervised
// restart never loses the owner's active download/upload limits.
type TorrentQueueLimits struct {
	QueueingEnabled    bool `json:"queueing_enabled"`
	MaxActiveDownloads int  `json:"max_active_downloads"`
	MaxActiveUploads   int  `json:"max_active_uploads"`
	MaxActiveTorrents  int  `json:"max_active_torrents"`
	Revision           int  `json:"revision"`
}

func (s *Service) TorrentQueueLimits(ctx context.Context) (TorrentQueueLimits, error) {
	var out TorrentQueueLimits
	var enabled int
	err := s.db.QueryRowContext(ctx, `SELECT builtin_queueing_enabled,builtin_max_active_downloads,
		builtin_max_active_uploads,builtin_max_active_torrents FROM download_client_preferences WHERE id=1`).
		Scan(&enabled, &out.MaxActiveDownloads, &out.MaxActiveUploads, &out.MaxActiveTorrents)
	if errors.Is(err, sql.ErrNoRows) {
		return TorrentQueueLimits{QueueingEnabled: true, MaxActiveDownloads: 3, MaxActiveUploads: 3, MaxActiveTorrents: 5, Revision: 1}, nil
	}
	if err != nil {
		return TorrentQueueLimits{}, err
	}
	out.QueueingEnabled = enabled == 1
	if err := s.db.QueryRowContext(ctx, `SELECT revision FROM download_client_preferences WHERE id=1`).Scan(&out.Revision); err != nil {
		return TorrentQueueLimits{}, err
	}
	return out, nil
}

// SetTorrentQueueLimits validates and saves the built-in client's queue
// settings, then applies them to a running client. A live-apply failure is
// reported after the values are saved; the next supervised start writes them
// into qBittorrent.conf.
func (s *Service) SetTorrentQueueLimits(ctx context.Context, in TorrentQueueLimits) (TorrentQueueLimits, error) {
	if in.MaxActiveDownloads < 1 || in.MaxActiveDownloads > 100 ||
		in.MaxActiveUploads < 1 || in.MaxActiveUploads > 100 ||
		in.MaxActiveTorrents < 1 || in.MaxActiveTorrents > 100 {
		return TorrentQueueLimits{}, problem("invalid_selection", "Choose active download, upload and torrent limits between 1 and 100.")
	}
	if in.Revision < 1 {
		return TorrentQueueLimits{}, problem("invalid_selection", "Reload the torrent page before saving queue limits.")
	}
	enabled := 0
	if in.QueueingEnabled {
		enabled = 1
	}
	result, err := s.db.ExecContext(ctx, `UPDATE download_client_preferences
		SET builtin_queueing_enabled=?,builtin_max_active_downloads=?,builtin_max_active_uploads=?,
		builtin_max_active_torrents=?,revision=revision+1
		WHERE id=1 AND revision=?`, enabled, in.MaxActiveDownloads, in.MaxActiveUploads, in.MaxActiveTorrents, in.Revision)
	if err != nil {
		return TorrentQueueLimits{}, err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return TorrentQueueLimits{}, problem("revision_conflict", "The queue limits changed. Reload the page and try again.")
	}
	out, err := s.TorrentQueueLimits(ctx)
	if err != nil {
		return TorrentQueueLimits{}, err
	}
	s.torrentPrefsMu.Lock()
	s.torrentPrefsUntil = time.Time{}
	s.torrentPrefsMu.Unlock()

	present, enabledClient, _, rowErr := s.bundledRow(ctx)
	if rowErr == nil && present && enabledClient {
		if manager, c, e := s.builtinTorrents(ctx); e == nil {
			if e = manager.SetServerQueueLimits(ctx, c, out); e != nil {
				var typed *Error
				if errors.As(e, &typed) && typed.Code == "builtin_unavailable" {
					return out, nil
				}
				return out, problem("queue_not_applied", "Queue limits were saved, but the running client could not apply them. They will apply on the next start.")
			}
		}
	}
	return out, nil
}
