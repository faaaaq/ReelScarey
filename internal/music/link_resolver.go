package music

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type LinkTrack struct {
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Album       string `json:"album"`
	AlbumArtist string `json:"album_artist"`
	Year        *int   `json:"year"`
	TrackNumber int    `json:"track_number"`
	DiscNumber  int    `json:"disc_number"`
	DurationMS  int    `json:"duration_ms"`
	CoverURL    string `json:"cover_url"`
	SourceURL   string `json:"source_url"`
	SearchQuery string `json:"search_query"`
}

type LinkPreview struct {
	Source   string      `json:"source"`
	Kind     string      `json:"kind"`
	Title    string      `json:"title"`
	Subtitle string      `json:"subtitle"`
	CoverURL string      `json:"cover_url"`
	Tracks   []LinkTrack `json:"tracks"`
}

var spotifyEmbedData = regexp.MustCompile(`(?s)<script id="__NEXT_DATA__" type="application/json">(.*?)</script>`)
var spotifyID = regexp.MustCompile(`^[A-Za-z0-9]{22}$`)
var deezerID = regexp.MustCompile(`^[0-9]+$`)
var yearPattern = regexp.MustCompile(`(19|20)[0-9]{2}`)

func (s *Service) ResolveLink(ctx context.Context, raw string) (LinkPreview, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2048 {
		return LinkPreview{}, &Error{Code: "music_link_invalid", Message: "Paste a valid Spotify, Deezer or YouTube link."}
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" {
		if strings.Contains(raw, "://") {
			return LinkPreview{}, &Error{Code: "music_link_invalid", Message: "Only public HTTPS links are accepted."}
		}
		return s.resolveSearch(ctx, raw)
	}
	if parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.Opaque != "" {
		return LinkPreview{}, &Error{Code: "music_link_invalid", Message: "Only public HTTPS links are accepted."}
	}
	host := strings.ToLower(parsed.Hostname())
	switch host {
	case "open.spotify.com", "play.spotify.com":
		return s.resolveSpotify(ctx, parsed)
	case "deezer.com", "www.deezer.com":
		return s.resolveDeezer(ctx, parsed)
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com", "youtu.be":
		return s.resolveYouTube(ctx, raw)
	default:
		return LinkPreview{}, &Error{Code: "music_link_unsupported", Message: "This link provider is not supported. Use Spotify, Deezer, YouTube or YouTube Music."}
	}
}

func (s *Service) resolveSearch(ctx context.Context, query string) (LinkPreview, error) {
	if s.audio == nil {
		return LinkPreview{}, &Error{Code: "music_acquisition_unavailable", Message: "Search downloading is unavailable because yt-dlp is not configured."}
	}
	candidates, err := s.audio.SearchAudio(ctx, query, 10)
	if err != nil {
		return LinkPreview{}, &Error{Code: "music_link_unavailable", Message: "YouTube search did not return results."}
	}
	out := LinkPreview{Source: "YouTube search", Kind: "search", Title: query, Tracks: []LinkTrack{}}
	for index, item := range candidates {
		artist := strings.TrimSpace(item.Channel)
		if artist == "" {
			artist = "YouTube"
		}
		out.Tracks = append(out.Tracks, LinkTrack{
			Title: item.Title, Artist: artist, Album: item.Title, AlbumArtist: artist,
			TrackNumber: index + 1, DiscNumber: 1, DurationMS: int(item.DurationSeconds * 1000),
			SourceURL: item.ID, SearchQuery: strings.TrimSpace(item.Title),
		})
	}
	if len(out.Tracks) == 0 {
		return LinkPreview{}, &Error{Code: "music_link_empty", Message: "The search returned no results."}
	}
	return out, nil
}

type spotifyEntity struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	Name        string `json:"name"`
	Subtitle    string `json:"subtitle"`
	Duration    int    `json:"duration"`
	ReleaseDate struct {
		ISOString string `json:"isoString"`
	} `json:"releaseDate"`
	Artists []struct {
		Name string `json:"name"`
	} `json:"artists"`
	TrackList []struct {
		Title    string `json:"title"`
		Subtitle string `json:"subtitle"`
		Duration int    `json:"duration"`
		URI      string `json:"uri"`
	} `json:"trackList"`
}

type spotifyNext struct {
	Props struct {
		PageProps struct {
			State struct {
				Data struct {
					Entity spotifyEntity `json:"entity"`
				} `json:"data"`
			} `json:"state"`
		} `json:"pageProps"`
	} `json:"props"`
}

