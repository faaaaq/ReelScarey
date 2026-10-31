package metadata

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

const kitsuFixture = `{"id":"1","type":"anime","attributes":{"canonicalTitle":"Cowboy Bebop","titles":{"en_jp":"Cowboy Bebop","ja_jp":"カウボーイビバップ"},"subtype":"TV","startDate":"1998-04-03","episodeCount":26}}`

func TestKitsuPublicProtocolAndInvalidIdentity(t *testing.T) {
	ctx := context.Background()
	mode := "search"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.api+json" || r.Header.Get("Authorization") != "" {
			t.Error("unexpected headers")
		}
		switch mode {
		case "search":
			if r.URL.Query().Get("filter[text]") != "Cowboy Bebop" || r.URL.Query().Get("page[limit]") != "10" {
				t.Error("query")
			}
			fmt.Fprint(w, `{"data":[`+kitsuFixture+`]}`)
		case "identity":
			fmt.Fprint(w, `{"data":`+kitsuFixture+`}`)
		case "limit":
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(429)
		case "redirect":
			http.Redirect(w, r, "http://not-contacted.invalid", 302)
		default:
			fmt.Fprint(w, `{"data":null}`)
		}
	}))
	defer server.Close()
	k := newKitsu(server.URL, server.Client())
	items, e := k.SearchAnime(ctx, "Cowboy Bebop")
	if e != nil || len(items) != 1 || items[0].Provider != "kitsu" || items[0].URL != "https://kitsu.io/anime/1" {
		t.Fatal(items, e)
	}
	mode = "identity"
	k.next = time.Time{}
	if _, e = k.Anime(ctx, 2); e == nil {
		t.Fatal("foreign identity accepted")
	}
	for _, v := range []string{"limit", "redirect", "null"} {
		mode = v
		k.next = time.Time{}
		if _, e = k.Anime(ctx, 1); e == nil {
			t.Fatal(v)
		}
	}
	if _, e = k.SearchAnime(ctx, "bad\x00query"); e == nil {
		t.Fatal("bad query accepted")
	}
}
func TestLiveKitsuIdentity(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_KITSU") != "1" {
		t.Skip("public read-only Kitsu check is opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	k := NewKitsu()
	v, e := k.Anime(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	items, e := k.SearchAnime(ctx, "Cowboy Bebop")
	if e != nil {
		t.Fatal(e)
	}
	for _, item := range items {
		if item.ID == v.ID && item.Provider == "kitsu" {
			t.Log("Public identity and title search passed without credentials or owner media")
			return
		}
	}
	t.Fatal("identity absent from search")
}
