package transfer

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/securestore"
)

// Called only by the opt-in isolated qBittorrent fixture. Both torrents and
// videos belong to this test; no owner library, provider or client is contacted.
func runRealMovieVersions(t *testing.T, ctx context.Context, f *workflowFixture, client Client, config ClientConfig) {
	t.Helper()
	data := map[string][]byte{}
	metainfo := map[string][]byte{}
	initialized := make(chan struct{})
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-initialized
		switch r.URL.Path {
		case "/api":
			if r.URL.Query().Get("apikey") != "owned-fixture" {
				w.WriteHeader(401)
				return
			}
			if r.URL.Query().Get("t") == "caps" {
				fmt.Fprint(w, `<caps><limits max="10"/><searching><search available="yes" supportedParams="q"/></searching></caps>`)
				return
			}
			fmt.Fprint(w, `<rss><channel>`)
			for _, version := range []string{"hd", "uhd"} {
				resolution := "1080p"
				if version == "uhd" {
					resolution = "2160p"
				}
				fmt.Fprintf(w, `<item><title>Test.Movie.2026.%s</title><guid>%s</guid><enclosure url="%s/%s.torrent" length="%d" type="application/x-bittorrent"/></item>`, resolution, version, server.URL, version, len(data[version]))
			}
			fmt.Fprint(w, `</channel></rss>`)
		case "/hd.torrent", "/uhd.torrent":
			w.Header().Set("Content-Type", "application/x-bittorrent")
			_, _ = w.Write(metainfo[r.URL.Path])
		case "/announce":
			_, _ = w.Write(bencode(map[string]any{"interval": int64(60), "peers": ""}))
		case "/hd.mp4", "/uhd.mp4":
			version := r.URL.Path[1 : len(r.URL.Path)-4]
			http.ServeContent(w, r, version+".mp4", time.Time{}, bytes.NewReader(data[version]))
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	for _, version := range []string{"hd", "uhd"} {
		size := "1920x1080"
		if version == "uhd" {
			size = "3840x2160"
		}
		name := filepath.Join(t.TempDir(), version+".mp4")
		cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=blue:s="+size+":r=2", "-frames:v", "2", "-c:v", "libx264", "-preset", "ultrafast", "-threads", "1", "-movflags", "faststart", name)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("generated %s: %s %v", version, output, err)
		}
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		data[version] = body
		var pieces []byte
		for start := 0; start < len(body); start += 16384 {
			end := min(start+16384, len(body))
			hash := sha1.Sum(body[start:end])
			pieces = append(pieces, hash[:]...)
		}
		metainfo["/"+version+".torrent"] = bencode(map[string]any{"announce": server.URL + "/announce", "url-list": server.URL + "/" + version + ".mp4", "info": map[string]any{"length": int64(len(body)), "name": version + ".mp4", "piece length": int64(16384), "pieces": string(pieces), "private": int64(1)}})
	}
	close(initialized)
	secrets, err := securestore.New(ctx, f.db, f.cfg.ConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	f.s = New(f.db, secrets, f.cfg, client, NewTorznab())
	key := "owned-fixture"
	if err = f.s.ConfigureIndexer(ctx, IndexerUpdate{URL: server.URL + "/api", APIKey: &key}); err != nil {
		t.Fatal(err)
	}
	if err = f.s.ConfigureClient(ctx, ClientUpdate{URL: config.URL, Username: config.Username, Password: &config.Password, DownloadPath: config.DownloadPath}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(`INSERT INTO movie_version_settings(media_id,enabled,hd_profile_id,uhd_profile_id) VALUES(?,1,1,2)`, f.movie.ID); err != nil {
		t.Fatal(err)
	}
	releases, err := f.s.Search(ctx, f.movie.ID, "")
	if err != nil || len(releases) != 2 {
		t.Fatal(releases, err)
	}
	jobs := []Job{}
	for n, version := range []string{"hd", "uhd"} {
		job, created, e := f.s.QueueMovieVersion(ctx, releases[n].ID, version)
		if e != nil || !created {
			t.Fatal(job, e)
		}
		jobs = append(jobs, job)
	}
	// Reopen while both reservations are pending. Restore actual adapters/prober;
	// the ordinary workflow fixture intentionally installs lightweight fakes.
	f.reopen(t)
	secrets, err = securestore.New(ctx, f.db, f.cfg.ConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	f.s = New(f.db, secrets, f.cfg, NewQBittorrent(), NewTorznab())
	for ctx.Err() == nil {
		if _, err = f.db.Exec(`UPDATE downloads SET next_run=0`); err != nil {
			t.Fatal(err)
		}
		if _, err = f.s.ProcessNext(ctx); err != nil {
			t.Fatal(err)
		}
		done := 0
		for _, job := range jobs {
			current, e := f.s.Job(ctx, job.ID)
			if e != nil {
				t.Fatal(e)
			}
			if current.State == "failed" || current.State == "blocked" {
				t.Fatal(current.State, current.ErrorCode, current.ErrorMessage)
			}
			if current.State == "completed" {
				done++
			}
		}
		if done == 2 {
			break
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
	details, err := f.s.Details(ctx, f.movie.ID, 1)
	if err != nil || details.FileCount != 2 || len(details.Files) != 2 {
		t.Fatal(details, err)
	}
	if details.Files[0].Path == details.Files[1].Path {
		t.Fatal("versions share destination")
	}
	for _, job := range jobs {
		var relative string
		var resolution int
		if err = f.db.QueryRow(`SELECT relative_path,video_resolution FROM media_files WHERE download_id=?`, job.ID).Scan(&relative, &resolution); err != nil {
			t.Fatal(err)
		}
		want := 1080
		if job.MovieVersion == "uhd" {
			want = 2160
		}
		if resolution != want {
			t.Fatal("wrong inspected resolution", resolution, want)
		}
		for _, name := range []string{filepath.Join(f.cfg.MediaDir, filepath.FromSlash(relative)), filepath.Join(config.DownloadPath, "ReelScarey", job.ID, job.MovieVersion+".mp4")} {
			body, e := os.ReadFile(name)
			if e != nil || !bytes.Equal(body, data[job.MovieVersion]) {
				t.Fatal("video/source altered", name, e)
			}
		}
		torrent, e := client.Find(ctx, config, job.Hash)
		if e != nil || torrent == nil || torrent.Progress < 1 {
			t.Fatal("seeding source lost", e)
		}
	}
	t.Log("PASS: real qBittorrent downloaded HD and 4K into independent targets across reopen; FFprobe verified both, both files registered, and both seeding sources retained")
}
