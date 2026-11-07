package transfer

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testHash = "0123456789abcdef0123456789abcdef01234567"
const testMagnet = "magnet:?xt=urn:btih:" + testHash + "&tr=https%3A%2F%2Ftracker.invalid%2Fprivate-passkey"

func TestTorznabContractAndCredentialContainment(t *testing.T) {
	foreignCalls := 0
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { foreignCalls++ }))
	defer foreign.Close()
	var base string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apikey") != "test-key" {
			t.Error("missing API key")
		}
		switch r.URL.Query().Get("t") {
		case "caps":
			fmt.Fprint(w, `<caps><searching><search available="yes" supportedParams="q"/></searching></caps>`)
		case "search":
			if r.URL.Query().Get("q") != "A & B 2026" || r.URL.Query().Get("cat") != "2000" || r.URL.Query().Get("extended") != "1" {
				t.Error("incorrect search contract")
			}
			fmt.Fprintf(w, `<rss xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel><item><title>A &amp; B.2026</title><guid>one</guid><comments>https://tracker.invalid/details/one</comments><pubDate>Thu, 22 Oct 2026 10:00:00 +0000</pubDate><enclosure url="%s/download?apikey=test-key" length="123"/><torznab:attr name="seeders" value="0"/><torznab:attr name="peers" value="4"/><torznab:attr name="magneturl" value="magnet:?xt=urn:btih:%s"/></item><item><title>foreign</title><link>%s/bad</link></item></channel></rss>`, base, testHash, foreign.URL)
		}
	}))
	defer server.Close()
	base = server.URL
	adapter := NewTorznab()
	config := IndexerConfig{URL: base + "/api", APIKey: "test-key", Generation: "generation"}
	if e := adapter.Test(context.Background(), config); e != nil {
		t.Fatal(e)
	}
	out, e := adapter.Search(context.Background(), config, "A & B 2026")
	if e != nil || len(out) != 1 {
		t.Fatal(out, e)
	}
	if out[0].Seeders == nil || *out[0].Seeders != 0 || out[0].Size != 123 || out[0].Published != "2026-10-22T10:00:00Z" || out[0].InfoURL != "https://tracker.invalid/details/one" {
		t.Fatal(out[0])
	}
	public, _ := json.Marshal(out[0].Release)
	if strings.Contains(string(public), "test-key") || strings.Contains(string(public), "magnet:") {
		t.Fatal("private release transport exposed")
	}
	payload, h, e := adapter.Fetch(context.Background(), config, out[0])
	if e != nil || h != testHash || payload.Magnet == "" {
		t.Fatal(e)
	}
	_, _, e = adapter.Fetch(context.Background(), config, Candidate{URL: foreign.URL + "/steal?apikey=test-key"})
	if e == nil || foreignCalls != 0 {
		t.Fatal("cross-origin credential fetch permitted")
	}
}
func TestTorznabErrorsAndRedirects(t *testing.T) {
	for _, body := range []string{`<error code="100" description="private-key"/>`, `<html>private-key</html>`, `<caps><searching><search available="no"/></searching></caps>`} {
		t.Run(body[:10], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
			defer server.Close()
			e := NewTorznab().Test(context.Background(), IndexerConfig{URL: server.URL, APIKey: "private-key"})
			if e == nil || strings.Contains(e.Error(), "private-key") {
				t.Fatal("unsafe or accepted error", e)
			}
		})
	}
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redirect.Close()
	if e := NewTorznab().Test(context.Background(), IndexerConfig{URL: redirect.URL, APIKey: "private"}); e == nil || called {
		t.Fatal("indexer redirect followed")
	}
}
func TestTorrentIdentityAndBounds(t *testing.T) {
	info := []byte("d6:lengthi3e4:name9:movie.mkv12:piece lengthi16384e6:pieces20:01234567890123456789e")
	body := append(append([]byte("d4:info"), info...), 'e')
	want := sha1.Sum(info)
	got, e := TorrentHash(body)
	if e != nil || got != hex.EncodeToString(want[:]) {
		t.Fatal(got, e)
	}
	h, e := MagnetHash(testMagnet)
	if e != nil || h != testHash {
		t.Fatal(h, e)
	}
	for _, bad := range []string{"", `d4:infod12:meta versioni2eee`, "d4:infoleee", "d4:infode4:infodee", strings.Repeat("l", 40) + strings.Repeat("e", 40)} {
		if _, e := TorrentHash([]byte(bad)); e == nil {
			t.Fatalf("accepted malformed torrent %q", bad)
		}
	}
	for _, bad := range []string{"magnet:?xt=urn:btmh:1220dead", "magnet:?xt=urn:btih:bad", testMagnet + "&xt=urn:btih:ffffffffffffffffffffffffffffffffffffffff"} {
		if _, e := MagnetHash(bad); e == nil {
			t.Fatal("accepted unsupported magnet")
		}
	}
}
func TestQBittorrentAPIContract(t *testing.T) {
	adds, logins := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "http://"+r.Host || r.Header.Get("Referer") != "http://"+r.Host+"/" {
			t.Error("missing qbt origin")
		}
		if r.URL.Path == "/api/v2/auth/login" {
			_ = r.ParseForm()
			if r.Form.Get("username") != "testadmin" || r.Form.Get("password") != "test-password" {
				t.Error("wrong login")
			}
			logins++
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test-session", Path: "/"})
			fmt.Fprint(w, "Ok.")
			return
		}
		if c, e := r.Cookie("SID"); e != nil || c.Value != "test-session" {
			t.Error("missing SID")
		}
		switch r.URL.Path {
		case "/api/v2/app/webapiVersion":
			fmt.Fprint(w, "2.8.3")
		case "/api/v2/torrents/info":
			if r.URL.Query().Get("hashes") != testHash {
				t.Error("hash missing")
			}
			fmt.Fprintf(w, `[{"hash":%q,"state":"downloading","tags":"reelscarey-job"}]`, testHash)
		case "/api/v2/torrents/files":
			fmt.Fprint(w, `[{"index":2,"name":"movie.mkv","size":100,"progress":1,"priority":1}]`)
		case "/api/v2/torrents/add":
			adds++
			if e := r.ParseMultipartForm(5 << 20); e != nil {
				t.Error(e)
			}
			if r.FormValue("tags") != "reelscarey-job" || r.FormValue("savepath") != "/downloads/ReelScarey/job" || r.FormValue("autoTMM") != "false" || r.FormValue("skip_checking") != "false" {
				t.Error("unsafe add options")
			}
			f, _, e := r.FormFile("torrents")
			if e != nil {
				t.Error(e)
			} else {
				data, _ := io.ReadAll(f)
				f.Close()
				if string(data) != "torrent fixture" {
					t.Error("wrong metainfo")
				}
			}
			fmt.Fprint(w, "Ok.")
		default:
			t.Error("unexpected endpoint", r.URL.Path)
		}
	}))
	defer server.Close()
	q := NewQBittorrent()
	c := ClientConfig{URL: server.URL, Username: "testadmin", Password: "test-password"}
	ctx := context.Background()
	if _, e := q.Test(ctx, c); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Find(ctx, c, testHash); e != nil {
		t.Fatal(e)
	}
	files, e := q.Files(ctx, c, testHash)
	if e != nil || len(files) != 1 || files[0].Index != 2 {
		t.Fatal(files, e)
	}
	if e = q.Add(ctx, c, "reelscarey-job", "/downloads/ReelScarey/job", Payload{Torrent: []byte("torrent fixture")}); e != nil {
		t.Fatal(e)
	}
	if adds != 1 || logins != 1 {
		t.Fatal("unexpected API retries or session churn", adds, logins)
	}
}
func TestPathMappingRejectsTraversal(t *testing.T) {
	for _, v := range []struct{ save, file, remote, want string }{{"/downloads/ReelScarey/job", "folder/movie.mkv", "/downloads", "ReelScarey/job/folder/movie.mkv"}, {`D:\Downloads\ReelScarey\job`, `movie.mkv`, `d:\Downloads`, "ReelScarey/job/movie.mkv"}} {
		out, e := MapSource(v.save, v.file, v.remote)
		if e != nil || out != v.want {
			t.Fatal(out, e)
		}
	}
	for _, v := range []struct{ save, file string }{{"/downloads-other/job", "movie.mkv"}, {"/downloads/job", "../movie.mkv"}, {"/downloads/job", "/etc/movie.mkv"}, {"/downloads/job", "x:stream.mkv"}, {"/downloads/../outside", "movie.mkv"}, {"/downloads/job", `folder\..\movie.mkv`}} {
		if _, e := MapSource(v.save, v.file, "/downloads"); e == nil {
			t.Fatal("unsafe mapping accepted", v)
		}
	}
}

