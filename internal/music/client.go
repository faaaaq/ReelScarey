package music

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const userAgent = "ReelScarey/0.39 ( https://github.com/faaaaq/ReelScarey )"

const (
	// Positive provider metadata changes slowly; searches move faster and are
	// cached for one week. A negative observation is only evidence that the
	// provider had no match at that moment and is rechecked after one day.
	musicbrainzMetadataTTL  = 30 * 24 * time.Hour
	musicbrainzSearchTTL    = 7 * 24 * time.Hour
	musicbrainzNegativeTTL  = 24 * time.Hour
	musicbrainzCleanupEvery = 50
)

var partialDate = regexp.MustCompile(`^[0-9]{4}(-[0-9]{2}(-[0-9]{2})?)?$`)

// MusicBrainzCache is the optional persistent response cache. Implementations
// must treat expired rows as misses. A cache failure is reported as a warning
// and the provider request continues.
type MusicBrainzCache interface {
	Get(ctx context.Context, key string) (payload []byte, negative bool, found bool, err error)
	Put(ctx context.Context, key string, payload []byte, ttl time.Duration, negative bool) error
	Cleanup(ctx context.Context) error
	Count(ctx context.Context) (int, error)
}

// MusicBrainzCooldownStore persists a server-requested retry window so a
// restart cannot immediately retry a rate-limited provider.
type MusicBrainzCooldownStore interface {
	Cooldown(ctx context.Context) (until time.Time, message string, err error)
	SetCooldown(ctx context.Context, until time.Time, message string) error
}

// Client talks to the fixed or owner-configured MusicBrainz web service. It
// never follows redirects and applies the public one-request-per-second policy
// or the shorter local-mirror spacing.
type Client struct {
	base     string
	baseMu   sync.RWMutex
	http     *http.Client
	interval time.Duration
	mu       sync.Mutex
	next     time.Time
	cache    MusicBrainzCache
	cooldown MusicBrainzCooldownStore
	calls    atomic.Uint64
}

func New() *Client {
	return newClient(DefaultMusicBrainzBaseURL, &http.Client{Timeout: 15 * time.Second}, PublicMusicBrainzInterval)
}

func newClient(base string, client *http.Client, interval time.Duration) *Client {
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("provider redirects are disabled") }
	return &Client{base: strings.TrimRight(base, "/"), http: &copy, interval: interval}
}

// SetCache injects the optional response cache. A nil cache disables caching.
func (c *Client) SetCache(cache MusicBrainzCache) { c.cache = cache }

// SetCooldownStore injects the optional persistent cooldown store.
func (c *Client) SetCooldownStore(store MusicBrainzCooldownStore) { c.cooldown = store }

func (c *Client) BaseURL() string {
	c.baseMu.RLock()
	defer c.baseMu.RUnlock()
	return c.base
}

// SetBase changes the provider URL and pacing for subsequent requests. A
// request already in flight keeps the base it started with, so changing a
// mirror never reroutes active work. The public server keeps one request per
// second; local/private mirrors use a shorter interval.
func (c *Client) SetBase(base string) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	interval := musicbrainzPacing(base)
	c.baseMu.Lock()
	c.base = base
	c.baseMu.Unlock()
	c.mu.Lock()
	c.interval = interval
	c.mu.Unlock()
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

func (c *Client) cooldownError(ctx context.Context) error {
	if c.cooldown == nil {
		return nil
	}
	until, message, err := c.cooldown.Cooldown(ctx)
	if err != nil {
		slog.Default().Warn("musicbrainz cooldown read failed", "error", err)
		return nil
	}
	remaining := time.Until(until)
	if remaining <= 0 {
		return nil
	}
	if strings.TrimSpace(message) == "" {
		message = cooldownMessage(until)
	}
	return &Error{Code: "music_rate_limited", Message: message, RetryAfter: retrySeconds(remaining)}
}

func (c *Client) cached(ctx context.Context, key string) ([]byte, bool, bool) {
	if c.cache == nil {
		return nil, false, false
	}
	payload, negative, found, err := c.cache.Get(ctx, key)
	if err != nil {
		slog.Default().Warn("musicbrainz cache read failed; contacting provider", "error", err)
		return nil, false, false
	}
	return payload, negative, found
}

func (c *Client) storeCache(ctx context.Context, key string, payload []byte, ttl time.Duration, negative bool) {
	if c.cache == nil {
		return
	}
	if err := c.cache.Put(ctx, key, payload, ttl, negative); err != nil {
		slog.Default().Warn("musicbrainz cache write failed; matching continues", "error", err)
	}
}

func (c *Client) maybeCleanup(ctx context.Context) {
	if c.cache == nil {
		return
	}
	if c.calls.Add(1)%musicbrainzCleanupEvery != 0 {
		return
	}
	if err := c.cache.Cleanup(ctx); err != nil {
		slog.Default().Warn("musicbrainz cache cleanup failed", "error", err)
	}
}

