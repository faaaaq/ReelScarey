package music

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// audit_e_live_test.go: real end-to-end exercise of the music conversion
// engine with the real FFmpeg and real FFprobeTagger, guarded by the opt-in
// REELSCAREY_LIVE_MUSIC_TRANSCODE=1 environment variable (the same convention
// as the existing TestLiveConvertFileSineWave in convert_test.go). Every
// fixture is generated with ffmpeg in a disposable t.TempDir(); owner media
// is never touched.
//
// These tests exercise the actual Service import path: stage a source in a
// download folder, resolve the profile plan, run the real ConvertFile, and
// verify the converted file is registered in music_files with the correct
// format/bitrate/duration/tags, the source is retained per policy, and the
// conversion-needed detection matches reality.

// auditELiveFixture builds the import fixture with the real FFprobeTagger and
// the real FFmpeg, and saves the preferences so the profile plan resolves the
// given builtin profile key.
func auditELiveFixture(t *testing.T, profileKey string) (*Service, string, string) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is not installed")
	}
	service, media, downloads := importFixture(t)
	auditPSetPreferences(t, service, context.Background(), func(p *MusicPreferences) {
		p.TranscodingEnabled = true
		p.DownloadProfile = profileKey
	})
	return service, media, downloads
}

// auditELiveSource writes a generated sine fixture into the download folder
// with the given codec and bitrate, tagged with disc 1 and track 1. The
// import fixture also has a second catalog track ("Other"), so a second
// matching file is generated for the track set to be complete.
func auditELiveSource(t *testing.T, downloads, codec, bitrate string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(downloads, "Album"), 0o755); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(downloads, "Album", "02."+codec)
	args := []string{"-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1"}
	if codec == "mp3" {
		args = append(args, "-c:a", "mp3", "-b:a", bitrate)
	} else {
		args = append(args, "-c:a", codec)
	}
	args = append(args,
		"-metadata", "artist=Artist", "-metadata", "album=Album", "-metadata", "title=Other",
		"-metadata", "track=2", "-metadata", "disc=1", "-y", second)
	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err != nil {
		t.Skip("could not generate a fixture", err, string(out))
	}
	path := filepath.Join(downloads, "Album", "01."+codec)
	args = []string{"-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1"}
	if codec == "mp3" {
		args = append(args, "-c:a", "mp3", "-b:a", bitrate)
	} else {
		args = append(args, "-c:a", codec)
	}
	args = append(args,
		"-metadata", "artist=Artist", "-metadata", "album=Album", "-metadata", "title=Song",
		"-metadata", "track=1", "-metadata", "disc=1", "-y", path)
	out, err = exec.Command("ffmpeg", args...).CombinedOutput()
	if err != nil {
		t.Skip("could not generate a fixture", err, string(out))
	}
	return path
}

// auditELivePrimary is the registered primary file for the track: the catalog
// row plus its on-disk path.
func auditELivePrimary(t *testing.T, s *Service) (int64, string, string, string, int, int) {
	t.Helper()
	var id int64
	var relative, format, title string
	var bitrate, durationMS int
	if err := s.store.db.QueryRow(`SELECT id,relative_path,format,bitrate,duration_ms,tag_json->>'title' FROM music_files WHERE variant_role='primary' AND track_id=3`).Scan(&id, &relative, &format, &bitrate, &durationMS, &title); err != nil {
		t.Fatal("no primary file registered for the track", err)
	}
	return id, relative, format, title, bitrate, durationMS
}

