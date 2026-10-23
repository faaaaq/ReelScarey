package transfer

import (
	"context"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTV(t *testing.T) (*workflowFixture, int64, []TVTarget) {
	t.Helper()
	f := setupWorkflow(t)
	ctx := context.Background()
	show := metadata.Series{Movie: metadata.Movie{TMDbID: 456, Title: "Test: Show"}, Seasons: []metadata.SeasonSummary{{TMDbID: 457, Number: 1, Title: "Season 1", EpisodeCount: 3}}}
	item, _, e := library.New(f.db).AddSeries(ctx, show, true)
	if e != nil {
		t.Fatal(e)
	}
	if e = library.New(f.db).SaveSeason(ctx, item.ID, metadata.Season{SeasonSummary: show.Seasons[0], Episodes: []metadata.Episode{{TMDbID: 458, Number: 1, Title: "Pilot"}, {TMDbID: 459, Number: 2, Title: "Second"}, {TMDbID: 460, Number: 3, Title: "Third"}}}, 1); e != nil {
		t.Fatal(e)
	}
	details, e := library.New(f.db).TVSeason(ctx, item.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	targets := []TVTarget{}
	for _, ep := range details.Episodes {
		targets = append(targets, TVTarget{EpisodeID: ep.ID, TMDbID: ep.TMDbID, Season: 1, Number: ep.Number, Title: ep.Title})
	}
	return f, item.ID, targets
}
func queueSeason(t *testing.T, f *workflowFixture, id int64) Job {
	t.Helper()
	out, e := f.s.SearchTV(context.Background(), id, 1, 0, "")
	if e != nil || len(out) != 1 {
		t.Fatal(out, e)
	}
	if f.indexer.query != "Test: Show S01" || f.indexer.category != "5000" {
		t.Fatal("TV search scope missing", f.indexer)
	}
	j, created, e := f.s.Queue(context.Background(), out[0].ID)
	if e != nil || !created || j.Kind != "series" || len(j.Targets) != 3 {
		t.Fatal(j, e)
	}
	return j
}
func completeTV(t *testing.T, f *workflowFixture, j Job) {
	t.Helper()
	for _, name := range []string{"Test.Show.S01E01-E02.mkv", "Test.Show.S01E03.mkv"} {
		f.source(t, j, name, []byte("owned fixture video "+name))
	}
	f.client.torrent.Progress = 1
	f.client.torrent.AmountLeft = 0
	f.client.torrent.State = "uploading"
	f.client.files = []TorrentFile{}
	for n, name := range []string{"Test.Show.S01E01-E02.mkv", "Test.Show.S01E03.mkv"} {
		f.client.files = append(f.client.files, TorrentFile{Index: n, Name: name, Size: int64(len("owned fixture video " + name)), Progress: 1, Priority: 1})
	}
}
func TestTVMultiEpisodeImportReopenAndSourceRetention(t *testing.T) {
	for _, copyMode := range []bool{false, true} {
		t.Run(map[bool]string{false: "link", true: "copy"}[copyMode], func(t *testing.T) {
			f, id, targets := setupTV(t)
			ctx := context.Background()
			j := queueSeason(t, f, id)
			f.client.lostResponse = true
			f.tick(t)
			completeTV(t, f, j)
			f.tick(t)
			current, e := f.s.Job(ctx, j.ID)
			if e != nil || current.ErrorCode != "map_episodes" || f.probe.calls != 0 || len(current.Choices) != 2 || len(current.Choices[0].Suggestions) != 2 {
				t.Fatal("import bypassed review", current, e)
			}
			bad := TVMappingUpdate{Files: []TVMapping{{FileIndex: 0, EpisodeIDs: []int64{targets[0].EpisodeID, targets[2].EpisodeID}}, {FileIndex: 1, EpisodeIDs: []int64{targets[1].EpisodeID}}}}
			if e = f.s.ConfirmTVMapping(ctx, j.ID, bad); e == nil {
				t.Fatal("nonconsecutive episode file accepted")
			}
			mapping := TVMappingUpdate{Files: []TVMapping{{FileIndex: 0, EpisodeIDs: []int64{targets[0].EpisodeID, targets[1].EpisodeID}}, {FileIndex: 1, EpisodeIDs: []int64{targets[2].EpisodeID}}}}
			if e = f.s.ConfirmTVMapping(ctx, j.ID, mapping); e != nil {
				t.Fatal(e)
			}
			if e = f.s.ConfirmTVMapping(ctx, j.ID, mapping); e == nil {
				t.Fatal("confirmed mapping was editable")
			}
			f.reopen(t)
			f.client.offline = true
			if copyMode {
				f.s.importer.link = func(*os.File, *os.File, string) error { return errors.New("forced cross device") }
			}
			f.tick(t)
			current, e = f.s.Job(ctx, j.ID)
			if e != nil || current.State != "completed" || f.client.adds != 1 {
				t.Fatal(current, e)
			}
			season, e := library.New(f.db).TVSeason(ctx, id, 1)
			if e != nil || season.Season.AvailableEpisodes != 3 || !season.Episodes[0].Available || season.Episodes[0].FilePath != season.Episodes[1].FilePath || season.Episodes[1].FilePath == season.Episodes[2].FilePath {
				t.Fatal("episode registration failed", season, e)
			}
			if !strings.Contains(season.Episodes[0].FilePath, "S01E01-E02") {
				t.Fatal("multi-episode naming wrong", season.Episodes[0].FilePath)
			}
			for _, ep := range season.Episodes {
				if _, e = os.Stat(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(ep.FilePath))); e != nil {
					t.Fatal(e)
				}
			}
			for _, file := range f.client.files {
				data, e := os.ReadFile(filepath.Join(f.cfg.DownloadsDir, "ReelScarey", j.ID, file.Name))
				if e != nil || string(data) != "owned fixture video "+file.Name {
					t.Fatal("seeding source changed", e)
				}
			}
			var count int
			if e = f.db.QueryRow(`SELECT COUNT(*) FROM episode_downloads`).Scan(&count); e != nil || count != 0 {
				t.Fatal("finished reservations retained", count, e)
			}
		})
	}
}
func TestTVPartialImportCollisionAndRestart(t *testing.T) {
	f, id, targets := setupTV(t)
	ctx := context.Background()
	j := queueSeason(t, f, id)
	f.tick(t)
	completeTV(t, f, j)
	f.tick(t)
	mapping := TVMappingUpdate{Files: []TVMapping{{FileIndex: 0, EpisodeIDs: []int64{targets[0].EpisodeID, targets[1].EpisodeID}}, {FileIndex: 1, EpisodeIDs: []int64{targets[2].EpisodeID}}}}
	if e := f.s.ConfirmTVMapping(ctx, j.ID, mapping); e != nil {
		t.Fatal(e)
	}
	idx := 1
	child := j
	child.fileIndex = &idx
	child.fileTargets = targets[2:]
	dest, e := f.s.tvDestination(ctx, child, "Test.Show.S01E03.mkv")
	if e != nil {
		t.Fatal(e)
	}
	collision := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(dest))
	if e = os.MkdirAll(filepath.Dir(collision), 0755); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(collision, []byte("keep this existing file"), 0644); e != nil {
		t.Fatal(e)
	}
	f.tick(t)
	current, e := f.s.Job(ctx, j.ID)
	if e != nil || current.ErrorCode != "destination_exists" {
		t.Fatal(current, e)
	}
	data, _ := os.ReadFile(collision)
	if string(data) != "keep this existing file" {
		t.Fatal("collision overwritten")
	}
	before := f.probe.calls
	f.reopen(t)
	f.client.offline = true
	// The fixture owner moves its own sentinel aside; the application never does.
	if e = os.Rename(collision, collision+".owner-backup"); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Retry(ctx, j.ID); e != nil {
		t.Fatal(e)
	}
	f.tick(t)
	current, e = f.s.Job(ctx, j.ID)
	if e != nil || current.State != "completed" || f.probe.calls != before {
		t.Fatal("partial job did not resume independently", current, e)
	}
}
func TestTVReservationsSkipAndDuplicateProtection(t *testing.T) {
	f, id, targets := setupTV(t)
	ctx := context.Background()
	j := queueSeason(t, f, id)
	f.tick(t)
	completeTV(t, f, j)
	f.tick(t)
	if e := f.s.ConfirmTVMapping(ctx, j.ID, TVMappingUpdate{Files: []TVMapping{{FileIndex: 55, EpisodeIDs: []int64{targets[0].EpisodeID}}}, SkipEpisodeIDs: []int64{targets[1].EpisodeID, targets[2].EpisodeID}}); e == nil {
		t.Fatal("unlisted file accepted")
	}
	if e := f.s.ConfirmTVMapping(ctx, j.ID, TVMappingUpdate{Files: []TVMapping{{FileIndex: 0, EpisodeIDs: []int64{targets[0].EpisodeID, targets[1].EpisodeID}}}, SkipEpisodeIDs: []int64{targets[2].EpisodeID}}); e != nil {
		t.Fatal(e)
	}
	f.tick(t)
	f.indexer.hash = strings.Repeat("a", 40)
	out, e := f.s.SearchTV(ctx, id, 1, 3, "")
	if e != nil || len(out) != 1 {
		t.Fatal(e)
	}
	next, created, e := f.s.Queue(ctx, out[0].ID)
	if e != nil || !created || len(next.Targets) != 1 {
		t.Fatal("skipped target remained reserved", next, e)
	}
	if _, e = f.s.SearchTV(ctx, id, 1, 1, ""); e == nil {
		t.Fatal("available episode offered as missing")
	}
	var n int
	if e = f.db.QueryRow(`SELECT COUNT(*) FROM episode_downloads WHERE download_id=?`, j.ID).Scan(&n); e != nil || n != 0 {
		t.Fatal("old reservations retained", e)
	}
}
func TestTVFilenameSuggestionsAreConservative(t *testing.T) {
	targets := []TVTarget{{EpisodeID: 1, Season: 1, Number: 1}, {EpisodeID: 2, Season: 1, Number: 2}, {EpisodeID: 3, Season: 1, Number: 3}}
	for _, v := range []struct {
		name  string
		count int
	}{{"Show.S01E01.mkv", 1}, {"Show.S01E01E02.mkv", 2}, {"Show.S01E01-E03.mkv", 3}, {"Show.1x01-02.mkv", 2}, {"Show.S02E01.mkv", 0}, {"Show.S01E01.S01E02.mkv", 0}, {"Show.2026.10.23.mkv", 0}, {"Show.S01E01E03.mkv", 0}, {"Show.S01E01-E99.mkv", 0}} {
		if got := suggestedEpisodes(v.name, targets); len(got) != v.count {
			t.Fatal(v.name, got)
		}
	}
}

