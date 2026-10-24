package library

import (
	"context"
	"database/sql"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Item struct {
	ID             int64  `json:"id"`
	Kind           string `json:"kind"`
	TMDbID         int64  `json:"tmdb_id"`
	Title          string `json:"title"`
	Year           *int   `json:"year"`
	Monitored      bool   `json:"monitored"`
	Status         string `json:"status"`
	Overview       string `json:"overview"`
	PosterPath     string `json:"poster_path"`
	PosterURL      string `json:"poster_url"`
	ReleaseDate    string `json:"release_date"`
	RuntimeMinutes *int   `json:"runtime_minutes"`
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

type Page struct {
	Items           []Item `json:"items"`
	Page            int    `json:"page"`
	Total           int    `json:"total"`
	Limit           int    `json:"limit"`
	CollectionTotal int    `json:"collection_total"`
}

const PageSize = 24
const columns = "id, kind, tmdb_id, title, year, monitored, status, overview, poster_path, release_date, runtime_minutes"

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Item, error) {
	var item Item
	err := row.Scan(&item.ID, &item.Kind, &item.TMDbID, &item.Title, &item.Year, &item.Monitored, &item.Status, &item.Overview, &item.PosterPath, &item.ReleaseDate, &item.RuntimeMinutes)
	item.PosterURL = metadata.PosterURL(item.PosterPath)
	return item, err
}

func (s *Store) List(ctx context.Context, kind string, page int) (Page, error) {
	return s.Browse(ctx, BrowseFilter{Kind: kind, Page: page})
}

var ErrBrowseInput = errors.New("Choose valid library filters and a page between 1 and 100000.")

type BrowseFilter struct {
	Kind, Query, Status, Monitored, Sort string
	Page                                 int
}

// Browse reads saved catalog state only. Ordering SQL comes from a fixed allowlist.
func (s *Store) Browse(ctx context.Context, f BrowseFilter) (Page, error) {
	orders := map[string]string{
		"title":      "title COLLATE NOCASE, id",
		"title_desc": "title COLLATE NOCASE DESC, id",
		"year_desc":  "year IS NULL, year DESC, title COLLATE NOCASE, id",
		"year":       "year IS NULL, year, title COLLATE NOCASE, id",
		"added":      "id DESC",
	}
	if f.Sort == "" {
		f.Sort = "title"
	}
	if f.Status == "" {
		f.Status = "all"
	}
	if f.Monitored == "" {
		f.Monitored = "all"
	}
	if (f.Kind != "movie" && f.Kind != "series") || f.Page < 1 || f.Page > 100000 ||
		orders[f.Sort] == "" || (f.Status != "all" && f.Status != "missing" && f.Status != "available" && f.Status != "downloading") ||
		(f.Monitored != "all" && f.Monitored != "monitored" && f.Monitored != "paused") ||
		len(f.Query) > 200 || !utf8.ValidString(f.Query) || strings.ContainsFunc(f.Query, unicode.IsControl) {
		return Page{}, ErrBrowseInput
	}
	f.Query = strings.TrimSpace(f.Query)
	where := " WHERE kind=? AND title LIKE ? ESCAPE '\\' AND (?='all' OR status=?) AND (?='all' OR monitored=?)"
	args := []any{f.Kind, planningText(f.Query), f.Status, f.Status, f.Monitored, f.Monitored == "monitored"}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Page{}, err
	}
	defer tx.Rollback()
	out := Page{Items: make([]Item, 0), Page: f.Page, Limit: PageSize}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM media_items WHERE kind=?", f.Kind).Scan(&out.CollectionTotal); err != nil {
		return Page{}, err
	}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM media_items"+where, args...).Scan(&out.Total); err != nil {
		return Page{}, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+columns+" FROM media_items"+where+" ORDER BY "+orders[f.Sort]+" LIMIT ? OFFSET ?", append(args, PageSize, (f.Page-1)*PageSize)...)
	if err != nil {
		return Page{}, err
	}
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			rows.Close()
			return Page{}, err
		}
		out.Items = append(out.Items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Page{}, err
	}
	if err := tx.Commit(); err != nil {
		return Page{}, err
	}
	return out, nil
}

func (s *Store) Movie(ctx context.Context, tmdbID int64) (Item, error) {
	return scan(s.db.QueryRowContext(ctx, "SELECT "+columns+" FROM media_items WHERE kind = 'movie' AND tmdb_id = ?", tmdbID))
}
func (s *Store) ByID(ctx context.Context, id int64) (Item, error) {
	return scan(s.db.QueryRowContext(ctx, "SELECT "+columns+" FROM media_items WHERE kind = 'movie' AND id = ?", id))
}
func (s *Store) Item(ctx context.Context, id int64) (Item, error) {
	return scan(s.db.QueryRowContext(ctx, "SELECT "+columns+" FROM media_items WHERE id=?", id))
}
func (s *Store) AddMovie(ctx context.Context, movie metadata.Movie, monitored bool) (Item, bool, error) {
	if movie.TMDbID < 1 || movie.Title == "" {
		return Item{}, false, errors.New("invalid movie metadata")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO media_items(kind,tmdb_id,title,year,monitored,overview,poster_path,release_date,runtime_minutes) VALUES('movie',?,?,?,?,?,?,?,?) ON CONFLICT(kind,tmdb_id) DO NOTHING`, movie.TMDbID, movie.Title, movie.Year, monitored, movie.Overview, movie.PosterPath, movie.ReleaseDate, movie.RuntimeMinutes)
	if err != nil {
		return Item{}, false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return Item{}, false, err
	}
	item, err := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM media_items WHERE kind='movie' AND tmdb_id=?", movie.TMDbID))
	if err != nil {
		return Item{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Item{}, false, err
	}
	return item, count == 1, nil
}
