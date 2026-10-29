// Package transfer implements the user-driven movie download workflow. Provider
// adapters never write library files; the leased worker owns reconciliation.
package transfer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/library"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode"
)

type Error struct {
	Code, Message string
	Temporary     bool
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
func remoteRoot(s string) (string, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\\", "/")
	if (!strings.HasPrefix(s, "/") && !regexp.MustCompile(`^[A-Za-z]:/`).MatchString(s)) || strings.HasPrefix(s, "//") || strings.ContainsAny(s, "\x00\r\n") {
		return "", problem("invalid_settings", "Enter an absolute download path as the download client sees it, such as /downloads or D:/Downloads.")
	}
	for _, part := range strings.Split(s, "/") {
		if part == ".." || part == "." {
			return "", problem("invalid_settings", "Download paths cannot contain dot segments.")
		}
	}
	s = path.Clean(s)
	if s == "/" || regexp.MustCompile(`^[A-Za-z]:/?$`).MatchString(s) {
		return "", problem("invalid_settings", "Use a dedicated download directory, not a filesystem root.")
	}
	return strings.TrimRight(s, "/"), nil
}

type IndexerConfig struct {
	ID            string `json:"id,omitempty"`
	Name          string `json:"name,omitempty"`
	Kind          string `json:"kind,omitempty"`
	Cookie        string `json:"cookie,omitempty"`
	UserAgent     string `json:"user_agent,omitempty"`
	FeedURL       string `json:"feed_url,omitempty"`
	FreeleechOnly bool   `json:"freeleech_only,omitempty"`
	Category      string `json:"category,omitempty"`
	URL           string `json:"url"`
	APIKey        string `json:"api_key"`
	Generation    string `json:"generation"`
}
type ClientConfig struct {
	LocalFolder                       string `json:"local_folder,omitempty"`
	URL                               string `json:"url"`
	Username                          string `json:"username"`
	Password                          string `json:"password"`
	DownloadPath                      string `json:"download_path"`
	APIKey                            string `json:"api_key,omitempty"`
	profileKey, kind, label           string
	profileRevision, defaultsRevision int
	ciphertext                        []byte
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
}
type Candidate struct {
	Release
	AnimeOrder                    *library.AnimeOrderSnapshot `json:"anime_order,omitempty"`
	AnimeSearch                   *AnimeSearchSnapshot        `json:"anime_search,omitempty"`
	Kind                          string                      `json:"kind,omitempty"`
	Targets                       []TVTarget                  `json:"targets,omitempty"`
	URL, Magnet, Hash, Generation string
}
type Payload struct {
	NZB     []byte `json:"nzb,omitempty"`
	Magnet  string `json:"magnet,omitempty"`
	Torrent []byte `json:"torrent,omitempty"`
}
type Torrent struct {
	Hash       string  `json:"hash"`
	Name       string  `json:"name"`
	State      string  `json:"state"`
	SavePath   string  `json:"save_path"`
	Progress   float64 `json:"progress"`
	AmountLeft int64   `json:"amount_left"`
	Size       int64   `json:"total_size"`
	Completed  int64   `json:"completed"`
	Tags       string  `json:"tags"`
}
type TorrentFile struct {
	Index    int     `json:"index"`
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"`
	Priority int     `json:"priority"`
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
	Fetch(context.Context, IndexerConfig, Candidate) (Payload, string, error)
}
