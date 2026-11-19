package transfer

import (
	"context"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/organization"
)

func TestCleanupAllowsCatalogAddsAndReleaseSearch(t *testing.T) {
	f, seriesID, _ := setupTV(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := f.db.Exec(`INSERT INTO organization_plans(id,state,snapshot,created_at,updated_at) VALUES('cleanup','running','{"track_cleanup":{"processed":19},"phase":"planned"}',1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	// Both the durable reservation and the actual filesystem lock are active.
	unlock := organization.WriteMedia()
	defer unlock()
	store := library.New(f.db)
	if _, _, err := store.AddMovie(ctx, metadata.Movie{TMDbID: 98761, Title: "New movie during cleanup"}, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AddSeries(ctx, metadata.Series{Movie: metadata.Movie{TMDbID: 98762, Title: "New show during cleanup"}}, false); err != nil {
		t.Fatal(err)
	}
	if releases, err := f.s.Search(ctx, f.movie.ID, ""); err != nil || len(releases) == 0 {
		t.Fatalf("movie search: %v %v", releases, err)
	}
	if releases, err := f.s.SearchTV(ctx, seriesID, 1, 1, ""); err != nil || len(releases) == 0 {
		t.Fatalf("TV search: %v %v", releases, err)
	}
	job := f.queue(t)
	claimed, err := f.s.claim(ctx)
	if err != nil || claimed.ID != job.ID {
		t.Fatalf("unrelated download could not be queued/claimed: %+v %v", claimed, err)
	}
	if err := organization.EnsureMediaAvailable(ctx, f.db); err == nil {
		t.Fatal("filesystem work lost its safety fence")
	}
}
