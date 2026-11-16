package music

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// iTunester is the injectable iTunes track-search used for corroborating
// artist/title evidence and recovering plausible names when MusicBrainz is
// unavailable. It never provides MusicBrainz identifiers; production wiring
// attaches the shared paced itunesCatalog client.
type iTunester interface {
	SearchTracks(ctx context.Context, term string, limit int) ([]iTunesTrack, error)
}

// iTunesTrack is one song result from the iTunes Search API.
type iTunesTrack struct {
	ArtistName     string
	TrackName      string
	CollectionName string
	DurationMS     int
}

// SearchTracks searches songs by a free-text term through the shared catalog
// client, so the documented request pacing and rate-limit handling apply. An
// empty or oversized term returns no results rather than an error, because a
// missing term is not a provider failure.
func (c *itunesCatalog) SearchTracks(ctx context.Context, term string, limit int) ([]iTunesTrack, error) {
	term = strings.TrimSpace(term)
	if term == "" || len(term) > 200 {
		return nil, nil
	}
	if limit < 1 || limit > 25 {
		limit = 10
	}
	items, err := c.request(ctx, "/search", url.Values{
		"term": {term}, "media": {"music"}, "entity": {"song"}, "limit": {strconv.Itoa(limit)},
	})
	if err != nil {
		return nil, err
	}
	tracks := make([]iTunesTrack, 0, len(items))
	for _, raw := range items {
		if raw.Wrapper != "" && raw.Wrapper != "track" {
			continue
		}
		artist, okArtist := cleanText(raw.Artist, 300)
		title, okTitle := cleanText(raw.Title, 500)
		if !okArtist || !okTitle {
			continue
		}
		track := iTunesTrack{ArtistName: artist, TrackName: title}
		if collection, ok := cleanText(raw.Album, 500); ok {
			track.CollectionName = collection
		}
		if raw.Duration > 0 {
			track.DurationMS = raw.Duration
		}
		tracks = append(tracks, track)
	}
	return tracks, nil
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
