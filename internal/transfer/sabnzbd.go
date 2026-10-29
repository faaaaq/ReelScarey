package transfer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type SABnzbd struct{ HTTP *http.Client }

func NewSABnzbd() *SABnzbd { return &SABnzbd{newHTTP()} }

// SABnzbd 4 uses prefixed IDs; SABnzbd 5 uses UUIDs. Both are opaque client
// identities, separate from the immutable NZB descriptor checksum.
var sabID = regexp.MustCompile(`^(?:SABnzbd_nzo_[A-Za-z0-9_-]{1,80}|[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12})$`)

const sabCategory = "reelscarey"

type SABJob struct {
	ID, Name, State, Storage string
	Total, Downloaded        int64
	Progress                 float64
	Complete, Failed         bool
}

func (s *SABnzbd) api(ctx context.Context, c ClientConfig, values url.Values, upload []byte, out any) error {
	base, e := endpoint(c.URL)
	if e != nil {
		return e
	}
	base.Path += "/api"
	values.Set("apikey", c.APIKey)
	values.Set("output", "json")
	var req *http.Request
	if upload != nil {
		var b bytes.Buffer
		w := multipart.NewWriter(&b)
		for key, items := range values {
			for _, v := range items {
				if e = w.WriteField(key, v); e != nil {
					return e
				}
			}
		}
		f, e := w.CreateFormFile("name", values.Get("nzbname")+".nzb")
		if e != nil {
			return e
		}
		if _, e = f.Write(upload); e != nil {
			return e
		}
		if e = w.Close(); e != nil {
			return e
		}
		req, e = http.NewRequestWithContext(ctx, http.MethodPost, base.String(), &b)
		if e == nil {
			req.Header.Set("Content-Type", w.FormDataContentType())
		}
	} else {
		base.RawQuery = values.Encode()
		req, e = http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	}
	if e != nil {
		return problem("invalid_settings", "The SABnzbd URL is invalid.")
	}
	req.Header.Set("Accept", "application/json")
	res, e := s.HTTP.Do(req)
	if e != nil {
		return unavailable("SABnzbd could not be reached. Check its address and network connection.")
	}
	defer res.Body.Close()
	if res.StatusCode == 401 || res.StatusCode == 403 {
		return problem("client_auth", "SABnzbd rejected its API key or host access. Use the full API key and allow this host.")
	}
	if res.StatusCode == 429 || res.StatusCode >= 500 {
		return unavailable("SABnzbd is busy or unavailable. Try again later.")
	}
	if res.StatusCode != 200 {
		return problem("client_response", "SABnzbd did not return a successful API response. Redirects are not followed.")
	}
	body, e := bounded(res.Body, 4<<20)
	if e != nil {
		return e
	}
	var failure struct {
		Status *bool  `json:"status"`
		Error  string `json:"error"`
	}
	if json.Unmarshal(body, &failure) == nil && (failure.Error != "" || (failure.Status != nil && !*failure.Status)) {
		return problem("client_rejected", "SABnzbd rejected the request. Check its API key, host permissions and dedicated category.")
	}
	if json.Unmarshal(body, out) != nil {
		return problem("client_response", "SABnzbd returned an invalid or oversized API response.")
	}
	return nil
}
func (s *SABnzbd) Test(ctx context.Context, c ClientConfig) error {
	var v struct {
		Version string `json:"version"`
	}
	if e := s.api(ctx, c, url.Values{"mode": {"version"}}, nil, &v); e != nil {
		return e
	}
	if !textOK(v.Version, 50) {
		return problem("client_response", "This URL did not return a SABnzbd version.")
	}
	var cats struct {
		Categories []string `json:"categories"`
	}
	if e := s.api(ctx, c, url.Values{"mode": {"get_cats"}}, nil, &cats); e != nil {
		return e
	}
	for _, c := range cats.Categories {
		if c == sabCategory {
			return nil
		}
	}
	return problem("invalid_settings", "Create a SABnzbd category named reelscarey with folder ReelScarey, no sorting and no script, then test again.")
}
func (s *SABnzbd) Add(ctx context.Context, c ClientConfig, tag string, nzb []byte) (string, error) {
	if _, e := NZBIdentity(nzb); e != nil {
		return "", e
	}
	var out struct {
		Status bool     `json:"status"`
		IDs    []string `json:"nzo_ids"`
	}
	e := s.api(ctx, c, url.Values{"mode": {"addfile"}, "nzbname": {tag}, "cat": {sabCategory}, "script": {"None"}, "priority": {"0"}, "pp": {"2"}}, nzb, &out)
	if e != nil {
		var p *Error
		if errors.As(e, &p) && p.Code == "client_rejected" {
			return "", problem("client_add_rejected", "SABnzbd rejected the NZB submission. Check the client before retrying.")
		}
		return "", e
	}
	if !out.Status || len(out.IDs) != 1 || !sabID.MatchString(out.IDs[0]) {
		return "", problem("usenet_submission_unknown", "SABnzbd's submission response could not be verified. Retry tracking to reconcile by the unique job name; an uncertain NZB is never resubmitted.")
	}
	return out.IDs[0], nil
}

