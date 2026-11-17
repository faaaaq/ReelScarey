package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const tvdbEndpoint = "https://api4.thetvdb.com/v4"
const tvdbSecret = "thetvdb"

var ErrTVDBNotConfigured = &Error{Code: "tvdb_not_configured", Message: "Connect TheTVDB in Settings to refresh this show."}
var tvdbSlug = regexp.MustCompile("^[a-z0-9]+(?:-[a-z0-9]+)*$")
var tvdbArt = regexp.MustCompile(`^https://artworks\.thetvdb\.com/banners/[a-zA-Z0-9/_-]+\.(jpg|png|webp)$`)

type TVDBCredentials struct {
	Key string `json:"key"`
	PIN string `json:"pin,omitempty"`
}

type tvdbClient struct {
	base  string
	http  *http.Client
	mu    sync.Mutex
	creds TVDBCredentials
	token string
	until time.Time
}

func newTVDBClient() *tvdbClient {
	return &tvdbClient{base: tvdbEndpoint, http: &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func tvdbError(code, message string) *Error { return &Error{Code: code, Message: message} }

func (c *tvdbClient) request(ctx context.Context, endpoint, token string, body []byte, out any) error {
	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+endpoint, bytes.NewReader(body))
	if err != nil {
		return tvdbError("tvdb_unavailable", "Could not prepare a request to TheTVDB.")
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return tvdbError("tvdb_unavailable", "TheTVDB is temporarily unavailable.")
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return tvdbError("tvdb_auth", "TheTVDB rejected the connection. Check the API key and subscriber PIN.")
	case http.StatusNotFound:
		return tvdbError("series_not_found", "TheTVDB could not find the show or season.")
	case http.StatusTooManyRequests:
		return tvdbError("tvdb_rate_limited", "TheTVDB is rate limited. Try again later.")
	case http.StatusOK:
	default:
		return tvdbError("tvdb_unavailable", "TheTVDB could not complete the request.")
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (4<<20)+1))
	if err != nil || len(raw) > 4<<20 || json.Unmarshal(raw, out) != nil {
		return tvdbError("tvdb_invalid_response", "TheTVDB returned invalid or incomplete TV metadata.")
	}
	return nil
}

func (c *tvdbClient) auth(ctx context.Context, creds TVDBCredentials, rejected string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.creds == creds && c.token != "" && c.token != rejected && c.until.After(time.Now()) {
		return c.token, nil
	}
	var out struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	raw, _ := json.Marshal(struct {
		Key string `json:"apikey"`
		PIN string `json:"pin,omitempty"`
	}{creds.Key, creds.PIN})
	if err := c.request(ctx, "/login", "", raw, &out); err != nil {
		return "", err
	}
	if out.Data.Token == "" || len(out.Data.Token) > 4096 || strings.ContainsFunc(out.Data.Token, unicode.IsControl) {
		return "", tvdbError("tvdb_invalid_response", "TheTVDB returned an invalid access token.")
	}
	c.creds, c.token, c.until = creds, out.Data.Token, time.Now().Add(28*24*time.Hour)
	return c.token, nil
}

func (c *tvdbClient) get(ctx context.Context, creds TVDBCredentials, endpoint string, out any) error {
	token, err := c.auth(ctx, creds, "")
	if err != nil {
		return err
	}
	err = c.request(ctx, endpoint, token, nil, out)
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != "tvdb_auth" {
		return err
	}
	token, err = c.auth(ctx, creds, token)
	if err != nil {
		return err
	}
	return c.request(ctx, endpoint, token, nil, out)
}

func (s *Service) tvdbCredentials(ctx context.Context) (TVDBCredentials, error) {
	var creds TVDBCredentials
	if s.secrets == nil || s.tvdb == nil {
		return creds, nil
	}
	raw, err := s.secrets.GetNamed(ctx, tvdbSecret)
	if err != nil || raw == nil {
		return creds, err
	}
	err = json.Unmarshal(raw, &creds)
	return creds, err
}

func (s *Service) TVDBConnected(ctx context.Context) (bool, error) {
	creds, err := s.tvdbCredentials(ctx)
	return creds.Key != "", err
}

func (s *Service) ConfigureTVDB(ctx context.Context, creds TVDBCredentials) error {
	creds.Key = strings.TrimSpace(creds.Key)
	creds.PIN = strings.TrimSpace(creds.PIN)
	if creds.Key == "" || len(creds.Key) > 256 || len(creds.PIN) > 128 ||
		strings.ContainsFunc(creds.Key, unicode.IsSpace) || strings.ContainsFunc(creds.PIN, unicode.IsSpace) ||
		strings.ContainsFunc(creds.Key, unicode.IsControl) || strings.ContainsFunc(creds.PIN, unicode.IsControl) {
		return tvdbError("tvdb_invalid_credentials", "Enter a valid TheTVDB API key and optional subscriber PIN.")
	}
	if _, err := s.tvdb.auth(ctx, creds, ""); err != nil {
		return err
	}
	raw, _ := json.Marshal(creds)
	return s.secrets.SetNamed(ctx, tvdbSecret, raw)
}

