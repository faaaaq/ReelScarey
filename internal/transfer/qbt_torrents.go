package transfer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// TransferInfo returns the client's current global rates and connection state.
func (q *QBittorrent) TransferInfo(ctx context.Context, c ClientConfig) (TransferInfo, error) {
	var out TransferInfo
	data, err := q.request(ctx, c, http.MethodGet, "transfer/info", "", nil)
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(data, &out); err != nil {
		return TransferInfo{}, problem("client_response", "qBittorrent returned invalid transfer information.")
	}
	return out, nil
}

// List returns every torrent in the client, newest activity first is left to
// the caller. The result is bounded because a torrent client can hold thousands
// of entries.
func (q *QBittorrent) List(ctx context.Context, c ClientConfig) ([]Torrent, error) {
	data, err := q.request(ctx, c, http.MethodGet, "torrents/info", "", nil)
	if err != nil {
		return nil, err
	}
	var out []Torrent
	if err = json.Unmarshal(data, &out); err != nil || len(out) > 10000 {
		return nil, problem("client_response", "qBittorrent returned an invalid torrent list.")
	}
	return out, nil
}

func (q *QBittorrent) torrentForm(ctx context.Context, c ClientConfig, api string, values url.Values) error {
	_, err := q.request(ctx, c, http.MethodPost, api, "application/x-www-form-urlencoded", strings.NewReader(values.Encode()))
	return err
}

// Pause and Resume target the shipped built-in qBittorrent (4.x). The built-in
// client is supervised by the application, so its API generation is fixed.
func (q *QBittorrent) Pause(ctx context.Context, c ClientConfig, hashes []string) error {
	return q.torrentForm(ctx, c, "torrents/pause", url.Values{"hashes": {strings.Join(hashes, "|")}})
}
func (q *QBittorrent) Resume(ctx context.Context, c ClientConfig, hashes []string) error {
	return q.torrentForm(ctx, c, "torrents/resume", url.Values{"hashes": {strings.Join(hashes, "|")}})
}
func (q *QBittorrent) Recheck(ctx context.Context, c ClientConfig, hashes []string) error {
	return q.torrentForm(ctx, c, "torrents/recheck", url.Values{"hashes": {strings.Join(hashes, "|")}})
}
func (q *QBittorrent) Delete(ctx context.Context, c ClientConfig, hashes []string, deleteFiles bool) error {
	return q.torrentForm(ctx, c, "torrents/delete", url.Values{
		"hashes":      {strings.Join(hashes, "|")},
		"deleteFiles": {map[bool]string{true: "true", false: "false"}[deleteFiles]},
	})
}

// qbtCommands is the closed set of per-torrent commands ReelScarey forwards.
// Location, rename, category, tag and tracker edits are intentionally absent.
var qbtCommands = map[string]struct{ api, param string }{
	"reannounce":        {"torrents/reannounce", ""},
	"force_start":       {"torrents/setForceStart", "value"},
	"super_seeding":     {"torrents/setSuperSeeding", "value"},
	"queue_top":         {"torrents/topPrio", ""},
	"queue_bottom":      {"torrents/bottomPrio", ""},
	"queue_up":          {"torrents/increasePrio", ""},
	"queue_down":        {"torrents/decreasePrio", ""},
	"download_limit":    {"torrents/setDownloadLimit", "limit"},
	"upload_limit":      {"torrents/setUploadLimit", "limit"},
	"toggle_sequential": {"torrents/toggleSequentialDownload", ""},
	"toggle_first_last": {"torrents/toggleFirstLastPiecePrio", ""},
}

// Command sends one allowlisted command to the selected torrents.
func (q *QBittorrent) Command(ctx context.Context, c ClientConfig, command string, hashes []string, value string) error {
	spec, ok := qbtCommands[command]
	if !ok {
		return problem("invalid_selection", "This torrent action is not supported.")
	}
	values := url.Values{"hashes": {strings.Join(hashes, "|")}}
	if spec.param != "" {
		values.Set(spec.param, value)
	}
	_, status, err := q.requestStatus(ctx, c, http.MethodPost, spec.api, "application/x-www-form-urlencoded", strings.NewReader(values.Encode()))
	if err != nil && status == http.StatusConflict && strings.HasPrefix(command, "queue_") {
		return problem("queueing_disabled", "Torrent queueing is turned off in the built-in client.")
	}
	return err
}

