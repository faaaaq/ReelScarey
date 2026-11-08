package quality

import "testing"

func TestPackTitleDistinguishesPacksFromEpisodes(t *testing.T) {
	for _, title := range []string{
		"Show S01 1080p",
		"Show Season 1 1080p",
		"Show Season 01 Complete 1080p",
		"Show S01E01-E03 1080p",
		"Show Complete Series 1080p",
		"Show Collection 1080p",
		"Show Batch 1080p",
	} {
		if !PackTitle(title) {
			t.Fatalf("%q was not recognized as a pack", title)
		}
	}
	for _, title := range []string{
		"Show S01E01 1080p",
		"Show 1x02 1080p",
		"Show Season 1 Episode 3 1080p",
		"Complete Unknown 2026 1080p",
	} {
		if PackTitle(title) {
			t.Fatalf("%q was wrongly recognized as a pack", title)
		}
	}
}

func TestPackTargetIdentityAndQuality(t *testing.T) {
	seeders := 20
	profile := Profile{Qualities: []int{1080}, Language: "any", MaxSizeGiB: 10}
	target := Target{Title: "Test: Show", Season: 1, TV: true, Pack: true}
	release := func(title string) Release {
		return Release{Title: title, Size: 1 << 30, Seeders: &seeders, Protocol: "torrent"}
	}
	for _, title := range []string{
		"Test.Show.S01.1080p.WEB",
		"Test.Show.Season.1.1080p.WEB",
		"Test.Show.Complete.Series.1080p.WEB",
		"Test.Show.S01E01-E03.1080p.WEB",
	} {
		if d := Evaluate(profile, target, release(title)); !d.Eligible {
			t.Fatalf("%q was rejected: %v", title, d.Reasons)
		}
	}
	for _, title := range []string{
		"Other.Show.S01.1080p.WEB",
		"Test.Show.S02.1080p.WEB",
		"Test.Show.S01E01.1080p.WEB",
		"Test.Show.720p.WEB",
	} {
		if d := Evaluate(profile, target, release(title)); d.Eligible {
			t.Fatalf("%q was accepted", title)
		}
	}
}
