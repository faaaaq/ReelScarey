package transfer

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
)

type orderTestIndexer struct {
	calls int
	query string
	fetch func()
}

func (*orderTestIndexer) Test(context.Context, IndexerConfig) error { return nil }
func (i *orderTestIndexer) Search(_ context.Context, c IndexerConfig, query string) ([]Candidate, error) {
	i.calls++
	i.query = query
	return []Candidate{{Release: Release{Title: "Owned Anime Complete Pack", Size: 1000}, Magnet: testMagnet, Generation: c.Generation}}, nil
}
func (i *orderTestIndexer) Fetch(context.Context, IndexerConfig, Candidate) (Payload, string, error) {
	if i.fetch != nil {
		i.fetch()
	}
	return Payload{Magnet: testMagnet}, testHash, nil
}

func saveTransferOrder(t *testing.T, f *workflowFixture, id int64, entries []library.AnimeOrderEntry) library.AnimeOrderSettings {
	t.Helper()
	ctx := context.Background()
	store := library.New(f.db)
	old, e := store.AnimeOrder(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	identity := library.AnimeIdentity{Provider: "anilist", ID: 9001, Title: "Owned Anime Identity", Format: "TV", URL: "https://anilist.co/anime/9001"}
	review, e := store.ReviewAnimeOrder(ctx, id, library.AnimeOrderUpdate{Revision: old.Revision, CategoryRevision: old.CategoryRevision, NumberingRevision: old.NumberingRevision, ProviderIDs: []int64{identity.ID}, Entries: entries}, []library.AnimeIdentity{identity})
	if e != nil {
		t.Fatal(e)
	}
	out, e := store.ApplyAnimeOrder(ctx, id, review.ReviewID)
	if e != nil {
		t.Fatal(e)
	}
	return out
}

func setupOrderPack(t *testing.T) (*workflowFixture, int64, []int64, library.AnimeOrderSettings, *orderTestIndexer) {
	t.Helper()
	f := setupWorkflow(t)
	ctx := context.Background()
	store := library.New(f.db)
	show := metadata.Series{Movie: metadata.Movie{TMDbID: 9010, Title: "Canonical Pack Show"}, Seasons: []metadata.SeasonSummary{{TMDbID: 9011, Number: 0, Title: "Specials", EpisodeCount: 1}, {TMDbID: 9012, Number: 1, Title: "Season 1", EpisodeCount: 1}, {TMDbID: 9013, Number: 2, Title: "Season 2", EpisodeCount: 1}}}
	item, _, e := store.AddSeries(ctx, show, true, "anime")
	if e != nil {
		t.Fatal(e)
	}
	ids := []int64{}
	entries := []library.AnimeOrderEntry{}
	for n, season := range show.Seasons {
		current, e := store.TVShow(ctx, item.ID)
		if e != nil {
			t.Fatal(e)
		}
		if e = store.SaveSeason(ctx, item.ID, metadata.Season{SeasonSummary: season, Episodes: []metadata.Episode{{TMDbID: int64(9020 + n), Number: 1, Title: season.Title + " owned episode", AirDate: "2000-01-01"}}}, current.Revision); e != nil {
			t.Fatal(e)
		}
		loaded, e := store.TVSeason(ctx, item.ID, season.Number)
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, loaded.Episodes[0].ID)
		title, label := "Pack Story", "1"
		if n == 0 {
			label = "SP1"
		}
		if n == 2 {
			title = "Pack Story Part 2"
		}
		entries = append(entries, library.AnimeOrderEntry{EpisodeID: loaded.Episodes[0].ID, ProviderID: 9001, ReleaseTitle: title, Label: label})
	}
	order := saveTransferOrder(t, f, item.ID, entries)
	indexer := &orderTestIndexer{}
	f.s.indexer = indexer
	return f, item.ID, ids, order, indexer
}

