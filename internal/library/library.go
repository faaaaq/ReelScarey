package library

import (
	"context"
	"database/sql"
	"fmt"
)

type Item struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	TMDbID    int64  `json:"tmdb_id"`
	Title     string `json:"title"`
	Year      *int   `json:"year"`
	Monitored bool   `json:"monitored"`
	Status    string `json:"status"`
}

type Counts struct {
	Movies int `json:"movies"`
	Series int `json:"series"`
}
type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Counts(ctx context.Context) (Counts, error) {
	var counts Counts
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(kind = 'movie'), 0), COALESCE(SUM(kind = 'series'), 0) FROM media_items`).Scan(&counts.Movies, &counts.Series)
	return counts, err
}

// List is bounded until the movie workflow introduces pagination and add APIs.
func (s *Store) List(ctx context.Context, kind string) ([]Item, error) {
	if kind != "movie" && kind != "series" {
		return nil, fmt.Errorf("kind must be movie or series")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, tmdb_id, title, year, monitored, status FROM media_items WHERE kind = ? ORDER BY title, id LIMIT 100`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Item, 0)
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.ID, &item.Kind, &item.TMDbID, &item.Title, &item.Year, &item.Monitored, &item.Status); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
