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

func TestAutomaticEpisodeTitleFallbackIdentity(t *testing.T) {
	h, err := automaticIdentity(LibraryScan{Kind: "series", Folder: "anime"}, DiscoveredFile{Path: "anime/Angels of Death (2018) [tmdb-80477]/Season/Angels of Death - SE - A vow cannot be stolen.mkv"})
	if err != nil || h.Title != "Angels of Death" || h.Year != 2018 || h.ProviderID != 80477 || h.Season != -1 || h.EpisodeTitle != "A vow cannot be stolen" || h.Episodes != nil {
		t.Fatalf("%+v %v", h, err)
	}
	h, err = automaticIdentity(LibraryScan{Kind: "series", Folder: "TV"}, DiscoveredFile{Path: "TV/Example Show (2020)/Season 02/Example Show - Promise.mkv"})
	if err != nil || h.Title != "Example Show" || h.Season != 2 || h.EpisodeTitle != "Promise" {
		t.Fatalf("%+v %v", h, err)
	}
	h, err = automaticIdentity(LibraryScan{Kind: "series", Folder: "TV"}, DiscoveredFile{Path: "TV/Example Show (2020)/Season 02/Example Show - S02E04 - Promise.mkv"})
	if err != nil || h.Season != 2 || !reflect.DeepEqual(h.Episodes, []int{4}) || h.EpisodeTitle != "Promise" {
		t.Fatalf("numbered episode title not retained: %+v %v", h, err)
	}
	h, err = automaticIdentity(LibraryScan{Kind: "series", Folder: "TV"}, DiscoveredFile{Path: "TV/Example Show (2020)/Season 02/Example Show - S02E04 - Episode 4.mkv"})
	if err != nil || h.EpisodeTitle != "" {
		t.Fatalf("episode placeholder became title evidence: %+v %v", h, err)
	}
	h, err = automaticIdentity(LibraryScan{Kind: "series", Folder: "TV"}, DiscoveredFile{Path: "TV/Example Show (2020)/Season 02/Example Show - S02E04 - 1080p.mkv"})
	if err != nil || h.EpisodeTitle != "" {
		t.Fatalf("quality label became title evidence: %+v %v", h, err)
	}
	if _, err := automaticIdentity(LibraryScan{Kind: "series", Folder: "TV"}, DiscoveredFile{Path: "TV/Example Show/Example Show.mkv"}); err == nil {
		t.Fatal("accepted a file without an episode title")
	}
}

