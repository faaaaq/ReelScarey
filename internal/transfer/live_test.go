package transfer

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"golang.org/x/crypto/pbkdf2"
)

// Opt-in only: starts its own authenticated localhost qBittorrent with an
// isolated profile. It never connects to an owner's existing client/indexer.
func TestRealQBittorrentDownloadAndImport(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_QBT") != "1" {
		t.Skip("set REELSCAREY_LIVE_QBT=1 with qbittorrent-nox and FFmpeg installed")
	}
	if runtime.GOOS != "linux" {
		t.Skip("headless Linux integration")
	}
	for _, binary := range []string{"qbittorrent-nox", "ffmpeg", "ffprobe"} {
		if _, e := exec.LookPath(binary); e != nil {
			t.Fatalf("required integration binary %s: %v", binary, e)
		}
	}
	f := setupWorkflow(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	profile := t.TempDir()
	configPath := filepath.Join(profile, "qBittorrent", "config")
	if e := os.MkdirAll(configPath, 0700); e != nil {
		t.Fatal(e)
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	password := ID()
	salt := []byte(ID()[:16])
	key := pbkdf2.Key([]byte(password), salt, 100000, 64, sha512.New)
	encoded := base64.StdEncoding.EncodeToString(salt) + ":" + base64.StdEncoding.EncodeToString(key)
	ini := fmt.Sprintf("[LegalNotice]\nAccepted=true\n[Preferences]\nWebUI\\Address=127.0.0.1\nWebUI\\Port=%d\nWebUI\\Username=ciadmin\nWebUI\\Password_PBKDF2=\"@ByteArray(%s)\"\nWebUI\\LocalHostAuth=true\nWebUI\\CSRFProtection=true\nWebUI\\HostHeaderValidation=true\n[BitTorrent]\nSession\\DHTEnabled=false\nSession\\LSDEnabled=false\nSession\\PeXEnabled=false\nSession\\Port=0\n", port, encoded)
	if e = os.WriteFile(filepath.Join(configPath, "qBittorrent.conf"), []byte(ini), 0600); e != nil {
		t.Fatal(e)
	}
	cmd := exec.CommandContext(ctx, "qbittorrent-nox", "--profile="+profile, "--webui-port="+strconv.Itoa(port), "--confirm-legal-notice")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() {
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}()
	qbt := NewQBittorrent()
	client := ClientConfig{URL: fmt.Sprintf("http://127.0.0.1:%d", port), Username: "ciadmin", Password: password, DownloadPath: filepath.ToSlash(f.cfg.DownloadsDir)}
	ready := false
	for start := time.Now(); time.Since(start) < 20*time.Second; {
		if _, e = qbt.Test(ctx, client); e == nil {
			ready = true
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(300 * time.Millisecond):
		}
	}
	if !ready {
		t.Fatal("isolated authenticated qBittorrent did not become ready", e)
	}
	generated := filepath.Join(t.TempDir(), "fixture.mp4")
	gen := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=128x96:rate=10", "-t", "2", "-c:v", "mpeg4", "-movflags", "faststart", generated)
	if output, e := gen.CombinedOutput(); e != nil {
		t.Fatalf("generate owned video: %v %s", e, output)
	}
	data, e := os.ReadFile(generated)
	if e != nil {
		t.Fatal(e)
	}
	var metainfo []byte
	initialized := make(chan struct{})
	var server *httptest.Server
	releaseBody := make(chan struct{})
	started := make(chan struct{})
	var first sync.Once
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-initialized
		switch r.URL.Path {
		case "/api":
			if r.URL.Query().Get("apikey") != "ci-indexer-key" {
				w.WriteHeader(401)
				return
			}
			switch r.URL.Query().Get("t") {
			case "caps":
				fmt.Fprint(w, `<caps><limits max="10"/><searching><search available="yes" supportedParams="q"/></searching></caps>`)
			case "search":
				if r.URL.Query().Get("limit") != "10" {
					t.Error("indexer limit was ignored")
				}
				fmt.Fprintf(w, `<rss><channel><item><title>Owned.Test.Video.2026</title><guid>owned-video</guid><enclosure url="%s/torrent" length="%d" type="application/x-bittorrent"/></item></channel></rss>`, server.URL, len(data))
			}
		case "/torrent":
			w.Header().Set("Content-Type", "application/x-bittorrent")
			_, _ = w.Write(metainfo)
		case "/announce":
			_, _ = w.Write(bencode(map[string]any{"interval": int64(60), "peers": ""}))
		case "/fixture.mp4":
			first.Do(func() { close(started) })
			select {
			case <-releaseBody:
			case <-r.Context().Done():
				return
			}
			http.ServeContent(w, r, "fixture.mp4", time.Now(), bytes.NewReader(data))
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	defer func() {
		select {
		case <-releaseBody:
		default:
			close(releaseBody)
		}
	}()
	var pieces []byte
	for start := 0; start < len(data); start += 16384 {
		end := start + 16384
		if end > len(data) {
			end = len(data)
		}
		sum := sha1.Sum(data[start:end])
		pieces = append(pieces, sum[:]...)
	}
	metainfo = bencode(map[string]any{"announce": server.URL + "/announce", "url-list": server.URL + "/fixture.mp4", "info": map[string]any{"length": int64(len(data)), "name": "fixture.mp4", "piece length": int64(16384), "pieces": string(pieces), "private": int64(1)}})
	close(initialized)
	secrets, e := securestore.New(ctx, f.db, f.cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	f.s = New(f.db, secrets, f.cfg, qbt, NewTorznab())
	apiKey := "ci-indexer-key"
	if e = f.s.ConfigureIndexer(ctx, IndexerUpdate{URL: server.URL + "/api", APIKey: &apiKey}); e != nil {
		t.Fatal(e)
	}
	if e = f.s.ConfigureClient(ctx, ClientUpdate{URL: client.URL, Username: client.Username, Password: &password, DownloadPath: client.DownloadPath}); e != nil {
		t.Fatal(e)
	}
	results, e := f.s.Search(ctx, f.movie.ID, "")
	if e != nil || len(results) != 1 {
		t.Fatal("real Torznab HTTP search failed", e)
	}
	job, created, e := f.s.Queue(ctx, results[0].ID)
	if e != nil || !created {
		t.Fatal("queue", e)
	}
	if _, e = f.s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case <-started:
	case <-time.After(45 * time.Second):
		t.Fatal("qBittorrent did not request the localhost web seed")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// Restart the application state while the real client waits for video bytes.
	f.db.Close()
	db, e := database.Open(ctx, f.cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	f.db = db
	secrets, e = securestore.New(ctx, db, f.cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	f.s = New(db, secrets, f.cfg, NewQBittorrent(), NewTorznab())
	close(releaseBody)
	for ctx.Err() == nil {
		_, _ = db.Exec(`UPDATE downloads SET next_run=0`)
		if _, e = f.s.ProcessNext(ctx); e != nil {
			t.Fatal(e)
		}
		current, e := f.s.Job(ctx, job.ID)
		if e != nil {
			t.Fatal(e)
		}
		if current.State == "completed" {
			break
		}
		if current.State == "blocked" || current.State == "failed" {
			t.Fatal("live workflow needs attention", current.ErrorCode, current.ErrorMessage)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(400 * time.Millisecond):
		}
	}
	if ctx.Err() != nil {
		t.Fatal(ctx.Err())
	}
	torrent, e := qbt.Find(ctx, client, job.Hash)
	if e != nil || torrent == nil || torrent.Progress < 1 {
		t.Fatal("seeding torrent was removed or changed", e)
	}
	var dest string
	if e = db.QueryRow(`SELECT relative_path FROM media_files WHERE download_id=?`, job.ID).Scan(&dest); e != nil {
		t.Fatal(e)
	}
	saved, e := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(dest)))
	if e != nil || !bytes.Equal(saved, data) {
		t.Fatal("live import differs from generated source", e)
	}
	source := filepath.Join(f.cfg.DownloadsDir, "ReelScarey", job.ID, "fixture.mp4")
	original, e := os.ReadFile(source)
	if e != nil || !bytes.Equal(original, data) {
		t.Fatal("seeding source changed", e)
	}
	t.Log("PASS: real authenticated qBittorrent downloaded owned video via localhost web seed; app restart reconciled it and FFprobe-verified import preserved the source")
}
func bencode(v any) []byte {
	switch x := v.(type) {
	case string:
		return append([]byte(strconv.Itoa(len(x))+":"), []byte(x)...)
	case int64:
		return []byte("i" + strconv.FormatInt(x, 10) + "e")
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var out bytes.Buffer
		out.WriteByte('d')
		for _, k := range keys {
			out.Write(bencode(k))
			out.Write(bencode(x[k]))
		}
		out.WriteByte('e')
		return out.Bytes()
	default:
		panic("unsupported test bencode type " + strings.TrimSpace(fmt.Sprintf("%T", v)))
	}
}
