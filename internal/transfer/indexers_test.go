package transfer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type managedFixture struct {
	fail    bool
	fetched string
}

func (f *managedFixture) Test(context.Context, IndexerConfig) error {
	if f.fail {
		return unavailable("Fixture offline.")
	}
	return nil
}
func (f *managedFixture) Search(_ context.Context, c IndexerConfig, _ string) ([]Candidate, error) {
	if f.fail {
		return nil, unavailable("Fixture offline.")
	}
	out := []Candidate{}
	for i := range 100 {
		out = append(out, Candidate{Release: Release{Title: fmt.Sprintf("Test Movie 2026 1080p %d", i)}, Magnet: testMagnet})
	}
	return out, nil
}
func (f *managedFixture) Fetch(_ context.Context, c IndexerConfig, v Candidate) (Payload, string, error) {
	f.fetched = c.ID
	return Payload{Magnet: v.Magnet}, testHash, nil
}

func TestManagedIndexersEncryptedLifecycleAndSourceFencing(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	if e := f.s.DeleteIndexer(ctx, "legacy", 1); e != nil {
		t.Fatal(e)
	}
	adapter := &managedFixture{}
	f.s.adapters["iptorrents"] = adapter
	secret := "session=generated-fixture-secret"
	in := ManagedIndexerUpdate{Name: "Private fixture", Kind: "iptorrents", URL: "https://tracker.invalid", Enabled: true, Cookie: &secret, UserAgent: "Fixture browser"}
	a, e := f.s.SaveIndexer(ctx, "", in)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(a)
	if strings.Contains(string(raw), secret) || !a.HasCookie || a.Revision != 1 {
		t.Fatal("secret/status contract failed")
	}
	var sealed []byte
	if e = f.db.QueryRow(`SELECT ciphertext FROM indexers WHERE id=?`, a.ID).Scan(&sealed); e != nil || strings.Contains(string(sealed), secret) {
		t.Fatal("plaintext credential stored", e)
	}
	releases, e := f.s.Search(ctx, f.movie.ID, "")
	if e != nil || len(releases) != 100 || releases[0].IndexerID != a.ID {
		t.Fatal("source lost", e)
	}
	adapter.fail = true
	disabled, e := f.s.SetIndexerEnabled(ctx, a.ID, a.Revision, false)
	if e != nil || disabled.Enabled {
		t.Fatal("offline disable failed", e)
	}
	if _, _, e = f.s.Queue(ctx, releases[0].ID); e == nil {
		t.Fatal("disabled selection queued")
	}
	if _, e = f.s.SetIndexerEnabled(ctx, a.ID, a.Revision, true); e == nil {
		t.Fatal("stale revision accepted")
	}
	adapter.fail = false
	a, e = f.s.SetIndexerEnabled(ctx, a.ID, disabled.Revision, true)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = f.s.Queue(ctx, releases[0].ID); e == nil {
		t.Fatal("rotated generation queued")
	}
	releases, e = f.s.Search(ctx, f.movie.ID, "")
	if e != nil {
		t.Fatal(e)
	}
	job, _, e := f.s.Queue(ctx, releases[0].ID)
	if e != nil || adapter.fetched != a.ID {
		t.Fatal("wrong source fetched", e)
	}
	if e = f.s.DeleteIndexer(ctx, a.ID, a.Revision); e != nil {
		t.Fatal(e)
	}
	f.tick(t)
	if _, e = f.s.Job(ctx, job.ID); e != nil || f.client.adds != 1 {
		t.Fatal("removing source broke queued download", e)
	}
	details, e := f.s.Details(ctx, f.movie.ID, 1)
	if e != nil || len(details.History) != 1 || details.History[0].Indexer != "Private fixture" {
		t.Fatal("source history lost", e)
	}
}

