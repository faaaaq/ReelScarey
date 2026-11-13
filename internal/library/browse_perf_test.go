package library

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/metadata"
)

func seedBrowseCatalog(t *testing.T, db *sql.DB, n int) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 1; i <= n; i++ {
		title := fmt.Sprintf("Browse Title %06d", i)
		if i%7 == 0 {
			title = "The " + title
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO media_items(id,kind,tmdb_id,title,year,status,category) VALUES(?,'movie',?,?,?,'available','standard')`, i, i, title, 2020+i%30); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// The default browse title sort must be served by the title_sort index
// (migration 099) instead of a temp B-tree over the whole catalog, and its
// results must stay identical to the original expression ordering (RS-0001).
func TestBrowseTitleSortQueryPlanAndBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("browse perf test skipped in -short mode")
	}
	ctx := context.Background()
	db, e := database.Open(ctx, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	const n = 10000
	seedBrowseCatalog(t, db, n)

	// Query plan: the representative default browse query must avoid a temp
	// B-tree sort and use the title_sort index.
	query := `SELECT id FROM media_items LEFT JOIN item_automation a ON a.media_id=media_items.id
		WHERE kind='movie' AND title LIKE ? ESCAPE '\' AND (?='all' OR status=?) AND (?='all' OR monitored=?)
		ORDER BY title_sort COLLATE NOCASE, id LIMIT ? OFFSET ?`
	plan, e := collectQueryPlan(ctx, db, query, "%%", "all", "available", "all", false, 48, 0)
	if e != nil {
		t.Fatal(e)
	}
	joined := strings.Join(plan, " | ")
	t.Logf("EQP default browse: %s", joined)
	if strings.Contains(joined, "TEMP B-TREE") {
		t.Fatalf("title sort still materializes a temp B-tree: %s", joined)
	}
	if !strings.Contains(joined, "media_items_title_sort") {
		t.Fatalf("title sort does not use the title_sort index: %s", joined)
	}

	// The generated column must order exactly like the original expression.
	var viaColumn, viaExpr []int64
	rows, e := db.QueryContext(ctx, `SELECT id FROM media_items ORDER BY title_sort COLLATE NOCASE, id LIMIT 50`)
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var id int64
		if e := rows.Scan(&id); e != nil {
			t.Fatal(e)
		}
		viaColumn = append(viaColumn, id)
	}
	rows.Close()
	rows, e = db.QueryContext(ctx, `SELECT id FROM media_items ORDER BY (CASE WHEN lower(title) LIKE 'the %' THEN substr(title,5) ELSE title END) COLLATE NOCASE, id LIMIT 50`)
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var id int64
		if e := rows.Scan(&id); e != nil {
			t.Fatal(e)
		}
		viaExpr = append(viaExpr, id)
	}
	rows.Close()
	for i := range viaColumn {
		if viaColumn[i] != viaExpr[i] {
			t.Fatalf("generated column ordering diverges from the expression at %d: %d vs %d", i, viaColumn[i], viaExpr[i])
		}
	}

	// Latency budget: repeated page views stay far below the audited
	// unindexed cost.
	var samples []time.Duration
	for i := 0; i < 20; i++ {
		start := time.Now()
		if _, e := New(db).Browse(ctx, BrowseFilter{Kind: "movie", Page: i%50 + 1}); e != nil {
			t.Fatal(e)
		}
		samples = append(samples, time.Since(start))
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	median := samples[len(samples)/2]
	t.Logf("browse 10k titles: median page view %v (min %v, max %v)", median, samples[0], samples[len(samples)-1])
	if median > 50*time.Millisecond {
		t.Fatalf("browse page view too slow at 10k titles: median %v", median)
	}
}

func collectQueryPlan(ctx context.Context, db *sql.DB, query string, args ...any) ([]string, error) {
	rows, err := db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id, parent int64
		var notUsed, detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			return nil, err
		}
		out = append(out, detail)
	}
	return out, rows.Err()
}

func benchmarkBrowse(b *testing.B, n int) {
	ctx := context.Background()
	db, err := database.Open(ctx, b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		b.Fatal(err)
	}
	for i := 1; i <= n; i++ {
		title := fmt.Sprintf("Browse Title %06d", i)
		if i%7 == 0 {
			title = "The " + title
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO media_items(id,kind,tmdb_id,title,year,status,category) VALUES(?,'movie',?,?,?,'available','standard')`, i, i, title, 2020+i%30); err != nil {
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	store := New(db)
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		if _, err := store.Browse(ctx, BrowseFilter{Kind: "movie", Page: i%50 + 1}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBrowseTitle10k(b *testing.B) { benchmarkBrowse(b, 10000) }
func BenchmarkBrowseTitle50k(b *testing.B) { benchmarkBrowse(b, 50000) }

// Compile-time guard that the metadata poster helper stays wired.
var _ = metadata.PosterURL
