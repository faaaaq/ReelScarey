package music

import (
	"context"
	"errors"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

// Regression tests written by the independent audit of the music quality
// profile resolution (audit_p_*): custom profile keys must be resolvable
// everywhere a profile is stored, deleted custom profiles must fail safe
// (import unchanged or honest error), and legacy no-profile behavior must
// stay exactly as before.

func auditPProfilesFixture(t *testing.T) (*Service, context.Context) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES(1,?, 'Artist',1,'missing',1,1)`, testMBID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(1,1,?, 'Album',1,'missing',1,1)`, testRGID); err != nil {
		t.Fatal(err)
	}
	return NewService(db, t.TempDir()), ctx
}

// auditPSetPreferences saves the global preferences after one mutation so the
// revision fence stays satisfied.
func auditPSetPreferences(t *testing.T, service *Service, ctx context.Context, mutate func(*MusicPreferences)) {
	t.Helper()
	prefs, err := service.GetPreferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mutate(&prefs)
	if _, err := service.SavePreferences(ctx, prefs); err != nil {
		t.Fatal(err)
	}
}

// auditPSetAlbumOverride saves the per-album override after one mutation so
// the album revision fence stays satisfied.
func auditPSetAlbumOverride(t *testing.T, service *Service, ctx context.Context, albumID int64, mutate func(*MusicPreferences)) {
	t.Helper()
	current, err := service.AlbumPreferences(ctx, albumID)
	if err != nil {
		t.Fatal(err)
	}
	in := current.Preferences
	in.Revision = current.Revision
	mutate(&in)
	if _, err := service.SaveAlbumPreferences(ctx, albumID, in); err != nil {
		t.Fatal(err)
	}
}

// TestSavePreferencesAcceptsCustomProfileKey covers the custom profile key
// acceptance: the settings page offers custom profiles as the default
// download profile, so the saved value must resolve at save time.
func TestSavePreferencesAcceptsCustomProfileKey(t *testing.T) {
	service, ctx := auditPProfilesFixture(t)
	custom, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "Car Opus", Codec: "opus", BitrateKbps: 128})
	if err != nil {
		t.Fatal(err)
	}
	auditPSetPreferences(t, service, ctx, func(p *MusicPreferences) { p.DownloadProfile = custom.Key })
	prefs, err := service.GetPreferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if prefs.DownloadProfile != custom.Key {
		t.Fatalf("custom profile key not stored: %q", prefs.DownloadProfile)
	}

	// The custom profile name is accepted as well (resolveStoredProfile
	// matches by name), and the stored value stays the stable key.
	auditPSetPreferences(t, service, ctx, func(p *MusicPreferences) { p.DownloadProfile = custom.Name })
	prefs, err = service.GetPreferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if prefs.DownloadProfile != custom.Key {
		t.Fatalf("custom profile name not normalized to the key: %q", prefs.DownloadProfile)
	}

	// A deleted custom profile can no longer be saved.
	if err := service.DeleteCustomProfile(ctx, custom.ID); err != nil {
		t.Fatal(err)
	}
	auditPSetPreferencesExpectError(t, service, ctx, custom.Key)
	auditPSetPreferencesExpectError(t, service, ctx, "opus-999")
}

func auditPSetPreferencesExpectError(t *testing.T, service *Service, ctx context.Context, key string) {
	t.Helper()
	prefs, err := service.GetPreferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	prefs.DownloadProfile = key
	if _, err := service.SavePreferences(ctx, prefs); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("SavePreferences(%q) = %v, want ErrInvalidInput", key, err)
	}
}

// TestSaveAlbumPreferencesValidatesDownloadProfile covers the album override
// validation: the same resolution rule as the global preference applies, so
// garbage keys are rejected before they are stored.
func TestSaveAlbumPreferencesValidatesDownloadProfile(t *testing.T) {
	service, ctx := auditPProfilesFixture(t)
	custom, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "Car Opus", Codec: "opus", BitrateKbps: 128})
	if err != nil {
		t.Fatal(err)
	}
	auditPSetAlbumOverride(t, service, ctx, 1, func(p *MusicPreferences) { p.DownloadProfile = custom.Key })
	current, err := service.AlbumPreferences(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if current.Preferences.DownloadProfile != custom.Key {
		t.Fatalf("custom profile key not stored: %q", current.Preferences.DownloadProfile)
	}
	if err := service.DeleteCustomProfile(ctx, custom.ID); err != nil {
		t.Fatal(err)
	}
	auditPSetAlbumOverrideExpectError(t, service, ctx, custom.Key)
	auditPSetAlbumOverrideExpectError(t, service, ctx, "opus-999")
}

func auditPSetAlbumOverrideExpectError(t *testing.T, service *Service, ctx context.Context, key string) {
	t.Helper()
	current, err := service.AlbumPreferences(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	in := current.Preferences
	in.Revision = current.Revision
	in.DownloadProfile = key
	if _, err := service.SaveAlbumPreferences(ctx, 1, in); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("SaveAlbumPreferences(%q) = %v, want ErrInvalidInput", key, err)
	}
}

