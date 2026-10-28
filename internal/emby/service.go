// Package emby reads an administrator-configured Emby server without changing playback.
package emby

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/securestore"
)

var ErrConfig = errors.New("Enter an HTTP or HTTPS Emby server URL and API key.")
var ErrUnavailable = errors.New("Could not read Emby activity. Check the server URL, API key and permissions.")
var userID = regexp.MustCompile(`^[a-zA-Z0-9-]{1,80}$`)

type Config struct {
	URL    string `json:"url"`
	APIKey string `json:"api_key,omitempty"`
}
type Settings struct {
	URL        string `json:"url"`
	Configured bool   `json:"configured"`
}
type Activity struct {
	User       string    `json:"user"`
	Title      string    `json:"title"`
	Series     string    `json:"series"`
	Kind       string    `json:"kind"`
	Season     *int      `json:"season"`
	Episode    *int      `json:"episode"`
	LastPlayed time.Time `json:"last_played"`
	PlayCount  int       `json:"play_count"`
	Played     bool      `json:"played"`
	Progress   float64   `json:"progress"`
}
type Result struct {
	Items     []Activity `json:"items"`
	Truncated bool       `json:"truncated"`
	CheckedAt time.Time  `json:"checked_at"`
}
type Service struct {
	secrets *securestore.Store
	client  *http.Client
}

func New(s *securestore.Store) *Service {
	return &Service{secrets: s, client: &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirects disabled") }}}
}
func (s *Service) config(ctx context.Context) (Config, error) {
	var c Config
	data, e := s.secrets.GetNamed(ctx, "emby")
	if e != nil {
		return c, e
	}
	if len(data) > 0 {
		e = json.Unmarshal(data, &c)
	}
	return c, e
}
func (s *Service) Settings(ctx context.Context) (Settings, error) {
	c, e := s.config(ctx)
	return Settings{URL: c.URL, Configured: c.APIKey != ""}, e
}
func (s *Service) Save(ctx context.Context, c Config) error {
	u, e := url.Parse(strings.TrimSpace(c.URL))
	if e != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(c.URL) > 1024 || strings.ContainsAny(c.URL, "\r\n\\") || strings.Contains(u.Path, "..") {
		return ErrConfig
	}
	c.URL = strings.TrimRight(u.String(), "/")
	if c.APIKey == "" {
		old, e := s.config(ctx)
		if e != nil {
			return e
		}
		if old.URL != c.URL {
			return ErrConfig
		}
		c.APIKey = old.APIKey
	}
	if len(c.APIKey) < 1 || len(c.APIKey) > 512 || strings.ContainsAny(c.APIKey, "\r\n") {
		return ErrConfig
	}
	data, e := json.Marshal(c)
	if e != nil {
		return e
	}
	return s.secrets.SetNamed(ctx, "emby", data)
}
func (s *Service) Delete(ctx context.Context) error { return s.secrets.DeleteNamed(ctx, "emby") }
func (s *Service) get(ctx context.Context, c Config, path string, out any) error {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, c.URL+path, nil)
	if e != nil {
		return ErrUnavailable
	}
	req.Header.Set("X-Emby-Token", c.APIKey)
	req.Header.Set("Accept", "application/json")
	res, e := s.client.Do(req)
	if e != nil {
		return ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ErrUnavailable
	}
	data, e := io.ReadAll(io.LimitReader(res.Body, 2<<20+1))
	if e != nil || len(data) > 2<<20 {
		return ErrUnavailable
	}
	if json.Unmarshal(data, out) != nil {
		return ErrUnavailable
	}
	return nil
}
func (s *Service) Recent(ctx context.Context) (Result, error) {
	out := Result{Items: []Activity{}, CheckedAt: time.Now().UTC()}
	c, e := s.config(ctx)
	if e != nil {
		return out, e
	}
	if c.APIKey == "" {
		return out, ErrConfig
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var users struct {
		Items []struct {
			ID   string `json:"Id"`
			Name string `json:"Name"`
		} `json:"Items"`
		Total int `json:"TotalRecordCount"`
	}
	if e = s.get(ctx, c, "/Users/Query?Limit=20", &users); e != nil {
		return out, e
	}
	if len(users.Items) > 20 {
		return out, ErrUnavailable
	}
	out.Truncated = users.Total > len(users.Items)
	for _, u := range users.Items {
		if !userID.MatchString(u.ID) {
			return out, ErrUnavailable
		}
		var items struct {
			Items []struct {
				Name     string `json:"Name"`
				Series   string `json:"SeriesName"`
				Kind     string `json:"Type"`
				Season   *int   `json:"ParentIndexNumber"`
				Episode  *int   `json:"IndexNumber"`
				UserData struct {
					LastPlayed *time.Time `json:"LastPlayedDate"`
					PlayCount  int        `json:"PlayCount"`
					Played     bool       `json:"Played"`
					Progress   float64    `json:"PlayedPercentage"`
				} `json:"UserData"`
			} `json:"Items"`
			Total int `json:"TotalRecordCount"`
		}
		path := "/Users/" + u.ID + "/Items?Recursive=true&IncludeItemTypes=Movie%2CEpisode&SortBy=DatePlayed&SortOrder=Descending&Limit=100&EnableUserData=true"
		if e = s.get(ctx, c, path, &items); e != nil {
			return out, e
		}
		if len(items.Items) > 100 {
			return out, ErrUnavailable
		}
		if items.Total > 100 {
			out.Truncated = true
		}
		for _, v := range items.Items {
			if v.UserData.LastPlayed == nil || v.UserData.LastPlayed.IsZero() {
				continue
			}
			out.Items = append(out.Items, Activity{User: u.Name, Title: v.Name, Series: v.Series, Kind: v.Kind, Season: v.Season, Episode: v.Episode, LastPlayed: *v.UserData.LastPlayed, PlayCount: v.UserData.PlayCount, Played: v.UserData.Played, Progress: v.UserData.Progress})
		}
	}
	sort.SliceStable(out.Items, func(i, j int) bool { return out.Items[i].LastPlayed.After(out.Items[j].LastPlayed) })
	return out, nil
}
