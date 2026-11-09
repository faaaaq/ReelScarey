package transfer

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	torrentHashRE = regexp.MustCompile(`^[A-Fa-f0-9]{40}$`)
	jobTagRE      = regexp.MustCompile(`^reelscarey-([A-Za-z0-9_-]{1,64})$`)
)

const (
	maxTorrentList    = 10000
	maxTorrentComment = 2000
	maxTrackerMessage = 500
	maxPeers          = 2000
	maxPeerResponse   = 20000
	maxPieceStates    = 1000000
	maxPieceBuckets   = 512
	maxBulkTorrents   = 500
	maxFileIndexes    = 10000
	// maxRateLimit keeps byte-per-second limits inside qBittorrent's int range.
	maxRateLimit       = 2000000000
	maxShareRatio      = 9998
	maxSeedingMinutes  = 525600
	manualTorrentTag   = "reelscarey-manual"
	manualPublicTag    = "reelscarey:public"
	torrentOriginJob   = "job"
	torrentOriginOwner = "manual"
	torrentOriginOther = "external"
)

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
	Command(ctx context.Context, c ClientConfig, command string, hashes []string, value string) error
	SetShareLimits(ctx context.Context, c ClientConfig, hashes []string, ratio float64, minutes int64) error
	SetFilePriority(ctx context.Context, c ClientConfig, hash string, indexes []int, priority int) error
	SpeedLimitsMode(context.Context, ClientConfig) (bool, error)
	ToggleSpeedLimitsMode(context.Context, ClientConfig) error
	SetGlobalLimit(ctx context.Context, c ClientConfig, upload bool, limit int64) error
	ServerPreferences(context.Context, ClientConfig) (TorrentServer, error)
	SetServerQueueLimits(context.Context, ClientConfig, TorrentQueueLimits) error
	Properties(context.Context, ClientConfig, string) (TorrentProperties, error)
	Trackers(context.Context, ClientConfig, string) ([]TorrentTracker, error)
	Peers(context.Context, ClientConfig, string) ([]TorrentPeer, error)
	WebSeeds(context.Context, ClientConfig, string) ([]TorrentWebSeed, error)
	PieceStates(context.Context, ClientConfig, string) ([]int, error)
}

type TorrentOverview struct {
	Available bool         `json:"available"`
	State     string       `json:"state"`
	Message   string       `json:"message"`
	Transfer  TransferInfo `json:"transfer"`
	// Server is null while the built-in client is unavailable.
	Server *TorrentServer     `json:"server"`
	Queue  TorrentQueueLimits `json:"queue"`
	Items  []Torrent          `json:"items"`
}

// TorrentServer is the small, read-only part of the client configuration the
// page displays. Limits are bytes per second; 0 means unlimited.
type TorrentServer struct {
	AltSpeedEnabled    bool  `json:"alt_speed_enabled"`
	QueueingEnabled    bool  `json:"queueing_enabled"`
	AltDLLimit         int64 `json:"alt_dl_limit"`
	AltUPLimit         int64 `json:"alt_up_limit"`
	MaxActiveDownloads int   `json:"max_active_downloads"`
	MaxActiveUploads   int   `json:"max_active_uploads"`
	MaxActiveTorrents  int   `json:"max_active_torrents"`
}

type TorrentProperties struct {
	SavePath         string  `json:"save_path"`
	CreationDate     flexInt `json:"creation_date"`
	PieceSize        flexInt `json:"piece_size"`
	Comment          string  `json:"comment"`
	TotalWasted      flexInt `json:"total_wasted"`
	TotalUploaded    flexInt `json:"total_uploaded"`
	TotalDownloaded  flexInt `json:"total_downloaded"`
	UPLimit          flexInt `json:"up_limit"`
	DLLimit          flexInt `json:"dl_limit"`
	TimeElapsed      flexInt `json:"time_elapsed"`
	SeedingTime      flexInt `json:"seeding_time"`
	Connections      flexInt `json:"nb_connections"`
	ConnectionsLimit flexInt `json:"nb_connections_limit"`
	ShareRatio       float64 `json:"share_ratio"`
	AdditionDate     flexInt `json:"addition_date"`
	CompletionDate   flexInt `json:"completion_date"`
	CreatedBy        string  `json:"created_by"`
	DLSpeedAvg       flexInt `json:"dl_speed_avg"`
	DLSpeed          flexInt `json:"dl_speed"`
	UPSpeedAvg       flexInt `json:"up_speed_avg"`
	UPSpeed          flexInt `json:"up_speed"`
	ETA              flexInt `json:"eta"`
	LastSeen         flexInt `json:"last_seen"`
	Peers            flexInt `json:"peers"`
	PeersTotal       flexInt `json:"peers_total"`
	Seeds            flexInt `json:"seeds"`
	SeedsTotal       flexInt `json:"seeds_total"`
	PiecesHave       flexInt `json:"pieces_have"`
	PiecesNum        flexInt `json:"pieces_num"`
	Reannounce       flexInt `json:"reannounce"`
	TotalSize        flexInt `json:"total_size"`
}

