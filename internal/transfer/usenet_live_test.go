package transfer

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"hash/crc32"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/library"
)

// This is a real, isolated SABnzbd process talking to a local NNTP fixture.
// Its only articles are videos generated here. No indexer/provider account or
// external NNTP service is used; it never opens the owner's SABnzbd profile.
func TestRealSABnzbdMovieAndTVImports(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_SAB") != "1" {
		t.Skip("set REELSCAREY_LIVE_SAB=1 and REELSCAREY_SABNZBD to a disposable SABnzbd executable/script")
	}
	if runtime.GOOS == "linux" {
		for _, binary := range []string{"par2", "unrar"} {
			if _, e := exec.LookPath(binary); e != nil {
				t.Fatalf("SABnzbd requires its real %s runtime tool: %v", binary, e)
			}
		}
	}
	for _, tv := range []bool{false, true} {
		t.Run(map[bool]string{false: "movie", true: "reviewed-TV"}[tv], func(t *testing.T) { runRealSAB(t, tv) })
	}
}
func TestRealSABnzbdNamedClientMovieAndTVImports(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_SAB") != "1" {
		t.Skip("set REELSCAREY_LIVE_SAB=1 for isolated real-client acceptance")
	}
	for _, tv := range []bool{false, true} {
		t.Run(map[bool]string{false: "movie", true: "reviewed-TV"}[tv], func(t *testing.T) { runRealSAB(t, tv, true) })
	}
}
func runRealSAB(t *testing.T, tv bool, named ...bool) {
	t.Helper()
	f, id, targets := setupTV(t)
	if !tv {
		id = f.movie.ID
	}
	// SABnzbd can spend a full 60-second connection interval establishing its
	// first NNTP connection. Leave bounded time for post-processing and review.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	generated := filepath.Join(t.TempDir(), "owned.mp4")
	if output, e := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=10", "-t", "2", "-c:v", "libx264", "-pix_fmt", "yuv420p", generated).CombinedOutput(); e != nil {
		t.Fatalf("generate owned video: %v %s", e, output)
	}
	data, e := os.ReadFile(generated)
	if e != nil {
		t.Fatal(e)
	}
	names := []string{"Owned.Movie.mp4"}
	if tv {
		names = []string{"Owned.Show.S01E01-E02.mp4", "Owned.Show.S01E03.mp4"}
		if len(named) > 0 && named[0] {
			names = []string{"Owned.Show.S01 E01 - E02.mp4", "Owned.Show.Season 1 Ep 3.mp4"}
		}
	}
	articles := map[string][]byte{}
	nzb := `<?xml version="1.0"?><nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">`
	for i, n := range names {
		mid := fmt.Sprintf("owned-%d@reelscarey.invalid", i)
		articles[mid] = encodeOwnedYenc(n, data)
		nzb += fmt.Sprintf(`<file poster="fixture@reelscarey.invalid" date="%d" subject='"%s" yEnc (1/1)'><groups><group>alt.test</group></groups><segments><segment bytes="%d" number="1">%s</segment></segments></file>`, time.Now().Unix(), n, len(articles[mid]), mid)
	}
	nzb += `</nzb>`
	releaseArticles := make(chan struct{})
	nntp := startOwnedNNTP(t, ctx, articles, releaseArticles)
	apiListener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := apiListener.Addr().(*net.TCPAddr).Port
	apiListener.Close()
	profile := t.TempDir()
	completed := f.cfg.DownloadsDir
	local := ""
	managed := len(named) > 0 && named[0]
	if managed {
		local = "named-sab"
		completed = filepath.Join(completed, local)
		if e = os.MkdirAll(completed, 0755); e != nil {
			t.Fatal(e)
		}
	}
	key := ID()
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		b, err := os.ReadFile(filepath.Join(profile, "logs", "sabnzbd.log"))
		if err == nil {
			if len(b) > 8000 {
				b = b[len(b)-8000:]
			}
			safe := regexp.MustCompile(`[a-fA-F0-9]{32}`).ReplaceAllString(string(b), "<redacted>")
			t.Logf("Isolated SABnzbd diagnostic tail:\n%s", safe)
		}
	})
	configFile := filepath.Join(profile, "sabnzbd.ini")
	ini := fmt.Sprintf(`__version__ = 19
[misc]
host = 127.0.0.1
port = %d
api_key = %s
nzb_key = %s
auto_browser = 0
check_new_rel = 0
api_logging = 0
enable_https = 0
inet_exposure = 0
host_whitelist = 127.0.0.1, localhost
download_dir = %s
complete_dir = %s
admin_dir = %s
log_dir = %s
pause_on_post_processing = 0
enable_recursive = 0
pre_check = 0
[categories]
[[reelscarey]]
name = reelscarey
dir = ReelScarey
script = None
pp = 2
[servers]
[[owned-fixture]]
name = owned-fixture
host = 127.0.0.1
port = %d
connections = 1
ssl = 0
enable = 1
retention = 0
`, port, key, ID(), filepath.ToSlash(filepath.Join(profile, "incomplete")), filepath.ToSlash(completed), filepath.ToSlash(filepath.Join(profile, "admin")), filepath.ToSlash(filepath.Join(profile, "logs")), nntp.Addr().(*net.TCPAddr).Port)
	if e = os.WriteFile(configFile, []byte(ini), 0600); e != nil {
		t.Fatal(e)
	}
	binary := os.Getenv("REELSCAREY_SABNZBD")
	if binary == "" {
		t.Fatal("REELSCAREY_SABNZBD must name the installed test client")
	}
	args := []string{"--config-file", configFile, "--browser", "0"}
	if strings.HasSuffix(binary, ".py") {
		python := os.Getenv("REELSCAREY_SABNZBD_PYTHON")
		if python == "" {
			python = "python3"
		}
		args = append([]string{binary}, args...)
		binary = python
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	defer func() {
		_ = cmd.Process.Kill()
		select {
		case <-exited:
		case <-time.After(10 * time.Second):
			t.Error("test SABnzbd did not exit")
		}
	}()
	c := ClientConfig{URL: fmt.Sprintf("http://127.0.0.1:%d", port), APIKey: key, DownloadPath: filepath.ToSlash(completed), LocalFolder: local}
	for start := time.Now(); ; {
		if e = f.s.sab.Test(ctx, c); e == nil {
			break
		}
		if time.Since(start) > 30*time.Second {
			t.Fatal("isolated SABnzbd did not become ready", e)
		}
		select {
		case err := <-exited:
			exited <- err
			t.Fatal("isolated SABnzbd exited", err)
		case <-time.After(250 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	// Newznab is an independently authored protocol fixture; SABnzbd, NNTP
	// article retrieval/yEnc assembly and FFprobe are real integration boundaries.
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/nzb" {
			fmt.Fprint(w, nzb)
			return
		}
		if r.URL.Path != "/api" || r.URL.Query().Get("apikey") != "owned-indexer-key" {
			w.WriteHeader(403)
			return
		}
		if r.URL.Query().Get("t") == "caps" {
			fmt.Fprint(w, `<caps><searching><search available="yes" supportedParams="q"/></searching></caps>`)
			return
		}
		fmt.Fprintf(w, `<rss><channel><item><title>Owned.Generated.Release.1080p</title><guid>owned</guid><enclosure url="http://%s/nzb" length="%d" type="application/x-nzb"/></item></channel></rss>`, r.Host, len(data)*len(names))
	}))
	defer indexer.Close()
	if e = f.s.Remove(ctx, "torznab"); e != nil {
		t.Fatal(e)
	}
	ikey := "owned-indexer-key"
	if _, e = f.s.SaveIndexer(ctx, "", ManagedIndexerUpdate{Name: "Owned Newznab fixture", Kind: "newznab", URL: indexer.URL + "/api", APIKey: &ikey, Enabled: true}); e != nil {
		t.Fatal(e)
	}
	var defaults ClientDefaults
	if managed {
		saved, err := f.s.SaveDownloadClient(ctx, DownloadClientUpdate{Name: "Owned SAB", Kind: "sabnzbd", URL: c.URL, APIKey: &key, DownloadPath: c.DownloadPath, LocalFolder: local})
		if err != nil {
			t.Fatal(err)
		}
		saved, err = f.s.SaveDownloadClient(ctx, DownloadClientUpdate{ID: saved.ID, Name: saved.Name, Kind: saved.Kind, URL: c.URL, DownloadPath: c.DownloadPath, LocalFolder: local, Enabled: true, Revision: saved.Revision})
		if err != nil {
			t.Fatal(err)
		}
		defaults, err = f.s.ClientDefaults(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defaults.Usenet = saved.ID
		defaults, err = f.s.SaveClientDefaults(ctx, defaults)
		if err != nil {
			t.Fatal(err)
		}
	} else if e = f.s.ConfigureSAB(ctx, SABUpdate{URL: c.URL, APIKey: &key, DownloadPath: c.DownloadPath}); e != nil {
		t.Fatal(e)
	}
	j := queueUsenet(t, f, id, tv)
	if managed {
		if j.ClientKey != defaults.Usenet || j.ClientKind != "sabnzbd" {
			t.Fatal("missing named-client binding", j)
		}
		defaults.Usenet = ""
		if _, e = f.s.SaveClientDefaults(ctx, defaults); e != nil {
			t.Fatal(e)
		}
	}
	f.s.importer.Prober = FFprobe{}
	f.tick(t)
	f.reopen(t)
	f.s.importer.Prober = FFprobe{}
	close(releaseArticles)
	for {
		f.tick(t)
		current, e := f.s.Job(ctx, j.ID)
		if e != nil {
			t.Fatal(e)
		}
		if current.State == "completed" {
			break
		}
		if current.ErrorCode == "map_episodes" {
			if !tv || len(current.Choices) != 2 {
				t.Fatal("unexpected TV review")
			}
			if len(current.Choices[0].Suggestions) != 2 || len(current.Choices[1].Suggestions) != 1 {
				t.Fatal("reviewed filename hints missing", current.Choices)
			}
			var count int
			if e = f.db.QueryRow(`SELECT COUNT(*) FROM media_files WHERE download_id=?`, j.ID).Scan(&count); e != nil || count != 0 {
				t.Fatal("TV imported without review")
			}
			if e = f.s.ConfirmTVMapping(ctx, j.ID, TVMappingUpdate{Files: []TVMapping{{FileIndex: 0, EpisodeIDs: []int64{targets[0].EpisodeID, targets[1].EpisodeID}}, {FileIndex: 1, EpisodeIDs: []int64{targets[2].EpisodeID}}}}); e != nil {
				t.Fatal(e)
			}
			f.reopen(t)
			f.s.importer.Prober = FFprobe{}
			continue
		}
		if current.State == "blocked" || current.State == "failed" {
			remote, err := f.s.sab.Find(ctx, c, current.ClientID, "reelscarey-"+j.ID)
			if err == nil && remote != nil {
				t.Logf("Owned SABnzbd final storage: %q", remote.Storage)
			}
			t.Fatalf("real SABnzbd import blocked: %s: %s (client %s)", current.ErrorCode, current.ErrorMessage, current.ClientState)
		}
		select {
		case <-ctx.Done():
			t.Fatal("real SABnzbd transfer timed out", current.State, current.ClientState)
		case <-time.After(300 * time.Millisecond):
		}
	}
	details, e := f.s.Details(ctx, id, 1)
	if e != nil || details.FileCount != len(names) || details.HistoryCount != 1 {
		t.Fatal("real import details", e)
	}
	for _, n := range names {
		source := filepath.Join(completed, "ReelScarey", "reelscarey-"+j.ID, n)
		body, e := os.ReadFile(source)
		if e != nil || !bytes.Equal(body, data) {
			t.Fatal("real completed source was changed", e)
		}
	}
	for _, file := range details.Files {
		body, e := os.ReadFile(file.Path)
		if e != nil || !bytes.Equal(body, data) || file.Probe.Width != 320 || file.Probe.Duration < 1.8 {
			t.Fatal("real media verification failed", e)
		}
	}
	if tv {
		season, e := library.New(f.db).TVSeason(ctx, id, 1)
		if e != nil || season.Season.AvailableEpisodes != 3 {
			t.Fatal("real TV availability", e)
		}
	}
	t.Log("Real SABnzbd downloaded locally generated yEnc articles, survived application reopen, and imported verified videos while retaining completed sources")
}