func (s *Service) RemoveTVDB(ctx context.Context) error {
	return s.secrets.DeleteNamed(ctx, tvdbSecret)
}

type tvdbSeriesRecord struct {
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	Overview          string `json:"overview"`
	Image             string `json:"image"`
	FirstAired        string `json:"firstAired"`
	OriginalLanguage  string `json:"originalLanguage"`
	DefaultSeasonType int    `json:"defaultSeasonType"`
	Status            struct {
		Name string `json:"name"`
	} `json:"status"`
	Seasons []struct {
		ID     int64  `json:"id"`
		Number int    `json:"number"`
		Name   string `json:"name"`
		Year   string `json:"year"`
		Type   struct {
			ID int `json:"id"`
		} `json:"type"`
	} `json:"seasons"`
	Genres []struct {
		Name string `json:"name"`
	} `json:"genres"`
	RemoteIDs []struct {
		ID string `json:"id"`
	} `json:"remoteIds"`
}

func tvdbMovie(id int64, title, overview, image, date, original string) (Movie, error) {
	out, err := normalize(movieResponse{ID: id, Title: title, Overview: overview, ReleaseDate: date, OriginalLanguage: original})
	if err != nil {
		return Movie{}, tvdbError("tvdb_invalid_response", "TheTVDB returned an invalid show title.")
	}
	out.TMDbID = -id
	out.TVDbID = id
	if tvdbArt.MatchString(image) {
		out.PosterPath, out.PosterURL = image, image
	}
	return out, nil
}

func (c *tvdbClient) search(ctx context.Context, creds TVDBCredentials, query string, page int) (SeriesResults, error) {
	const limit = 25
	params := url.Values{"query": {query}, "type": {"series"}, "limit": {"25"}, "offset": {strconv.Itoa((page - 1) * limit)}}
	var result struct {
		Data []struct {
			ID         json.Number `json:"tvdb_id"`
			Type       string      `json:"type"`
			Name       string      `json:"name"`
			Overview   string      `json:"overview"`
			Image      string      `json:"image_url"`
			FirstAired string      `json:"first_air_time"`
			Language   string      `json:"primary_language"`
			Status     string      `json:"status"`
		} `json:"data"`
		Links struct {
			Next  *string `json:"next"`
			Total int     `json:"total_items"`
		} `json:"links"`
	}
	if err := c.get(ctx, creds, "/search?"+params.Encode(), &result); err != nil {
		return SeriesResults{}, err
	}
	out := SeriesResults{Page: page, TotalPages: page, TotalResults: (page - 1) * limit, Items: []Series{}}
	for _, found := range result.Data {
		id, err := found.ID.Int64()
		if err != nil || id < 1 || id > 9007199254740991 || found.Type != "" && found.Type != "series" {
			continue
		}
		m, err := tvdbMovie(id, found.Name, found.Overview, found.Image, found.FirstAired, found.Language)
		if err == nil {
			out.Items = append(out.Items, Series{Movie: m, SeriesStatus: found.Status, Seasons: []SeasonSummary{}})
		}
	}
	out.TotalResults += len(out.Items)
	if result.Links.Total > out.TotalResults {
		out.TotalResults = result.Links.Total
		out.TotalPages = min(500, max(page, (result.Links.Total+limit-1)/limit))
	} else if result.Links.Next != nil && *result.Links.Next != "" {
		out.TotalPages = min(500, page+1)
	}
	return out, nil
}

func (c *tvdbClient) record(ctx context.Context, creds TVDBCredentials, id int64) (tvdbSeriesRecord, error) {
	var out struct {
		Data tvdbSeriesRecord `json:"data"`
	}
	if err := c.get(ctx, creds, fmt.Sprintf("/series/%d/extended", id), &out); err != nil {
		return out.Data, err
	}
	if out.Data.ID != id || len(out.Data.Seasons) > 2000 {
		return tvdbSeriesRecord{}, tvdbError("tvdb_invalid_response", "TheTVDB returned different or incomplete show data.")
	}
	return out.Data, nil
}