func (c *Client) request(ctx context.Context, endpoint string, query url.Values, out any, limit int64) error {
	if query == nil {
		query = url.Values{}
	}
	query.Set("fmt", "json")
	base := c.BaseURL()
	key := base + endpoint + "?" + query.Encode()
	if payload, negative, found := c.cached(ctx, key); found {
		if negative && len(payload) == 0 {
			return notFoundError()
		}
		if err := json.Unmarshal(payload, out); err == nil {
			return nil
		}
		// Corrupt or incompatible cache data is ignored rather than failing
		// matching. The provider response repopulates the row.
	}
	if err := c.cooldownError(ctx); err != nil {
		return err
	}
	if err := c.wait(ctx); err != nil {
		return err
	}
	c.maybeCleanup(ctx)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+endpoint+"?"+query.Encode(), nil)
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
		c.storeCache(ctx, key, nil, musicbrainzNegativeTTL, true)
		return notFoundError()
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		delay := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
		until := time.Now().Add(delay)
		message := cooldownMessage(until)
		if c.cooldown != nil {
			if err := c.cooldown.SetCooldown(ctx, until, message); err != nil {
				slog.Default().Warn("musicbrainz cooldown write failed", "error", err)
			}
		}
		return &Error{Code: "music_rate_limited", Message: message, RetryAfter: retrySeconds(delay)}
	default:
		return &Error{Code: "music_unavailable", Message: "MusicBrainz is temporarily unavailable. Try again later."}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(body)) > limit || json.Unmarshal(body, out) != nil {
		return &Error{Code: "music_invalid_response", Message: "MusicBrainz returned an unreadable response. Try again later."}
	}
	if !cacheableMusicBrainzBody(body) {
		return nil
	}
	negative := emptyMusicBrainzResponse(body)
	ttl := musicbrainzMetadataTTL
	if negative {
		ttl = musicbrainzNegativeTTL
	} else if musicbrainzSearchEndpoint(endpoint, query) {
		ttl = musicbrainzSearchTTL
	}
	c.storeCache(ctx, key, body, ttl, negative)
	return nil
}

func notFoundError() *Error {
	return &Error{Code: "music_not_found", Message: "MusicBrainz no longer has this item."}
}

func musicbrainzSearchEndpoint(endpoint string, query url.Values) bool {
	if query.Get("query") == "" {
		return false
	}
	return endpoint == "/artist" || endpoint == "/release-group" || endpoint == "/release"
}

// cacheableMusicBrainzBody refuses empty objects and JSON literals. A caller
// still validates provider identity; this guard only prevents obviously empty
// payloads from being stored as positive evidence.
func cacheableMusicBrainzBody(body []byte) bool {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil {
		return false
	}
	return len(object) > 0
}

// emptyMusicBrainzResponse recognizes a successful response that contains no
// metadata. It is cached as a negative observation for one day, not as a
// permanent absence of the artist, album or release.
func emptyMusicBrainzResponse(body []byte) bool {
	var probe struct {
		Count             *int              `json:"count"`
		ReleaseGroupCount *int              `json:"release-group-count"`
		ReleaseCount      *int              `json:"release-count"`
		Artists           []json.RawMessage `json:"artists"`
		ReleaseGroups     []json.RawMessage `json:"release-groups"`
		Releases          []json.RawMessage `json:"releases"`
		Media             []json.RawMessage `json:"media"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return false
	}
	switch {
	case probe.Count != nil && *probe.Count == 0 && len(probe.Artists) == 0 && len(probe.ReleaseGroups) == 0 && len(probe.Releases) == 0:
		return true
	case probe.ReleaseGroupCount != nil && *probe.ReleaseGroupCount == 0 && len(probe.ReleaseGroups) == 0:
		return true
	case probe.ReleaseCount != nil && *probe.ReleaseCount == 0 && len(probe.Releases) == 0:
		return true
	case probe.Media != nil && len(probe.Media) == 0:
		return true
	default:
		return false
	}
}

// parseRetryAfter accepts delay-seconds or an HTTP date. Invalid or missing
// values use a short conservative delay. The delay is bounded so a hostile or
// broken header cannot disable matching indefinitely.
func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	delay := 5 * time.Second
	if value != "" {
		if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
			delay = time.Duration(seconds) * time.Second
		} else if when, err := http.ParseTime(value); err == nil {
			delay = when.Sub(now)
		}
	}
	if delay < time.Second {
		delay = time.Second
	}
	if delay > 24*time.Hour {
		delay = 24 * time.Hour
	}
	return delay
}

func retrySeconds(delay time.Duration) int {
	seconds := int(math.Ceil(delay.Seconds()))
	if seconds < 1 {
		return 1
	}
	return seconds
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
	return c.searchArtists(ctx, query, page, true)
}

// SearchArtistName searches the exact artist-name field across one bounded
// page. Discovery uses it so a common name is not buried by free-text ranking;
// the caller still has to compare the returned names exactly.
func (c *Client) SearchArtistName(ctx context.Context, name string, page int) (SearchResults, error) {
	return c.searchArtists(ctx, "artist:"+quoteSearchValue(name), page, false)
}

func (c *Client) searchArtists(ctx context.Context, query string, page int, dismax bool) (SearchResults, error) {
	const limit = 25
	values := url.Values{
		"query":  {query},
		"limit":  {strconv.Itoa(limit)},
		"offset": {strconv.Itoa((page - 1) * limit)},
	}
	if dismax {
		values.Set("dismax", "true")
	}
	var body mbArtistSearch
	if err := c.request(ctx, "/artist", values, &body, 2<<20); err != nil {
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