func TestAutomaticMatchingCreatesSeasonsAddedAfterTheShowWasSaved(t *testing.T) {
	f := setupWorkflow(t)
	year := 2024
	s1 := metadata.SeasonSummary{TMDbID: 951, Number: 1, Title: "Season 1", EpisodeCount: 1}
	s2 := metadata.SeasonSummary{TMDbID: 952, Number: 2, Title: "Season 2", EpisodeCount: 1}
	provider := &automaticProviderFixture{
		show: metadata.Series{Movie: metadata.Movie{TMDbID: 950, Title: "DAN", Year: &year}, Seasons: []metadata.SeasonSummary{s1}},
		seasonsByNumber: map[int]metadata.Season{
			1: {SeasonSummary: s1, Episodes: []metadata.Episode{{TMDbID: 961, Number: 1, Title: "First"}}},
			2: {SeasonSummary: s2, Episodes: []metadata.Episode{{TMDbID: 962, Number: 1, Title: "Hey, It's a Kaiju!"}}},
		},
	}
	f.s.SetAutomaticMetadata(provider)
	existingVideo(t, f, "Anime/DAN (2024) [tmdb-950]/Season 01/DAN - S01E01.mp4", []byte("owned season one episode"))
	existingVideo(t, f, "Anime/DAN (2024) [tmdb-950]/Season 2/DAN - S02E01 - Hey, It's a Kaiju!.mp4", []byte("owned season two episode"))
	scan, err := f.s.StartLibraryScanWithMatching(context.Background(), "series", "anime", true)
	if err != nil {
		t.Fatal(err)
	}
	finishScan(t, f, scan.ID)
	// The first step resolves the title and saves the show with only the
	// seasons the provider listed at that moment.
	if _, err := f.s.ProcessAutomaticMatchNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The provider now lists a season that did not exist when the show was
	// saved, exactly like an ongoing anime continuing on TMDb.
	provider.show.Seasons = []metadata.SeasonSummary{s1, s2}
	finishAutomatic(t, f, scan.ID)
	current, err := f.s.LibraryScan(context.Background(), scan.ID)
	if err != nil || current.Registered != 2 {
		t.Fatal(current, err)
	}
	var season2, episode int64
	if err := f.db.QueryRow(`SELECT s.number,e.tmdb_id FROM episode_files ef JOIN episodes e ON e.id=ef.episode_id JOIN seasons s ON s.id=e.season_id WHERE e.tmdb_id=962`).Scan(&season2, &episode); err != nil || season2 != 2 || episode != 962 {
		t.Fatal("season 2 episode was not registered", season2, episode, err)
	}
	// Refreshing a show saved before TMDb added season 2 must save the new
	// season and load it rather than reporting that the provider has no season.
	var loaded int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM seasons s JOIN media_items m ON m.id=s.series_id WHERE m.tmdb_id=950 AND s.number=2 AND s.loaded_at<>''`).Scan(&loaded); err != nil || loaded != 1 {
		t.Fatal("new provider season was not refreshed and loaded", loaded, err)
	}
	var message string
	if err := f.db.QueryRow(`SELECT message FROM library_scan_files WHERE scan_id=? AND relative_path LIKE '%S02E01%'`, scan.ID).Scan(&message); err != nil || strings.Contains(message, "does not list this season") {
		t.Fatal("new provider season was reported as missing", message, err)
	}
}

func TestAutomaticMatchingRetriesStaleSeriesRevisionWhenProviderAddsSeason(t *testing.T) {
	f := setupWorkflow(t)
	year := 2023
	s1 := metadata.SeasonSummary{TMDbID: 971, Number: 1, Title: "Season 1", EpisodeCount: 1}
	s2 := metadata.SeasonSummary{TMDbID: 972, Number: 2, Title: "Season 2", EpisodeCount: 1}
	provider := &automaticProviderFixture{
		show: metadata.Series{Movie: metadata.Movie{TMDbID: 970, Title: "Revision Show", Year: &year}, Seasons: []metadata.SeasonSummary{s1}},
		seasonsByNumber: map[int]metadata.Season{
			1: {SeasonSummary: s1, Episodes: []metadata.Episode{{TMDbID: 981, Number: 1, Title: "First"}}},
			2: {SeasonSummary: s2, Episodes: []metadata.Episode{{TMDbID: 982, Number: 1, Title: "Second"}}},
		},
	}
	f.s.SetAutomaticMetadata(provider)
	existingVideo(t, f, "Anime/Revision Show (2023) [tmdb-970]/Season 01/Revision Show - S01E01.mp4", []byte("owned season one revision fixture"))
	existingVideo(t, f, "Anime/Revision Show (2023) [tmdb-970]/Season 2/Revision Show - S02E01.mp4", []byte("owned season two revision fixture"))
	scan, err := f.s.StartLibraryScanWithMatching(context.Background(), "series", "anime", true)
	if err != nil {
		t.Fatal(err)
	}
	finishScan(t, f, scan.ID)
	if _, err := f.s.ProcessAutomaticMatchNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	provider.show.Seasons = []metadata.SeasonSummary{s1, s2}
	bumped := false
	provider.beforeSeries = func() {
		if bumped {
			return
		}
		bumped = true
		// Simulate the automatic metadata refresh queue advancing the show
		// revision between the missing-season read and the refresh write.
		if _, err := f.db.Exec(`UPDATE series_details SET revision=revision+1 WHERE media_id=(SELECT id FROM media_items WHERE tmdb_id=970 AND kind='series')`); err != nil {
			t.Fatal(err)
		}
	}
	finishAutomatic(t, f, scan.ID)
	current, err := f.s.LibraryScan(context.Background(), scan.ID)
	status, err := f.s.AutomaticMatchStatus(context.Background(), scan.ID)
	if err != nil || current.Registered != 2 || status.Review != 0 || status.Queued != 2 {
		t.Fatal(current, status, err)
	}
	if !bumped {
		t.Fatal("stale revision was not exercised")
	}
	var count int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM episode_files ef JOIN episodes e ON e.id=ef.episode_id WHERE e.tmdb_id=982`).Scan(&count); err != nil || count != 1 {
		t.Fatal("season added during a revision race was not registered", count, err)
	}
}

