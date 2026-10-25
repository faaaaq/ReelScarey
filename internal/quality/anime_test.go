package quality

import (
	"reflect"
	"testing"
)

func TestAnimeSingleEpisodeIdentityUsesExplicitNumberingAndTitles(t *testing.T) {
	absolute := 25
	year := 2024
	base := Target{Title: "The Example: Story", Year: &year, Season: 2, Episode: 1, TV: true, Anime: true, AnimeMode: "absolute", AbsoluteNumber: &absolute, AlternateTitles: []string{"Rei no Monogatari", "例の物語"}}
	for _, tc := range []struct {
		name string
		ok   bool
	}{
		{"[Fansubs] The Example Story - 025 [1080p][WEB][ABCDEF12]", true},
		{"[Group] Rei no Monogatari - 25v2 (1080p) [HEVC 10bit AAC 2.0] [12345678].mkv", true},
		{"例の物語 - 0025 [720p]", true},
		{"The.Example.Story.2024.025.1080p.WEB", true},
		{"The Example Story - 026 [1080p]", false},
		{"Another The Example Story - 025 [1080p]", false},
		{"[Group] Other Romanization - 025 [1080p]", false},
		{"[Group] The Example Story S02E01 [1080p]", false},
		{"The Example Story - 025-026 [1080p]", false},
		{"The Example Story - 025 + 026 [1080p]", false},
		{"The Example Story - 025 & 026 [1080p]", false},
		{"The Example Story - 025 [26] [1080p]", false},
		{"The Example Story - 025 [E26] [1080p]", false},
		{"The Example Story - 025 [S02E01] [1080p]", false},
		{"The Example Story - 025 [BATCH] [1080p]", false},
		{"The Example Story - 025 [SP] [1080p]", false},
		{"The Example Story - 025 [OVA] [1080p]", false},
		{"The Example Story - 025 [ONA] [1080p]", false},
		{"The Example Story - 025 [NCOP] [1080p]", false},
		{"The Example Story - 025v0 [1080p]", false},
		{"The Example Story - 025v02 [1080p]", false},
		{"The Example Story - 025v2x [1080p]", false},
		{"[S02E02] The Example Story - 025 [1080p]", false},
		{"[1080p] The Example Story - 025 [WEB]", false},
		{"[Group] [WEB] The Example Story - 025 [1080p]", false},
		{"The Example Story - 025 [1080p", false},
		{"The Example Story - 025 12345678 [1080p]", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Identify(tc.name, base); got != tc.ok {
				t.Fatalf("Identify=%v want %v", got, tc.ok)
			}
		})
	}
	base.AbsoluteNumber = nil
	if Identify("The Example Story - 025 [1080p]", base) {
		t.Fatal("missing aliases must never infer absolute order")
	}
	base.AnimeMode = "season"
	for _, tc := range []struct {
		name string
		ok   bool
	}{{"[Group] The Example Story S02E01v2 [1080p]", true}, {"Rei no Monogatari.2024.S02E01.1080p.WEB", true}, {"The Example Story - 025 [1080p]", false}, {"The Example Story S01E01 [1080p]", false}, {"The Example Story S02E01E02 [1080p]", false}, {"The Example Story S02E01-E02 [1080p]", false}, {"The Example Story S02E01 + 02 [1080p]", false}, {"The Example Story S02E01 S02E02 [1080p]", false}, {"The Example Story S02E01 [Special] [1080p]", false}} {
		if got := Identify(tc.name, base); got != tc.ok {
			t.Fatal(tc, got)
		}
	}
	base.Season = 0
	if Identify("The Example Story S00E01 [1080p]", base) {
		t.Fatal("specials must remain outside automatic regular-episode selection")
	}
}

func TestAnimeLabelsDoNotBorrowQualityOrLanguageFromIdentity(t *testing.T) {
	number := 25
	seeders := 3
	p := Profile{Qualities: []int{720, 1080}, Cutoff: 1080, Language: "english", MinSeeders: 1, MaxSizeGiB: 4, RequiredTerms: "web"}
	target := Target{Title: "English WEB 1080p", TV: true, Season: 1, Episode: 1, Anime: true, AnimeMode: "absolute", AbsoluteNumber: &number}
	good := Evaluate(p, target, Release{Title: "[Fansub] English WEB 1080p - 025v2 [1080p][English][WEB]", Seeders: &seeders, Size: 1 << 30})
	if !good.Eligible || good.Quality != 1080 || good.Language != "english" {
		t.Fatal(good)
	}
	for _, name := range []string{"[English WEB] English WEB 1080p - 025 [720p]", "English WEB 1080p - 025 [English][WEB]", "English WEB 1080p - 025 [1080p][720p][English][WEB]", "English WEB 1080p - 025 [1080p][English][CAM][WEB]"} {
		d := Evaluate(p, target, Release{Title: name, Seeders: &seeders, Size: 1 << 30})
		if d.Eligible {
			t.Fatal("unsafe label evidence accepted", name, d)
		}
	}
}

func TestAnimeQueriesAreBoundedAndNeedExplicitAliases(t *testing.T) {
	n := 1
	tgt := Target{Title: "Canonical", TV: true, Season: 2, Episode: 3, Anime: true, AnimeMode: "absolute", AbsoluteNumber: &n, AlternateTitles: []string{"Alias A", "Alias B"}}
	if got := AnimeQueries(tgt, "Chosen Search"); !reflect.DeepEqual(got, []string{"Chosen Search 1", "Chosen Search 01", "Chosen Search 001"}) {
		t.Fatal(got)
	}
	n = 25
	if got := AnimeQueries(tgt, ""); !reflect.DeepEqual(got, []string{"Canonical 25", "Canonical 025"}) {
		t.Fatal(got)
	}
	n = 125
	if got := AnimeQueries(tgt, ""); !reflect.DeepEqual(got, []string{"Canonical 125"}) {
		t.Fatal(got)
	}
	tgt.AbsoluteNumber = nil
	if got := AnimeQueries(tgt, ""); len(got) != 0 {
		t.Fatal("missing mapping queried provider", got)
	}
	tgt.AnimeMode = "season"
	if got := AnimeQueries(tgt, "Chosen"); !reflect.DeepEqual(got, []string{"Chosen S02E03"}) {
		t.Fatal(got)
	}
	tgt.AnimeMode = "disabled"
	if got := AnimeQueries(tgt, ""); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestAnimeIdentityDoesNotConfuseYearResolutionOrAttachedUnicode(t *testing.T) {
	year := 2024
	n := year
	tgt := Target{Title: "Story", Year: &year, TV: true, Anime: true, AnimeMode: "absolute", Season: 1, Episode: 1, AbsoluteNumber: &n}
	if Identify("Story 2024 [1080p]", tgt) {
		t.Fatal("bare show year interpreted as absolute episode")
	}
	if !Identify("Story - 2024 [1080p]", tgt) {
		t.Fatal("explicit episode delimiter should support a large mapped episode")
	}
	n = 1080
	if Identify("Story 1080 [WEB]", tgt) {
		t.Fatal("bare resolution interpreted as absolute episode")
	}
	if !Identify("Story - 1080 [720p]", tgt) {
		t.Fatal("explicit mapped episode delimiter rejected")
	}
	n = 25
	tgt.Title = "物語"
	for _, release := range []string{"物語025 [1080p]", "物語 - 025話 [1080p]"} {
		if Identify(release, tgt) {
			t.Fatal("attached unicode word treated as a separate numeric identity", release)
		}
	}
}
