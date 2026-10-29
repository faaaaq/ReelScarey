package metadata

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type EpisodeGroup struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Type         int    `json:"type"`
	EpisodeCount int    `json:"episode_count"`
	GroupCount   int    `json:"group_count"`
}
type GroupEpisode struct {
	ID     int64  `json:"id"`
	ShowID int64  `json:"show_id"`
	Season int    `json:"season_number"`
	Number int    `json:"episode_number"`
	Order  int    `json:"order"`
	Name   string `json:"name"`
}
type EpisodeSubgroup struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Order    int            `json:"order"`
	Episodes []GroupEpisode `json:"episodes"`
}
type EpisodeGroupDetails struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Type   int               `json:"type"`
	Groups []EpisodeSubgroup `json:"groups"`
}
type EpisodeGroupProvider interface {
	EpisodeGroups(context.Context, string, int64) ([]EpisodeGroup, error)
	EpisodeGroup(context.Context, string, int64, string) (EpisodeGroupDetails, error)
}

var groupIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)

func (c *Client) EpisodeGroups(ctx context.Context, token string, id int64) ([]EpisodeGroup, error) {
	var out struct {
		ID      int64          `json:"id"`
		Results []EpisodeGroup `json:"results"`
	}
	if e := c.request(ctx, token, "/tv/"+strconv.FormatInt(id, 10)+"/episode_groups", nil, &out); e != nil {
		return nil, e
	}
	if out.ID != id || len(out.Results) > 100 {
		return nil, animeProviderError("tmdb_invalid_response", "TMDb returned invalid episode orders.")
	}
	seen := map[string]bool{}
	for _, g := range out.Results {
		if !groupIDPattern.MatchString(g.ID) || seen[g.ID] || strings.TrimSpace(g.Name) == "" || !validAnimeText(g.Name) || g.Type < 1 || g.Type > 7 || g.EpisodeCount < 0 || g.EpisodeCount > 5000 || g.GroupCount < 0 || g.GroupCount > 500 {
			return nil, animeProviderError("tmdb_invalid_response", "TMDb returned invalid episode orders.")
		}
		seen[g.ID] = true
	}
	if out.Results == nil {
		out.Results = []EpisodeGroup{}
	}
	return out.Results, nil
}
func (c *Client) EpisodeGroup(ctx context.Context, token string, seriesID int64, id string) (EpisodeGroupDetails, error) {
	if !groupIDPattern.MatchString(id) {
		return EpisodeGroupDetails{}, animeProviderError("invalid_anime_order", "Choose an episode order listed for this show.")
	}
	groups, e := c.EpisodeGroups(ctx, token, seriesID)
	if e != nil {
		return EpisodeGroupDetails{}, e
	}
	found := false
	for _, g := range groups {
		if g.ID == id {
			found = true
		}
	}
	if !found {
		return EpisodeGroupDetails{}, animeProviderError("invalid_anime_order", "That episode order does not belong to this show.")
	}
	var out EpisodeGroupDetails
	if e = c.requestLimit(ctx, token, "/tv/episode_group/"+url.PathEscape(id), nil, &out, 8<<20); e != nil {
		return out, e
	}
	if out.ID != id || len(out.Groups) > 500 || !validAnimeText(out.Name) {
		return out, animeProviderError("tmdb_invalid_response", "TMDb returned an invalid episode order.")
	}
	total := 0
	seen := map[string]bool{}
	for _, g := range out.Groups {
		if !groupIDPattern.MatchString(g.ID) || seen[g.ID] || !validAnimeText(g.Name) || g.Order < 0 || g.Order > 10000 {
			return out, animeProviderError("tmdb_invalid_response", "TMDb returned invalid order groups.")
		}
		seen[g.ID] = true
		total += len(g.Episodes)
		for _, ep := range g.Episodes {
			if ep.ID < 1 || ep.ShowID != seriesID || ep.Season < 0 || ep.Season > 10000 || ep.Number < 1 || ep.Number > 100000 || ep.Order < 0 || ep.Order >= 5000 || !validAnimeText(ep.Name) {
				return out, animeProviderError("tmdb_invalid_response", "TMDb returned foreign or invalid order episodes.")
			}
		}
	}
	if total > 5000 {
		return out, animeProviderError("tmdb_invalid_response", "This episode order exceeds 5000 entries.")
	}
	return out, nil
}
func (s *Service) EpisodeGroups(ctx context.Context, id int64) ([]EpisodeGroup, error) {
	p, ok := s.provider.(EpisodeGroupProvider)
	if !ok {
		return nil, animeProviderError("metadata_unavailable", "Episode orders are unavailable from this provider.")
	}
	token, e := s.token(ctx)
	if e != nil {
		return nil, e
	}
	return p.EpisodeGroups(ctx, token, id)
}
func (s *Service) EpisodeGroup(ctx context.Context, seriesID int64, id string) (EpisodeGroupDetails, error) {
	p, ok := s.provider.(EpisodeGroupProvider)
	if !ok {
		return EpisodeGroupDetails{}, animeProviderError("metadata_unavailable", "Episode orders are unavailable from this provider.")
	}
	token, e := s.token(ctx)
	if e != nil {
		return EpisodeGroupDetails{}, e
	}
	return p.EpisodeGroup(ctx, token, seriesID, id)
}
