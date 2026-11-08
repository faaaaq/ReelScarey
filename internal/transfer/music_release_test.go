package transfer

import (
	"testing"
)

func TestParseMusicRelease(t *testing.T) {
	tests := []struct {
		input    string
		expected ParsedMusicRelease
	}{
		{
			input: "Radiohead - OK Computer (1997) [FLAC]",
			expected: ParsedMusicRelease{
				Artist:   "Radiohead",
				Album:    "OK Computer",
				Year:     func(i int) *int { return &i }(1997),
				Format:   "FLAC",
				Bitrate:  0,
				Lossless: true,
			},
		},
		{
			input: "Radiohead-OK Computer-1997-FLAC-GROUP",
			expected: ParsedMusicRelease{
				Artist:   "Radiohead",
				Album:    "OK Computer",
				Year:     func(i int) *int { return &i }(1997),
				Format:   "FLAC",
				Bitrate:  0,
				Lossless: true,
			},
		},
		{
			input: "Artist - Album [MP3 320]",
			expected: ParsedMusicRelease{
				Artist:   "Artist",
				Album:    "Album",
				Year:     nil,
				Format:   "MP3",
				Bitrate:  320,
				Lossless: false,
			},
		},
		{
			input: "Artist - Album (2020) [ALAC 24bit]",
			expected: ParsedMusicRelease{
				Artist:   "Artist",
				Album:    "Album",
				Year:     func(i int) *int { return &i }(2020),
				Format:   "ALAC",
				Bitrate:  0,
				Lossless: true,
			},
		},
		{
			input: "Artist - Album [V0]",
			expected: ParsedMusicRelease{
				Artist:   "Artist",
				Album:    "Album",
				Year:     nil,
				Format:   "MP3",
				Bitrate:  0,
				Lossless: false,
			},
		},
		{
			input: "unparseable",
			expected: ParsedMusicRelease{
				Artist:   "unparseable",
				Album:    "",
				Year:     nil,
				Format:   "",
				Bitrate:  0,
				Lossless: false,
			},
		},
	}

	for _, tc := range tests {
		res := ParseMusicRelease(tc.input)

		if res.Artist != tc.expected.Artist {
			t.Errorf("Input %q: expected Artist %q, got %q", tc.input, tc.expected.Artist, res.Artist)
		}
		if res.Album != tc.expected.Album {
			t.Errorf("Input %q: expected Album %q, got %q", tc.input, tc.expected.Album, res.Album)
		}
		if (res.Year == nil) != (tc.expected.Year == nil) {
			t.Errorf("Input %q: expected Year nil %v, got %v", tc.input, tc.expected.Year == nil, res.Year == nil)
		} else if res.Year != nil && *res.Year != *tc.expected.Year {
			t.Errorf("Input %q: expected Year %d, got %d", tc.input, *tc.expected.Year, *res.Year)
		}
		if res.Format != tc.expected.Format {
			t.Errorf("Input %q: expected Format %q, got %q", tc.input, tc.expected.Format, res.Format)
		}
		if res.Bitrate != tc.expected.Bitrate {
			t.Errorf("Input %q: expected Bitrate %d, got %d", tc.input, tc.expected.Bitrate, res.Bitrate)
		}
		if res.Lossless != tc.expected.Lossless {
			t.Errorf("Input %q: expected Lossless %v, got %v", tc.input, tc.expected.Lossless, res.Lossless)
		}
	}
}
