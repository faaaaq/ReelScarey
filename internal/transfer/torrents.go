package transfer

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var torrentHashRE = regexp.MustCompile(`^[A-Fa-f0-9]{40}$`)

type torrentManager interface {
	TransferInfo(context.Context, ClientConfig) (TransferInfo, error)
	List(context.Context, ClientConfig) ([]Torrent, error)
	Find(context.Context, ClientConfig, string) (*Torrent, error)
	Files(context.Context, ClientConfig, string) ([]TorrentFile, error)
	Add(context.Context, ClientConfig, string, string, Payload) error
	Pause(context.Context, ClientConfig, []string) error
	Resume(context.Context, ClientConfig, []string) error
	Recheck(context.Context, ClientConfig, []string) error
	Delete(context.Context, ClientConfig, []string, bool) error
}

type TorrentOverview struct {
	Available bool         `json:"available"`
	State     string       `json:"state"`
	Message   string       `json:"message"`
	Transfer  TransferInfo `json:"transfer"`
	Items     []Torrent    `json:"items"`
}

type TorrentDetail struct {
	Torrent Torrent       `json:"torrent"`
	Files   []TorrentFile `json:"files"`
}

func (s *Service) builtinTorrents(ctx context.Context) (torrentManager, ClientConfig, error) {
	present, enabled, _, err := s.bundledRow(ctx)
	if err != nil {
		return nil, ClientConfig{}, err
	}
	if !present || !enabled {
		return nil, ClientConfig{}, problem("builtin_unavailable", "Enable the built-in qBittorrent client in Settings first.")
	}
	manager, ok := s.client.(torrentManager)
	if !ok {
		return nil, ClientConfig{}, problem("builtin_unavailable", "This download client does not support torrent management.")
	}
	c, err := s.managedClientConfig(ctx, builtinClientID)
	if err != nil {
		return nil, ClientConfig{}, err
	}
	return manager, c, nil
}

// Torrents lists the built-in client's queue. A disabled client is reported as
// unavailable rather than as an error, so the page can explain how to enable it.
func (s *Service) Torrents(ctx context.Context) (TorrentOverview, error) {
	present, enabled, _, err := s.bundledRow(ctx)
	if err != nil {
		return TorrentOverview{}, err
	}
	if !present || !enabled {
		message := "The built-in qBittorrent client is not enabled."
		if !present {
			message = "The built-in qBittorrent client is not configured."
		}
		return TorrentOverview{Available: false, State: "disabled", Message: message, Items: []Torrent{}}, nil
	}
	manager, c, err := s.builtinTorrents(ctx)
	if err != nil {
		return TorrentOverview{}, err
	}
	info, err := manager.TransferInfo(ctx, c)
	if err != nil {
		return TorrentOverview{}, err
	}
	items, err := manager.List(ctx, c)
	if err != nil {
		return TorrentOverview{}, err
	}
	state := "running"
	if info.Connection == "disconnected" {
		state = "disconnected"
	}
	return TorrentOverview{Available: true, State: state, Transfer: info, Items: items}, nil
}

func (s *Service) Torrent(ctx context.Context, hash string) (TorrentDetail, error) {
	out := TorrentDetail{Files: []TorrentFile{}}
	if !torrentHashRE.MatchString(hash) {
		return out, problem("invalid_selection", "Choose a valid torrent.")
	}
	manager, c, err := s.builtinTorrents(ctx)
	if err != nil {
		return out, err
	}
	found, err := manager.Find(ctx, c, strings.ToLower(hash))
	if err != nil {
		return out, err
	}
	if found == nil {
		return out, problem("not_found", "This torrent is no longer in the built-in client.")
	}
	files, err := manager.Files(ctx, c, found.Hash)
	if err != nil {
		return out, err
	}
	out.Torrent, out.Files = *found, files
	return out, nil
}

func (s *Service) TorrentAction(ctx context.Context, hash, action string, deleteFiles, reviewed bool) error {
	if !torrentHashRE.MatchString(hash) {
		return problem("invalid_selection", "Choose a valid torrent.")
	}
	manager, c, err := s.builtinTorrents(ctx)
	if err != nil {
		return err
	}
	hashes := []string{strings.ToLower(hash)}
	switch action {
	case "pause":
		return manager.Pause(ctx, c, hashes)
	case "resume":
		return manager.Resume(ctx, c, hashes)
	case "recheck":
		return manager.Recheck(ctx, c, hashes)
	case "remove":
		if !reviewed {
			return problem("review_required", "Review the torrent removal before deleting it.")
		}
		return manager.Delete(ctx, c, hashes, deleteFiles)
	default:
		return problem("invalid_selection", "Choose pause, resume, recheck or remove.")
	}
}

// AddManualTorrent accepts only an explicit magnet link or uploaded metainfo.
// Remote torrent URLs are not fetched by the server, which keeps the request
// from becoming an arbitrary outbound fetch.
func (s *Service) AddManualTorrent(ctx context.Context, magnet string, metainfo []byte) error {
	manager, c, err := s.builtinTorrents(ctx)
	if err != nil {
		return err
	}
	magnet = strings.TrimSpace(magnet)
	if magnet != "" {
		if len(magnet) > 64<<10 || !strings.HasPrefix(magnet, "magnet:?") || strings.ContainsAny(magnet, "\x00\r\n") {
			return problem("invalid_selection", "Enter a valid magnet link.")
		}
	} else {
		if len(metainfo) < 2 || len(metainfo) > 10<<20 || metainfo[0] != 'd' {
			return problem("invalid_selection", "Choose a valid .torrent file.")
		}
	}
	folder := filepath.Join(s.cfg.DownloadsDir, "builtin", "ReelScarey", "manual")
	if e := os.MkdirAll(folder, 0o700); e != nil {
		return unavailable("The built-in download folder is not writable.")
	}
	return manager.Add(ctx, c, "reelscarey-manual", folder, Payload{Magnet: magnet, Torrent: metainfo})
}