func TestTVActiveReservationsAndCancelledResumeConflicts(t *testing.T) {
	f, id, _ := setupTV(t)
	ctx := context.Background()
	old := queueSeason(t, f, id)
	f.tick(t)
	completeTV(t, f, old)
	f.tick(t)
	f.indexer.hash = strings.Repeat("a", 40)
	out, e := f.s.SearchTV(ctx, id, 1, 1, "")
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = f.s.Queue(ctx, out[0].ID); e == nil {
		t.Fatal("overlapping episode reservation accepted")
	}
	var count int
	if e = f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&count); e != nil || count != 1 {
		t.Fatal("failed reservation left a partial job", e)
	}
	if e = f.s.Cancel(ctx, old.ID); e != nil {
		t.Fatal(e)
	}
	next, _, e := f.s.Queue(ctx, out[0].ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Retry(ctx, old.ID); e == nil {
		t.Fatal("cancelled job stole a new reservation")
	}
	if e = f.db.QueryRow(`SELECT COUNT(*) FROM episode_downloads WHERE download_id=?`, old.ID).Scan(&count); e != nil || count != 0 {
		t.Fatal("failed resume left reservations", e)
	}
	f.indexer.hash = strings.Repeat("b", 40)
	out, e = f.s.SearchTV(ctx, id, 1, 3, "")
	if e != nil {
		t.Fatal(e)
	}
	other, _, e := f.s.Queue(ctx, out[0].ID)
	if e != nil || other.ID == next.ID {
		t.Fatal("nonoverlapping TV jobs were blocked", e)
	}
}
