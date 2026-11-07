package transfer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Prowlarr exposes a Newznab-compatible endpoint per indexer at
// /<indexer-id>/api. NZBGeek exposes its own Newznab endpoint at /api. Both use
// the generic newznab kind with a path-qualified URL and the saved API key.
func TestProwlarrStyleNewznabEndpointIsSupported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/3/api" || r.URL.Query().Get("apikey") != "prowlarr-key" {
			t.Error("wrong Prowlarr endpoint contract", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		if r.URL.Query().Get("t") == "caps" {
			fmt.Fprint(w, `<caps><searching><search available="yes" supportedParams="q"/></searching></caps>`)
			return
		}
		fmt.Fprintf(w, `<rss xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel><item><title>Usenet fixture</title><guid>one</guid><comments>https://indexer.invalid/details/one</comments><pubDate>Thu, 22 Oct 2026 10:00:00 +0000</pubDate><enclosure type="application/x-nzb" url="http://%s/nzb" length="100"/></item></channel></rss>`, r.Host)
	}))
	defer server.Close()
	out, e := NewNewznab().Search(context.Background(), IndexerConfig{URL: server.URL + "/3/api", APIKey: "prowlarr-key"}, "Usenet fixture")
	if e != nil || len(out) != 1 || out[0].Protocol != "usenet" || out[0].Published != "2026-10-22T10:00:00Z" || out[0].InfoURL != "https://indexer.invalid/details/one" {
		t.Fatal(out, e)
	}
}

func TestNewznabProtocolAndNZBCredentialContainment(t *testing.T) {
	var foreignCalls atomic.Int32
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { foreignCalls.Add(1); fmt.Fprint(w, ownedNZB) }))
	defer foreign.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api":
			if r.URL.Query().Get("apikey") != "private-key" {
				t.Error("missing indexer key")
			}
			if r.URL.Query().Get("t") == "caps" {
				fmt.Fprint(w, `<caps><limits max="7"/><searching><search available="yes" supportedParams="q"/></searching></caps>`)
				return
			}
			if r.URL.Query().Get("q") != "Owned Show S01E01" || r.URL.Query().Get("cat") != "5000" || r.URL.Query().Get("limit") != "7" {
				t.Error("incorrect Newznab request", r.URL.Path)
			}
			fmt.Fprintf(w, `<rss xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel><item><title>Owned</title><guid>one</guid><enclosure type="application/x-nzb" url="http://%s/nzb" length="100"/><newznab:attr name="seeders" value="999"/></item><item><title>Foreign</title><guid>two</guid><enclosure type="application/x-nzb" url="%s/nzb"/></item><item><title>Torrent</title><guid>three</guid><enclosure type="application/x-bittorrent" url="http://%s/nzb"/></item></channel></rss>`, r.Host, foreign.URL, r.Host)
		case "/nzb":
			fmt.Fprint(w, ownedNZB)
		case "/redirect":
			http.Redirect(w, r, foreign.URL+"/nzb", http.StatusFound)
		case "/bad":
			fmt.Fprint(w, `<error code="100" description="private-key"/>`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	n := NewNewznab()
	ctx := context.Background()
	c := IndexerConfig{URL: server.URL + "/api", APIKey: "private-key", Category: "5000"}
	out, e := n.Search(ctx, c, "Owned Show S01E01")
	if e != nil || len(out) != 1 || out[0].Protocol != "usenet" || out[0].Seeders != nil {
		t.Fatal(out, e)
	}
	p, id, e := n.Fetch(ctx, c, out[0])
	if e != nil || string(p.NZB) != ownedNZB || !strings.HasPrefix(id, "nzb:") {
		t.Fatal("invalid NZB normalization", e)
	}
	for _, path := range []string{foreign.URL + "/nzb", server.URL + "/redirect", server.URL + "/bad"} {
		if _, _, e = n.Fetch(ctx, c, Candidate{URL: path}); e == nil || strings.Contains(e.Error(), "private-key") {
			t.Fatal("unsafe NZB retrieval or error", e)
		}
	}
	if foreignCalls.Load() != 0 {
		t.Fatal("foreign credentials request followed")
	}
}
