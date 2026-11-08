package music

import "testing"

func TestNormalizeMatchNameFoldsCasePunctuationAndUnicode(t *testing.T) {
	cases := []struct {
		left, right string
	}{
		{"OK Computer", "ok  computer"},
		{"Don't Stop Me Now", "Don\u2019t Stop Me Now"},
		{"Don't Stop Me Now", "Don't Stop Me Now!"},
		{"Sigur R\u00f3s", "Sigur Ro\u0301s"},
		{"Sigur R\u00f3s", "Sigur Ros"},
		{"Mötley Crüe", "Motley Crue"},
		{"AC/DC", "ACDC"},
		{"Album \u2013 Deluxe Edition", "Album - Deluxe Edition"},
		{"Album\u00a0Two", "Album Two"},
		{"Bj\u00f6rk", "Bjo\u0308rk"},
		{"Sgt. Pepper's", "Sgt Pepper's"},
	}
	for _, tc := range cases {
		if !matchNameEqual(tc.left, tc.right) {
			t.Errorf("expected %q and %q to match (%q vs %q)", tc.left, tc.right, normalizeMatchName(tc.left), normalizeMatchName(tc.right))
		}
	}
	if matchNameEqual("Album One", "Album Two") {
		t.Fatal("different titles matched")
	}
}

func TestNormalizeArtistNameIgnoresLeadingTheAndVariousArtistsAliases(t *testing.T) {
	if !artistNamesEqual("The Beatles", "Beatles") {
		t.Fatal("leading article was not ignored")
	}
	if !artistNamesEqual("Various", "Various Artists") {
		t.Fatal("various-artists alias was not folded")
	}
	if artistNamesEqual("The The", "The") {
		t.Fatal("leading article folding produced an empty artist")
	}
}

func TestPrimaryArtistNameStripsFeaturedCredit(t *testing.T) {
	for _, input := range []string{"Calvin Harris feat. Rihanna", "Calvin Harris ft. Rihanna", "Calvin Harris featuring Rihanna"} {
		primary, ok := primaryArtistName(input)
		if !ok || primary != "Calvin Harris" {
			t.Fatalf("primary artist for %q = %q, %v", input, primary, ok)
		}
	}
	if _, ok := primaryArtistName("Florence + the Machine"); ok {
		t.Fatal("plain artist name was treated as a featured credit")
	}
}
