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
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"golang.org/x/crypto/pbkdf2"
)

// Opt-in only: starts its own authenticated localhost qBittorrent with an
// isolated profile. It never connects to an owner's existing client/indexer.
func TestRealQBittorrentDownloadAndImport(t *testing.T) {
	runRealQBittorrent(t, false)
}
func TestRealQBittorrentTVDownloadAndImport(t *testing.T) {
	runRealQBittorrent(t, true)
}
func TestRealQBittorrentAnimeOrderPackDownloadAndImport(t *testing.T) {
	runRealQBittorrent(t, true, true)
}
func TestRealQBittorrentNamedClientDownloadAndImport(t *testing.T) {
	runRealQBittorrent(t, false, false, true)
}
func TestRealQBittorrentNamedClientTVDownloadAndImport(t *testing.T) {
	runRealQBittorrent(t, true, false, true)
}
func TestRealTransmissionDownloadAndImport(t *testing.T) {
	runRealQBittorrent(t, false, false, true, true)
}
func TestRealTransmissionTVDownloadAndImport(t *testing.T) {
	runRealQBittorrent(t, true, false, true, true)
}
func runRealQBittorrent(t *testing.T, tv bool, providerOrder ...bool) {
	transmission := len(providerOrder) > 2 && providerOrder[2]
	binary, kind := "qbittorrent-nox", "qbittorrent"
	if transmission {
		binary, kind = "transmission-daemon", "transmission"
		if os.Getenv("REELSCAREY_LIVE_TRANSMISSION") != "1" {
			t.Skip("set REELSCAREY_LIVE_TRANSMISSION=1 with Transmission 4 and FFmpeg")
		}
	}
	if !transmission && os.Getenv("REELSCAREY_LIVE_QBT") != "1" {
		t.Skip("set REELSCAREY_LIVE_QBT=1 with qbittorrent-nox and FFmpeg installed")
	}
	if runtime.GOOS != "linux" {
		t.Skip("headless Linux integration")
	}
	for _, binary := range []string{binary, "ffmpeg", "ffprobe"} {
		if _, e := exec.LookPath(binary); e != nil {
			t.Fatalf("required integration binary %s: %v", binary, e)
		}
	}
	pack := len(providerOrder) > 0 && providerOrder[0]
	named := len(providerOrder) > 1 && providerOrder[1]
	var f *workflowFixture
	var packID int64
	var packEpisodeIDs []int64
	var packOrder library.AnimeOrderSettings
	if pack {
		f, packID, packEpisodeIDs, packOrder, _ = setupOrderPack(t)
	} else {
		f = setupWorkflow(t)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var e error
	var qbt Client
	var client ClientConfig
	var password string
	if transmission {
		qbt, client, password = startTransmissionFixture(t, ctx, f)
	} else {
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
		password = ID()
		salt := []byte(ID()[:16])
		key := pbkdf2.Key([]byte(password), salt, 100000, 64, sha512.New)
		encoded := base64.StdEncoding.EncodeToString(salt) + ":" + base64.StdEncoding.EncodeToString(key)
		ini := fmt.Sprintf("[LegalNotice]\nAccepted=true\n[Preferences]\nWebUI\\Address=127.0.0.1\nWebUI\\Port=%d\nWebUI\\Username=ciadmin\nWebUI\\Password_PBKDF2=\"@ByteArray(%s)\"\nWebUI\\LocalHostAuth=true\nWebUI\\CSRFProtection=true\nWebUI\\HostHeaderValidation=true\n[BitTorrent]\nSession\\DHTEnabled=false\nSession\\LSDEnabled=false\nSession\\PeXEnabled=false\nSession\\Port=0\n", port, encoded)
		if e = os.WriteFile(filepath.Join(configPath, "qBittorrent.conf"), []byte(ini), 0600); e != nil {
			t.Fatal(e)
		}
		// The isolated profile already accepts the notice. The command-line notice
		// flag is unavailable in older supported clients, including Ubuntu's 4.6.3.
		cmd := exec.CommandContext(ctx, "qbittorrent-nox", "--profile="+profile, "--webui-port="+strconv.Itoa(port))
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()
		defer func() {
			_ = cmd.Process.Signal(os.Interrupt)
			select {
			case <-exited:
			case <-time.After(15 * time.Second):
				_ = cmd.Process.Kill()
				<-exited
			}
		}()
		qbt = NewQBittorrent()
		client = ClientConfig{URL: fmt.Sprintf("http://127.0.0.1:%d", port), Username: "ciadmin", Password: password, DownloadPath: filepath.ToSlash(f.cfg.DownloadsDir)}
		if named {
			client.LocalFolder = "named-qbt"
			client.DownloadPath = filepath.ToSlash(filepath.Join(f.cfg.DownloadsDir, client.LocalFolder))
			if e = os.MkdirAll(client.DownloadPath, 0755); e != nil {
				t.Fatal(e)
			}
		}
		ready := false
		for start := time.Now(); time.Since(start) < 20*time.Second; {
			if _, e = qbt.Test(ctx, client); e == nil {
				ready = true
				break
			}
			select {
			case exitErr := <-exited:
				exited <- exitErr
				t.Fatalf("isolated qBittorrent exited before becoming ready: %v", exitErr)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(300 * time.Millisecond):
			}
		}
		if !ready {
			t.Fatal("isolated authenticated qBittorrent did not become ready", e)
		}
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
	videoNames := []string{"fixture.mp4"}
	allData := data
	if tv {
		videoNames = []string{"Owned.S01E01-E02.mp4", "Owned.S01E03.mp4"}
		if named {
			videoNames = []string{"Owned.Season 1 Ep 1-Ep 2.mp4", "Owned.S01EP03.mp4"}
		}
		if pack {
			videoNames = []string{"Pack Story - SP01.mp4", "Pack Story - 001.mp4", "Pack Story Part 2 - 001v2.mp4"}
		}
		allData = nil
		for range videoNames {
			allData = append(allData, data...)
		}
	}
	var metainfo []byte
	initialized := make(chan struct{})
	var server *httptest.Server
	releaseBody := make(chan struct{})
	started := make(chan struct{})
	var first sync.Once
	ownedPaths := map[string]bool{}
	for _, name := range videoNames {
		prefix := "/"
		if tv {
			prefix = "/tv-fixture/"
		}
		ownedPaths[prefix+name] = true
	}
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
				category := "2000"
				if tv {
					category = "5000"
				}
				if r.URL.Query().Get("cat") != category {
					t.Error("wrong media category")
				}
				if r.URL.Query().Get("limit") != "10" {
					t.Error("indexer limit was ignored")
				}
				title := "Owned.Test.Video.2026"
				if pack {
					title = "Owned Anime Complete Pack"
				}
				fmt.Fprintf(w, `<rss><channel><item><title>%s</title><guid>owned-video</guid><enclosure url="%s/torrent" length="%d" type="application/x-bittorrent"/></item></channel></rss>`, title, server.URL, len(allData))
			}
		case "/torrent":
			w.Header().Set("Content-Type", "application/x-bittorrent")
			_, _ = w.Write(metainfo)
		case "/announce":
			_, _ = w.Write(bencode(map[string]any{"interval": int64(60), "peers": ""}))
		default:
			if !ownedPaths[r.URL.Path] {
				w.WriteHeader(404)
				return
			}
			first.Do(func() { close(started) })
			select {
			case <-releaseBody:
			case <-r.Context().Done():
				return
			}
			http.ServeContent(w, r, "fixture.mp4", time.Now(), bytes.NewReader(data))
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
	for start := 0; start < len(allData); start += 16384 {
		end := start + 16384
		if end > len(allData) {
			end = len(allData)
		}
		sum := sha1.Sum(allData[start:end])
		pieces = append(pieces, sum[:]...)
	}
	metainfo = bencode(map[string]any{"announce": server.URL + "/announce", "url-list": server.URL + "/fixture.mp4", "info": map[string]any{"length": int64(len(data)), "name": "fixture.mp4", "piece length": int64(16384), "pieces": string(pieces), "private": int64(1)}})
	if tv {
		files := []any{}
		for _, name := range videoNames {
			files = append(files, map[string]any{"length": int64(len(data)), "path": []any{name}})
		}
		metainfo = bencode(map[string]any{"announce": server.URL + "/announce", "url-list": server.URL + "/", "info": map[string]any{"files": files, "name": "tv-fixture", "piece length": int64(16384), "pieces": string(pieces), "private": int64(1)}})
	}
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
	var defaults ClientDefaults
	if named {
		saved, err := f.s.SaveDownloadClient(ctx, DownloadClientUpdate{Name: "Owned torrent", Kind: kind, URL: client.URL, Username: client.Username, Password: &password, DownloadPath: client.DownloadPath, LocalFolder: client.LocalFolder})
		if err != nil {
			t.Fatal(err)
		}
		saved, err = f.s.SaveDownloadClient(ctx, DownloadClientUpdate{ID: saved.ID, Name: saved.Name, Kind: saved.Kind, Enabled: true, Revision: saved.Revision, URL: client.URL, Username: client.Username, DownloadPath: client.DownloadPath, LocalFolder: client.LocalFolder})
		if err != nil {
			t.Fatal(err)
		}
		defaults, err = f.s.ClientDefaults(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defaults.Torrent = saved.ID
		defaults, err = f.s.SaveClientDefaults(ctx, defaults)
		if err != nil {
			t.Fatal(err)
		}
	} else if e = f.s.ConfigureClient(ctx, ClientUpdate{URL: client.URL, Username: client.Username, Password: &password, DownloadPath: client.DownloadPath}); e != nil {
		t.Fatal(e)
	}
	var results []Release
	if pack {
		results, e = f.s.SearchAnimePack(ctx, packID, packEpisodeIDs, "Owned Anime Pack", packOrder.Revision)
	} else if tv {
		show := metadata.Series{Movie: metadata.Movie{TMDbID: 456, Title: "Owned Test Series"}, Seasons: []metadata.SeasonSummary{{TMDbID: 457, Number: 1, Title: "Season 1", EpisodeCount: 3}}}
		item, _, err := library.New(f.db).AddSeries(ctx, show, true)
		if err != nil {
			t.Fatal(err)
		}
		if err = library.New(f.db).SaveSeason(ctx, item.ID, metadata.Season{SeasonSummary: show.Seasons[0], Episodes: []metadata.Episode{{TMDbID: 458, Number: 1, Title: "One"}, {TMDbID: 459, Number: 2, Title: "Two"}, {TMDbID: 460, Number: 3, Title: "Three"}}}, 1); err != nil {
			t.Fatal(err)
		}
		results, e = f.s.SearchTV(ctx, item.ID, 1, 0, "")
	} else {
		results, e = f.s.Search(ctx, f.movie.ID, "")
	}
	if e != nil || len(results) != 1 {
		t.Fatal("real Torznab HTTP search failed", e)
	}
	job, created, e := f.s.Queue(ctx, results[0].ID)
	if e != nil || !created {
		t.Fatal("queue", e)
	}
	if named {
		if job.ClientKey != defaults.Torrent || job.ClientKind != kind {
			t.Fatal("named binding missing")
		}
		defaults.Torrent = ""
		if _, e = f.s.SaveClientDefaults(ctx, defaults); e != nil {
			t.Fatal(e)
		}
	}
	if pack {
		changed := append([]library.AnimeOrderEntry{}, packOrder.Entries...)
		for n := range changed {
			changed[n].ReleaseTitle = "Changed Future Pack Title"
			changed[n].Label = []string{"SP9", "9", "10"}[n]
		}
		saveTransferOrder(t, f, packID, changed)
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
	defer func() { db.Close() }()
	f.db = db
	secrets, e = securestore.New(ctx, db, f.cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	f.s = New(db, secrets, f.cfg, NewQBittorrent(), NewTorznab())
	close(releaseBody)
	reviewed := false
	for ctx.Err() == nil {
		_, _ = db.Exec(`UPDATE downloads SET next_run=0`)
		if _, e = f.s.ProcessNext(ctx); e != nil {
			t.Fatal(e)
		}
		current, e := f.s.Job(ctx, job.ID)
		if e != nil {
			t.Fatal(e)
		}
		if pack && (current.AnimeOrder == nil || current.AnimeOrder.Revision != packOrder.Revision || len(current.AnimeOrder.Entries) != 3) {
			t.Fatal("queued provider order snapshot lost after edit/reopen", current.AnimeOrder)
		}
		if current.State == "completed" {
			if tv && !reviewed {
				t.Fatal("TV/Anime pack imported without a confirmed file mapping")
			}
			break
		}
		if tv && current.State == "blocked" && current.ErrorCode == "map_episodes" {
			if reviewed {
				t.Fatal("confirmed TV/Anime file mapping was lost after reopen")
			}
			var count int
			if e = db.QueryRow(`SELECT COUNT(*) FROM media_files WHERE download_id=?`, job.ID).Scan(&count); e != nil || count != 0 {
				t.Fatal("TV imported before confirmation", e)
			}
			mapping := TVMappingUpdate{Files: []TVMapping{}, SkipEpisodeIDs: []int64{}}
			for _, choice := range current.Choices {
				if len(choice.Suggestions) == 0 {
					t.Fatal("TV fixture filename mapping absent")
				}
				mapping.Files = append(mapping.Files, TVMapping{FileIndex: choice.Index, EpisodeIDs: choice.Suggestions})
			}
			if e = f.s.ConfirmTVMapping(ctx, job.ID, mapping); e != nil {
				t.Fatal(e)
			}
			reviewed = true
			// Reopen after confirmation as well as during the real download.
			db.Close()
			db, e = database.Open(ctx, f.cfg.ConfigDir)
			if e != nil {
				t.Fatal(e)
			}
			f.db = db
			secrets, e = securestore.New(ctx, db, f.cfg.ConfigDir)
			if e != nil {
				t.Fatal(e)
			}
			f.s = New(db, secrets, f.cfg, NewQBittorrent(), NewTorznab())
			continue
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
	rows, e := db.Query(`SELECT relative_path FROM media_files WHERE download_id=? ORDER BY file_index`, job.ID)
	if e != nil {
		t.Fatal(e)
	}
	destinations := []string{}
	for rows.Next() {
		var dest string
		if e = rows.Scan(&dest); e != nil {
			t.Fatal(e)
		}
		destinations = append(destinations, dest)
	}
	rows.Close()
	if len(destinations) != len(videoNames) {
		t.Fatal("wrong imported file count", len(destinations))
	}
	for _, dest := range destinations {
		saved, e := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(dest)))
		if e != nil || !bytes.Equal(saved, data) {
			t.Fatal("live import differs from generated source", e)
		}
	}
	for _, name := range videoNames {
		source := filepath.Join(client.DownloadPath, "ReelScarey", job.ID)
		if tv {
			source = filepath.Join(source, "tv-fixture")
		}
		original, e := os.ReadFile(filepath.Join(source, name))
		if e != nil || !bytes.Equal(original, data) {
			t.Fatal("seeding source changed", e)
		}
	}
	if tv {
		if pack {
			for n, number := range []int{0, 1, 2} {
				season, e := library.New(db).TVSeason(ctx, packID, number)
				if e != nil || season.Season.AvailableEpisodes != 1 || len(season.Episodes) != 1 || season.Episodes[0].ID != packEpisodeIDs[n] || !season.Episodes[0].Available {
					t.Fatal("reviewed cross-season/special registration incorrect", number, season, e)
				}
				if number == 0 && season.Season.Monitored {
					t.Fatal("reviewed special import enabled monitoring")
				}
			}
			details, e := f.s.Details(ctx, packID, 1)
			if e != nil || details.FileCount != 3 || len(details.Files) != 3 {
				t.Fatal("cross-season pack did not register three distinct files", details, e)
			}
			for _, file := range details.Files {
				if file.Probe.Width != 128 || file.Probe.Height != 96 || file.Probe.Codec == "" || !strings.Contains(file.Path, filepath.Join("Anime", "TV Shows")) {
					t.Fatal("pack import lacked real FFprobe or Anime organization", file)
				}
			}
			t.Log("PASS:", kind, "downloaded three generated Anime pack videos; explicit provider order survived edits and reopen, mandatory mapping registered canonical S00E01/S01E01/S02E01, FFprobe verified each file and seeding sources remained intact")
			return
		}
		season, e := library.New(db).TVSeason(ctx, job.MovieID, 1)
		if e != nil || season.Season.AvailableEpisodes != 3 || season.Episodes[0].FilePath != season.Episodes[1].FilePath || season.Episodes[1].FilePath == season.Episodes[2].FilePath {
			t.Fatal("live episode registration incorrect", e)
		}
		t.Log("PASS:", kind, "downloaded a generated season pack; review confirmation survived reopen; two FFprobe-verified files registered three episodes and preserved seeding sources")
		return
	}
	t.Log("PASS:", kind, "downloaded owned video via localhost web seed; app restart reconciled it and FFprobe-verified import preserved the source")
}
func bencode(v any) []byte {
	switch x := v.(type) {
	case []any:
		var out bytes.Buffer
		out.WriteByte('l')
		for _, v := range x {
			out.Write(bencode(v))
		}
		out.WriteByte('e')
		return out.Bytes()
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