func TestAutomaticTitleOnlyEpisodeMatchesUniqueCanonicalTitle(t *testing.T) {
	f := setupWorkflow(t)
	year := 2018
	s1 := metadata.SeasonSummary{TMDbID: 901, Number: 1, Title: "Season 1", EpisodeCount: 2}
	s2 := metadata.SeasonSummary{TMDbID: 902, Number: 2, Title: "Season 2", EpisodeCount: 1}
	provider := &automaticProviderFixture{
		show: metadata.Series{Movie: metadata.Movie{TMDbID: 900, Title: "Angels", Year: &year}, Seasons: []metadata.SeasonSummary{s1, s2}},
		seasonsByNumber: map[int]metadata.Season{
			1: {SeasonSummary: s1, Episodes: []metadata.Episode{{TMDbID: 911, Number: 1, Title: "A vow cannot be stolen"}, {TMDbID: 912, Number: 2, Title: "I'm not Your God."}}},
			2: {SeasonSummary: s2, Episodes: []metadata.Episode{{TMDbID: 913, Number: 1, Title: "Stop crying and smile"}}},
		},
	}
	f.s.SetAutomaticMetadata(provider)
	existingVideo(t, f, "Anime/Angels (2018) [tmdb-900]/Season/Angels - SE - A vow cannot be stolen.mp4", []byte("owned title-matched episode"))
	scan, err := f.s.StartLibraryScanWithMatching(context.Background(), "series", "anime", true)
	if err != nil {
		t.Fatal(err)
	}
	finishScan(t, f, scan.ID)
	finishAutomatic(t, f, scan.ID)
	current, err := f.s.LibraryScan(context.Background(), scan.ID)
	if err != nil || current.Registered != 1 {
		t.Fatal(current, err)
	}
	var count int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM episode_files ef JOIN episodes e ON e.id=ef.episode_id WHERE e.tmdb_id=911`).Scan(&count); err != nil || count != 1 {
		t.Fatal("the wrong episode was registered", count, err)
	}
}

func TestAutomaticTitleOnlyAmbiguousTitleStaysReview(t *testing.T) {
	f := setupWorkflow(t)
	year := 2018
	s1 := metadata.SeasonSummary{TMDbID: 901, Number: 1, Title: "Season 1", EpisodeCount: 1}
	s2 := metadata.SeasonSummary{TMDbID: 902, Number: 2, Title: "Season 2", EpisodeCount: 1}
	provider := &automaticProviderFixture{
		show: metadata.Series{Movie: metadata.Movie{TMDbID: 900, Title: "Angels", Year: &year}, Seasons: []metadata.SeasonSummary{s1, s2}},
		seasonsByNumber: map[int]metadata.Season{
			1: {SeasonSummary: s1, Episodes: []metadata.Episode{{TMDbID: 911, Number: 1, Title: "Recap"}}},
			2: {SeasonSummary: s2, Episodes: []metadata.Episode{{TMDbID: 913, Number: 1, Title: "Recap"}}},
		},
	}
	f.s.SetAutomaticMetadata(provider)
	existingVideo(t, f, "Anime/Angels (2018) [tmdb-900]/Season/Angels - SE - Recap.mp4", []byte("owned ambiguous episode"))
	scan, err := f.s.StartLibraryScanWithMatching(context.Background(), "series", "anime", true)
	if err != nil {
		t.Fatal(err)
	}
	finishScan(t, f, scan.ID)
	finishAutomatic(t, f, scan.ID)
	status, err := f.s.AutomaticMatchStatus(context.Background(), scan.ID)
	if err != nil || status.Review != 1 || status.Queued != 0 {
		t.Fatal(status, err)
	}
	var count int
	f.db.QueryRow(`SELECT COUNT(*) FROM episode_files`).Scan(&count)
	if count != 0 {
		t.Fatal("ambiguous title registered a file", count)
	}
}

func TestAutomaticNumberedEpisodeMapsAcrossSeasonsByExactTitle(t *testing.T) {
	f := setupWorkflow(t)
	year := 2024
	s1 := metadata.SeasonSummary{TMDbID: 2404111, Number: 1, Title: "Season 1", EpisodeCount: 2}
	s2 := metadata.SeasonSummary{TMDbID: 2404112, Number: 2, Title: "Season 2", EpisodeCount: 2}
	provider := &automaticProviderFixture{
		show: metadata.Series{Movie: metadata.Movie{TMDbID: 240411, Title: "Dan Da Dan", Year: &year}, Seasons: []metadata.SeasonSummary{s1, s2}},
		seasonsByNumber: map[int]metadata.Season{
			1: {SeasonSummary: s1, Episodes: []metadata.Episode{
				{TMDbID: 24041101, Number: 1, Title: "That's a Space Alien, Ain't It?!"},
				{TMDbID: 24041102, Number: 11, Title: "Hey, It's a Kaiju!"},
			}},
			2: {SeasonSummary: s2, Episodes: []metadata.Episode{
				{TMDbID: 24041201, Number: 1, Title: "A New Season"},
				{TMDbID: 24041202, Number: 2, Title: "Another Episode"},
			}},
		},
	}
	f.s.SetAutomaticMetadata(provider)
	// The file's own season/number is not how TMDb numbers this episode.
	existingVideo(t, f, "Anime/Dan Da Dan (2024) [tmdb-240411]/Season 2/DAN DA DAN - S02E11 - Hey, It's a Kaiju!.mp4", []byte("owned cross-season fixture"))
	scan, err := f.s.StartLibraryScanWithMatching(context.Background(), "series", "anime", true)
	if err != nil {
		t.Fatal(err)
	}
	finishScan(t, f, scan.ID)
	finishAutomatic(t, f, scan.ID)
	status, err := f.s.AutomaticMatchStatus(context.Background(), scan.ID)
	current, err := f.s.LibraryScan(context.Background(), scan.ID)
	if err != nil || status.Review != 0 || status.Queued != 1 || current.Registered != 1 {
		t.Fatal(status, current, err)
	}
	var season, number int
	var tmdbID int64
	if err := f.db.QueryRow(`SELECT s.number,e.number,e.tmdb_id FROM episode_files ef JOIN episodes e ON e.id=ef.episode_id JOIN seasons s ON s.id=e.season_id`).Scan(&season, &number, &tmdbID); err != nil || season != 1 || number != 11 || tmdbID != 24041102 {
		t.Fatal("episode was not mapped to its canonical season", season, number, tmdbID, err)
	}
	var message string
	if err := f.db.QueryRow(`SELECT message FROM library_scan_files WHERE scan_id=?`, scan.ID).Scan(&message); err != nil || !strings.Contains(message, "Mapped S02E11 to TMDb S01E11") {
		t.Fatal("cross-season mapping was not reported after registration", message, err)
	}
}

func TestAutomaticNumberedEpisodeMapsWhenHintSeasonIsAbsent(t *testing.T) {
	f := setupWorkflow(t)
	year := 2024
	s1 := metadata.SeasonSummary{TMDbID: 2404411, Number: 1, Title: "Season 1", EpisodeCount: 2}
	provider := &automaticProviderFixture{
		show: metadata.Series{Movie: metadata.Movie{TMDbID: 240441, Title: "Only Season Show", Year: &year}, Seasons: []metadata.SeasonSummary{s1}},
		seasonsByNumber: map[int]metadata.Season{
			1: {SeasonSummary: s1, Episodes: []metadata.Episode{
				{TMDbID: 24044101, Number: 1, Title: "First"},
				{TMDbID: 24044102, Number: 11, Title: "Hey, It's a Kaiju!"},
			}},
		},
	}
	f.s.SetAutomaticMetadata(provider)
	// The file says season 2, but this provider record only has season 1.
	existingVideo(t, f, "Anime/Only Season Show (2024) [tmdb-240441]/Season 2/Only Season Show - S02E11 - Hey, It's a Kaiju!.mp4", []byte("owned missing-hint-season fixture"))
	scan, err := f.s.StartLibraryScanWithMatching(context.Background(), "series", "anime", true)
	if err != nil {
		t.Fatal(err)
	}
	finishScan(t, f, scan.ID)
	finishAutomatic(t, f, scan.ID)
	status, err := f.s.AutomaticMatchStatus(context.Background(), scan.ID)
	current, err := f.s.LibraryScan(context.Background(), scan.ID)
	if err != nil || status.Review != 0 || status.Queued != 1 || current.Registered != 1 {
		t.Fatal(status, current, err)
	}
	var count int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM episode_files ef JOIN episodes e ON e.id=ef.episode_id WHERE e.tmdb_id=24044102`).Scan(&count); err != nil || count != 1 {
		t.Fatal("missing hint season was not mapped by exact title", count, err)
	}
	var message string
	if err := f.db.QueryRow(`SELECT message FROM library_scan_files WHERE scan_id=?`, scan.ID).Scan(&message); err != nil || !strings.Contains(message, "Mapped S02E11 to TMDb S01E11") {
		t.Fatal("missing hint season mapping was not reported", message, err)
	}
}

