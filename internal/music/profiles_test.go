package music

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

// profilesFixture opens a database in a fixed directory so a test can reopen
// the same database and verify persistence across close.
func profilesFixture(t *testing.T) (*Service, *sql.DB, context.Context, string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewService(db, ""), db, ctx, dir
}

func TestBuiltinProfiles(t *testing.T) {
	builtins := BuiltinProfiles()
	if len(builtins) != 12 {
		t.Fatalf("expected 12 builtin profiles, got %d", len(builtins))
	}
	want := []struct {
		key       string
		name      string
		codec     string
		container string
		bitrate   int
		vbr       bool
	}{
		{"original", "Original", "original", "", 0, true},
		{"lossless-archive", "Lossless Archive", "flac", "flac", 0, true},
		{"lossless-opus", "Lossless Opus", "opus", "opus", 128, true},
		{"opus-economy", "Opus Economy", "opus", "opus", 64, true},
		{"opus-balanced", "Opus Balanced", "opus", "opus", 96, true},
		{"opus-high", "Opus High", "opus", "opus", 128, true},
		{"opus-premium", "Opus Premium", "opus", "opus", 160, true},
		{"opus-maximum", "Opus Maximum", "opus", "opus", 192, true},
		{"mp3-compatible", "MP3 Compatible", "mp3", "mp3", 192, false},
		{"mp3-maximum", "MP3 Maximum", "mp3", "mp3", 320, false},
		{"aac-compatible", "AAC Compatible", "aac", "m4a", 128, false},
		{"aac-high", "AAC High", "aac", "m4a", 256, true},
	}
	for i, want := range want {
		got := builtins[i]
		if got.Key != want.key || got.Name != want.name || got.Codec != want.codec || got.Container != want.container || got.BitrateKbps != want.bitrate || got.VBR != want.vbr {
			t.Fatalf("builtin %d: got %+v want %+v", i, got, want)
		}
		if got.Custom {
			t.Fatalf("builtin %d must not be a custom row", i)
		}
	}
	// The original codec means no conversion is ever applied, and the
	// lossless profiles keep the original file.
	if builtins[0].Codec != "original" {
		t.Fatal("original profile must use the original codec")
	}
	if !builtins[1].KeepOriginal || !builtins[2].KeepOriginal {
		t.Fatal("lossless profiles must keep the original file")
	}
}

func TestProfileByKey(t *testing.T) {
	for _, profile := range BuiltinProfiles() {
		got, ok := ProfileByKey(profile.Key)
		if !ok || got.Key != profile.Key {
			t.Fatalf("ProfileByKey(%s) returned %+v ok=%v", profile.Key, got, ok)
		}
	}
	if _, ok := ProfileByKey(""); ok {
		t.Fatal("empty key must not match a builtin")
	}
	if _, ok := ProfileByKey("opus-96"); ok {
		t.Fatal("unknown key must not match a builtin")
	}
}

func TestCustomProfileValidation(t *testing.T) {
	service, _, ctx, _ := profilesFixture(t)
	cases := []struct {
		name    string
		profile QualityProfile
	}{
		{"bad codec", QualityProfile{Name: "Custom", Codec: "ogg"}},
		{"bad codec case", QualityProfile{Name: "Custom", Codec: "MP3"}},
		{"bad codec empty", QualityProfile{Name: "Custom"}},
		{"bad name empty", QualityProfile{Codec: "mp3", BitrateKbps: 192}},
		{"bad name too long", QualityProfile{Name: strings.Repeat("a", 101), Codec: "mp3", BitrateKbps: 192}},
		{"bad name control char", QualityProfile{Name: "Custom\n", Codec: "mp3", BitrateKbps: 192}},
		{"bad bitrate lossy zero", QualityProfile{Name: "Custom", Codec: "opus", BitrateKbps: 0}},
		{"bad bitrate lossy high", QualityProfile{Name: "Custom", Codec: "opus", BitrateKbps: 321}},
		{"bad bitrate lossy negative", QualityProfile{Name: "Custom", Codec: "opus", BitrateKbps: -1}},
		{"bad bitrate lossless", QualityProfile{Name: "Custom", Codec: "flac", BitrateKbps: 192}},
		{"bad bitrate original", QualityProfile{Name: "Custom", Codec: "original", BitrateKbps: 192}},
		{"bad compression level", QualityProfile{Name: "Custom", Codec: "flac", CompressionLevel: 9}},
		{"bad container chars", QualityProfile{Name: "Custom", Codec: "opus", BitrateKbps: 96, Container: "opus-1"}},
		{"bad container too long", QualityProfile{Name: "Custom", Codec: "opus", BitrateKbps: 96, Container: "opus-96x"}},
	}
	for _, test := range cases {
		if _, err := service.SaveCustomProfile(ctx, test.profile); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s: invalid profile accepted: %v", test.name, err)
		}
	}
}

