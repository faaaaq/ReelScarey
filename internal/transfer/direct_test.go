package transfer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// Independently authored protocol fixtures contain no tracker account data.
func trackerPage(kind, origin string) string {
	if kind == "iptorrents" {
		return `<a href="/lout.php">Log out</a><table id="torrents"><tr><th>Name</th><th>Size</th><th title="Sort by seeders">S</th><th title="Sort by leechers">L</th></tr><tr><td><a class="hv" href="/t/1">Test.Movie.2026.1080p</a><span class="free">Free</span><a href="` + origin + `/download.php/1/test.torrent">Download</a></td><td>1.5 GiB</td><td>12</td><td>3</td><td>2026-10-22 10:00:00</td></tr></table>`
	}
	return `<a href="logout.php">Log out</a><table id="sortabletable"><tr><td>Movie</td><td><a href="details.php?id=1">Test.Movie.2026.1080p</a><img alt="Free Torrent"><a href="` + origin + `/download.php?id=1">Download</a></td><td>1</td><td>today</td><td>1.5 GiB</td><td>2</td><td>12</td><td>3</td><td>2026-10-22 10:00:00</td></tr></table>`
}

func TestDirectPrivateTrackerProtocols(t *testing.T) {
	for _, kind := range []string{"iptorrents", "xspeeds"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Cookie") != "session=fixture-only" || r.UserAgent() != "Fixture browser" {
					t.Error("session/agent missing")
				}
				if kind == "iptorrents" {
					if r.Method != "GET" || r.URL.Path != "/t" || r.URL.Query().Get("q") != "Test Movie 2026" || !r.URL.Query().Has("73") || r.URL.Query().Get("qf") != "ti" || r.URL.Query().Get("free") != "on" {
						t.Error("wrong IPT search contract")
					}
				} else {
					_ = r.ParseForm()
					if r.Method != "POST" || r.URL.Path != "/browse.php" || r.PostForm.Get("keywords") != "Test Movie 2026" || r.PostForm.Get("do") != "search" || r.PostForm.Get("search_type") != "t_name" || r.PostForm.Get("sort") != "free" {
						t.Error("wrong XS search contract")
					}
				}
				fmt.Fprint(w, trackerPage(kind, ""))
			}))
			defer server.Close()
			d := NewDirect(kind)
			d.HTTP = server.Client()
			c := IndexerConfig{URL: server.URL, Cookie: "session=fixture-only", UserAgent: "Fixture browser", Category: "5000", FreeleechOnly: true, Generation: "one"}
			for range 2 {
				items, e := d.Search(context.Background(), c, "Test Movie 2026")
				if e != nil || len(items) != 1 {
					t.Fatal(items, e)
				}
				v := items[0]
				wantInfo := server.URL + "/details.php?id=1"
				if kind == "iptorrents" {
					wantInfo = server.URL + "/t/1"
				}
				if v.Size != 1610612736 || v.Seeders == nil || *v.Seeders != 12 || v.Peers == nil || *v.Peers != 15 || !v.Freeleech || !strings.HasPrefix(v.URL, server.URL+"/download.php") || v.InfoURL != wantInfo || v.Published != "2026-10-22T10:00:00Z" {
					t.Fatalf("bad normalization: %+v", v)
				}
			}
			if calls != 1 {
				t.Fatal("cached search repeated network request")
			}
		})
	}
}

func TestDirectRejectsChallengesForeignLinksAndMalformedFeeds(t *testing.T) {
	base, _ := url.Parse("https://tracker.invalid")
	for _, kind := range []string{"iptorrents", "xspeeds"} {
		if _, e := parseTrackerHTML([]byte(`<title>Sign in</title>`), base, kind, false); e == nil {
			t.Fatal("challenge accepted")
		}
		if _, e := parseTrackerHTML([]byte(`<a href="/logout.php">out</a>`), base, kind, false); e == nil {
			t.Fatal("unknown layout accepted")
		}
		v, e := parseTrackerHTML([]byte(trackerPage(kind, "https://elsewhere.invalid")), base, kind, false)
		if e != nil || len(v) != 0 {
			t.Fatal("foreign download accepted", e)
		}
		v, e = parseTrackerHTML([]byte(strings.ReplaceAll(trackerPage(kind, ""), "Free Torrent", "Silver Torrent")), base, kind, true)
		if kind == "xspeeds" && (e != nil || len(v) != 0) {
			t.Fatal("silver torrent marked free")
		}
	}
	for _, body := range []string{`<html>login</html>`, `<rss/>`, `<rss><channel>`} {
		if _, e := parseDirectRSS([]byte(body), base); e == nil {
			t.Fatal("invalid feed accepted")
		}
	}
	for _, raw := range []string{"https://other.invalid/download.php", "//other.invalid/file.torrent", "javascript:alert(1)", "https://user:pass@tracker.invalid/file"} {
		if _, _, ok := releaseLink(base, raw); ok {
			t.Fatal("unsafe link accepted")
		}
	}
}

