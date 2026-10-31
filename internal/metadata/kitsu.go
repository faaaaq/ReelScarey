package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Kitsu struct {
	endpoint string
	http     *http.Client
	mu       sync.Mutex
	next     time.Time
}

func NewKitsu() *Kitsu {
	return newKitsu("https://kitsu.io/api/edge", &http.Client{Timeout: 10 * time.Second})
}
func newKitsu(endpoint string, client *http.Client) *Kitsu {
	h := *client
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("provider redirects disabled") }
	return &Kitsu{endpoint: endpoint, http: &h}
}

type kitsuAnime struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Attributes struct {
		CanonicalTitle string `json:"canonicalTitle"`
		Titles         struct {
			English   string `json:"en"`
			EnglishUS string `json:"en_us"`
			Romaji    string `json:"en_jp"`
			Native    string `json:"ja_jp"`
		} `json:"titles"`
		Subtype   string `json:"subtype"`
		StartDate string `json:"startDate"`
		Episodes  *int   `json:"episodeCount"`
	} `json:"attributes"`
}

func AnimeIdentityURL(provider string, id int64) string {
	if id < 1 || id > 2147483647 {
		return ""
	}
	switch provider {
	case "anilist":
		return "https://anilist.co/anime/" + strconv.FormatInt(id, 10)
	case "kitsu":
		return "https://kitsu.io/anime/" + strconv.FormatInt(id, 10)
	}
	return ""
}
func normalizeKitsu(v kitsuAnime) (AnimeIdentity, error) {
	bad := func() (AnimeIdentity, error) {
		return AnimeIdentity{}, animeProviderError("kitsu_invalid_response", "Kitsu returned invalid Anime details.")
	}
	id, e := strconv.ParseInt(v.ID, 10, 64)
	a := v.Attributes
	if e != nil || strconv.FormatInt(id, 10) != v.ID || v.Type != "anime" || AnimeIdentityURL("kitsu", id) == "" {
		return bad()
	}
	english := a.Titles.English
	if english == "" {
		english = a.Titles.EnglishUS
	}
	out := AnimeIdentity{Provider: "kitsu", ID: id, Title: strings.TrimSpace(a.CanonicalTitle), Romaji: strings.TrimSpace(a.Titles.Romaji), English: strings.TrimSpace(english), Native: strings.TrimSpace(a.Titles.Native), Format: strings.ToUpper(a.Subtype), Episodes: a.Episodes, URL: AnimeIdentityURL("kitsu", id)}
	if a.StartDate != "" {
		date, e := time.Parse("2006-01-02", a.StartDate)
		if e != nil {
			return bad()
		}
		y := date.Year()
		out.Year = &y
	}
	if out.Title == "" || !validAnimeText(out.Title) || !validAnimeText(out.Romaji) || !validAnimeText(out.English) || !validAnimeText(out.Native) || !validAnimeText(out.Format) || (out.Episodes != nil && (*out.Episodes < 1 || *out.Episodes > 100000)) {
		return bad()
	}
	return out, nil
}
func (k *Kitsu) request(ctx context.Context, path string, out any) error {
	k.mu.Lock()
	if time.Until(k.next) > 5*time.Second {
		seconds := max(1, int(time.Until(k.next).Seconds())+1)
		k.mu.Unlock()
		return &Error{Code: "kitsu_rate_limited", Message: "Kitsu requests are limited. Wait before trying again.", RetryAfter: seconds}
	}
	start := time.Now()
	if k.next.After(start) {
		start = k.next
	}
	k.next = start.Add(time.Second)
	k.mu.Unlock()
	if delay := time.Until(start); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, k.endpoint+path, nil)
	if e != nil {
		return e
	}
	req.Header.Set("Accept", "application/vnd.api+json")
	req.Header.Set("User-Agent", "ReelScarey/0.26")
	resp, e := k.http.Do(req)
	if e != nil {
		return animeProviderError("kitsu_unavailable", "Kitsu could not be reached. Saved mappings remain available.")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 429 {
		seconds, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		seconds = min(3600, max(60, seconds))
		k.mu.Lock()
		k.next = time.Now().Add(time.Duration(seconds) * time.Second)
		k.mu.Unlock()
		return &Error{Code: "kitsu_rate_limited", Message: "Kitsu is limiting requests. Wait before trying again.", RetryAfter: seconds}
	}
	if resp.StatusCode == 404 {
		return animeProviderError("anime_not_found", "This Anime entry is no longer available from Kitsu.")
	}
	if resp.StatusCode != 200 {
		return animeProviderError("kitsu_unavailable", "Kitsu did not return a successful response.")
	}
	body, e := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if e != nil || len(body) > 1<<20 {
		return animeProviderError("kitsu_invalid_response", "Kitsu returned an unreadable response.")
	}
	var envelope struct {
		Data   json.RawMessage   `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(body, &envelope) != nil || len(envelope.Errors) > 0 || len(envelope.Data) == 0 || string(envelope.Data) == "null" || json.Unmarshal(envelope.Data, out) != nil {
		return animeProviderError("kitsu_invalid_response", "Kitsu returned incomplete Anime details.")
	}
	return nil
}
func (k *Kitsu) SearchAnime(ctx context.Context, query string) ([]AnimeIdentity, error) {
	query = strings.TrimSpace(query)
	if !ValidQuery(query, 1) || len(query) > 200 {
		return nil, animeProviderError("invalid_anime_query", "Enter an Anime title of at most 200 UTF-8 bytes.")
	}
	var data []kitsuAnime
	if e := k.request(ctx, "/anime?"+url.Values{"filter[text]": {query}, "page[limit]": {"10"}}.Encode(), &data); e != nil {
		return nil, e
	}
	if len(data) > 10 {
		return nil, animeProviderError("kitsu_invalid_response", "Kitsu returned too many results.")
	}
	out := []AnimeIdentity{}
	seen := map[int64]bool{}
	for _, v := range data {
		item, e := normalizeKitsu(v)
		if e != nil {
			return nil, e
		}
		if seen[item.ID] {
			return nil, animeProviderError("kitsu_invalid_response", "Kitsu returned repeated results.")
		}
		seen[item.ID] = true
		out = append(out, item)
	}
	return out, nil
}
func (k *Kitsu) Anime(ctx context.Context, id int64) (AnimeIdentity, error) {
	if AnimeIdentityURL("kitsu", id) == "" {
		return AnimeIdentity{}, animeProviderError("invalid_anime_id", "Choose a valid Kitsu Anime ID.")
	}
	var data kitsuAnime
	if e := k.request(ctx, "/anime/"+strconv.FormatInt(id, 10), &data); e != nil {
		return AnimeIdentity{}, e
	}
	out, e := normalizeKitsu(data)
	if e == nil && out.ID != id {
		e = animeProviderError("kitsu_invalid_response", "Kitsu returned another Anime identity.")
	}
	return out, e
}