func TestCustomProfileSaveAndList(t *testing.T) {
	service, _, ctx, _ := profilesFixture(t)
	saved, err := service.SaveCustomProfile(ctx, QualityProfile{Name: " Opus 96 ", Codec: "opus", Container: "opus", BitrateKbps: 96, VBR: true})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID <= 0 || saved.Name != "Opus 96" || saved.Codec != "opus" || saved.Container != "opus" || saved.BitrateKbps != 96 || saved.Key != fmt.Sprintf("custom-%d", saved.ID) || !saved.Custom {
		t.Fatalf("unexpected saved profile %+v", saved)
	}
	// The name is trimmed and the stored row persists.
	if _, ok := ProfileByKey("Opus 96"); ok {
		t.Fatal("custom profile must not be a builtin")
	}
	listed, err := service.ListProfiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 13 {
		t.Fatalf("expected 12 builtins + 1 custom, got %d", len(listed))
	}
	custom := listed[len(listed)-1]
	if custom.ID != saved.ID || !custom.Custom {
		t.Fatalf("custom profile not listed: %+v", custom)
	}

	// A custom profile can be updated by ID and deleted.
	updated, err := service.SaveCustomProfile(ctx, QualityProfile{ID: saved.ID, Name: "Opus 96 v2", Codec: "opus", BitrateKbps: 96})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Opus 96 v2" {
		t.Fatalf("update did not apply: %+v", updated)
	}
	if err := service.DeleteCustomProfile(ctx, saved.ID); err != nil {
		t.Fatal(err)
	}
	listed, err = service.ListProfiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 12 {
		t.Fatalf("expected 12 builtins after delete, got %d", len(listed))
	}
	// Unknown profile IDs are rejected.
	if err := service.DeleteCustomProfile(ctx, 999); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unknown profile delete accepted: %v", err)
	}
}

func TestCustomProfilePersistenceAcrossReopen(t *testing.T) {
	service, db, ctx, dir := profilesFixture(t)
	saved, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "Opus 96", Codec: "opus", Container: "opus", BitrateKbps: 96, VBR: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	service2 := NewService(reopened, "")
	listed, err := service2.ListProfiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 13 {
		t.Fatalf("expected 12 builtins + 1 custom, got %d", len(listed))
	}
	custom := listed[len(listed)-1]
	if custom.ID != saved.ID || custom.Name != "Opus 96" || custom.Codec != "opus" || custom.BitrateKbps != 96 || !custom.Custom {
		t.Fatalf("custom profile not persisted across reopen: %+v", custom)
	}
	// The custom profile is resolved by its name.
	resolved, err := service2.ResolveProfileKey(ctx, custom.Name)
	if err != nil {
		t.Fatalf("custom profile not resolved: %v", err)
	}
	if resolved.ID != saved.ID {
		t.Fatalf("resolved profile not the custom row: %+v", resolved)
	}
}

func TestResolveProfilePrecedence(t *testing.T) {
	global := MusicPreferences{DownloadProfile: "opus-high"}
	// Explicit overrides the album override and the global preference.
	if profile, err := ResolveProfile(global, "opus-balanced", "mp3-maximum"); err != nil || profile.Key != "mp3-maximum" {
		t.Fatalf("explicit key not resolved: %+v %v", profile, err)
	}
	// The album override wins over the global preference.
	if profile, err := ResolveProfile(global, "opus-balanced", ""); err != nil || profile.Key != "opus-balanced" {
		t.Fatalf("album override not resolved: %+v %v", profile, err)
	}
	// The global preference is the fallback.
	if profile, err := ResolveProfile(global, "", ""); err != nil || profile.Key != "opus-high" {
		t.Fatalf("global preference not resolved: %+v %v", profile, err)
	}
	// The original codec means no conversion is ever applied.
	if profile, err := ResolveProfile(global, "", "original"); err != nil || profile.Codec != "original" {
		t.Fatalf("original codec not resolved: %+v %v", profile, err)
	}
	// Without any key, resolution is an error.
	if _, err := ResolveProfile(MusicPreferences{}, "", ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty key accepted: %v", err)
	}
	// An invalid key is never silently resolved.
	if _, err := ResolveProfile(global, "", "opus-999"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unknown key accepted: %v", err)
	}
}

