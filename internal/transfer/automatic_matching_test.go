package transfer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/metadata"
)

func TestAutomaticIdentityEvidence(t *testing.T) {
	for _, v := range []struct {
		kind, folder, name, title string
		year, season              int
		episodes                  []int
	}{
		{"movie", "Movies", "Movies/2001 A Space Odyssey (1968)/2001.A.Space.Odyssey.1968.1080p.mkv", "2001 A Space Odyssey", 1968, 0, nil},
		{"movie", "Movies", "Movies/The Matrix (1999) [tmdb-603]/obfuscated.mkv", "The Matrix", 1999, 0, nil},
		{"movie", "Movies", "Movies/Test.Movie.2026.1080p.mkv", "Test Movie", 2026, 0, nil},
		{"series", "TV", "TV/Example Show (2020)/Season 01/Example.Show.S01E02-E03.mkv", "Example Show", 2020, 1, []int{2, 3}},
		{"series", "TV", "TV/Example Show/Season 02/S02E04.mkv", "Example Show", 0, 2, []int{4}},
		{"movie", "Movies", "Movies/One Movie (2020)/Alternate.Release.Name.2020.mkv", "One Movie", 2020, 0, nil},
		{"movie", "Movies", "Movies/One Movie/obfuscated.mkv", "One Movie", 0, 0, nil},
		{"series", "TV", "TV/One Show (2020)/Season 01/Alternate.Title.S01E01.mkv", "One Show", 2020, 1, []int{1}},
		{"series", "TV", "TV/Grouped/Example Show/S01E02.mkv", "Example Show", 0, 1, []int{2}},
		{"series", "TV", "TV/1923/Season 01/1923.S01E01.mkv", "1923", 0, 1, []int{1}},
		{"series", "TV", "TV/Alley Cats (2026) [tmdb-300480]/Season 1/Alley Cats - S01E01 - Lost.mkv", "Alley Cats", 2026, 1, []int{1}},
		{"series", "TV", "TV/3rd Rock from the Sun (1996) [tmdb-155]/Season 2/3rd Rock from the Sun - S02E23 - 15 Minutes of Dick.mkv", "3rd Rock from the Sun", 1996, 2, []int{23}},
	} {
		h, err := automaticIdentity(LibraryScan{Kind: v.kind, Folder: v.folder}, DiscoveredFile{Path: v.name})
		if err != nil || h.Title != v.title || h.Year != v.year || h.Season != v.season || !reflect.DeepEqual(h.Episodes, v.episodes) {
			t.Fatalf("%s: %+v %v", v.name, h, err)
		}
	}
	h, err := automaticIdentity(LibraryScan{Kind: "series", Folder: "TV"}, DiscoveredFile{Path: "TV/Alley Cats (2026) [tmdb-300480]/Season 1/Alley Cats - S01E01 - Lost.mkv"})
	if err != nil || h.ProviderID != 300480 {
		t.Fatalf("TMDb folder identity not retained: %+v %v", h, err)
	}
	for _, v := range []struct{ kind, folder, name string }{
		{"movie", "Movies", "Movies/A Movie.mkv"},
		{"movie", "Movies", "Movies/One Movie (2020) [tmdb-10]/Another.Movie.2020.[tmdb-11].mkv"},
		{"movie", "Movies", "Movies/One Movie (2020)/One.Movie.2021.mkv"},
		{"movie", "Movies", "Other/One.Movie.2020.mkv"},
		{"series", "TV", "TV/One Show [tmdb-10]/Season 01/Another.Show.[tmdb-11].S01E01.mkv"},
		{"series", "TV", "TV/One Show/Season 02/One.Show.S01E01.mkv"},
		{"series", "TV", "TV/One Show/One.Show.013.mkv"},
		{"series", "TV", "TV/One Show/One.Show.2026.11.02.mkv"},
		{"series", "TV", "TV/One Show/One.Show.S01E01E03.mkv"},
	} {
		if h, err := automaticIdentity(LibraryScan{Kind: v.kind, Folder: v.folder}, DiscoveredFile{Path: v.name}); err == nil {
			t.Fatalf("accepted %s: %+v", v.name, h)
		}
	}
}
func finishAutomatic(t *testing.T, f *workflowFixture, id string) {
	t.Helper()
	ctx := context.Background()
	for range 100 {
		status, err := f.s.AutomaticMatchStatus(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if status != nil && status.State == "completed" {
			for range 100 {
				worked, err := f.s.ProcessLibraryNext(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if !worked {
					return
				}
			}
			t.Fatal("file verification did not finish")
		}
		worked, err := f.s.ProcessAutomaticMatchNext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			t.Fatalf("matching stopped: %+v", status)
		}
	}
	t.Fatal("matching did not finish")
}
func automaticMovieScan(t *testing.T, f *workflowFixture, names ...string) LibraryScan {
	t.Helper()
	for _, name := range names {
		existingVideo(t, f, name, []byte("owned automatic-matching fixture"))
	}
	scan, err := f.s.StartLibraryScanWithMatching(context.Background(), "movie", "", true)
	if err != nil {
		t.Fatal(err)
	}
	finishScan(t, f, scan.ID)
	return scan
}
func TestAutomaticMovieRegistrationSurvivesRestartWithoutManualConsentBypass(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	scan := automaticMovieScan(t, f, "Movies/Test Movie (2026)/Test.Movie.2026.mp4")
	page, _ := f.s.DiscoveredFiles(ctx, scan.ID, 1, "")
	if err := f.s.RegisterDiscovered(ctx, page.Items[0].ID, ExistingRegistration{MediaID: f.movie.ID}); err == nil {
		t.Fatal("manual review bypassed")
	}
	if _, err := f.s.ProcessAutomaticMatchNext(ctx); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := f.db.QueryRow(`SELECT payload FROM library_match_candidates WHERE scan_id=?`, scan.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var candidate automaticCandidate
	if err := json.Unmarshal([]byte(raw), &candidate); err != nil {
		t.Fatal(err)
	}
	if err := f.s.registerAutomaticCandidate(ctx, scan.ID, "foreign-owner", page.Items[0].ID, candidate); !errors.Is(err, ErrLease) {
		t.Fatalf("missing automatic lease accepted: %v", err)
	}
	f.reopen(t)
	finishAutomatic(t, f, scan.ID)
	current, err := f.s.LibraryScan(ctx, scan.ID)
	if err != nil || current.Registered != 1 {
		t.Fatal(current, err)
	}
	var count int
	f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&count)
	if count != 0 {
		t.Fatal("fabricated download")
	}
}
func TestAutomaticMatchingRegistersAllMovieVersions(t *testing.T) {
	f := setupWorkflow(t)
	scan := automaticMovieScan(t, f, "Movies/Test Movie (2026)/Test.Movie.2026.720p.mp4", "Movies/Test Movie (2026)/Test.Movie.2026.1080p.mp4")
	finishAutomatic(t, f, scan.ID)
	status, _ := f.s.AutomaticMatchStatus(context.Background(), scan.ID)
	current, _ := f.s.LibraryScan(context.Background(), scan.ID)
	if status.Review != 0 || status.Queued != 2 || current.Registered != 2 {
		t.Fatal(status, current)
	}
}
func TestAutomaticMatchingResumesResolvedCandidates(t *testing.T) {
	f := setupWorkflow(t)
	scan := automaticMovieScan(t, f,
		"Movies/Test Movie (2026)/Test.Movie.2026.720p.mp4",
		"Movies/Test Movie (2026)/Test.Movie.2026.1080p.mp4",
	)
	ctx := context.Background()
	if _, err := f.s.ProcessAutomaticMatchNext(ctx); err != nil {
		t.Fatal(err)
	}
	var ready, cache int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM library_match_candidates WHERE scan_id=? AND state='ready'`, scan.ID).Scan(&ready); err != nil || ready != 1 {
		t.Fatal("first candidate was not resolved", ready, err)
	}
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM library_match_cache WHERE scan_id=?`, scan.ID).Scan(&cache); err != nil || cache == 0 {
		t.Fatal("title cache was not written", cache, err)
	}
	if _, err := f.db.Exec(`UPDATE library_match_jobs SET state='paused',lease_owner='',lease_until=0 WHERE scan_id=?`, scan.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.StartAutomaticMatching(ctx, scan.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM library_match_candidates WHERE scan_id=? AND state='ready'`, scan.ID).Scan(&ready); err != nil || ready != 1 {
		t.Fatal("resume discarded a verified candidate", ready, err)
	}
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM library_match_cache WHERE scan_id=?`, scan.ID).Scan(&cache); err != nil || cache == 0 {
		t.Fatal("resume discarded the title cache", cache, err)
	}
}

func TestAutomaticInvalidFolderIDBecomesReviewWithoutStoppingScan(t *testing.T) {
	f := setupWorkflow(t)
	scan := automaticMovieScan(t, f, "Movies/Bad Folder (2024) [tmdb-999]/Bad.Folder.2024.mp4")
	year := 2024
	provider := &automaticProviderFixture{
		movie:      metadata.Movie{TMDbID: 999, Title: "Bad Folder", Year: &year},
		movieError: &metadata.Error{Code: "tmdb_invalid_response", Message: "unreadable"},
	}
	f.s.SetAutomaticMetadata(provider)
	worked, err := f.s.ProcessAutomaticMatchNext(context.Background())
	if err != nil || !worked {
		t.Fatal("single bad ID stopped the matcher", worked, err)
	}
	status, err := f.s.AutomaticMatchStatus(context.Background(), scan.ID)
	if err != nil || status.Review != 1 || status.State == "paused" {
		t.Fatal(status, err)
	}
	var message string
	if err := f.db.QueryRow(`SELECT message FROM library_scan_files WHERE scan_id=?`, scan.ID).Scan(&message); err != nil || !strings.Contains(message, "folder's ID") {
		t.Fatal(message, err)
	}
}

func TestAutomaticInvalidProviderTitleBecomesReviewWithoutStoppingScan(t *testing.T) {
	f := setupWorkflow(t)
	scan := automaticMovieScan(t, f, "Movies/Broken Title (2024)/Broken.Title.2024.mp4")
	f.s.SetAutomaticMetadata(&automaticProviderFixture{
		movie:       metadata.Movie{TMDbID: 77, Title: "Broken Title"},
		searchError: &metadata.Error{Code: "tmdb_invalid_response", Message: "unreadable"},
	})
	worked, err := f.s.ProcessAutomaticMatchNext(context.Background())
	if err != nil || !worked {
		t.Fatal("one malformed title stopped the matcher", worked, err)
	}
	status, err := f.s.AutomaticMatchStatus(context.Background(), scan.ID)
	if err != nil || status.Review != 1 || status.State == "paused" {
		t.Fatal(status, err)
	}
	var message string
	if err := f.db.QueryRow(`SELECT message FROM library_scan_files WHERE scan_id=?`, scan.ID).Scan(&message); err != nil || !strings.Contains(message, "invalid details") {
		t.Fatal(message, err)
	}
}

func TestAutomaticInvalidSeasonDetailsBecomesReviewWithoutStoppingScan(t *testing.T) {
	f := setupWorkflow(t)
	year := 2022
	season := metadata.SeasonSummary{TMDbID: 821, Number: 1, Title: "Season 1", EpisodeCount: 1}
	provider := &automaticProviderFixture{
		show:        metadata.Series{Movie: metadata.Movie{TMDbID: 820, Title: "New Show", Year: &year}, Seasons: []metadata.SeasonSummary{season}},
		seasonError: &metadata.Error{Code: "tmdb_invalid_response", Message: "unreadable"},
	}
	f.s.SetAutomaticMetadata(provider)
	existingVideo(t, f, "Anime/New Show (2022) [tmdb-820]/Season 01/S01E01.mp4", []byte("owned season fixture"))
	scan, err := f.s.StartLibraryScanWithMatching(context.Background(), "series", "anime", true)
	if err != nil {
		t.Fatal(err)
	}
	finishScan(t, f, scan.ID)
	worked, err := f.s.ProcessAutomaticMatchNext(context.Background())
	if err != nil || !worked {
		t.Fatal("invalid season stopped the matcher", worked, err)
	}
	status, err := f.s.AutomaticMatchStatus(context.Background(), scan.ID)
	if err != nil || status.Review != 1 || status.State == "paused" {
		t.Fatal(status, err)
	}
	var message string
	if err := f.db.QueryRow(`SELECT message FROM library_scan_files WHERE scan_id=?`, scan.ID).Scan(&message); err != nil || !strings.Contains(message, "invalid season") {
		t.Fatal(message, err)
	}
}

func TestAutomaticMatchingRequiresExplicitStartForOldScansAndRejectsChangedSource(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	name := existingVideo(t, f, "Movies/Test Movie (2026)/Test.Movie.2026.mp4", []byte("before"))
	scan, err := f.s.StartLibraryScan(ctx, "movie")
	if err != nil {
		t.Fatal(err)
	}
	finishScan(t, f, scan.ID)
	if worked, err := f.s.ProcessAutomaticMatchNext(ctx); worked || err != nil {
		t.Fatal("old scan started automatic work", worked, err)
	}
	if err = f.s.StartAutomaticMatching(ctx, scan.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ProcessAutomaticMatchNext(ctx); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(name, []byte("changed owner bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	finishAutomatic(t, f, scan.ID)
	status, _ := f.s.AutomaticMatchStatus(ctx, scan.ID)
	if status.Queued != 0 || status.Review != 1 {
		t.Fatal(status)
	}
	b, _ := os.ReadFile(name)
	if string(b) != "changed owner bytes" {
		t.Fatal("source overwritten")
	}
}

type automaticProviderFixture struct {
	movie             metadata.Movie
	show              metadata.Series
	season            metadata.Season
	searches, seasons int
	fail              bool
	ambiguous         bool
	beforeMovie       func()
	movieError        error
	searchError       error
	seasonError       error
}

func (p *automaticProviderFixture) Search(context.Context, string, int) (metadata.Results, error) {
	p.searches++
	if p.searchError != nil {
		return metadata.Results{}, p.searchError
	}
	if p.fail {
		return metadata.Results{}, errors.New("fixture provider unavailable")
	}
	items := []metadata.Movie{p.movie}
	if p.ambiguous {
		other := p.movie
		other.TMDbID++
		items = append(items, other)
	}
	return metadata.Results{TotalPages: 1, Items: items}, nil
}
func (p *automaticProviderFixture) Movie(context.Context, int64) (metadata.Movie, error) {
	if p.beforeMovie != nil {
		p.beforeMovie()
	}
	return p.movie, p.movieError
}
func (p *automaticProviderFixture) SearchSeries(context.Context, string, int) (metadata.SeriesResults, error) {
	p.searches++
	return metadata.SeriesResults{TotalPages: 1, Items: []metadata.Series{p.show}}, nil
}
func (p *automaticProviderFixture) Series(context.Context, int64) (metadata.Series, error) {
	return p.show, nil
}
func (p *automaticProviderFixture) Season(context.Context, int64, int) (metadata.Season, error) {
	p.seasons++
	if p.seasonError != nil {
		return metadata.Season{}, p.seasonError
	}
	return p.season, nil
}
func TestAutomaticAnimeMatchesDirectSeriesFolderAndLoadsSeasonOnce(t *testing.T) {
	f := setupWorkflow(t)
	year := 2022
	season := metadata.SeasonSummary{TMDbID: 821, Number: 1, Title: "Season 1", EpisodeCount: 2}
	provider := &automaticProviderFixture{show: metadata.Series{Movie: metadata.Movie{TMDbID: 820, Title: "New Show", Year: &year}, Seasons: []metadata.SeasonSummary{season}}, season: metadata.Season{SeasonSummary: season, Episodes: []metadata.Episode{{TMDbID: 822, Number: 1, Title: "First"}, {TMDbID: 823, Number: 2, Title: "Second"}}}}
	f.s.SetAutomaticMetadata(provider)
	existingVideo(t, f, "Anime/New Show (2022)/Season 01/S01E01.mp4", []byte("first owned episode"))
	existingVideo(t, f, "Anime/New Show (2022)/Season 01/S01E02.mp4", []byte("second owned episode"))
	if _, err := f.s.StartLibraryScanWithMatching(context.Background(), "movie", "anime", true); err == nil {
		t.Fatal("Anime accepted a movie scan")
	}
	scan, err := f.s.StartLibraryScanWithMatching(context.Background(), "series", "anime", true)
	if err != nil {
		t.Fatal(err)
	}
	finishScan(t, f, scan.ID)
	finishAutomatic(t, f, scan.ID)
	current, _ := f.s.LibraryScan(context.Background(), scan.ID)
	if current.Registered != 2 || provider.searches != 1 || provider.seasons != 1 {
		t.Fatal(current, provider.searches, provider.seasons)
	}
	var monitored, count int
	var category string
	f.db.QueryRow(`SELECT monitored,category FROM media_items WHERE tmdb_id=820 AND kind='series'`).Scan(&monitored, &category)
	f.db.QueryRow(`SELECT COUNT(*) FROM episode_files`).Scan(&count)
	if monitored != 0 || count != 2 || category != "anime" || scan.Folder != "Anime" {
		t.Fatal(monitored, count, category, scan.Folder)
	}
}
func TestAutomaticCancellationDuringProviderFetchPreventsCatalogCreation(t *testing.T) {
	f := setupWorkflow(t)
	year := 2024
	ctx := context.Background()
	scan := automaticMovieScan(t, f, "Movies/New Film (2024)/New.Film.2024.mp4")
	provider := &automaticProviderFixture{movie: metadata.Movie{TMDbID: 9001, Title: "New Film", Year: &year}}
	provider.beforeMovie = func() {
		if err := f.s.CancelAutomaticMatching(ctx, scan.ID); err != nil {
			t.Fatal(err)
		}
	}
	f.s.SetAutomaticMetadata(provider)
	if _, err := f.s.ProcessAutomaticMatchNext(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	f.db.QueryRow(`SELECT COUNT(*) FROM media_items WHERE tmdb_id=9001`).Scan(&count)
	status, _ := f.s.AutomaticMatchStatus(ctx, scan.ID)
	if count != 0 || status.State != "cancelled" {
		t.Fatal(count, status)
	}
}
func TestAutomaticProviderFailuresPauseWithoutRegisteringFiles(t *testing.T) {
	f := setupWorkflow(t)
	scan := automaticMovieScan(t, f, "Movies/New Film (2024)/New.Film.2024.mp4")
	f.s.SetAutomaticMetadata(&automaticProviderFixture{fail: true})
	for range 5 {
		if _, err := f.db.Exec(`UPDATE library_match_jobs SET retry_at=0 WHERE scan_id=?`, scan.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.ProcessAutomaticMatchNext(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	status, _ := f.s.AutomaticMatchStatus(context.Background(), scan.ID)
	if status.State != "paused" || status.Queued != 0 {
		t.Fatal(status)
	}
}

func TestAutomaticMatchingRejectsChangedCatalogAndFailedDiscovery(t *testing.T) {
	for _, mode := range []string{"title", "category", "discovery"} {
		t.Run(mode, func(t *testing.T) {
			f := setupWorkflow(t)
			ctx := context.Background()
			scan := automaticMovieScan(t, f, "Movies/Test Movie (2026)/Test.Movie.2026.mp4")
			if _, err := f.s.ProcessAutomaticMatchNext(ctx); err != nil {
				t.Fatal(err)
			}
			var err error
			switch mode {
			case "title":
				_, err = f.db.Exec(`UPDATE media_items SET title='Different identity' WHERE id=?`, f.movie.ID)
			case "category":
				_, err = f.db.Exec(`UPDATE media_items SET category_revision=category_revision+1 WHERE id=?`, f.movie.ID)
			case "discovery":
				_, err = f.db.Exec(`UPDATE library_scans SET state='failed' WHERE id=?`, scan.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "discovery" {
				if _, err = f.s.ProcessAutomaticMatchNext(ctx); err != nil {
					t.Fatal(err)
				}
				status, _ := f.s.AutomaticMatchStatus(ctx, scan.ID)
				if status.State != "paused" {
					t.Fatal(status)
				}
				if err = f.s.RemoveLibraryScan(ctx, scan.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				finishAutomatic(t, f, scan.ID)
				status, _ := f.s.AutomaticMatchStatus(ctx, scan.ID)
				if status.Queued != 0 || status.Review != 1 {
					t.Fatal(status)
				}
			}
		})
	}
}

func TestAutomaticAmbiguousProviderIdentityStaysUnregistered(t *testing.T) {
	f := setupWorkflow(t)
	year := 2024
	scan := automaticMovieScan(t, f, "Movies/New Film (2024)/New.Film.2024.mp4")
	f.s.SetAutomaticMetadata(&automaticProviderFixture{movie: metadata.Movie{TMDbID: 9001, Title: "New Film", Year: &year}, ambiguous: true})
	finishAutomatic(t, f, scan.ID)
	status, _ := f.s.AutomaticMatchStatus(context.Background(), scan.ID)
	if status.Review != 1 || status.Queued != 0 {
		t.Fatal(status)
	}
	var count int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM media_items WHERE tmdb_id IN (9001,9002)`).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func TestAutomaticRegistrationContinuesPastMissingMetadataInLargeCatalog(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	if _, err := f.db.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<10001)
		INSERT INTO media_items(kind,tmdb_id,title,year) SELECT 'movie',10000+i,'Other title '||i,2026 FROM n`); err != nil {
		t.Fatal(err)
	}
	f.s.SetAutomaticMetadata(&automaticProviderFixture{movieError: &metadata.Error{Code: "movie_not_found"}})
	scan := automaticMovieScan(t, f,
		"Movies/Removed Film (2026) [tmdb-999]/Removed.Film.2026.mp4",
		"Movies/Test Movie (2026)/Test.Movie.2026.mp4",
	)
	finishAutomatic(t, f, scan.ID)
	current, err := f.s.LibraryScan(ctx, scan.ID)
	if err != nil || current.Registered != 1 || current.NeedsMatch != 1 || current.AutomaticMatching == nil || current.AutomaticMatching.Review != 1 {
		t.Fatal(current, err)
	}
}