// TorrentTracker URLs are always redacted to scheme://host[:port].
type TorrentTracker struct {
	URL           string  `json:"url"`
	Status        flexInt `json:"status"`
	Tier          flexInt `json:"tier"`
	NumPeers      flexInt `json:"num_peers"`
	NumSeeds      flexInt `json:"num_seeds"`
	NumLeeches    flexInt `json:"num_leeches"`
	NumDownloaded flexInt `json:"num_downloaded"`
	Message       string  `json:"msg"`
}

type TorrentPeer struct {
	IP          string  `json:"ip"`
	Port        flexInt `json:"port"`
	Client      string  `json:"client"`
	CountryCode string  `json:"country_code"`
	Connection  string  `json:"connection"`
	Flags       string  `json:"flags"`
	Progress    float64 `json:"progress"`
	DLSpeed     flexInt `json:"dl_speed"`
	UPSpeed     flexInt `json:"up_speed"`
	Downloaded  flexInt `json:"downloaded"`
	Uploaded    flexInt `json:"uploaded"`
	Relevance   float64 `json:"relevance"`
	key         string
}

// TorrentWebSeed URLs are always redacted to scheme://host[:port].
type TorrentWebSeed struct {
	URL string `json:"url"`
}

type TorrentDetail struct {
	Torrent    Torrent           `json:"torrent"`
	Files      []TorrentFile     `json:"files"`
	Properties TorrentProperties `json:"properties"`
	Trackers   []TorrentTracker  `json:"trackers"`
	Peers      []TorrentPeer     `json:"peers"`
	WebSeeds   []TorrentWebSeed  `json:"webseeds"`
	// Pieces holds at most 512 buckets: 0 = no piece in the bucket is
	// downloading or complete, 2 = every piece is complete, 1 = anything else.
	Pieces []int `json:"pieces"`
}

// TorrentBulkAction is one reviewed action for up to 500 selected torrents.
type TorrentBulkAction struct {
	Hashes           []string
	Action           string
	Enabled          *bool
	Limit            *int64
	RatioLimit       *float64
	SeedingTimeLimit *int64
	DeleteFiles      bool
	Reviewed         bool
}

// TorrentSpeedLimits changes global client limits; nil fields are unchanged.
type TorrentSpeedLimits struct {
	AltEnabled    *bool
	DownloadLimit *int64
	UploadLimit   *int64
}

// classifyTorrent derives ownership from the tags ReelScarey applies when it
// submits a torrent. Any value previously present is replaced.
func classifyTorrent(t *Torrent) {
	t.Origin, t.JobID = torrentOriginOther, ""
	t.Tracker = RedactTrackerURL(t.Tracker)
	manual := false
	for _, tag := range strings.Split(t.Tags, ",") {
		tag = strings.TrimSpace(tag)
		if tag == manualTorrentTag {
			manual = true
			continue
		}
		if m := jobTagRE.FindStringSubmatch(tag); m != nil {
			t.Origin, t.JobID = torrentOriginJob, m[1]
			return
		}
	}
	if manual {
		t.Origin = torrentOriginOwner
	}
}

