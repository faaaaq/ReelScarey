package transfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

type fixtureIndexer struct{ query, category, hash string }

func (*fixtureIndexer) Test(context.Context, IndexerConfig) error { return nil }
func (f *fixtureIndexer) Search(_ context.Context, c IndexerConfig, q string) ([]Candidate, error) {
	f.query = q
	f.category = c.Category
	return []Candidate{{Release: Release{Title: "Test.Movie.2026.1080p", Size: 12345, Published: "2026-10-22T10:00:00Z", InfoURL: "https://indexer.invalid/details/fixture"}, Magnet: testMagnet, Generation: c.Generation}}, nil
}
func (f *fixtureIndexer) Recent(ctx context.Context, c IndexerConfig) ([]Candidate, error) {
	return f.Search(ctx, c, "")
}
func (f *fixtureIndexer) Fetch(_ context.Context, _ IndexerConfig, v Candidate) (Payload, string, error) {
	if f.hash != "" {
		return Payload{Magnet: "magnet:?xt=urn:btih:" + f.hash}, f.hash, nil
	}
	return Payload{Magnet: v.Magnet}, testHash, nil
}

type fixtureClient struct {
	torrent      *Torrent
	files        []TorrentFile
	adds         int
	lostResponse bool
	offline      bool
	transfer     TransferInfo
	list         []Torrent
	action       string
	deleteFiles  bool
	lastTag      string
	lastSave     string
	lastPayload  Payload
	lastHashes   []string
	lastValue    string
	commands     int
	commandErr   error
	altSpeed     bool
	toggles      int
	globalLimits map[bool]int64
	server       TorrentServer
	properties   TorrentProperties
	trackers     []TorrentTracker
	peers        []TorrentPeer
	webseeds     []TorrentWebSeed
	pieces       []int
	beforeAdd    func()
}

