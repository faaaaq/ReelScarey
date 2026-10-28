package metadata

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLiveWikidataFranchise(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_WIKIDATA") != "1" {
		t.Skip("set REELSCAREY_LIVE_WIKIDATA=1 for public provider acceptance")
	}
	c := NewWikidataFranchises()
	for _, v := range []struct {
		kind string
		id   int64
	}{{"movie", 11}, {"series", 105971}} {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		out, e := c.FindFranchise(ctx, v.kind, v.id)
		cancel()
		if e != nil {
			t.Fatal(v, e)
		}
		movie, series := false, false
		for _, item := range out.Items {
			movie = movie || item.Kind == "movie"
			series = series || item.Kind == "series"
		}
		if !movie || !series {
			t.Fatal("missing cross-media members", v, len(out.Items), out.Truncated)
		}
		t.Logf("%s: %d members, movies and shows", v.kind, len(out.Items))
	}
}
