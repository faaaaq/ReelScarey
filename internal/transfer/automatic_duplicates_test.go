package transfer

import (
	"context"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/metadata"
)

func duplicateFixtureScan(t *testing.T, f *workflowFixture, names ...string) LibraryScan {
	t.Helper()
	year := 2011
	season := metadata.SeasonSummary{TMDbID: 4629801, Number: 2, Title: "Season 2", EpisodeCount: 2}
	provider := &automaticProviderFixture{
		show: metadata.Series{Movie: metadata.Movie{TMDbID: 46298, Title: "Hunter x Hunter", Year: &year}, Seasons: []metadata.SeasonSummary{season}},
		seasonsByNumber: map[int]metadata.Season{
			2: {SeasonSummary: season, Episodes: []metadata.Episode{
				{TMDbID: 46298011, Number: 1, Title: "A x Hard x Master"},
				{TMDbID: 46298012, Number: 2, Title: "Strengthen x And x Threaten"},
			}},
		},
	}
	f.s.SetAutomaticMetadata(provider)
	for _, name := range names {
		existingVideo(t, f, name, []byte("owned duplicate fixture "+name))
	}
	scan, err := f.s.StartLibraryScanWithMatching(context.Background(), "series", "anime", true)
	if err != nil {
		t.Fatal(err)
	}
	finishScan(t, f, scan.ID)
	return scan
}

func TestAutomaticDuplicateEpisodeVariantsBecomeExplicitGroup(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	scan := duplicateFixtureScan(t, f,
		"anime/Hunter x Hunter (2011) [tmdb-46298]/Season 2/Hunter x Hunter - S02E63 - A x Hard x Master.mkv",
		"anime/Hunter x Hunter (2011) [tmdb-46298]/Season 2/Hunter x Hunter - S02E63 - A x Hard x Master.mp4",
		"anime/Hunter x Hunter (2011) [tmdb-46298]/Season 2/Hunter x Hunter - S02E64 - Strengthen x And x Threaten.mkv",
		"anime/Hunter x Hunter (2011) [tmdb-46298]/Season 2/Hunter x Hunter - S02E64 - Strengthen x And x Threaten.mp4",
	)
	finishAutomatic(t, f, scan.ID)

	// Nothing was guessed or registered: every variant is waiting for a choice.
	status, err := f.s.AutomaticMatchStatus(ctx, scan.ID)
	if err != nil || status.Review != 4 || status.Queued != 0 {
		t.Fatal("overlap was not kept in review", status, err)
	}
	var registered int
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM media_files`).Scan(&registered); err != nil || registered != 0 {
		t.Fatal("a duplicate variant was registered automatically", registered, err)
	}

	page, err := f.s.DuplicateGroups(ctx, scan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Truncated {
		t.Fatalf("expected two duplicate groups: %+v", page)
	}
	first := page.Items[0]
	if first.SeriesTitle != "Hunter x Hunter" || first.Episode.Season != 2 || first.Episode.Number != 1 || first.Episode.Title != "A x Hard x Master" || len(first.Files) != 2 {
		t.Fatalf("wrong first group: %+v", first)
	}
	if first.Files[0].Hint != "Hunter x Hunter" || first.Files[1].Hint != "Hunter x Hunter" {
		t.Fatalf("stale show hints: %+v", first.Files)
	}

	// A resolution without explicit review consent changes nothing.
	if _, err = f.s.ResolveDuplicate(ctx, scan.ID, DuplicateResolution{Key: first.Key, KeepFileID: first.Files[0].ID}); err == nil {
		t.Fatal("duplicate resolution bypassed review")
	}
	still, err := f.s.DuplicateGroups(ctx, scan.ID)
	if err != nil || len(still.Items) != 2 {
		t.Fatal("unreviewed resolution changed the review", still, err)
	}

	keep := first.Files[0]
	skipped, err := f.s.ResolveDuplicate(ctx, scan.ID, DuplicateResolution{Key: first.Key, KeepFileID: keep.ID, Reviewed: true})
	if err != nil || skipped != 1 {
		t.Fatal("duplicate group was not resolved", skipped, err)
	}
	for {
		worked, err := f.s.ProcessLibraryNext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			break
		}
	}
	current, err := f.s.LibraryScan(ctx, scan.ID)
	if err != nil || current.Registered != 1 {
		t.Fatal("chosen variant was not registered", current, err)
	}
	var files, episodes, skippedFiles int
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM media_files`).Scan(&files); err != nil || files != 1 {
		t.Fatal("more than one file registered", files, err)
	}
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM episode_files`).Scan(&episodes); err != nil || episodes != 1 {
		t.Fatal("episode was registered twice", episodes, err)
	}
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM library_scan_files WHERE state='skipped' AND message LIKE '%duplicate variant%'`).Scan(&skippedFiles); err != nil || skippedFiles != 1 {
		t.Fatal("duplicate variant was not clearly skipped", skippedFiles, err)
	}
	var path string
	if err = f.db.QueryRow(`SELECT relative_path FROM media_files`).Scan(&path); err != nil || !strings.HasSuffix(path, ".mkv") {
		t.Fatal("the reviewed variant was not the registered file", path, err)
	}

	// The second episode still has its own unresolved duplicate group.
	remaining, err := f.s.DuplicateGroups(ctx, scan.ID)
	if err != nil || len(remaining.Items) != 1 || remaining.Items[0].Episode.Number != 2 {
		t.Fatal("an unrelated duplicate group changed", remaining, err)
	}
}