func (s *Service) resolveSpotify(ctx context.Context, parsed *url.URL) (LinkPreview, error) {
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(parts) != 2 || !spotifyID.MatchString(parts[1]) {
		return LinkPreview{}, &Error{Code: "music_link_invalid", Message: "Choose a Spotify track, album, playlist or artist link."}
	}
	kind := strings.ToLower(parts[0])
	if kind != "track" && kind != "album" && kind != "playlist" && kind != "artist" {
		return LinkPreview{}, &Error{Code: "music_link_unsupported", Message: "This Spotify link type is not supported."}
	}
	body, err := linkGetBytes(ctx, "https://open.spotify.com/embed/"+kind+"/"+parts[1], 2<<20)
	if err != nil {
		return LinkPreview{}, err
	}
	match := spotifyEmbedData.FindSubmatch(body)
	if len(match) != 2 {
		return LinkPreview{}, &Error{Code: "music_link_unavailable", Message: "Spotify did not return readable metadata for this link."}
	}
	var next spotifyNext
	if err := json.Unmarshal(match[1], &next); err != nil {
		return LinkPreview{}, &Error{Code: "music_link_unavailable", Message: "Spotify returned invalid metadata for this link."}
	}
	entity := next.Props.PageProps.State.Data.Entity
	title := strings.TrimSpace(firstNonEmpty(entity.Title, entity.Name))
	if title == "" {
		return LinkPreview{}, &Error{Code: "music_link_empty", Message: "Spotify returned no title for this link."}
	}
	out := LinkPreview{Source: "Spotify", Kind: kind, Title: title}
	out.Subtitle = strings.TrimSpace(entity.Subtitle)
	year := yearFromText(entity.ReleaseDate.ISOString)
	if kind == "track" {
		artist := entity.Subtitle
		if artist == "" && len(entity.Artists) > 0 {
			artist = entity.Artists[0].Name
		}
		out.Subtitle = artist
		out.Tracks = []LinkTrack{{
			Title: title, Artist: artist, Album: title, AlbumArtist: artist, Year: year,
			TrackNumber: 1, DiscNumber: 1, DurationMS: entity.Duration, SearchQuery: strings.TrimSpace(artist + " " + title),
		}}
		return out, nil
	}
	for index, item := range entity.TrackList {
		if len(out.Tracks) >= 100 {
			break
		}
		trackTitle := strings.TrimSpace(item.Title)
		if trackTitle == "" {
			continue
		}
		artist := strings.TrimSpace(item.Subtitle)
		album := ""
		albumArtist := artist
		if kind == "album" {
			album = title
			if out.Subtitle != "" {
				albumArtist = out.Subtitle
			}
		} else {
			album = trackTitle
		}
		out.Tracks = append(out.Tracks, LinkTrack{
			Title: trackTitle, Artist: artist, Album: album, AlbumArtist: albumArtist, Year: year,
			TrackNumber: index + 1, DiscNumber: 1, DurationMS: item.Duration,
			SearchQuery: strings.TrimSpace(artist + " " + trackTitle),
		})
	}
	if len(out.Tracks) == 0 {
		return LinkPreview{}, &Error{Code: "music_link_empty", Message: "Spotify returned no tracks for this link."}
	}
	return out, nil
}

type deezerTrack struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Duration int    `json:"duration"`
	Artist   struct {
		Name string `json:"name"`
	} `json:"artist"`
	Album struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
		Cover string `json:"cover_medium"`
	} `json:"album"`
}

type deezerResponse struct {
	Error    any    `json:"error"`
	Title    string `json:"title"`
	Name     string `json:"name"`
	Duration int    `json:"duration"`
	Artist   struct {
		Name string `json:"name"`
	} `json:"artist"`
	Cover   string `json:"cover_medium"`
	Picture string `json:"picture_medium"`
	Tracks  struct {
		Data []deezerTrack `json:"data"`
	} `json:"tracks"`
	Data []deezerTrack `json:"data"`
}

