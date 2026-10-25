package transfer

import (
	"reflect"
	"testing"
)

func TestExplicitAbsoluteSuggestionsNeverInventEpisodeOrder(t *testing.T) {
	a, b := 13, 14
	targets := []TVTarget{{EpisodeID: 10, Season: 2, Number: 1, AbsoluteNumber: &a}, {EpisodeID: 11, Season: 2, Number: 2, AbsoluteNumber: &b}}
	for _, in := range []struct {
		name string
		want []int64
	}{
		{"[Group] Story - 013v2 [1080p][AB0123EF].mkv", []int64{10}},
		{"Story - 013-014 [1080p].mkv", []int64{10, 11}},
		{"Story S02E01.mkv", []int64{10}},
		{"Story - 001 [1080p].mkv", nil},
		{"Story 1080p.mkv", nil},
		{"Story - 013-015.mkv", nil},
		{"Story - 013 - 014.mkv", nil},
		{"Story S01E13 - 013.mkv", nil},
	} {
		if got := suggestedEpisodes(in.name, targets); !reflect.DeepEqual(got, in.want) {
			t.Fatalf("%s got %v want %v", in.name, got, in.want)
		}
	}
	targets[1].Season = 3
	if got := suggestedEpisodes("Story - 013-014.mkv", targets); got != nil {
		t.Fatal("cross-season absolute guess", got)
	}
	targets[1].Season = 2
	targets[1].Number = 3
	if got := suggestedEpisodes("Story - 013-014.mkv", targets); got != nil {
		t.Fatal("nonconsecutive canonical episodes", got)
	}
	if got := suggestedEpisodes("Story - 013.mkv", []TVTarget{{EpisodeID: 10, Season: 1, Number: 13}}); got != nil {
		t.Fatal("invented an alias", got)
	}
}
