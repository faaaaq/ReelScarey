package transfer

import (
	"testing"

	"github.com/faaaaq/ReelScarey/internal/music"
)

func builtinProfile(t *testing.T, key string) music.QualityProfile {
	t.Helper()
	profile, ok := music.ProfileByKey(key)
	if !ok {
		t.Fatalf("unknown builtin profile %q", key)
	}
	return profile
}

// TestMusicProfileFit covers the ranking adjustment and honest labeling the
// album's effective quality profile applies to parsed music releases. The
// fit is candidate-selection only: the actual conversion happens in the
// music import step, so QueueMusicGuarded is unaffected.
func TestMusicProfileFit(t *testing.T) {
	cases := []struct {
		name      string
		profile   music.QualityProfile
		parsed    ParsedMusicRelease
		wantScore int
		wantFit   string
	}{
		{"no profile", music.QualityProfile{}, ParsedMusicRelease{Format: "MP3", Bitrate: 320}, 0, ""},
		{"original profile", builtinProfile(t, "original"), ParsedMusicRelease{Format: "MP3"}, 0, ""},
		{"lossless satisfies lossless requirement", music.QualityProfile{Codec: "flac", Container: "flac", RequireLossless: true}, ParsedMusicRelease{Format: "FLAC", Lossless: true}, 6, "yes"},
		{"lossy fails lossless requirement", music.QualityProfile{Codec: "flac", Container: "flac", RequireLossless: true}, ParsedMusicRelease{Format: "MP3", Bitrate: 320}, -100, "lossy"},
		{"flac profile prefers lossless", builtinProfile(t, "lossless-archive"), ParsedMusicRelease{Format: "MP3", Bitrate: 320}, -4, "lossy"},
		{"opus at target bitrate", builtinProfile(t, "opus-high"), ParsedMusicRelease{Format: "OPUS", Bitrate: 128}, 7, "yes"},
		{"opus above target bitrate", builtinProfile(t, "opus-balanced"), ParsedMusicRelease{Format: "OGG", Bitrate: 320}, 7, "yes"},
		{"opus below target bitrate", builtinProfile(t, "opus-high"), ParsedMusicRelease{Format: "OPUS", Bitrate: 96}, 2, "low"},
		{"opus unknown bitrate", builtinProfile(t, "opus-high"), ParsedMusicRelease{Format: "OPUS"}, 4, "unknown"},
		{"other codec family stays listed", builtinProfile(t, "opus-high"), ParsedMusicRelease{Format: "MP3", Bitrate: 320}, 0, "other"},
		{"mp3 profile at target", builtinProfile(t, "mp3-maximum"), ParsedMusicRelease{Format: "MP3", Bitrate: 320}, 7, "yes"},
		{"mp3 profile below target", builtinProfile(t, "mp3-compatible"), ParsedMusicRelease{Format: "MP3", Bitrate: 128}, 2, "low"},
		{"aac family", builtinProfile(t, "aac-high"), ParsedMusicRelease{Format: "AAC", Bitrate: 256}, 7, "yes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			score, fit := musicProfileFit(tc.profile, tc.parsed)
			if score != tc.wantScore || fit != tc.wantFit {
				t.Fatalf("musicProfileFit = (%d, %q), want (%d, %q)", score, fit, tc.wantScore, tc.wantFit)
			}
		})
	}
}

func TestMusicFormatFamily(t *testing.T) {
	cases := map[string]string{
		"FLAC": "lossless",
		"ALAC": "lossless",
		"WAV":  "lossless",
		"MP3":  "mp3",
		"AAC":  "aac",
		"OGG":  "opus",
		"OPUS": "opus",
		"":     "",
		"WMA":  "",
	}
	for format, want := range cases {
		if got := musicFormatFamily(format); got != want {
			t.Fatalf("musicFormatFamily(%q) = %q, want %q", format, got, want)
		}
	}
}
