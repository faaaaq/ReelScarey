package subtitle

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Only documented individually unpacked files are candidates. ZIP archives and
// inferred episode identities never enter the importer.
type SubDL struct{ http *http.Client }

func NewSubDL() *SubDL {
	return &SubDL{http: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("subtitle redirects disabled") }}}
}

var subdlPath = regexp.MustCompile(`^/subtitle/[A-Za-z0-9_-]{1,128}/[A-Za-z0-9_-]{1,128}$`)

func subdlURL(raw string) (string, error) {
	u, e := url.Parse(raw)
	if e != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || !subdlPath.MatchString(u.Path) || (u.Host != "" && (u.Host != "dl.subdl.com" || u.Scheme != "https")) || (u.Host == "" && u.Scheme != "") {
		return "", problem("invalid_subtitle", "SubDL returned an unsafe subtitle download address.")
	}
	return "https://dl.subdl.com" + u.Path, nil
}
func subdlID(address string) int64 {
	sum := sha256.Sum256([]byte(address))
	return int64(binary.BigEndian.Uint64(sum[:8])>>16) + 1
}
func (s *SubDL) get(ctx context.Context, address string, c Credentials, limit int, authenticate bool) ([]byte, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Accept", "application/json")
	if authenticate {
		q := req.URL.Query()
		q.Set("api_key", c.APIKey)
		req.URL.RawQuery = q.Encode()
	}
	resp, e := s.http.Do(req)
	if e != nil {
		return nil, retry("SubDL could not be reached.", time.Minute)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, problem("provider_auth", "SubDL rejected the API key or account access.")
	}
	if resp.StatusCode == 429 {
		seconds, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		return nil, &Error{Code: "provider_rate_limit", Message: "SubDL requests or downloads are limited. Waiting before retrying.", RetryAt: time.Now().Add(time.Duration(min(86400, max(60, seconds))) * time.Second).Unix()}
	}
	if resp.StatusCode != 200 {
		return nil, retry("SubDL did not return a successful response.", 5*time.Minute)
	}
	data, e := readBounded(resp.Body, limit)
	if e != nil {
		return nil, problem("provider_response", "SubDL returned an unreadable or oversized response.")
	}
	return data, nil
}

type subdlResponse struct {
	Status  bool `json:"status"`
	Results []struct {
		TMDbID number `json:"tmdb_id"`
		Type   string `json:"type"`
	} `json:"results"`
	Subtitles []struct {
		Production int `json:"production_type"`
		Files      []struct {
			ID       string `json:"file_n_id"`
			Name     string `json:"name"`
			Release  string `json:"release_name"`
			Season   *int   `json:"season"`
			Episode  *int   `json:"episode"`
			Language string `json:"language"`
			HI       bool   `json:"hi"`
			Format   string `json:"format"`
			Size     int64  `json:"size"`
			URL      string `json:"url"`
		} `json:"unpack_files"`
	} `json:"subtitles"`
}

var subdlLanguages = map[string]string{"en": "EN", "es": "ES", "fr": "FR", "de": "DE", "it": "IT", "ja": "JA", "ko": "KO", "pt-br": "BR_PT", "pt-pt": "PT", "zh-cn": "ZH", "zh-tw": "ZH", "nl": "NL", "ru": "RU", "ar": "AR", "hi": "HI", "pl": "PL", "sv": "SV", "uk": "UK"}

