package music

import (
	"context"
	"database/sql"
)

// WantedAlbum is one monitored album that has no registered files yet.
type WantedAlbum struct {
	ID         int64  `json:"id"`
	ArtistID   int64  `json:"artist_id"`
	ArtistName string `json:"artist_name"`
	Title      string `json:"title"`
	Year       *int   `json:"year"`
	Status     string `json:"status"`
	TrackCount int    `json:"track_count"`
}

type WantedAlbumPage struct {
	Items []WantedAlbum `json:"items"`
	Page  int           `json:"page"`
	Total int           `json:"total"`
	Limit int           `json:"limit"`
}

const wantedAlbumSelection = `SELECT a.id, a.artist_id, ar.name, a.title, a.year, a.status,
	(SELECT COUNT(*) FROM music_tracks t WHERE t.album_id=a.id)
	FROM music_albums a
	JOIN music_artists ar ON ar.id=a.artist_id
	WHERE a.monitored=1 AND ar.monitored=1
	AND NOT EXISTS(SELECT 1 FROM music_files f WHERE f.album_id=a.id)`

const wantedAlbumQuery = wantedAlbumSelection + ` ORDER BY ar.name COLLATE NOCASE, a.title COLLATE NOCASE, a.id`

// WantedAlbums projects monitored albums that have no registered files. The
// projection never scans disk or contacts a provider.
func (s *Service) WantedAlbums(ctx context.Context, page int) (WantedAlbumPage, error) {
	if page < 1 || page > 100000 {
		return WantedAlbumPage{}, ErrInvalidInput
	}
	out := WantedAlbumPage{Items: []WantedAlbum{}, Page: page, Limit: pageSize}
	if err := s.store.db.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM music_albums a
		JOIN music_artists ar ON ar.id=a.artist_id
		WHERE a.monitored=1 AND ar.monitored=1
		AND NOT EXISTS(SELECT 1 FROM music_files f WHERE f.album_id=a.id)`).Scan(&out.Total); err != nil {
		return WantedAlbumPage{}, err
	}
	rows, err := s.store.db.QueryContext(ctx, wantedAlbumQuery+` LIMIT ? OFFSET ?`, pageSize, (page-1)*pageSize)
	if err != nil {
		return WantedAlbumPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		album, err := scanWantedAlbum(rows)
		if err != nil {
			return WantedAlbumPage{}, err
		}
		out.Items = append(out.Items, album)
	}
	return out, rows.Err()
}

func scanWantedAlbum(row interface{ Scan(...any) error }) (WantedAlbum, error) {
	var album WantedAlbum
	var year sql.NullInt64
	if err := row.Scan(&album.ID, &album.ArtistID, &album.ArtistName, &album.Title, &year, &album.Status, &album.TrackCount); err != nil {
		return WantedAlbum{}, err
	}
	if year.Valid {
		value := int(year.Int64)
		album.Year = &value
	}
	return album, nil
}
