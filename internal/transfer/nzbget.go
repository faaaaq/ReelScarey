package transfer

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// NZBGet uses the documented positional JSON-RPC API. It never changes server
// configuration, removes jobs, runs scripts or retries an uncertain append.
type NZBGet struct{ HTTP *http.Client }

func NewNZBGet() *NZBGet { return &NZBGet{HTTP: newHTTP()} }
func (n *NZBGet) rpc(ctx context.Context, c ClientConfig, method string, params []any, out any) error {
	u, e := endpoint(c.URL)
	if e != nil {
		return e
	}
	u.Path += "/jsonrpc"
	if params == nil {
		params = []any{}
	}
	body, e := json.Marshal(map[string]any{"method": method, "params": params, "id": 1})
	if e != nil {
		return e
	}
	request, e := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(body))
	if e != nil {
		return problem("invalid_settings", "The NZBGet address is invalid.")
	}
	request.SetBasicAuth(c.Username, c.Password)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, e := n.HTTP.Do(request)
	if e != nil {
		return unavailable("NZBGet could not be reached. Check its address and network connection.")
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return problem("client_auth", "NZBGet rejected the username/password or access level.")
	}
	if response.StatusCode == http.StatusTooManyRequests {
		e := &Error{Code: "upstream_unavailable", Message: "NZBGet is limiting requests. Try again later.", Temporary: true}
		e.RetryAt = retryAfterSeconds(response.Header.Get("Retry-After"))
		return e
	}
	if response.StatusCode != 200 {
		return unavailable("NZBGet did not return a successful response. Redirects are not followed.")
	}
	b, e := bounded(response.Body, 4<<20)
	if e != nil {
		return e
	}
	var result struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
		ID     int             `json:"id"`
	}
	if json.Unmarshal(b, &result) != nil || result.ID != 1 {
		return problem("client_response", "NZBGet returned an invalid or oversized response.")
	}
	if len(result.Error) > 0 && string(result.Error) != "null" {
		return problem("client_rejected", "NZBGet rejected the RPC request. Check credentials, API access and client version.")
	}
	if len(result.Result) == 0 || json.Unmarshal(result.Result, out) != nil {
		return problem("client_response", "NZBGet returned an unexpected response shape.")
	}
	return nil
}

type nzbgetOption struct {
	Name  string `json:"Name"`
	Value string `json:"Value"`
}

func (n *NZBGet) Test(ctx context.Context, c ClientConfig) error {
	var version string
	if e := n.rpc(ctx, c, "version", nil, &version); e != nil {
		return e
	}
	if !textOK(version, 100) {
		return problem("client_response", "NZBGet did not return its version.")
	}
	var config []nzbgetOption
	if e := n.rpc(ctx, c, "config", nil, &config); e != nil {
		return e
	}
	if len(config) > 3000 {
		return problem("client_response", "NZBGet's configuration could not be bounded.")
	}
	values := map[string]string{}
	for _, v := range config {
		values[v.Name] = v.Value
	}
	for key, value := range values {
		if strings.HasPrefix(key, "Category") && strings.HasSuffix(key, ".Name") && value == sabCategory {
			prefix := strings.TrimSuffix(key, "Name")
			root, e := remoteRoot(values[prefix+"DestDir"])
			if e != nil || root != path.Join(c.DownloadPath, "ReelScarey") {
				return problem("invalid_settings", "Set the NZBGet reelscarey category destination to <completed folder>/ReelScarey.")
			}
			if values["Extensions"] != "" || values[prefix+"Extensions"] != "" || values["ScriptOrder"] != "" || values[prefix+"Script"] != "" {
				return problem("invalid_settings", "Use an NZBGet profile without post-processing extensions or renaming scripts for ReelScarey downloads.")
			}
			return nil
		}
	}
	return problem("invalid_settings", "Create an NZBGet category named reelscarey with a separate ReelScarey output folder and no extensions.")
}
func (n *NZBGet) Add(ctx context.Context, c ClientConfig, tag string, nzb []byte) (string, error) {
	if _, e := NZBIdentity(nzb); e != nil {
		return "", e
	}
	var id int64
	e := n.rpc(ctx, c, "append", []any{tag + ".nzb", base64.StdEncoding.EncodeToString(nzb), sabCategory, 0, false, false, tag, 0, "SCORE"}, &id)
	if e != nil {
		return "", e
	}
	if id < 1 || id > 2147483647 {
		return "", problem("client_add_rejected", "NZBGet did not accept the NZB. Check duplicate history before retrying.")
	}
	return strconv.FormatInt(id, 10), nil
}