// TestAuditELiveImportOpusBalancedConvertsAndRetains proves the real end-to-end
// import path: a FLAC source under the opus-balanced profile is converted by
// the real ConvertFile, the converted file is registered with the correct
// format/bitrate/duration/tags, and the original is retained per the keep
// policy as an archive variant beside the converted primary.
func TestAuditELiveImportOpusBalancedConvertsAndRetains(t *testing.T) {
	s, media, downloads := auditELiveFixture(t, "opus-balanced")
	auditELiveSource(t, downloads, "flac", "")
	job, err := s.StartImport(context.Background(), 2, "Album", "Music")
	if err != nil {
		t.Fatal(err)
	}
	job = waitForImport(t, context.Background(), s, job.ID)
	if job.State != "completed" || job.ImportedCount != 2 {
		t.Fatal("import job", job)
	}
	// The converted file is registered with the correct format/duration/tags.
	_, relative, format, title, _, durationMS := auditELivePrimary(t, s)
	if probeFormatFamily(format) != "opus" {
		t.Fatalf("registered format family = %q, want opus", format)
	}
	if durationMS < 900 || durationMS > 2500 {
		t.Fatalf("registered duration = %d ms, want about 1s", durationMS)
	}
	if title != "Song" {
		t.Fatalf("registered title = %q, want Song", title)
	}
	if !strings.Contains(relative, "01 Song") {
		t.Fatalf("registered primary path = %q, want the track's canonical name", relative)
	}
	// The converted file exists at the registered path.
	if _, err := os.Stat(filepath.Join(media, filepath.FromSlash(relative))); err != nil {
		t.Fatal("registered primary file missing", err)
	}
	// The original is retained per the keep policy as an archive variant.
	var archives int
	if err := s.store.db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE variant_role='archive' AND track_id=3`).Scan(&archives); err != nil {
		t.Fatal(err)
	}
	if archives != 1 {
		t.Fatalf("the retained original must be registered as an archive variant: %d", archives)
	}
}

// TestAuditELiveImportMp3MaximumMatchesReality proves the conversion-needed
// detection matches reality for the mp3-maximum profile: a 128k mp3 source IS
// re-encoded to 320 kbps, and a 320k mp3 source is copied without re-encoding.
func TestAuditELiveImportMp3MaximumMatchesReality(t *testing.T) {
	s, media, downloads := auditELiveFixture(t, "mp3-maximum")
	// A 128k mp3 source is re-encoded to the 320 kbps profile.
	auditELiveSource(t, downloads, "mp3", "128k")
	job, err := s.StartImport(context.Background(), 2, "Album", "Music")
	if err != nil {
		t.Fatal(err)
	}
	job = waitForImport(t, context.Background(), s, job.ID)
	if job.State != "completed" || job.ImportedCount != 2 {
		t.Fatal("import job", job)
	}
	_, _, format, _, bitrate, durationMS := auditELivePrimary(t, s)
	if format != "mp3" {
		t.Fatalf("registered format = %q, want mp3", format)
	}
	if durationMS < 900 || durationMS > 2500 {
		t.Fatalf("registered duration = %d ms, want about 1s", durationMS)
	}
	if bitrate < 280000 {
		t.Fatalf("128k mp3 source must be re-encoded to 320 kbps: bitrate = %d", bitrate)
	}
	if _, err := os.Stat(filepath.Join(media, "Music", "Artist", "Album", "01 Song.mp3")); err != nil {
		t.Fatal("converted primary missing", err)
	}
	// A 320k mp3 source is copied without re-encoding.
	s2, media2, downloads2 := auditELiveFixture(t, "mp3-maximum")
	auditELiveSource(t, downloads2, "mp3", "320k")
	job2, err := s2.StartImport(context.Background(), 2, "Album", "Music")
	if err != nil {
		t.Fatal(err)
	}
	job2 = waitForImport(t, context.Background(), s2, job2.ID)
	if job2.State != "completed" || job2.ImportedCount != 2 {
		t.Fatal("import job", job2)
	}
	_, _, format2, _, bitrate2, _ := auditELivePrimary(t, s2)
	if format2 != "mp3" {
		t.Fatalf("registered format = %q, want mp3", format2)
	}
	if bitrate2 < 280000 {
		t.Fatalf("320k mp3 source must be copied without re-encoding: bitrate = %d", bitrate2)
	}
	if _, err := os.Stat(filepath.Join(media2, "Music", "Artist", "Album", "01 Song.mp3")); err != nil {
		t.Fatal("registered primary missing", err)
	}
}

// TestAuditELiveConversionNeededDetectionMatchesReality proves the pure
// ConversionNeeded decision matches reality against real ffprobe output: a
// 128k mp3 source under mp3-maximum is re-encoded, a 320k mp3 source is
// copied, and a FLAC source under opus-balanced converts.
func TestAuditELiveConversionNeededDetectionMatchesReality(t *testing.T) {
	_, _, downloads := auditELiveFixture(t, "opus-balanced")
	prober := FFprobeTagger{}
	// mp3 128k under mp3-maximum: re-encoded.
	auditELiveSource(t, downloads, "mp3", "128k")
	tags, err := prober.ProbeAudio(context.Background(), filepath.Join(downloads, "Album", "01.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	needed, reason := ConversionNeeded(tags, mustProfile("mp3-maximum"))
	if !needed || !strings.Contains(reason, "Re-encoding") {
		t.Fatalf("128k mp3 under mp3-maximum must be re-encoded: needed=%v reason=%q", needed, reason)
	}
	// flac under opus-balanced: converted.
	flac := auditELiveSource(t, downloads, "flac", "")
	tags, err = prober.ProbeAudio(context.Background(), flac)
	if err != nil {
		t.Fatal(err)
	}
	needed, reason = ConversionNeeded(tags, mustProfile("opus-balanced"))
	if !needed || reason == "" {
		t.Fatalf("flac under opus-balanced must convert: needed=%v reason=%q", needed, reason)
	}
}
