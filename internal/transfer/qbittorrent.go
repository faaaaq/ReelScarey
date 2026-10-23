package transfer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

type QBittorrent struct {
	mu       sync.Mutex
	sessions map[string]*http.Client
}

func NewQBittorrent() *QBittorrent { return &QBittorrent{sessions: map[string]*http.Client{}} }
func clientKey(c ClientConfig) string {
	sum := sha256.Sum256([]byte(c.URL + "\x00" + c.Username + "\x00" + c.Password))
	return hex.EncodeToString(sum[:])
}
func qbtRequest(ctx context.Context, c ClientConfig, client *http.Client, method, api, contentType string, body io.Reader) ([]byte, int, error) {
	u, err := endpoint(c.URL)
	if err != nil {
		return nil, 0, err
	}
	origin := u.Scheme + "://" + u.Host
	u.Path += "/api/v2/" + strings.SplitN(api, "?", 2)[0]
	if parts := strings.SplitN(api, "?", 2); len(parts) == 2 {
		u.RawQuery = parts[1]
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, 0, problem("invalid_settings", "The qBittorrent URL is invalid.")
	}
	req.Header.Set("Referer", origin+"/")
	req.Header.Set("Origin", origin)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, unavailable("qBittorrent could not be reached. Check its URL, TLS certificate, and network connection.")
	}
	defer resp.Body.Close()
	data, err := bounded(resp.Body, 4<<20)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode == 403 || resp.StatusCode == 401 {
		return nil, resp.StatusCode, problem("client_auth", "qBittorrent rejected the session or credentials.")
	}
	if resp.StatusCode >= 500 || resp.StatusCode == 429 {
		return nil, resp.StatusCode, unavailable("qBittorrent is temporarily unavailable.")
	}
	if resp.StatusCode != 200 && resp.StatusCode != 202 && resp.StatusCode != 204 {
		operation := strings.SplitN(api, "?", 2)[0]
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			return nil, resp.StatusCode, problem("client_redirect", fmt.Sprintf("qBittorrent redirected %s (HTTP %d). Use its direct Web UI API address; credentials are not forwarded through redirects.", operation, resp.StatusCode))
		}
		return nil, resp.StatusCode, problem("client_response", fmt.Sprintf("qBittorrent returned HTTP %d for %s. Check that the Web UI URL exposes its /api/v2 endpoints.", resp.StatusCode, operation))
	}
	return data, resp.StatusCode, nil
}
func (q *QBittorrent) session(ctx context.Context, c ClientConfig) (*http.Client, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	key := clientKey(c)
	if client := q.sessions[key]; client != nil {
		return client, nil
	}
	client := newHTTP()
	jar, _ := cookiejar.New(nil)
	client.Jar = jar
	data, status, err := qbtRequest(ctx, c, client, http.MethodPost, "auth/login", "application/x-www-form-urlencoded", strings.NewReader(url.Values{"username": {c.Username}, "password": {c.Password}}.Encode()))
	if err != nil {
		return nil, err
	}
	u, _ := endpoint(c.URL)
	u.Path += "/"
	hasSID := false
	for _, cookie := range jar.Cookies(u) {
		modernName := false
		if strings.HasPrefix(cookie.Name, "QBT_SID_") {
			port, e := strconv.Atoi(strings.TrimPrefix(cookie.Name, "QBT_SID_"))
			modernName = e == nil && port >= 1 && port <= 65535
		}
		if (cookie.Name == "SID" || modernName) && cookie.Value != "" {
			hasSID = true
		}
	}
	if !((status == 200 && strings.TrimSpace(string(data)) == "Ok.") || (status == 204 && len(data) == 0)) || !hasSID {
		return nil, problem("client_auth", "qBittorrent rejected the username or password. Use its Web UI credentials.")
	}
	if len(q.sessions) >= 4 {
		q.sessions = map[string]*http.Client{}
	}
	q.sessions[key] = client
	return client, nil
}
func (q *QBittorrent) request(ctx context.Context, c ClientConfig, method, api, ct string, body io.Reader) ([]byte, error) {
	client, err := q.session(ctx, c)
	if err != nil {
		return nil, err
	}
	data, status, err := qbtRequest(ctx, c, client, method, api, ct, body)
	if status == 401 || status == 403 {
		q.mu.Lock()
		delete(q.sessions, clientKey(c))
		q.mu.Unlock()
		return nil, unavailable("The qBittorrent session expired. Reconnecting with the saved credentials.")
	}
	return data, err
}
func (q *QBittorrent) Test(ctx context.Context, c ClientConfig) (string, error) {
	data, err := q.request(ctx, c, http.MethodGet, "app/webapiVersion", "", nil)
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(data))
	parts := strings.Split(version, ".")
	valid := len(parts) >= 2
	major, minor, patch := 0, 0, 0
	if valid {
		major, _ = strconv.Atoi(parts[0])
		minor, _ = strconv.Atoi(parts[1])
		if len(parts) > 2 {
			patch, _ = strconv.Atoi(parts[2])
		}
	}
	if !valid || major != 2 || (minor < 8) || (minor == 8 && patch < 3) {
		return "", problem("client_version", "Use qBittorrent with Web API 2.8.3 or newer (qBittorrent 4.3.3+).")
	}
	return version, nil
}
func (q *QBittorrent) Find(ctx context.Context, c ClientConfig, hash string) (*Torrent, error) {
	data, err := q.request(ctx, c, http.MethodGet, "torrents/info?"+url.Values{"hashes": {hash}}.Encode(), "", nil)
	if err != nil {
		return nil, err
	}
	var out []Torrent
	if json.Unmarshal(data, &out) != nil || len(out) > 1 {
		return nil, problem("client_response", "qBittorrent returned invalid torrent details.")
	}
	if len(out) == 0 {
		return nil, nil
	}
	if strings.ToLower(out[0].Hash) != hash {
		return nil, problem("client_response", "qBittorrent returned an unexpected torrent hash.")
	}
	return &out[0], nil
}
func (q *QBittorrent) Files(ctx context.Context, c ClientConfig, hash string) ([]TorrentFile, error) {
	data, err := q.request(ctx, c, http.MethodGet, "torrents/files?"+url.Values{"hash": {hash}}.Encode(), "", nil)
	if err != nil {
		return nil, err
	}
	var out []TorrentFile
	if json.Unmarshal(data, &out) != nil || len(out) > 10000 {
		return nil, problem("client_response", "qBittorrent returned an invalid file list.")
	}
	return out, nil
}
func (q *QBittorrent) Add(ctx context.Context, c ClientConfig, tag, savePath string, p Payload) error {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	fields := map[string]string{"savepath": savePath, "tags": tag, "autoTMM": "false", "skip_checking": "false", "paused": "false", "stopped": "false"}
	if p.Magnet != "" {
		fields["urls"] = p.Magnet
	}
	for k, v := range fields {
		if err := form.WriteField(k, v); err != nil {
			return err
		}
	}
	if len(p.Torrent) > 0 {
		file, err := form.CreateFormFile("torrents", "release.torrent")
		if err != nil {
			return err
		}
		if _, err = file.Write(p.Torrent); err != nil {
			return err
		}
	}
	if err := form.Close(); err != nil {
		return err
	}
	data, err := q.request(ctx, c, http.MethodPost, "torrents/add", form.FormDataContentType(), &body)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(data)) == "Ok." || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	var result struct {
		Success *int `json:"success_count"`
		Failure *int `json:"failure_count"`
		Pending *int `json:"pending_count"`
	}
	if json.Unmarshal(data, &result) == nil && result.Success != nil && result.Failure != nil && result.Pending != nil && *result.Failure == 0 && *result.Success >= 0 && *result.Pending >= 0 && *result.Success+*result.Pending == 1 {
		return nil
	}
	return problem("client_add_rejected", "qBittorrent did not accept the torrent. Check the client and retry.")
}
