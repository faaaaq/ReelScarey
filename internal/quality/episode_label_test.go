package quality

import (
	"reflect"
	"strings"
	"testing"
)

func TestExpandedTVLabelsRemainExactSingleEpisodeEvidence(t *testing.T) {
	year := 2026
	target := Target{Title: "Fixture Show", Year: &year, TV: true, Season: 1, Episode: 2}
	p := Profile{Name: "Fixture", Qualities: []int{1080}, Cutoff: 1080, Language: "any", MaxSizeGiB: 10}
	seeders := 2
	for _, label := range []string{"1x02", "S1 E02", "S01.E02", "Season 1 Episode 2", "S01EP02", "S01 Episode 02", "Season 1 Ep 2", "Season01Episode02"} {
		title := "Fixture Show 2026 " + label + " 1080p WEB-DL H265"
		d := Evaluate(p, target, Release{Title: title, Size: 1 << 30, Seeders: &seeders})
		if !d.Eligible || d.Source != "webdl" || d.Codec != "h265" {
			t.Fatal(title, d)
		}
	}
	for _, title := range []string{"Other Fixture Show 1x02 1080p", "Fixture Show 1x03 1080p", "Fixture Show 11x02 1080p", "Fixture Show 1x02-03 1080p", "Fixture Show 1x02x03 1080p", "Fixture Show Season 1 Episode 2 Episode 3 1080p", "Fixture Show S1 E02 S1 E03 1080p", "Fixture Show 1x02 Complete 1080p", "Fixture Show Season 1 Episode 2-4 1080p", "Fixture Show AS01E02 1080p"} {
		if Evaluate(p, target, Release{Title: title, Size: 1 << 30, Seeders: &seeders}).Eligible {
			t.Fatal("ambiguous identity accepted", title)
		}
	}
}

func TestNumericEpisodeTitlesAreNotTreatedAsRanges(t *testing.T) {
	single := []struct {
		title  string
		season int
		number int
	}{
		{"3rd Rock from the Sun - S02E23 - 15 Minutes of Dick", 2, 23},
		{"Abbott Elementary - S03E10 - 2 Ava 2 Fest", 3, 10},
		{"Weeds - S01E01 - 36! 24! 36! Dick", 1, 1},
		{"Show - S01E01 - 15 Minutes", 1, 1},
		{"Show - S01E01 - 02", 1, 1},
	}
	for _, v := range single {
		season, numbers, _, _, ok := EpisodeSpan(v.title)
		if !ok || season != v.season || !reflect.DeepEqual(numbers, []int{v.number}) {
			t.Fatalf("numeric title became a range: %q => %d %v %v", v.title, season, numbers, ok)
		}
	}
	for _, title := range []string{
		"Show S01E01-E02",
		"Show S01E01 - E02",
		"Show S01E01-02",
	} {
		season, numbers, _, _, ok := EpisodeSpan(title)
		if !ok || season != 1 || !reflect.DeepEqual(numbers, []int{1, 2}) {
			t.Fatalf("explicit range not parsed: %q => %d %v %v", title, season, numbers, ok)
		}
	}
}

func TestEpisodeSpansAreBoundedHintsAndNeverAutomaticPacks(t *testing.T) {
	for _, label := range []string{"S01E01-E03", "S01 E01 - E03", "Season 1 Episodes 1-3", "Season 1 Ep 1 - Ep 3", "1x01-03", "S01E01E02E03", "S01 Ep 1 Ep 2 Ep 3"} {
		title := "Owned Show " + label + " 1080p WEB-DL"
		season, numbers, start, end, ok := EpisodeSpan(title)
		if !ok || season != 1 || !reflect.DeepEqual(numbers, []int{1, 2, 3}) || title[start:end] != label {
			t.Fatal(title, season, numbers, start, end, ok)
		}
		if Identify(title, Target{Title: "Owned Show", TV: true, Season: 1, Episode: 1}) {
			t.Fatal("pack automatically identified as one episode", title)
		}
	}
	for _, title := range []string{
		"Owned Show AS01E01-E03", "Owned Show S01E01E03", "Owned Show S01E01-E01", "Owned Show S01E03-E01", "Owned Show S01E01-E21", "Owned Show S01E01-E03 E04", "Owned Show S01E01E02-E03", "Owned Show S01E01 S02E02", "Owned Show S01 Ep01 x02", "Owned Show S01E01+02", "Owned Show S01E01 and 02", "Owned Show Season 1 Episode 1 to 3", "Owned Show S01E01-S02", "Owned Show S01E01-03bad", "Owned Show S01E00", "Owned Show S01E01\nE02", strings.Repeat("x", 1001),
	} {
		if _, _, _, _, ok := EpisodeSpan(title); ok {
			t.Fatal("ambiguous span accepted", title)
		}
	}
	for _, label := range []string{"S01E01-1080p", "S01EP01-720p", "Season 1 Ep 1-2160p"} {
		if s, e, _, _, ok := EpisodeLabel(label); !ok || s != 1 || e != 1 {
			t.Fatal("resolution confused with range", label)
		}
	}
	for _, title := range []string{"Owned S01E01-1080p-E02", "Owned S01EP01 WEB Ep 2", "Owned S01E01-E03 1080p E04", "Owned S01E01 WEB S02", "Owned S01E01 WEB x02"} {
		if _, _, _, _, ok := EpisodeSpan(title); ok {
			t.Fatal("unconsumed episode evidence accepted", title)
		}
	}
}
