package quality

import (
	"testing"
)

func TestExpandedTVLabelsRemainExactSingleEpisodeEvidence(t *testing.T) {
	year := 2026
	target := Target{Title: "Fixture Show", Year: &year, TV: true, Season: 1, Episode: 2}
	p := Profile{Name: "Fixture", Qualities: []int{1080}, Cutoff: 1080, Language: "any", MaxSizeGiB: 10}
	seeders := 2
	for _, label := range []string{"1x02", "S1 E02", "S01.E02", "Season 1 Episode 2"} {
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