func encodeOwnedYenc(name string, data []byte) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "=ybegin line=128 size=%d name=%s\r\n", len(data), name)
	column := 0
	for _, v := range data {
		c := v + 42
		if c == 0 || c == 10 || c == 13 || c == 61 || c == 9 || c == 32 {
			b.WriteByte('=')
			b.WriteByte(c + 64)
			column += 2
		} else {
			b.WriteByte(c)
			column++
		}
		if column >= 126 {
			b.WriteString("\r\n")
			column = 0
		}
	}
	if column > 0 {
		b.WriteString("\r\n")
	}
	fmt.Fprintf(&b, "=yend size=%d crc32=%08x\r\n", len(data), crc32.ChecksumIEEE(data))
	return b.Bytes()
}
func startOwnedNNTP(t *testing.T, ctx context.Context, articles map[string][]byte, release <-chan struct{}) net.Listener {
	t.Helper()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	connections := map[net.Conn]bool{}
	closed := false
	go func() {
		for {
			conn, e := listener.Accept()
			if e != nil {
				return
			}
			mu.Lock()
			if closed {
				conn.Close()
				mu.Unlock()
				return
			}
			connections[conn] = true
			wg.Add(1)
			mu.Unlock()
			go func() {
				defer wg.Done()
				defer conn.Close()
				defer func() { mu.Lock(); delete(connections, conn); mu.Unlock() }()
				r := textproto.NewReader(bufio.NewReader(conn))
				w := textproto.NewWriter(bufio.NewWriter(conn))
				w.PrintfLine("200 ReelScarey owned fixture ready")
				for {
					line, e := r.ReadLine()
					if e != nil {
						return
					}
					words := strings.Fields(line)
					if len(words) == 0 {
						return
					}
					command := strings.ToUpper(words[0])
					t.Logf("Owned NNTP command: %s", command)
					id := ""
					if len(words) > 1 {
						id = strings.Trim(words[1], "<>")
					}
					switch command {
					case "CAPABILITIES":
						w.PrintfLine("101 capabilities")
						dot := w.DotWriter()
						io.WriteString(dot, "VERSION 2\r\nREADER\r\n")
						dot.Close()
					case "MODE":
						w.PrintfLine("200 reader mode")
					case "AUTHINFO":
						w.PrintfLine("281 authentication accepted")
					case "GROUP":
						w.PrintfLine("211 %d 1 %d alt.test", len(articles), len(articles))
					case "DATE":
						w.PrintfLine("111 %s", time.Now().UTC().Format("20060102150405"))
					case "STAT":
						if _, ok := articles[id]; !ok {
							w.PrintfLine("430 no owned article")
						} else {
							w.PrintfLine("223 0 <%s>", id)
						}
					case "BODY", "ARTICLE", "HEAD":
						body, ok := articles[id]
						if !ok {
							w.PrintfLine("430 no owned article")
							continue
						}
						if command != "HEAD" {
							select {
							case <-release:
							case <-ctx.Done():
								return
							}
						}
						codes := map[string]int{"BODY": 222, "ARTICLE": 220, "HEAD": 221}
						w.PrintfLine("%d 0 <%s>", codes[command], id)
						dot := w.DotWriter()
						if command != "BODY" {
							fmt.Fprintf(dot, "From: owned@reelscarey.invalid\r\nSubject: owned generated video\r\nMessage-ID: <%s>\r\nNewsgroups: alt.test\r\nDate: %s\r\n\r\n", id, time.Now().UTC().Format(time.RFC1123Z))
						}
						if command != "HEAD" {
							dot.Write(body)
						}
						dot.Close()
					case "QUIT":
						w.PrintfLine("205 goodbye")
						return
					default:
						w.PrintfLine("500 unsupported fixture command %s", strconv.Quote(command))
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		mu.Lock()
		closed = true
		listener.Close()
		for c := range connections {
			c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	return listener
}
