package automation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/quality"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

type indexer struct {
	calls   int
	err     error
	onFetch func()
	title   string
}

func TestNewznabAutomationUsesSABAndDoesNotApplyTorrentSeedRules(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/nzb":
			fmt.Fprint(w, `<nzb><file subject="owned"><groups><group>alt.test</group></groups><segments><segment bytes="10" number="1">owned@invalid</segment></segments></file></nzb>`)
		case "/api":
			if r.URL.Query().Get("mode") == "version" {
				fmt.Fprint(w, `{"version":"5.1.3"}`)
				return
			}
			if r.URL.Query().Get("mode") == "get_cats" {
				fmt.Fprint(w, `{"categories":["reelscarey"]}`)
				return
			}
			if r.URL.Query().Get("t") == "caps" {
				fmt.Fprint(w, `<caps><searching><search available="yes" supportedParams="q"/></searching></caps>`)
				return
			}
			fmt.Fprintf(w, `<rss><channel><item><title>A.Test.Movie.2020.1080p.English.WEB</title><guid>owned</guid><enclosure type="application/x-nzb" url="http://%s/nzb" length="1000"/></item></channel></rss>`, r.Host)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	if e := f.w.Remove(ctx, "torznab"); e != nil {
		t.Fatal(e)
	}
	key := "fixture-key"
	if _, e := f.w.SaveIndexer(ctx, "", transfer.ManagedIndexerUpdate{Name: "Owned Newznab", Kind: "newznab", URL: server.URL + "/api", APIKey: &key, Enabled: true}); e != nil {
		t.Fatal(e)
	}
	configure(t, f.s, false, "search", 10)
	if e := f.s.Request(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	runs, _ := f.s.Runs(ctx)
	results, _ := f.s.Results(ctx, runs[0].ID)
	if len(results) != 1 || results[0].Outcome != "match" {
		t.Fatal("NZB rejected for lack of seeds", results)
	}
	configure(t, f.s, false, "download", 10)
	if e := f.s.Request(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	var count int
	if e := f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&count); e != nil || count != 0 {
		t.Fatal("NZB routed to torrent client", count, e)
	}
	if e := f.w.ConfigureSAB(ctx, transfer.SABUpdate{URL: server.URL, APIKey: &key, DownloadPath: "/completed"}); e != nil {
		t.Fatal(e)
	}
	if e := f.s.Request(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	var protocol string
	if e := f.db.QueryRow(`SELECT protocol FROM downloads`).Scan(&protocol); e != nil || protocol != "usenet" {
		t.Fatal("NZB did not pass atomic policy guard", protocol, e)
	}
}

func (*indexer) Test(context.Context, transfer.IndexerConfig) error { return nil }
func (i *indexer) Search(context.Context, transfer.IndexerConfig, string) ([]transfer.Candidate, error) {
	i.calls++
	if i.err != nil {
		return nil, i.err
	}
	n := 5
	return []transfer.Candidate{{Release: transfer.Release{Title: i.title, Size: 2 << 30, Seeders: &n}}}, nil
}
func (i *indexer) Fetch(context.Context, transfer.IndexerConfig, transfer.Candidate) (transfer.Payload, string, error) {
	if i.onFetch != nil {
		i.onFetch()
	}
	h := strings.Repeat("a", 40)
	return transfer.Payload{Magnet: "magnet:?xt=urn:btih:" + h}, h, nil
}

type client struct{ torrent *transfer.Torrent }

func (*client) Test(context.Context, transfer.ClientConfig) (string, error) { return "test", nil }
func (c *client) Find(context.Context, transfer.ClientConfig, string) (*transfer.Torrent, error) {
	return c.torrent, nil
}
func (c *client) Add(ctx context.Context, cfg transfer.ClientConfig, tag, save string, payload transfer.Payload) error {
	hash, e := transfer.MagnetHash(payload.Magnet)
	if e != nil {
		return e
	}
	c.torrent = &transfer.Torrent{Hash: hash, Tags: tag, SavePath: save, State: "uploading", Progress: 1, Size: 32, Completed: 32}
	return nil
}
func (*client) Files(context.Context, transfer.ClientConfig, string) ([]transfer.TorrentFile, error) {
	return []transfer.TorrentFile{{Index: 0, Name: "Show.S01E01.mp4", Size: 32, Progress: 1, Priority: 1}}, nil
}

type fixture struct {
	db      *sql.DB
	s       *Service
	w       *transfer.Service
	i       *indexer
	dir     string
	cfg     config.Config
	secrets *securestore.Store
}

func setup(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, e := database.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	secrets, e := securestore.New(ctx, db, dir)
	if e != nil {
		t.Fatal(e)
	}
	i := &indexer{title: "A.Test.Movie.2020.1080p.English.WEB"}
	cfg := config.Config{ConfigDir: dir, DownloadsDir: filepath.Join(dir, "downloads"), MediaDir: filepath.Join(dir, "media")}
	w := transfer.New(db, secrets, cfg, &client{}, i)
	key, password := "test-api-key", "test-password"
	if e = w.ConfigureIndexer(ctx, transfer.IndexerUpdate{URL: "http://indexer.invalid", APIKey: &key}); e != nil {
		t.Fatal(e)
	}
	if e = w.ConfigureClient(ctx, transfer.ClientUpdate{URL: "http://client.invalid", Username: "test", Password: &password, DownloadPath: "/downloads"}); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,year,monitored,release_date) VALUES(1,'movie',1,'A Test Movie',2020,1,'2020-01-01')`); e != nil {
		t.Fatal(e)
	}
	return fixture{db: db, s: New(db, w), w: w, i: i, dir: dir, cfg: cfg, secrets: secrets}
}
func configure(t *testing.T, s *Service, enabled bool, mode string, batch int) {
	t.Helper()
	v, e := s.Get(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Update(context.Background(), Update{Enabled: enabled, Mode: mode, Interval: 15, Batch: batch, DefaultProfile: 1, Revision: v.Revision})
	if e != nil {
		t.Fatal(e)
	}
}
func TestSearchOnlyThenAutomaticQueuePersistsAndDeduplicates(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	configure(t, f.s, false, "search", 10)
	if ok, e := f.s.ProcessNext(ctx); ok || e != nil {
		t.Fatal("disabled schedule ran", ok, e)
	}
	if e := f.s.Request(ctx); e != nil {
		t.Fatal(e)
	}
	if ok, e := f.s.ProcessNext(ctx); !ok || e != nil {
		t.Fatal(ok, e)
	}
	runs, _ := f.s.Runs(ctx)
	results, _ := f.s.Results(ctx, runs[0].ID)
	if len(results) != 1 || results[0].Outcome != "match" {
		t.Fatal(results)
	}
	var count int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&count)
	if count != 0 {
		t.Fatal("search mode queued downloads")
	}
	configure(t, f.s, true, "download", 10)
	if _, e := f.s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
	f.db.Close()
	db, e := database.Open(ctx, f.dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	reopenedSecrets, e := securestore.New(ctx, db, f.dir)
	if e != nil {
		t.Fatal(e)
	}
	w := transfer.New(db, reopenedSecrets, f.cfg, &client{}, f.i)
	s := New(db, w)
	s.now = func() time.Time { return time.Now().Add(16 * time.Minute) }
	if _, e = s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	_ = db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&count)
	if count != 1 || f.i.calls != 2 {
		t.Fatal("duplicate or active target searched after restart", count, f.i.calls)
	}
}

func TestInvalidSavedDatesDoNotAuthorizeScheduledSearches(t *testing.T) {
	for _, date := range []string{"", "2026-02-30", "2025-02-29", "0000-01-01", "2020-01-01T00:00:00Z", "unknown"} {
		t.Run(date, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			if _, e := f.db.Exec(`UPDATE media_items SET release_date=? WHERE id=1`, date); e != nil {
				t.Fatal(e)
			}
			if _, e := f.db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,monitored) VALUES(2,'series',2,'Saved Show',1);INSERT INTO series_details(media_id) VALUES(2);INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored,loaded_at) VALUES(1,2,3,1,'First','','',1,1,'loaded');INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(1,1,4,1,'Episode','','',1)`); e != nil {
				t.Fatal(e)
			}
			if _, e := f.db.Exec(`UPDATE episodes SET air_date=? WHERE id=1`, date); e != nil {
				t.Fatal(e)
			}
			configure(t, f.s, false, "search", 10)
			if e := f.s.Request(ctx); e != nil {
				t.Fatal(e)
			}
			if _, e := f.s.ProcessNext(ctx); e != nil {
				t.Fatal(e)
			}
			if f.i.calls != 0 {
				t.Fatal("invalid catalog date triggered provider work", date)
			}
		})
	}
}
func TestUnconfiguredIndexerWaitsWithoutRequests(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if e := f.w.Remove(ctx, "torznab"); e != nil {
		t.Fatal(e)
	}
	configure(t, f.s, true, "download", 10)
	if _, e := f.s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	v, e := f.s.Get(ctx)
	if e != nil || v.State != "waiting_indexer" || f.i.calls != 0 {
		t.Fatal(v, e, f.i.calls)
	}
	if ok, e := f.s.ProcessNext(ctx); ok || e != nil {
		t.Fatal("waiting job busy-looped", ok, e)
	}
}