func TestReviewedAnimePackIsManualOnlyAndRejectsInvalidTargetsBeforeSearch(t *testing.T) {
	f, id, ids, order, indexer := setupOrderPack(t)
	ctx := context.Background()
	for _, bad := range [][]int64{nil, {ids[0], ids[0]}, {999999}} {
		if _, e := f.s.SearchAnimePack(ctx, id, bad, "", order.Revision); e == nil {
			t.Fatal("invalid target selection accepted", bad)
		}
	}
	if indexer.calls != 0 {
		t.Fatal("invalid selection contacted an indexer")
	}
	out, e := f.s.SearchAnimePack(ctx, id, ids, "", order.Revision)
	if e != nil || len(out) != 1 || indexer.query != "Pack Story" {
		t.Fatal(out, indexer.query, e)
	}
	var downloads, reservations int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&downloads)
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM episode_downloads`).Scan(&reservations)
	if downloads != 0 || reservations != 0 {
		t.Fatal("read-only search created work", downloads, reservations)
	}
	guardCalls := 0
	guard := func(context.Context, *sql.Tx, Candidate) error { guardCalls++; return nil }
	if _, _, e = f.s.QueueAutomatic(ctx, out[0].ID, guard); e == nil || guardCalls != 0 {
		t.Fatal("manual pack eligible for automatic queue", e, guardCalls)
	}
	job, created, e := f.s.Queue(ctx, out[0].ID)
	if e != nil || !created || job.AnimeOrder == nil || len(job.Targets) != 3 || job.Targets[0].Season != 0 || job.Targets[2].Season != 2 {
		t.Fatal(job, e)
	}
	if _, _, e = f.s.QueueAutomatic(ctx, out[0].ID, guard); e == nil {
		t.Fatal("already-queued manual pack bypassed automatic rejection")
	}
	if _, e = f.s.SearchAnimePack(ctx, id, []int64{ids[0]}, "", order.Revision); e == nil {
		t.Fatal("busy target selection searched")
	}
}

func TestReviewedAnimePackQueueFencesOrderChangesDuringMetainfo(t *testing.T) {
	f, id, ids, order, indexer := setupOrderPack(t)
	ctx := context.Background()
	out, e := f.s.SearchAnimePack(ctx, id, ids, "owned pack", order.Revision)
	if e != nil {
		t.Fatal(e)
	}
	indexer.fetch = func() {
		if _, e := f.db.Exec(`UPDATE anime_orders SET revision=revision+1 WHERE media_id=?`, id); e != nil {
			t.Fatal(e)
		}
	}
	if _, _, e = f.s.Queue(ctx, out[0].ID); e == nil {
		t.Fatal("stale provider order queued")
	}
	var typed *Error
	if !errors.As(e, &typed) || typed.Code != "anime_order_changed" {
		t.Fatal("order conflict should be actionable", e)
	}
	for _, table := range []string{"downloads", "episode_downloads", "download_targets"} {
		var count int
		if e = f.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); e != nil || count != 0 {
			t.Fatal("stale order left partial writes", table, count, e)
		}
	}
}

func TestReviewedAnimePackAcrossSeasonsAndSpecialsRetainsCanonicalReview(t *testing.T) {
	f, id, ids, order, _ := setupOrderPack(t)
	ctx := context.Background()
	specialBefore, e := library.New(f.db).TVSeason(ctx, id, 0)
	if e != nil {
		t.Fatal(e)
	}
	out, e := f.s.SearchAnimePack(ctx, id, ids, "owned pack", order.Revision)
	if e != nil {
		t.Fatal(e)
	}
	job, _, e := f.s.Queue(ctx, out[0].ID)
	if e != nil {
		t.Fatal(e)
	}
	f.tick(t)
	names := []string{"[Owned] Pack Story - SP01.mkv", "[Owned] Pack Story - 001.mkv", "[Owned] Pack Story Part 2 - 001v2.mkv"}
	f.client.files = nil
	for n, name := range names {
		body := []byte("owned pack video " + name)
		f.source(t, job, name, body)
		f.client.files = append(f.client.files, TorrentFile{Index: n, Name: name, Size: int64(len(body)), Progress: 1, Priority: 1})
	}
	f.client.torrent.Progress = 1
	f.client.torrent.AmountLeft = 0
	f.client.torrent.State = "uploading"
	changed := append([]library.AnimeOrderEntry{}, order.Entries...)
	for n := range changed {
		changed[n].ReleaseTitle = "Changed Future Release Title"
		changed[n].Label = []string{"SP9", "9", "10"}[n]
	}
	saveTransferOrder(t, f, id, changed)
	f.reopen(t)
	f.tick(t)
	current, e := f.s.Job(ctx, job.ID)
	if e != nil || current.ErrorCode != "map_episodes" || current.AnimeOrder == nil || !reflect.DeepEqual(current.AnimeOrder.Entries, job.AnimeOrder.Entries) {
		t.Fatal("queued provider order changed", current, e)
	}
	var registered int
	if e = f.db.QueryRow(`SELECT COUNT(*) FROM media_files WHERE download_id=?`, job.ID).Scan(&registered); e != nil || registered != 0 {
		t.Fatal("pack imported before review", registered, e)
	}
	for n, choice := range current.Choices {
		if !reflect.DeepEqual(choice.Suggestions, []int64{ids[n]}) {
			t.Fatal("snapshot label hint confused canonical order", choice, ids[n])
		}
	}
	bad := TVMappingUpdate{Files: []TVMapping{{FileIndex: 0, EpisodeIDs: []int64{ids[1], ids[2]}}}, SkipEpisodeIDs: []int64{ids[0]}}
	if e = f.s.ConfirmTVMapping(ctx, job.ID, bad); e == nil {
		t.Fatal("shared file crossing seasons accepted")
	}
	mapping := TVMappingUpdate{Files: []TVMapping{{FileIndex: 0, EpisodeIDs: []int64{ids[0]}}, {FileIndex: 1, EpisodeIDs: []int64{ids[1]}}, {FileIndex: 2, EpisodeIDs: []int64{ids[2]}}}}
	if e = f.s.ConfirmTVMapping(ctx, job.ID, mapping); e != nil {
		t.Fatal(e)
	}
	f.reopen(t)
	f.client.offline = true
	f.tick(t)
	current, e = f.s.Job(ctx, job.ID)
	if e != nil || current.State != "completed" {
		t.Fatal(current, e)
	}
	for n, season := range []int{0, 1, 2} {
		view, e := library.New(f.db).TVSeason(ctx, id, season)
		if e != nil || len(view.Episodes) != 1 || view.Episodes[0].ID != ids[n] || !view.Episodes[0].Available || !strings.Contains(view.Episodes[0].FilePath, "Season "+[]string{"00", "01", "02"}[n]) {
			t.Fatal("canonical season registration wrong", view, e)
		}
		if n == 0 && (view.Season.Monitored != specialBefore.Season.Monitored || view.Episodes[0].Monitored != specialBefore.Episodes[0].Monitored) {
			t.Fatal("manual import changed specials monitoring")
		}
		data, e := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(view.Episodes[0].FilePath)))
		if e != nil || string(data) != "owned pack video "+names[n] {
			t.Fatal("imported bytes changed", e)
		}
		source, e := os.ReadFile(filepath.Join(f.cfg.DownloadsDir, "ReelScarey", job.ID, names[n]))
		if e != nil || string(source) != string(data) {
			t.Fatal("seeding source changed", e)
		}
	}
}

func TestProviderOrderHintsKeepSpecialTypesAndCanonicalFallbackUnambiguous(t *testing.T) {
	targets := []TVTarget{{EpisodeID: 1, Season: 1, Number: 1}, {EpisodeID: 2, Season: 0, Number: 1}, {EpisodeID: 3, Season: 2, Number: 1}}
	snapshot := &library.AnimeOrderSnapshot{Entries: []library.AnimeOrderEntry{{EpisodeID: 1, ReleaseTitle: "Source Name", Label: "1"}, {EpisodeID: 2, ReleaseTitle: "Source Name", Label: "SP1"}, {EpisodeID: 3, ReleaseTitle: "Other Source", Label: "OVA1"}}}
	for _, tc := range []struct {
		name string
		want []int64
	}{{"[Group] Source Name - 001v2 [1080p].mkv", []int64{1}}, {"Source Name - SP01.mkv", []int64{2}}, {"Other Source - OVA01.mkv", []int64{3}}, {"Source Name - OVA01.mkv", nil}, {"Source Name - 001-002.mkv", nil}, {"Source Name - SP01 002.mkv", nil}, {"Unlisted Source - SP01.mkv", nil}, {"Canonical Show S02E01.mkv", []int64{3}}, {"Other Show S02E01.mkv", nil}, {"Source Name S02E01.mkv", nil}} {
		if got := suggestedOrderEpisodes(tc.name, targets, snapshot, "Canonical Show"); !reflect.DeepEqual(got, tc.want) {
			t.Fatal(tc.name, got, tc.want)
		}
	}
}
