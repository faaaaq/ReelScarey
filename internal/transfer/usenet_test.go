package transfer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/library"
)

const ownedNZB = `<?xml version="1.0"?><!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd"><nzb xmlns="http://www.newzbin.com/DTD/2003/nzb"><file poster="fixture" date="1" subject="owned.mp4"><groups><group>alt.test</group></groups><segments><segment bytes="100" number="1">owned-fixture@invalid</segment></segments></file></nzb>`

type sabFixture struct {
	mu                       sync.Mutex
	tag, state, storage      string
	adds                     int
	lost, missing, duplicate bool
	upload                   []byte
}

func installUsenet(t *testing.T, f *workflowFixture) *sabFixture {
	t.Helper()
	ctx := context.Background()
	v := &sabFixture{state: "Downloading"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.mu.Lock()
		defer v.mu.Unlock()
		if r.URL.Path == "/newznab" {
			if r.URL.Query().Get("apikey") != "private-indexer-key" {
				w.WriteHeader(403)
				return
			}
			if r.URL.Query().Get("t") == "caps" {
				fmt.Fprint(w, `<caps><limits max="10"/><searching><search available="yes" supportedParams="q"/></searching></caps>`)
				return
			}
			if r.URL.Query().Get("cat") != "2000" && r.URL.Query().Get("cat") != "5000" {
				t.Error("missing media category")
			}
			fmt.Fprintf(w, `<rss xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel><item><title>Test.Movie.2026.1080p.English</title><guid>owned</guid><enclosure url="http://%s/nzb?pass=private" length="100" type="application/x-nzb"/><newznab:attr name="size" value="12345"/></item></channel></rss>`, r.Host)
			return
		}
		if r.URL.Path == "/nzb" {
			fmt.Fprint(w, ownedNZB)
			return
		}
		if r.URL.Path != "/api" {
			w.WriteHeader(404)
			return
		}
		if r.Method == "POST" {
			if e := r.ParseMultipartForm(5 << 20); e != nil {
				t.Error(e)
				w.WriteHeader(400)
				return
			}
			defer r.MultipartForm.RemoveAll()
		}
		if r.FormValue("apikey") != "private-sab-key" {
			w.WriteHeader(403)
			return
		}
		out := any(nil)
		switch r.FormValue("mode") {
		case "version":
			out = map[string]any{"version": "4.5.3"}
		case "get_cats":
			out = map[string]any{"categories": []string{"*", "reelscarey"}}
		case "addfile":
			if r.Method != "POST" || r.FormValue("cat") != "reelscarey" || r.FormValue("script") != "None" || r.FormValue("pp") != "2" {
				t.Error("unsafe submission options")
			}
			file, _, e := r.FormFile("name")
			if e != nil {
				t.Error(e)
				return
			}
			v.upload, _ = io.ReadAll(file)
			file.Close()
			if string(v.upload) != ownedNZB {
				t.Error("NZB changed in upload")
			}
			v.tag = r.FormValue("nzbname")
			v.adds++
			if v.lost {
				conn, _, e := w.(http.Hijacker).Hijack()
				if e == nil {
					conn.Close()
				}
				return
			}
			out = map[string]any{"status": true, "nzo_ids": []string{"SABnzbd_nzo_owned"}}
		case "queue", "history":
			mode := r.FormValue("mode")
			items := []map[string]any{}
			if v.tag != "" && !v.missing && ((mode == "queue" && v.state == "Downloading") || (mode == "history" && v.state != "Downloading")) {
				item := map[string]any{"nzo_id": "SABnzbd_nzo_owned", "filename": v.tag, "name": v.tag, "status": v.state, "cat": "reelscarey", "category": "reelscarey", "mb": "1", "mbleft": "0.5", "bytes": 100, "downloaded": 100, "storage": v.storage, "loaded": v.state != "Completed"}
				items = append(items, item)
				if v.duplicate {
					copy := map[string]any{}
					for k, v := range item {
						copy[k] = v
					}
					copy["nzo_id"] = "SABnzbd_nzo_duplicate"
					items = append(items, copy)
				}
			}
			out = map[string]any{mode: map[string]any{"slots": items}}
		default:
			t.Error("unexpected SAB mode", r.FormValue("mode"))
			w.WriteHeader(400)
			return
		}
		json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(server.Close)
	if e := f.s.Remove(ctx, "torznab"); e != nil {
		t.Fatal(e)
	}
	key := "private-indexer-key"
	if _, e := f.s.SaveIndexer(ctx, "", ManagedIndexerUpdate{Name: "Owned Newznab", Kind: "newznab", URL: server.URL + "/newznab", APIKey: &key, Enabled: true}); e != nil {
		t.Fatal(e)
	}
	key = "private-sab-key"
	if e := f.s.ConfigureSAB(ctx, SABUpdate{URL: server.URL, APIKey: &key, DownloadPath: "/completed"}); e != nil {
		t.Fatal(e)
	}
	return v
}
func queueUsenet(t *testing.T, f *workflowFixture, mediaID int64, tv bool) Job {
	t.Helper()
	ctx := context.Background()
	var releases []Release
	var e error
	if tv {
		releases, e = f.s.SearchTV(ctx, mediaID, 1, 0, "")
	} else {
		releases, e = f.s.Search(ctx, mediaID, "")
	}
	if e != nil || len(releases) != 1 || releases[0].Protocol != "usenet" || releases[0].Seeders != nil {
		t.Fatal("Newznab search", releases, e)
	}
	j, created, e := f.s.Queue(ctx, releases[0].ID)
	if e != nil || !created || j.Protocol != "usenet" || !strings.HasPrefix(j.Hash, "nzb:") {
		t.Fatal("NZB queue", j, e)
	}
	duplicate, created, e := f.s.Queue(ctx, releases[0].ID)
	if e != nil || created || duplicate.ID != j.ID {
		t.Fatal("non-idempotent queue", e)
	}
	return j
}
func usenetSource(t *testing.T, f *workflowFixture, j Job, name string) string {
	t.Helper()
	relative := "ReelScarey/reelscarey-" + j.ID + "/" + name
	filename := filepath.Join(f.cfg.DownloadsDir, filepath.FromSlash(relative))
	if e := os.MkdirAll(filepath.Dir(filename), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filename, []byte("owned generated unit fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	return filename
}
func TestUsenetPostProcessingRestartAndSourceRetention(t *testing.T) {
	f := setupWorkflow(t)
	v := installUsenet(t, f)
	ctx := context.Background()
	j := queueUsenet(t, f, f.movie.ID, false)
	v.mu.Lock()
	v.lost = true
	v.mu.Unlock()
	f.tick(t)
	current, _ := f.s.Job(ctx, j.ID)
	if current.attempted != 1 || current.ClientID != "" {
		t.Fatal("missing ambiguous-submission fence")
	}
	f.reopen(t)
	f.tick(t)
	current, _ = f.s.Job(ctx, j.ID)
	if current.ClientID != "SABnzbd_nzo_owned" {
		t.Fatal("lost reply was not reconciled")
	}
	source := usenetSource(t, f, j, "owned.mp4")
	before, _ := os.Stat(source)
	v.mu.Lock()
	v.state = "Extracting"
	v.storage = "/completed/ReelScarey/reelscarey-" + j.ID
	v.mu.Unlock()
	f.tick(t)
	current, _ = f.s.Job(ctx, j.ID)
	if current.State != "downloading" || f.probe.calls != 0 {
		t.Fatal("post-processing imported prematurely", current.State)
	}
	v.mu.Lock()
	v.state = "Completed"
	v.mu.Unlock()
	f.reopen(t)
	f.tick(t)
	current, e := f.s.Job(ctx, j.ID)
	if e != nil || current.State != "completed" || f.client.adds != 0 {
		t.Fatal("Usenet movie import", current.State, current.ErrorCode, e)
	}
	after, _ := os.Stat(source)
	if after == nil || !os.SameFile(before, after) || before.ModTime() != after.ModTime() {
		t.Fatal("completed source changed")
	}
	v.mu.Lock()
	adds := v.adds
	v.mu.Unlock()
	if adds != 1 {
		t.Fatal("duplicate NZB submission", adds)
	}
	details, e := f.s.Details(ctx, f.movie.ID, 1)
	if e != nil || details.FileCount != 1 || details.HistoryCount != 1 || details.Files[0].Release != "Test.Movie.2026.1080p.English" {
		t.Fatal("Usenet provenance", e)
	}
	c, e := f.s.sabConfig(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.ConfigureSAB(ctx, SABUpdate{URL: c.URL, DownloadPath: c.DownloadPath}); e != nil {
		t.Fatal("blank key failed to preserve secret", e)
	}
	var cipher []byte
	if e = f.db.QueryRow(`SELECT ciphertext FROM integration_configs WHERE name='sabnzbd'`).Scan(&cipher); e != nil || strings.Contains(string(cipher), "private-sab-key") {
		t.Fatal("secret not encrypted")
	}
}
func TestUsenetTVPackImportsAutomatically(t *testing.T) {
	f, id, _ := setupTV(t)
	v := installUsenet(t, f)
	j := queueUsenet(t, f, id, true)
	f.tick(t)
	usenetSource(t, f, j, "Show.S01E01-E02.mp4")
	usenetSource(t, f, j, "Show.S01E03.mp4")
	v.mu.Lock()
	v.state = "Completed"
	v.storage = "/completed/ReelScarey/reelscarey-" + j.ID
	v.mu.Unlock()
	f.tick(t)
	current, _ := f.s.Job(context.Background(), j.ID)
	if current.ErrorCode != "" || current.State != "importing" || len(current.Choices) != 0 {
		t.Fatal("a labeled TV pack waited for review", current.ErrorCode)
	}
	f.reopen(t)
	f.tick(t)
	current, e := f.s.Job(context.Background(), j.ID)
	if e != nil || current.State != "completed" {
		t.Fatal("TV NZB import", current.ErrorCode, e)
	}
	season, e := library.New(f.db).TVSeason(context.Background(), id, 1)
	if e != nil || season.Season.AvailableEpisodes != 3 || season.Episodes[0].FilePath != season.Episodes[1].FilePath {
		t.Fatal("shared TV file associations", e)
	}
}
func TestUsenetUncertainSubmissionNeverAutomaticallyResubmits(t *testing.T) {
	f := setupWorkflow(t)
	v := installUsenet(t, f)
	j := queueUsenet(t, f, f.movie.ID, false)
	f.tick(t)
	v.mu.Lock()
	v.missing = true
	v.mu.Unlock()
	if _, e := f.db.Exec(`UPDATE downloads SET submitted_at=unixepoch()-100 WHERE id=?`, j.ID); e != nil {
		t.Fatal(e)
	}
	f.tick(t)
	current, _ := f.s.Job(context.Background(), j.ID)
	if current.ErrorCode != "usenet_submission_unknown" {
		t.Fatal(current.ErrorCode)
	}
	if e := f.s.Retry(context.Background(), j.ID); e != nil {
		t.Fatal(e)
	}
	f.reopen(t)
	f.tick(t)
	v.mu.Lock()
	adds := v.adds
	v.mu.Unlock()
	if adds != 1 {
		t.Fatal("ambiguous NZB was resubmitted")
	}
}
func TestUsenetOutputBoundsAndClientOwnership(t *testing.T) {
	for _, kind := range []string{"outside", "duplicate", "failed", "symlink", "payload"} {
		t.Run(kind, func(t *testing.T) {
			f := setupWorkflow(t)
			v := installUsenet(t, f)
			j := queueUsenet(t, f, f.movie.ID, false)
			if kind == "payload" {
				f.db.Exec(`UPDATE downloads SET info_hash='nzb:wrong' WHERE id=?`, j.ID)
				f.tick(t)
				v.mu.Lock()
				adds := v.adds
				v.mu.Unlock()
				if adds != 0 {
					t.Fatal("invalid NZB submitted")
				}
				return
			}
			f.tick(t)
			source := usenetSource(t, f, j, "owned.mp4")
			v.mu.Lock()
			v.state = "Completed"
			v.storage = "/completed/ReelScarey/reelscarey-" + j.ID
			if kind == "outside" {
				v.storage = "/completed/owner-videos"
			}
			if kind == "duplicate" {
				v.duplicate = true
			}
			if kind == "failed" {
				v.state = "Failed"
			}
			v.mu.Unlock()
			if kind == "symlink" {
				if e := os.Symlink(source, filepath.Join(filepath.Dir(source), "link.mp4")); e != nil {
					t.Skip("symlink permission unavailable")
				}
			}
			f.tick(t)
			current, _ := f.s.Job(context.Background(), j.ID)
			if current.State != "blocked" || f.probe.calls != 0 {
				t.Fatal("unsafe output imported", current.State, current.ErrorCode)
			}
			if e := f.s.Remove(context.Background(), "sabnzbd"); e == nil {
				t.Fatal("active client disconnected")
			}
		})
	}
}
func TestNZBDescriptorValidation(t *testing.T) {
	h, e := NZBIdentity([]byte(ownedNZB))
	if e != nil || len(h) != 68 {
		t.Fatal(h, e)
	}
	for _, body := range []string{"", `<html/>`, `<nzb/>`, ownedNZB + `<nzb/>`, strings.Replace(ownedNZB, `number="1"`, `number="0"`, 1), strings.Replace(ownedNZB, `bytes="100"`, `bytes="-1"`, 1), strings.Replace(ownedNZB, `owned-fixture@invalid`, `bad id`, 1), strings.Replace(ownedNZB, `http://www.newzbin.com/DTD/2003/nzb`, `http://other.invalid`, 1), strings.Repeat("x", (4<<20)+1)} {
		if _, e := NZBIdentity([]byte(body)); e == nil {
			t.Fatal("invalid descriptor accepted")
		}
	}
}