// downsamplePieces reduces qBittorrent piece states (0 missing, 1 downloading,
// 2 complete) to at most max buckets of near-equal size. A bucket is 2 only if
// every piece in it is complete, 0 only if every piece is missing, and 1 when
// any piece is downloading or the bucket is partially complete.
func downsamplePieces(states []int, max int) []int {
	n := len(states)
	if n == 0 || max <= 0 {
		return []int{}
	}
	buckets := n
	if buckets > max {
		buckets = max
	}
	out := make([]int, buckets)
	for b := 0; b < buckets; b++ {
		start, end := b*n/buckets, (b+1)*n/buckets
		complete, missing := true, true
		for _, v := range states[start:end] {
			if v != 2 {
				complete = false
			}
			if v != 0 {
				missing = false
			}
		}
		switch {
		case complete:
			out[b] = 2
		case missing:
			out[b] = 0
		default:
			out[b] = 1
		}
	}
	return out
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
	queue, err := s.TorrentQueueLimits(ctx)
	if err != nil {
		return TorrentOverview{}, err
	}
	if !present || !enabled {
		message := "The built-in qBittorrent client is not enabled."
		if !present {
			message = "The built-in qBittorrent client is not configured."
		}
		return TorrentOverview{Available: false, State: "disabled", Message: message, Queue: queue, Items: []Torrent{}}, nil
	}
	manager, c, err := s.builtinTorrents(ctx)
	if err != nil {
		return TorrentOverview{}, err
	}
	info, err := manager.TransferInfo(ctx, c)
	if err != nil {
		return TorrentOverview{}, err
	}
	info.DLRateLimit, info.UPRateLimit = max(info.DLRateLimit, 0), max(info.UPRateLimit, 0)
	server, err := s.torrentServerPreferences(ctx, manager, c)
	if err != nil {
		return TorrentOverview{}, err
	}
	items, err := manager.List(ctx, c)
	if err != nil {
		return TorrentOverview{}, err
	}
	if items == nil {
		items = []Torrent{}
	}
	for n := range items {
		classifyTorrent(&items[n])
	}
	state := "running"
	if info.Connection == "disconnected" {
		state = "disconnected"
	}
	return TorrentOverview{Available: true, State: state, Transfer: info, Server: &server, Queue: queue, Items: items}, nil
}

func (s *Service) torrentServerPreferences(ctx context.Context, manager torrentManager, c ClientConfig) (TorrentServer, error) {
	s.torrentPrefsMu.Lock()
	defer s.torrentPrefsMu.Unlock()
	if time.Now().Before(s.torrentPrefsUntil) {
		return s.torrentPrefs, nil
	}
	prefs, err := manager.ServerPreferences(ctx, c)
	if err != nil {
		return TorrentServer{}, err
	}
	if prefs.AltSpeedEnabled, err = manager.SpeedLimitsMode(ctx, c); err != nil {
		return TorrentServer{}, err
	}
	s.torrentPrefs, s.torrentPrefsUntil = prefs, time.Now().Add(15*time.Second)
	return prefs, nil
}

func (s *Service) Torrent(ctx context.Context, hash string) (TorrentDetail, error) {
	return s.TorrentSection(ctx, hash, "")
}