// SetShareLimits sets per-torrent ratio and seeding-time limits. qBittorrent
// 4.5 requires only ratioLimit and seedingTimeLimit and ignores other fields;
// 4.6+ also requires inactiveSeedingTimeLimit, which is sent as "use global".
func (q *QBittorrent) SetShareLimits(ctx context.Context, c ClientConfig, hashes []string, ratio float64, minutes int64) error {
	return q.torrentForm(ctx, c, "torrents/setShareLimits", url.Values{
		"hashes":                   {strings.Join(hashes, "|")},
		"ratioLimit":               {strconv.FormatFloat(ratio, 'f', -1, 64)},
		"seedingTimeLimit":         {strconv.FormatInt(minutes, 10)},
		"inactiveSeedingTimeLimit": {"-2"},
	})
}

func (q *QBittorrent) SetFilePriority(ctx context.Context, c ClientConfig, hash string, indexes []int, priority int) error {
	ids := make([]string, len(indexes))
	for n, v := range indexes {
		ids[n] = strconv.Itoa(v)
	}
	values := url.Values{"hash": {hash}, "id": {strings.Join(ids, "|")}, "priority": {strconv.Itoa(priority)}}
	_, status, err := q.requestStatus(ctx, c, http.MethodPost, "torrents/filePrio", "application/x-www-form-urlencoded", strings.NewReader(values.Encode()))
	switch {
	case err != nil && status == http.StatusConflict:
		return problem("file_priority_rejected", "qBittorrent has not loaded this torrent's file list yet, or a file no longer exists.")
	case err != nil && status == http.StatusNotFound:
		return problem("not_found", "This torrent is no longer in the built-in client.")
	}
	return err
}

func (q *QBittorrent) SpeedLimitsMode(ctx context.Context, c ClientConfig) (bool, error) {
	data, err := q.request(ctx, c, http.MethodGet, "transfer/speedLimitsMode", "", nil)
	if err != nil {
		return false, err
	}
	switch strings.TrimSpace(string(data)) {
	case "1":
		return true, nil
	case "0":
		return false, nil
	}
	return false, problem("client_response", "qBittorrent returned an invalid speed limit mode.")
}

func (q *QBittorrent) ToggleSpeedLimitsMode(ctx context.Context, c ClientConfig) error {
	return q.torrentForm(ctx, c, "transfer/toggleSpeedLimitsMode", url.Values{})
}

// SetGlobalLimit sets the global limit of the currently active speed mode in
// bytes per second; 0 removes the limit.
func (q *QBittorrent) SetGlobalLimit(ctx context.Context, c ClientConfig, upload bool, limit int64) error {
	api := "transfer/setDownloadLimit"
	if upload {
		api = "transfer/setUploadLimit"
	}
	return q.torrentForm(ctx, c, api, url.Values{"limit": {strconv.FormatInt(limit, 10)}})
}

// ServerPreferences decodes only the queueing switch and alternative limits.
// Every other preference, including credentials and autorun programs, is
// discarded unread.
func (q *QBittorrent) ServerPreferences(ctx context.Context, c ClientConfig) (TorrentServer, error) {
	data, err := q.request(ctx, c, http.MethodGet, "app/preferences", "", nil)
	if err != nil {
		return TorrentServer{}, err
	}
	var prefs struct {
		Queueing *bool  `json:"queueing_enabled"`
		AltDL    *int64 `json:"alt_dl_limit"`
		AltUP    *int64 `json:"alt_up_limit"`
	}
	if json.Unmarshal(data, &prefs) != nil || prefs.Queueing == nil || prefs.AltDL == nil || prefs.AltUP == nil {
		return TorrentServer{}, problem("client_response", "qBittorrent returned invalid preferences.")
	}
	// Although the Web API documentation says KiB/s, qBittorrent 4.5 converts
	// the stored KiB value and reports these preferences in bytes per second.
	limit := func(v int64) int64 {
		if v <= 0 || v > 1<<40 {
			return 0
		}
		return v
	}
	return TorrentServer{QueueingEnabled: *prefs.Queueing, AltDLLimit: limit(*prefs.AltDL), AltUPLimit: limit(*prefs.AltUP)}, nil
}

// detail reads one per-torrent detail endpoint and maps a missing torrent.
func (q *QBittorrent) detail(ctx context.Context, c ClientConfig, api, hash string, out any, what string) error {
	data, status, err := q.requestStatus(ctx, c, http.MethodGet, api+"?"+url.Values{"hash": {hash}}.Encode(), "", nil)
	if err != nil {
		if status == http.StatusNotFound {
			return problem("not_found", "This torrent is no longer in the built-in client.")
		}
		return err
	}
	if json.Unmarshal(data, out) != nil {
		return problem("client_response", "qBittorrent returned invalid "+what+".")
	}
	return nil
}