func TestDuplicateGroupRefusesToSkipVariantsWithAdditionalEpisodes(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	year := 2011
	season := metadata.SeasonSummary{TMDbID: 4629811, Number: 2, Title: "Season 2", EpisodeCount: 2}
	provider := &automaticProviderFixture{
		show: metadata.Series{Movie: metadata.Movie{TMDbID: 46299, Title: "Hunter x Hunter", Year: &year}, Seasons: []metadata.SeasonSummary{season}},
		seasonsByNumber: map[int]metadata.Season{
			2: {SeasonSummary: season, Episodes: []metadata.Episode{
				{TMDbID: 46298111, Number: 1, Title: "A x Hard x Master"},
				{TMDbID: 46298112, Number: 2, Title: "Strengthen x And x Threaten"},
			}},
		},
	}
	f.s.SetAutomaticMetadata(provider)
	existingVideo(t, f, "anime/Hunter x Hunter (2011) [tmdb-46299]/Season 2/Hunter x Hunter - S02E01 - A x Hard x Master.mkv", []byte("owned single episode"))
	existingVideo(t, f, "anime/Hunter x Hunter (2011) [tmdb-46299]/Season 2/Hunter x Hunter - S02E01-E02 - A x Hard x Master.mkv", []byte("owned two episode"))
	scan, err := f.s.StartLibraryScanWithMatching(ctx, "series", "anime", true)
	if err != nil {
		t.Fatal(err)
	}
	finishScan(t, f, scan.ID)
	finishAutomatic(t, f, scan.ID)
	page, err := f.s.DuplicateGroups(ctx, scan.ID)
	if err != nil || len(page.Items) != 1 || len(page.Items[0].Files) != 2 {
		t.Fatal("range duplicate was not grouped", page, err)
	}
	group := page.Items[0]
	single := group.Files[0]
	if strings.Contains(single.Path, "S02E01-E02") {
		single = group.Files[1]
	}
	if _, err = f.s.ResolveDuplicate(ctx, scan.ID, DuplicateResolution{Key: group.Key, KeepFileID: single.ID, Reviewed: true}); err == nil {
		t.Fatal("a variant covering another episode was skipped")
	}
	still, err := f.s.DuplicateGroups(ctx, scan.ID)
	if err != nil || len(still.Items) != 1 || len(still.Items[0].Files) != 2 {
		t.Fatal("refused resolution changed the review", still, err)
	}
}
