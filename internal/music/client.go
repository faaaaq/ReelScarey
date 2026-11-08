package music

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
)

const userAgent = "ReelScarey/0.39 ( https://github.com/faaaaq/ReelScarey )"

var partialDate = regexp.MustCompile(`^[0-9]{4}(-[0-9]{2}(-[0-9]{2})?)?$`)

// Client talks to the fixed MusicBrainz web service. It never follows
// redirects and spaces requests by one second, matching the provider policy.
type Client struct {
	base     string
	http     *http.Client
	interval time.Duration
	mu       sync.Mutex
	next     time.Time
}

func New() *Client {
	return newClient("https://musicbrainz.org/ws/2", &http.Client{Timeout: 15 * time.Second}, time.Second)
}

func newClient(base string, client *http.Client, interval time.Duration) *Client {
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("provider redirects are disabled") }
	return &Client{base: strings.TrimRight(base, "/"), http: &copy, interval: interval}
}

func (c *Client) wait(ctx context.Context) error {
	c.mu.Lock()
	now := time.Now()
	delay := time.Until(c.next)
	if delay < 0 {
		delay = 0
	}
	c.next = now.Add(delay + c.interval)
	c.mu.Unlock()
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return &Error{Code: "music_unavailable", Message: "The MusicBrainz request was cancelled."}
	case <-timer.C:
		return nil
	}
}

func (c *Client) request(ctx context.Context, endpoint string, query url.Values, out any, limit int64) error {
	if err := c.wait(ctx); err != nil {
		return err
	}
	if query == nil {
		query = url.Values{}
	}
	query.Set("fmt", "json")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return &Error{Code: "music_unavailable", Message: "MusicBrainz could not be reached."}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return &Error{Code: "music_unavailable", Message: "MusicBrainz could not be reached. Check your network and try again."}
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusBadRequest:
		return &Error{Code: "music_invalid_request", Message: "MusicBrainz rejected the request. Check the artist or album and try again."}
	case http.StatusNotFound:
		return &Error{Code: "music_not_found", Message: "MusicBrainz no longer has this item."}
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		seconds := 5
		if value := resp.Header.Get("Retry-After"); value != "" {
			if n, e := strconv.Atoi(value); e == nil {
				seconds = n
			}
		}
		seconds = min(300, max(1, seconds))
		return &Error{Code: "music_rate_limited", Message: "MusicBrainz is limiting requests. Wait before trying again.", RetryAfter: seconds}
	default:
		return &Error{Code: "music_unavailable", Message: "MusicBrainz is temporarily unavailable. Try again later."}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(body)) > limit || json.Unmarshal(body, out) != nil {
		return &Error{Code: "music_invalid_response", Message: "MusicBrainz returned an unreadable response. Try again later."}
	}
	return nil
}

type mbArtist struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	SortName       string `json:"sort-name"`
	Disambiguation string `json:"disambiguation"`
	Country        string `json:"country"`
	Type           string `json:"type"`
	LifeSpan       struct {
		Begin string `json:"begin"`
		End   string `json:"end"`
		Ended *bool  `json:"ended"`
	} `json:"life-span"`
	Tags []struct {
		Name string `json:"name"`
	} `json:"tags"`
	Aliases []struct {
		Name string `json:"name"`
	} `json:"aliases"`
}

type mbArtistSearch struct {
	Count   int        `json:"count"`
	Offset  int        `json:"offset"`
	Artists []mbArtist `json:"artists"`
}

type mbReleaseGroup struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	PrimaryType      string   `json:"primary-type"`
	SecondaryTypes   []string `json:"secondary-types"`
	FirstReleaseDate string   `json:"first-release-date"`
}

type mbReleaseGroupBrowse struct {
	Count         int              `json:"release-group-count"`
	Offset        int              `json:"release-group-offset"`
	ReleaseGroups []mbReleaseGroup `json:"release-groups"`
}

type mbReleaseGroupSearch struct {
	Count         int              `json:"count"`
	Offset        int              `json:"offset"`
	ReleaseGroups []mbReleaseGroup `json:"release-groups"`
}

