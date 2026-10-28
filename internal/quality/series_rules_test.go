package quality

import "testing"

func TestSeriesReleaseRulesMatchExplicitCodecAndGroup(t *testing.T) {
	seeds := 4
	p := Profile{Qualities: []int{1080}, Cutoff: 1080, Language: "any", MaxSizeGiB: 10}
	target := Target{Title: "Example Show", TV: true, Season: 1, Episode: 2, SeriesCodec: "h265", SeriesGroup: "MeGusta"}
	for _, v := range []struct {
		title string
		ok    bool
	}{
		{"Example.Show.S01E02.1080p.HEVC-MeGusta", true},
		{"Example.Show.S01E02.1080p.x265-MeGusta", true},
		{"Example.Show.S01E02.1080p.x264-MeGusta", false},
		{"Example.Show.S01E02.1080p.x265-Other", false},
		{"Example.Show.S01E02.1080p.MeGusta.x265", false},
	} {
		d := Evaluate(p, target, Release{Title: v.title, Size: 1 << 30, Seeders: &seeds})
		if d.Eligible != v.ok {
			t.Fatalf("%s: %+v", v.title, d)
		}
	}
}