func TestQBittorrentModernNoContentRequiresSession(t *testing.T) {
	for _, withCookie := range []bool{true, false} {
		t.Run(fmt.Sprint(withCookie), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v2/auth/login":
					if withCookie {
						http.SetCookie(w, &http.Cookie{Name: "QBT_SID_8080", Value: "modern-test-session", Path: "/"})
					}
					w.WriteHeader(http.StatusNoContent)
				case "/api/v2/app/webapiVersion":
					fmt.Fprint(w, "2.12.0")
				case "/api/v2/torrents/add":
					w.WriteHeader(http.StatusNoContent)
				default:
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			q := NewQBittorrent()
			c := ClientConfig{URL: server.URL, Username: "testadmin", Password: "test-password"}
			_, e := q.Test(context.Background(), c)
			if !withCookie {
				if e == nil {
					t.Fatal("empty unauthenticated response accepted")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if e = q.Add(context.Background(), c, "reelscarey-test", "/downloads/test", Payload{Magnet: testMagnet}); e != nil {
				t.Fatal("modern empty add response rejected", e)
			}
		})
	}
}

func TestQBittorrentModernAddResults(t *testing.T) {
	for _, body := range []string{`{"success_count":1,"failure_count":0,"pending_count":0,"added_torrent_ids":[]}`, `{"success_count":0,"failure_count":0,"pending_count":1,"added_torrent_ids":[]}`, `{"success_count":0,"failure_count":1,"pending_count":0}`, `<html>not an API response</html>`} {
		t.Run(body[:10], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v2/auth/login" {
					http.SetCookie(w, &http.Cookie{Name: "QBT_SID_8080", Value: "modern-test-session", Path: "/"})
					w.WriteHeader(204)
					return
				}
				w.WriteHeader(202)
				fmt.Fprint(w, body)
			}))
			defer server.Close()
			q := NewQBittorrent()
			e := q.Add(context.Background(), ClientConfig{URL: server.URL, Username: "testadmin", Password: "test-password"}, "reelscarey-test", "/downloads/test", Payload{Magnet: testMagnet})
			want := strings.Contains(body, `"success_count":1`) || strings.Contains(body, `"pending_count":1`)
			if (e == nil) != want {
				t.Fatal("unexpected modern add result", e)
			}
		})
	}
}
