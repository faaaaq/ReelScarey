package subtitle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/faaaaq/ReelScarey/internal/quality"
)

type session struct {
	token, host string
	until       time.Time
	err         error
}
type OpenSubtitles struct {
	client   *http.Client
	mu       sync.Mutex
	loginMu  sync.Mutex
	paceMu   sync.Mutex
	sessions map[string]session
	last     time.Time
	spacing  time.Duration
}

func NewOpenSubtitles() *OpenSubtitles {
	return &OpenSubtitles{client: &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: nil, MaxIdleConns: 4, IdleConnTimeout: time.Minute}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, sessions: map[string]session{}, spacing: time.Second}
}
func (o *OpenSubtitles) wait(ctx context.Context) error {
	// Pacing is its own short critical section so one provider call's HTTP
	// round trip never blocks the next job's preparation.
	o.paceMu.Lock()
	defer o.paceMu.Unlock()
	delay := time.Until(o.last.Add(o.spacing))
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	o.last = time.Now()
	return nil
}
func readBounded(r io.Reader, max int) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, int64(max)+1))
	if e != nil {
		return nil, retry("The subtitle provider response could not be read.", time.Minute)
	}
	if len(b) > max {
		return nil, problem("provider_response", "The subtitle provider response exceeded the supported size.")
	}
	return b, nil
}
func (o *OpenSubtitles) api(ctx context.Context, c Credentials, host, token, method, endpoint string, body any, out any) error {
	if host != "api.opensubtitles.com" && host != "vip-api.opensubtitles.com" {
		return problem("provider_response", "OpenSubtitles returned an unsupported API host.")
	}
	if e := o.wait(ctx); e != nil {
		return e
	}
	var payload io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return e
		}
		payload = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(ctx, method, "https://"+host+"/api/v1"+endpoint, payload)
	if e != nil {
		return problem("provider_response", "The provider request could not be created.")
	}
	req.Header.Set("Api-Key", c.APIKey)
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, e := o.client.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return retry("OpenSubtitles could not be reached. Check the server's network connection.", time.Minute)
	}
	defer response.Body.Close()
	b, e := readBounded(response.Body, 2<<20)
	if e != nil {
		return e
	}
	if response.StatusCode != 200 {
		switch response.StatusCode {
		case 401:
			return problem("provider_auth", "OpenSubtitles rejected the account credentials. Update or test the connection before retrying.")
		case 403:
			return problem("provider_access", "OpenSubtitles denied API access. Check the API key and registered application name.")
		case 406:
			return &Error{Code: "provider_quota", Message: "OpenSubtitles download allowance is exhausted. Downloads will wait for the reset.", Temporary: true, RetryAt: resetTime(b)}
		case 429:
			delay := int64(60)
			if n, e := strconv.ParseInt(response.Header.Get("Retry-After"), 10, 64); e == nil {
				delay = max(1, min(n, 86400))
			} else if t, e := http.ParseTime(response.Header.Get("Retry-After")); e == nil {
				delay = max(1, min(t.Unix()-time.Now().Unix(), 86400))
			}
			return &Error{Code: "provider_rate_limit", Message: "OpenSubtitles asked ReelScarey to slow down. Requests will resume after its cooldown.", Temporary: true, RetryAt: time.Now().Unix() + delay}
		}
		if response.StatusCode >= 500 {
			return retry("OpenSubtitles is temporarily unavailable.", time.Minute)
		}
		return problem("provider_response", "OpenSubtitles did not return a successful API response. Check account access and application registration.")
	}
	if e = json.Unmarshal(b, out); e != nil {
		return problem("provider_response", "OpenSubtitles returned invalid response data.")
	}
	return nil
}
func resetTime(b []byte) int64 {
	var r struct {
		Reset json.RawMessage `json:"reset_time"`
		UTC   string          `json:"reset_time_utc"`
	}
	_ = json.Unmarshal(b, &r)
	now := time.Now().Unix()
	var reset int64
	if json.Unmarshal(r.Reset, &reset) != nil {
		var value string
		if json.Unmarshal(r.Reset, &value) == nil {
			reset, _ = strconv.ParseInt(value, 10, 64)
		}
	}
	if reset > now && reset <= now+3*86400 {
		return reset
	}
	for _, format := range []string{time.RFC3339, "2006-01-02T15:04:05.000Z", "2006-01-02 15:04:05 MST"} {
		if t, e := time.Parse(format, r.UTC); e == nil && t.Unix() > now {
			return min(t.Unix(), now+3*86400)
		}
	}
	return now + 24*3600
}
func (o *OpenSubtitles) cachedSession(generation string) (session, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	s, ok := o.sessions[generation]
	return s, ok
}

func (o *OpenSubtitles) storeSession(generation string, s session) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.sessions) > 8 {
		clear(o.sessions)
	}
	o.sessions[generation] = s
}

func (o *OpenSubtitles) clearSession(generation string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.sessions, generation)
}