type sabSlot struct {
	ID              string `json:"nzo_id"`
	Filename        string `json:"filename"`
	Name            string `json:"name"`
	NZBName         string `json:"nzb_name"`
	State           string `json:"status"`
	Category        string `json:"cat"`
	HistoryCategory string `json:"category"`
	MB              string `json:"mb"`
	Left            string `json:"mbleft"`
	Bytes           int64  `json:"bytes"`
	Downloaded      int64  `json:"downloaded"`
	Loaded          bool   `json:"loaded"`
	Storage         string `json:"storage"`
}

func (s *SABnzbd) Find(ctx context.Context, c ClientConfig, id, tag string) (*SABJob, error) {
	if id != "" && !sabID.MatchString(id) {
		return nil, problem("client_response", "The saved SABnzbd job identity is invalid.")
	}
	var found *SABJob
	for _, mode := range []string{"queue", "history"} {
		values := url.Values{"mode": {mode}, "limit": {"1000"}, "start": {"0"}, "cat": {sabCategory}, "search": {tag}}
		// Keep the exact-name lookup even with a saved ID: it detects duplicate jobs
		// and never treats a changed/reused client record as ownership evidence.
		var out map[string]struct {
			Slots []sabSlot `json:"slots"`
			Total int       `json:"noofslots_total"`
			Count int       `json:"noofslots"`
		}
		if e := s.api(ctx, c, values, nil, &out); e != nil {
			return nil, e
		}
		page, ok := out[mode]
		if !ok || len(page.Slots) > 1000 || page.Total > 1000 || page.Count > 1000 {
			return nil, problem("client_response", "SABnzbd's job listing could not be bounded. Keep the unique job in its reelscarey category and retain its history.")
		}
		for _, slot := range page.Slots {
			name := slot.Filename
			cat := slot.Category
			if mode == "history" {
				name = slot.Name
				if name == "" {
					name = slot.NZBName
				}
				cat = slot.HistoryCategory
			}
			matches := strings.TrimSuffix(name, ".nzb") == tag
			if slot.ID == id && !matches {
				return nil, problem("client_changed", "The SABnzbd job was renamed. Restore its original ReelScarey job name before tracking it.")
			}
			if !matches {
				continue
			}
			if !sabID.MatchString(slot.ID) || cat != sabCategory || (id != "" && id != slot.ID) || found != nil {
				return nil, problem("client_conflict", "Multiple or changed SABnzbd jobs match this download. Resolve them in SABnzbd before retrying tracking.")
			}
			j := &SABJob{ID: slot.ID, Name: name, State: slot.State, Storage: slot.Storage}
			if !textOK(j.State, 50) {
				return nil, problem("client_response", "SABnzbd returned an invalid job status.")
			}
			if mode == "history" {
				j.Complete = slot.State == "Completed" && !slot.Loaded
				j.Failed = slot.State == "Failed"
				j.Total = slot.Bytes
				j.Downloaded = slot.Downloaded
				if j.Complete {
					j.Progress = 1
				}
			} else {
				total, e1 := strconv.ParseFloat(slot.MB, 64)
				left, e2 := strconv.ParseFloat(slot.Left, 64)
				if e1 != nil || e2 != nil || math.IsNaN(total) || math.IsInf(total, 0) || math.IsNaN(left) || math.IsInf(left, 0) || total < 0 || total > 2<<20 || left < 0 || left > total {
					return nil, problem("client_response", "SABnzbd returned invalid download progress.")
				}
				j.Total = int64(total * (1 << 20))
				j.Downloaded = int64((total - left) * (1 << 20))
				if total > 0 {
					j.Progress = (total - left) / total
				}
			}
			if j.Total < 0 || j.Downloaded < 0 {
				return nil, problem("client_response", "SABnzbd returned invalid file sizes.")
			}
			found = j
		}
	}
	return found, nil
}
