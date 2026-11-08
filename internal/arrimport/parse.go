// Package arrimport reads only the title list and monitored flag from a
// Radarr or Sonarr backup. It never reads credentials, indexers, clients,
// paths, quality profiles or other private tables.
package arrimport

import (
	"archive/zip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/faaaaq/ReelScarey/internal/database"
)

const (
	maxArchiveBytes  = 256 << 20
	maxDatabaseBytes = 512 << 20
	maxItems         = 10000
)

type Candidate struct {
	Kind      string `json:"kind"`
	TMDbID    int64  `json:"tmdb_id,omitempty"`
	TVDBID    int64  `json:"tvdb_id,omitempty"`
	Title     string `json:"title"`
	Year      *int   `json:"year,omitempty"`
	Monitored bool   `json:"monitored"`
}

type Backup struct {
	Source string      `json:"source"`
	Items  []Candidate `json:"items"`
}

// Parse reads a Radarr or Sonarr backup ZIP and returns its title list. The
// archive may be a full backup; only the database file is extracted.
func Parse(ctx context.Context, archive string) (Backup, error) {
	info, err := os.Stat(archive)
	if err != nil {
		return Backup{}, err
	}
	if info.Size() <= 0 || info.Size() > maxArchiveBytes {
		return Backup{}, fmt.Errorf("the backup must be between 1 byte and %d MiB", maxArchiveBytes>>20)
	}
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return Backup{}, fmt.Errorf("open backup: %w", err)
	}
	defer zr.Close()
	var dbEntry, walEntry, shmEntry *zip.File
	var source string
	for _, f := range zr.File {
		base := strings.ToLower(path.Base(f.Name))
		switch base {
		case "radarr.db":
			if source != "" && source != "radarr" {
				return Backup{}, fmt.Errorf("the backup contains both Radarr and Sonarr databases")
			}
			source, dbEntry = "radarr", f
		case "sonarr.db":
			if source != "" && source != "sonarr" {
				return Backup{}, fmt.Errorf("the backup contains both Radarr and Sonarr databases")
			}
			source, dbEntry = "sonarr", f
		case "radarr.db-wal", "sonarr.db-wal":
			walEntry = f
		case "radarr.db-shm", "sonarr.db-shm":
			shmEntry = f
		}
	}
	if dbEntry == nil {
		return Backup{}, fmt.Errorf("the backup has no radarr.db or sonarr.db")
	}
	dir, err := os.MkdirTemp("", "reelscarey-arr-import-")
	if err != nil {
		return Backup{}, err
	}
	defer os.RemoveAll(dir)
	dbPath := path.Join(dir, path.Base(dbEntry.Name))
	if err = extract(dbEntry, dbPath); err != nil {
		return Backup{}, err
	}
	if walEntry != nil {
		if err = extract(walEntry, dbPath+"-wal"); err != nil {
			return Backup{}, err
		}
	}
	if shmEntry != nil {
		if err = extract(shmEntry, dbPath+"-shm"); err != nil {
			return Backup{}, err
		}
	}
	db, err := database.OpenExisting(ctx, dbPath, false)
	if err != nil {
		return Backup{}, fmt.Errorf("open backup database: %w", err)
	}
	defer db.Close()
	var items []Candidate
	if source == "radarr" {
		items, err = radarrMovies(ctx, db)
	} else {
		items, err = sonarrSeries(ctx, db)
	}
	if err != nil {
		return Backup{}, err
	}
	sort.SliceStable(items, func(i, j int) bool {
		return strings.ToLower(items[i].Title) < strings.ToLower(items[j].Title)
	})
	if len(items) > maxItems {
		return Backup{}, fmt.Errorf("the backup has more than %d titles", maxItems)
	}
	return Backup{Source: source, Items: items}, nil
}

