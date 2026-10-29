package transfer

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/library"
)

// Real NZBGet, local generated yEnc articles and FFprobe. No owner service,
// credentials, client configuration or media is used by this acceptance test.
func TestRealNZBGetNamedClientMovieAndTVImports(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_NZBGET") != "1" {
		t.Skip("set REELSCAREY_LIVE_NZBGET=1 and REELSCAREY_NZBGET for isolated real-client acceptance")
	}
	binary := os.Getenv("REELSCAREY_NZBGET")
	if binary == "" {
		t.Fatal("REELSCAREY_NZBGET must name the isolated test executable")
	}
	for _, tv := range []bool{false, true} {
		t.Run(map[bool]string{false: "movie", true: "reviewed-TV"}[tv], func(t *testing.T) { runRealNZBGet(t, binary, tv) })
	}
}
func runRealNZBGet(t *testing.T, binary string, tv bool) {
	f, id, targets := setupTV(t)
	if !tv {
		id = f.movie.ID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	generated := filepath.Join(t.TempDir(), "owned.mp4")
	if output, e := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=10", "-t", "2", "-c:v", "libx264", "-pix_fmt", "yuv420p", generated).CombinedOutput(); e != nil {
		t.Fatalf("generate owned video: %v %s", e, output)
	}
	body, e := os.ReadFile(generated)
	if e != nil {
		t.Fatal(e)
	}
	names := []string{"Owned.Movie.mp4"}
	if tv {
		names = []string{"Owned.Show.S01E01-E02.mp4", "Owned.Show.S01E03.mp4"}
	}
	articles := map[string][]byte{}
	nzb := `<?xml version="1.0"?><nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">`
	for i, name := range names {
		mid := fmt.Sprintf("owned-nzbget-%d@reelscarey.invalid", i)
		articles[mid] = encodeOwnedYenc(name, body)
		nzb += fmt.Sprintf(`<file poster="fixture@reelscarey.invalid" date="%d" subject='"%s" yEnc (1/1)'><groups><group>alt.test</group></groups><segments><segment bytes="%d" number="1">%s</segment></segments></file>`, time.Now().Unix(), name, len(articles[mid]), mid)
	}
	nzb += `</nzb>`
	release := make(chan struct{})
	nntp := startOwnedNNTP(t, ctx, articles, release)
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	profile := t.TempDir()
	local := "nzbget-client"
	completed := filepath.Join(f.cfg.DownloadsDir, local)
	if e = os.MkdirAll(completed, 0755); e != nil {
		t.Fatal(e)
	}
	password := ID()
	cfgFile := filepath.Join(profile, "nzbget.conf")
	configuration := fmt.Sprintf(`MainDir=%s
DestDir=%s
InterDir=%s/incomplete
NzbDir=%s/nzb
QueueDir=%s/queue
TempDir=%s/tmp
ScriptDir=%s/scripts
LogFile=%s/nzbget.log
LockFile=%s/nzbget.lock
OutputMode=loggable
ControlIP=127.0.0.1
ControlPort=%d
ControlUsername=fixture
ControlPassword=%s
SecureControl=no
AuthorizedIP=
AppendCategoryDir=no
Extensions=
ScriptOrder=
Category1.Name=reelscarey
Category1.DestDir=%s/ReelScarey
Category1.Unpack=yes
Category1.Extensions=
Server1.Active=yes
Server1.Name=Owned local articles
Server1.Level=0
Server1.Host=127.0.0.1
Server1.Port=%d
Server1.Username=
Server1.Password=
Server1.Encryption=no
Server1.Connections=1
ArticleRetries=1
ArticleTimeout=15
HealthCheck=pause
`, filepath.ToSlash(profile), filepath.ToSlash(completed), filepath.ToSlash(profile), filepath.ToSlash(profile), filepath.ToSlash(profile), filepath.ToSlash(profile), filepath.ToSlash(profile), filepath.ToSlash(profile), filepath.ToSlash(profile), port, password, filepath.ToSlash(completed), nntp.Addr().(*net.TCPAddr).Port)
	if e = os.WriteFile(cfgFile, []byte(configuration), 0600); e != nil {
		t.Fatal(e)
	}
	command := exec.CommandContext(ctx, binary, "-s", "-c", cfgFile)
	var diagnostic bytes.Buffer
	command.Stdout = &diagnostic
	command.Stderr = &diagnostic
	if e = command.Start(); e != nil {
		t.Fatal(e)
	}
	exited := make(chan error, 1)
	go func() { exited <- command.Wait() }()
	defer func() {
		command.Process.Kill()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			t.Error("test NZBGet did not exit")
		}
	}()
	c := ClientConfig{URL: fmt.Sprintf("http://127.0.0.1:%d", port), Username: "fixture", Password: password, DownloadPath: filepath.ToSlash(completed), LocalFolder: local}
	for start := time.Now(); ; {
		e = f.s.nzbget.Test(ctx, c)
		if e == nil {
			break
		}
		if time.Since(start) > 15*time.Second {
			t.Fatal("isolated NZBGet failed to become ready", e)
		}
		select {
		case stopped := <-exited:
			exited <- stopped
			t.Fatal("isolated NZBGet exited", stopped, strings.ReplaceAll(diagnostic.String(), password, "<redacted>"))
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(200 * time.Millisecond):
		}
	}
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
		fmt.Fprintf(w, `<rss><channel><item><title>Owned.NZBGet.Release.1080p</title><guid>owned</guid><enclosure url="http://%s/nzb" length="%d" type="application/x-nzb"/></item></channel></rss>`, r.Host, len(body)*len(names))
	}))
	defer indexer.Close()
	if e = f.s.Remove(ctx, "torznab"); e != nil {
		t.Fatal(e)
	}
	key := "owned-indexer-key"
	if _, e = f.s.SaveIndexer(ctx, "", ManagedIndexerUpdate{Name: "Owned NZBGet Newznab", Kind: "newznab", URL: indexer.URL + "/api", APIKey: &key, Enabled: true}); e != nil {
		t.Fatal(e)
	}
	saved, e := f.s.SaveDownloadClient(ctx, DownloadClientUpdate{Name: "Owned NZBGet", Kind: "nzbget", URL: c.URL, Username: c.Username, Password: &password, DownloadPath: c.DownloadPath, LocalFolder: local})
	if e != nil {
		t.Fatal(e)
	}
	saved, e = f.s.SaveDownloadClient(ctx, DownloadClientUpdate{ID: saved.ID, Name: saved.Name, Kind: saved.Kind, URL: c.URL, Username: c.Username, DownloadPath: c.DownloadPath, LocalFolder: local, Enabled: true, Revision: saved.Revision})
	if e != nil {
		t.Fatal(e)
	}
	defaults, e := f.s.ClientDefaults(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defaults.Usenet = saved.ID
	defaults, e = f.s.SaveClientDefaults(ctx, defaults)
	if e != nil {
		t.Fatal(e)
	}
	job := queueUsenet(t, f, id, tv)
	if job.ClientKey != saved.ID || job.ClientKind != "nzbget" {
		t.Fatal("client binding missing", job)
	}
	// Changing the default after reservation must not redirect this job, including
	// after reopening the application while the server is waiting for its articles.
	defaults.Usenet = ""
	if _, e = f.s.SaveClientDefaults(ctx, defaults); e != nil {
		t.Fatal(e)
	}
	f.s.importer.Prober = FFprobe{}
	f.tick(t)
	f.reopen(t)
	f.s.importer.Prober = FFprobe{}
	close(release)
	for {
		f.tick(t)
		current, e := f.s.Job(ctx, job.ID)
		if e != nil {
			t.Fatal(e)
		}
		if current.State == "completed" {
			break
		}
		if current.ErrorCode == "map_episodes" {
			if !tv || len(current.Choices) != 2 {
				t.Fatal("unexpected TV review", current)
			}
			var count int
			f.db.QueryRow(`SELECT COUNT(*) FROM media_files WHERE download_id=?`, job.ID).Scan(&count)
			if count != 0 {
				t.Fatal("TV import bypassed review")
			}
			if e = f.s.ConfirmTVMapping(ctx, job.ID, TVMappingUpdate{Files: []TVMapping{{FileIndex: 0, EpisodeIDs: []int64{targets[0].EpisodeID, targets[1].EpisodeID}}, {FileIndex: 1, EpisodeIDs: []int64{targets[2].EpisodeID}}}}); e != nil {
				t.Fatal(e)
			}
			f.reopen(t)
			f.s.importer.Prober = FFprobe{}
			continue
		}
		if current.State == "blocked" || current.State == "failed" {
			t.Fatalf("real NZBGet import blocked: %s: %s (%s)", current.ErrorCode, current.ErrorMessage, current.ClientState)
		}
		select {
		case <-ctx.Done():
			t.Fatal("real NZBGet transfer timed out", current.State, current.ClientState)
		case <-time.After(250 * time.Millisecond):
		}
	}
	details, e := f.s.Details(ctx, id, 1)
	if e != nil || details.FileCount != len(names) || details.HistoryCount != 1 {
		t.Fatal(details, e)
	}
	for _, name := range names {
		p := filepath.Join(completed, "ReelScarey", "reelscarey-"+job.ID, name)
		content, e := os.ReadFile(p)
		if e != nil || !bytes.Equal(content, body) {
			t.Fatal("completed NZBGet source changed", e)
		}
	}
	for _, file := range details.Files {
		content, e := os.ReadFile(file.Path)
		if e != nil || !bytes.Equal(content, body) || file.Probe.Width != 320 || file.Probe.Duration < 1.8 {
			t.Fatal("generated imported video invalid", e)
		}
	}
	if tv {
		season, e := library.New(f.db).TVSeason(ctx, id, 1)
		if e != nil || season.Season.AvailableEpisodes != 3 {
			t.Fatal(season, e)
		}
	}
	t.Log("Real NZBGet fetched generated yEnc articles, retained its named client/local-folder binding across default changes and reopen, required TV review and imported FFprobe-verified videos with sources preserved")
}
