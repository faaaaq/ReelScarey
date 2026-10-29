package metadata

import (
	"context"
	"net/url"
	"regexp"
	"strings"
)

var imdbIDPattern = regexp.MustCompile(`^tt[0-9]{5,12}$`)

type ExternalTitle struct {
	Kind   string `json:"kind"`
	TMDbID int64  `json:"tmdb_id"`
	Title  string `json:"title"`
}
type imdbFinder interface {
	FindIMDb(context.Context, string, string) (ExternalTitle, error)
}

func (s *Service) FindIMDb(ctx context.Context, id string) (ExternalTitle, error) {
	if !imdbIDPattern.MatchString(id) {
		return ExternalTitle{}, &Error{Code: "invalid_imdb_id", Message: "Use a valid IMDb title ID."}
	}
	provider, ok := s.provider.(imdbFinder)
	if !ok {
		return ExternalTitle{}, &Error{Code: "tmdb_unavailable", Message: "IMDb matching is unavailable."}
	}
	token, err := s.token(ctx)
	if err != nil {
		return ExternalTitle{}, err
	}
	return provider.FindIMDb(ctx, token, id)
}
func (c *Client) FindIMDb(ctx context.Context, token, id string) (ExternalTitle, error) {
	if !imdbIDPattern.MatchString(id) {
		return ExternalTitle{}, &Error{Code: "invalid_imdb_id", Message: "Use a valid IMDb title ID."}
	}
	var out struct {
		Movies []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
		} `json:"movie_results"`
		Shows []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"tv_results"`
	}
	if err := c.request(ctx, token, "/find/"+id, url.Values{"external_source": {"imdb_id"}}, &out); err != nil {
		return ExternalTitle{}, err
	}
	if len(out.Movies)+len(out.Shows) != 1 {
		return ExternalTitle{}, &Error{Code: "imdb_unmatched", Message: "IMDb title did not resolve to exactly one movie or show."}
	}
	if len(out.Movies) == 1 && out.Movies[0].ID > 0 && strings.TrimSpace(out.Movies[0].Title) != "" {
		return ExternalTitle{Kind: "movie", TMDbID: out.Movies[0].ID, Title: out.Movies[0].Title}, nil
	}
	if len(out.Shows) == 1 && out.Shows[0].ID > 0 && strings.TrimSpace(out.Shows[0].Name) != "" {
		return ExternalTitle{Kind: "series", TMDbID: out.Shows[0].ID, Title: out.Shows[0].Name}, nil
	}
	return ExternalTitle{}, &Error{Code: "imdb_unmatched", Message: "IMDb title did not resolve to exactly one movie or show."}
}
