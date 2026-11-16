package music

import (
	"strings"
	"testing"
)

func TestParseAudioFileNamePatterns(t *testing.T) {
	cases := []struct {
		base   string
		artist string
		title  string
		number int
	}{
		{"Artist - Title", "Artist", "Title", 0},
		{"01 - Artist - Title", "Artist", "Title", 1},
		{"12 - Artist - Title", "Artist", "Title", 12},
		{"01. Title", "", "Title", 1},
		{"07 Title", "", "Title", 7},
		{"07 When Doves Cry", "", "When Doves Cry", 7},
		{"99 Problems", "", "99 Problems", 0},
		{"101 Dalmatians", "", "101 Dalmatians", 0},
		{"Artist - Title (feat. Other)", "Artist", "Title (feat. Other)", 0},
		{"Jay-Z - Song 99", "Jay-Z", "Song 99", 0},
		{"99 Problems", "", "99 Problems", 0},
		{"308 Boyz - Big Ole Rimz", "308 Boyz", "Big Ole Rimz", 0},
		{"(Hed) P.E. - Tits, Clits, Bong Hits", "(Hed) P.E.", "Tits, Clits, Bong Hits", 0},
		{"2Pac - Broken Wings", "2Pac", "Broken Wings", 0},
		{"01 - 2Pac - Thugs Mansion", "2Pac", "Thugs Mansion", 1},
	}
	for _, c := range cases {
		artist, title, number := parseAudioFileName(c.base)
		if artist != c.artist || title != c.title || number != c.number {
			t.Errorf("parse %q = (%q, %q, %d), want (%q, %q, %d)", c.base, artist, title, number, c.artist, c.title, c.number)
		}
	}
}

func TestInferFileIdentity(t *testing.T) {
	scanFile := func(path string, tags ScanFile) ScanFile {
		tags.RelativePath = path
		return tags
	}

	t.Run("filename fills missing artist and title", func(t *testing.T) {
		identity := inferFileIdentity(scanFile("music/308 Boyz/Unknown/308 Boyz - Big Ole Rimz.MP3", ScanFile{}))
		if identity.Artist != "308 Boyz" || identity.Title != "Big Ole Rimz" {
			t.Fatalf("identity %+v", identity)
		}
		if identity.ArtistSource != sourceFilename || identity.TitleSource != sourceFilename {
			t.Fatalf("sources %q %q", identity.ArtistSource, identity.TitleSource)
		}
		if identity.Album != "" {
			t.Fatalf("placeholder folder became an album: %q", identity.Album)
		}
	})

	t.Run("real album folder fills the album", func(t *testing.T) {
		identity := inferFileIdentity(scanFile("music/2Pac/Special 2 Pac/2Pac - Thugs Mansion.mp3", ScanFile{Artist: "2Pac"}))
		if identity.Album != "Special 2 Pac" || identity.AlbumSource != sourcePath {
			t.Fatalf("identity %+v", identity)
		}
	})

	t.Run("tags win over filename", func(t *testing.T) {
		identity := inferFileIdentity(scanFile("music/2Pac/New Folder/2Pac - Broken Wings.mp3", ScanFile{
			Artist: "2Pac", AlbumArtist: "2Pac", Album: "Real Album", Title: "Real Title", Track: intPtr(3),
		}))
		if identity.Album != "Real Album" || identity.Title != "Real Title" || *identity.Track != 3 {
			t.Fatalf("identity %+v", identity)
		}
	})

	t.Run("numberless filename pattern", func(t *testing.T) {
		identity := inferFileIdentity(scanFile("music/2Pac/New Folder/01 - 2Pac - Thugs Mansion.mp3", ScanFile{}))
		if identity.Title != "Thugs Mansion" || identity.Artist != "2Pac" || identity.Track == nil || *identity.Track != 1 {
			t.Fatalf("identity %+v", identity)
		}
	})

	t.Run("placeholder album tags are dropped", func(t *testing.T) {
		for _, album := range []string{"<Unknown>", "Unknown", "-Unknown-", "New Folder", "None"} {
			identity := inferFileIdentity(scanFile("music/Adam Sandler/Unknown/Adam Sandler - Flat Butts.mp3", ScanFile{
				Artist: "Adam Sandler", AlbumArtist: "Adam Sandler", Album: album, Title: "Flat Butts",
			}))
			if identity.Album != "" {
				t.Fatalf("album %q was kept", album)
			}
		}
	})

	t.Run("cd folder contributes release name and disc", func(t *testing.T) {
		identity := inferFileIdentity(scanFile("music/Artiste inconnu/Americaz Crucify [CD2]/Makaveli - Good Life.wma", ScanFile{Artist: "Makaveli"}))
		if identity.Album != "Americaz Crucify" || identity.Disc == nil || *identity.Disc != 2 {
			t.Fatalf("identity %+v", identity)
		}
	})
}

func TestIdentityArtistCandidates(t *testing.T) {
	identity := inferFileIdentity(scanFileTest("music/5th Lane/Tech N9ne Guest Appearances/Tech N9ne - I Go Again.mp3", ScanFile{
		Artist: "Tech N9ne", AlbumArtist: "5th Lane", Title: "I Go Again",
	}))
	candidates := identityArtistCandidates(identity)
	if len(candidates) != 2 || candidates[0] != "5th Lane" || candidates[1] != "Tech N9ne" {
		t.Fatalf("candidates %v", candidates)
	}
	featured := inferFileIdentity(scanFileTest("music/Calvin Harris/Single/song.mp3", ScanFile{
		Artist: "Calvin Harris feat. Rihanna", Title: "Song",
	}))
	featuredCandidates := identityArtistCandidates(featured)
	if len(featuredCandidates) != 2 || featuredCandidates[0] != "Calvin Harris feat. Rihanna" || featuredCandidates[1] != "Calvin Harris" {
		t.Fatalf("featured candidates %v", featuredCandidates)
	}
	placeholder := inferFileIdentity(scanFileTest("music/Artiste inconnu/Americaz Crucify/Makaveli - Let's B Friends.wma", ScanFile{Artist: "Makaveli", AlbumArtist: "Artiste inconnu"}))
	placeholderCandidates := identityArtistCandidates(placeholder)
	if len(placeholderCandidates) != 1 || placeholderCandidates[0] != "Makaveli" {
		t.Fatalf("placeholder candidates %v", placeholderCandidates)
	}
}

func scanFileTest(path string, tags ScanFile) ScanFile {
	tags.RelativePath = path
	return tags
}

func TestPlaceholderNameFolds(t *testing.T) {
	for _, value := range []string{"", "<Unknown>", "unknown", "-Unknown-", "New Folder", "None", "Artiste inconnu", "Unknown Artist", "[Unknown]", "untitled"} {
		if !isPlaceholderName(value) {
			t.Errorf("%q should be a placeholder", value)
		}
	}
	for _, value := range []string{"Insomnia", "Special 2 Pac", "Broken Wings", "Greatest Hits 1974-1978"} {
		if isPlaceholderName(value) {
			t.Errorf("%q should not be a placeholder", value)
		}
	}
	if !strings.EqualFold(compactMatchName("<Unknown>"), "unknown") {
		t.Fatal("compact folding changed")
	}
}
