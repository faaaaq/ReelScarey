package transfer

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSAB is a disposable loopback SABnzbd API. It proves the supervisor's
// readiness check, queue parsing and action forwarding without a provider,
// daemon binary or real download. Lifecycle tests still start a stub process so
// process ownership and shutdown are exercised.
type fakeSAB struct {
	mu       sync.Mutex
	requests []url.Values
	slots    []map[string]any
	queue    map[string]any
	adds     int
	server   *httptest.Server
	port     int
	limit    string
}

func newFakeSAB(t *testing.T) *fakeSAB {
	t.Helper()
	f := &fakeSAB{}
	f.queue = map[string]any{
		"status":          "Downloading",
		"paused":          false,
		"kbpersec":        "2048.0",
		"mb":              "1500",
		"mbleft":          "750",
		"timeleft":        "0:10:00",
		"speedlimit":      "100",
		"speedlimit_abs":  "0",
		"noofslots":       2,
		"noofslots_total": 2,
		"slots": []map[string]any{
			{"nzo_id": "SABnzbd_nzo_aaa111", "filename": "reelscarey-movie", "status": "Downloading", "mb": "1000", "mbleft": "250", "percentage": "75", "timeleft": "0:05:00", "cat": "reelscarey", "priority": "Normal"},
			{"nzo_id": "SABnzbd_nzo_bbb222", "filename": "reelscarey-paused", "status": "Paused", "mb": "500", "mbleft": "500", "percentage": "0", "timeleft": "0:00:00", "cat": "reelscarey", "priority": "Low"},
		},
	}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if r.Method == http.MethodPost {
			if e := r.ParseMultipartForm(2 << 20); e == nil && r.MultipartForm != nil {
				q = url.Values(r.MultipartForm.Value)
			} else if e := r.ParseForm(); e == nil {
				q = r.PostForm
			}
		}
		f.mu.Lock()
		f.requests = append(f.requests, q)
		mode := q.Get("mode")
		name := q.Get("name")
		limit := q.Get("value")
		slots := f.slots
		queue := f.queue
		if mode == "addfile" {
			f.adds++
		}
		if mode == "config" && name == "speedlimit" {
			f.limit = limit
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case mode == "version":
			_, _ = w.Write([]byte(`{"version":"3.7.2"}`))
		case mode == "get_cats":
			_, _ = w.Write([]byte(`{"categories":["*","reelscarey"]}`))
		case mode == "addfile":
			_, _ = w.Write([]byte(`{"status":true,"nzo_ids":["SABnzbd_nzo_new123"]}`))
		case mode == "config" && name == "speedlimit":
			_, _ = w.Write([]byte(`{"status":true}`))
		case mode == "queue" && name != "":
			_, _ = w.Write([]byte(`{"status":true,"nzo_ids":["SABnzbd_nzo_aaa111"]}`))
		case mode == "queue":
			if slots != nil {
				queue = map[string]any{"status": "Downloading", "paused": false, "kbpersec": "2048.0", "mb": "1", "mbleft": "1", "timeleft": "0:00:01", "speedlimit": "100", "speedlimit_abs": "0", "noofslots": len(slots), "noofslots_total": len(slots), "slots": slots}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"queue": queue})
		default:
			_, _ = w.Write([]byte(`{"status":true}`))
		}
	}))
	u, e := url.Parse(f.server.URL)
	if e != nil {
		t.Fatal(e)
	}
	f.port, e = strconv.Atoi(u.Port())
	if e != nil {
		t.Fatal(e)
	}
	old := bundledSABWebPort
	bundledSABWebPort = f.port
	t.Cleanup(func() {
		bundledSABWebPort = old
		f.server.Close()
	})
	return f
}

func (f *fakeSAB) lastRequest(mode string) url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.requests) - 1; i >= 0; i-- {
		if f.requests[i].Get("mode") == mode {
			return f.requests[i]
		}
	}
	return nil
}

func (f *fakeSAB) hasRequest(mode, name string) url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.requests) - 1; i >= 0; i-- {
		if f.requests[i].Get("mode") == mode && f.requests[i].Get("name") == name {
			return f.requests[i]
		}
	}
	return nil
}