func TestPreferencesTranscodingFieldsRoundTrip(t *testing.T) {
	service, db, ctx, dir := profilesFixture(t)

	// The seeded/migrated default of transcoding_enabled=1 reads as true.
	prefs, err := service.GetPreferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !prefs.TranscodingEnabled {
		t.Fatalf("seeded transcoding_enabled default did not read as true: %+v", prefs)
	}

	saved, err := service.SavePreferences(ctx, MusicPreferences{
		AllowLossless:      false,
		MinBitrate:         192,
		DownloadProfile:    "opus-balanced",
		StreamingQuality:   "premium",
		OriginalRetention:  "ask",
		FallbackPolicy:     "approve",
		TranscodingEnabled: false,
		CacheLimitMB:       4096,
		MaxConversions:     4,
		Revision:           1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.DownloadProfile != "opus-balanced" || saved.StreamingQuality != "premium" || saved.OriginalRetention != "ask" || saved.FallbackPolicy != "approve" || saved.TranscodingEnabled || saved.CacheLimitMB != 4096 || saved.MaxConversions != 4 {
		t.Fatalf("unexpected saved preferences %+v", saved)
	}
	reread, err := service.GetPreferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if reread.DownloadProfile != "opus-balanced" || reread.StreamingQuality != "premium" || reread.OriginalRetention != "ask" || reread.FallbackPolicy != "approve" || reread.TranscodingEnabled || reread.CacheLimitMB != 4096 || reread.MaxConversions != 4 {
		t.Fatalf("transcoding fields were not persisted: %+v", reread)
	}

	// A user-provided true must not be silently flipped to false.
	if _, err := service.SavePreferences(ctx, MusicPreferences{
		DownloadProfile:    "opus-balanced",
		TranscodingEnabled: true,
		CacheLimitMB:       512,
		MaxConversions:     2,
		Revision:           2,
	}); err != nil {
		t.Fatal(err)
	}
	flipped, err := service.GetPreferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !flipped.TranscodingEnabled {
		t.Fatalf("user-provided true was silently flipped to false: %+v", flipped)
	}
	// A user-provided false must not be silently flipped to true either.
	if _, err := service.SavePreferences(ctx, MusicPreferences{
		DownloadProfile:    "opus-balanced",
		TranscodingEnabled: false,
		CacheLimitMB:       512,
		MaxConversions:     2,
		Revision:           3,
	}); err != nil {
		t.Fatal(err)
	}
	fresh, err := service.GetPreferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.TranscodingEnabled {
		t.Fatalf("user-provided false was silently flipped to true: %+v", fresh)
	}
	if fresh.DownloadProfile != "opus-balanced" || fresh.StreamingQuality != "balanced" || fresh.OriginalRetention != "keep" || fresh.FallbackPolicy != "best" || fresh.CacheLimitMB != 512 || fresh.MaxConversions != 2 {
		t.Fatalf("unexpected preference defaults %+v", fresh)
	}
	// The values persist across reopen.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	service2 := NewService(reopened, "")
	after, err := service2.GetPreferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.DownloadProfile != "opus-balanced" || after.StreamingQuality != "balanced" || after.OriginalRetention != "keep" || after.FallbackPolicy != "best" || after.CacheLimitMB != 512 || after.MaxConversions != 2 {
		t.Fatalf("transcoding fields were not persisted across reopen: %+v", after)
	}
	if after.TranscodingEnabled {
		t.Fatalf("user-provided false did not persist across reopen: %+v", after)
	}
}

func TestPreferencesTranscodingFieldsValidation(t *testing.T) {
	service, _, ctx, _ := profilesFixture(t)
	valid := func() MusicPreferences {
		return MusicPreferences{
			AllowLossless:      false,
			MinBitrate:         192,
			DownloadProfile:    "opus-balanced",
			StreamingQuality:   "premium",
			OriginalRetention:  "ask",
			FallbackPolicy:     "approve",
			TranscodingEnabled: false,
			CacheLimitMB:       512,
			MaxConversions:     2,
			Revision:           1,
		}
	}
	cases := []struct {
		name   string
		mutate func(*MusicPreferences)
	}{
		{"streaming quality unknown", func(p *MusicPreferences) { p.StreamingQuality = "premium2" }},
		{"streaming quality uppercase", func(p *MusicPreferences) { p.StreamingQuality = "PREMIUM" }},
		{"streaming quality trailing space", func(p *MusicPreferences) { p.StreamingQuality = "premium " }},
		{"retention unknown", func(p *MusicPreferences) { p.OriginalRetention = "discard" }},
		{"retention uppercase", func(p *MusicPreferences) { p.OriginalRetention = "KEEP" }},
		{"fallback unknown", func(p *MusicPreferences) { p.FallbackPolicy = "approve2" }},
		{"fallback uppercase", func(p *MusicPreferences) { p.FallbackPolicy = "APPROVE" }},
		{"cache limit too low", func(p *MusicPreferences) { p.CacheLimitMB = 63 }},
		{"cache limit too high", func(p *MusicPreferences) { p.CacheLimitMB = 4097 }},
		{"conversions negative", func(p *MusicPreferences) { p.MaxConversions = -1 }},
		{"conversions too high", func(p *MusicPreferences) { p.MaxConversions = 5 }},
		{"download profile unknown", func(p *MusicPreferences) { p.DownloadProfile = "opus-999" }},
	}
	for _, test := range cases {
		p := valid()
		test.mutate(&p)
		if _, err := service.SavePreferences(ctx, p); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s: invalid preferences accepted: %v", test.name, err)
		}
	}
	// Boundary values are valid.
	for _, test := range []struct {
		name   string
		mutate func(*MusicPreferences)
	}{
		{"cache limit low", func(p *MusicPreferences) { p.CacheLimitMB = 64 }},
		{"cache limit high", func(p *MusicPreferences) { p.CacheLimitMB = 4096 }},
		{"conversions low", func(p *MusicPreferences) { p.MaxConversions = 1 }},
		{"conversions high", func(p *MusicPreferences) { p.MaxConversions = 4 }},
		{"streaming quality data-saver", func(p *MusicPreferences) { p.StreamingQuality = "data-saver" }},
		{"streaming quality original", func(p *MusicPreferences) { p.StreamingQuality = "original" }},
		{"retention delete", func(p *MusicPreferences) { p.OriginalRetention = "delete" }},
		{"fallback skip", func(p *MusicPreferences) { p.FallbackPolicy = "skip" }},
		{"download profile empty", func(p *MusicPreferences) { p.DownloadProfile = "" }},
	} {
		p := valid()
		test.mutate(&p)
		current, err := service.GetPreferences(ctx)
		if err != nil {
			t.Fatal(err)
		}
		p.Revision = current.Revision
		if _, err := service.SavePreferences(ctx, p); err != nil {
			t.Fatalf("%s: valid preferences rejected: %v", test.name, err)
		}
	}
}

func TestCustomProfileNameCollisionsRejected(t *testing.T) {
	service, _, ctx, _ := profilesFixture(t)
	// A builtin key can never be a custom name: ResolveProfileKey checks
	// builtins first, so such a row would be unreachable by name.
	if _, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "original", Codec: "opus", Container: "opus", BitrateKbps: 96}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("builtin-key name accepted: %v", err)
	}
	if _, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "Original", Codec: "opus", Container: "opus", BitrateKbps: 96}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("builtin-key name accepted: %v", err)
	}
	// The generated custom-<id> namespace is reserved.
	if _, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "custom-5", Codec: "opus", Container: "opus", BitrateKbps: 96}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("generated-key namespace name accepted: %v", err)
	}
	// Names that merely start with custom- stay allowed.
	saved, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "custom-lossless", Codec: "flac"})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Key != fmt.Sprintf("custom-%d", saved.ID) {
		t.Fatalf("allowed custom- prefixed name lost its generated key: %+v", saved)
	}
	// Duplicate names are a typed conflict (the name column is UNIQUE).
	if _, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "Custom-Lossless", Codec: "opus", Container: "opus", BitrateKbps: 96}); !errors.Is(err, ErrProfileNameTaken) {
		t.Fatalf("duplicate name not a conflict: %v", err)
	}
	// Renaming a row onto another row's name is also a conflict.
	other, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "Second Profile", Codec: "opus", Container: "opus", BitrateKbps: 128})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SaveCustomProfile(ctx, QualityProfile{ID: other.ID, Name: "custom-lossless", Codec: "opus", Container: "opus", BitrateKbps: 128}); !errors.Is(err, ErrProfileNameTaken) {
		t.Fatalf("rename onto taken name not a conflict: %v", err)
	}
	if _, err := service.SaveCustomProfile(ctx, QualityProfile{ID: other.ID, Name: "Second Profile", Codec: "opus", Container: "opus", BitrateKbps: 128}); err != nil {
		t.Fatalf("unchanged update wrongly rejected: %v", err)
	}
}
