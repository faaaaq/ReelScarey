package transfer

import (
	"strings"
	"testing"
)

func TestAutomaticIdentityPreservesLeadingNumericEpisodeTitle(t *testing.T) {
	for _, test := range []struct {
		base string
		want string
	}{
		{"Example Show - 15 Minutes.mkv", "15 Minutes"},
		{"Example Show - 2 Ava 2 Fest.mkv", "2 Ava 2 Fest"},
		{"Example Show - 02 - Pilot.mkv", "Pilot"},
	} {
		h, err := automaticIdentity(
			LibraryScan{Kind: "series", Folder: "TV"},
			DiscoveredFile{Path: "TV/Example Show (2020)/Season 02/" + test.base},
		)
		if err != nil {
			t.Fatalf("%s: %v", test.base, err)
		}
		if h.EpisodeTitle != test.want {
			t.Fatalf("%s: episode title %q, want %q", test.base, h.EpisodeTitle, test.want)
		}
	}
}

func TestAutomaticLooseMovieDoesNotInventFutureReleaseYear(t *testing.T) {
	h, err := parseAutomaticTitle("Blade.Runner.2049.1080p")
	if err != nil {
		t.Fatal(err)
	}
	if h.Title != "Blade Runner 2049" || h.Year != 0 {
		t.Fatalf("invented title/year evidence: %#v", h)
	}
	_, err = automaticIdentity(
		LibraryScan{Kind: "movie", Folder: "Movies"},
		DiscoveredFile{Path: "Movies/Blade.Runner.2049.1080p.mkv"},
	)
	if err == nil || !strings.Contains(err.Error(), "needs a year or explicit TMDb ID") {
		t.Fatalf("ambiguous loose movie did not stay in review: %v", err)
	}
}