func tvdbSeries(record tvdbSeriesRecord) (Series, error) {
	m, err := tvdbMovie(record.ID, record.Name, record.Overview, record.Image, record.FirstAired, record.OriginalLanguage)
	if err != nil {
		return Series{}, err
	}
	status := record.Status.Name
	switch strings.ToLower(status) {
	case "continuing":
		status = "Returning Series"
	case "upcoming":
		status = "In Production"
	}
	out := Series{Movie: m, SeriesStatus: status, Seasons: []SeasonSummary{}}
	order := record.DefaultSeasonType
	if order == 0 {
		order = 1
	}
	for _, season := range record.Seasons {
		if season.Type.ID != 0 && season.Type.ID != order {
			continue
		}
		if season.ID < 1 || season.Number < 0 || season.Number > 10000 || slices.ContainsFunc(out.Seasons, func(v SeasonSummary) bool { return v.Number == season.Number }) {
			return Series{}, tvdbError("tvdb_invalid_response", "TheTVDB returned ambiguous season numbering.")
		}
		name := strings.TrimSpace(season.Name)
		if name == "" {
			name = fmt.Sprintf("Season %d", season.Number)
			if season.Number == 0 {
				name = "Specials"
			}
		}
		date := ""
		if season.Number == 1 {
			date = m.ReleaseDate
		}
		if date == "" && len(season.Year) == 4 {
			date = airDate(season.Year + "-01-01")
		}
		out.Seasons = append(out.Seasons, SeasonSummary{TMDbID: -season.ID, Number: season.Number, Title: name, AirDate: date})
	}
	return out, nil
}

func (c *tvdbClient) season(ctx context.Context, creds TVDBCredentials, id int64, number int) (Season, error) {
	record, err := c.record(ctx, creds, id)
	if err != nil {
		return Season{}, err
	}
	show, err := tvdbSeries(record)
	if err != nil {
		return Season{}, err
	}
	index := slices.IndexFunc(show.Seasons, func(v SeasonSummary) bool { return v.Number == number })
	if index < 0 {
		return Season{}, tvdbError("series_not_found", "This season is missing from TheTVDB's default order.")
	}
	out := Season{SeasonSummary: show.Seasons[index], Episodes: []Episode{}}
	ids := map[int64]bool{}
	numbers := map[int]bool{}
	for page := 0; page < 25; page++ {
		params := url.Values{"season": {strconv.Itoa(number)}, "page": {strconv.Itoa(page)}}
		var payload struct {
			Data struct {
				Episodes []struct {
					ID       int64  `json:"id"`
					SeriesID int64  `json:"seriesId"`
					Number   int    `json:"number"`
					Season   int    `json:"seasonNumber"`
					Name     string `json:"name"`
					Overview string `json:"overview"`
					Aired    string `json:"aired"`
					Runtime  *int   `json:"runtime"`
				} `json:"episodes"`
			} `json:"data"`
			Links struct {
				Next *string `json:"next"`
			} `json:"links"`
		}
		if err = c.get(ctx, creds, fmt.Sprintf("/series/%d/episodes/default?%s", id, params.Encode()), &payload); err != nil {
			return Season{}, err
		}
		for _, ep := range payload.Data.Episodes {
			if ep.ID < 1 || ep.SeriesID != 0 && ep.SeriesID != id || ep.Season != number || ep.Number < 1 || ep.Number > 100000 || ids[ep.ID] || numbers[ep.Number] || len(out.Episodes) >= 2000 {
				return Season{}, tvdbError("tvdb_invalid_response", "TheTVDB returned invalid or ambiguous episodes.")
			}
			ids[ep.ID], numbers[ep.Number] = true, true
			name := strings.TrimSpace(ep.Name)
			if name == "" {
				name = fmt.Sprintf("Episode %d", ep.Number)
			}
			m, err := normalize(movieResponse{ID: ep.ID, Title: name, Overview: ep.Overview, Runtime: ep.Runtime})
			if err != nil {
				return Season{}, tvdbError("tvdb_invalid_response", "TheTVDB returned an invalid episode title.")
			}
			date := airDate(ep.Aired)
			if date != "" && (out.AirDate == "" || date < out.AirDate) {
				out.AirDate = date
			}
			out.Episodes = append(out.Episodes, Episode{TMDbID: -ep.ID, Number: ep.Number, Title: m.Title, Overview: m.Overview, AirDate: date, RuntimeMinutes: m.RuntimeMinutes})
		}
		if payload.Links.Next == nil || *payload.Links.Next == "" {
			out.EpisodeCount = len(out.Episodes)
			return out, nil
		}
	}
	return Season{}, tvdbError("tvdb_invalid_response", "TheTVDB season exceeds the episode page limit.")
}

func titleKey(title string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, title)
}

func tvdbTitleQuery(query string) (string, bool) {
	u, err := url.Parse(query)
	if err != nil || u.Scheme != "https" || (u.Host != "thetvdb.com" && u.Host != "www.thetvdb.com") || u.User != nil {
		return query, false
	}
	slug := strings.TrimPrefix(strings.TrimSuffix(u.Path, "/"), "/series/")
	if !tvdbSlug.MatchString(slug) {
		return query, false
	}
	return strings.ReplaceAll(slug, "-", " "), true
}
