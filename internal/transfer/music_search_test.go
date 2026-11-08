package transfer

import "testing"

func TestMusicIdentityMatch(t *testing.T) {
	cases := []struct {
		title, artist, album string
		want                 bool
	}{
		{"Radiohead - OK Computer (1997) [FLAC]", "Radiohead", "OK Computer", true},
		{"Radiohead-OK Computer-1997-FLAC-GROUP", "Radiohead", "OK Computer", true},
		{"Some Other Band - Different Album [FLAC]", "Radiohead", "OK Computer", false},
		{"VA - OK Computer Tribute [MP3]", "Radiohead", "OK Computer", false},
	}
	for _, tc := range cases {
		parsed := ParseMusicRelease(tc.title)
		if got := musicIdentityMatch(parsed, tc.title, tc.artist, tc.album); got != tc.want {
			t.Fatal(tc.title, got, parsed)
		}
	}
	if !musicSame("OK Computer", "ok computer") || musicSame("OK Computer", "Kid A") {
		t.Fatal("musicSame")
	}
}
