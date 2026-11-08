package transfer

import (
	"context"
	"database/sql"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func BenchmarkSavedAutomaticTitle(b *testing.B) {
	ctx := context.Background()
	db, err := database.Open(ctx, b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`WITH RECURSIVE ids(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM ids WHERE i<10000)
		INSERT INTO media_items(kind,tmdb_id,title,year) SELECT 'movie',i,'Fixture Title '||i,2020 FROM ids`); err != nil {
		b.Fatal(err)
	}
	service := &Service{db: db}
	hint := automaticHint{Title: "Fixture Title 9999", Year: 2020}
	rows, err := service.savedAutomaticTitles(ctx, "movie", hint)
	if err != nil {
		b.Fatal(err)
	}
	rows.Close()
	for _, indexed := range []bool{false, true} {
		name := "catalog_scan"
		if indexed {
			name = "indexed"
		}
		b.Run(name, func(b *testing.B) {
			for b.Loop() {
				var rows *sql.Rows
				var err error
				if indexed {
					rows, err = service.savedAutomaticTitles(ctx, "movie", hint)
				} else {
					rows, err = db.QueryContext(ctx, `SELECT id,tmdb_id,title,year FROM media_items WHERE kind=? AND (?=0 OR tmdb_id=?) AND (?=0 OR year=?)`,
						"movie", hint.ProviderID, hint.ProviderID, hint.Year, hint.Year)
				}
				if err != nil {
					b.Fatal(err)
				}
				matches := 0
				for rows.Next() {
					var id, provider int64
					var title string
					var year *int
					if err := rows.Scan(&id, &provider, &title, &year); err != nil {
						b.Fatal(err)
					}
					if hint.matches(title, year, provider) {
						matches++
					}
				}
				rows.Close()
				if err := rows.Err(); err != nil || matches != 1 {
					b.Fatal(matches, err)
				}
			}
		})
	}
}
