package transfer

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
)

// Transmission 4 retains its original RPC protocol (17/18). Ownership labels
// must be attached atomically by torrent-add; older clients cannot do this.
type Transmission struct{ http *http.Client }

func NewTransmission() *Transmission { return &Transmission{http: newHTTP()} }

func (t *Transmission) call(ctx context.Context, c ClientConfig, method string, args, out any) error {
	u, e := endpoint(c.URL)
	if e != nil {
		return e
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/transmission/rpc"
	}
	body, e := json.Marshal(map[string]any{"method": method, "arguments": args, "tag": 1})
	if e != nil {
		return e
	}
	session := ""
	for attempt := 0; attempt < 2; attempt++ {
		req, e := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
		if e != nil {
			return e
		}
		req.SetBasicAuth(c.Username, c.Password)
		req.Header.Set("Content-Type", "application/json")
		if session != "" {
			req.Header.Set("X-Transmission-Session-Id", session)
		}
		resp, e := t.http.Do(req)
		if e != nil {
			return unavailable("Transmission could not be reached. Check its RPC address and connection.")
		}
		data, readErr := bounded(resp.Body, 4<<20)
		resp.Body.Close()
		if resp.StatusCode == http.StatusConflict && attempt == 0 {
			session = resp.Header.Get("X-Transmission-Session-Id")
			if !textOK(session, 256) {
				return problem("client_response", "Transmission returned an invalid session challenge.")
			}
			continue // A 409 challenge explicitly rejects the operation before execution.
		}
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return problem("client_auth", "Transmission rejected the credentials or RPC access.")
		}
		if resp.StatusCode != 200 || readErr != nil {
			return unavailable("Transmission did not return a readable successful response.")
		}
		var reply struct {
			Result    string          `json:"result"`
			Arguments json.RawMessage `json:"arguments"`
			Tag       int             `json:"tag"`
		}
		if json.Unmarshal(data, &reply) != nil || reply.Tag != 1 || reply.Result != "success" || len(reply.Arguments) == 0 || json.Unmarshal(reply.Arguments, out) != nil {
			return problem("client_response", "Transmission rejected the operation or returned invalid details. Check the client before retrying.")
		}
		return nil
	}
	return problem("client_response", "Transmission did not accept the session challenge.")
}

func (t *Transmission) Test(ctx context.Context, c ClientConfig) (string, error) {
	var info struct {
		Version string `json:"version"`
		RPC     int    `json:"rpc-version"`
		Minimum int    `json:"rpc-version-minimum"`
		Added   bool   `json:"script-torrent-added-enabled"`
		Done    bool   `json:"script-torrent-done-enabled"`
		Seeded  bool   `json:"script-torrent-done-seeding-enabled"`
	}
	if e := t.call(ctx, c, "session-get", map[string]any{}, &info); e != nil {
		return "", e
	}
	if info.RPC < 17 || info.Minimum > 17 || !strings.HasPrefix(info.Version, "4.") {
		return "", problem("client_version", "Use Transmission 4 with RPC 17 compatibility and atomic torrent labels.")
	}
	if info.Added || info.Done || info.Seeded {
		return "", problem("invalid_settings", "Use a Transmission profile with torrent scripts disabled before connecting it.")
	}
	return info.Version, nil
}

type transmissionTorrent struct {
	Hash      string   `json:"hashString"`
	Name      string   `json:"name"`
	Directory string   `json:"downloadDir"`
	Labels    []string `json:"labels"`
	Status    int      `json:"status"`
	Error     int      `json:"error"`
	Size      int64    `json:"sizeWhenDone"`
	Left      int64    `json:"leftUntilDone"`
	Valid     int64    `json:"haveValid"`
	Unchecked int64    `json:"haveUnchecked"`
	Progress  float64  `json:"percentDone"`
	Metadata  float64  `json:"metadataPercentComplete"`
	Files     []struct {
		Name      string `json:"name"`
		Length    int64  `json:"length"`
		Completed int64  `json:"bytesCompleted"`
	} `json:"files"`
	Stats []struct {
		Wanted bool `json:"wanted"`
	} `json:"fileStats"`
}