func (q *QBittorrent) Properties(ctx context.Context, c ClientConfig, hash string) (TorrentProperties, error) {
	var out TorrentProperties
	if err := q.detail(ctx, c, "torrents/properties", hash, &out, "torrent properties"); err != nil {
		return TorrentProperties{}, err
	}
	out.Comment = cleanClientText(out.Comment, maxTorrentComment)
	out.CreatedBy = cleanClientText(out.CreatedBy, 200)
	return out, nil
}

func (q *QBittorrent) Trackers(ctx context.Context, c ClientConfig, hash string) ([]TorrentTracker, error) {
	var raw []TorrentTracker
	if err := q.detail(ctx, c, "torrents/trackers", hash, &raw, "trackers"); err != nil {
		return nil, err
	}
	if len(raw) > maxTorrentList {
		return nil, problem("client_response", "qBittorrent returned too many trackers.")
	}
	for n := range raw {
		original := raw[n].URL
		raw[n].URL = RedactTrackerURL(original)
		msg := raw[n].Message
		if strings.TrimSpace(original) != "" {
			msg = strings.ReplaceAll(msg, original, raw[n].URL)
		}
		raw[n].Message = cleanClientText(redactURLsInText(msg), maxTrackerMessage)
	}
	return raw, nil
}

func (q *QBittorrent) WebSeeds(ctx context.Context, c ClientConfig, hash string) ([]TorrentWebSeed, error) {
	var raw []TorrentWebSeed
	if err := q.detail(ctx, c, "torrents/webseeds", hash, &raw, "web seeds"); err != nil {
		return nil, err
	}
	if len(raw) > maxTorrentList {
		return nil, problem("client_response", "qBittorrent returned too many web seeds.")
	}
	for n := range raw {
		raw[n].URL = RedactTrackerURL(raw[n].URL)
	}
	return raw, nil
}

// Peers reads a full peer snapshot (rid=0). The map keyed by "ip:port" becomes
// a list ordered by combined transfer rate, then address, capped at maxPeers.
func (q *QBittorrent) Peers(ctx context.Context, c ClientConfig, hash string) ([]TorrentPeer, error) {
	data, status, err := q.requestStatus(ctx, c, http.MethodGet, "sync/torrentPeers?"+url.Values{"hash": {hash}, "rid": {"0"}}.Encode(), "", nil)
	if err != nil {
		if status == http.StatusNotFound {
			return nil, problem("not_found", "This torrent is no longer in the built-in client.")
		}
		return nil, err
	}
	var raw struct {
		Peers map[string]TorrentPeer `json:"peers"`
	}
	if json.Unmarshal(data, &raw) != nil || len(raw.Peers) > maxPeerResponse {
		return nil, problem("client_response", "qBittorrent returned an invalid peer list.")
	}
	out := make([]TorrentPeer, 0, len(raw.Peers))
	for key, p := range raw.Peers {
		p.key = key
		p.IP = cleanClientText(p.IP, 64)
		p.Client = cleanClientText(p.Client, 200)
		p.CountryCode = cleanClientText(p.CountryCode, 8)
		p.Connection = cleanClientText(p.Connection, 32)
		p.Flags = cleanClientText(p.Flags, 64)
		out = append(out, p)
	}
	sortPeers(out)
	if len(out) > maxPeers {
		out = out[:maxPeers]
	}
	return out, nil
}

func sortPeers(peers []TorrentPeer) {
	sort.Slice(peers, func(i, j int) bool {
		a, b := peers[i].DLSpeed+peers[i].UPSpeed, peers[j].DLSpeed+peers[j].UPSpeed
		if a != b {
			return a > b
		}
		return peers[i].key < peers[j].key
	})
}

func (q *QBittorrent) PieceStates(ctx context.Context, c ClientConfig, hash string) ([]int, error) {
	var raw []int
	if err := q.detail(ctx, c, "torrents/pieceStates", hash, &raw, "piece states"); err != nil {
		return nil, err
	}
	if len(raw) > maxPieceStates {
		return nil, problem("client_response", "qBittorrent returned too many piece states.")
	}
	for _, v := range raw {
		if v < 0 || v > 2 {
			return nil, problem("client_response", "qBittorrent returned invalid piece states.")
		}
	}
	return raw, nil
}
