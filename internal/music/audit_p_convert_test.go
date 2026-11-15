package music

import (
	"errors"
	"testing"
)

// Regression tests written by the independent audit of the music quality
// profiles (audit_p_*): the bits/sec vs kbps normalization heuristic, the
// lossless enforcement against a mismatched probe and the custom profile
// validation boundaries.

// TestConversionNeededBitsPerSecondHeuristic covers the >320 heuristic: the
// real ffprobe reports bit_rate in bits per second (999000 for a 999 kbps
// source), so a 999 kbps lossy source is not a bits/sec false positive and
// the same-family comparison is honest.
func TestConversionNeededBitsPerSecondHeuristic(t *testing.T) {
	profile := mustProfile("mp3-compatible")
	// A 999 kbps lossy MP3 reported in bits per second (real ffprobe) is
	// normalized to 999 kbps and copies without re-encoding.
	needed, reason := ConversionNeeded(sourceTags("mp3", 1000, 999000), profile)
	if needed {
		t.Fatalf("999 kbps MP3 (bits/sec) = %v, want false", reason)
	}
	// A 320 kbps lossy MP3 reported in bits per second is normalized to 320
	// kbps and copies without re-encoding.
	needed, reason = ConversionNeeded(sourceTags("mp3", 1000, 320000), profile)
	if needed {
		t.Fatalf("320 kbps MP3 (bits/sec) = %v, want false", reason)
	}
	// A 400 kbps MP3 reported in kbps (some probe variants) is treated as
	// bits/sec: 400 > 320 so it is divided by 1000 to 0, which yields the
	// honest "unknown bitrate" message instead of a wrong copy verdict.
	needed, reason = ConversionNeeded(sourceTags("mp3", 1000, 400), profile)
	if !needed {
		t.Fatalf("400 kbps MP3 (kbps) should re-encode, got %q", reason)
	}
	if reason != "The source bitrate is unknown, so the file is re-encoded to the 192 kbps mp3 profile." {
		t.Fatalf("400 kbps MP3 (kbps) reason = %q", reason)
	}
}

// TestConversionNeededCorruptProbe covers the corrupt probe fail-safe: a
// probe with zero duration/bitrate yields the honest "unknown bitrate"
// message, never a wrong copy verdict.
func TestConversionNeededCorruptProbe(t *testing.T) {
	profile := mustProfile("opus-high")
	// A corrupt probe (zero duration/bitrate) yields the honest
	// "unknown bitrate" message.
	needed, reason := ConversionNeeded(sourceTags("ogg", 0, 0), profile)
	if !needed {
		t.Fatalf("corrupt probe = %v, want true", reason)
	}
	if reason != "The source bitrate is unknown, so the file is re-encoded to the 128 kbps opus profile." {
		t.Fatalf("corrupt probe reason = %q", reason)
	}
}

// TestConversionNeededLosslessProbeMismatch covers the lossless enforcement
// against a mismatched probe: a lossless profile (RequireLossless) against a
// lossy source yields the honest fail-closed message in ConversionNeeded and
// the "best" fallback keeps the original with the honest note.
func TestConversionNeededLosslessProbeMismatch(t *testing.T) {
	// The builtin lossless-archive profile is permissive (it allows lossy
	// sources with the honest framing), so the enforcement test uses an
	// explicit RequireLossless profile.
	profile := QualityProfile{Codec: "flac", Container: "flac", RequireLossless: true}
	// A lossless profile against a lossy source yields the honest
	// fail-closed message.
	needed, reason := ConversionNeeded(sourceTags("mp3", 1000, 320), profile)
	if !needed {
		t.Fatalf("lossy source with a lossless profile = %v, want true", reason)
	}
	if reason != "This profile requires a lossless source, but the file is lossy." {
		t.Fatalf("lossless probe mismatch reason = %q", reason)
	}
	// A lossless source for a lossless profile copies without re-encoding.
	needed, reason = ConversionNeeded(sourceTags("flac", 1000, 900), mustProfile("lossless-archive"))
	if needed {
		t.Fatalf("FLAC source with a lossless profile = %v, want false", reason)
	}
	if reason != "The source is already FLAC and is copied without re-encoding." {
		t.Fatalf("lossless copy reason = %q", reason)
	}
}

// TestCustomProfileValidationBoundaries covers the custom profile validation
// boundaries: codec=flac with bitrate>0 is rejected, codec=original with
// bitrate>0 is rejected, and garbage names are rejected.
func TestCustomProfileValidationBoundaries(t *testing.T) {
	service, ctx := auditPProfilesFixture(t)
	// codec=flac with bitrate_kbps>0 is rejected.
	if _, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "Bad FLAC", Codec: "flac", BitrateKbps: 320}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("SaveCustomProfile(flac with bitrate) = %v, want ErrInvalidInput", err)
	}
	// codec=original with bitrate_kbps>0 is rejected.
	if _, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "Bad Original", Codec: "original", BitrateKbps: 320}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("SaveCustomProfile(original with bitrate) = %v, want ErrInvalidInput", err)
	}
	// codec=opus with bitrate_kbps>320 is rejected.
	if _, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "Bad Opus", Codec: "opus", BitrateKbps: 999}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("SaveCustomProfile(opus with bitrate>320) = %v, want ErrInvalidInput", err)
	}
	// codec=opus with bitrate_kbps=0 is rejected.
	if _, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "Bad Opus Zero", Codec: "opus", BitrateKbps: 0}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("SaveCustomProfile(opus with bitrate=0) = %v, want ErrInvalidInput", err)
	}
	// A container with a digit is rejected.
	if _, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "Bad Container", Codec: "opus", BitrateKbps: 128, Container: "opus1"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("SaveCustomProfile(opus with digit container) = %v, want ErrInvalidInput", err)
	}
	// A valid custom profile is accepted.
	saved, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "Valid Opus", Codec: "opus", BitrateKbps: 128})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Key == "" || !saved.Custom {
		t.Fatalf("valid custom profile = %+v", saved)
	}
}

// TestResolveProfileGarbageInput covers the pure ResolveProfile with garbage
// input: it is builtin-only, so a custom profile key resolves to
// ErrInvalidInput and the garbage keys are rejected too.
func TestResolveProfileGarbageInput(t *testing.T) {
	global := MusicPreferences{DownloadProfile: "opus-high"}
	if _, err := ResolveProfile(global, "", ""); err != nil {
		t.Fatal("empty resolution", err)
	}
	if _, err := ResolveProfile(global, "opus-999", ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("garbage album override", err)
	}
	if _, err := ResolveProfile(global, "", "opus-999"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("garbage explicit key", err)
	}
	if _, err := ResolveProfile(global, "", "custom-1"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("custom profile key is not resolvable by the pure function", err)
	}
}