func (o *OpenSubtitles) login(ctx context.Context, c Credentials, force bool) (session, error) {
	// Serialize logins only; a cached session and the provider API calls do not
	// hold a process-wide lock.
	o.loginMu.Lock()
	defer o.loginMu.Unlock()
	if force {
		o.clearSession(c.Generation)
	}
	if s, ok := o.cachedSession(c.Generation); ok && (s.err != nil || s.until.After(time.Now())) {
		return s, s.err
	}
	var raw struct {
		Token string `json:"token"`
		Base  string `json:"base_url"`
	}
	e := o.api(ctx, c, "api.opensubtitles.com", "", http.MethodPost, "/login", map[string]string{"username": c.Username, "password": c.Password}, &raw)
	s := session{}
	if e != nil {
		var p *Error
		if errors.As(e, &p) && (p.Code == "provider_auth" || p.Code == "provider_access") {
			s.err = e
			o.storeSession(c.Generation, s)
		}
		return s, e
	}
	host := strings.TrimPrefix(strings.TrimSuffix(raw.Base, "/"), "https://")
	if host != "api.opensubtitles.com" && host != "vip-api.opensubtitles.com" {
		return s, problem("provider_response", "OpenSubtitles returned an unsupported API host.")
	}
	if !textOK(raw.Token, 8192) {
		return s, problem("provider_response", "OpenSubtitles did not return a valid account token.")
	}
	s = session{token: raw.Token, host: host, until: time.Now().Add(time.Hour)}
	o.storeSession(c.Generation, s)
	return s, nil
}
func (o *OpenSubtitles) Test(ctx context.Context, c Credentials) error {
	_, e := o.login(ctx, c, true)
	return e
}
func (o *OpenSubtitles) authorized(ctx context.Context, c Credentials, method, endpoint string, body any, out any) error {
	s, e := o.login(ctx, c, false)
	if e != nil {
		return e
	}
	e = o.api(ctx, c, s.host, s.token, method, endpoint, body, out)
	var p *Error
	if errors.As(e, &p) && p.Code == "provider_auth" {
		s, e = o.login(ctx, c, true)
		if e != nil {
			return e
		}
		return o.api(ctx, c, s.host, s.token, method, endpoint, body, out)
	}
	return e
}

type number int64

