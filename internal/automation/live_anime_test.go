package automation

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/quality"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"github.com/faaaaq/ReelScarey/internal/transfer"
	"golang.org/x/crypto/pbkdf2"
)

// Opt-in Linux acceptance uses a generated video, private torrent and its own
// authenticated localhost client. No owner services or media are test fixtures.
func TestRealAnimeAutomaticDownloadAndReviewedImport(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_ANIME") != "1" {
		t.Skip("set REELSCAREY_LIVE_ANIME=1 with qbittorrent-nox, FFmpeg and FFprobe installed")
	}
	if runtime.GOOS != "linux" {
		t.Skip("headless Linux integration")
	}
	for _, name := range []string{"qbittorrent-nox", "ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Fatalf("required integration binary %s: %v", name, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	root := t.TempDir()
	cfg := config.Config{ConfigDir: filepath.Join(root, "config"), MediaDir: filepath.Join(root, "media"), DownloadsDir: filepath.Join(root, "downloads")}
	for _, name := range []string{cfg.MediaDir, cfg.DownloadsDir} {
		if err := os.MkdirAll(name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	db, err := database.Open(ctx, cfg.ConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	client, password := startAnimeQBittorrent(t, ctx, cfg.DownloadsDir)
	generated := filepath.Join(t.TempDir(), "owned.mp4")
	gen := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=128x96:rate=10", "-t", "2", "-c:v", "mpeg4", "-movflags", "faststart", generated)
	if output, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("generate owned Anime video: %v %s", err, output)
	}
	data, err := os.ReadFile(generated)
	if err != nil {
		t.Fatal(err)
	}
	videoName := "Test Anime - 025v2.mp4"
	const releaseTitle = "[OwnedGroup] Test Anime - 025v2 [1080p] English"
	const searchTitle = "Owned Romanized Anime"
	var metainfo []byte
	var server *httptest.Server
	initialized := make(chan struct{})
	var queryMu sync.Mutex
	queries := []string{}
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-initialized:
		case <-r.Context().Done():
			return
		}
		switch r.URL.Path {
		case "/api":
			if r.URL.Query().Get("apikey") != "owned-anime-key" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			switch r.URL.Query().Get("t") {
			case "caps":
				fmt.Fprint(w, `<caps><limits max="10"/><searching><search available="yes" supportedParams="q"/></searching></caps>`)
			case "search":
				query := r.URL.Query().Get("q")
				if query != searchTitle+" 25" && query != searchTitle+" 025" {
					t.Errorf("unexpected Anime search variant %q", query)
				}
				if r.URL.Query().Get("cat") != "5000" || r.URL.Query().Get("limit") != "10" {
					t.Error("Anime query lost TV category or bounded capabilities")
				}
				queryMu.Lock()
				queries = append(queries, query)
				queryMu.Unlock()
				fmt.Fprintf(w, `<rss xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel><item><title>%s</title><guid>owned-anime-25</guid><enclosure url="%s/torrent" length="%d" type="application/x-bittorrent"/><torznab:attr name="seeders" value="5"/></item></channel></rss>`, releaseTitle, server.URL, len(data))
			default:
				w.WriteHeader(http.StatusBadRequest)
			}
		case "/torrent":
			w.Header().Set("Content-Type", "application/x-bittorrent")
			_, _ = w.Write(metainfo)
		case "/announce":
			_, _ = w.Write(animeBencode(map[string]any{"interval": int64(60), "peers": ""}))
		case "/" + videoName:
			http.ServeContent(w, r, videoName, time.Unix(1700000000, 0), bytes.NewReader(data))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	var pieces []byte
	for start := 0; start < len(data); start += 16384 {
		end := min(start+16384, len(data))
		sum := sha1.Sum(data[start:end])
		pieces = append(pieces, sum[:]...)
	}
	metainfo = animeBencode(map[string]any{"announce": server.URL + "/announce", "url-list": server.URL + "/" + url.PathEscape(videoName), "info": map[string]any{"length": int64(len(data)), "name": videoName, "piece length": int64(16384), "pieces": string(pieces), "private": int64(1)}})
	expectedHash, err := transfer.TorrentHash(metainfo)
	if err != nil {
		t.Fatal(err)
	}
	close(initialized)
	secrets, err := securestore.New(ctx, db, cfg.ConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	workflow := transfer.New(db, secrets, cfg, transfer.NewQBittorrent(), transfer.NewTorznab())
	key := "owned-anime-key"
	if err = workflow.ConfigureIndexer(ctx, transfer.IndexerUpdate{URL: server.URL + "/api", APIKey: &key}); err != nil {
		t.Fatal(err)
	}
	if err = workflow.ConfigureClient(ctx, transfer.ClientUpdate{URL: client.URL, Username: client.Username, Password: &password, DownloadPath: client.DownloadPath}); err != nil {
		t.Fatal(err)
	}
	catalog := library.New(db)
	seasonMetadata := metadata.SeasonSummary{TMDbID: 901002, Number: 2, Title: "Season 2", AirDate: "2000-01-01", EpisodeCount: 1}
	show, _, err := catalog.AddSeries(ctx, metadata.Series{Movie: metadata.Movie{TMDbID: 901001, Title: "Test Anime"}, Seasons: []metadata.SeasonSummary{seasonMetadata}}, true, "anime")
	if err != nil {
		t.Fatal(err)
	}
	if err = catalog.SaveSeason(ctx, show.ID, metadata.Season{SeasonSummary: seasonMetadata, Episodes: []metadata.Episode{{TMDbID: 901003, Number: 1, Title: "Owned Episode", AirDate: "2000-01-01"}}}, 1); err != nil {
		t.Fatal(err)
	}
	classification, err := catalog.Anime(ctx, show.ID)
	if err != nil || len(classification.Episodes) != 1 {
		t.Fatal(classification, err)
	}
	episodeID := classification.Episodes[0].EpisodeID
	absolute := 25
	if _, err = catalog.SaveAnime(ctx, show.ID, library.AnimeUpdate{Category: "anime", Revision: classification.Revision, NumberingRevision: classification.NumberingRevision, Episodes: []library.AnimeAlias{{EpisodeID: episodeID, AbsoluteNumber: &absolute}}}); err != nil {
		t.Fatal(err)
	}
	policy, err := catalog.AnimeSearch(ctx, show.ID)
	if err != nil {
		t.Fatal(err)
	}
	policy, err = catalog.SaveAnimeSearch(ctx, show.ID, library.AnimeSearchUpdate{Revision: policy.Revision, CategoryRevision: policy.CategoryRevision, NumberingRevision: policy.NumberingRevision, Mode: "absolute", SearchTitle: searchTitle, AlternateTitles: []string{searchTitle}})
	if err != nil || policy.ReadyCount != 1 {
		t.Fatal(policy, err)
	}
	profile, err := quality.New(db).Get(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	profile.Language = "english"
	if _, err = quality.New(db).Save(ctx, profile); err != nil {
		t.Fatal(err)
	}
	automatic := New(db, workflow)
	schedule, err := automatic.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = automatic.Update(ctx, Update{Enabled: true, Mode: "download", Interval: 15, Batch: 1, DefaultProfile: 1, Revision: schedule.Revision}); err != nil {
		t.Fatal(err)
	}
	if processed, err := automatic.ProcessNext(ctx); err != nil || !processed {
		t.Fatal("automatic Anime search did not run", processed, err)
	}
	page, err := workflow.List(ctx, 1)
	if err != nil || page.Total != 1 || len(page.Items) != 1 {
		runs, _ := automatic.Runs(ctx)
		t.Fatalf("automatic Anime queue missing: jobs=%+v runs=%+v err=%v", page, runs, err)
	}
	job := page.Items[0]
	if job.State != "queued" || job.Kind != "series" || job.Hash != expectedHash || job.AnimeSearch == nil || job.AnimeSearch.Mode != "absolute" || job.AnimeSearch.PolicyRevision != policy.Revision || job.AnimeSearch.AbsoluteNumber == nil || *job.AnimeSearch.AbsoluteNumber != 25 || len(job.Targets) != 1 || job.Targets[0].EpisodeID != episodeID || job.Targets[0].Season != 2 || job.Targets[0].Number != 1 || job.Targets[0].AbsoluteNumber == nil || *job.Targets[0].AbsoluteNumber != 25 {
		t.Fatal("queued Anime audit/identity snapshot incorrect", job)
	}
	queryMu.Lock()
	queryCount := len(queries)
	queryMu.Unlock()
	if queryCount != 2 {
		t.Fatalf("want two deduplicated bounded numeric variants, got %d", queryCount)
	}
	var queued int
	if err = db.QueryRow(`SELECT SUM(queued) FROM automation_runs`).Scan(&queued); err != nil || queued != 1 {
		t.Fatal("scheduler did not record its own automatic queue", queued, err)
	}
	// Opt out and edit current absolute numbering after queueing. Approved jobs
	// retain their old policy/targets; import authority remains canonical IDs.
	if _, err = catalog.SaveAnimeSearch(ctx, show.ID, library.AnimeSearchUpdate{Revision: policy.Revision, CategoryRevision: policy.CategoryRevision, NumberingRevision: policy.NumberingRevision, Mode: "disabled", SearchTitle: searchTitle, AlternateTitles: []string{searchTitle}}); err != nil {
		t.Fatal(err)
	}
	classification, err = catalog.Anime(ctx, show.ID)
	if err != nil {
		t.Fatal(err)
	}
	changedAbsolute := 125
	if _, err = catalog.SaveAnime(ctx, show.ID, library.AnimeUpdate{Category: "anime", Revision: classification.Revision, NumberingRevision: classification.NumberingRevision, Episodes: []library.AnimeAlias{{EpisodeID: episodeID, AbsoluteNumber: &changedAbsolute}}}); err != nil {
		t.Fatal(err)
	}
	reopen := func() {
		t.Helper()
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		var err error
		db, err = database.Open(ctx, cfg.ConfigDir)
		if err != nil {
			t.Fatal(err)
		}
		secrets, err := securestore.New(ctx, db, cfg.ConfigDir)
		if err != nil {
			t.Fatal(err)
		}
		workflow = transfer.New(db, secrets, cfg, transfer.NewQBittorrent(), transfer.NewTorznab())
	}
	reopen()
	reviewed := false
	for ctx.Err() == nil {
		if _, err = db.ExecContext(ctx, `UPDATE downloads SET next_run=0 WHERE id=?`, job.ID); err != nil {
			t.Fatal(err)
		}
		if _, err = workflow.ProcessNext(ctx); err != nil {
			t.Fatal(err)
		}
		current, err := workflow.Job(ctx, job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.State == "completed" {
			if !reviewed {
				t.Fatal("Anime automatically imported without a confirmed episode mapping")
			}
			break
		}
		if current.State == "blocked" && current.ErrorCode == "map_episodes" {
			if reviewed {
				t.Fatal("confirmed Anime mapping was lost after reopen")
			}
			var registered int
			if err = db.QueryRow(`SELECT COUNT(*) FROM media_files WHERE download_id=?`, job.ID).Scan(&registered); err != nil || registered != 0 {
				t.Fatal("Anime registered media before mapping review", registered, err)
			}
			if len(current.Choices) != 1 || len(current.Choices[0].Suggestions) != 1 || current.Choices[0].Suggestions[0] != episodeID || len(current.Targets) != 1 || current.Targets[0].AbsoluteNumber == nil || *current.Targets[0].AbsoluteNumber != 25 {
				t.Fatal("review did not retain explicit queued absolute mapping", current)
			}
			if err = workflow.ConfirmTVMapping(ctx, job.ID, transfer.TVMappingUpdate{Files: []transfer.TVMapping{{FileIndex: current.Choices[0].Index, EpisodeIDs: []int64{episodeID}}}}); err != nil {
				t.Fatal(err)
			}
			reviewed = true
			reopen()
			continue
		}
		if current.State == "blocked" || current.State == "failed" {
			t.Fatal("live automatic Anime workflow needs attention", current.ErrorCode, current.ErrorMessage)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(300 * time.Millisecond):
		}
	}
	if ctx.Err() != nil {
		t.Fatal(ctx.Err())
	}
	var relative, probeJSON string
	var actualEpisode, fileCount int64
	if err = db.QueryRow(`SELECT COUNT(*) FROM media_files WHERE download_id=?`, job.ID).Scan(&fileCount); err != nil || fileCount != 1 {
		t.Fatal("wrong imported Anime file count", fileCount, err)
	}
	if err = db.QueryRow(`SELECT f.relative_path,f.probe_json,ef.episode_id FROM media_files f JOIN episode_files ef ON ef.media_file_id=f.id WHERE f.download_id=?`, job.ID).Scan(&relative, &probeJSON, &actualEpisode); err != nil || actualEpisode != episodeID || !strings.HasPrefix(relative, "Anime/TV Shows/") || !strings.Contains(relative, "S02E01") {
		t.Fatal("canonical episode or Anime destination changed", relative, actualEpisode, err)
	}
	var probe transfer.Probe
	if err = json.Unmarshal([]byte(probeJSON), &probe); err != nil || probe.Width != 128 || probe.Height != 96 || probe.Codec == "" || probe.Duration <= 0 {
		t.Fatal("import did not retain real FFprobe results; labels must not replace inspected quality", probe, err)
	}
	for _, name := range []string{filepath.Join(cfg.MediaDir, filepath.FromSlash(relative)), filepath.Join(cfg.DownloadsDir, "ReelScarey", job.ID, videoName)} {
		saved, err := os.ReadFile(name)
		if err != nil || !bytes.Equal(saved, data) {
			t.Fatal("verified Anime import changed or removed owned bytes", name, err)
		}
	}
	torrent, err := transfer.NewQBittorrent().Find(ctx, client, job.Hash)
	if err != nil || torrent == nil || torrent.Progress < 1 {
		t.Fatal("completed seeding source was removed or changed", err)
	}
	current, err := workflow.Job(ctx, job.ID)
	if err != nil || current.AnimeSearch == nil || current.AnimeSearch.Mode != "absolute" || current.AnimeSearch.PolicyRevision != policy.Revision || current.AnimeSearch.AbsoluteNumber == nil || *current.AnimeSearch.AbsoluteNumber != 25 {
		t.Fatal("approved policy snapshot did not survive opt-out and reopen", current, err)
	}
	t.Log("PASS: scheduler matched explicit Anime absolute alias through bounded real Torznab HTTP queries, queued to isolated authenticated qBittorrent, survived opt-out/alias edits and restart, required reviewed canonical mapping, and FFprobe-verified generated media under Anime while preserving seeding bytes")
}

func startAnimeQBittorrent(t *testing.T, ctx context.Context, downloads string) (transfer.ClientConfig, string) {
	t.Helper()
	profile := t.TempDir()
	configPath := filepath.Join(profile, "qBittorrent", "config")
	if err := os.MkdirAll(configPath, 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	password := transfer.ID()
	salt := []byte(transfer.ID()[:16])
	key := pbkdf2.Key([]byte(password), salt, 100000, 64, sha512.New)
	encoded := base64.StdEncoding.EncodeToString(salt) + ":" + base64.StdEncoding.EncodeToString(key)
	ini := fmt.Sprintf("[LegalNotice]\nAccepted=true\n[Preferences]\nWebUI\\Address=127.0.0.1\nWebUI\\Port=%d\nWebUI\\Username=ciadmin\nWebUI\\Password_PBKDF2=\"@ByteArray(%s)\"\nWebUI\\LocalHostAuth=true\nWebUI\\CSRFProtection=true\nWebUI\\HostHeaderValidation=true\n[BitTorrent]\nSession\\DHTEnabled=false\nSession\\LSDEnabled=false\nSession\\PeXEnabled=false\nSession\\Port=0\n", port, encoded)
	if err = os.WriteFile(filepath.Join(configPath, "qBittorrent.conf"), []byte(ini), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "qbittorrent-nox", "--profile="+profile, "--webui-port="+strconv.Itoa(port))
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-exited:
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
		}
	})
	client := transfer.ClientConfig{URL: fmt.Sprintf("http://127.0.0.1:%d", port), Username: "ciadmin", Password: password, DownloadPath: filepath.ToSlash(downloads)}
	qbt := transfer.NewQBittorrent()
	for start := time.Now(); time.Since(start) < 20*time.Second; {
		if _, err = qbt.Test(ctx, client); err == nil {
			return client, password
		}
		select {
		case exitErr := <-exited:
			exited <- exitErr
			t.Fatalf("isolated Anime qBittorrent exited before becoming ready: %v", exitErr)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(300 * time.Millisecond):
		}
	}
	t.Fatal("isolated authenticated Anime qBittorrent did not become ready", err)
	return transfer.ClientConfig{}, ""
}

func animeBencode(v any) []byte {
	switch x := v.(type) {
	case string:
		return append([]byte(strconv.Itoa(len(x))+":"), []byte(x)...)
	case int64:
		return []byte("i" + strconv.FormatInt(x, 10) + "e")
	case map[string]any:
		keys := make([]string, 0, len(x))
		for key := range x {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var out bytes.Buffer
		out.WriteByte('d')
		for _, key := range keys {
			out.Write(animeBencode(key))
			out.Write(animeBencode(x[key]))
		}
		out.WriteByte('e')
		return out.Bytes()
	default:
		panic(fmt.Sprintf("unsupported owned Anime test bencode type %T", v))
	}
}
