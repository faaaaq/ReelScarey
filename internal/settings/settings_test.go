package settings

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func TestValidation(t *testing.T) {
	badNaming := Update{LibraryName: "Cinema", MovieFolder: "Movies", SeriesFolder: "TV", EpisodeNaming: "../../unsafe", Revision: 1}
	if badNaming.Validate() == nil {
		t.Fatal("invalid TV naming accepted")
	}
	for _, value := range []string{"../Movies", "/media/Movies", `C:\Movies`, ".", "..", "CON", "com1", "NUL", "Movies/Other", "", strings.Repeat("A", 81)} {
		t.Run(value, func(t *testing.T) {
			in := Update{LibraryName: "Cinema", MovieFolder: value, SeriesFolder: "TV", Revision: 1}
			if in.Validate() == nil {
				t.Fatal("unsafe folder accepted")
			}
		})
	}
	for _, name := range []string{"", "\t\n", "A\x00B", strings.Repeat("A", 81)} {
		in := Update{LibraryName: name, MovieFolder: "Movies", SeriesFolder: "TV", Revision: 1}
		if in.Validate() == nil {
			t.Fatalf("invalid library name accepted: %q", name)
		}
	}
	in := Update{LibraryName: " Cinema ", MovieFolder: " Movies ", SeriesFolder: "movies", Revision: 1}
	if in.Validate() == nil {
		t.Fatal("case-insensitive folder collision accepted")
	}
}

func TestEpisodeNamingPersistsAcrossLegacyUpdates(t *testing.T) {
	ctx := context.Background()
	db, e := database.Open(ctx, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := New(db)
	out, e := s.Update(ctx, Update{LibraryName: "Cinema", MovieFolder: "Movies", SeriesFolder: "TV", EpisodeNaming: "with-title", Revision: 1})
	if e != nil || out.EpisodeNaming != "with-title" {
		t.Fatal(out, e)
	}
	out, e = s.Update(ctx, Update{LibraryName: "Cinema", MovieFolder: "Movies", SeriesFolder: "TV", Revision: out.Revision})
	if e != nil || out.EpisodeNaming != "with-title" {
		t.Fatal("omitted naming reset existing preference", out, e)
	}
}

func TestConcurrentUpdateAndRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	store := New(db)
	in := Update{LibraryName: " Cinema ", MovieFolder: "Movies", SeriesFolder: "TV", Revision: 1}
	var wg sync.WaitGroup
	errorsSeen := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, err := store.Update(ctx, in); errorsSeen <- err })
	}
	wg.Wait()
	close(errorsSeen)
	successes, conflicts := 0, 0
	for err := range errorsSeen {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("lost-update protection failed: %d successes, %d conflicts", successes, conflicts)
	}
	db.Close()
	db, err = database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	out, err := New(db).Get(ctx)
	if err != nil || out.LibraryName != "Cinema" || out.Revision != 2 {
		t.Fatalf("settings not persisted: %+v %v", out, err)
	}
}
