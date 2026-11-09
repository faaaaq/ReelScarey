package music

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const artworkTTL = 30 * 24 * time.Hour

type Artwork struct {
	File        *os.File
	Name        string
	ContentType string
	ModTime     time.Time
}

var artworkFetch = fetchArtworkBytes

func (s *Service) ArtistImage(ctx context.Context, id int64) (Artwork, error) {
	if id < 1 || s.artworkDir == "" {
		return Artwork{}, &Error{Code: "music_artwork_missing", Message: "No cached artist image is available."}
	}
	s.artworkMu.Lock()
	art, fresh, _ := s.cachedArtwork("artist", id)
	s.artworkMu.Unlock()
	if fresh {
		return art, nil
	}
	if art.File != nil {
		_ = art.File.Close()
		art = Artwork{}
	}
	unlock := s.lockArtwork("artist-" + strconv.FormatInt(id, 10))
	defer unlock()
	art, fresh, _ = s.cachedArtwork("artist", id)
	if fresh {
		return art, nil
	}
	source, err := s.artistImageSource(ctx, id)
	if err == nil && source != "" {
		if err := s.downloadArtwork(ctx, "artist", id, source); err == nil {
			if art, _, err = s.cachedArtwork("artist", id); err == nil {
				return art, nil
			}
		}
	}
	if art.File != nil {
		return art, nil
	}
	return Artwork{}, &Error{Code: "music_artwork_missing", Message: "No artist image is available."}
}

func (s *Service) AlbumImage(ctx context.Context, id int64) (Artwork, error) {
	if id < 1 || s.artworkDir == "" {
		return Artwork{}, &Error{Code: "music_artwork_missing", Message: "No cached album cover is available."}
	}
	s.artworkMu.Lock()
	art, fresh, _ := s.cachedArtwork("album", id)
	s.artworkMu.Unlock()
	if fresh {
		return art, nil
	}
	if art.File != nil {
		_ = art.File.Close()
		art = Artwork{}
	}
	unlock := s.lockArtwork("album-" + strconv.FormatInt(id, 10))
	defer unlock()
	art, fresh, _ = s.cachedArtwork("album", id)
	if fresh {
		return art, nil
	}
	source, err := s.albumCoverSource(ctx, id)
	if err == nil && source != "" {
		if err := s.downloadArtwork(ctx, "album", id, source); err == nil {
			if art, _, err = s.cachedArtwork("album", id); err == nil {
				return art, nil
			}
		}
	}
	if art.File != nil {
		return art, nil
	}
	return Artwork{}, &Error{Code: "music_artwork_missing", Message: "No album cover is available."}
}

func (s *Service) artworkBase(kind string, id int64) string {
	return filepath.Join(s.artworkDir, kind+"-"+strconv.FormatInt(id, 10))
}

func (s *Service) lockArtwork(key string) func() {
	s.artworkMu.Lock()
	if s.artworkLocks == nil {
		s.artworkLocks = map[string]*sync.Mutex{}
	}
	mu, ok := s.artworkLocks[key]
	if !ok {
		mu = &sync.Mutex{}
		s.artworkLocks[key] = mu
	}
	s.artworkMu.Unlock()
	mu.Lock()
	return mu.Unlock
}

func (s *Service) cachedArtwork(kind string, id int64) (Artwork, bool, error) {
	for _, ext := range []string{".jpg", ".png", ".webp"} {
		name := kind + "-" + strconv.FormatInt(id, 10) + ext
		info, err := os.Stat(filepath.Join(s.artworkDir, name))
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		file, err := os.Open(filepath.Join(s.artworkDir, name))
		if err != nil {
			return Artwork{}, false, err
		}
		return Artwork{File: file, Name: name, ContentType: artworkContentType(ext), ModTime: info.ModTime()}, time.Since(info.ModTime()) < artworkTTL, nil
	}
	return Artwork{}, false, nil
}

func artworkContentType(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}

func (s *Service) artistImageSource(ctx context.Context, id int64) (string, error) {
	var imageURL, name string
	if err := s.store.db.QueryRowContext(ctx, `SELECT image_url,name FROM music_artists WHERE id=?`, id).Scan(&imageURL, &name); err != nil {
		return "", err
	}
	if allowedArtworkURL(imageURL) {
		return imageURL, nil
	}
	base := s.topBaseURL
	if base == "" {
		base = defaultTopTracksBaseURL
	}
	picture, err := deezerArtistPicture(ctx, base, name)
	if err != nil || picture == "" {
		return "", err
	}
	if _, err := s.store.db.ExecContext(ctx, `UPDATE music_artists SET image_url=?,updated_at=unixepoch() WHERE id=?`, picture, id); err != nil {
		return "", err
	}
	return picture, nil
}

