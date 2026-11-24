package httpapi

import (
	"context"
	"strconv"

	"github.com/faaaaq/ReelScarey/internal/metadata"
)

type recentTitle struct {
	ID       int64  `json:"id"`
	Kind     string `json:"kind"`
	Category string `json:"category"`
	Title    string `json:"title"`
	Year     *int   `json:"year"`
	Artwork  string `json:"artwork_url"`
}

// The two catalogs store timestamps differently. Convert only the newest five
// candidates from each before ordering the combined shelf.
func (s *Server) recentCatalog(ctx context.Context) ([]recentTitle, error) {
	rows, err := s.db.QueryContext(ctx, `
        WITH video AS (
            SELECT id, kind, category, title, year, poster_path, created_at
            FROM media_items ORDER BY created_at DESC, id DESC LIMIT 5
        ), albums AS (
            SELECT id, title, year, created_at FROM music_albums ORDER BY created_at DESC, id DESC LIMIT 5
        ), recent AS (
            SELECT id, kind, category, title, year, poster_path, julianday(created_at) AS added FROM video
            UNION ALL
            SELECT id, 'album', '', title, year, '', julianday(created_at, 'unixepoch') FROM albums
        )
        SELECT id, kind, category, title, year, poster_path FROM recent
        ORDER BY added DESC, id DESC, kind LIMIT 5`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	titles := make([]recentTitle, 0, 5)
	for rows.Next() {
		var item recentTitle
		var poster string
		if err := rows.Scan(&item.ID, &item.Kind, &item.Category, &item.Title, &item.Year, &poster); err != nil {
			return nil, err
		}
		if item.Kind == "album" {
			item.Artwork = "/api/v1/library/music/albums/" + strconv.FormatInt(item.ID, 10) + "/cover"
		} else {
			item.Artwork = metadata.PosterURL(poster)
		}
		titles = append(titles, item)
	}
	return titles, rows.Err()
}