func (s *Service) resolveDeezer(ctx context.Context, parsed *url.URL) (LinkPreview, error) {
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(parts) != 2 || !deezerID.MatchString(parts[1]) {
		return LinkPreview{}, &Error{Code: "music_link_invalid", Message: "Choose a Deezer track, album, playlist or artist link."}
	}
	kind := strings.ToLower(parts[0])
	if kind != "track" && kind != "album" && kind != "playlist" && kind != "artist" {
		return LinkPreview{}, &Error{Code: "music_link_unsupported", Message: "This Deezer link type is not supported."}
	}
	out := LinkPreview{Source: "Deezer", Kind: kind}
	var tracks []deezerTrack
	if kind == "track" {
		var body deezerTrack
		if err := linkGetJSON(ctx, "https://api.deezer.com/track/"+parts[1], &body); err != nil {
			return LinkPreview{}, err
		}
		out.Title = strings.TrimSpace(body.Title)
		tracks = []deezerTrack{body}
	} else {
		endpoint := "https://api.deezer.com/" + kind + "/" + parts[1]
		if kind == "artist" {
			endpoint += "/top?limit=50"
		}
		var body deezerResponse
		if err := linkGetJSON(ctx, endpoint, &body); err != nil {
			return LinkPreview{}, err
		}
		if body.Error != nil {
			return LinkPreview{}, &Error{Code: "music_link_unavailable", Message: "Deezer did not return metadata for this link."}
		}
		out.Title = strings.TrimSpace(firstNonEmpty(body.Title, body.Name))
		out.CoverURL = strings.TrimSpace(firstNonEmpty(body.Cover, body.Picture))
		if kind == "artist" {
			tracks = body.Data
		} else {
			tracks = body.Tracks.Data
		}
	}
	if out.Title == "" {
		out.Title = "Deezer link"
	}
	index := 0
	for _, item := range tracks {
		if len(out.Tracks) >= 100 {
			break
		}
		title := strings.TrimSpace(item.Title)
		if title == "" {
			continue
		}
		artist := strings.TrimSpace(item.Artist.Name)
		album := strings.TrimSpace(item.Album.Title)
		if kind == "album" {
			album = out.Title
			if out.Subtitle != "" {
				artist = out.Subtitle
			}
		}
		if album == "" {
			album = title
		}
		index++
		out.Tracks = append(out.Tracks, LinkTrack{
			Title: title, Artist: artist, Album: album, AlbumArtist: artist,
			TrackNumber: index, DiscNumber: 1, DurationMS: item.Duration * 1000,
			CoverURL: strings.TrimSpace(item.Album.Cover), SearchQuery: strings.TrimSpace(artist + " " + title),
		})
	}
	if len(out.Tracks) == 0 {
		return LinkPreview{}, &Error{Code: "music_link_empty", Message: "Deezer returned no tracks for this link."}
	}
	if out.Subtitle == "" {
		out.Subtitle = out.Tracks[0].Artist
	}
	return out, nil
}

func (s *Service) resolveYouTube(ctx context.Context, raw string) (LinkPreview, error) {
	if s.audio == nil {
		return LinkPreview{}, &Error{Code: "music_acquisition_unavailable", Message: "YouTube link inspection is unavailable because yt-dlp is not configured."}
	}
	inspection, err := s.audio.InspectAudio(ctx, raw)
	if err != nil {
		return LinkPreview{}, &Error{Code: "music_link_unavailable", Message: "YouTube did not return readable metadata for this link."}
	}
	out := LinkPreview{Source: "YouTube", Kind: inspection.Kind, Title: strings.TrimSpace(inspection.Title), Subtitle: strings.TrimSpace(inspection.Channel)}
	if out.Title == "" {
		out.Title = "YouTube link"
	}
	index := 0
	for _, entry := range inspection.Entries {
		if len(out.Tracks) >= 100 {
			break
		}
		title := strings.TrimSpace(entry.Title)
		if title == "" {
			continue
		}
		artist := strings.TrimSpace(entry.Channel)
		if artist == "" {
			artist = out.Subtitle
		}
		index++
		out.Tracks = append(out.Tracks, LinkTrack{
			Title: title, Artist: artist, Album: title, AlbumArtist: artist,
			TrackNumber: index, DiscNumber: 1, DurationMS: int(entry.DurationSeconds * 1000),
			SourceURL: entry.ID, SearchQuery: strings.TrimSpace(artist + " " + title),
		})
	}
	if len(out.Tracks) == 0 {
		return LinkPreview{}, &Error{Code: "music_link_empty", Message: "YouTube returned no playable entries for this link."}
	}
	return out, nil
}

func linkGetBytes(ctx context.Context, endpoint string, maximum int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, &Error{Code: "music_link_invalid", Message: "The link could not be requested."}
	}
	request.Header.Set("Accept", "text/html,application/json")
	request.Header.Set("User-Agent", "ReelScarey/1.0 (+https://github.com/faaaaq/ReelScarey)")
	response, err := topTracksHTTP.Do(request)
	if err != nil {
		return nil, &Error{Code: "music_link_unavailable", Message: "The link provider is unavailable."}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, &Error{Code: "music_link_unavailable", Message: "The link provider rejected the request."}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maximum))
	if err != nil {
		return nil, &Error{Code: "music_link_unavailable", Message: "The link provider returned an unreadable response."}
	}
	return body, nil
}

func linkGetJSON(ctx context.Context, endpoint string, out any) error {
	body, err := linkGetBytes(ctx, endpoint, 2<<20)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return &Error{Code: "music_link_unavailable", Message: "The link provider returned invalid metadata."}
	}
	return nil
}

func yearFromText(value string) *int {
	match := yearPattern.FindString(value)
	if match == "" {
		return nil
	}
	year, err := strconv.Atoi(match)
	if err != nil || year < 1900 || year > 2100 {
		return nil
	}
	return &year
}