func TestManagedIndexerEnablesAutomationWithoutLegacyConnection(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if e := f.w.Remove(ctx, "torznab"); e != nil {
		t.Fatal(e)
	}
	source, e := f.w.SaveIndexer(ctx, "", transfer.ManagedIndexerUpdate{Name: "Managed source", Kind: "torznab", URL: "https://fixture.invalid/api", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	v, e := f.s.Get(ctx)
	if e != nil || !v.IndexerConnected {
		t.Fatal("managed source not available to scheduler", e)
	}
	configure(t, f.s, true, "search", 10)
	if _, e = f.s.ProcessNext(ctx); e != nil || f.i.calls != 1 {
		t.Fatal("managed source not searched", e)
	}
	if _, e = f.w.SetIndexerEnabled(ctx, source.ID, source.Revision, false); e != nil {
		t.Fatal(e)
	}
	v, e = f.s.Get(ctx)
	if e != nil || v.IndexerConnected {
		t.Fatal("disabled source remained available", e)
	}
}
func TestLeaseReclaimAndPauseFenceDuringMetainfoFetch(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	configure(t, f.s, true, "download", 10)
	f.i.onFetch = func() { configure(t, f.s, false, "search", 10) }
	if _, e := f.s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	var count int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&count)
	if count != 0 {
		t.Fatal("queued after pause")
	}
	v, _ := f.s.Get(ctx)
	if v.Enabled || v.State != "paused" {
		t.Fatal(v)
	}
	f.i.onFetch = nil
	configure(t, f.s, true, "search", 10)
	first, e := f.s.claim(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.claim(ctx); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal("concurrent claim accepted", e)
	}
	f.s.now = func() time.Time { return time.Now().Add(61 * time.Second) }
	second, e := f.s.claim(ctx)
	if e != nil || second.ID == first.ID {
		t.Fatal(second, e)
	}
	if !errors.Is(f.s.fence(ctx, first), ErrLease) {
		t.Fatal("expired owner passed fence")
	}
	runs, _ := f.s.Runs(ctx)
	if runs[1].State != "interrupted" {
		t.Fatal(runs)
	}
}
func TestProfileAndMonitoringChangesFenceAutomaticQueue(t *testing.T) {
	for _, change := range []string{"profile", "monitor", "assignment", "bulk-pause-resume", "bulk-profile-roundtrip"} {
		t.Run(change, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			configure(t, f.s, true, "download", 10)
			f.i.onFetch = func() {
				switch change {
				case "profile":
					p, _ := quality.New(f.db).Get(ctx, 1)
					p.MinSeeders = 100
					_, e := quality.New(f.db).Save(ctx, p)
					if e != nil {
						t.Fatal(e)
					}
				case "monitor":
					off := false
					if e := f.s.Assign(ctx, 1, AssignmentUpdate{Revision: 0, Monitored: &off}); e != nil {
						t.Fatal(e)
					}
				case "bulk-profile-roundtrip":
					id := int64(2)
					for _, choice := range []ProfileChoice{{ProfileID: &id}, {UseDefault: true}} {
						review, e := f.s.ReviewProfiles(ctx, "movie", []int64{1}, choice)
						if e != nil {
							t.Fatal(e)
						}
						if _, e = f.s.ApplyProfiles(ctx, applySnapshot("movie", review)); e != nil {
							t.Fatal(e)
						}
					}
				case "bulk-pause-resume":
					store := library.New(f.db)
					for _, monitored := range []bool{false, true} {
						review, e := store.ReviewMonitoring(ctx, "movie", []int64{1})
						if e != nil {
							t.Fatal(e)
						}
						v := review[0]
						expected := []library.MonitoringExpectation{{ID: v.ID, Monitored: &v.Monitored, SeriesRevision: v.SeriesRevision, AutomationRevision: v.AutomationRevision}}
						if _, e = store.ApplyMonitoring(ctx, "movie", expected, monitored); e != nil {
							t.Fatal(e)
						}
					}
				case "assignment":
					id := int64(2)
					if e := f.s.Assign(ctx, 1, AssignmentUpdate{ProfileID: &id}); e != nil {
						t.Fatal(e)
					}
				}
			}
			if _, e := f.s.ProcessNext(ctx); e != nil {
				t.Fatal(e)
			}
			var count int
			_ = f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&count)
			if count != 0 {
				t.Fatal("stale policy queued a download")
			}
		})
	}
}
func TestBoundedCursorMonitoringDatesAndTVSelections(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	_, e := f.db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,year,monitored,release_date) VALUES(2,'movie',2,'Future',2099,1,'2099-01-01'),(3,'movie',3,'Paused',2020,0,'2020-01-01'),(4,'movie',4,'Second',2020,1,'2020-01-01'),(5,'series',5,'Show',2020,1,'2020-01-01');INSERT INTO series_details(media_id) VALUES(5);INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored,loaded_at) VALUES(1,5,10,1,'Season','','2020-01-01',4,1,'2020-01-01');INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,monitored,air_date) VALUES(1,1,11,1,'One','',1,'2020-01-01'),(2,1,12,2,'Paused','',0,'2020-01-01'),(3,1,13,3,'Future','',1,'2099-01-01'),(4,1,14,4,'Unknown','',1,'')`)
	if e != nil {
		t.Fatal(e)
	}
	targets, e := f.s.targets(ctx, "", 25)
	if e != nil || len(targets) != 3 || targets[0].EpisodeID == nil {
		t.Fatal(targets, e)
	}
	cursor := targets[0].Key
	rotated, e := f.s.targets(ctx, cursor, 2)
	if e != nil || len(rotated) != 2 || rotated[0].MediaID != 1 || rotated[1].MediaID != 4 {
		t.Fatal(rotated, e)
	}
	wrapped, e := f.s.targets(ctx, rotated[1].Key, 2)
	if e != nil || len(wrapped) != 2 || wrapped[0].EpisodeID == nil || wrapped[1].MediaID != 1 {
		t.Fatal(wrapped, e)
	}
	if _, e = f.db.Exec(`UPDATE seasons SET monitored=0 WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	targets, e = f.s.targets(ctx, "", 25)
	if e != nil || len(targets) != 2 {
		t.Fatal(targets, e)
	}
}
func TestTransientFailuresBackoffAndEventuallyStopOneOffRetry(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	now := time.Now()
	f.s.now = func() time.Time { return now }
	configure(t, f.s, false, "search", 10)
	f.i.err = &transfer.Error{Code: "upstream_unavailable", Message: "test outage", Temporary: true}
	if e := f.s.Request(ctx); e != nil {
		t.Fatal(e)
	}
	for n := 1; n <= 5; n++ {
		if _, e := f.s.ProcessNext(ctx); e != nil {
			t.Fatal(e)
		}
		v, _ := f.s.Get(ctx)
		if n < 5 {
			if v.State != "retrying" || !v.Requested || v.NextRun <= now.Unix() {
				t.Fatal(v)
			}
			now = time.Unix(v.NextRun+1, 0)
		} else if v.State != "failed" || v.Requested {
			t.Fatal(v)
		}
	}
	if ok, e := f.s.ProcessNext(ctx); ok || e != nil {
		t.Fatal("exhausted one-off request ran", ok, e)
	}
}

