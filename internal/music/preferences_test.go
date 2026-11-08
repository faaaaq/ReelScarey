package music

import (
	"context"
	"errors"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func preferencesFixture(t *testing.T) (*Service, context.Context) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewService(db, ""), ctx
}

func TestPreferencesDefaultsAndRevision(t *testing.T) {
	service, ctx := preferencesFixture(t)
	prefs, err := service.GetPreferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !prefs.AllowLossless || prefs.MinBitrate != 0 || prefs.Revision != 1 {
		t.Fatal("unexpected defaults", prefs)
	}
	if len(prefs.PreferredFormats) != 3 || prefs.PreferredFormats[0] != "FLAC" || prefs.PreferredFormats[2] != "MP3" {
		t.Fatal("unexpected default formats", prefs.PreferredFormats)
	}

	saved, err := service.SavePreferences(ctx, MusicPreferences{
		AllowLossless:    false,
		MinBitrate:       192,
		PreferredFormats: []string{"flac", " MP3 ", "flac"},
		Revision:         1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Revision != 2 || saved.AllowLossless || saved.MinBitrate != 192 {
		t.Fatal("unexpected saved preferences", saved)
	}
	if len(saved.PreferredFormats) != 2 || saved.PreferredFormats[0] != "FLAC" || saved.PreferredFormats[1] != "MP3" {
		t.Fatal("formats were not normalized", saved.PreferredFormats)
	}
	reread, err := service.GetPreferences(ctx)
	if err != nil || reread.Revision != 2 || reread.MinBitrate != 192 {
		t.Fatal("preferences were not persisted", reread, err)
	}

	if _, err := service.SavePreferences(ctx, MusicPreferences{Revision: 1}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale revision accepted: %v", err)
	}
	if _, err := service.SavePreferences(ctx, MusicPreferences{Revision: 2}); err != nil {
		t.Fatal("current revision rejected", err)
	}
}

func TestPreferencesValidation(t *testing.T) {
	service, ctx := preferencesFixture(t)
	cases := []MusicPreferences{
		{MinBitrate: -1, Revision: 1},
		{MinBitrate: 321, Revision: 1},
		{PreferredFormats: []string{"AACX"}, Revision: 1},
		{PreferredFormats: []string{""}, Revision: 1},
		{PreferredFormats: []string{"FLAC", "ALAC", "MP3", "AAC", "OGG", "OPUS", "WAV", "FLAC"}, Revision: 1},
	}
	for _, in := range cases {
		if _, err := service.SavePreferences(ctx, in); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid preferences accepted: %+v", in)
		}
	}
	// An empty format list means any format is acceptable, and the allowed
	// boundary values are valid.
	if _, err := service.SavePreferences(ctx, MusicPreferences{AllowLossless: true, MinBitrate: 320, PreferredFormats: []string{}, Revision: 1}); err != nil {
		t.Fatal("empty format list rejected", err)
	}
}

func TestCandidateAllowed(t *testing.T) {
	prefs := MusicPreferences{AllowLossless: false, MinBitrate: 192, PreferredFormats: []string{"MP3", "AAC"}}
	cases := []struct {
		name     string
		format   string
		bitrate  int
		lossless bool
		want     bool
	}{
		{"preferred lossy", "mp3", 320, false, true},
		{"case insensitive", "AAC", 256, false, true},
		{"unlisted format", "OGG", 320, false, false},
		{"below minimum", "MP3", 128, false, false},
		{"lossless rejected", "FLAC", 900, true, false},
	}
	for _, test := range cases {
		if got := CandidateAllowed(prefs, test.format, test.bitrate, test.lossless); got != test.want {
			t.Fatalf("%s: got %v want %v", test.name, got, test.want)
		}
	}

	open := MusicPreferences{AllowLossless: true, MinBitrate: 0}
	for _, test := range []struct {
		format   string
		bitrate  int
		lossless bool
	}{
		{"FLAC", 900, true},
		{"OGG", 0, false},
		{"MP3", 96, false},
	} {
		if !CandidateAllowed(open, test.format, test.bitrate, test.lossless) {
			t.Fatalf("unrestricted preferences rejected %+v", test)
		}
	}
	if CandidateAllowed(MusicPreferences{AllowLossless: true, MinBitrate: 320}, "MP3", 319, false) {
		t.Fatal("bitrate below the minimum was allowed")
	}
}
