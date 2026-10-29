package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// Anime identities are scoped provider entries, not per-episode order evidence.
type AnimeIdentity struct {
	Provider string `json:"provider"`
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Romaji   string `json:"romaji"`
	English  string `json:"english"`
	Native   string `json:"native"`
	Format   string `json:"format"`
	Year     *int   `json:"year"`
	Episodes *int   `json:"episodes"`
	URL      string `json:"url"`
}
type AnimeProvider interface {
	SearchAnime(context.Context, string) ([]AnimeIdentity, error)
	Anime(context.Context, int64) (AnimeIdentity, error)
}
type AniList struct {
	endpoint string
	http     *http.Client
	mu       sync.Mutex
	next     time.Time
}

func NewAniList() *AniList {
	return newAniList("https://graphql.anilist.co", &http.Client{Timeout: 10 * time.Second})
}
func newAniList(endpoint string, h *http.Client) *AniList {
	c := *h
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("provider redirects disabled") }
	return &AniList{endpoint: endpoint, http: &c}
}

const animeFields = `id type format episodes startDate { year } title { romaji english native }`

type animeMedia struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Format    string `json:"format"`
	Episodes  *int   `json:"episodes"`
	StartDate struct {
		Year *int `json:"year"`
	} `json:"startDate"`
	Title struct {
		Romaji  string `json:"romaji"`
		English string `json:"english"`
		Native  string `json:"native"`
	} `json:"title"`
}

func validAnimeText(v string) bool {
	return len(v) <= 1000 && utf8.ValidString(v) && !strings.ContainsFunc(v, unicode.IsControl)
}
func normalizeAnime(in animeMedia) (AnimeIdentity, error) {
	out := AnimeIdentity{Provider: "anilist", ID: in.ID, Romaji: strings.TrimSpace(in.Title.Romaji), English: strings.TrimSpace(in.Title.English), Native: strings.TrimSpace(in.Title.Native), Format: in.Format, Year: in.StartDate.Year, Episodes: in.Episodes}
	out.Title = out.Romaji
	if out.Title == "" {
		out.Title = out.English
	}
	if out.Title == "" {
		out.Title = out.Native
	}
	if in.Type != "ANIME" || in.ID < 1 || in.ID > 2147483647 || out.Title == "" || !validAnimeText(out.Romaji) || !validAnimeText(out.English) || !validAnimeText(out.Native) || !validAnimeText(out.Format) || (out.Year != nil && (*out.Year < 1 || *out.Year > 9999)) || (out.Episodes != nil && (*out.Episodes < 1 || *out.Episodes > 100000)) {
		return AnimeIdentity{}, animeProviderError("anilist_invalid_response", "AniList returned invalid Anime details.")
	}
	out.URL = "https://anilist.co/anime/" + strconv.FormatInt(in.ID, 10)
	return out, nil
}
func animeProviderError(code, message string) *Error { return &Error{Code: code, Message: message} }
func (c *AniList) request(ctx context.Context, query string, variables any, out any) error {
	c.mu.Lock()
	now := time.Now()
	if time.Until(c.next) > 5*time.Second {
		seconds := max(1, int(time.Until(c.next).Seconds())+1)
		c.mu.Unlock()
		return &Error{Code: "anilist_rate_limited", Message: "AniList requests are limited. Wait before trying again.", RetryAfter: seconds}
	}
	start := now
	if c.next.After(start) {
		start = c.next
	}
	c.next = start.Add(2100 * time.Millisecond)
	c.mu.Unlock()
	if wait := time.Until(start); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	payload, e := json.Marshal(map[string]any{"query": query, "variables": variables})
	if e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ReelScarey/0.18")
	resp, e := c.http.Do(req)
	if e != nil {
		return animeProviderError("anilist_unavailable", "AniList could not be reached. Saved mappings remain available.")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 429 || resp.Header.Get("X-RateLimit-Remaining") == "0" {
		seconds, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		if reset, e := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); e == nil {
			seconds = max(seconds, int(time.Until(time.Unix(reset, 0)).Seconds())+1)
		}
		seconds = min(3600, max(60, seconds))
		c.mu.Lock()
		c.next = time.Now().Add(time.Duration(seconds) * time.Second)
		c.mu.Unlock()
		if resp.StatusCode == 429 {
			return &Error{Code: "anilist_rate_limited", Message: "AniList is limiting requests. Wait before trying again.", RetryAfter: seconds}
		}
	}
	if resp.StatusCode == 404 {
		return animeProviderError("anime_not_found", "This Anime entry is no longer available from AniList.")
	}
	if resp.StatusCode != 200 {
		return animeProviderError("anilist_unavailable", "AniList did not return a successful response.")
	}
	body, e := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if e != nil || len(body) > 1<<20 {
		return animeProviderError("anilist_invalid_response", "AniList returned an unreadable response.")
	}
	var envelope struct {
		Errors []json.RawMessage `json:"errors"`
		Data   json.RawMessage   `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil || len(envelope.Errors) > 0 || len(envelope.Data) == 0 || string(envelope.Data) == "null" || json.Unmarshal(envelope.Data, out) != nil {
		return animeProviderError("anilist_invalid_response", "AniList returned incomplete or unreadable Anime details.")
	}
	return nil
}
func (c *AniList) SearchAnime(ctx context.Context, query string) ([]AnimeIdentity, error) {
	query = strings.TrimSpace(query)
	if !ValidQuery(query, 1) || len(query) > 200 {
		return nil, animeProviderError("invalid_anime_query", "Enter an Anime title of at most 200 UTF-8 bytes.")
	}
	var out struct {
		Page struct {
			Media []animeMedia `json:"media"`
		} `json:"Page"`
	}
	if e := c.request(ctx, `query($search:String!){Page(page:1,perPage:10){media(search:$search,type:ANIME){`+animeFields+`}}}`, map[string]any{"search": query}, &out); e != nil {
		return nil, e
	}
	if len(out.Page.Media) > 10 {
		return nil, animeProviderError("anilist_invalid_response", "AniList returned too many results.")
	}
	items := []AnimeIdentity{}
	seen := map[int64]bool{}
	for _, m := range out.Page.Media {
		v, e := normalizeAnime(m)
		if e != nil || seen[v.ID] {
			return nil, animeProviderError("anilist_invalid_response", "AniList returned invalid or repeated Anime results.")
		}
		seen[v.ID] = true
		items = append(items, v)
	}
	return items, nil
}
func (c *AniList) Anime(ctx context.Context, id int64) (AnimeIdentity, error) {
	if id < 1 || id > 2147483647 {
		return AnimeIdentity{}, animeProviderError("invalid_anime_id", "Choose a valid AniList Anime ID.")
	}
	var out struct {
		Media animeMedia `json:"Media"`
	}
	if e := c.request(ctx, `query($id:Int!){Media(id:$id,type:ANIME){`+animeFields+`}}`, map[string]any{"id": id}, &out); e != nil {
		return AnimeIdentity{}, e
	}
	if out.Media.ID != id {
		return AnimeIdentity{}, animeProviderError("anilist_invalid_response", "AniList returned another Anime identity.")
	}
	return normalizeAnime(out.Media)
}
