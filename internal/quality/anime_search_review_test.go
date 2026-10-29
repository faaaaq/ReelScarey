package quality

import (
	"reflect"
	"strings"
	"testing"
)

func animeSearchReviewTarget(mode string) Target {
	year, absolute := 2024, 25
	return Target{Title: "Canonical Show", Year: &year, Season: 2, Episode: 1, TV: true, Anime: true, AnimeMode: mode, AlternateTitles: []string{"Romanized Show", "別名"}, AbsoluteNumber: &absolute}
}

func TestReviewAnimeIdentityAcceptsOnlyTheChosenExplicitNumberingMode(t *testing.T) {
	for _, mode := range []string{"season", "absolute"} {
		target := animeSearchReviewTarget(mode)
		for _, tc := range []struct {
			name string
			want bool
		}{
			{"[Fansub] Canonical Show S02E01v2 [1080p][Japanese][ABCDEF12]", mode == "season"},
			{"Romanized Show S02E01 (1080p HEVC AAC 2.0 10-bit)", mode == "season"},
			{"Canonical Show 2024 S02E01 [1080p]", mode == "season"},
			{"[Fansub] Canonical Show - 025v2 [1080p][Japanese][ABCDEF12]", mode == "absolute"},
			{"別名 - 25 (1080p HEVC AAC 2.0 10-bit)", mode == "absolute"},
			{"Canonical Show 2024 - 025 [1080p][12345678]", mode == "absolute"},
			{"Unlisted Show - 025 [1080p]", false},
			{"Canonical Show 2023 - 025 [1080p]", false},
			{"Canonical Show S01E01 [1080p]", false},
			{"Canonical Show S02E02 [1080p]", false},
			{"Canonical Show - 026 [1080p]", false},
			{"Canonical Show S02E01-E02 [1080p]", false},
			{"Canonical Show S02E01E02 [1080p]", false},
			{"Canonical Show - 025-026 [1080p]", false},
			{"Canonical Show - 025 026 [1080p]", false},
			{"Canonical Show - 025 [E026][1080p]", false},
			{"Canonical Show - 025 [S02E01][1080p]", false},
			{"Canonical Show - 025 [2024-01-01][1080p]", false},
			{"Canonical Show - 025 Batch [1080p]", false},
			{"Canonical Show - 025 OVA [1080p]", false},
			{"Canonical Show - 025 Movie [1080p]", false},
			{"Canonical Show S02E01 Movie [1080p]", false},
			{"Canonical Show - 025v0 [1080p]", false},
			{"Canonical Show - 025v100 [1080p]", false},
			{"[1080p] Canonical Show - 025 [Japanese]", false},
			{"[Group][Other] Canonical Show - 025 [1080p]", false},
			{"Canonical Show - 025 [1080p", false},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				if got := Identify(tc.name, target); got != tc.want {
					t.Fatal("incorrect explicit episode identity", got, tc.want, tc.name)
				}
			})
		}
	}
	missing := animeSearchReviewTarget("absolute")
	missing.AbsoluteNumber = nil
	if Identify("Canonical Show - 025 [1080p]", missing) {
		t.Fatal("absolute matching inferred an alias from canonical episode number")
	}
	disabled := animeSearchReviewTarget("disabled")
	if Identify("Canonical Show S02E01 [1080p]", disabled) {
		t.Fatal("disabled matching silently fell back to standard TV rules")
	}
}

func TestReviewAnimeTechnicalEvidenceDoesNotComeFromGroupOrSeriesName(t *testing.T) {
	target := animeSearchReviewTarget("absolute")
	profile := Profile{Qualities: []int{1080}, Cutoff: 1080, Language: "japanese", MinSeeders: 1, MaxSizeGiB: 10}
	seeds := 5
	for _, tc := range []struct {
		title string
		want  bool
	}{
		{"[Fansub] Canonical Show - 025v2 [1080p][Japanese][ABCDEF12]", true},
		{"[Japanese] Canonical Show - 025 [1080p]", false},
		{"[1080p] Canonical Show - 025 [Japanese]", false},
		{"[Fansub] Canonical Show - 025 [720p][1080p][Japanese]", false},
		{"[Fansub] Canonical Show - 025 [1080p][MULTI]", false},
	} {
		got := Evaluate(profile, target, Release{ID: "review", Title: tc.title, Size: 1 << 30, Seeders: &seeds})
		if got.Eligible != tc.want {
			t.Fatal("incorrect technical evidence", tc.title, got)
		}
	}
	target.Title = "Japanese 1080p Show"
	target.AlternateTitles = nil
	got := Evaluate(profile, target, Release{ID: "review", Title: "Japanese 1080p Show - 025 [WEB]", Size: 1 << 30, Seeders: &seeds})
	if got.Eligible || got.Quality != 0 || got.Language != "unknown" {
		t.Fatal("canonical series title was treated as technical evidence", got)
	}
}

func TestReviewAnimeQueriesStayBoundedAndDoNotFanOutAliases(t *testing.T) {
	target := animeSearchReviewTarget("absolute")
	number := 5
	target.AbsoluteNumber = &number
	want := []string{"Romanized Show 5", "Romanized Show 05", "Romanized Show 005"}
	if got := AnimeQueries(target, "Romanized Show"); !reflect.DeepEqual(got, want) {
		t.Fatal("numeric variants or explicitly selected name changed", got, want)
	}
	number = 125
	if got := AnimeQueries(target, "Romanized Show"); !reflect.DeepEqual(got, []string{"Romanized Show 125"}) {
		t.Fatal("equivalent padded queries were not deduplicated", got)
	}
	target.AnimeMode = "season"
	if got := AnimeQueries(target, "Romanized Show"); !reflect.DeepEqual(got, []string{"Romanized Show S02E01"}) {
		t.Fatal("season query used alternate/absolute fan-out", got)
	}
	for _, invalid := range []string{strings.Repeat("a", 201), "Bad\nName", "!!!"} {
		if got := AnimeQueries(target, invalid); len(got) != 0 {
			t.Fatal("invalid query title reached provider queries", invalid, got)
		}
	}
}

func TestReviewAnimeSeasonQueryAndMatcherAgreeForLongRunningEpisode(t *testing.T) {
	target := animeSearchReviewTarget("season")
	target.Season = 1
	target.Episode = 1000
	queries := AnimeQueries(target, "")
	if len(queries) != 1 || !Identify(queries[0]+" [1080p][Japanese]", target) {
		t.Fatal("the generated exact canonical query cannot match its own episode", queries)
	}
}