func (s *SubDL) search(ctx context.Context, c Credentials, t Target, languages []string) (subdlResponse, error) {
	q := url.Values{"tmdb_id": {strconv.FormatInt(t.TMDbID, 10)}, "type": {"movie"}, "unpack": {"1"}, "subs_per_page": {"30"}, "client": {"custom_integration"}}
	if t.Kind == "series" {
		q.Set("type", "tv")
		if len(t.Episodes) != 1 {
			return subdlResponse{Status: true}, nil
		}
		q.Set("season_number", strconv.Itoa(t.Episodes[0].Season))
		q.Set("episode_number", strconv.Itoa(t.Episodes[0].Number))
	}
	codes := []string{}
	for _, l := range languages {
		if code := subdlLanguages[l]; code != "" {
			codes = append(codes, code)
		}
	}
	if len(codes) == 0 {
		return subdlResponse{Status: true}, nil
	}
	q.Set("languages", strings.Join(codes, ","))
	b, e := s.get(ctx, "https://api.subdl.com/api/v1/subtitles?"+q.Encode(), c, 2<<20, true)
	if e != nil {
		return subdlResponse{}, e
	}
	var out subdlResponse
	if json.Unmarshal(b, &out) != nil || !out.Status || len(out.Results) > 30 || len(out.Subtitles) > 30 {
		return out, problem("provider_response", "SubDL could not confirm a valid search response. Check the saved key and provider status.")
	}
	return out, nil
}
func (s *SubDL) Test(ctx context.Context, c Credentials) error {
	_, e := s.search(ctx, c, Target{Kind: "movie", TMDbID: 27205}, []string{"en"})
	return e
}
func (s *SubDL) Search(ctx context.Context, c Credentials, t Target, languages []string) ([]Candidate, error) {
	out := []Candidate{}
	if t.TMDbID < 1 || (t.Kind != "movie" && t.Kind != "series") {
		return out, problem("invalid_media", "Choose media with a canonical movie or series identity.")
	}
	r, e := s.search(ctx, c, t, languages)
	if e != nil {
		return nil, e
	}
	kind := "movie"
	if t.Kind == "series" {
		kind = "tv"
	}
	if len(r.Results) == 0 {
		return out, nil
	}
	if int64(r.Results[0].TMDbID) != t.TMDbID || r.Results[0].Type != kind {
		return out, nil
	} // The API may broaden failed exact searches.
	seen := map[int64]string{}
	for _, sub := range r.Subtitles {
		if len(sub.Files) > 100 {
			return nil, problem("provider_response", "SubDL returned too many unpacked files.")
		}
		for _, f := range sub.Files {
			if len(out) >= 100 {
				return out, nil
			}
			if t.Kind == "series" && (len(t.Episodes) != 1 || f.Season == nil || f.Episode == nil || *f.Season != t.Episodes[0].Season || *f.Episode != t.Episodes[0].Number) {
				continue
			}
			address, e := subdlURL(f.URL)
			if e != nil {
				continue
			}
			format := strings.ToLower(f.Format)
			if !supportedSubtitleFormat(format) || subtitleExtension(f.Name) != "."+format || !textOK(f.Name, 512) || !textOK(f.Release, 1000) || f.Size < 1 || f.Size > MaxSubtitle {
				continue
			}
			lang := language(f.Language)
			if f.Language == "BR_PT" {
				lang = "pt-br"
			}
			if f.Language == "PT" {
				lang = "pt-pt"
			}
			// SubDL's ZH code does not distinguish simplified/traditional text.
			if strings.EqualFold(f.Language, "ZH") {
				continue
			}
			if lang == "" {
				continue
			}
			id := subdlID(address)
			if old, ok := seen[id]; ok {
				if old != address {
					return nil, problem("provider_response", "SubDL file identities collided.")
				}
				continue
			}
			seen[id] = address
			variant := "full"
			label := strings.ToLower(f.Name + " " + f.Release)
			if strings.Contains(label, "forced") {
				variant = "forced"
			} else if strings.Contains(label, "signs") || strings.Contains(label, "songs") {
				variant = "signs"
			}
			out = append(out, Candidate{Provider: "subdl", DownloadURL: address, FileID: id, SubtitleID: f.ID, FileName: f.Name, Release: f.Release, Track: Track{Language: lang, Variant: variant}, HearingImpaired: f.HI, Identity: true, Translated: sub.Production == 3, RequireReview: true})
		}
	}
	return out, nil
}
func (s *SubDL) Fetch(ctx context.Context, c Credentials, v Candidate) (Download, error) {
	address, e := subdlURL(v.DownloadURL)
	if e != nil {
		return Download{}, e
	}
	if v.Provider != "subdl" || v.FileID != subdlID(address) || !supportedSubtitleFormat(v.Format) || subtitleExtension(v.FileName) != "."+v.Format {
		return Download{}, problem("invalid_subtitle", "Choose the original supported SubDL file format; conversion is not available.")
	}
	// Public file downloads do not receive an account key, password or local path.
	b, e := s.get(ctx, address, c, MaxSubtitle, false)
	return Download{Content: b}, e
}

func providerName(value string) string {
	if value == "" {
		return "opensubtitles"
	}
	return value
}
func (s *Service) selectedProvider(c Credentials) Provider {
	if c.Provider == "subdl" {
		return s.subdl
	}
	return s.provider
}
