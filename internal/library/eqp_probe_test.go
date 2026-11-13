package library

import (
	"context"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

// Records the query plan for the query-filtered browse view so the remaining
// cost (leading-wildcard LIKE) is documented, not guessed (RS-0001 follow-up).
func TestBrowseFilteredQueryPlanRecorded(t *testing.T) {
	if testing.Short() {
		t.Skip("skip in -short mode")
	}
	ctx := context.Background()
	db, e := database.Open(ctx, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	seedBrowseCatalog(t, db, 3000)
	query := `SELECT id FROM media_items LEFT JOIN item_automation a ON a.media_id=media_items.id
		WHERE kind='movie' AND title LIKE ? ESCAPE '\' AND (?='all' OR status=?) AND (?='all' OR monitored=?)
		ORDER BY title_sort COLLATE NOCASE, id LIMIT ? OFFSET ?`
	plan, e := collectQueryPlan(ctx, db, query, "%6000%", "all", "available", "all", false, 48, 0)
	if e != nil {
		t.Fatal(e)
	}
	for _, line := range plan {
		t.Logf("EQP filtered browse: %s", line)
	}
	if len(plan) == 0 {
		t.Fatal("no plan rows")
	}
}