func (n *number) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*n = 0
		return nil
	}
	v, e := strconv.ParseInt(strings.Trim(string(b), `"`), 10, 64)
	*n = number(v)
	return e
}
func (o *OpenSubtitles) Search(ctx context.Context, c Credentials, t Target, languages []string) ([]Candidate, error) {
	langs := slices.Clone(languages)
	slices.Sort(langs)
	// Machine translations are excluded by the provider's default. Sending that
	// default explicitly causes a canonical redirect, which we intentionally refuse.
	q := url.Values{"languages": {strings.Join(langs, ",")}, "ai_translated": {"exclude"}}
	if t.Kind == "movie" {
		q.Set("tmdb_id", strconv.FormatInt(t.TMDbID, 10))
		q.Set("type", "movie")
	} else {
		if len(t.Episodes) == 0 {
			return nil, problem("invalid_media", "The registered TV file has no episode associations.")
		}
		ep := t.Episodes[0]
		q.Set("parent_tmdb_id", strconv.FormatInt(t.TMDbID, 10))
		q.Set("season_number", strconv.Itoa(ep.Season))
		q.Set("episode_number", strconv.Itoa(ep.Number))
		q.Set("type", "episode")
	}
	if t.MovieHash != "" {
		q.Set("moviehash", t.MovieHash)
	}
	var raw struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				Language  string `json:"language"`
				Release   string `json:"release"`
				HI        bool   `json:"hearing_impaired"`
				Forced    bool   `json:"foreign_parts_only"`
				Hash      bool   `json:"moviehash_match"`
				Trusted   bool   `json:"from_trusted"`
				AI        bool   `json:"ai_translated"`
				Machine   bool   `json:"machine_translated"`
				Downloads int    `json:"download_count"`
				Parts     int    `json:"nb_cd"`
				Feature   struct {
					Type    string `json:"feature_type"`
					TMDb    number `json:"tmdb_id"`
					Parent  number `json:"parent_tmdb_id"`
					Season  int    `json:"season_number"`
					Episode int    `json:"episode_number"`
				} `json:"feature_details"`
				Files []struct {
					ID   number `json:"file_id"`
					Name string `json:"file_name"`
				} `json:"files"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if e := o.authorized(ctx, c, http.MethodGet, "/subtitles?"+q.Encode(), nil, &raw); e != nil {
		return nil, e
	}
	out := []Candidate{}
	for _, v := range raw.Data {
		a := v.Attributes
		if len(out) >= 50 {
			break
		}
		if len(a.Files) != 1 || a.Files[0].ID <= 0 || !textOK(a.Files[0].Name, 512) || !textOK(a.Release, 512) {
			continue
		}
		identity := false
		if t.Kind == "movie" {
			identity = strings.EqualFold(a.Feature.Type, "movie") && int64(a.Feature.TMDb) == t.TMDbID
		} else {
			ep := t.Episodes[0]
			identity = strings.EqualFold(a.Feature.Type, "episode") && (int64(a.Feature.TMDb) == ep.TMDbID || (int64(a.Feature.Parent) == t.TMDbID && a.Feature.Season == ep.Season && a.Feature.Episode == ep.Number))
		}
		labels := func(value string) string {
			n := quality.Normalize(value)
			return strings.TrimPrefix(n, quality.Normalize(t.Title))
		}
		normalized := " " + labels(a.Release) + " " + labels(a.Files[0].Name) + " "
		variant := "full"
		if strings.Contains(normalized, " signs ") {
			variant = "signs"
		} else if a.Forced || strings.Contains(normalized, " forced ") {
			variant = "forced"
		}
		out = append(out, Candidate{FileID: int64(a.Files[0].ID), SubtitleID: v.ID, Track: Track{Language: language(a.Language), Variant: variant}, Release: a.Release, FileName: a.Files[0].Name, HearingImpaired: a.HI, HashMatch: a.Hash && t.MovieHash != "", Trusted: a.Trusted, Downloads: max(0, a.Downloads), Identity: identity, Translated: a.AI || a.Machine, Multipart: a.Parts > 1})
	}
	return out, nil
}
func downloadURL(raw string) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil || len(raw) > 8192 || u.Scheme != "https" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") || !slices.Contains([]string{"dl.opensubtitles.com", "www.opensubtitles.com", "dl.opensubtitles.org"}, u.Hostname()) {
		return nil, problem("unsafe_download", "OpenSubtitles returned an unsupported subtitle download location.")
	}
	return u, nil
}
func (o *OpenSubtitles) Fetch(ctx context.Context, c Credentials, candidate Candidate) (Download, error) {
	format := candidate.Format
	if format == "" {
		format = "srt"
	}
	if !supportedSubtitleFormat(format) || (format != "srt" && subtitleExtension(candidate.FileName) != "."+format) {
		return Download{}, problem("invalid_subtitle", "The selected subtitle format is unsupported or no longer matches its original file.")
	}
	payload := map[string]any{"file_id": candidate.FileID}
	// Omit conversion when preserving styled/original files. Never flatten ASS
	// or SSA styling through an intermediate SRT conversion.
	if format == "srt" && subtitleExtension(candidate.FileName) != ".srt" {
		payload["sub_format"] = "srt"
	}
	var raw struct {
		Link      string          `json:"link"`
		Remaining *int            `json:"remaining"`
		Reset     json.RawMessage `json:"reset_time"`
		ResetUTC  string          `json:"reset_time_utc"`
	}
	if e := o.authorized(ctx, c, http.MethodPost, "/download", payload, &raw); e != nil {
		return Download{}, e
	}
	link, e := downloadURL(raw.Link)
	if e != nil {
		return Download{}, e
	}
	for hops := 0; hops < 4; hops++ {
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, link.String(), nil)
		if e != nil {
			return Download{}, problem("unsafe_download", "The subtitle download location is invalid.")
		}
		req.Header.Set("User-Agent", c.UserAgent)
		response, e := o.client.Do(req)
		if e != nil {
			if ctx.Err() != nil {
				return Download{}, ctx.Err()
			}
			return Download{}, retry("The subtitle file could not be downloaded.", time.Minute)
		}
		if response.StatusCode >= 300 && response.StatusCode <= 399 {
			location, e := response.Location()
			response.Body.Close()
			if e != nil {
				return Download{}, problem("unsafe_download", "The subtitle download redirect was invalid.")
			}
			link, e = downloadURL(location.String())
			if e != nil {
				return Download{}, e
			}
			continue
		}
		if response.StatusCode != 200 {
			response.Body.Close()
			return Download{}, retry("The subtitle download server returned an unsuccessful response.", time.Minute)
		}
		body, e := readBounded(response.Body, MaxSubtitle)
		response.Body.Close()
		if e != nil {
			return Download{}, e
		}
		encoded, _ := json.Marshal(map[string]any{"reset_time": raw.Reset, "reset_time_utc": raw.ResetUTC})
		return Download{Content: body, Remaining: raw.Remaining, ResetAt: resetTime(encoded)}, nil
	}
	return Download{}, problem("unsafe_download", "The subtitle download redirected too many times.")
}
func releaseName(s string) string {
	for {
		ext := strings.ToLower(path.Ext(s))
		if !slices.Contains([]string{".mkv", ".mp4", ".avi", ".m4v", ".srt", ".ass", ".ssa", ".vtt", ".ts", ".m2ts"}, ext) {
			break
		}
		s = strings.TrimSuffix(s, path.Ext(s))
	}
	return quality.Normalize(s)
}
