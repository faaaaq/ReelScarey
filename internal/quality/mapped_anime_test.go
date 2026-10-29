package quality

import (
	"reflect"
	"strings"
	"testing"
)

func TestMappedAnimeLabelsAreTypedAndBounded(t *testing.T) {
	for _, tc := range []struct {
		label, family string
		number        int
	}{{"001", "number", 1}, {"sp002", "SP", 2}, {"OVA100000", "OVA", 100000}, {"ONA1", "ONA", 1}, {"OAD3", "OAD", 3}, {"S00E01", "S0", 1}, {"s10000e100000", "S10000", 100000}} {
		f, n, ok := MappedAnimeLabel(tc.label)
		if !ok || f != tc.family || n != tc.number {
			t.Fatal(tc, f, n, ok)
		}
	}
	for _, bad := range []string{"0", "100001", "SP0", "E1", "SP1v2", "S10001E1", "S0E0", "1-2", "1 2", "S1E1E2"} {
		if _, _, ok := MappedAnimeLabel(bad); ok {
			t.Fatal("invalid label", bad)
		}
	}
}

func TestMappedAnimeIdentityRequiresExactTypedCoverage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		labels []string
		ok     bool
	}{
		{"[Group] Source - 001v2 [1080p][WEB][ABCDEF12]", []string{"1"}, true},
		{"Source SP01 [1080p]", []string{"SP1"}, true},
		{"Source OVA01-OVA03 [Pack] [1080p]", []string{"OVA3", "OVA1", "OVA2"}, true},
		{"Source S00E01-S00E02 Batch [1080p]", []string{"S0E1", "S0E2"}, true},
		{"Source - 01 – 03 [1080p]", []string{"1", "2", "3"}, true},
		{"Source - 1 [1080p]", []string{"1", "2"}, false},
		{"Source - 1-3 [1080p]", []string{"1", "3"}, false},
		{"Source - 1-3 [1080p]", []string{"1", "2"}, false},
		{"Source - 1-2 [1080p]", []string{"1", "1"}, false},
		{"Source SP1-SP2 [1080p]", []string{"1", "2"}, false},
		{"Source S01E01-S02E02 [1080p]", []string{"S1E1", "S2E2"}, false},
		{"Source SP1-2 [1080p]", []string{"SP1", "SP2"}, false},
		{"Source - 1-2 [Complete] [1080p]", []string{"1", "2"}, false},
		{"Source Complete [1080p]", []string{"1", "2"}, false},
		{"Source Batch [1080p]", []string{"1", "2"}, false},
		{"Source - 1-2 [Batch] [Pack] [1080p]", []string{"1", "2"}, false},
		{"Source - 1 [Pack] [1080p]", []string{"1"}, false},
		{"Source - 1 + 2 [1080p]", []string{"1", "2"}, false},
		{"Source - 1-2 SP1 [1080p]", []string{"1", "2"}, false},
		{"Source - 1-2 [3] [1080p]", []string{"1", "2"}, false},
		{"Source - 1v2-2 [1080p]", []string{"1", "2"}, false},
		{"Source - 1-2v2 [1080p]", []string{"1", "2"}, false},
		{"Source 2024 - 1 [1080p]", []string{"1"}, false},
		{"Other Source - 1 [1080p]", []string{"1"}, false},
		{"[SP1] Source - 1 [1080p]", []string{"1"}, false},
		{"[Group] [Other] Source - 1 [1080p]", []string{"1"}, false},
		{"Source - 1話 [1080p]", []string{"1"}, false},
		{"Source - 1 [1080p", []string{"1"}, false},
	} {
		t.Run(tc.name+strings.Join(tc.labels, ","), func(t *testing.T) {
			target := Target{TV: true, Anime: true, AnimeMode: "mapped", Title: "Catalog differs", AnimeOrderTitle: "Source", AnimeOrderLabels: tc.labels}
			if got := Identify(tc.name, target); got != tc.ok {
				t.Fatalf("Identify=%v want %v", got, tc.ok)
			}
		})
	}
}

func TestMappedAnimeDoesNotScoreIdentityAsQuality(t *testing.T) {
	seeds := 10
	p := Profile{Qualities: []int{720, 1080}, Cutoff: 1080, Language: "english", MinSeeders: 1, MaxSizeGiB: 2, RequiredTerms: "web"}
	target := Target{TV: true, Anime: true, AnimeMode: "mapped", AnimeOrderTitle: "English WEB 1080p", AnimeOrderLabels: []string{"SP1", "SP2"}}
	for _, tc := range []struct {
		name string
		size int64
		ok   bool
	}{
		{"English WEB 1080p SP1-SP2 [Batch] [1080p][English][WEB]", 1 << 30, true},
		{"[English WEB] English WEB 1080p SP1-SP2 [720p]", 1 << 30, false},
		{"English WEB 1080p SP1-SP2 [English][WEB]", 1 << 30, false},
		{"English WEB 1080p SP1-SP2 [1080p][English][WEB]", 3 << 30, false},
	} {
		d := Evaluate(p, target, Release{Title: tc.name, Seeders: &seeds, Size: tc.size})
		if d.Eligible != tc.ok {
			t.Fatal(tc, d)
		}
	}
}

func TestMappedAnimeQueriesAreBoundedAndExplicit(t *testing.T) {
	for _, tc := range []struct{ labels, want []string }{
		{[]string{"1"}, []string{"Source 1", "Source 01", "Source 001"}},
		{[]string{"SP1"}, []string{"Source SP1", "Source SP01", "Source SP001"}},
		{[]string{"S0E1"}, []string{"Source S00E01"}},
		{[]string{"2", "1"}, []string{"Source 1-2", "Source"}},
		{[]string{"1", "3"}, nil},
		{[]string{"SP1", "OVA2"}, nil},
	} {
		if got := MappedAnimeQueries("Source", tc.labels); !reflect.DeepEqual(got, tc.want) {
			t.Fatal(tc, got)
		}
	}
	if got := MappedAnimeQueries(strings.Repeat("a", 181), []string{"1"}); got != nil {
		t.Fatal(got)
	}
	if got := MappedAnimeQueries("Source", make([]string, 101)); got != nil {
		t.Fatal(got)
	}
}