func TestNYAAAndPrivateRSSNormalization(t *testing.T) {
	base, _ := url.Parse("https://nyaa.si")
	feed := `<rss xmlns:nyaa="https://nyaa.si/xmlns/nyaa"><channel><item><title>Fixture S01E01</title><link>https://nyaa.si/download/1.torrent</link><guid>https://nyaa.si/view/1</guid><nyaa:size>700 MiB</nyaa:size><nyaa:seeders>8</nyaa:seeders><nyaa:leechers>2</nyaa:leechers><nyaa:infoHash>` + testHash + `</nyaa:infoHash><pubDate>Fri, 23 Oct 2026 00:00:00 +0000</pubDate></item><item><title>duplicate</title><link>/download/1.torrent</link></item><item><title>foreign</title><link>https://other.invalid/a.torrent</link></item></channel></rss>`
	items, e := parseDirectRSS([]byte(feed), base)
	if e != nil || len(items) != 1 || items[0].Hash != testHash || items[0].Size != 734003200 || *items[0].Peers != 10 || items[0].Published != "2026-10-23T00:00:00Z" || items[0].InfoURL != "https://nyaa.si/view/1" {
		t.Fatal(items, e)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("page") != "rss" || r.URL.Query().Get("q") != "Fixture" || r.Header.Get("Cookie") != "" {
			t.Error("wrong NYAA contract")
		}
		fmt.Fprint(w, `<rss><channel/></rss>`)
	}))
	defer server.Close()
	d := NewDirect("nyaa")
	if _, e = d.Search(context.Background(), IndexerConfig{URL: server.URL}, "Fixture"); e != nil || calls != 1 {
		t.Fatal(e)
	}
}

func TestDirectMetainfoIdentityAndSessionContainment(t *testing.T) {
	body := []byte("d4:infod6:lengthi3e4:name9:movie.mkv12:piece lengthi16384e6:pieces20:01234567890123456789ee")
	hash, e := TorrentHash(body)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "session=fixture" {
			t.Error("missing download session")
		}
		_, _ = w.Write(body)
	}))
	defer server.Close()
	for _, want := range []string{hash, testHash} {
		d := NewDirect("iptorrents")
		payload, got, e := d.Fetch(context.Background(), IndexerConfig{URL: server.URL, Cookie: "session=fixture"}, Candidate{URL: server.URL + "/download.php/1", Hash: want})
		if want == hash {
			if e != nil || got != hash || string(payload.Torrent) != string(body) {
				t.Fatal("metainfo identity failed", e)
			}
		} else if e == nil {
			t.Fatal("changed metainfo accepted")
		}
	}
}

func TestDirectRedirectAndCancellationNeverForwardCredentials(t *testing.T) {
	foreignCalls := 0
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { foreignCalls++ }))
	defer foreign.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, foreign.URL, 302) }))
	defer server.Close()
	d := NewDirect("rss")
	if e := d.Test(context.Background(), IndexerConfig{URL: server.URL, FeedURL: server.URL + "/rss?passkey=fixture", Cookie: "private=fixture"}); e == nil || strings.Contains(e.Error(), "passkey") {
		t.Fatal("unsafe redirect error", e)
	}
	if foreignCalls != 0 {
		t.Fatal("credentials followed redirect")
	}
	d.next = time.Now().Add(time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := d.Search(ctx, IndexerConfig{URL: server.URL, FeedURL: server.URL + "/rss"}, ""); e == nil {
		t.Fatal("cancelled request succeeded")
	}
}

func TestLiveNYAAReadOnly(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_NYAA") != "1" {
		t.Skip("opt-in public RSS search; never downloads torrents")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	d := NewDirect("nyaa")
	transport := d.HTTP.Transport
	d.HTTP.Transport = publicDiagnosticTransport{transport, t}
	items, e := d.Search(ctx, IndexerConfig{URL: "https://nyaa.si"}, "S01E01")
	if e != nil {
		t.Fatal(e)
	}
	if len(items) == 0 {
		t.Fatal("live feed returned no matching entries")
	}
	t.Logf("Live NYAA search parsed %d releases; torrent retrieval was not invoked", len(items))
}

// Only used against the fixed public URL above, never with owner credentials.
type publicDiagnosticTransport struct {
	transport http.RoundTripper
	t         *testing.T
}

func (d publicDiagnosticTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	out, e := d.transport.RoundTrip(r)
	if e != nil {
		d.t.Logf("Public NYAA network diagnostic: %v", e)
	}
	return out, e
}

func TestDirectSearchStripsTitlePunctuationForTrackerQueries(t *testing.T) {
	var got string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("q")
		fmt.Fprint(w, trackerPage("iptorrents", ""))
	}))
	defer server.Close()
	d := NewDirect("iptorrents")
	d.HTTP = server.Client()
	c := IndexerConfig{URL: server.URL, Cookie: "session=fixture-only", UserAgent: "Fixture browser", Category: "5000", Generation: "punctuation"}
	items, e := d.Search(context.Background(), c, "Test Movie 2026?")
	if e != nil || len(items) != 1 {
		t.Fatalf("a punctuated title returned no releases: %v %v", items, e)
	}
	if got != "Test Movie 2026" {
		t.Fatalf("tracker received %q; punctuation should be stripped", got)
	}
}

func TestNormalizeSearchTextMatchesPunctuatedTitles(t *testing.T) {
	for _, tc := range []struct{ query, want string }{
		{"Am I the Asshole? S02E02", "am i the asshole s02e02"},
		{"Jimmy Carr's Show", "jimmy carrs show"},
		{"Spider-Man: No Way Home", "spider man no way home"},
	} {
		if got := normalizeSearchText(tc.query); got != tc.want {
			t.Fatalf("normalize(%q) = %q, want %q", tc.query, got, tc.want)
		}
	}
}
