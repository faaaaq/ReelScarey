package nfo

import (
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type parsedMovie struct {
	XMLName   xml.Name `xml:"movie"`
	Title     string   `xml:"title"`
	Year      int      `xml:"year"`
	Plot      string   `xml:"plot"`
	Premiered string   `xml:"premiered"`
	UniqueID  struct {
		Type  string `xml:"type,attr"`
		Value int64  `xml:",chardata"`
	} `xml:"uniqueid"`
	Season  *int `xml:"season"`
	Episode *int `xml:"episode"`
}

type parsedEpisode struct {
	XMLName   xml.Name `xml:"episodedetails"`
	Title     string   `xml:"title"`
	Year      int      `xml:"year"`
	Season    int      `xml:"season"`
	Episode   int      `xml:"episode"`
	Plot      string   `xml:"plot"`
	Premiered string   `xml:"premiered"`
	UniqueID  struct {
		Type  string `xml:"type,attr"`
		Value int64  `xml:",chardata"`
	} `xml:"uniqueid"`
}

func TestRenderMovieEscapesAndRoundTrips(t *testing.T) {
	title := `A & B <tag> "quoted" 'apos'`
	plot := "Line one & <two>\n]]> 100% <script>alert(1)</script>"
	raw, err := RenderMovie(Movie{Title: title, Year: 2021, TMDbID: 12345, Plot: plot, Premiered: "2021-10-31"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.HasPrefix(text, "<?xml") || !strings.Contains(text, "<movie>") {
		t.Fatalf("unexpected document: %s", text)
	}
	for _, escaped := range []string{"&amp;", "&lt;tag&gt;", "&#34;quoted&#34;", "&#39;apos&#39;"} {
		if !strings.Contains(text, escaped) {
			t.Fatalf("missing escaped %q in %s", escaped, text)
		}
	}
	var parsed parsedMovie
	if err = xml.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Title != title || parsed.Plot != plot || parsed.Year != 2021 || parsed.Premiered != "2021-10-31" {
		t.Fatalf("round trip changed fields: %+v", parsed)
	}
	if parsed.UniqueID.Type != "tmdb" || parsed.UniqueID.Value != 12345 {
		t.Fatalf("uniqueid = %+v", parsed.UniqueID)
	}
	if parsed.Season != nil || parsed.Episode != nil {
		t.Fatal("movie NFO must not carry episode fields")
	}
}

func TestRenderEpisodeIncludesBoundedFields(t *testing.T) {
	raw, err := RenderEpisode(Episode{Title: "Pilot & Start", Year: 2019, TMDbID: 777, Plot: "A <beginning>", Premiered: "2019-01-02", Season: 0, Episode: 1})
	if err != nil {
		t.Fatal(err)
	}
	var parsed parsedEpisode
	if err = xml.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Title != "Pilot & Start" || parsed.Season != 0 || parsed.Episode != 1 || parsed.Year != 2019 || parsed.Plot != "A <beginning>" || parsed.Premiered != "2019-01-02" || parsed.UniqueID.Value != 777 {
		t.Fatalf("unexpected episode NFO: %+v", parsed)
	}
	if strings.Contains(string(raw), "<movie>") {
		t.Fatal("episode NFO used the movie root")
	}
}

func TestRenderOmitsUnknownOptionalFields(t *testing.T) {
	raw, err := RenderMovie(Movie{Title: "Only Title"})
	if err != nil {
		t.Fatal(err)
	}
	for _, element := range []string{"<year>", "<plot>", "<premiered>", "<uniqueid"} {
		if strings.Contains(string(raw), element) {
			t.Fatalf("unexpected %s in %s", element, raw)
		}
	}
}

func TestRenderRequiresValidFields(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
	}{
		{"movie without title", func() error { _, err := RenderMovie(Movie{Year: 2020}); return err }},
		{"movie blank title", func() error { _, err := RenderMovie(Movie{Title: "   "}); return err }},
		{"movie invalid utf8 title", func() error { _, err := RenderMovie(Movie{Title: string([]byte{0xff, 'x'})}); return err }},
		{"long title", func() error {
			_, err := RenderMovie(Movie{Title: strings.Repeat("x", maxTitleRunes+1)})
			return err
		}},
		{"bad year", func() error { _, err := RenderMovie(Movie{Title: "A", Year: 9999}); return err }},
		{"negative tmdb", func() error { _, err := RenderMovie(Movie{Title: "A", TMDbID: -1}); return err }},
		{"bad premiered", func() error { _, err := RenderMovie(Movie{Title: "A", Premiered: "2020-13-40"}); return err }},
		{"oversized plot", func() error {
			_, err := RenderMovie(Movie{Title: "A", Plot: strings.Repeat("x", maxPlotBytes+1)})
			return err
		}},
		{"episode without title", func() error { _, err := RenderEpisode(Episode{Season: 1, Episode: 1}); return err }},
		{"episode zero", func() error { _, err := RenderEpisode(Episode{Title: "A", Season: 1}); return err }},
		{"negative season", func() error { _, err := RenderEpisode(Episode{Title: "A", Season: -1, Episode: 1}); return err }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestWriteFileAtomicRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "movie.nfo")
	first, err := RenderMovie(Movie{Title: "First"})
	if err != nil {
		t.Fatal(err)
	}
	if err = WriteFileAtomic(name, first, 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := RenderMovie(Movie{Title: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	if err = WriteFileAtomic(name, second, 0o644); !errors.Is(err, ErrExists) {
		t.Fatalf("overwrite error = %v", err)
	}
	saved, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != string(first) {
		t.Fatal("existing NFO was replaced")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "movie.nfo" {
		t.Fatalf("temporary files were left behind: %v", entries)
	}
}

func TestWriteFileAtomicRefusesBadDestinations(t *testing.T) {
	dir := t.TempDir()
	if err := WriteFileAtomic(filepath.Join(dir, "missing", "movie.nfo"), []byte("x"), 0o644); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing directory error = %v", err)
	}
	if err := WriteFileAtomic(filepath.Join(dir, "movie.nfo"), []byte(strings.Repeat("x", MaxFileBytes+1)), 0o644); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized document error = %v", err)
	}
	if err := WriteFileAtomic("", []byte("x"), 0o644); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty name error = %v", err)
	}
}