func (*fixtureClient) Test(context.Context, ClientConfig) (string, error) { return "2.11.3", nil }
func (f *fixtureClient) Find(_ context.Context, _ ClientConfig, hash string) (*Torrent, error) {
	if f.offline {
		return nil, unavailable("test client offline")
	}
	for _, item := range f.list {
		if strings.EqualFold(item.Hash, hash) {
			return &item, nil
		}
	}
	return f.torrent, nil
}
func (f *fixtureClient) Add(_ context.Context, _ ClientConfig, tag, save string, p Payload) error {
	if f.beforeAdd != nil {
		f.beforeAdd()
	}
	f.adds++
	f.lastTag, f.lastSave, f.lastPayload = tag, save, p
	f.torrent = &Torrent{Hash: testHash, Tags: tag, SavePath: save, State: "downloading", AmountLeft: 10}
	if f.lostResponse {
		return unavailable("test lost response")
	}
	return nil
}
func (f *fixtureClient) Files(context.Context, ClientConfig, string) ([]TorrentFile, error) {
	return f.files, nil
}
func (f *fixtureClient) TransferInfo(context.Context, ClientConfig) (TransferInfo, error) {
	if f.offline {
		return TransferInfo{}, unavailable("test client offline")
	}
	return f.transfer, nil
}
func (f *fixtureClient) List(context.Context, ClientConfig) ([]Torrent, error) {
	if f.offline {
		return nil, unavailable("test client offline")
	}
	return f.list, nil
}
func (f *fixtureClient) Pause(_ context.Context, _ ClientConfig, hashes []string) error {
	f.action, f.lastHashes = "pause", append([]string(nil), hashes...)
	return nil
}
func (f *fixtureClient) Resume(_ context.Context, _ ClientConfig, hashes []string) error {
	f.action, f.lastHashes = "resume", append([]string(nil), hashes...)
	return nil
}
func (f *fixtureClient) Recheck(_ context.Context, _ ClientConfig, hashes []string) error {
	f.action, f.lastHashes = "recheck", append([]string(nil), hashes...)
	return nil
}
func (f *fixtureClient) Delete(_ context.Context, _ ClientConfig, hashes []string, deleteFiles bool) error {
	f.action, f.deleteFiles = "remove", deleteFiles
	f.lastHashes = append([]string(nil), hashes...)
	return nil
}
func (f *fixtureClient) Command(_ context.Context, _ ClientConfig, command string, hashes []string, value string) error {
	f.action, f.lastHashes, f.lastValue = command, append([]string(nil), hashes...), value
	f.commands++
	return f.commandErr
}
func (f *fixtureClient) SetShareLimits(_ context.Context, _ ClientConfig, hashes []string, ratio float64, minutes int64) error {
	f.action, f.lastHashes, f.lastValue = "share_limits", hashes, fmt.Sprint(ratio, "/", minutes)
	return nil
}
func (f *fixtureClient) SetFilePriority(_ context.Context, _ ClientConfig, hash string, indexes []int, priority int) error {
	f.action, f.lastValue = "file_priority", fmt.Sprint(hash, indexes, priority)
	return nil
}
func (f *fixtureClient) SpeedLimitsMode(context.Context, ClientConfig) (bool, error) {
	return f.altSpeed, nil
}
func (f *fixtureClient) ToggleSpeedLimitsMode(context.Context, ClientConfig) error {
	f.altSpeed = !f.altSpeed
	f.toggles++
	return nil
}
func (f *fixtureClient) SetGlobalLimit(_ context.Context, _ ClientConfig, upload bool, limit int64) error {
	if f.globalLimits == nil {
		f.globalLimits = map[bool]int64{}
	}
	f.globalLimits[upload] = limit
	return nil
}
func (f *fixtureClient) ServerPreferences(context.Context, ClientConfig) (TorrentServer, error) {
	return f.server, nil
}
func (f *fixtureClient) SetServerQueueLimits(_ context.Context, _ ClientConfig, in TorrentQueueLimits) error {
	f.server.QueueingEnabled = in.QueueingEnabled
	f.server.MaxActiveDownloads = in.MaxActiveDownloads
	f.server.MaxActiveUploads = in.MaxActiveUploads
	f.server.MaxActiveTorrents = in.MaxActiveTorrents
	return nil
}
func (f *fixtureClient) Properties(context.Context, ClientConfig, string) (TorrentProperties, error) {
	return f.properties, nil
}
func (f *fixtureClient) Trackers(context.Context, ClientConfig, string) ([]TorrentTracker, error) {
	return f.trackers, nil
}
func (f *fixtureClient) Peers(context.Context, ClientConfig, string) ([]TorrentPeer, error) {
	return f.peers, nil
}
func (f *fixtureClient) WebSeeds(context.Context, ClientConfig, string) ([]TorrentWebSeed, error) {
	return f.webseeds, nil
}
func (f *fixtureClient) PieceStates(context.Context, ClientConfig, string) ([]int, error) {
	return f.pieces, nil
}

type fixtureProbe struct {
	calls int
	err   error
}

func (f *fixtureProbe) Probe(context.Context, *os.File) (Probe, error) {
	f.calls++
	return Probe{Codec: "h264", Width: 1920, Height: 1080, Duration: 90}, f.err
}

type workflowFixture struct {
	s       *Service
	db      *sql.DB
	cfg     config.Config
	client  *fixtureClient
	indexer *fixtureIndexer
	probe   *fixtureProbe
	movie   library.Item
}