// TorrentSection loads only the requested panel. An empty section preserves
// the full response for existing clients.
func (s *Service) TorrentSection(ctx context.Context, hash, section string) (TorrentDetail, error) {
	out := TorrentDetail{Files: []TorrentFile{}, Trackers: []TorrentTracker{}, Peers: []TorrentPeer{}, WebSeeds: []TorrentWebSeed{}, Pieces: []int{}}
	switch section {
	case "", "general", "content", "trackers", "peers", "http":
	default:
		return out, problem("invalid_selection", "Choose a valid torrent detail section.")
	}
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
	out.Torrent = *found
	classifyTorrent(&out.Torrent)
	h := found.Hash
	if section == "" || section == "content" {
		files, err := manager.Files(ctx, c, h)
		if err != nil {
			return out, err
		}
		if files != nil {
			out.Files = files
		}
	}
	if section == "" || section == "general" {
		props, err := manager.Properties(ctx, c, h)
		if err != nil {
			return out, err
		}
		states, err := manager.PieceStates(ctx, c, h)
		if err != nil {
			return out, err
		}
		out.Properties, out.Pieces = props, downsamplePieces(states, maxPieceBuckets)
	}
	if section == "" || section == "trackers" {
		trackers, err := manager.Trackers(ctx, c, h)
		if err != nil {
			return out, err
		}
		for _, t := range trackers {
			t.URL = RedactTrackerURL(t.URL)
			out.Trackers = append(out.Trackers, t)
		}
	}
	if section == "" || section == "peers" {
		peers, err := manager.Peers(ctx, c, h)
		if err != nil {
			return out, err
		}
		if peers != nil {
			out.Peers = peers
		}
	}
	if section == "" || section == "http" {
		seeds, err := manager.WebSeeds(ctx, c, h)
		if err != nil {
			return out, err
		}
		for _, seed := range seeds {
			out.WebSeeds = append(out.WebSeeds, TorrentWebSeed{URL: RedactTrackerURL(seed.URL)})
		}
	}
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

func normalizeTorrentHashes(in []string) ([]string, error) {
	if len(in) == 0 || len(in) > maxBulkTorrents {
		return nil, problem("invalid_selection", "Choose between 1 and 500 torrents.")
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, h := range in {
		if !torrentHashRE.MatchString(h) {
			return nil, problem("invalid_selection", "Choose valid torrents.")
		}
		h = strings.ToLower(h)
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out, nil
}

func validRateLimit(v *int64) bool { return v != nil && *v >= 0 && *v <= maxRateLimit }

// TorrentBulk applies one action to the selected built-in client torrents.
// Every input is validated before the client is contacted.
func (s *Service) TorrentBulk(ctx context.Context, in TorrentBulkAction) error {
	hashes, err := normalizeTorrentHashes(in.Hashes)
	if err != nil {
		return err
	}
	needEnabled := func() error {
		if in.Enabled == nil {
			return problem("invalid_selection", "Choose whether to turn this option on or off.")
		}
		return nil
	}
	switch in.Action {
	case "pause", "resume", "recheck", "reannounce", "queue_top", "queue_bottom", "queue_up", "queue_down":
	case "force_start", "sequential", "first_last_piece", "super_seeding":
		if err = needEnabled(); err != nil {
			return err
		}
	case "set_download_limit", "set_upload_limit":
		if !validRateLimit(in.Limit) {
			return problem("invalid_selection", "Enter a speed limit between 0 (unlimited) and 2000000000 bytes per second.")
		}
	case "set_share_limits":
		r, m := in.RatioLimit, in.SeedingTimeLimit
		if r == nil || math.IsNaN(*r) || math.IsInf(*r, 0) || !(*r == -2 || *r == -1 || (*r >= 0 && *r <= maxShareRatio)) {
			return problem("invalid_selection", "Enter a ratio limit of -2 (global), -1 (unlimited) or between 0 and 9998.")
		}
		if m == nil || !(*m == -2 || *m == -1 || (*m >= 0 && *m <= maxSeedingMinutes)) {
			return problem("invalid_selection", "Enter a seeding time limit of -2 (global), -1 (unlimited) or between 0 and 525600 minutes.")
		}
	case "remove":
		if !in.Reviewed {
			return problem("review_required", "Review the torrent removal before deleting it.")
		}
	default:
		return problem("invalid_selection", "This torrent action is not supported.")
	}
	manager, c, err := s.builtinTorrents(ctx)
	if err != nil {
		return err
	}
	switch in.Action {
	case "pause":
		return manager.Pause(ctx, c, hashes)
	case "resume":
		return manager.Resume(ctx, c, hashes)
	case "recheck":
		return manager.Recheck(ctx, c, hashes)
	case "remove":
		return manager.Delete(ctx, c, hashes, in.DeleteFiles)
	case "reannounce", "queue_top", "queue_bottom", "queue_up", "queue_down":
		return manager.Command(ctx, c, in.Action, hashes, "")
	case "force_start", "super_seeding":
		return manager.Command(ctx, c, in.Action, hashes, strconv.FormatBool(*in.Enabled))
	case "set_download_limit":
		return manager.Command(ctx, c, "download_limit", hashes, strconv.FormatInt(*in.Limit, 10))
	case "set_upload_limit":
		return manager.Command(ctx, c, "upload_limit", hashes, strconv.FormatInt(*in.Limit, 10))
	case "set_share_limits":
		return manager.SetShareLimits(ctx, c, hashes, *in.RatioLimit, *in.SeedingTimeLimit)
	}
	// qBittorrent only toggles sequential and first/last piece modes, so only
	// the torrents whose current state differs are sent.
	items, err := manager.List(ctx, c)
	if err != nil {
		return err
	}
	current := make(map[string]Torrent, len(items))
	for _, t := range items {
		current[strings.ToLower(t.Hash)] = t
	}
	var differing []string
	for _, h := range hashes {
		t, ok := current[h]
		if !ok {
			return problem("not_found", "One or more selected torrents are no longer in the built-in client.")
		}
		state := t.SequentialDL
		if in.Action == "first_last_piece" {
			state = t.FirstLastPiece
		}
		if state != *in.Enabled {
			differing = append(differing, h)
		}
	}
	if len(differing) == 0 {
		return nil
	}
	command := "toggle_sequential"
	if in.Action == "first_last_piece" {
		command = "toggle_first_last"
	}
	return manager.Command(ctx, c, command, differing, "")
}

// SetTorrentFilePriority changes file priorities of manual or external
// torrents. ReelScarey's own downloads are refused: their file selection is
// part of the reviewed import and changing it can break that import.
func (s *Service) SetTorrentFilePriority(ctx context.Context, hash string, indexes []int, priority int) error {
	if !torrentHashRE.MatchString(hash) {
		return problem("invalid_selection", "Choose a valid torrent.")
	}
	if priority != 0 && priority != 1 && priority != 6 && priority != 7 {
		return problem("invalid_selection", "Choose priority 0 (do not download), 1 (normal), 6 (high) or 7 (maximum).")
	}
	if len(indexes) == 0 || len(indexes) > maxFileIndexes {
		return problem("invalid_selection", "Choose between 1 and 10000 files.")
	}
	seen := make(map[int]bool, len(indexes))
	for _, i := range indexes {
		if i < 0 || seen[i] {
			return problem("invalid_selection", "Choose each file once.")
		}
		seen[i] = true
	}
	manager, c, err := s.builtinTorrents(ctx)
	if err != nil {
		return err
	}
	found, err := manager.Find(ctx, c, strings.ToLower(hash))
	if err != nil {
		return err
	}
	if found == nil {
		return problem("not_found", "This torrent is no longer in the built-in client.")
	}
	classifyTorrent(found)
	if found.Origin == torrentOriginJob {
		return problem("managed_torrent", "ReelScarey chooses the files for its own downloads. Change this download from Activity instead.")
	}
	files, err := manager.Files(ctx, c, found.Hash)
	if err != nil {
		return err
	}
	exists := make(map[int]bool, len(files))
	for _, f := range files {
		exists[f.Index] = true
	}
	for _, i := range indexes {
		if !exists[i] {
			return problem("invalid_selection", "One or more selected files are not part of this torrent.")
		}
	}
	return manager.SetFilePriority(ctx, c, found.Hash, indexes, priority)
}

// SetTorrentSpeedLimits switches the alternative speed mode only when it
// differs, then sets the global limits of the mode that is active afterwards.
func (s *Service) SetTorrentSpeedLimits(ctx context.Context, in TorrentSpeedLimits) error {
	if in.AltEnabled == nil && in.DownloadLimit == nil && in.UploadLimit == nil {
		return problem("invalid_selection", "Choose a speed limit change.")
	}
	if (in.DownloadLimit != nil && !validRateLimit(in.DownloadLimit)) || (in.UploadLimit != nil && !validRateLimit(in.UploadLimit)) {
		return problem("invalid_selection", "Enter a speed limit between 0 (unlimited) and 2000000000 bytes per second.")
	}
	manager, c, err := s.builtinTorrents(ctx)
	if err != nil {
		return err
	}
	// Serialize the toggle and invalidate the cached read even if a later
	// limit update fails after changing the active mode.
	s.torrentPrefsMu.Lock()
	defer s.torrentPrefsMu.Unlock()
	s.torrentPrefsUntil = time.Time{}
	if in.AltEnabled != nil {
		current, err := manager.SpeedLimitsMode(ctx, c)
		if err != nil {
			return err
		}
		if current != *in.AltEnabled {
			if err = manager.ToggleSpeedLimitsMode(ctx, c); err != nil {
				return err
			}
		}
	}
	if in.DownloadLimit != nil {
		if err = manager.SetGlobalLimit(ctx, c, false, *in.DownloadLimit); err != nil {
			return err
		}
	}
	if in.UploadLimit != nil {
		if err = manager.SetGlobalLimit(ctx, c, true, *in.UploadLimit); err != nil {
			return err
		}
	}
	return nil
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
	tag := manualTorrentTag
	if magnet == "" {
		if info, err := TorrentInfo(metainfo); err == nil && !info.Private {
			tag += "," + manualPublicTag
		}
	}
	return manager.Add(ctx, c, tag, folder, Payload{Magnet: magnet, Torrent: metainfo})
}