func (t *Transmission) get(ctx context.Context, c ClientConfig, hash string, files bool) (*transmissionTorrent, error) {
	if len(hash) != 40 || strings.ContainsAny(hash, ", \n\r") {
		return nil, problem("client_response", "Choose a valid torrent hash.")
	}
	fields := []string{"hashString", "name", "downloadDir", "labels", "status", "error", "sizeWhenDone", "leftUntilDone", "haveValid", "haveUnchecked", "percentDone", "metadataPercentComplete"}
	if files {
		fields = append(fields, "files", "fileStats")
	}
	var reply struct {
		Torrents []transmissionTorrent `json:"torrents"`
	}
	if e := t.call(ctx, c, "torrent-get", map[string]any{"ids": []string{hash}, "fields": fields}, &reply); e != nil {
		return nil, e
	}
	if len(reply.Torrents) == 0 {
		return nil, nil
	}
	if len(reply.Torrents) != 1 || strings.ToLower(reply.Torrents[0].Hash) != hash {
		return nil, problem("client_response", "Transmission returned another torrent identity.")
	}
	v := &reply.Torrents[0]
	if v.Size < 0 || v.Left < 0 || v.Left > v.Size || v.Valid < 0 || v.Unchecked < 0 || len(v.Labels) > 100 || v.Status < 0 || v.Status > 6 {
		return nil, problem("client_response", "Transmission returned invalid torrent facts.")
	}
	for _, label := range v.Labels {
		if !textOK(label, 256) || strings.Contains(label, ",") {
			return nil, problem("client_response", "Transmission returned an invalid ownership label.")
		}
	}
	return v, nil
}

func (t *Transmission) Find(ctx context.Context, c ClientConfig, hash string) (*Torrent, error) {
	v, e := t.get(ctx, c, hash, false)
	if e != nil || v == nil {
		return nil, e
	}
	state := "downloading"
	if v.Status == 1 || v.Status == 2 {
		state = "checkingDL"
	}
	if v.Error != 0 {
		state = "error"
	} else if v.Metadata == 1 && v.Progress == 1 && v.Left == 0 && v.Unchecked == 0 && v.Valid >= v.Size && (v.Status == 0 || v.Status == 5 || v.Status == 6) {
		state = "uploading"
	}
	return &Torrent{Hash: hash, Name: v.Name, State: state, SavePath: v.Directory, Tags: strings.Join(v.Labels, ","), Size: v.Size, Completed: v.Size - v.Left, AmountLeft: v.Left, Progress: v.Progress}, nil
}

func (t *Transmission) Files(ctx context.Context, c ClientConfig, hash string) ([]TorrentFile, error) {
	v, e := t.get(ctx, c, hash, true)
	if e != nil {
		return nil, e
	}
	if v == nil || len(v.Files) == 0 || len(v.Files) > 10000 || len(v.Files) != len(v.Stats) {
		return nil, problem("client_response", "Transmission returned an incomplete file list.")
	}
	out := make([]TorrentFile, 0, len(v.Files))
	for i, f := range v.Files {
		if f.Length < 0 || f.Completed < 0 || f.Completed > f.Length {
			return nil, problem("client_response", "Transmission returned invalid file progress.")
		}
		progress := 0.0
		if f.Length > 0 {
			progress = float64(f.Completed) / float64(f.Length)
		}
		priority := 0
		if v.Stats[i].Wanted {
			priority = 1
		}
		out = append(out, TorrentFile{Index: i, Name: f.Name, Size: f.Length, Progress: progress, Priority: priority})
	}
	return out, nil
}

func (t *Transmission) Add(ctx context.Context, c ClientConfig, tag, savePath string, p Payload) error {
	if _, e := t.Test(ctx, c); e != nil {
		return e
	}
	args := map[string]any{"download-dir": savePath, "labels": []string{tag}, "paused": false}
	var hash string
	var e error
	if len(p.Torrent) > 0 && p.Magnet == "" {
		hash, e = TorrentHash(p.Torrent)
		args["metainfo"] = base64.StdEncoding.EncodeToString(p.Torrent)
	} else if p.Magnet != "" && len(p.Torrent) == 0 {
		hash, e = MagnetHash(p.Magnet)
		args["filename"] = p.Magnet
	} else {
		return problem("payload_mismatch", "Choose exactly one torrent source.")
	}
	if e != nil || len(p.NZB) > 0 {
		return problem("payload_mismatch", "The saved torrent source is invalid.")
	}
	var reply struct {
		Added *struct {
			Hash string `json:"hashString"`
		} `json:"torrent-added"`
		Duplicate json.RawMessage `json:"torrent-duplicate"`
	}
	if e = t.call(ctx, c, "torrent-add", args, &reply); e != nil {
		return e
	}
	if len(reply.Duplicate) > 0 {
		return problem("torrent_exists", "This torrent already exists in Transmission and was left unchanged.")
	}
	if reply.Added == nil || strings.ToLower(reply.Added.Hash) != hash {
		return problem("client_response", "Transmission did not confirm the expected torrent identity.")
	}
	return nil
}

func (s *Service) torrentClient(c ClientConfig) Client {
	if c.kind == "transmission" {
		return s.transmission
	}
	return s.client
}