func TestAutomaticNumberedEpisodeCrossSeasonAmbiguousTitleStaysReview(t *testing.T) {
	f := setupWorkflow(t)
	year := 2024
	s1 := metadata.SeasonSummary{TMDbID: 2404211, Number: 1, Title: "Season 1", EpisodeCount: 1}
	s2 := metadata.SeasonSummary{TMDbID: 2404212, Number: 2, Title: "Season 2", EpisodeCount: 1}
	provider := &automaticProviderFixture{
		show: metadata.Series{Movie: metadata.Movie{TMDbID: 240421, Title: "Recap Show", Year: &year}, Seasons: []metadata.SeasonSummary{s1, s2}},
		seasonsByNumber: map[int]metadata.Season{
			1: {SeasonSummary: s1, Episodes: []metadata.Episode{{TMDbID: 24042101, Number: 5, Title: "Recap"}}},
			2: {SeasonSummary: s2, Episodes: []metadata.Episode{{TMDbID: 24042102, Number: 1, Title: "Recap"}}},
		},
	}
	f.s.SetAutomaticMetadata(provider)
	existingVideo(t, f, "Anime/Recap Show (2024) [tmdb-240421]/Season 2/Recap Show - S02E11 - Recap.mp4", []byte("owned ambiguous cross-season fixture"))
	scan, err := f.s.StartLibraryScanWithMatching(context.Background(), "series", "anime", true)
	if err != nil {
		t.Fatal(err)
	}
	finishScan(t, f, scan.ID)
	finishAutomatic(t, f, scan.ID)
	status, err := f.s.AutomaticMatchStatus(context.Background(), scan.ID)
	if err != nil || status.Review != 1 || status.Queued != 0 {
		t.Fatal(status, err)
	}
	var count int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM episode_files`).Scan(&count); err != nil || count != 0 {
		t.Fatal("ambiguous cross-season title registered a file", count, err)
	}
	var message string
	if err := f.db.QueryRow(`SELECT message FROM library_scan_files WHERE scan_id=?`, scan.ID).Scan(&message); err != nil || !strings.Contains(message, "more than one episode is titled") || !strings.Contains(message, "seasons 1, 2") {
		t.Fatal("ambiguous cross-season message was not clear", message, err)
	}
}

func TestAutomaticNumberedEpisodeWithoutTitleStaysReview(t *testing.T) {
	f := setupWorkflow(t)
	year := 2024
	s1 := metadata.SeasonSummary{TMDbID: 2404311, Number: 1, Title: "Season 1", EpisodeCount: 1}
	s2 := metadata.SeasonSummary{TMDbID: 2404312, Number: 2, Title: "Season 2", EpisodeCount: 1}
	provider := &automaticProviderFixture{
		show: metadata.Series{Movie: metadata.Movie{TMDbID: 240431, Title: "Numbered Show", Year: &year}, Seasons: []metadata.SeasonSummary{s1, s2}},
		seasonsByNumber: map[int]metadata.Season{
			1: {SeasonSummary: s1, Episodes: []metadata.Episode{{TMDbID: 24043101, Number: 1, Title: "First"}}},
			2: {SeasonSummary: s2, Episodes: []metadata.Episode{{TMDbID: 24043102, Number: 1, Title: "Second"}}},
		},
	}
	f.s.SetAutomaticMetadata(provider)
	existingVideo(t, f, "Anime/Numbered Show (2024) [tmdb-240431]/Season 2/Numbered Show - S02E11.mp4", []byte("owned missing-title fixture"))
	scan, err := f.s.StartLibraryScanWithMatching(context.Background(), "series", "anime", true)
	if err != nil {
		t.Fatal(err)
	}
	finishScan(t, f, scan.ID)
	finishAutomatic(t, f, scan.ID)
	status, err := f.s.AutomaticMatchStatus(context.Background(), scan.ID)
	if err != nil || status.Review != 1 || status.Queued != 0 {
		t.Fatal(status, err)
	}
	var count int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM episode_files`).Scan(&count); err != nil || count != 0 {
		t.Fatal("file without episode title registered", count, err)
	}
	var message string
	if err := f.db.QueryRow(`SELECT message FROM library_scan_files WHERE scan_id=?`, scan.ID).Scan(&message); err != nil || !strings.Contains(message, "does not include a usable episode title") {
		t.Fatal("missing-title message was not clear", message, err)
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
	beforeSeries      func()
	movieError        error
	searchError       error
	seasonError       error
	seasonsByNumber   map[int]metadata.Season
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
	if p.beforeSeries != nil {
		p.beforeSeries()
	}
	return p.show, nil
}
func (p *automaticProviderFixture) Season(_ context.Context, _ int64, number int) (metadata.Season, error) {
	p.seasons++
	if p.seasonError != nil {
		return metadata.Season{}, p.seasonError
	}
	if p.seasonsByNumber != nil {
		season, ok := p.seasonsByNumber[number]
		if !ok {
			return metadata.Season{}, &metadata.Error{Code: "series_not_found", Message: "missing fixture season"}
		}
		return season, nil
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
