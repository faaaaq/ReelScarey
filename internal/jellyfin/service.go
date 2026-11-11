// Package jellyfin reads an administrator-configured Jellyfin server. It never
// changes playback or media; the only write is an explicit library refresh.
package jellyfin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/securestore"
)

var (
	ErrConfig      = errors.New("Enter an HTTP or HTTPS Jellyfin server URL and API key.")
	ErrUnavailable = errors.New("Could not read the Jellyfin server. Check the URL, API key and permissions.")
)

type Config struct {
	URL    string `json:"url"`
	APIKey string `json:"api_key,omitempty"`
}

type Settings struct {
	URL        string `json:"url"`
	Configured bool   `json:"configured"`
}

type Activity struct {
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
	return &Service{secrets: s, client: &http.Client{
		Timeout:   15 * time.Second,
		Transport: &http.Transport{Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirects disabled")
		},
	}}
}

func (s *Service) config(ctx context.Context) (Config, error) {
	var c Config
	data, err := s.secrets.GetNamed(ctx, "jellyfin")
	if err != nil {
		return c, err
	}
	if len(data) > 0 {
		err = json.Unmarshal(data, &c)
	}
	return c, err
}

func (s *Service) Settings(ctx context.Context) (Settings, error) {
	c, err := s.config(ctx)
	return Settings{URL: c.URL, Configured: c.APIKey != ""}, err
}

func (s *Service) Save(ctx context.Context, c Config) error {
	u, err := url.Parse(strings.TrimSpace(c.URL))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(c.URL) > 1024 || strings.ContainsAny(c.URL, "\r\n\\") || strings.Contains(u.Path, "..") {
		return ErrConfig
	}
	c.URL = strings.TrimRight(u.String(), "/")
	if c.APIKey == "" {
		old, err := s.config(ctx)
		if err != nil {
			return err
		}
		if old.URL != c.URL {
			return ErrConfig
		}
		c.APIKey = old.APIKey
	}
	if len(c.APIKey) < 1 || len(c.APIKey) > 512 || strings.ContainsAny(c.APIKey, "\r\n") {
		return ErrConfig
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return s.secrets.SetNamed(ctx, "jellyfin", data)
}

func (s *Service) Delete(ctx context.Context) error { return s.secrets.DeleteNamed(ctx, "jellyfin") }

func (s *Service) request(ctx context.Context, method, path string, out any) error {
	c, err := s.config(ctx)
	if err != nil {
		return err
	}
	if c.APIKey == "" || c.URL == "" {
		return ErrConfig
	}
	req, err := http.NewRequestWithContext(ctx, method, c.URL+path, nil)
	if err != nil {
		return ErrUnavailable
	}
	req.Header.Set("X-Emby-Token", c.APIKey)
	req.Header.Set("Accept", "application/json")
	response, err := s.client.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNoContent {
		return ErrUnavailable
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
	if err != nil || len(data) > 4<<20 {
		return ErrUnavailable
	}
	if err := json.Unmarshal(data, out); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) Recent(ctx context.Context) (Result, error) {
	var payload struct {
		Items []struct {
			Name              string `json:"Name"`
			SeriesName        string `json:"SeriesName"`
			Type              string `json:"Type"`
			IndexNumber       *int   `json:"IndexNumber"`
			ParentIndexNumber *int   `json:"ParentIndexNumber"`
			RunTimeTicks      int64  `json:"RunTimeTicks"`
			UserData          struct {
				LastPlayedDate        string  `json:"LastPlayedDate"`
				PlayCount             int     `json:"PlayCount"`
				Played                bool    `json:"Played"`
				PlayedPercentage      float64 `json:"PlayedPercentage"`
				PlaybackPositionTicks int64   `json:"PlaybackPositionTicks"`
			} `json:"UserData"`
		} `json:"Items"`
	}
	path := "/Items?Recursive=true&IncludeItemTypes=Movie,Episode&SortBy=DatePlayed&SortOrder=Descending&Limit=200&Fields=SeriesName,IndexNumber,ParentIndexNumber,UserData"
	if err := s.request(ctx, http.MethodGet, path, &payload); err != nil {
		return Result{}, err
	}
	items := make([]Activity, 0, len(payload.Items))
	for _, item := range payload.Items {
		progress := 0.0
		if item.RunTimeTicks > 0 {
			progress = float64(item.UserData.PlaybackPositionTicks) / float64(item.RunTimeTicks)
		} else if item.UserData.PlayedPercentage > 0 {
			progress = item.UserData.PlayedPercentage / 100
		}
		last := time.Time{}
		if item.UserData.LastPlayedDate != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, item.UserData.LastPlayedDate); err == nil {
				last = parsed
			}
		}
		items = append(items, Activity{
			Title: item.Name, Series: item.SeriesName, Kind: strings.ToLower(item.Type),
			Season: item.ParentIndexNumber, Episode: item.IndexNumber, LastPlayed: last,
			PlayCount: item.UserData.PlayCount, Played: item.UserData.Played || item.UserData.PlayCount > 0,
			Progress: clamp(progress),
		})
	}
	return Result{Items: items, Truncated: len(items) >= 200, CheckedAt: time.Now()}, nil
}

func (s *Service) Refresh(ctx context.Context) error {
	return s.request(ctx, http.MethodPost, "/Library/Refresh", nil)
}

func clamp(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
