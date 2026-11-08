package transfer

import (
	"context"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
)

func savePublicPause(t *testing.T, f *workflowFixture, in PublicPauseUpdate) PublicPause {
	t.Helper()
	if in.Revision == 0 {
		in.Revision = 1
	}
	out, err := f.s.SavePublicPause(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func publicPauseEventCount(t *testing.T, f *workflowFixture, hash string) int {
	t.Helper()
	var count int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE kind='download' AND object_id=? AND state=?`, hash, publicPauseEventState).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestPublicPauseDefaultsAndRevisionGuard(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	p, err := f.s.PublicPause(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.Enabled || p.Revision != 1 || p.UpdatedAt < 1 {
		t.Fatal(p)
	}
	if _, err := f.s.SavePublicPause(ctx, PublicPauseUpdate{Enabled: true}); problemCode(err) != "revision_conflict" {
		t.Fatal("missing revision accepted", err)
	}
	if _, err := f.s.SavePublicPause(ctx, PublicPauseUpdate{Enabled: true, Revision: 0}); problemCode(err) != "revision_conflict" {
		t.Fatal("zero revision accepted", err)
	}
	saved := savePublicPause(t, f, PublicPauseUpdate{Enabled: true, Revision: 1})
	if !saved.Enabled || saved.Revision != 2 || saved.UpdatedAt < 1 {
		t.Fatal(saved)
	}
	if _, err := f.s.SavePublicPause(ctx, PublicPauseUpdate{Enabled: false, Revision: 1}); problemCode(err) != "revision_conflict" {
		t.Fatal("stale revision accepted", err)
	}
}

func TestPublicPauseStopsOnlyFinishedPublicSources(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	enableFixtureBuiltin(t, f)
	savePublicPause(t, f, PublicPauseUpdate{Enabled: true, Revision: 1})

	// ReelScarey jobs: the queue recorded a public Nyaa source for one hash
	// and a private source for the other.
	if _, e := f.db.Exec(`INSERT INTO release_candidates(id,movie_id,title,size_bytes,published_at,payload,expires_at) VALUES
		('rel-pub',?,'Public Job',100,'2026-01-01T00:00:00Z',x'00',unixepoch()+3600),
		('rel-priv',?,'Private Job',100,'2026-01-01T00:00:00Z',x'00',unixepoch()+3600)`, f.movie.ID, f.movie.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := f.db.Exec(`INSERT INTO downloads(id,movie_id,release_id,title,info_hash,payload,client_url,save_path,state,created_at,updated_at,indexer_name,protocol,indexer_public)
		VALUES('pubjob',?,'rel-pub','Public Job',?,x'00','http://client.invalid','/downloads','completed',1,1,'Nyaa','torrent',1),
		('privjob',?,'rel-priv','Private Job',?,x'00','http://client.invalid','/downloads','completed',1,1,'IPTorrents','torrent',0)`, f.movie.ID, strings.ToUpper(testHash), f.movie.ID, otherHash); e != nil {
		t.Fatal(e)
	}
	f.client.list = []Torrent{
		{Hash: testHash, Name: "Public.Job", State: "uploading", Progress: 1, AmountLeft: 0, Tags: "reelscarey-pubjob"},
		{Hash: otherHash, Name: "Private.Job", State: "uploading", Progress: 1, AmountLeft: 0, Tags: "reelscarey-privjob"},
		{Hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Name: "Manual.Add", State: "stalledUP", Progress: 1, AmountLeft: 0, Tags: "reelscarey-manual"},
		{Hash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Name: "External", State: "uploading", Progress: 1, AmountLeft: 0, Tags: "linux"},
		{Hash: "cccccccccccccccccccccccccccccccccccccccc", Name: "Incomplete", State: "downloading", Progress: 0.5, AmountLeft: 100, Tags: "reelscarey-manual"},
		{Hash: "dddddddddddddddddddddddddddddddddddddddd", Name: "Already.Paused", State: "stoppedUP", Progress: 1, AmountLeft: 0, Tags: "reelscarey-manual"},
	}
	paused, err := f.s.RunPublicPause(ctx)
	if err != nil || paused != 2 {
		t.Fatal(paused, err)
	}
	// Each torrent is paused in its own client call; the fixture records the
	// last one. History proves both were handled.
	if f.client.action != "pause" {
		t.Fatal(f.client.action, f.client.lastHashes)
	}
	// The last pass paused the newest qualifying torrent; both pauses were
	// recorded in History.
	if got := publicPauseEventCount(t, f, testHash); got != 1 {
		t.Fatal("public job history", got)
	}
	if got := publicPauseEventCount(t, f, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); got != 1 {
		t.Fatal("manual history", got)
	}
	if got := publicPauseEventCount(t, f, otherHash); got != 0 {
		t.Fatal("private job was paused", got)
	}

	// A paused torrent is skipped, so a repeated pass does nothing new.
	f.client.action = ""
	f.client.lastHashes = nil
	paused, err = f.s.RunPublicPause(ctx)
	if err != nil || paused != 0 || f.client.action != "" {
		t.Fatal(paused, err, f.client.action)
	}
}

func TestPublicPauseDisabledPassDoesNothing(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	enableFixtureBuiltin(t, f)
	f.client.list = []Torrent{{Hash: testHash, Name: "Manual.Add", State: "uploading", Progress: 1, AmountLeft: 0, Tags: "reelscarey-manual"}}
	paused, err := f.s.RunPublicPause(ctx)
	if err != nil || paused != 0 || f.client.action != "" {
		t.Fatal(paused, err, f.client.action)
	}
}

func TestPublicPauseQuietWhenClientUnavailable(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	savePublicPause(t, f, PublicPauseUpdate{Enabled: true, Revision: 1})
	if _, err := f.s.RunPublicPause(ctx); problemCode(err) != "builtin_unavailable" {
		t.Fatal("manual run hid the client error", err)
	}
}

// The public-source classification is recorded when a download is queued: the
// public Nyaa indexer and manual uploads are public; credential-backed
// indexer kinds are private.
type sourceAdapter struct{ hash string }

func (*sourceAdapter) Test(context.Context, IndexerConfig) error { return nil }
func (*sourceAdapter) Search(_ context.Context, _ IndexerConfig, _ string) ([]Candidate, error) {
	return []Candidate{{Release: Release{Title: "Test Movie 2026 1080p"}, Magnet: "magnet:?xt=urn:btih:" + testHash}}, nil
}
func (a *sourceAdapter) Fetch(_ context.Context, _ IndexerConfig, v Candidate) (Payload, string, error) {
	return Payload{Magnet: v.Magnet}, a.hash, nil
}

func TestQueueRecordsPublicSource(t *testing.T) {
	ctx := context.Background()
	f := setupWorkflow(t)
	if e := f.s.DeleteIndexer(ctx, "legacy", 1); e != nil {
		t.Fatal(e)
	}
	f.s.adapters["nyaa"] = &sourceAdapter{hash: strings.Repeat("a", 40)}
	f.s.adapters["iptorrents"] = &sourceAdapter{hash: strings.Repeat("b", 40)}
	nyaa, e := f.s.SaveIndexer(ctx, "", ManagedIndexerUpdate{Name: "Nyaa fixture", Kind: "nyaa", URL: "https://nyaa.invalid", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	releases, e := f.s.Search(ctx, f.movie.ID, "")
	if e != nil || len(releases) == 0 {
		t.Fatal(releases, e)
	}
	first := releases[0].ID
	for _, r := range releases {
		if r.IndexerID == nyaa.ID {
			first = r.ID
			break
		}
	}
	job, _, e := f.s.Queue(ctx, first)
	if e != nil {
		t.Fatal(e)
	}
	var public int
	if e = f.db.QueryRow(`SELECT indexer_public FROM downloads WHERE id=?`, job.ID).Scan(&public); e != nil || public != 1 {
		t.Fatal("the Nyaa job was not recorded as public", public, e)
	}

	private, e := f.s.SaveIndexer(ctx, "", ManagedIndexerUpdate{Name: "Private fixture", Kind: "iptorrents", URL: "https://tracker.invalid", Enabled: true, Cookie: &[]string{"session=fixture-secret"}[0], UserAgent: "Fixture browser"})
	if e != nil {
		t.Fatal(e)
	}
	releases, e = f.s.Search(ctx, f.movie.ID, "")
	if e != nil || len(releases) == 0 {
		t.Fatal(releases, e)
	}
	second := releases[0].ID
	for _, r := range releases {
		if r.IndexerID == private.ID {
			second = r.ID
			break
		}
	}
	// The movie queue is exclusive, so the first job is cancelled before the
	// private-indexer job is queued for the same movie.
	if _, e = f.db.Exec(`UPDATE downloads SET state='cancelled' WHERE id=?`, job.ID); e != nil {
		t.Fatal(e)
	}
	job, _, e = f.s.Queue(ctx, second)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.db.QueryRow(`SELECT indexer_public FROM downloads WHERE id=?`, job.ID).Scan(&public); e != nil || public != 0 {
		t.Fatal("the private job was recorded as public", public, e)
	}

	// A fresh movie avoids the first job's version reservation.
	year := 2027
	secondMovie, _, e := library.New(f.db).AddMovie(ctx, metadata.Movie{TMDbID: 456, Title: "Second: Movie", Year: &year}, true)
	if e != nil {
		t.Fatal(e)
	}
	torrent, _ := singleFileTorrent("Manual Fixture", 1000)
	manual, _, e := f.s.QueueManualTorrent(ctx, secondMovie.ID, "", torrent)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.db.QueryRow(`SELECT indexer_public FROM downloads WHERE id=?`, manual.ID).Scan(&public); e != nil || public != 1 {
		t.Fatal("the manual upload was not recorded as public", public, e)
	}
}