// TestDeletedCustomProfileReferenceResolution covers the deleted-custom-profile
// fail-safe: a stored global preference or album override that references a
// since-deleted custom profile must resolve to no conversion (import unchanged
// or honest error), never crash or silently convert to a wrong codec.
func TestDeletedCustomProfileReferenceResolution(t *testing.T) {
	service, ctx := auditPProfilesFixture(t)
	custom, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "Car Opus", Codec: "opus", BitrateKbps: 128})
	if err != nil {
		t.Fatal(err)
	}
	auditPSetPreferences(t, service, ctx, func(p *MusicPreferences) { p.DownloadProfile = custom.Key })
	prefs, err := service.GetPreferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := service.resolveStoredProfile(ctx, prefs, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if profile.Key != custom.Key {
		t.Fatalf("resolved profile = %+v, want %q", profile, custom.Key)
	}
	if err := service.DeleteCustomProfile(ctx, custom.ID); err != nil {
		t.Fatal(err)
	}
	// The deleted profile is no longer resolvable: the import path fails
	// safe to no conversion and the ranking path drops the profile.
	if _, err := service.resolveStoredProfile(ctx, prefs, "", ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("resolveStoredProfile with a deleted profile = %v, want ErrInvalidInput", err)
	}
	if plan := service.importProfilePlan(ctx, 1, 1); plan != nil {
		t.Fatalf("importProfilePlan with a deleted profile = %+v, want nil", plan)
	}
	if _, ok := service.MusicQualityProfile(ctx, 1); ok {
		t.Fatal("MusicQualityProfile applied a deleted profile")
	}
	if _, _, err := service.resolveAcquisitionProfile(ctx, custom.Key); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("resolveAcquisitionProfile with a deleted profile = %v, want ErrInvalidInput", err)
	}
}

// TestImportProfilePlanCustomProfileNameDropped verifies the corrected
// per-download behavior: a custom profile NAME stored on the acquisition
// job's format column resolves at import time instead of being silently
// dropped (the import.go defect found by the production-readiness audit and
// fixed there).
func TestImportProfilePlanCustomProfileNameDropped(t *testing.T) {
	service, ctx := auditPProfilesFixture(t)
	custom, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "Car Opus", Codec: "opus", BitrateKbps: 128})
	if err != nil {
		t.Fatal(err)
	}
	auditPSetPreferences(t, service, ctx, func(p *MusicPreferences) {
		p.TranscodingEnabled = true
		p.DownloadProfile = "original"
	})
	if _, err := service.store.db.Exec(`INSERT INTO music_acquisition_jobs(id,root,format,state,track_count,created_at,updated_at) VALUES(7,'Music',?, 'queued',1,1,1)`, custom.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.db.Exec(`INSERT INTO music_acquisition_tracks(id,job_id,position,title,artist,album,state,import_job_id,created_at,updated_at) VALUES(1,7,1,'Song','Artist','Album','completed',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	plan := service.importProfilePlan(ctx, 1, 1)
	if plan == nil {
		t.Fatal("per-download custom profile name was dropped at import time")
	}
	if plan.Profile.Key != custom.Key || plan.Profile.BitrateKbps != 128 {
		t.Fatalf("unexpected resolved plan profile: %+v", plan.Profile)
	}
}

// TestLegacyNoProfilePassthrough covers the legacy no-profile passthrough:
// the migration default 'original' resolves to no conversion (the source is
// imported unchanged), the legacy yt-dlp values resolve to the builtin keys
// and an empty download profile value resolves to no conversion.
func TestLegacyNoProfilePassthrough(t *testing.T) {
	service, ctx := auditPProfilesFixture(t)
	// The migration default is 'original' and resolves to no conversion.
	auditPSetPreferences(t, service, ctx, func(p *MusicPreferences) { p.DownloadProfile = "original" })
	prefs, err := service.GetPreferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if prefs.DownloadProfile != "original" {
		t.Fatalf("default download profile = %q, want original", prefs.DownloadProfile)
	}
	if plan := service.importProfilePlan(ctx, 1, 1); plan != nil {
		t.Fatalf("default plan = %+v, want nil", plan)
	}
	if _, ok := service.MusicQualityProfile(ctx, 1); ok {
		t.Fatal("MusicQualityProfile applied the default profile")
	}

	// An empty download profile value resolves to no conversion.
	auditPSetPreferences(t, service, ctx, func(p *MusicPreferences) { p.DownloadProfile = "" })
	prefs, err = service.GetPreferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if prefs.DownloadProfile != "" {
		t.Fatalf("empty download profile = %q, want empty", prefs.DownloadProfile)
	}
	if plan := service.importProfilePlan(ctx, 1, 1); plan != nil {
		t.Fatalf("empty plan = %+v, want nil", plan)
	}
	if _, ok := service.MusicQualityProfile(ctx, 1); ok {
		t.Fatal("MusicQualityProfile applied the empty profile")
	}

	// The legacy yt-dlp values resolve to the builtin keys.
	for _, legacy := range []struct {
		value    string
		wantKey  string
		wantCode string
	}{
		{"", "", "original"},
		{"source", "original", "original"},
		{"mp3", "mp3-maximum", "mp3"},
		{"flac", "lossless-archive", "flac"},
		{"m4a", "aac-high", "aac"},
		{"opus", "opus-maximum", "opus"},
	} {
		profile, key, err := service.resolveAcquisitionProfile(ctx, legacy.value)
		if err != nil {
			t.Fatalf("resolveAcquisitionProfile(%q) = %v", legacy.value, err)
		}
		if profile.Codec != legacy.wantCode {
			t.Fatalf("resolveAcquisitionProfile(%q) codec = %q, want %q", legacy.value, profile.Codec, legacy.wantCode)
		}
		if legacy.value == "" {
			if key != "" {
				t.Fatalf("resolveAcquisitionProfile(\"\") key = %q, want empty", key)
			}
			continue
		}
		if key != legacy.wantKey {
			t.Fatalf("resolveAcquisitionProfile(%q) key = %q, want %q", legacy.value, key, legacy.wantKey)
		}
	}
}