func TestMultiIndexerFairnessPartialFailureAndSecretPreservation(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	_ = f.s.DeleteIndexer(ctx, "legacy", 1)
	good, bad := &managedFixture{}, &managedFixture{}
	f.s.adapters["iptorrents"] = good
	f.s.adapters["xspeeds"] = bad
	secret := "session=fixture"
	sources := []IndexerStatus{}
	for _, kind := range []string{"iptorrents", "xspeeds", "iptorrents"} {
		a, e := f.s.SaveIndexer(ctx, "", ManagedIndexerUpdate{Name: kind, Kind: kind, URL: "https://tracker.invalid", Enabled: true, Cookie: &secret, UserAgent: "browser"})
		if e != nil {
			t.Fatal(e)
		}
		sources = append(sources, a)
	}
	items, e := f.s.searchIndexers(ctx, "fixture", "2000")
	if e != nil || len(items) != 200 {
		t.Fatal(len(items), e)
	}
	seen := map[string]bool{}
	for _, v := range items[:3] {
		seen[v.IndexerID] = true
	}
	if len(seen) != 3 {
		t.Fatal("source starvation")
	}
	bad.fail = true
	items, e = f.s.searchIndexers(ctx, "fixture", "2000")
	if e != nil || len(items) != 200 || len(f.s.IndexerWarnings(ctx)) != 1 {
		t.Fatal("partial failure lost healthy sources", e)
	}
	a := sources[0]
	updated, e := f.s.SaveIndexer(ctx, a.ID, ManagedIndexerUpdate{Name: "Renamed", Kind: a.Kind, URL: a.URL, Enabled: true, Revision: a.Revision, UserAgent: a.UserAgent})
	if e != nil || !updated.HasCookie {
		t.Fatal("blank edit lost saved credential", e)
	}
	if _, e = f.s.SaveIndexer(ctx, a.ID, ManagedIndexerUpdate{Name: "stale", Kind: a.Kind, URL: a.URL, Revision: 1}); e == nil {
		t.Fatal("stale edit accepted")
	}
	_, e = f.s.SaveIndexer(ctx, updated.ID, ManagedIndexerUpdate{Name: "Moved", Kind: a.Kind, URL: "https://other.invalid", Enabled: true, Revision: updated.Revision, UserAgent: a.UserAgent})
	var p *Error
	if !errors.As(e, &p) || p.Code != "invalid_settings" {
		t.Fatal("old cookie forwarded to changed origin", e)
	}
	good.fail = true
	if _, e = f.s.searchIndexers(ctx, "fixture", "2000"); e == nil {
		t.Fatal("all failures presented as empty success")
	}
}

func TestItemDetailsKeepsImportedFactsAcrossRestart(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	j := f.queue(t)
	f.tick(t)
	data := []byte(strings.Repeat("generated disposable video", 1000))
	f.complete(t, j, data)
	f.tick(t)
	if _, e := f.db.Exec(`INSERT INTO media_enrichment(media_id,payload,updated_at) VALUES(?,?,123)`, f.movie.ID, `{"tagline":"Saved fixture","genres":[],"cast":[],"crew":[]}`); e != nil {
		t.Fatal(e)
	}
	f.reopen(t)
	details, e := f.s.Details(ctx, f.movie.ID, 1)
	if e != nil || details.FileCount != 1 || details.TotalSize != int64(len(data)) || len(details.Files) != 1 || len(details.History) != 1 || details.Information == nil || details.Information.Tagline != "Saved fixture" {
		t.Fatalf("details: %+v %v", details, e)
	}
	file := details.Files[0]
	if file.Probe.Height != 1080 || !strings.HasPrefix(file.Path, f.cfg.MediaDir) || file.Release != "Test.Movie.2026.1080p" || file.DownloadID != j.ID {
		t.Fatal("incorrect import facts", file)
	}
	details, e = f.s.Details(ctx, f.movie.ID, 2)
	if e != nil || len(details.Files) != 0 || details.FileCount != 1 || details.HistoryCount != 1 {
		t.Fatal("pagination changed totals", e)
	}
}
