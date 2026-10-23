package metadata

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type SeasonSummary struct {
	TMDbID       int64  `json:"tmdb_id"`
	Number       int    `json:"number"`
	Title        string `json:"title"`
	Overview     string `json:"overview"`
	AirDate      string `json:"air_date"`
	EpisodeCount int    `json:"episode_count"`
}
type Series struct {
	Movie
	SeriesStatus string          `json:"series_status"`
	Seasons      []SeasonSummary `json:"seasons"`
}
type SeriesResults struct {
	Page         int      `json:"page"`
	TotalPages   int      `json:"total_pages"`
	TotalResults int      `json:"total_results"`
	Items        []Series `json:"items"`
}
type Episode struct {
	TMDbID         int64  `json:"tmdb_id"`
	Number         int    `json:"number"`
	Title          string `json:"title"`
	Overview       string `json:"overview"`
	AirDate        string `json:"air_date"`
	RuntimeMinutes *int   `json:"runtime_minutes"`
}
type Season struct {
	SeasonSummary
	Episodes []Episode `json:"episodes"`
}
type SeriesProvider interface {
	SearchSeries(context.Context, string, string, int) (SeriesResults, error)
	Series(context.Context, string, int64) (Series, error)
	Season(context.Context, string, int64, int) (Season, error)
}
type seasonResponse struct {
	ID           int64  `json:"id"`
	Number       int    `json:"season_number"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	AirDate      string `json:"air_date"`
	EpisodeCount int    `json:"episode_count"`
}
type seriesResponse struct {
	ID           int64            `json:"id"`
	Name         string           `json:"name"`
	Overview     string           `json:"overview"`
	FirstAirDate string           `json:"first_air_date"`
	PosterPath   string           `json:"poster_path"`
	Status       string           `json:"status"`
	Seasons      []seasonResponse `json:"seasons"`
}

func tvError() error {
	return &Error{Code: "tmdb_invalid_response", Message: "TMDb returned invalid TV details. Try again later."}
}
func tvNotFound(err error) error {
	var p *Error
	if errors.As(err, &p) && p.Code == "movie_not_found" {
		return &Error{Code: "series_not_found", Message: "This show or season is no longer available from TMDb."}
	}
	return err
}
func airDate(raw string) string {
	if d, e := time.Parse("2006-01-02", raw); e == nil && d.Year() >= 1800 {
		return raw
	}
	return ""
}
func normalizeSeason(in seasonResponse) (SeasonSummary, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = fmt.Sprintf("Season %d", in.Number)
		if in.Number == 0 {
			name = "Specials"
		}
	}
	m, e := normalize(movieResponse{ID: in.ID, Title: name, Overview: in.Overview})
	if e != nil || in.Number < 0 || in.Number > 10000 || in.EpisodeCount < 0 || in.EpisodeCount > 100000 {
		return SeasonSummary{}, tvError()
	}
	return SeasonSummary{TMDbID: in.ID, Number: in.Number, Title: m.Title, Overview: m.Overview, AirDate: airDate(in.AirDate), EpisodeCount: in.EpisodeCount}, nil
}
func normalizeSeries(in seriesResponse) (Series, error) {
	m, e := normalize(movieResponse{ID: in.ID, Title: in.Name, Overview: in.Overview, ReleaseDate: in.FirstAirDate, PosterPath: in.PosterPath})
	if e != nil || len(in.Seasons) > 500 || len(in.Status) > 100 {
		return Series{}, tvError()
	}
	out := Series{Movie: m, SeriesStatus: in.Status, Seasons: []SeasonSummary{}}
	seen := map[int]bool{}
	for _, v := range in.Seasons {
		season, e := normalizeSeason(v)
		if e != nil || seen[v.Number] {
			return Series{}, tvError()
		}
		seen[v.Number] = true
		out.Seasons = append(out.Seasons, season)
	}
	return out, nil
}
func (c *Client) SearchSeries(ctx context.Context, token, query string, page int) (SeriesResults, error) {
	var body struct {
		Page         int              `json:"page"`
		TotalPages   int              `json:"total_pages"`
		TotalResults int              `json:"total_results"`
		Results      []seriesResponse `json:"results"`
	}
	if e := c.request(ctx, token, "/search/tv", url.Values{"query": {query}, "page": {strconv.Itoa(page)}, "language": {"en-US"}, "include_adult": {"false"}}, &body); e != nil {
		return SeriesResults{}, tvNotFound(e)
	}
	if body.Page != page || body.TotalPages < 0 || body.TotalResults < 0 || len(body.Results) > 100 {
		return SeriesResults{}, tvError()
	}
	out := SeriesResults{Page: page, TotalPages: min(500, body.TotalPages), TotalResults: body.TotalResults, Items: []Series{}}
	for _, v := range body.Results {
		series, e := normalizeSeries(v)
		if e != nil {
			return SeriesResults{}, e
		}
		out.Items = append(out.Items, series)
	}
	return out, nil
}
func (c *Client) Series(ctx context.Context, token string, id int64) (Series, error) {
	var body seriesResponse
	if e := c.request(ctx, token, "/tv/"+strconv.FormatInt(id, 10), url.Values{"language": {"en-US"}}, &body); e != nil {
		return Series{}, tvNotFound(e)
	}
	if body.ID != id {
		return Series{}, tvError()
	}
	return normalizeSeries(body)
}
func (c *Client) Season(ctx context.Context, token string, id int64, number int) (Season, error) {
	var body struct {
		seasonResponse
		Episodes []struct {
			ID           int64  `json:"id"`
			Number       int    `json:"episode_number"`
			SeasonNumber int    `json:"season_number"`
			Name         string `json:"name"`
			Overview     string `json:"overview"`
			AirDate      string `json:"air_date"`
			Runtime      *int   `json:"runtime"`
		} `json:"episodes"`
	}
	if e := c.request(ctx, token, fmt.Sprintf("/tv/%d/season/%d", id, number), url.Values{"language": {"en-US"}}, &body); e != nil {
		return Season{}, tvNotFound(e)
	}
	summary, e := normalizeSeason(body.seasonResponse)
	if e != nil || body.Number != number || len(body.Episodes) > 2000 {
		return Season{}, tvError()
	}
	out := Season{SeasonSummary: summary, Episodes: []Episode{}}
	seen := map[int]bool{}
	ids := map[int64]bool{}
	for _, v := range body.Episodes {
		name := strings.TrimSpace(v.Name)
		if name == "" {
			name = fmt.Sprintf("Episode %d", v.Number)
		}
		m, e := normalize(movieResponse{ID: v.ID, Title: name, Overview: v.Overview, Runtime: v.Runtime})
		if e != nil || v.SeasonNumber != number || v.Number < 1 || v.Number > 100000 || seen[v.Number] || ids[v.ID] {
			return Season{}, tvError()
		}
		seen[v.Number] = true
		ids[v.ID] = true
		out.Episodes = append(out.Episodes, Episode{TMDbID: v.ID, Number: v.Number, Title: m.Title, Overview: m.Overview, AirDate: airDate(v.AirDate), RuntimeMinutes: m.RuntimeMinutes})
	}
	out.EpisodeCount = len(out.Episodes)
	return out, nil
}
func (s *Service) SupportsSeries() bool { _, ok := s.provider.(SeriesProvider); return ok }
func (s *Service) seriesProvider(ctx context.Context) (SeriesProvider, string, error) {
	p, ok := s.provider.(SeriesProvider)
	if !ok {
		return nil, "", &Error{Code: "tv_unavailable", Message: "TV metadata is unavailable."}
	}
	token, e := s.token(ctx)
	return p, token, e
}
func (s *Service) SearchSeries(ctx context.Context, query string, page int) (SeriesResults, error) {
	p, token, e := s.seriesProvider(ctx)
	if e != nil {
		return SeriesResults{}, e
	}
	return p.SearchSeries(ctx, token, query, page)
}
func (s *Service) Series(ctx context.Context, id int64) (Series, error) {
	p, token, e := s.seriesProvider(ctx)
	if e != nil {
		return Series{}, e
	}
	return p.Series(ctx, token, id)
}
func (s *Service) Season(ctx context.Context, id int64, number int) (Season, error) {
	p, token, e := s.seriesProvider(ctx)
	if e != nil {
		return Season{}, e
	}
	return p.Season(ctx, token, id, number)
}
