package transfer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
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
