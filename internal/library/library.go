package library

import (
	"context"
	"database/sql"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Item struct {
	ID                    int64           `json:"id"`
	Kind                  string          `json:"kind"`
	TMDbID                int64           `json:"tmdb_id"`
	Title                 string          `json:"title"`
	Year                  *int            `json:"year"`
	Monitored             bool            `json:"monitored"`
	Status                string          `json:"status"`
	Overview              string          `json:"overview"`
	PosterPath            string          `json:"poster_path"`
	PosterURL             string          `json:"poster_url"`
	ReleaseDate           string          `json:"release_date"`
	TheatricalReleaseDate string          `json:"theatrical_release_date"`
	DigitalReleaseDate    string          `json:"digital_release_date"`
	PhysicalReleaseDate   string          `json:"physical_release_date"`
	RuntimeMinutes        *int            `json:"runtime_minutes"`
	QualityProfile        *CatalogProfile `json:"quality_profile,omitempty"`
	Category              string          `json:"category"`
	CategoryRevision      int             `json:"category_revision"`
}

type Counts struct {
	Movies int `json:"movies"`
	Series int `json:"series"`
	Anime  int `json:"anime"`
}
type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Counts(ctx context.Context) (Counts, error) {
	var counts Counts
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(kind = 'movie'), 0), COALESCE(SUM(kind = 'series' AND category='standard'), 0),COALESCE(SUM(kind='series' AND category='anime'),0) FROM media_items`).Scan(&counts.Movies, &counts.Series, &counts.Anime)
	return counts, err
}

type Page struct {
	Items           []Item          `json:"items"`
	Page            int             `json:"page"`
	Total           int             `json:"total"`
	Limit           int             `json:"limit"`
	CollectionTotal int             `json:"collection_total"`
	ProfileContext  *ProfileContext `json:"profile_context,omitempty"`
}

const PageSize = 48

// MaxAllItems bounds page=all so one request cannot materialize an unbounded
// catalog response.
const MaxAllItems = 5000

// titleSortExpr orders alphabetically by title without a leading "The ",
// while the stored and displayed title stays unchanged. Browse uses the
// materialized title_sort generated column (migration 099) with the identical
// definition so its ORDER BY can use the title_sort index; queries that read
// projections without that column (planning) keep the explicit expression.
// SQLite's lower() is ASCII-only, matching the existing COLLATE NOCASE
// behavior.
const titleSortExpr = "(CASE WHEN lower(title) LIKE 'the %' THEN substr(title,5) ELSE title END)"

// TitleSort references the generated column for ordering.
const TitleSort = "title_sort"

const columns = "id, kind, tmdb_id, title, year, monitored, status, overview, poster_path, release_date, theatrical_release_date, digital_release_date, physical_release_date, runtime_minutes, category, category_revision"

type scanner interface{ Scan(...any) error }

func scan(row scanner, extra ...any) (Item, error) {
	var item Item
	args := []any{&item.ID, &item.Kind, &item.TMDbID, &item.Title, &item.Year, &item.Monitored, &item.Status, &item.Overview, &item.PosterPath, &item.ReleaseDate, &item.TheatricalReleaseDate, &item.DigitalReleaseDate, &item.PhysicalReleaseDate, &item.RuntimeMinutes, &item.Category, &item.CategoryRevision}
	err := row.Scan(append(args, extra...)...)
	item.PosterURL = metadata.PosterURL(item.PosterPath)
	return item, err
}

func (s *Store) List(ctx context.Context, kind string, page int) (Page, error) {
	return s.Browse(ctx, BrowseFilter{Kind: kind, Page: page})
}

var ErrBrowseInput = errors.New("Choose valid library filters and a page between 1 and 100000.")

// ErrBrowseAllLimit means page=all matched more than MaxAllItems. The caller
// must narrow the filters or request a numbered page.
var ErrBrowseAllLimit = errors.New("This library view has more than 5000 items. Narrow the filters or choose a page.")

type BrowseFilter struct {
	Kind, Query, Status, Monitored, Sort, Profile string
	Page                                          int
	All                                           bool
}

// Browse reads saved catalog state only. Ordering SQL comes from a fixed allowlist.
func (s *Store) Browse(ctx context.Context, f BrowseFilter) (Page, error) {
	orders := map[string]string{
		"title":      TitleSort + " COLLATE NOCASE, id",
		"title_desc": TitleSort + " COLLATE NOCASE DESC, id",
		"year_desc":  "year IS NULL, year DESC, " + TitleSort + " COLLATE NOCASE, id",
		"year":       "year IS NULL, year, " + TitleSort + " COLLATE NOCASE, id",
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
	if (f.Kind != "movie" && f.Kind != "series" && f.Kind != "anime") || f.Page < 1 || f.Page > 100000 ||
		orders[f.Sort] == "" || (f.Status != "all" && f.Status != "missing" && f.Status != "available" && f.Status != "downloading") ||
		(f.Monitored != "all" && f.Monitored != "monitored" && f.Monitored != "paused") ||
		len(f.Query) > 200 || !utf8.ValidString(f.Query) || strings.ContainsFunc(f.Query, unicode.IsControl) {
		return Page{}, ErrBrowseInput
	}
	f.Query = strings.TrimSpace(f.Query)
	collectionWhere := " WHERE kind=? AND category='standard'"
	collectionArgs := []any{f.Kind}
	if f.Kind == "movie" {
		collectionWhere = " WHERE kind=?"
	}
	if f.Kind == "anime" {
		collectionWhere = " WHERE kind='series' AND category='anime'"
		collectionArgs = nil
	}
	where := collectionWhere + " AND title LIKE ? ESCAPE '\\' AND (?='all' OR status=?) AND (?='all' OR monitored=?)"
	args := append(append([]any{}, collectionArgs...), planningText(f.Query), f.Status, f.Status, f.Monitored, f.Monitored == "monitored")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Page{}, err
	}
	defer tx.Rollback()
	out := Page{Items: make([]Item, 0), Page: f.Page, Limit: PageSize}
	profileContext, profiles, e := catalogProfiles(ctx, tx)
	if e != nil {
		return Page{}, e
	}
	out.ProfileContext = profileContext
	// item_automation is keyed by media_id (its primary key), so the per-row
	// profile subselects collapse into one left join without changing rows or
	// ordering (RS-0001).
	from := " FROM media_items LEFT JOIN item_automation a ON a.media_id=media_items.id"
	if f.Profile == "default" {
		where += ` AND a.profile_id IS NULL`
	} else if f.Profile != "" && f.Profile != "all" {
		id, e := strconv.ParseInt(f.Profile, 10, 64)
		if e != nil || id < 1 || strconv.FormatInt(id, 10) != f.Profile || profiles[id].ID == 0 {
			return Page{}, ErrBrowseInput
		}
		where += ` AND COALESCE(a.profile_id,?)=?`
		args = append(args, profileContext.Default.ID, id)
	}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM media_items"+collectionWhere, collectionArgs...).Scan(&out.CollectionTotal); err != nil {
		return Page{}, err
	}
	// Without a query, status, monitoring or profile filter the filtered
	// total equals the collection total, so the second full scan is skipped
	// on the default browse view (RS-0001).
	if f.Query != "" || f.Status != "all" || f.Monitored != "all" || (f.Profile != "" && f.Profile != "all") {
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM media_items LEFT JOIN item_automation a ON a.media_id=media_items.id"+where, args...).Scan(&out.Total); err != nil {
			return Page{}, err
		}
	} else {
		out.Total = out.CollectionTotal
	}
	if f.All && out.Total > MaxAllItems {
		return Page{}, ErrBrowseAllLimit
	}
	query := "SELECT " + columns + ",a.profile_id,COALESCE(a.revision,0)" + from + where + " ORDER BY " + orders[f.Sort]
	if f.All {
		out.Page = 1
		out.Limit = out.Total
	} else {
		query += " LIMIT ? OFFSET ?"
		args = append(args, PageSize, (f.Page-1)*PageSize)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return Page{}, err
	}
	for rows.Next() {
		var profileID sql.NullInt64
		var revision int
		item, err := scan(rows, &profileID, &revision)
		if err != nil {
			rows.Close()
			return Page{}, err
		}
		effectiveID := profileContext.Default.ID
		assignment := &CatalogProfile{Revision: revision}
		if profileID.Valid {
			id := profileID.Int64
			assignment.ProfileID = &id
			effectiveID = id
		}
		var exists bool
		assignment.Effective, exists = profiles[effectiveID]
		if !exists {
			rows.Close()
			return Page{}, errors.New("saved profile assignment is unavailable")
		}
		item.QualityProfile = assignment
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
func (s *Store) AddMovie(ctx context.Context, movie metadata.Movie, monitored bool, category ...string) (Item, bool, error) {
	return s.AddMovieAuthorized(ctx, movie, monitored, nil, category...)
}

// AddMovieAuthorized fences durable workers in the catalog creation transaction.
func (s *Store) AddMovieAuthorized(ctx context.Context, movie metadata.Movie, monitored bool, authorize func(*sql.Tx) error, category ...string) (Item, bool, error) {
	section, e := addCategory(category)
	if e != nil {
		return Item{}, false, e
	}
	if movie.TMDbID < 1 || movie.Title == "" {
		return Item{}, false, errors.New("invalid movie metadata")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, false, err
	}
	defer tx.Rollback()
	if authorize != nil {
		if err := authorize(tx); err != nil {
			return Item{}, false, err
		}
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO media_items(kind,tmdb_id,title,year,monitored,overview,poster_path,release_date,theatrical_release_date,digital_release_date,physical_release_date,runtime_minutes,category) VALUES('movie',?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(kind,tmdb_id) DO NOTHING`, movie.TMDbID, movie.Title, movie.Year, monitored, movie.Overview, movie.PosterPath, movie.ReleaseDate, movie.TheatricalReleaseDate, movie.DigitalReleaseDate, movie.PhysicalReleaseDate, movie.RuntimeMinutes, section)
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
	if count == 1 {
		if err = QueueMetadataRefreshTx(ctx, tx, item.ID, false); err != nil {
			return Item{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Item{}, false, err
	}
	return item, count == 1, nil
}
