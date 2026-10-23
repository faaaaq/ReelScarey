// Package metadata normalizes TMDb responses without exposing provider tokens.
package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

type Error struct {
	Code       string
	Message    string
	RetryAfter int
}

func (e *Error) Error() string { return e.Message }

var ErrNotConfigured = &Error{Code: "tmdb_not_configured", Message: "Connect TMDb in Settings before searching for movies or TV shows."}

type Movie struct {
	TMDbID         int64  `json:"tmdb_id"`
	Title          string `json:"title"`
	Overview       string `json:"overview"`
	ReleaseDate    string `json:"release_date"`
	Year           *int   `json:"year"`
	PosterPath     string `json:"poster_path"`
	PosterURL      string `json:"poster_url"`
	RuntimeMinutes *int   `json:"runtime_minutes"`
}
type Results struct {
	Page         int     `json:"page"`
	TotalPages   int     `json:"total_pages"`
	TotalResults int     `json:"total_results"`
	Items        []Movie `json:"items"`
}
type movieResponse struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Overview    string `json:"overview"`
	ReleaseDate string `json:"release_date"`
	PosterPath  string `json:"poster_path"`
	Runtime     *int   `json:"runtime"`
}

var posterPattern = regexp.MustCompile(`^/[A-Za-z0-9_-]+\.(jpg|png|webp)$`)

func PosterURL(p string) string {
	if !posterPattern.MatchString(p) {
		return ""
	}
	return "https://image.tmdb.org/t/p/w500" + p
}
func normalize(in movieResponse) (Movie, error) {
	if in.ID < 1 || strings.TrimSpace(in.Title) == "" || utf8.RuneCountInString(in.Title) > 500 || len(in.Overview) > 32000 || strings.IndexFunc(in.Title, unicode.IsControl) >= 0 {
		return Movie{}, &Error{Code: "tmdb_invalid_response", Message: "TMDb returned invalid movie details. Try again later."}
	}
	out := Movie{TMDbID: in.ID, Title: strings.TrimSpace(in.Title), Overview: in.Overview, RuntimeMinutes: in.Runtime}
	if in.ReleaseDate != "" {
		if date, err := time.Parse("2006-01-02", in.ReleaseDate); err == nil && date.Year() >= 1800 {
			year := date.Year()
			out.Year = &year
			out.ReleaseDate = in.ReleaseDate
		}
	}
	if PosterURL(in.PosterPath) != "" {
		out.PosterPath = in.PosterPath
		out.PosterURL = PosterURL(in.PosterPath)
	}
	if out.RuntimeMinutes != nil && (*out.RuntimeMinutes < 1 || *out.RuntimeMinutes > 10000) {
		out.RuntimeMinutes = nil
	}
	return out, nil
}

type MovieProvider interface {
	Validate(context.Context, string) error
	Search(context.Context, string, string, int) (Results, error)
	Movie(context.Context, string, int64) (Movie, error)
}
type Client struct {
	base    string
	http    *http.Client
	mu      sync.Mutex
	retryAt time.Time
}

