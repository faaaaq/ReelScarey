package music

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultTopTracksBaseURL = "https://api.deezer.com"

type TopTrack struct {
	ProviderID string `json:"provider_id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	CoverURL   string `json:"cover_url"`
	DurationMS int    `json:"duration_ms"`
	InLibrary  bool   `json:"in_library"`
	FileID     *int64 `json:"file_id"`
	AlbumID    *int64 `json:"album_id"`
	TrackID    *int64 `json:"track_id"`
}

type TopTrackPage struct {
	Source  string     `json:"source"`
	Items   []TopTrack `json:"items"`
	Message string     `json:"message"`
}

type deezerSearchArtist struct {
	Data []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"data"`
}

type deezerArtistTop struct {
	Data []struct {
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
	} `json:"data"`
}

var topTracksHTTP = &http.Client{Timeout: 15 * time.Second}

// ArtistTopTracks returns the provider's overall top tracks, not just tracks
// already registered in this library. Deezer's public API is the popularity
// source; each item is then matched against the local catalog for playback or
// acquisition.
func (s *Service) ArtistTopTracks(ctx context.Context, artistID int64, limit int) (TopTrackPage, error) {
	if artistID < 1 || limit < 1 || limit > 50 {
		return TopTrackPage{}, ErrInvalidInput
	}
	artist, err := s.store.Artist(ctx, artistID)
	if err != nil {
		return TopTrackPage{}, err
	}
	base := s.topBaseURL
	if base == "" {
		base = defaultTopTracksBaseURL
	}
	providerID, err := deezerArtistID(ctx, base, artist.Name)
	if err != nil {
		return TopTrackPage{}, err
	}
	if providerID == 0 {
		return TopTrackPage{Source: "Deezer", Items: []TopTrack{}, Message: "Deezer has no exact artist match for this name."}, nil
	}
	out := TopTrackPage{Source: "Deezer", Items: []TopTrack{}}
	var body deezerArtistTop
	if err := deezerGet(ctx, fmt.Sprintf("%s/artist/%d/top?limit=%d", base, providerID, limit), &body); err != nil {
		return TopTrackPage{}, err
	}
	for _, item := range body.Data {
		title := strings.TrimSpace(item.Title)
		album := strings.TrimSpace(item.Album.Title)
		if title == "" {
			continue
		}
		track := TopTrack{
			ProviderID: fmt.Sprintf("%d", item.ID),
			Title:      title,
			Artist:     strings.TrimSpace(item.Artist.Name),
			Album:      album,
			CoverURL:   strings.TrimSpace(item.Album.Cover),
			DurationMS: item.Duration * 1000,
		}
		var fileID, albumID, trackID sql.NullInt64
		trackID, albumID, fileID, err = s.libraryMatch(ctx, title, artist.Name, album)
		if err != nil {
			return TopTrackPage{}, err
		}
		if trackID.Valid {
			value := trackID.Int64
			track.TrackID = &value
		}
		if albumID.Valid {
			value := albumID.Int64
			track.AlbumID = &value
		}
		if fileID.Valid {
			value := fileID.Int64
			track.FileID = &value
			track.InLibrary = true
		}
		out.Items = append(out.Items, track)
	}
	if len(out.Items) == 0 {
		out.Message = "Deezer returned no top tracks for this artist."
	}
	return out, nil
}

func (s *Service) libraryMatch(ctx context.Context, title, artist, album string) (sql.NullInt64, sql.NullInt64, sql.NullInt64, error) {
	var fileID, albumID, trackID sql.NullInt64
	err := s.store.db.QueryRowContext(ctx, `SELECT t.id,a.id,(SELECT MIN(f.id) FROM music_files f WHERE f.track_id=t.id)
		FROM music_tracks t JOIN music_albums a ON a.id=t.album_id JOIN music_artists ar ON ar.id=a.artist_id
		WHERE lower(trim(t.title))=lower(trim(?)) AND lower(trim(ar.name))=lower(trim(?))
		ORDER BY CASE WHEN lower(trim(a.title))=lower(trim(?)) THEN 0 ELSE 1 END,t.id LIMIT 1`,
		title, artist, album).Scan(&trackID, &albumID, &fileID)
	if errors.Is(err, sql.ErrNoRows) {
		return trackID, albumID, fileID, nil
	}
	return trackID, albumID, fileID, err
}

// ChartTracks returns Deezer's global chart as overall popular tracks, with
// local library matches for playback and acquisition decisions.
func (s *Service) ChartTracks(ctx context.Context, limit int) (TopTrackPage, error) {
	if limit < 1 || limit > 50 {
		return TopTrackPage{}, ErrInvalidInput
	}
	base := s.topBaseURL
	if base == "" {
		base = defaultTopTracksBaseURL
	}
	var body struct {
		Data []deezerTrack `json:"data"`
	}
	if err := deezerGet(ctx, base+"/chart/0/tracks?limit="+strconv.Itoa(limit), &body); err != nil {
		return TopTrackPage{}, err
	}
	out := TopTrackPage{Source: "Deezer charts", Items: []TopTrack{}}
	for _, item := range body.Data {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			continue
		}
		track := TopTrack{
			ProviderID: fmt.Sprintf("%d", item.ID), Title: title,
			Artist: strings.TrimSpace(item.Artist.Name), Album: strings.TrimSpace(item.Album.Title),
			CoverURL: strings.TrimSpace(item.Album.Cover), DurationMS: item.Duration * 1000,
		}
		fileID, albumID, trackID, err := s.libraryMatch(ctx, track.Title, track.Artist, track.Album)
		if err != nil {
			return TopTrackPage{}, err
		}
		if trackID.Valid {
			value := trackID.Int64
			track.TrackID = &value
		}
		if albumID.Valid {
			value := albumID.Int64
			track.AlbumID = &value
		}
		if fileID.Valid {
			value := fileID.Int64
			track.FileID = &value
			track.InLibrary = true
		}
		out.Items = append(out.Items, track)
	}
	if len(out.Items) == 0 {
		out.Message = "Deezer returned no chart tracks."
	}
	return out, nil
}

func deezerArtistID(ctx context.Context, base, name string) (int64, error) {
	var body deezerSearchArtist
	if err := deezerGet(ctx, base+"/search/artist?limit=10&q="+url.QueryEscape(name), &body); err != nil {
		return 0, err
	}
	key := compactMatchName(name)
	for _, item := range body.Data {
		if compactMatchName(item.Name) == key {
			return item.ID, nil
		}
	}
	return 0, nil
}

func deezerGet(ctx context.Context, endpoint string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "ReelScarey/1.0 (+https://github.com/faaaaq/ReelScarey)")
	response, err := topTracksHTTP.Do(request)
	if err != nil {
		return &Error{Code: "music_top_tracks_unavailable", Message: "The top-tracks provider is unavailable."}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return &Error{Code: "music_top_tracks_unavailable", Message: "The top-tracks provider rejected the request."}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return &Error{Code: "music_top_tracks_unavailable", Message: "The top-tracks provider returned an unreadable response."}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return &Error{Code: "music_top_tracks_unavailable", Message: "The top-tracks provider returned invalid data."}
	}
	return nil
}
