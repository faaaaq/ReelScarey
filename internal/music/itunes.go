package music

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultiTunesBaseURL is the public iTunes Search API.
const DefaultiTunesBaseURL = "https://itunes.apple.com"

// itunesRequestInterval paces requests so bounded batch matching stays well
// inside the documented usage of the public search endpoint.
const itunesRequestInterval = 300 * time.Millisecond

// itunesTimeout bounds one search request.
const itunesTimeout = 10 * time.Second

// iTunester is the injectable iTunes Search API used for corroborating
// artist/title evidence and recovering plausible names when MusicBrainz is
// unavailable. It never provides MusicBrainz identifiers.
type iTunester interface {
	SearchTracks(ctx context.Context, term string, limit int) ([]iTunesTrack, error)
}

// iTunesClient calls the public iTunes Search API. It is paced client-side and
// needs no credential.
type iTunesClient struct {
	BaseURL string
	HTTP    *http.Client

	mu       chan struct{}
	lastCall time.Time
}

// NewiTunesClient builds the default public client.
func NewiTunesClient() *iTunesClient {
	return &iTunesClient{BaseURL: DefaultiTunesBaseURL, HTTP: &http.Client{Timeout: itunesTimeout}, mu: make(chan struct{}, 1)}
}

// iTunesTrack is one song result from the iTunes Search API.
type iTunesTrack struct {
	ArtistName     string
	TrackName      string
	CollectionName string
	DurationMS     int
}

type itunesSearchBody struct {
	ResultCount int              `json:"resultCount"`
	Results     []itunesResultJS `json:"results"`
}

type itunesResultJS struct {
	WrapperType     string `json:"wrapperType"`
	Kind            string `json:"kind"`
	ArtistName      string `json:"artistName"`
	TrackName       string `json:"trackName"`
	CollectionName  string `json:"collectionName"`
	TrackTimeMillis *int64 `json:"trackTimeMillis"`
}

// SearchTracks searches songs by a free-text term. Empty or oversized terms
// return no results rather than an error, because a missing term is not a
// provider failure.
func (c *iTunesClient) SearchTracks(ctx context.Context, term string, limit int) ([]iTunesTrack, error) {
	term = strings.TrimSpace(term)
	if term == "" || len(term) > 200 {
		return nil, nil
	}
	if limit < 1 || limit > 25 {
		limit = 10
	}
	if err := c.pace(ctx); err != nil {
		return nil, err
	}
	endpoint := c.BaseURL + "/search?term=" + url.QueryEscape(term) + "&entity=song&limit=" + strconv.Itoa(limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, &Error{Code: "music_itunes_unavailable", Message: "The iTunes search could not be reached."}
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, &Error{Code: "music_itunes_unavailable", Message: "The iTunes search could not be reached. Check your network and try again."}
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusTooManyRequests:
		return nil, &Error{Code: "music_itunes_rate_limited", Message: "iTunes is limiting requests. Matching continues with the other providers.", RetryAfter: 60}
	default:
		return nil, &Error{Code: "music_itunes_unavailable", Message: "The iTunes search is temporarily unavailable. Try again later."}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, &Error{Code: "music_itunes_invalid_response", Message: "The iTunes search returned an invalid response."}
	}
	var parsed itunesSearchBody
	if json.Unmarshal(body, &parsed) != nil {
		return nil, &Error{Code: "music_itunes_invalid_response", Message: "The iTunes search returned an invalid response."}
	}
	tracks := make([]iTunesTrack, 0, len(parsed.Results))
	for _, raw := range parsed.Results {
		if raw.WrapperType != "" && raw.WrapperType != "track" {
			continue
		}
		artist, okArtist := cleanText(raw.ArtistName, 300)
		title, okTitle := cleanText(raw.TrackName, 500)
		if !okArtist || !okTitle {
			continue
		}
		track := iTunesTrack{ArtistName: artist, TrackName: title}
		if collection, ok := cleanText(raw.CollectionName, 500); ok {
			track.CollectionName = collection
		}
		if raw.TrackTimeMillis != nil && *raw.TrackTimeMillis > 0 {
			track.DurationMS = int(*raw.TrackTimeMillis)
		}
		tracks = append(tracks, track)
	}
	return tracks, nil
}

// pace serializes requests with a small spacing so batch matching cannot
// hammer the public endpoint.
func (c *iTunesClient) pace(ctx context.Context) error {
	if c.mu == nil {
		c.mu = make(chan struct{}, 1)
	}
	select {
	case c.mu <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-c.mu }()
	wait := itunesRequestInterval - time.Since(c.lastCall)
	if wait > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	c.lastCall = time.Now()
	return nil
}

// iTunesSearchTermFor builds the search term from an artist and title. Either
// part may be empty; the term then degrades to the other part only.
func iTunesSearchTermFor(artist, title string) string {
	artist, title = strings.TrimSpace(artist), strings.TrimSpace(title)
	switch {
	case artist != "" && title != "":
		return artist + " " + title
	case artist != "":
		return artist
	default:
		return title
	}
}
