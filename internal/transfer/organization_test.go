package transfer

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/organization"
)

func TestQueuedMovieRetainsReviewedCollectionAndNamingAcrossChanges(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	service := organization.New(f.db, f.cfg)
	c, e := service.Get(ctx)
	if e != nil {
		t.Fatal(e)
	}
	c.Libraries = append(c.Libraries, organization.Library{Key: "family", Name: "Family", Folder: "Family", Content: "mixed", Preset: "jellyfin"})
	if _, e = service.Update(ctx, c); e != nil {
		t.Fatal(e)
	}
	if _, e = service.PutAssignment(ctx, 1, "family", 0); e != nil {
		t.Fatal(e)
	}
	j := f.queue(t)
	var snapshot organization.Snapshot
	if e = json.Unmarshal([]byte(j.organizationSnapshot), &snapshot); e != nil {
		t.Fatal(e)
	}
	snapshot.Quality = organization.QualityTag(1920, 1080, false)
	expected, e := organization.Destination(snapshot, nil, "movie.mkv")
	if e != nil || !strings.HasPrefix(expected, "Family/Movies/") || !strings.Contains(expected, "[tmdbid-123]") {
		t.Fatal("wrong queued layout", expected, e)
	}
	c, e = service.Get(ctx)
	if e != nil {
		t.Fatal(e)
	}
	c.Libraries[len(c.Libraries)-1].Folder = "Changed Family"
	c.Libraries[len(c.Libraries)-1].Preset = "plex"
	if _, e = service.Update(ctx, c); e != nil {
		t.Fatal(e)
	}
	a, e := service.GetAssignment(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = service.PutAssignment(ctx, 1, "movies", a.Revision); e != nil {
		t.Fatal(e)
	}
	f.tick(t)
	f.complete(t, j, []byte(strings.Repeat("owned queued snapshot video ", 100)))
	f.reopen(t)
	f.tick(t)
	saved, e := f.s.Job(ctx, j.ID)
	if e != nil || saved.State != "completed" {
		t.Fatal(saved.State, saved.ErrorMessage, e)
	}
	var actual string
	if e = f.db.QueryRow(`SELECT relative_path FROM media_files WHERE download_id=?`, j.ID).Scan(&actual); e != nil || actual != expected {
		t.Fatal("in-flight movie was renamed by later preferences", actual, expected, e)
	}
	details, e := f.s.Details(ctx, 1, 1)
	if e != nil || details.LibraryRoot != filepath.Join(f.cfg.MediaDir, "Movies") {
		t.Fatal("details should separately show current preference and immutable file location", details.LibraryRoot, e)
	}
}

func TestQueuedAnimeAbsoluteAliasesAndCustomNamingStayImmutable(t *testing.T) {
	f, mediaID, targets := setupTV(t)
	ctx := context.Background()
	if _, e := f.db.Exec(`UPDATE media_items SET category='anime',category_revision=2 WHERE id=?`, mediaID); e != nil {
		t.Fatal(e)
	}
	for n, target := range targets {
		if _, e := f.db.Exec(`INSERT INTO anime_episode_numbers(episode_id,series_id,absolute_number) VALUES(?,?,?)`, target.EpisodeID, mediaID, 25+n); e != nil {
			t.Fatal(e)
		}
	}
	service := organization.New(f.db, f.cfg)
	c, e := service.Get(ctx)
	if e != nil {
		t.Fatal(e)
	}
	c.Libraries[2].Preset = "custom"
	c.Libraries[2].MovieTemplate = "{title}/{title}"
	c.Libraries[2].SeriesTemplate = "{title}"
	c.Libraries[2].EpisodeTemplate = "Absolute/{title} - {absolute:03}{absolute_range}"
	if _, e = service.Update(ctx, c); e != nil {
		t.Fatal(e)
	}
	j := queueSeason(t, f, mediaID)
	if j.Targets[0].AbsoluteNumber == nil || *j.Targets[0].AbsoluteNumber != 25 {
		t.Fatal("absolute aliases not snapshotted", j.Targets)
	}
	if _, e = f.db.Exec(`UPDATE anime_episode_numbers SET absolute_number=absolute_number+100 WHERE series_id=?`, mediaID); e != nil {
		t.Fatal(e)
	}
	c, e = service.Get(ctx)
	if e != nil {
		t.Fatal(e)
	}
	c.Libraries[2].Preset = "plex"
	if _, e = service.Update(ctx, c); e != nil {
		t.Fatal(e)
	}
	f.tick(t)
	completeTV(t, f, j)
	f.tick(t)
	mapping := TVMappingUpdate{Files: []TVMapping{{FileIndex: 0, EpisodeIDs: []int64{targets[0].EpisodeID, targets[1].EpisodeID}}, {FileIndex: 1, EpisodeIDs: []int64{targets[2].EpisodeID}}}}
	if e = f.s.ConfirmTVMapping(ctx, j.ID, mapping); e != nil {
		t.Fatal(e)
	}
	f.reopen(t)
	f.tick(t)
	saved, e := f.s.Job(ctx, j.ID)
	if e != nil || saved.State != "completed" {
		t.Fatal(saved.State, saved.ErrorMessage, e)
	}
	rows, e := f.db.Query(`SELECT relative_path FROM media_files WHERE download_id=? ORDER BY file_index`, j.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var name string
		if e = rows.Scan(&name); e != nil {
			t.Fatal(e)
		}
		paths = append(paths, name)
	}
	if len(paths) != 2 || !strings.Contains(paths[0], "025-026") || !strings.Contains(paths[1], "027") || !strings.HasPrefix(paths[0], "Anime/TV Shows/") {
		t.Fatal("queued aliases/templates changed after review", paths)
	}
}

func TestQueueRefusesChangedAnimeAliasAfterReleaseSearch(t *testing.T) {
	f, id, targets := setupTV(t)
	ctx := context.Background()
	out, e := f.s.SearchTV(ctx, id, 1, 0, "")
	if e != nil || len(out) != 1 {
		t.Fatal(out, e)
	}
	if _, e = f.db.Exec(`INSERT INTO anime_episode_numbers(episode_id,series_id,absolute_number) VALUES(?,?,42)`, targets[0].EpisodeID, id); e != nil {
		t.Fatal(e)
	}
	if _, _, e = f.s.Queue(ctx, out[0].ID); e == nil {
		t.Fatal("release queued against a changed absolute mapping")
	}
	var count int
	if e = f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&count); e != nil || count != 0 {
		t.Fatal("mapping conflict created a partial download", count, e)
	}
}
