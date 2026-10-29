package organization

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMediaServerPresetsCarryDistinctCanonicalProviderTags(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	year := 2026
	for _, tc := range []struct{ preset, tag string }{{"plex", "{tmdb-123}"}, {"jellyfin", "[tmdbid-123]"}, {"emby", "[tmdbid=123]"}} {
		t.Run(tc.preset, func(t *testing.T) {
			c, e := f.s.Get(ctx)
			if e != nil {
				t.Fatal(e)
			}
			c.Libraries[0].Preset = tc.preset
			c, e = f.s.Update(ctx, c)
			if e != nil {
				t.Fatal(e)
			}
			name, e := Destination(Snapshot{Library: c.Libraries[0], Content: Content{Kind: "movie", Title: "Example", Year: &year, TMDbID: 123}}, nil, "owned.mkv")
			if e != nil || strings.Count(name, tc.tag) != 2 || !strings.HasSuffix(name, ".mkv") {
				t.Fatal("server provider tag missing or extension changed", name, e)
			}
		})
	}
}

func TestOrganizationCopyFallbackPublishesBeforeRemovingOldLibraryPath(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	source := "Movies/Original/film.mkv"
	body := []byte(strings.Repeat("owned bytes for copy fallback ", 10000))
	f.movie(t, 1, source, body)
	f.s.link = func(*os.Root, string, string) error { return errors.New("test cross-device hardlink unavailable") }
	p, e := f.s.Preview(ctx, []int64{1}, "anime")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Apply(ctx, p.ID); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	v, e := f.s.Plan(ctx, p.ID)
	if e != nil || v.State != "completed" {
		t.Fatal(v.State, e)
	}
	data, e := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(p.Files[0].Destination)))
	if e != nil || string(data) != string(body) {
		t.Fatal("copied publication changed bytes", e)
	}
	if _, e = os.Stat(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(source))); !os.IsNotExist(e) {
		t.Fatal("verified copied file did not finish source cleanup", e)
	}
}
