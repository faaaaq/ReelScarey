package library

import (
	"context"
	"fmt"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"sync"
	"testing"
)

func TestConcurrentAddAndPagination(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	store := New(db)
	var wg sync.WaitGroup
	created := make(chan bool, 2)
	failures := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			_, added, err := store.AddMovie(ctx, metadata.Movie{TMDbID: 11, Title: "A test movie", Overview: "Persisted metadata", PosterPath: "/poster.jpg"}, false)
			created <- added
			failures <- err
		})
	}
	wg.Wait()
	close(created)
	close(failures)
	count := 0
	for value := range created {
		if value {
			count++
		}
	}
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if count != 1 {
		t.Fatal("duplicate adds created multiple rows")
	}
	for i := 1; i < 25; i++ {
		if _, _, err := store.AddMovie(ctx, metadata.Movie{TMDbID: int64(i + 100), Title: fmt.Sprintf("Movie %03d", i)}, true); err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.List(ctx, "movie", 1)
	if err != nil || len(page.Items) != 24 || page.Total != 25 {
		t.Fatalf("bad first page: %+v %v", page, err)
	}
	page, err = store.List(ctx, "movie", 2)
	if err != nil || len(page.Items) != 1 {
		t.Fatal("second page incorrect")
	}
	db.Close()
	db, err = database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	item, err := New(db).Movie(ctx, 11)
	if err != nil || item.Overview != "Persisted metadata" || item.Monitored || item.Status != "missing" || item.PosterURL == "" {
		t.Fatalf("metadata did not persist: %+v %v", item, err)
	}
}
