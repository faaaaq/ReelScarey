// Package transfer implements the user-driven movie download workflow. Provider
// adapters never write library files; the leased worker owns reconciliation.
package transfer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/library"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Error struct {
	Code, Message string
	Temporary     bool
	// RetryAt is an optional unix-seconds timestamp supplied by the provider
	// (for example an HTTP 429 Retry-After header). When it is later than the
	// computed backoff, the worker waits until it instead.
	RetryAt int64
}

// retryAfterSeconds parses a Retry-After header into a unix-seconds retry
// time, clamped to 1..3600 seconds from now. A missing or malformed header
// yields 0, leaving the caller's own backoff in charge.
func retryAfterSeconds(header string) int64 {
	seconds, err := strconv.ParseInt(strings.TrimSpace(header), 10, 64)
	if err != nil || seconds < 1 {
		return 0
	}
	if seconds > 3600 {
		seconds = 3600
	}
	return time.Now().Unix() + seconds
}

func (e *Error) Error() string           { return e.Message }
func problem(code, message string) error { return &Error{Code: code, Message: message} }
func unavailable(message string) error {
	return &Error{Code: "upstream_unavailable", Message: message, Temporary: true}
}

var ErrLease = errors.New("download lease was lost")

func ID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func textOK(s string, max int) bool {
	return len(s) > 0 && len(s) <= max && !strings.ContainsFunc(s, unicode.IsControl)
}
func endpoint(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.Contains(u.Path, "..") || strings.ContainsAny(raw, "\r\n\\") {
		return nil, problem("invalid_settings", "Use an HTTP or HTTPS service URL without credentials, query parameters, or fragments.")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u, nil
}
func newHTTP() *http.Client {
	return &http.Client{Timeout: 12 * time.Second, Transport: &http.Transport{Proxy: nil, MaxIdleConns: 10, IdleConnTimeout: time.Minute}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

var (
	driveRootRE  = regexp.MustCompile(`^[A-Za-z]:/`)
	driveOnlyRE  = regexp.MustCompile(`^[A-Za-z]:/?$`)
	nonWordRunRE = regexp.MustCompile(`[^\pL\pN]+`)
	queryBreakRE = regexp.MustCompile(`[?#]+`)
	apostropheRE = strings.NewReplacer("'", "", "\u2019", "", "\u2018", "")
)

// normalizeSearchText lowercases and reduces a title to the letter/number
// tokens used for result matching. Apostrophes are dropped rather than spaced
// so "Carr's" matches releases written "Carrs".
func normalizeSearchText(s string) string {
	s = apostropheRE.Replace(s)
	return strings.ToLower(strings.TrimSpace(nonWordRunRE.ReplaceAllString(s, " ")))
}

// trackerQuery removes only characters that break tracker search forms. A
// question mark (or fragment marker) in a show title such as "Am I the
// Asshole?" otherwise produces zero results even though matching releases
// exist. Meaningful search characters such as "&" are preserved, and the
// original query is still used for result matching.
func trackerQuery(query string) string {
	query = apostropheRE.Replace(query)
	query = queryBreakRE.ReplaceAllString(query, " ")
	return strings.Join(strings.Fields(query), " ")
}

func remoteRoot(s string) (string, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\\", "/")
	if (!strings.HasPrefix(s, "/") && !driveRootRE.MatchString(s)) || strings.HasPrefix(s, "//") || strings.ContainsAny(s, "\x00\r\n") {
		return "", problem("invalid_settings", "Enter an absolute download path as the download client sees it, such as /downloads or D:/Downloads.")
	}
	for _, part := range strings.Split(s, "/") {
		if part == ".." || part == "." {
			return "", problem("invalid_settings", "Download paths cannot contain dot segments.")
		}
	}
	s = path.Clean(s)
	if s == "/" || driveOnlyRE.MatchString(s) {
		return "", problem("invalid_settings", "Use a dedicated download directory, not a filesystem root.")
	}
	return strings.TrimRight(s, "/"), nil
}

type IndexerConfig struct {
	ID            string            `json:"id,omitempty"`
	Name          string            `json:"name,omitempty"`
	Kind          string            `json:"kind,omitempty"`
	Cookie        string            `json:"cookie,omitempty"`
	UserAgent     string            `json:"user_agent,omitempty"`
	FeedURL       string            `json:"feed_url,omitempty"`
	FreeleechOnly bool              `json:"freeleech_only,omitempty"`
	Category      string            `json:"category,omitempty"`
	URL           string            `json:"url"`
	APIKey        string            `json:"api_key"`
	Definition    string            `json:"definition,omitempty"`
	Settings      map[string]string `json:"settings,omitempty"`
	Generation    string            `json:"generation"`
}
type ClientConfig struct {
	LocalFolder  string `json:"local_folder,omitempty"`
	URL          string `json:"url"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	DownloadPath string `json:"download_path"`
	APIKey       string `json:"api_key,omitempty"`
	// PeerPort is the BitTorrent listen port of the app-supervised built-in
	// client. External clients manage their own listen port, so it stays 0.
	PeerPort int `json:"peer_port,omitempty"`
	// The built-in Usenet client stores its NNTP provider inside the same
	// encrypted client configuration. These fields are never returned by the
	// API; the safe status exposes host/port/TLS/connections only.
	NewsHost                string `json:"news_host,omitempty"`
	NewsPort                int    `json:"news_port,omitempty"`
	NewsTLS                 bool   `json:"news_tls,omitempty"`
	NewsUsername            string `json:"news_username,omitempty"`
	NewsPassword            string `json:"news_password,omitempty"`
	NewsConnections         int    `json:"news_connections,omitempty"`
	profileKey, kind, label string
	profileRevision         int
	defaultsRevision        int
	ciphertext              []byte
}
type Release struct {
	Protocol    string `json:"protocol,omitempty"`
	IndexerID   string `json:"indexer_id,omitempty"`
	IndexerName string `json:"indexer,omitempty"`
	Freeleech   bool   `json:"freeleech,omitempty"`
	ID          string `json:"id"`
	MovieID     int64  `json:"movie_id"`
	Title       string `json:"title"`
	Size        int64  `json:"size_bytes"`
	Seeders     *int   `json:"seeders"`
	Peers       *int   `json:"peers"`
	Published   string `json:"published_at"`
	// InfoURL is the tracker's own details page when the provider supplied
	// one. It is never synthesized, fetched server-side or treated as a
	// download link.
	InfoURL string `json:"info_url"`
	// Pack marks a season/range/complete-series release found by a season
	// search. Single-episode searches leave it false.
	Pack bool `json:"pack,omitempty"`
}
type Candidate struct {
	Release
	Announce                      *AnnounceRef                `json:"announce,omitempty"`
	AnimeOrder                    *library.AnimeOrderSnapshot `json:"anime_order,omitempty"`
	AnimeSearch                   *AnimeSearchSnapshot        `json:"anime_search,omitempty"`
	Kind                          string                      `json:"kind,omitempty"`
	Targets                       []TVTarget                  `json:"targets,omitempty"`
	URL, Magnet, Hash, Generation string
	// Guid is the feed's provider identity. It is never returned by the API and
	// is only used to deduplicate recent-release entries between checks.
	Guid string `json:"-"`
	// Manual torrents are owner-supplied metainfo bound to one title: no
	// indexer is involved, so generation fencing and network fetch do not
	// apply, and the bytes are queued exactly as uploaded.
	Manual  bool   `json:"manual,omitempty"`
	Torrent []byte `json:"torrent,omitempty"`
}
type Payload struct {
	NZB     []byte `json:"nzb,omitempty"`
	Magnet  string `json:"magnet,omitempty"`
	Torrent []byte `json:"torrent,omitempty"`
}
type Torrent struct {
	Hash         string  `json:"hash"`
	Name         string  `json:"name"`
	State        string  `json:"state"`
	SavePath     string  `json:"save_path"`
	Progress     float64 `json:"progress"`
	AmountLeft   int64   `json:"amount_left"`
	Size         int64   `json:"total_size"`
	Completed    int64   `json:"completed"`
	Tags         string  `json:"tags"`
	Category     string  `json:"category"`
	Downloaded   int64   `json:"downloaded"`
	Uploaded     int64   `json:"uploaded"`
	DLSpeed      int64   `json:"dlspeed"`
	UPSpeed      int64   `json:"upspeed"`
	ETA          int64   `json:"eta"`
	Ratio        float64 `json:"ratio"`
	NumSeeds     int     `json:"num_seeds"`
	NumLeechs    int     `json:"num_leechs"`
	AddedOn      int64   `json:"added_on"`
	CompletionOn int64   `json:"completion_on"`
	ContentPath  string  `json:"content_path"`

	Priority          int     `json:"priority"`
	NumComplete       int     `json:"num_complete"`
	NumIncomplete     int     `json:"num_incomplete"`
	DLLimit           int64   `json:"dl_limit"`
	UPLimit           int64   `json:"up_limit"`
	SequentialDL      bool    `json:"seq_dl"`
	FirstLastPiece    bool    `json:"f_l_piece_prio"`
	ForceStart        bool    `json:"force_start"`
	SuperSeeding      bool    `json:"super_seeding"`
	AutoTMM           bool    `json:"auto_tmm"`
	TimeActive        int64   `json:"time_active"`
	SeedingTime       int64   `json:"seeding_time"`
	LastActivity      int64   `json:"last_activity"`
	Availability      float64 `json:"availability"`
	RatioLimit        float64 `json:"ratio_limit"`
	SeedingTimeLimit  int64   `json:"seeding_time_limit"`
	DownloadedSession int64   `json:"downloaded_session"`
	UploadedSession   int64   `json:"uploaded_session"`
	// Tracker is always reduced to scheme://host[:port]; private tracker URLs
	// carry passkeys in their path, query or user information.
	Tracker string `json:"tracker"`
	// Origin and JobID are derived by ReelScarey from the torrent tags and are
	// never accepted from the client response.
	Origin string `json:"origin,omitempty"`
	JobID  string `json:"job_id,omitempty"`
}

// UnmarshalJSON redacts the tracker as soon as a client response is decoded,
// so no later caller can forward a raw announce URL by accident.
func (t *Torrent) UnmarshalJSON(b []byte) error {
	type plain Torrent
	var v plain
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*t = Torrent(v)
	t.Tracker = RedactTrackerURL(t.Tracker)
	t.Origin, t.JobID = "", ""
	return nil
}

type TransferInfo struct {
	DLSpeed     int64  `json:"dl_info_speed"`
	UPSpeed     int64  `json:"up_info_speed"`
	Downloaded  int64  `json:"dl_info_data"`
	Uploaded    int64  `json:"up_info_data"`
	Connection  string `json:"connection_status"`
	DHTNodes    int64  `json:"dht_nodes"`
	DLRateLimit int64  `json:"dl_rate_limit"`
	UPRateLimit int64  `json:"up_rate_limit"`
}

type TorrentFile struct {
	Index        int     `json:"index"`
	Name         string  `json:"name"`
	Size         int64   `json:"size"`
	Progress     float64 `json:"progress"`
	Priority     int     `json:"priority"`
	Availability float64 `json:"availability"`
	IsSeed       bool    `json:"is_seed"`
}
type Client interface {
	Test(context.Context, ClientConfig) (string, error)
	Find(context.Context, ClientConfig, string) (*Torrent, error)
	Add(context.Context, ClientConfig, string, string, Payload) error
	Files(context.Context, ClientConfig, string) ([]TorrentFile, error)
}
type Indexer interface {
	Test(context.Context, IndexerConfig) error
	Search(context.Context, IndexerConfig, string) ([]Candidate, error)
	// Recent returns the provider's newest releases without a title query. It
	// backs Sonarr/Radarr-style feed checking.
	Recent(context.Context, IndexerConfig) ([]Candidate, error)
	Fetch(context.Context, IndexerConfig, Candidate) (Payload, string, error)
}
