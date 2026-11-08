package metadata

import (
	"context"
	"net/url"
	"regexp"
	"strings"
)

var tvdbIDPattern = regexp.MustCompile(`^[0-9]{1,12}$`)

type tvdbFinder interface {
	FindTVDB(context.Context, string, string) (ExternalTitle, error)
}

// FindTVDB resolves one TVDB series ID to a TMDb show. It is used only by the
// explicit backup import; no stored library identity changes implicitly.
func (s *Service) FindTVDB(ctx context.Context, id string) (ExternalTitle, error) {
	if !tvdbIDPattern.MatchString(id) {
		return ExternalTitle{}, &Error{Code: "invalid_tvdb_id", Message: "Use a valid TVDB series ID."}
	}
	provider, ok := s.provider.(tvdbFinder)
	if !ok {
		return ExternalTitle{}, &Error{Code: "tmdb_unavailable", Message: "TVDB matching is unavailable."}
	}
	token, err := s.token(ctx)
	if err != nil {
		return ExternalTitle{}, err
	}
	return provider.FindTVDB(ctx, token, id)
}

func (c *Client) FindTVDB(ctx context.Context, token, id string) (ExternalTitle, error) {
	if !tvdbIDPattern.MatchString(id) {
		return ExternalTitle{}, &Error{Code: "invalid_tvdb_id", Message: "Use a valid TVDB series ID."}
	}
	var out struct {
		Shows []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"tv_results"`
	}
	if err := c.request(ctx, token, "/find/"+id, url.Values{"external_source": {"tvdb_id"}}, &out); err != nil {
		return ExternalTitle{}, err
	}
	if len(out.Shows) != 1 || out.Shows[0].ID < 1 || strings.TrimSpace(out.Shows[0].Name) == "" {
		return ExternalTitle{}, &Error{Code: "tvdb_unmatched", Message: "TVDB series did not resolve to exactly one TMDb show."}
	}
	return ExternalTitle{Kind: "series", TMDbID: out.Shows[0].ID, Title: out.Shows[0].Name}, nil
}
