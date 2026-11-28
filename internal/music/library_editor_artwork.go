package music

import (
	"context"
	"net/url"
	"strings"
)

type LibraryArtworkCandidate struct {
	Title  string `json:"title"`
	Artist string `json:"artist,omitempty"`
	URL    string `json:"url"`
}

func (s *Service) SearchLibraryArtwork(ctx context.Context, kind, query string) ([]LibraryArtworkCandidate, error) {
	query = strings.TrimSpace(query)
	if (kind != "artists" && kind != "albums") || !ValidQuery(query, 1) {
		return nil, ErrInvalidInput
	}
	base := s.topBaseURL
	if base == "" {
		base = defaultTopTracksBaseURL
	}
	items := []LibraryArtworkCandidate{}
	seen := map[string]bool{}
	add := func(title, artist string, pictures ...string) {
		if len(items) >= 12 {
			return
		}
		for _, picture := range pictures {
			if allowedArtworkURL(picture) && !seen[picture] {
				items = append(items, LibraryArtworkCandidate{Title: title, Artist: artist, URL: picture})
				seen[picture] = true
				return
			}
		}
	}
	params := "?limit=12&q=" + url.QueryEscape(query)
	if kind == "artists" {
		var body struct {
			Data []struct {
				Name          string `json:"name"`
				PictureXL     string `json:"picture_xl"`
				PictureBig    string `json:"picture_big"`
				PictureMedium string `json:"picture_medium"`
			} `json:"data"`
		}
		if err := deezerGet(ctx, base+"/search/artist"+params, &body); err != nil {
			return nil, err
		}
		for _, item := range body.Data {
			add(item.Name, "", item.PictureXL, item.PictureBig, item.PictureMedium)
		}
	} else {
		var body deezerAlbumSearch
		if err := deezerGet(ctx, base+"/search/album"+params, &body); err != nil {
			return nil, err
		}
		for _, item := range body.Data {
			add(item.Title, item.Artist.Name, item.CoverXL, item.CoverBig, item.CoverMedium)
		}
	}
	return items, nil
}
