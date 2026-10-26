package metadata

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestLiveAniListIdentity(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_ANILIST") != "1" {
		t.Skip("set REELSCAREY_LIVE_ANILIST=1 for a public, read-only AniList identity check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	provider := NewAniList()
	provider.http.Transport = animeLiveDiagnostic{http.DefaultTransport, t}
	identity, e := provider.Anime(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	if identity.ID != 1 || identity.Provider != "anilist" || identity.Title == "" || identity.URL != "https://anilist.co/anime/1" {
		t.Fatal("invalid real provider identity", identity)
	}
	matches, e := provider.SearchAnime(ctx, "Cowboy Bebop")
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, v := range matches {
		if v.ID == identity.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("public identity missing from bounded title search")
	}
	t.Log("PASS: fixed public AniList Anime identity and title search verified without credentials; no owner catalog or media was used")
}

type animeLiveDiagnostic struct {
	http.RoundTripper
	t *testing.T
}

func (d animeLiveDiagnostic) RoundTrip(r *http.Request) (*http.Response, error) {
	out, e := d.RoundTripper.RoundTrip(r)
	if e != nil {
		d.t.Logf("Fixed public AniList network diagnostic: %v", e)
	}
	return out, e
}