type nzbgetGroup struct {
	ID            int64  `json:"NZBID"`
	Kind          string `json:"Kind"`
	Name          string `json:"NZBName"`
	HistoryName   string `json:"Name"`
	Category      string `json:"Category"`
	DupeKey       string `json:"DupeKey"`
	Status        string `json:"Status"`
	Destination   string `json:"DestDir"`
	Final         string `json:"FinalDir"`
	TotalLow      int64  `json:"FileSizeLo"`
	TotalHigh     int64  `json:"FileSizeHi"`
	RemainingLow  int64  `json:"RemainingSizeLo"`
	RemainingHigh int64  `json:"RemainingSizeHi"`
	Unpack        string `json:"UnpackStatus"`
	Par           string `json:"ParStatus"`
	Delete        string `json:"DeleteStatus"`
	Mark          string `json:"MarkStatus"`
}

func nzbgetBytes(high, low int64) (int64, bool) {
	if high < 0 || high > 512 || low < 0 || low > 4294967295 {
		return 0, false
	}
	return high<<32 | low, true
}
func (n *NZBGet) Find(ctx context.Context, c ClientConfig, id, tag string) (*SABJob, error) {
	if id != "" {
		v, e := strconv.ParseInt(id, 10, 32)
		if e != nil || v < 1 || strconv.FormatInt(v, 10) != id {
			return nil, problem("client_response", "The saved NZBGet job identity is invalid.")
		}
	}
	var found *SABJob
	for _, method := range []string{"listgroups", "history"} {
		var groups []nzbgetGroup
		var params []any
		if method == "history" {
			params = []any{false}
		} else {
			params = []any{0}
		}
		if e := n.rpc(ctx, c, method, params, &groups); e != nil {
			return nil, e
		}
		if len(groups) > 2000 {
			return nil, problem("client_response", "NZBGet queue/history exceeds the bounded tracking limit. Retain the owned job in visible history.")
		}
		for _, g := range groups {
			key := strconv.FormatInt(g.ID, 10)
			name := g.Name
			if name == "" {
				name = g.HistoryName
			}
			matches := strings.TrimSuffix(name, ".nzb") == tag
			if id != "" && key == id && !matches {
				return nil, problem("client_changed", "The NZBGet job was renamed. Restore its original ReelScarey name before tracking.")
			}
			if !matches {
				continue
			}
			if found != nil || g.ID < 1 || g.ID > 2147483647 || g.Kind != "NZB" || g.Category != sabCategory || g.DupeKey != tag || (id != "" && key != id) {
				return nil, problem("client_conflict", "A duplicate or changed NZBGet job matches this download. Resolve it in NZBGet before retrying.")
			}
			total, ok := nzbgetBytes(g.TotalHigh, g.TotalLow)
			left, leftOK := nzbgetBytes(g.RemainingHigh, g.RemainingLow)
			if !ok || !leftOK || total <= 0 || left > total || !textOK(g.Status, 100) {
				return nil, problem("client_response", "NZBGet returned invalid file sizes or status.")
			}
			job := &SABJob{ID: key, Name: name, State: g.Status, Storage: g.Destination, Total: total, Downloaded: total - left, Progress: float64(total-left) / float64(total)}
			if method == "history" {
				if g.Final != "" && g.Final != g.Destination {
					return nil, problem("source_changed", "An NZBGet extension changed the output folder; import is held.")
				}
				switch g.Status {
				case "SUCCESS/ALL", "SUCCESS/UNPACK", "SUCCESS/PAR", "SUCCESS/HEALTH":
					job.Complete = (g.Unpack == "NONE" || g.Unpack == "SUCCESS") && (g.Par == "NONE" || g.Par == "SUCCESS") && (g.Delete == "NONE" || g.Delete == "") && g.Mark != "BAD"
				default:
					job.Failed = true
				}
				if job.Complete {
					job.Downloaded = total
					job.Progress = 1
				} else {
					job.Failed = true
				}
			}
			found = job
		}
	}
	return found, nil
}