func NewTMDb() *Client {
	return newClient("https://api.themoviedb.org/3", &http.Client{Timeout: 10 * time.Second})
}
func newClient(base string, client *http.Client) *Client {
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("provider redirects are disabled") }
	return &Client{base: strings.TrimRight(base, "/"), http: &copy}
}
func ValidToken(token string) bool {
	return len(token) >= 20 && len(token) <= 4096 && strings.TrimSpace(token) == token && strings.IndexFunc(token, func(r rune) bool { return r < 33 || r > 126 }) < 0
}
func (c *Client) request(ctx context.Context, token, endpoint string, query url.Values, out any) error {
	if !ValidToken(token) {
		return &Error{Code: "tmdb_invalid_token", Message: "Enter a valid TMDb API Read Access Token."}
	}
	c.mu.Lock()
	retryAt := c.retryAt
	c.mu.Unlock()
	if time.Now().Before(retryAt) {
		return &Error{Code: "tmdb_rate_limited", Message: "TMDb is limiting requests. Wait before trying again.", RetryAfter: max(1, int(time.Until(retryAt).Seconds())+1)}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return &Error{Code: "tmdb_unavailable", Message: "TMDb could not be reached. Try again later."}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ReelScarey/0.4")
	resp, err := c.http.Do(req)
	if err != nil {
		return &Error{Code: "tmdb_unavailable", Message: "TMDb could not be reached. Check your network and try again."}
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 200:
	case 401, 403:
		return &Error{Code: "tmdb_unauthorized", Message: "TMDb rejected the access token. Update it in Settings."}
	case 404:
		return &Error{Code: "movie_not_found", Message: "This movie is no longer available from TMDb."}
	case 429:
		seconds, err := strconv.Atoi(resp.Header.Get("Retry-After"))
		if err != nil {
			if when, parseErr := http.ParseTime(resp.Header.Get("Retry-After")); parseErr == nil {
				seconds = int(time.Until(when).Seconds())
			}
		}
		seconds = min(300, max(1, seconds))
		if seconds < 2 {
			seconds = 5
		}
		c.mu.Lock()
		c.retryAt = time.Now().Add(time.Duration(seconds) * time.Second)
		c.mu.Unlock()
		return &Error{Code: "tmdb_rate_limited", Message: "TMDb is limiting requests. Wait before trying again.", RetryAfter: seconds}
	default:
		return &Error{Code: "tmdb_unavailable", Message: "TMDb is temporarily unavailable. Try again later."}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(body) > 2<<20 || json.Unmarshal(body, out) != nil {
		return &Error{Code: "tmdb_invalid_response", Message: "TMDb returned an unreadable response. Try again later."}
	}
	return nil
}
func (c *Client) Validate(ctx context.Context, token string) error {
	var out struct {
		Success bool `json:"success"`
	}
	if err := c.request(ctx, token, "/authentication", nil, &out); err != nil {
		return err
	}
	if !out.Success {
		return &Error{Code: "tmdb_unauthorized", Message: "TMDb could not validate this access token."}
	}
	return nil
}
func ValidQuery(query string, page int) bool {
	return utf8.ValidString(query) && utf8.RuneCountInString(query) >= 1 && utf8.RuneCountInString(query) <= 200 && strings.IndexFunc(query, unicode.IsControl) < 0 && page >= 1 && page <= 500
}
func (c *Client) Search(ctx context.Context, token, query string, page int) (Results, error) {
	var body struct {
		Page         int             `json:"page"`
		TotalPages   int             `json:"total_pages"`
		TotalResults int             `json:"total_results"`
		Results      []movieResponse `json:"results"`
	}
	if err := c.request(ctx, token, "/search/movie", url.Values{"query": {query}, "page": {strconv.Itoa(page)}, "language": {"en-US"}, "include_adult": {"false"}}, &body); err != nil {
		return Results{}, err
	}
	if body.Page != page || body.TotalPages < 0 || body.TotalResults < 0 || len(body.Results) > 100 {
		return Results{}, &Error{Code: "tmdb_invalid_response", Message: "TMDb returned invalid search results."}
	}
	out := Results{Page: body.Page, TotalPages: min(500, body.TotalPages), TotalResults: body.TotalResults, Items: make([]Movie, 0, len(body.Results))}
	for _, item := range body.Results {
		movie, err := normalize(item)
		if err != nil {
			return Results{}, err
		}
		out.Items = append(out.Items, movie)
	}
	return out, nil
}
func (c *Client) Movie(ctx context.Context, token string, id int64) (Movie, error) {
	var body movieResponse
	if err := c.request(ctx, token, "/movie/"+strconv.FormatInt(id, 10), url.Values{"language": {"en-US"}}, &body); err != nil {
		return Movie{}, err
	}
	if body.ID != id {
		return Movie{}, &Error{Code: "tmdb_invalid_response", Message: "TMDb returned details for a different movie."}
	}
	return normalize(body)
}