type mbReleaseGroupLookup struct {
	mbReleaseGroup
	ArtistCredit []struct {
		Artist struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"artist"`
	} `json:"artist-credit"`
}

type mbTrack struct {
	ID        string `json:"id"`
	Position  int    `json:"position"`
	Number    string `json:"number"`
	Title     string `json:"title"`
	Length    *int   `json:"length"`
	Recording *struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Length *int   `json:"length"`
	} `json:"recording"`
}

type mbMedium struct {
	Position   int       `json:"position"`
	TrackCount int       `json:"track-count"`
	Tracks     []mbTrack `json:"tracks"`
}

type mbRelease struct {
	ID     string     `json:"id"`
	Title  string     `json:"title"`
	Status string     `json:"status"`
	Date   string     `json:"date"`
	Media  []mbMedium `json:"media"`
}

type mbReleaseBrowse struct {
	Count    int         `json:"release-count"`
	Offset   int         `json:"release-offset"`
	Releases []mbRelease `json:"releases"`
}

func (c *Client) SearchArtists(ctx context.Context, query string, page int) (SearchResults, error) {
	const limit = 25
	var body mbArtistSearch
	if err := c.request(ctx, "/artist", url.Values{
		"query":  {query},
		"limit":  {strconv.Itoa(limit)},
		"offset": {strconv.Itoa((page - 1) * limit)},
		"dismax": {"true"},
	}, &body, 2<<20); err != nil {
		return SearchResults{}, err
	}
	if body.Count < 0 || body.Offset < 0 || len(body.Artists) > limit {
		return SearchResults{}, invalidResponse("search results")
	}
	items := make([]Artist, 0, len(body.Artists))
	for _, raw := range body.Artists {
		artist, err := normalizeArtist(raw)
		if err != nil {
			return SearchResults{}, err
		}
		items = append(items, artist)
	}
	pages := (body.Count + limit - 1) / limit
	if pages < 1 {
		pages = 1
	}
	if pages > 500 {
		pages = 500
	}
	return SearchResults{Page: page, TotalPages: pages, TotalResults: body.Count, Items: items}, nil
}

func (c *Client) Artist(ctx context.Context, mbid string) (Artist, error) {
	var body mbArtist
	if err := c.request(ctx, "/artist/"+url.PathEscape(mbid), nil, &body, 2<<20); err != nil {
		return Artist{}, err
	}
	if !strings.EqualFold(body.ID, mbid) {
		return Artist{}, invalidResponse("artist details")
	}
	return normalizeArtist(body)
}

// ArtistAlbums browses the full discography. MusicBrainz caps lookup includes
// at 25 linked entities, so browse is the only complete path.
func (c *Client) ArtistAlbums(ctx context.Context, mbid string) ([]Album, error) {
	items := []Album{}
	for offset := 0; offset < 1000; {
		var body mbReleaseGroupBrowse
		if err := c.request(ctx, "/release-group", url.Values{
			"artist": {mbid},
			"limit":  {"100"},
			"offset": {strconv.Itoa(offset)},
		}, &body, 4<<20); err != nil {
			return nil, err
		}
		if body.Count < 0 || body.Offset < 0 || len(body.ReleaseGroups) > 100 {
			return nil, invalidResponse("album list")
		}
		for _, raw := range body.ReleaseGroups {
			album, err := normalizeAlbum(raw)
			if err != nil {
				return nil, err
			}
			items = append(items, album)
		}
		offset += len(body.ReleaseGroups)
		if len(body.ReleaseGroups) == 0 || offset >= body.Count || len(items) >= 1000 {
			break
		}
	}
	return items, nil
}

// SearchReleaseGroups searches by exact artist ID and a quoted album title.
// The bool reports whether the single bounded page covered every provider
// result, so discovery can distinguish "no exact album" from "too many
// results to be certain".
func (c *Client) SearchReleaseGroups(ctx context.Context, artistID, title string) ([]Album, bool, error) {
	const limit = 100
	var body mbReleaseGroupSearch
	if err := c.request(ctx, "/release-group", url.Values{
		"query":  {"arid:" + artistID + " AND releasegroup:" + quoteSearchValue(title)},
		"limit":  {strconv.Itoa(limit)},
		"offset": {"0"},
	}, &body, 4<<20); err != nil {
		return nil, false, err
	}
	if body.Count < 0 || body.Offset < 0 || len(body.ReleaseGroups) > limit {
		return nil, false, invalidResponse("album search")
	}
	items := make([]Album, 0, len(body.ReleaseGroups))
	for _, raw := range body.ReleaseGroups {
		album, err := normalizeAlbum(raw)
		if err != nil {
			return nil, false, err
		}
		items = append(items, album)
	}
	return items, body.Count <= len(body.ReleaseGroups), nil
}

// ReleaseGroup looks up one release group and its primary credited artist ID.
// Discovery uses the provider identity already present in the file tag; the
// returned album is only saved after this exact lookup succeeds.
func (c *Client) ReleaseGroup(ctx context.Context, mbid string) (Album, string, error) {
	var body mbReleaseGroupLookup
	if err := c.request(ctx, "/release-group/"+url.PathEscape(mbid), url.Values{
		"inc": {"artist-credits"},
	}, &body, 2<<20); err != nil {
		return Album{}, "", err
	}
	if !strings.EqualFold(body.ID, mbid) {
		return Album{}, "", invalidResponse("release group details")
	}
	album, err := normalizeAlbum(body.mbReleaseGroup)
	if err != nil {
		return Album{}, "", err
	}
	for _, credit := range body.ArtistCredit {
		if ValidMBID(credit.Artist.ID) {
			return album, credit.Artist.ID, nil
		}
	}
	return Album{}, "", invalidResponse("release group artist")
}

func quoteSearchValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}

func (c *Client) ReleaseGroupReleases(ctx context.Context, mbid string) ([]mbRelease, error) {
	var body mbReleaseBrowse
	if err := c.request(ctx, "/release", url.Values{
		"release-group": {mbid},
		"limit":         {"100"},
		"inc":           {"media+artist-credits"},
	}, &body, 4<<20); err != nil {
		return nil, err
	}
	if body.Count < 0 || body.Offset < 0 || len(body.Releases) > 100 {
		return nil, invalidResponse("release list")
	}
	return body.Releases, nil
}

func (c *Client) ReleaseTracks(ctx context.Context, mbid string) ([]Track, error) {
	var body mbRelease
	if err := c.request(ctx, "/release/"+url.PathEscape(mbid), url.Values{
		"inc": {"recordings+artist-credits"},
	}, &body, 8<<20); err != nil {
		return nil, err
	}
	if !strings.EqualFold(body.ID, mbid) {
		return nil, invalidResponse("release tracks")
	}
	tracks := []Track{}
	for _, media := range body.Media {
		disc := media.Position
		if disc < 1 {
			disc = 1
		}
		for _, raw := range media.Tracks {
			track, ok := normalizeTrack(raw, disc)
			if ok {
				tracks = append(tracks, track)
			}
		}
	}
	if len(tracks) == 0 {
		return nil, &Error{Code: "music_not_found", Message: "MusicBrainz has no tracks for this release."}
	}
	return tracks, nil
}

func invalidResponse(subject string) *Error {
	return &Error{Code: "music_invalid_response", Message: "MusicBrainz returned invalid " + subject + ". Try again later."}
}

func normalizeArtist(raw mbArtist) (Artist, error) {
	name, ok := cleanText(raw.Name, 500)
	if !ok || !ValidMBID(raw.ID) {
		return Artist{}, invalidResponse("artist details")
	}
	out := Artist{
		MusicBrainzID:  raw.ID,
		Name:           name,
		SortName:       strings.TrimSpace(raw.SortName),
		Disambiguation: strings.TrimSpace(raw.Disambiguation),
		Country:        strings.TrimSpace(raw.Country),
		Type:           strings.TrimSpace(raw.Type),
		Monitored:      true,
		Status:         "missing",
	}
	parts := []string{}
	if out.Type != "" {
		parts = append(parts, out.Type)
	}
	if out.Country != "" {
		parts = append(parts, out.Country)
	}
	span := strings.TrimSpace(raw.LifeSpan.Begin)
	if raw.LifeSpan.End != "" {
		span += "–" + strings.TrimSpace(raw.LifeSpan.End)
	} else if span != "" && (raw.LifeSpan.Ended == nil || !*raw.LifeSpan.Ended) {
		span += "–"
	}
	if span != "" {
		parts = append(parts, span)
	}
	if len(parts) > 0 {
		out.Overview = strings.Join(parts, " · ")
	}
	return out, nil
}

func normalizeAlbum(raw mbReleaseGroup) (Album, error) {
	title, ok := cleanText(raw.Title, 500)
	if !ok || !ValidMBID(raw.ID) {
		return Album{}, invalidResponse("album details")
	}
	out := Album{
		MusicBrainzID:  raw.ID,
		Title:          title,
		PrimaryType:    strings.TrimSpace(raw.PrimaryType),
		SecondaryTypes: append([]string{}, raw.SecondaryTypes...),
		Monitored:      true,
		Status:         "missing",
		CoverURL:       coverURL(raw.ID),
	}
	if date := strings.TrimSpace(raw.FirstReleaseDate); partialDate.MatchString(date) {
		out.ReleaseDate = date
		year, _ := strconv.Atoi(date[:4])
		if year >= 1800 {
			out.Year = &year
		}
	}
	return out, nil
}

func normalizeTrack(raw mbTrack, disc int) (Track, bool) {
	title, ok := cleanText(raw.Title, 500)
	if !ok {
		return Track{}, false
	}
	number := raw.Position
	if number < 1 {
		if parsed, err := strconv.Atoi(strings.TrimSpace(raw.Number)); err == nil {
			number = parsed
		}
	}
	if number < 1 {
		return Track{}, false
	}
	duration := 0
	if raw.Length != nil {
		duration = *raw.Length
	}
	if duration <= 0 && raw.Recording != nil && raw.Recording.Length != nil {
		duration = *raw.Recording.Length
	}
	if duration < 0 || duration > 24*60*60*1000 {
		duration = 0
	}
	recording := ""
	if raw.Recording != nil && ValidMBID(raw.Recording.ID) {
		recording = raw.Recording.ID
	}
	trackID := ""
	if ValidMBID(raw.ID) {
		trackID = raw.ID
	}
	return Track{
		MusicBrainzID: trackID,
		RecordingID:   recording,
		Disc:          disc,
		Number:        number,
		Title:         title,
		DurationMS:    duration,
	}, true
}