func TestUpgradeSuggestionsPreserveRegisteredFilesAndHonorCutoff(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	configure(t, f.s, true, "download", 10)
	if _, e := f.s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	_, e := f.db.Exec(`UPDATE downloads SET state='completed';INSERT INTO media_files(id,movie_id,download_id,relative_path,size_bytes,sha256,probe_json,imported_at) SELECT 1,1,id,'Movies/existing.mkv',1024,'existing-digest','{"width":1280,"height":536}',unixepoch() FROM downloads LIMIT 1;UPDATE media_items SET status='available' WHERE id=1`)
	if e != nil {
		t.Fatal(e)
	}
	configure(t, f.s, true, "download", 10)
	if _, e = f.s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	runs, _ := f.s.Runs(ctx)
	results, _ := f.s.Results(ctx, runs[0].ID)
	if len(results) != 1 || results[0].Outcome != "upgrade_available" || results[0].DownloadID != nil {
		t.Fatal(results)
	}
	var count int
	var path, hash string
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&count)
	_ = f.db.QueryRow(`SELECT relative_path,sha256 FROM media_files WHERE id=1`).Scan(&path, &hash)
	if count != 1 || path != "Movies/existing.mkv" || hash != "existing-digest" {
		t.Fatal("upgrade changed registered media", count, path, hash)
	}
	if _, e = f.db.Exec(`UPDATE media_files SET probe_json='{"width":1920,"height":800}' WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	calls := f.i.calls
	configure(t, f.s, true, "download", 10)
	if _, e = f.s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	if f.i.calls != calls {
		t.Fatal("searched a file already meeting cutoff")
	}
}

func TestAutomaticTVUsesReservationsAndStillRequiresMappingReview(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	_, e := f.db.Exec(`DELETE FROM media_items;INSERT INTO media_items(id,kind,tmdb_id,title,year,monitored) VALUES(5,'series',5,'Show',2020,1);INSERT INTO series_details(media_id) VALUES(5);INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored,loaded_at) VALUES(1,5,10,1,'Season','','2020-01-01',1,1,'2020-01-01');INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(1,1,11,1,'One','','2020-01-01',1)`)
	if e != nil {
		t.Fatal(e)
	}
	f.i.title = "Show.S01E01.1080p.English.WEB"
	configure(t, f.s, true, "download", 10)
	if _, e = f.s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	jobs, e := f.w.List(ctx, 1)
	if e != nil || len(jobs.Items) != 1 || jobs.Items[0].Kind != "series" || len(jobs.Items[0].Targets) != 1 {
		t.Fatal(jobs, e)
	}
	job := jobs.Items[0]
	if _, e = f.w.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = f.db.Exec(`UPDATE downloads SET next_run=0`); e != nil {
		t.Fatal(e)
	}
	if _, e = f.w.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	job, e = f.w.Job(ctx, job.ID)
	if e != nil || job.State != "blocked" || job.ErrorCode != "map_episodes" {
		t.Fatal(job.State, job.ErrorCode, e)
	}
	var count int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM media_files`).Scan(&count)
	if count != 0 {
		t.Fatal("unreviewed automatic episode was imported")
	}
	// Simulate an already registered lower-resolution file to exercise TV upgrade
	// discovery. No real media is replaced or opened by this scheduler test.
	_, e = f.db.Exec(`UPDATE downloads SET state='completed';INSERT INTO media_files(id,series_id,download_id,file_index,relative_path,size_bytes,sha256,probe_json,imported_at) SELECT 1,5,id,0,'TV/existing.mp4',32,'existing','{"width":1280,"height":720}',unixepoch() FROM downloads;INSERT INTO episode_files(episode_id,media_file_id) VALUES(1,1)`)
	if e != nil {
		t.Fatal(e)
	}
	configure(t, f.s, true, "download", 10)
	if _, e = f.s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	runs, _ := f.s.Runs(ctx)
	results, _ := f.s.Results(ctx, runs[0].ID)
	if len(results) != 1 || results[0].Outcome != "upgrade_available" {
		t.Fatal(results)
	}
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&count)
	if count != 1 {
		t.Fatal("TV upgrade queued a replacement", count)
	}
}

func TestInterruptedOneOffRetainsIntentForRestart(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	configure(t, f.s, false, "download", 10)
	if e := f.s.Request(ctx); e != nil {
		t.Fatal(e)
	}
	f.i.onFetch = cancel
	if _, e := f.s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	v, e := f.s.Get(context.Background())
	if e != nil || !v.Requested || v.Enabled || v.State != "interrupted" {
		t.Fatal(v, e)
	}
	f.i.onFetch = nil
	if _, e = f.s.ProcessNext(context.Background()); e != nil {
		t.Fatal(e)
	}
	var count int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&count)
	if count != 1 {
		t.Fatal("one-off intent lost across cancellation", count)
	}
}