func extract(f *zip.File, destination string) error {
	if f.UncompressedSize64 > maxDatabaseBytes {
		return fmt.Errorf("the backup database is larger than %d MiB", maxDatabaseBytes>>20)
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	written, err := io.Copy(out, io.LimitReader(rc, maxDatabaseBytes+1))
	if err != nil {
		return err
	}
	if written > maxDatabaseBytes {
		return fmt.Errorf("the backup database is larger than %d MiB", maxDatabaseBytes>>20)
	}
	return out.Close()
}

func columns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, kind string
		var dflt sql.NullString
		if err = rows.Scan(&cid, &name, &kind, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

func radarrMovies(ctx context.Context, db *sql.DB) ([]Candidate, error) {
	movieCols, err := columns(ctx, db, "Movies")
	if err != nil {
		return nil, err
	}
	if len(movieCols) == 0 {
		return nil, fmt.Errorf("the Radarr backup has no Movies table")
	}
	tmdbExpr, titleExpr, yearExpr := "m.TmdbId", "m.Title", "m.Year"
	from := "Movies m"
	if !movieCols["TmdbId"] || !movieCols["Title"] || !movieCols["Year"] {
		metaCols, err := columns(ctx, db, "MovieMetadata")
		if err != nil || len(metaCols) == 0 || !movieCols["MovieMetadataId"] {
			return nil, fmt.Errorf("this Radarr backup does not expose movie titles and TMDb IDs")
		}
		if !movieCols["TmdbId"] {
			if !metaCols["TmdbId"] {
				return nil, fmt.Errorf("this Radarr backup does not expose movie TMDb IDs")
			}
			tmdbExpr = "mm.TmdbId"
		}
		if !movieCols["Title"] {
			if !metaCols["Title"] {
				return nil, fmt.Errorf("this Radarr backup does not expose movie titles")
			}
			titleExpr = "mm.Title"
		}
		if !movieCols["Year"] {
			if !metaCols["Year"] {
				return nil, fmt.Errorf("this Radarr backup does not expose movie years")
			}
			yearExpr = "mm.Year"
		}
		from = "Movies m JOIN MovieMetadata mm ON mm.Id=m.MovieMetadataId"
	}
	monitored := "1"
	if movieCols["Monitored"] {
		monitored = "m.Monitored"
	}
	rows, err := db.QueryContext(ctx, "SELECT "+tmdbExpr+","+titleExpr+","+yearExpr+","+monitored+" FROM "+from+" WHERE "+tmdbExpr+">0 ORDER BY "+tmdbExpr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[int64]bool{}
	out := []Candidate{}
	for rows.Next() {
		var tmdb int64
		var title string
		var year sql.NullInt64
		var mon sql.NullInt64
		if err = rows.Scan(&tmdb, &title, &year, &mon); err != nil {
			return nil, err
		}
		title = strings.TrimSpace(title)
		if tmdb < 1 || title == "" || seen[tmdb] {
			continue
		}
		seen[tmdb] = true
		var y *int
		if year.Valid && year.Int64 > 0 && year.Int64 <= 9999 {
			v := int(year.Int64)
			y = &v
		}
		out = append(out, Candidate{Kind: "movie", TMDbID: tmdb, Title: title, Year: y, Monitored: !mon.Valid || mon.Int64 != 0})
		if len(out) > maxItems {
			return nil, fmt.Errorf("the backup has more than %d movies", maxItems)
		}
	}
	return out, rows.Err()
}

func sonarrSeries(ctx context.Context, db *sql.DB) ([]Candidate, error) {
	seriesCols, err := columns(ctx, db, "Series")
	if err != nil {
		return nil, err
	}
	if len(seriesCols) == 0 || !seriesCols["TvdbId"] {
		return nil, fmt.Errorf("the Sonarr backup has no Series table with TVDB IDs")
	}
	monitored := "1"
	if seriesCols["Monitored"] {
		monitored = "Monitored"
	}
	rows, err := db.QueryContext(ctx, "SELECT TvdbId,Title,Year,"+monitored+" FROM Series WHERE TvdbId>0 ORDER BY TvdbId")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[int64]bool{}
	out := []Candidate{}
	for rows.Next() {
		var tvdb int64
		var title string
		var year sql.NullInt64
		var mon sql.NullInt64
		if err = rows.Scan(&tvdb, &title, &year, &mon); err != nil {
			return nil, err
		}
		title = strings.TrimSpace(title)
		if tvdb < 1 || title == "" || seen[tvdb] {
			continue
		}
		seen[tvdb] = true
		var y *int
		if year.Valid && year.Int64 > 0 && year.Int64 <= 9999 {
			v := int(year.Int64)
			y = &v
		}
		out = append(out, Candidate{Kind: "series", TVDBID: tvdb, Title: title, Year: y, Monitored: !mon.Valid || mon.Int64 != 0})
		if len(out) > maxItems {
			return nil, fmt.Errorf("the backup has more than %d series", maxItems)
		}
	}
	return out, rows.Err()
}
