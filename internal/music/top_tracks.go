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
	"sort"
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

type DiscographyAlbum struct {
	ProviderID string `json:"provider_id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Year       *int   `json:"year"`
	CoverURL   string `json:"cover_url"`
	TrackCount int    `json:"track_count"`
	InLibrary  bool   `json:"in_library"`
	AlbumID    *int64 `json:"album_id"`
	Status     string `json:"status"`
}

type DiscographyPage struct {
	Source  string             `json:"source"`
	Items   []DiscographyAlbum `json:"items"`
	Message string             `json:"message"`
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

// deezerCandidate is one same-named artist returned by the Deezer search.
type deezerCandidate struct {
	id   int64
	name string
	fans int64
}

// resolveDeezerArtist picks the Deezer artist behind a catalog artist name
// using evidence instead of popularity. Every exact and normalized name match
// is compared against the artist's saved catalog album titles; the first
// candidate whose discography contains a catalog album wins. When the catalog
// has albums and no candidate corroborates them, the name is not resolved —
// a popular same-named stranger must never own the page. When the catalog has
// no albums yet there is nothing to corroborate, so the best exact-name match
// is used as before. The bool reports whether the identity was verified
// against the catalog.
func (s *Service) resolveDeezerArtist(ctx context.Context, base, name string) (int64, bool, error) {
	var body struct {
		Data []struct {
			ID    int64  `json:"id"`
			Name  string `json:"name"`
			NbFan int64  `json:"nb_fan"`
		} `json:"data"`
	}
	if err := deezerGet(ctx, base+"/search/artist?limit=25&q="+url.QueryEscape(name), &body); err != nil {
		return 0, false, err
	}
	key := compactMatchName(name)
	candidates := []deezerCandidate{}
	for _, item := range body.Data {
		if item.ID <= 0 || compactMatchName(item.Name) != key {
			continue
		}
		candidates = append(candidates, deezerCandidate{id: item.ID, name: item.Name, fans: item.NbFan})
	}
	if len(candidates) == 0 {
		return 0, false, nil
	}
	// Exact display-name matches are stronger evidence than normalized ones.
	sort.SliceStable(candidates, func(i, j int) bool {
		iExact, jExact := candidates[i].name == name, candidates[j].name == name
		if iExact != jExact {
			return iExact
		}
		return candidates[i].fans > candidates[j].fans
	})
	titles, verifiable, err := s.catalogAlbumTitles(ctx, name)
	if err != nil {
		return 0, false, err
	}
	if !verifiable {
		return candidates[0].id, false, nil
	}
	checked := 0
	for _, candidate := range candidates {
		if checked >= 4 {
			break
		}
		checked++
		if s.deezerAlbumsContain(ctx, base, candidate.id, titles) {
			return candidate.id, true, nil
		}
	}
	// Same-named candidates do not carry the catalog's albums. The display
	// name may differ from the catalog name (a stage-name alias, such as
	// "ABK" versus "Anybody Killa"), so the catalog's own album titles are
	// searched instead: whoever owns the catalog's albums on Deezer is the
	// artist, regardless of the display name.
	id := s.deezerArtistByCatalogAlbums(ctx, base, titles)
	if id != 0 {
		return id, true, nil
	}
	// Nothing on Deezer carries the saved catalog's albums; a popular
	// same-named stranger must not own the page.
	return 0, false, nil
}

// deezerArtistByCatalogAlbums searches the catalog's album titles and returns
// the Deezer artist that owns the most of them. An album-title search result
// alone is not enough — the candidate's own album list must contain the
// catalog album, and when several catalog albums exist the majority wins.
func (s *Service) deezerArtistByCatalogAlbums(ctx context.Context, base string, titles []string) int64 {
	type deezerTally struct {
		name  string
		hits  int
		order int
	}
	tallies := map[int64]*deezerTally{}
	order := 0
	checked := 0
	for _, title := range titles {
		if checked >= 3 {
			break
		}
		checked++
		var body struct {
			Data []struct {
				Title  string `json:"title"`
				Artist struct {
					ID   int64  `json:"id"`
					Name string `json:"name"`
				} `json:"artist"`
			} `json:"data"`
		}
		if err := deezerGet(ctx, base+"/search/album?limit=10&q="+url.QueryEscape(title), &body); err != nil {
			return 0
		}
		for _, album := range body.Data {
			if album.Artist.ID <= 0 || !matchNameEqual(album.Title, title) {
				continue
			}
			tally := tallies[album.Artist.ID]
			if tally == nil {
				tally = &deezerTally{name: album.Artist.Name, order: order}
				order++
				tallies[album.Artist.ID] = tally
			}
		}
	}
	if len(tallies) == 0 {
		return 0
	}
	// Verify each candidate's own album list before trusting a search hit.
	type verified struct {
		id    int64
		hits  int
		name  string
		order int
	}
	candidates := []verified{}
	for id, tally := range tallies {
		hits := 0
		var body struct {
			Data []struct {
				Title string `json:"title"`
			} `json:"data"`
		}
		if err := deezerGet(ctx, base+"/artist/"+strconv.FormatInt(id, 10)+"/albums?limit=100", &body); err != nil {
			continue
		}
		for _, album := range body.Data {
			for _, title := range titles {
				if matchNameEqual(album.Title, title) {
					hits++
					break
				}
			}
		}
		if hits > 0 {
			candidates = append(candidates, verified{id: id, hits: hits, name: tally.name, order: tally.order})
		}
	}
	if len(candidates) == 0 {
		return 0
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].hits != candidates[j].hits {
			return candidates[i].hits > candidates[j].hits
		}
		return candidates[i].order < candidates[j].order
	})
	return candidates[0].id
}

// deezerAlbumsContain reports whether the candidate artist's bounded album
// list contains one of the catalog album titles.
func (s *Service) deezerAlbumsContain(ctx context.Context, base string, providerID int64, titles []string) bool {
	var body struct {
		Data []struct {
			Title string `json:"title"`
		} `json:"data"`
	}
	if err := deezerGet(ctx, base+"/artist/"+strconv.FormatInt(providerID, 10)+"/albums?limit=100", &body); err != nil {
		return false
	}
	for _, album := range body.Data {
		for _, title := range titles {
			if matchNameEqual(album.Title, title) {
				return true
			}
		}
	}
	return false
}

// catalogAlbumTitles returns the saved album titles of the catalog artist
// with the given name. The bool reports whether album evidence can be used:
// it is false when several catalog artists share the name, because the
// albums could belong to any of them, and when the artist has no saved
// albums at all, because there is nothing to corroborate against.
func (s *Service) catalogAlbumTitles(ctx context.Context, name string) ([]string, bool, error) {
	rows, err := s.store.db.QueryContext(ctx, `SELECT ar.id, a.title FROM music_artists ar JOIN music_albums a ON a.artist_id=ar.id
		WHERE lower(trim(ar.name))=lower(trim(?))`, name)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	artistIDs := map[int64]bool{}
	titles := []string{}
	for rows.Next() {
		var artistID int64
		var title string
		if err := rows.Scan(&artistID, &title); err != nil {
			return nil, false, err
		}
		artistIDs[artistID] = true
		if title = strings.TrimSpace(title); title != "" {
			titles = append(titles, title)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(artistIDs) > 1 || len(titles) == 0 {
		return nil, false, nil
	}
	return titles, true, nil
}

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
	providerID, verified, err := s.resolveDeezerArtist(ctx, base, artist.Name)
	if err != nil {
		return TopTrackPage{}, err
	}
	if providerID == 0 {
		if verified {
			// Unreachable: a refused resolution reports verified=false.
			return TopTrackPage{Source: "Deezer", Items: []TopTrack{}}, nil
		}
		var titles []string
		titles, verifiable, titleErr := s.catalogAlbumTitles(ctx, artist.Name)
		if titleErr != nil {
			return TopTrackPage{}, titleErr
		}
		if verifiable && len(titles) > 0 {
			// The saved catalog contradicts every same-named Deezer artist;
			// showing a stranger's tracks would be wrong.
			return TopTrackPage{Source: "Deezer", Items: []TopTrack{}, Message: "Deezer's same-named artists do not match the saved albums for this artist, so no top tracks are shown."}, nil
		}
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
		fallback, source, fallbackErr := s.searchTopTracks(ctx, base, artist.Name, limit)
		if fallbackErr == nil && len(fallback) > 0 {
			out.Source = source
			out.Items = fallback
			return out, nil
		}
	}
	if len(out.Items) == 0 {
		out.Message = "Deezer and iTunes returned no top tracks for this artist."
	}
	return out, nil
}

func (s *Service) searchTopTracks(ctx context.Context, base, artistName string, limit int) ([]TopTrack, string, error) {
	key := compactMatchName(artistName)
	items := []TopTrack{}
	var deezerBody struct {
		Data []deezerTrack `json:"data"`
	}
	if err := deezerGet(ctx, base+"/search/track?limit="+strconv.Itoa(limit*3)+"&q="+url.QueryEscape(artistName), &deezerBody); err == nil {
		for _, item := range deezerBody.Data {
			if compactMatchName(item.Artist.Name) != key {
				continue
			}
			title := strings.TrimSpace(item.Title)
			if title == "" {
				continue
			}
			track := TopTrack{
				ProviderID: fmt.Sprintf("%d", item.ID), Title: title,
				Artist: strings.TrimSpace(item.Artist.Name), Album: strings.TrimSpace(item.Album.Title),
				CoverURL: strings.TrimSpace(item.Album.Cover), DurationMS: item.Duration * 1000,
			}
			var fileID, albumID, trackID sql.NullInt64
			trackID, albumID, fileID, err = s.libraryMatch(ctx, title, artistName, track.Album)
			if err != nil {
				return nil, "", err
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
			items = append(items, track)
			if len(items) >= limit {
				return items, "Deezer search", nil
			}
		}
	}
	if len(items) > 0 {
		return items, "Deezer search", nil
	}
	var itunesBody struct {
		Results []struct {
			TrackID         int64  `json:"trackId"`
			TrackName       string `json:"trackName"`
			ArtistName      string `json:"artistName"`
			CollectionName  string `json:"collectionName"`
			ArtworkURL100   string `json:"artworkUrl100"`
			TrackTimeMillis int64  `json:"trackTimeMillis"`
		} `json:"results"`
	}
	endpoint := "https://itunes.apple.com/search?term=" + url.QueryEscape(artistName) + "&entity=song&limit=" + strconv.Itoa(limit*2)
	if err := linkGetJSON(ctx, endpoint, &itunesBody); err == nil {
		for _, item := range itunesBody.Results {
			if compactMatchName(item.ArtistName) != key {
				continue
			}
			title := strings.TrimSpace(item.TrackName)
			if title == "" {
				continue
			}
			track := TopTrack{
				ProviderID: fmt.Sprintf("itunes:%d", item.TrackID), Title: title,
				Artist: strings.TrimSpace(item.ArtistName), Album: strings.TrimSpace(item.CollectionName),
				CoverURL: strings.TrimSpace(item.ArtworkURL100), DurationMS: int(item.TrackTimeMillis),
			}
			var fileID, albumID, trackID sql.NullInt64
			trackID, albumID, fileID, err = s.libraryMatch(ctx, title, artistName, track.Album)
			if err != nil {
				return nil, "", err
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
			items = append(items, track)
			if len(items) >= limit {
				break
			}
		}
	}
	if len(items) > 0 {
		return items, "iTunes search", nil
	}
	return nil, "", nil
}

func (s *Service) libraryMatch(ctx context.Context, title, artist, album string) (sql.NullInt64, sql.NullInt64, sql.NullInt64, error) {
	var fileID, albumID, trackID sql.NullInt64
	err := s.store.db.QueryRowContext(ctx, `SELECT t.id,a.id,(SELECT MIN(f.id) FROM music_files f WHERE f.track_id=t.id AND f.variant_role='primary')
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

// ArtistDiscography returns the provider's full album list, including albums
// with no registered files. It never adds catalog rows.
func (s *Service) ArtistDiscography(ctx context.Context, artistID int64, limit int) (DiscographyPage, error) {
	if artistID < 1 || limit < 1 || limit > 100 {
		return DiscographyPage{}, ErrInvalidInput
	}
	artist, err := s.store.Artist(ctx, artistID)
	if err != nil {
		return DiscographyPage{}, err
	}
	base := s.topBaseURL
	if base == "" {
		base = defaultTopTracksBaseURL
	}
	providerID, verified, err := s.resolveDeezerArtist(ctx, base, artist.Name)
	if err != nil {
		return DiscographyPage{}, err
	}
	if providerID == 0 {
		if !verified {
			var titles []string
			titles, verifiable, titleErr := s.catalogAlbumTitles(ctx, artist.Name)
			if titleErr != nil {
				return DiscographyPage{}, titleErr
			}
			if verifiable && len(titles) > 0 {
				return DiscographyPage{Source: "Deezer", Items: []DiscographyAlbum{}, Message: "Deezer's same-named artists do not match the saved albums for this artist, so the full discography is not shown."}, nil
			}
		}
		return DiscographyPage{Source: "Deezer", Items: []DiscographyAlbum{}, Message: "Deezer has no exact artist match for this name."}, nil
	}
	var body struct {
		Data []struct {
			ID          int64  `json:"id"`
			Title       string `json:"title"`
			CoverXL     string `json:"cover_xl"`
			CoverBig    string `json:"cover_big"`
			CoverMedium string `json:"cover_medium"`
			ReleaseDate string `json:"release_date"`
			NbTracks    int    `json:"nb_tracks"`
			RecordType  string `json:"record_type"`
			Artist      struct {
				Name string `json:"name"`
			} `json:"artist"`
		} `json:"data"`
	}
	if err := deezerGet(ctx, base+"/artist/"+strconv.FormatInt(providerID, 10)+"/albums?limit="+strconv.Itoa(limit), &body); err != nil {
		return DiscographyPage{}, err
	}
	out := DiscographyPage{Source: "Deezer", Items: []DiscographyAlbum{}}
	for _, item := range body.Data {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			continue
		}
		album := DiscographyAlbum{
			ProviderID: strconv.FormatInt(item.ID, 10),
			Title:      title,
			Artist:     strings.TrimSpace(item.Artist.Name),
			Year:       yearFromText(item.ReleaseDate),
			CoverURL:   strings.TrimSpace(firstNonEmpty(item.CoverXL, item.CoverBig, item.CoverMedium)),
			TrackCount: item.NbTracks,
			Status:     strings.TrimSpace(item.RecordType),
		}
		var albumID int64
		var status, cover string
		err := s.store.db.QueryRowContext(ctx, `SELECT a.id,a.status,a.cover_url FROM music_albums a JOIN music_artists ar ON ar.id=a.artist_id
			WHERE lower(trim(a.title))=lower(trim(?)) AND lower(trim(ar.name))=lower(trim(?)) ORDER BY a.id LIMIT 1`, title, artist.Name).
			Scan(&albumID, &status, &cover)
		if err == nil {
			album.AlbumID = &albumID
			album.InLibrary = status == "available"
			if album.CoverURL == "" {
				album.CoverURL = cover
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return DiscographyPage{}, err
		}
		out.Items = append(out.Items, album)
	}
	if len(out.Items) == 0 {
		out.Message = "Deezer returned no albums for this artist."
	}
	return out, nil
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
