// Package plex reads an administrator-configured Plex server. It never changes
// playback or media; the only write is an explicit library-section refresh.
package plex

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/securestore"
)

var (
	ErrConfig      = errors.New("Enter an HTTP or HTTPS Plex server URL and X-Plex-Token.")
	ErrUnavailable = errors.New("Could not read the Plex server. Check the URL, token and permissions.")
)

type Config struct {
	URL       string `json:"url"`
	Token     string `json:"token,omitempty"`
	SectionID string `json:"section_id,omitempty"`
}

type Settings struct {
	URL        string `json:"url"`
	SectionID  string `json:"section_id"`
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
	data, err := s.secrets.GetNamed(ctx, "plex")
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
	return Settings{URL: c.URL, SectionID: c.SectionID, Configured: c.Token != ""}, err
}

func (s *Service) Save(ctx context.Context, c Config) error {
	u, err := url.Parse(strings.TrimSpace(c.URL))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(c.URL) > 1024 || strings.ContainsAny(c.URL, "\r\n\\") || strings.Contains(u.Path, "..") {
		return ErrConfig
	}
	c.URL = strings.TrimRight(u.String(), "/")
	c.SectionID = strings.TrimSpace(c.SectionID)
	if c.SectionID != "" && !digits(c.SectionID) {
		return ErrConfig
	}
	if c.Token == "" {
		old, err := s.config(ctx)
		if err != nil {
			return err
		}
		if old.URL != c.URL {
			return ErrConfig
		}
		c.Token = old.Token
	}
	if len(c.Token) < 8 || len(c.Token) > 512 || strings.ContainsAny(c.Token, "\r\n") {
		return ErrConfig
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return s.secrets.SetNamed(ctx, "plex", data)
}

func (s *Service) Delete(ctx context.Context) error { return s.secrets.DeleteNamed(ctx, "plex") }

func digits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}

func (s *Service) request(ctx context.Context, method, path string) ([]byte, error) {
	c, err := s.config(ctx)
	if err != nil {
		return nil, err
	}
	if c.Token == "" || c.URL == "" {
		return nil, ErrConfig
	}
	req, err := http.NewRequestWithContext(ctx, method, c.URL+path, nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	req.Header.Set("X-Plex-Token", c.Token)
	req.Header.Set("X-Plex-Product", "ReelScarey")
	req.Header.Set("Accept", "application/xml")
	response, err := s.client.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNoContent {
		return nil, ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
	if err != nil || len(data) > 4<<20 {
		return nil, ErrUnavailable
	}
	return data, nil
}

type mediaContainer struct {
	XMLName xml.Name `xml:"MediaContainer"`
	Size    int      `xml:"size,attr"`
	Videos  []struct {
		Title            string `xml:"title,attr"`
		Type             string `xml:"type,attr"`
		GrandparentTitle string `xml:"grandparentTitle,attr"`
		ParentIndex      int    `xml:"parentIndex,attr"`
		Index            int    `xml:"index,attr"`
		AddedAt          int64  `xml:"addedAt,attr"`
		LastViewedAt     int64  `xml:"lastViewedAt,attr"`
		ViewCount        int    `xml:"viewCount,attr"`
		ViewOffset       int64  `xml:"viewOffset,attr"`
		Duration         int64  `xml:"duration,attr"`
	} `xml:"Video"`
}

func (s *Service) Recent(ctx context.Context) (Result, error) {
	data, err := s.request(ctx, http.MethodGet, "/library/recentlyAdded?X-Plex-Container-Start=0&X-Plex-Container-Size=200")
	if err != nil {
		return Result{}, err
	}
	var container mediaContainer
	if err := xml.Unmarshal(data, &container); err != nil {
		return Result{}, ErrUnavailable
	}
	items := make([]Activity, 0, len(container.Videos))
	for _, video := range container.Videos {
		kind := strings.ToLower(video.Type)
		if kind != "movie" && kind != "episode" {
			continue
		}
		var season, episode *int
		if kind == "episode" {
			value := video.ParentIndex
			episodeValue := video.Index
			season, episode = &value, &episodeValue
		}
		progress := 0.0
		if video.Duration > 0 {
			progress = float64(video.ViewOffset) / float64(video.Duration)
		}
		stamp := video.LastViewedAt
		if stamp == 0 {
			stamp = video.AddedAt
		}
		items = append(items, Activity{
			Title: video.Title, Series: video.GrandparentTitle, Kind: kind,
			Season: season, Episode: episode, LastPlayed: time.Unix(stamp, 0).UTC(),
			PlayCount: video.ViewCount, Played: video.ViewCount > 0, Progress: clamp(progress),
		})
	}
	return Result{Items: items, Truncated: container.Size > 200, CheckedAt: time.Now()}, nil
}

func (s *Service) Refresh(ctx context.Context) error {
	c, err := s.config(ctx)
	if err != nil {
		return err
	}
	path := "/library/refresh"
	if c.SectionID != "" {
		path = "/library/sections/" + c.SectionID + "/refresh"
	}
	_, err = s.request(ctx, http.MethodGet, path)
	return err
}

func (s *Service) Sections(ctx context.Context) ([]string, error) {
	data, err := s.request(ctx, http.MethodGet, "/library/sections")
	if err != nil {
		return nil, err
	}
	var container struct {
		Directories []struct {
			Key   string `xml:"key,attr"`
			Title string `xml:"title,attr"`
			Type  string `xml:"type,attr"`
		} `xml:"Directory"`
	}
	if err := xml.Unmarshal(data, &container); err != nil {
		return nil, ErrUnavailable
	}
	out := []string{}
	for _, directory := range container.Directories {
		out = append(out, directory.Key+": "+directory.Title+" ("+directory.Type+")")
	}
	return out, nil
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