func setupWorkflow(t *testing.T) *workflowFixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	cfg := config.Config{ConfigDir: filepath.Join(root, "config"), MediaDir: filepath.Join(root, "media"), DownloadsDir: filepath.Join(root, "downloads")}
	for _, p := range []string{cfg.MediaDir, cfg.DownloadsDir} {
		if e := os.MkdirAll(p, 0755); e != nil {
			t.Fatal(e)
		}
	}
	db, e := database.Open(ctx, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	secrets, e := securestore.New(ctx, db, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	client, indexer, probe := &fixtureClient{}, &fixtureIndexer{}, &fixtureProbe{}
	s := New(db, secrets, cfg, client, indexer)
	s.importer.Prober = probe
	apiKey, password := "indexer-test-private-key", "client-test-password"
	if e = s.ConfigureIndexer(ctx, IndexerUpdate{URL: "https://indexer.invalid/api", APIKey: &apiKey}); e != nil {
		t.Fatal(e)
	}
	if e = s.ConfigureClient(ctx, ClientUpdate{URL: "https://qbt.invalid", Username: "testadmin", Password: &password, DownloadPath: "/downloads"}); e != nil {
		t.Fatal(e)
	}
	year := 2026
	movie, _, e := library.New(db).AddMovie(ctx, metadata.Movie{TMDbID: 123, Title: "Test: Movie", Year: &year}, true)
	if e != nil {
		t.Fatal(e)
	}
	return &workflowFixture{s: s, db: db, cfg: cfg, client: client, indexer: indexer, probe: probe, movie: movie}
}
func (f *workflowFixture) queue(t *testing.T) Job {
	t.Helper()
	out, e := f.s.Search(context.Background(), f.movie.ID, "")
	if e != nil || len(out) != 1 {
		t.Fatal(out, e)
	}
	job, created, e := f.s.Queue(context.Background(), out[0].ID)
	if e != nil || !created {
		t.Fatal(job, e)
	}
	duplicate, created, e := f.s.Queue(context.Background(), out[0].ID)
	if e != nil || created || duplicate.ID != job.ID {
		t.Fatal("queue retry was not idempotent", e)
	}
	return job
}
func (f *workflowFixture) tick(t *testing.T) {
	t.Helper()
	if _, e := f.db.Exec(`UPDATE downloads SET next_run=0`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.ProcessNext(context.Background()); e != nil {
		t.Fatal(e)
	}
}
func (f *workflowFixture) source(t *testing.T, j Job, name string, data []byte) string {
	t.Helper()
	relative := path.Join("ReelScarey", j.ID, name)
	absolute := filepath.Join(f.cfg.DownloadsDir, filepath.FromSlash(relative))
	if e := os.MkdirAll(filepath.Dir(absolute), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(absolute, data, 0644); e != nil {
		t.Fatal(e)
	}
	return relative
}
func (f *workflowFixture) complete(t *testing.T, j Job, data []byte) {
	f.source(t, j, "movie.mkv", data)
	f.client.torrent.Progress = 1
	f.client.torrent.AmountLeft = 0
	f.client.torrent.State = "uploading"
	f.client.torrent.Size = int64(len(data))
	f.client.torrent.Completed = int64(len(data))
	f.client.files = []TorrentFile{{Index: 0, Name: "movie.mkv", Size: int64(len(data)), Progress: 1, Priority: 1}}
}
func (f *workflowFixture) reopen(t *testing.T) {
	t.Helper()
	f.db.Close()
	db, e := database.Open(context.Background(), f.cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	f.db = db
	secrets, e := securestore.New(context.Background(), db, f.cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	f.s = New(db, secrets, f.cfg, f.client, f.indexer)
	f.s.importer.Prober = f.probe
}
func TestMovieDownloadImportAndIdempotency(t *testing.T) {
	for _, copyMode := range []bool{false, true} {
		t.Run(map[bool]string{false: "prefer-hardlink", true: "cross-filesystem-copy"}[copyMode], func(t *testing.T) {
			f := setupWorkflow(t)
			if copyMode {
				f.s.importer.link = func(*os.File, *os.File, string) error { return errors.New("test cross-device link") }
			}
			j := f.queue(t)
			if f.indexer.query != "Test: Movie 2026" {
				t.Fatal(f.indexer.query)
			}
			var published, info string
			if e := f.db.QueryRow(`SELECT published_at,info_url FROM release_candidates`).Scan(&published, &info); e != nil || published != "2026-10-22T10:00:00Z" || info != "https://indexer.invalid/details/fixture" {
				t.Fatal("release dates/details were not persisted", published, info, e)
			}
			f.tick(t)
			data := []byte(strings.Repeat("test video bytes ", 10000))
			f.complete(t, j, data)
			f.tick(t)
			job, e := f.s.Job(context.Background(), j.ID)
			if e != nil || job.State != "completed" || f.client.adds != 1 {
				t.Fatal(job, e)
			}
			queue, e := f.s.List(context.Background(), 1)
			if e != nil || len(queue.Items) != 0 || queue.Total != 0 {
				t.Fatal("completed job remained in the queue list", queue, e)
			}
			var completedEvents int
			if e = f.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='download' AND object_id=? AND state='completed'`, j.ID).Scan(&completedEvents); e != nil || completedEvents != 1 {
				t.Fatal("completed job has no single History event", completedEvents, e)
			}
			var relative, mode, phase string
			if e = f.db.QueryRow(`SELECT relative_path FROM media_files WHERE download_id=?`, j.ID).Scan(&relative); e != nil {
				t.Fatal(e)
			}
			if e = f.db.QueryRow(`SELECT mode,phase FROM import_operations WHERE download_id=?`, j.ID).Scan(&mode, &phase); e != nil {
				t.Fatal(e)
			}
			dest := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(relative))
			saved, e := os.ReadFile(dest)
			if e != nil || string(saved) != string(data) || phase != "committed" {
				t.Fatal("bad publication", e, phase)
			}
			source := filepath.Join(f.cfg.DownloadsDir, "ReelScarey", j.ID, "movie.mkv")
			original, _ := os.ReadFile(source)
			if string(original) != string(data) {
				t.Fatal("source modified")
			}
			a, _ := os.Stat(source)
			b, _ := os.Stat(dest)
			if copyMode && (mode != "copy" || os.SameFile(a, b)) {
				t.Fatal("cross-device copy was not exercised")
			}
			if runtime.GOOS == "linux" && !copyMode && (mode != "hardlink" || !os.SameFile(a, b)) {
				t.Fatal("Linux anchored hardlink did not work", mode)
			}
			f.reopen(t)
			f.tick(t)
			item, e := library.New(f.db).ByID(context.Background(), f.movie.ID)
			if e != nil || item.Status != "available" {
				t.Fatal(item, e)
			}
			completed, e := f.s.Job(context.Background(), j.ID)
			if e != nil || completed.State != "completed" {
				t.Fatal("direct completed job read failed", completed, e)
			}
			if e = f.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='download' AND object_id=? AND state='completed'`, j.ID).Scan(&completedEvents); e != nil || completedEvents != 1 {
				t.Fatal("repeat tick duplicated the completion event", completedEvents, e)
			}
		})
	}
}
func TestCopyImportPreferenceWritesIndependentFile(t *testing.T) {
	f := setupWorkflow(t)
	if _, e := f.db.Exec(`UPDATE settings SET import_mode='copy'`); e != nil {
		t.Fatal(e)
	}
	j := f.queue(t)
	f.tick(t)
	data := []byte(strings.Repeat("copy-only video bytes ", 10000))
	f.complete(t, j, data)
	f.tick(t)
	job, e := f.s.Job(context.Background(), j.ID)
	if e != nil || job.State != "completed" {
		t.Fatalf("copy-mode import did not complete: %+v %v", job, e)
	}
	var relative, mode, phase string
	if e = f.db.QueryRow(`SELECT relative_path FROM media_files WHERE download_id=?`, j.ID).Scan(&relative); e != nil {
		t.Fatal(e)
	}
	if e = f.db.QueryRow(`SELECT mode,phase FROM import_operations WHERE download_id=?`, j.ID).Scan(&mode, &phase); e != nil {
		t.Fatal(e)
	}
	source := filepath.Join(f.cfg.DownloadsDir, "ReelScarey", j.ID, "movie.mkv")
	dest := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(relative))
	a, e := os.Stat(source)
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.Stat(dest)
	if e != nil {
		t.Fatal(e)
	}
	if mode != "copy" || phase != "committed" || os.SameFile(a, b) {
		t.Fatalf("copy preference was not honored: mode=%s phase=%s same=%v", mode, phase, os.SameFile(a, b))
	}
	saved, e := os.ReadFile(dest)
	if e != nil || string(saved) != string(data) {
		t.Fatal("library copy does not match the source", e)
	}
}

func TestLostAddResponseReconcilesAfterRestart(t *testing.T) {
	f := setupWorkflow(t)
	f.client.lostResponse = true
	j := f.queue(t)
	f.tick(t)
	if f.client.adds != 1 {
		t.Fatal("not submitted")
	}
	f.reopen(t)
	f.tick(t)
	job, e := f.s.Job(context.Background(), j.ID)
	if e != nil || job.State != "downloading" || f.client.adds != 1 {
		t.Fatal("ambiguous submission duplicated", job, e)
	}
}
func TestMultipleVideosRequireFileChoice(t *testing.T) {
	f := setupWorkflow(t)
	j := f.queue(t)
	f.tick(t)
	data := []byte("test media")
	f.complete(t, j, data)
	f.source(t, j, "second.mkv", data)
	f.client.files = append(f.client.files, TorrentFile{Index: 1, Name: "second.mkv", Size: int64(len(data)), Progress: 1, Priority: 1})
	f.tick(t)
	job, _ := f.s.Job(context.Background(), j.ID)
	if job.State != "blocked" || job.ErrorCode != "choose_file" || len(job.Choices) != 2 {
		t.Fatal(job)
	}
	if e := f.s.SelectFile(context.Background(), j.ID, 9); e == nil {
		t.Fatal("invalid file index accepted")
	}
	if e := f.s.SelectFile(context.Background(), j.ID, 1); e != nil {
		t.Fatal(e)
	}
	f.tick(t)
	job, _ = f.s.Job(context.Background(), j.ID)
	if job.State != "completed" {
		t.Fatal(job)
	}
}
func TestSecretsRemainEncryptedAndReplacementFailClosed(t *testing.T) {
	f := setupWorkflow(t)
	j := f.queue(t)
	for _, query := range []string{`SELECT ciphertext FROM integration_configs`, `SELECT payload FROM release_candidates`, `SELECT payload FROM downloads`} {
		rows, e := f.db.Query(query)
		if e != nil {
			t.Fatal(e)
		}
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				t.Fatal(e)
			}
			if strings.Contains(string(b), "private") || strings.Contains(string(b), "password") {
				t.Fatal("secret transport stored in cleartext")
			}
		}
		rows.Close()
	}
	status, e := f.s.Status(context.Background(), "qbittorrent")
	encoded, _ := json.Marshal(status)
	if e != nil || strings.Contains(string(encoded), "password") {
		t.Fatal("credential returned in status")
	}
	bad := ClientUpdate{URL: "https://changed.invalid", Username: "testadmin", DownloadPath: "/downloads"}
	if e = f.s.ConfigureClient(context.Background(), bad); e == nil {
		t.Fatal("credentials forwarded to changed service")
	}
	if e = f.s.Remove(context.Background(), "qbittorrent"); e == nil {
		t.Fatal("active client disconnected")
	}
	if _, e = f.s.Job(context.Background(), j.ID); e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(filepath.Join(f.cfg.ConfigDir, "secrets.key")); e != nil {
		t.Fatal(e)
	}
	if _, e = securestore.New(context.Background(), f.db, f.cfg.ConfigDir); e == nil {
		t.Fatal("encrypted integration key silently regenerated")
	}
}
func TestLeaseFencesStaleWorker(t *testing.T) {
	f := setupWorkflow(t)
	f.queue(t)
	j, e := f.s.claim(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.db.Exec(`UPDATE downloads SET lease_owner='new-worker',lease_until=unixepoch()+60 WHERE id=?`, j.ID); e != nil {
		t.Fatal(e)
	}
	if e = f.s.owned(context.Background(), j, `UPDATE downloads SET submission_attempted=1`); !errors.Is(e, ErrLease) {
		t.Fatal("stale owner can submit", e)
	}
}

func TestPayloadIdentityCheckedBeforeSubmission(t *testing.T) {
	f := setupWorkflow(t)
	j := f.queue(t)
	if _, e := f.db.Exec(`UPDATE downloads SET info_hash=? WHERE id=?`, strings.Repeat("f", 40), j.ID); e != nil {
		t.Fatal(e)
	}
	f.tick(t)
	current, e := f.s.Job(context.Background(), j.ID)
	if e != nil || current.ErrorCode != "payload_mismatch" || f.client.adds != 0 {
		t.Fatal("mismatched payload was submitted", current.ErrorCode, e)
	}
}