func awaitUsenetState(t *testing.T, f *workflowFixture, state string) UsenetBundledStatus {
	t.Helper()
	deadline := time.Now().Add(25 * time.Second)
	for {
		out, e := f.s.BundledUsenetStatus(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		if out.State == state {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatalf("built-in Usenet client never reached %q: %+v", state, out)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func enableUsenetFixture(t *testing.T, f *workflowFixture, _ *fakeSAB) (context.CancelFunc, <-chan struct{}, UsenetBundledStatus) {
	t.Helper()
	t.Setenv("REELSCAREY_BUNDLED_SAB", bundledStubBinary(t))
	password := "fixture-nntp-password"
	if _, e := f.s.SaveNewsServer(context.Background(), NewsServerUpdate{Host: "news.example.com", Port: 563, TLS: true, Username: "fixture", Password: &password, Connections: 8}); e != nil {
		t.Fatal(e)
	}
	before, e := f.s.BundledUsenetStatus(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.s.SuperviseBundledUsenet(ctx, slog.New(slog.DiscardHandler))
	}()
	status, e := f.s.UpdateBundledUsenet(ctx, UsenetBundledUpdate{Revision: before.Revision, Enabled: boolPtr(true)})
	if e != nil {
		cancel()
		<-done
		t.Fatal(e)
	}
	if status.State != "running" || !status.Enabled {
		cancel()
		<-done
		t.Fatalf("built-in Usenet client did not start: %+v", status)
	}
	return cancel, done, status
}

func TestBuiltinUsenetLifecycleAndNewsServer(t *testing.T) {
	f := setupWorkflow(t)
	sab := newFakeSAB(t)
	ctx := context.Background()
	status, e := f.s.BundledUsenetStatus(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if status.Present || status.Enabled || status.Configured {
		t.Fatal("a fresh installation reported a configured Usenet client", status)
	}
	// A disabled row cannot be enabled before a provider exists.
	if _, e = f.s.UpdateBundledUsenet(ctx, UsenetBundledUpdate{Enabled: boolPtr(true)}); e == nil {
		t.Fatal("enabling without a news server succeeded")
	}
	password := "fixture-nntp-password"
	for _, bad := range []NewsServerUpdate{
		{Host: "https://news.example.com", Port: 563, Connections: 8},
		{Host: "news.example.com", Port: 0, Connections: 8},
		{Host: "news.example.com", Port: 563, Connections: 0},
		{Host: "news.example.com", Port: 563, Username: "user", Connections: 8},
	} {
		if _, e = f.s.SaveNewsServer(ctx, bad); e == nil {
			t.Fatalf("invalid news server accepted: %+v", bad)
		}
	}
	status, e = f.s.SaveNewsServer(ctx, NewsServerUpdate{Host: "news.example.com", Port: 563, TLS: true, Username: "fixture", Password: &password, Connections: 8})
	if e != nil {
		t.Fatal(e)
	}
	if !status.Present || status.Enabled || !status.Configured || status.Host != "news.example.com" || status.Port != 563 || !status.TLS || status.Connections != 8 {
		t.Fatal(status)
	}
	encoded, _ := json.Marshal(status)
	if strings.Contains(string(encoded), password) {
		t.Fatal("safe status exposed the NNTP password")
	}
	// Changing the host requires the password again.
	if _, e = f.s.SaveNewsServer(ctx, NewsServerUpdate{Host: "other.example.com", Port: 563, TLS: true, Username: "fixture", Connections: 8}); e == nil {
		t.Fatal("changing the host without re-entering the password succeeded")
	}
	replacement := "replacement-password"
	if _, e = f.s.SaveNewsServer(ctx, NewsServerUpdate{Host: "other.example.com", Port: 563, TLS: true, Username: "fixture", Password: &replacement, Connections: 8}); e != nil {
		t.Fatal(e)
	}

	cancel, done, running := enableUsenetFixture(t, f, sab)
	defer func() {
		cancel()
		<-done
	}()
	if !running.Installed {
		t.Fatal("the stub binary was not reported as installed")
	}
	// Repeating the enable is idempotent and must not wait for a new lifecycle.
	if again, e := f.s.UpdateBundledUsenet(ctx, UsenetBundledUpdate{Revision: running.Revision, Enabled: boolPtr(true)}); e != nil || again.State != "running" {
		t.Fatal("repeating enable waited or failed", again, e)
	}
	ini, e := os.ReadFile(filepath.Join(f.cfg.ConfigDir, "sabnzbd", "sabnzbd.ini"))
	if e != nil {
		t.Fatal(e)
	}
	text := string(ini)
	for _, want := range []string{
		"host = news.example.com",
		"port = 563",
		"connections = 8",
		"ssl = 1",
		"__version__ = 19",
		"[[reelscarey]]",
		"dir = \"\"\"ReelScarey\"\"\"",
		"complete_dir = \"\"\"" + strings.ReplaceAll(filepath.Join(f.cfg.DownloadsDir, builtinUsenetFolder), `\`, `\`) + "\"\"\"",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated SABnzbd configuration lacks %q:\n%s", want, text)
		}
	}
	// The default routing is explicit: enabling does not make it the Usenet
	// default, but the owner can choose it.
	defaults, e := f.s.ClientDefaults(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if defaults.Usenet != "" {
		t.Fatal("enabling the built-in Usenet client silently became the default", defaults)
	}
	updated, e := f.s.SaveClientDefaults(ctx, ClientDefaults{Torrent: defaults.Torrent, Usenet: builtinUsenetClientID, Revision: defaults.Revision})
	if e != nil {
		t.Fatal(e)
	}
	selected, e := f.s.selectedClient(ctx, "usenet")
	if e != nil || selected.profileKey != builtinUsenetClientID || selected.kind != "sabnzbd" {
		t.Fatal("the built-in Usenet client was not routable as the Usenet default", selected, e)
	}
	// Disabling clears the default and never leaves a process behind.
	saved, e := f.s.UpdateBundledUsenet(ctx, UsenetBundledUpdate{Revision: running.Revision, Enabled: boolPtr(false)})
	if e != nil || saved.Enabled || saved.State != "stopped" {
		t.Fatal(saved, e)
	}
	defaults, e = f.s.ClientDefaults(ctx)
	if e != nil || defaults.Usenet != "" || defaults.Revision != updated.Revision+1 {
		t.Fatal("disabling kept the Usenet default", defaults, e)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		status, e = f.s.BundledUsenetStatus(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if status.State == "stopped" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the supervised SABnzbd stub did not stop", status)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func TestBuiltinUsenetMissingBinaryFailsHonestly(t *testing.T) {
	f := setupWorkflow(t)
	newFakeSAB(t)
	t.Setenv("REELSCAREY_BUNDLED_SAB", filepath.Join(t.TempDir(), "missing-sabnzbd"))
	password := "fixture-nntp-password"
	if _, e := f.s.SaveNewsServer(context.Background(), NewsServerUpdate{Host: "news.example.com", Port: 563, Username: "fixture", Password: &password, Connections: 4}); e != nil {
		t.Fatal(e)
	}
	before, e := f.s.BundledUsenetStatus(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.s.SuperviseBundledUsenet(ctx, slog.New(slog.DiscardHandler))
	}()
	if _, e := f.s.UpdateBundledUsenet(ctx, UsenetBundledUpdate{Revision: before.Revision, Enabled: boolPtr(true)}); e == nil {
		t.Fatal("enabling without a SABnzbd binary succeeded")
	}
	status := awaitUsenetState(t, f, "failed")
	if status.Message == "" {
		t.Fatal("a missing daemon was not explained")
	}
	if status.Installed {
		t.Fatal("a missing binary was reported as installed")
	}
	if _, e := f.s.UpdateBundledUsenet(ctx, UsenetBundledUpdate{Revision: status.Revision, Enabled: boolPtr(false)}); e != nil {
		t.Fatal(e)
	}
	cancel()
	<-done
}

func TestBuiltinUsenetQueueAndActions(t *testing.T) {
	f := setupWorkflow(t)
	sab := newFakeSAB(t)
	cancel, done, _ := enableUsenetFixture(t, f, sab)
	defer func() {
		cancel()
		<-done
	}()
	ctx := context.Background()
	overview, e := f.s.UsenetOverview(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if overview.Status.State != "running" || !overview.Queue.Available || len(overview.Queue.Items) != 2 {
		t.Fatalf("queue overview is incomplete: %+v", overview)
	}
	first := overview.Queue.Items[0]
	if first.ID != "SABnzbd_nzo_aaa111" || first.Size != 1000*(1<<20) || first.Remaining != 250*(1<<20) || first.Progress != 0.75 || first.Speed != 2048*(1<<10) || first.ETA != "0:05:00" || first.Category != "reelscarey" {
		t.Fatalf("queue item was parsed incorrectly: %+v", first)
	}
	if overview.Queue.Items[1].Speed != 0 || overview.Queue.Items[1].Progress != 0 {
		t.Fatalf("a paused job reported activity: %+v", overview.Queue.Items[1])
	}
	if overview.Queue.Speed != 2048*(1<<10) || overview.Queue.Remaining != 750*(1<<20) || overview.Queue.LimitPercent != 100 || overview.Queue.Paused {
		t.Fatalf("queue totals were parsed incorrectly: %+v", overview.Queue)
	}
	// Pause and resume forward the selected IDs.
	if e = f.s.UsenetQueueAction(ctx, UsenetQueueAction{IDs: []string{"SABnzbd_nzo_aaa111"}, Action: "pause"}); e != nil {
		t.Fatal(e)
	}
	if req := sab.hasRequest("queue", "pause"); req == nil || req.Get("value") != "SABnzbd_nzo_aaa111" {
		t.Fatal("pause was not forwarded", req)
	}
	if e = f.s.UsenetQueueAction(ctx, UsenetQueueAction{IDs: []string{"SABnzbd_nzo_aaa111"}, Action: "resume"}); e != nil {
		t.Fatal(e)
	}
	if req := sab.hasRequest("queue", "resume"); req == nil {
		t.Fatal("resume was not forwarded")
	}
	// Delete requires review and refuses jobs still tracked by an unfinished
	// ReelScarey download.
	if e = f.s.UsenetQueueAction(ctx, UsenetQueueAction{IDs: []string{"SABnzbd_nzo_aaa111"}, Action: "delete"}); e == nil {
		t.Fatal("delete without review succeeded")
	}
	if _, e = f.db.Exec(`INSERT INTO release_candidates(id,movie_id,title,size_bytes,published_at,payload,expires_at) VALUES('owned-usenet-release',?, 'fixture', 1, '2026-01-01T00:00:00Z', x'00', 0)`, f.movie.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = f.db.Exec(`INSERT INTO downloads(id,movie_id,release_id,title,info_hash,payload,client_url,save_path,state,created_at,updated_at,protocol,client_id) VALUES('owned-usenet-job',?, 'owned-usenet-release', 'fixture', 'nzb:owned', x'00', '', '', 'downloading', 0, 0, 'usenet', 'SABnzbd_nzo_aaa111')`, f.movie.ID); e != nil {
		t.Fatal(e)
	}
	if e = f.s.UsenetQueueAction(ctx, UsenetQueueAction{IDs: []string{"SABnzbd_nzo_aaa111"}, Action: "delete", Reviewed: true, DeleteFiles: true}); e == nil {
		t.Fatal("deleting a tracked ReelScarey job succeeded")
	}
	if _, e = f.db.Exec(`DELETE FROM downloads WHERE id='owned-usenet-job'`); e != nil {
		t.Fatal(e)
	}
	if e = f.s.UsenetQueueAction(ctx, UsenetQueueAction{IDs: []string{"SABnzbd_nzo_aaa111", "SABnzbd_nzo_bbb222"}, Action: "delete", Reviewed: true, DeleteFiles: true}); e != nil {
		t.Fatal(e)
	}
	req := sab.hasRequest("queue", "delete")
	if req == nil || req.Get("del_files") != "1" || req.Get("value") != "SABnzbd_nzo_aaa111,SABnzbd_nzo_bbb222" {
		t.Fatal("delete was not forwarded with its reviewed file choice", req)
	}
	// Manual NZBs use the same dedicated category and are not linked to a job.
	if _, e = f.s.AddManualNZB(ctx, []byte(ownedNZB)); e != nil {
		t.Fatal(e)
	}
	if req := sab.lastRequest("addfile"); req == nil || req.Get("cat") != sabCategory || req.Get("nzbname") == "" {
		t.Fatal("manual NZB did not use the dedicated category", req)
	}
	if _, e = f.s.AddManualNZB(ctx, []byte("<html/>")); e == nil {
		t.Fatal("an invalid NZB was accepted")
	}
	// Speed limits are absolute bytes per second; zero restores the client
	// default without sending SABnzbd's percentage range.
	limit := int64(4 << 20)
	if e = f.s.SetUsenetSpeedLimit(ctx, UsenetSpeedLimitUpdate{DownloadLimit: &limit}); e != nil {
		t.Fatal(e)
	}
	if req := sab.hasRequest("config", "speedlimit"); req == nil || req.Get("value") != "4194304" {
		t.Fatal("speed limit was not forwarded", req)
	}
	zero := int64(0)
	if e = f.s.SetUsenetSpeedLimit(ctx, UsenetSpeedLimitUpdate{DownloadLimit: &zero}); e != nil {
		t.Fatal(e)
	}
	if req := sab.hasRequest("config", "speedlimit"); req == nil || req.Get("value") != "" {
		t.Fatal("clearing the speed limit was not forwarded", req)
	}
	tooSmall := int64(50)
	if e = f.s.SetUsenetSpeedLimit(ctx, UsenetSpeedLimitUpdate{DownloadLimit: &tooSmall}); e == nil {
		t.Fatal("an ambiguous percentage-range limit was accepted")
	}
}

func TestBuiltinUsenetQueueUnavailableStates(t *testing.T) {
	f := setupWorkflow(t)
	newFakeSAB(t)
	ctx := context.Background()
	q, e := f.s.UsenetQueue(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if q.Available || q.State != "disabled" || q.Message == "" || len(q.Items) != 0 {
		t.Fatal(q)
	}
	password := "fixture-nntp-password"
	if _, e = f.s.SaveNewsServer(ctx, NewsServerUpdate{Host: "news.example.com", Port: 563, Username: "fixture", Password: &password, Connections: 4}); e != nil {
		t.Fatal(e)
	}
	q, e = f.s.UsenetQueue(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if q.Available || q.State != "disabled" || !strings.Contains(q.Message, "not enabled") {
		t.Fatal(q)
	}
	overview, e := f.s.UsenetOverview(ctx)
	if e != nil || overview.Status.Configured != true || overview.Status.Enabled {
		t.Fatal(overview, e)
	}
}

func TestSABProcessOutputRedactsSecrets(t *testing.T) {
	c := ClientConfig{APIKey: "generated-api-key", NewsPassword: "nntp-secret"}
	got := redactSABText("failed key=generated-api-key password=nntp-secret", c)
	if strings.Contains(got, "generated-api-key") || strings.Contains(got, "nntp-secret") {
		t.Fatal(got)
	}
	if !strings.Contains(got, "[redacted]") {
		t.Fatal(got)
	}
}

func TestNewsServerTestReportsReachability(t *testing.T) {
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	if e := TestNewsServer(context.Background(), "127.0.0.1", port); e != nil {
		t.Fatal(e)
	}
	if e := TestNewsServer(context.Background(), "127.0.0.1", 1); e == nil {
		t.Fatal("an unreachable news server was reported as reachable")
	}
	if e := TestNewsServer(context.Background(), "https://news.example.com", 563); e == nil {
		t.Fatal("an invalid host was tested")
	}
}
