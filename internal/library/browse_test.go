package library

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func TestBrowseFiltersStableSortsAndReadOnly(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, e := database.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	for _, v := range []struct {
		title, kind, status string
		year                any
		monitored           bool
	}{
		{"Alpha 100%_\\", "movie", "missing", 2020, true}, {"alpha", "movie", "available", 2024, false}, {"Beta", "movie", "downloading", nil, true}, {"Alpha 100x", "movie", "missing", 2000, true}, {"Alpha", "series", "available", 2022, true},
	} {
		if _, e = db.Exec(`INSERT INTO media_items(kind,tmdb_id,title,year,status,monitored) VALUES(?,(SELECT COALESCE(MAX(tmdb_id),0)+1 FROM media_items),?,?,?,?)`, v.kind, v.title, v.year, v.status, v.monitored); e != nil {
			t.Fatal(e)
		}
	}
	store := New(db)
	var changes int
	db.QueryRow(`SELECT total_changes()`).Scan(&changes)
	for _, tc := range []struct {
		f     BrowseFilter
		ids   string
		total int
	}{
		{BrowseFilter{Kind: "movie", Page: 1}, "2,1,4,3", 4},
		{BrowseFilter{Kind: "movie", Page: 1, Query: `100%_\`}, "1", 1},
		{BrowseFilter{Kind: "movie", Page: 1, Query: "ALPHA", Status: "missing", Monitored: "monitored"}, "1,4", 2},
		{BrowseFilter{Kind: "movie", Page: 1, Monitored: "paused"}, "2", 1},
		{BrowseFilter{Kind: "movie", Page: 1, Sort: "year_desc"}, "2,1,4,3", 4},
		{BrowseFilter{Kind: "movie", Page: 1, Sort: "year"}, "4,1,2,3", 4},
		{BrowseFilter{Kind: "movie", Page: 1, Sort: "title_desc"}, "3,4,1,2", 4},
		{BrowseFilter{Kind: "movie", Page: 1, Sort: "added"}, "4,3,2,1", 4},
		{BrowseFilter{Kind: "series", Page: 1, Status: "available"}, "5", 1},
	} {
		out, e := store.Browse(ctx, tc.f)
		if e != nil {
			t.Fatal(e)
		}
		ids := []string{}
		for _, i := range out.Items {
			ids = append(ids, fmt.Sprint(i.ID))
		}
		if strings.Join(ids, ",") != tc.ids || out.Total != tc.total {
			t.Fatalf("%+v: %+v %v", tc.f, out, e)
		}
		if tc.f.Kind == "movie" && out.CollectionTotal != 4 {
			t.Fatal("lost collection count")
		}
	}
	for i := 0; i < 54; i++ {
		if _, e = db.Exec(`INSERT INTO media_items(kind,tmdb_id,title) VALUES('movie',?, 'Repeated title')`, 100+i); e != nil {
			t.Fatal(e)
		}
	}
	f := BrowseFilter{Kind: "movie", Page: 1, Query: "Repeated"}
	first, e := store.Browse(ctx, f)
	if e != nil || len(first.Items) != 48 || first.Total != 54 {
		t.Fatal(first, e)
	}
	f.Page = 2
	second, e := store.Browse(ctx, f)
	if e != nil || len(second.Items) != 6 || second.Items[0].ID <= first.Items[47].ID {
		t.Fatal(second, e)
	}
	allFilter := f
	allFilter.Page, allFilter.All = 1, true
	all, e := store.Browse(ctx, allFilter)
	if e != nil || len(all.Items) != 54 || all.Items[48].ID != second.Items[0].ID {
		t.Fatal(all, e)
	}
	db.QueryRow(`SELECT total_changes()`).Scan(&changes)
	_, e = store.Browse(ctx, f)
	var after int
	db.QueryRow(`SELECT total_changes()`).Scan(&after)
	if e != nil || changes != after {
		t.Fatal("browse wrote to database", e)
	}
	db.Close()
	db, e = database.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	out, e := New(db).Browse(ctx, f)
	if e != nil || out.Total != 54 || out.Items[0].ID != second.Items[0].ID {
		t.Fatal("browse changed on reopen", e)
	}
}

func TestBrowseRejectsUnboundedOrUnrecognizedFilters(t *testing.T) {
	db, e := database.Open(context.Background(), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	for _, f := range []BrowseFilter{{Kind: "all", Page: 1}, {Kind: "movie", Page: 0}, {Kind: "movie", Page: 100001}, {Kind: "movie", Page: 1, Sort: "id; DROP TABLE media_items"}, {Kind: "movie", Page: 1, Status: "failed"}, {Kind: "movie", Page: 1, Monitored: "yes"}, {Kind: "movie", Page: 1, Query: strings.Repeat("x", 201)}, {Kind: "movie", Page: 1, Query: "line\nbreak"}, {Kind: "movie", Page: 1, Query: string([]byte{0xff})}} {
		if _, e = New(db).Browse(context.Background(), f); !errors.Is(e, ErrBrowseInput) {
			t.Fatal("accepted invalid filters", f, e)
		}
	}
}