func (s *Service) albumCoverSource(ctx context.Context, id int64) (string, error) {
	var coverURL, title, artist, mbid string
	if err := s.store.db.QueryRowContext(ctx, `SELECT a.cover_url,a.title,ar.name,a.musicbrainz_id
		FROM music_albums a JOIN music_artists ar ON ar.id=a.artist_id WHERE a.id=?`, id).
		Scan(&coverURL, &title, &artist, &mbid); err != nil {
		return "", err
	}
	if allowedArtworkURL(coverURL) {
		return coverURL, nil
	}
	base := s.topBaseURL
	if base == "" {
		base = defaultTopTracksBaseURL
	}
	if cover, err := deezerAlbumCover(ctx, base, artist, title); err == nil && cover != "" {
		_, _ = s.store.db.ExecContext(ctx, `UPDATE music_albums SET cover_url=?,updated_at=unixepoch() WHERE id=? AND cover_url=''`, cover, id)
		return cover, nil
	}
	if ValidMBID(mbid) {
		return coverURLFor(mbid), nil
	}
	return "", nil
}

func coverURLFor(mbid string) string {
	return "https://coverartarchive.org/release-group/" + mbid + "/front-500"
}

type deezerArtistSearch struct {
	Data []struct {
		ID            int64  `json:"id"`
		Name          string `json:"name"`
		PictureXL     string `json:"picture_xl"`
		PictureBig    string `json:"picture_big"`
		PictureMedium string `json:"picture_medium"`
	} `json:"data"`
}

func deezerArtistPicture(ctx context.Context, base, name string) (string, error) {
	var body deezerArtistSearch
	if err := deezerGet(ctx, base+"/search/artist?limit=10&q="+url.QueryEscape(name), &body); err != nil {
		return "", err
	}
	key := compactMatchName(name)
	for _, item := range body.Data {
		if compactMatchName(item.Name) != key {
			continue
		}
		for _, image := range []string{item.PictureXL, item.PictureBig, item.PictureMedium} {
			if allowedArtworkURL(image) {
				return image, nil
			}
		}
	}
	return "", nil
}

type deezerAlbumSearch struct {
	Data []struct {
		Title  string `json:"title"`
		Artist struct {
			Name string `json:"name"`
		} `json:"artist"`
		CoverXL     string `json:"cover_xl"`
		CoverBig    string `json:"cover_big"`
		CoverMedium string `json:"cover_medium"`
	} `json:"data"`
}

func deezerAlbumCover(ctx context.Context, base, artist, album string) (string, error) {
	var body deezerAlbumSearch
	query := strings.TrimSpace(artist + " " + album)
	if err := deezerGet(ctx, base+"/search/album?limit=10&q="+url.QueryEscape(query), &body); err != nil {
		return "", err
	}
	titleKey := compactMatchName(album)
	artistKey := compactMatchName(artist)
	for _, item := range body.Data {
		if compactMatchName(item.Title) != titleKey || compactMatchName(item.Artist.Name) != artistKey {
			continue
		}
		for _, image := range []string{item.CoverXL, item.CoverBig, item.CoverMedium} {
			if allowedArtworkURL(image) {
				return image, nil
			}
		}
	}
	return "", nil
}

func (s *Service) downloadArtwork(ctx context.Context, kind string, id int64, source string) error {
	body, contentType, err := artworkFetch(ctx, source)
	if err != nil || len(body) == 0 {
		return &Error{Code: "music_artwork_missing", Message: "The artwork could not be downloaded."}
	}
	ext := ""
	switch strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0])) {
	case "image/jpeg", "image/jpg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	default:
		return &Error{Code: "music_artwork_missing", Message: "The artwork response is not a supported image."}
	}
	if err := os.MkdirAll(s.artworkDir, 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(s.artworkDir, ".artwork-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := temp.Write(body); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	final := s.artworkBase(kind, id) + ext
	if err := os.Rename(tempName, final); err != nil {
		return err
	}
	for _, other := range []string{".jpg", ".png", ".webp"} {
		if other != ext {
			_ = os.Remove(s.artworkBase(kind, id) + other)
		}
	}
	return nil
}

func allowedArtworkURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" {
		return false
	}
	return allowedArtworkHost(strings.ToLower(parsed.Hostname()))
}

func allowedArtworkHost(host string) bool {
	switch host {
	case "coverartarchive.org", "archive.org", "i.scdn.co",
		"image-cdn-fa.spotifycdn.com", "image-cdn-ak.spotifycdn.com", "image-cdn.spotifycdn.com",
		"e-cdns-images.dzcdn.net", "cdn-images.dzcdn.net":
		return true
	}
	return strings.HasSuffix(host, ".archive.org")
}

var artworkHTTP = &http.Client{
	Timeout: 20 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many artwork redirects")
		}
		if !allowedArtworkHost(strings.ToLower(req.URL.Hostname())) {
			return errors.New("artwork redirect host is not allowed")
		}
		return nil
	},
}

func fetchArtworkBytes(ctx context.Context, source string) ([]byte, string, error) {
	if !allowedArtworkURL(source) {
		return nil, "", &Error{Code: "music_artwork_missing", Message: "The artwork source is not allowed."}
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, "", err
	}
	request.Header.Set("Accept", "image/*")
	request.Header.Set("User-Agent", "ReelScarey/1.0 (+https://github.com/faaaaq/ReelScarey)")
	response, err := artworkHTTP.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", &Error{Code: "music_artwork_missing", Message: "The artwork request failed."}
	}
	contentType := response.Header.Get("Content-Type")
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, "", err
	}
	return body, contentType, nil
}
