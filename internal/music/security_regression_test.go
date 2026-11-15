package music

// Agent E security regression tests (FIX-OWNERSHIP: new security_regression_
// file). These tests exercise the security fences of the music transcoding
// stack from the outside: a tampered or adversarial input must never move a
// filesystem operation outside its fenced root, and user-facing job errors
// must never carry absolute server paths.
//
// Covered fences:
//   - conversionVariantPath/safeVariantLabel: a custom profile *label* (the
//     only profile-provided string that reaches a filename) cannot introduce
//     path separators or traversal segments into the published variant path.
//   - music_stream_cache.relative_path: a tampered cache row can never point
//     the open or the delete outside the cache dir.
//   - cacheProfileKey: the profile key that becomes a cache filename fails
//     closed on separators, traversal, control characters and oversize.
//   - conversion job error hygiene: raw OS errors (which embed absolute
//     paths) are scrubbed before they are stored on the queue row.
//   - stage-directory cleanup: a failed conversion never leaves staged bytes.
//   - retention delete: a variant whose published bytes no longer verify can
//     never authorize deleting the original.

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// insertHostileCustomProfile inserts a custom quality profile row directly,
// bypassing SaveCustomProfile validation. This models a tampered database row
// or a legacy row written before validation existed: the fence must hold at
// path-derivation time, not only at save time.
func insertHostileCustomProfile(t *testing.T, db *sql.DB, name, codec string, bitrate int) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO music_quality_profiles
		(name, key, codec, container, bitrate_kbps, vbr, compression_level, require_lossless, keep_original, custom, created_at, updated_at)
		VALUES (?, '', ?, '', ?, 1, 0, 0, 0, 1, 1, 1)`, name, codec, bitrate); err != nil {
		t.Fatal(err)
	}
}

// TestSecurityRegressionHostileCustomProfileLabelStaysContained proves the
// full conversion pipeline with adversarial custom profile names: the label
// is the only profile-provided string that reaches a filename, so a name
// carrying separators, traversal segments or control characters must produce
// a variant file inside the source file's own directory and never anywhere
// else on the filesystem.
func TestSecurityRegressionHostileCustomProfileLabelStaysContained(t *testing.T) {
	cases := []struct {
		name         string
		label        string
		wantFilename string // sanitized filename inside the album directory
	}{
		{"parent traversal", "flac/../../evil", "01 Song [flac-..-..-evil].opus"},
		{"backslash separator", `..\..\evil`, "01 Song [..-..-evil].opus"},
		{"dot-only label", "..", "01 Song [..].opus"},
		{"control characters", "evil\x01name\x7f", "01 Song [evil-name-].opus"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			helperFFmpeg(t)
			s, db, media, downloads, fileID := conversionFixture(t)
			s.SetProber(conversionProber())
			ctx := context.Background()

			insertHostileCustomProfile(t, db, tc.label, "opus", 128)

			// The queue accepts the label (ResolveProfileKey matches custom
			// profiles by name) and records the job.
			id, err := s.EnqueueConversion(ctx, "library", fileID, tc.label, 5)
			if err != nil {
				t.Fatalf("enqueue with hostile label %q: %v", tc.label, err)
			}
			if job := runConversionUntil(t, s, id, "completed"); job.State != "completed" {
				t.Fatalf("conversion did not complete: %+v", job)
			}

			// The catalog variant path must be contained and sanitized.
			variantID, relative, role, profile := variantRow(t, db, tc.label)
			if role != "variant" || profile != tc.label {
				t.Fatalf("variant row role=%q profile=%q", role, profile)
			}
			if clean, err := cleanMediaRelative(relative); err != nil || clean != relative {
				t.Fatalf("published variant relative path is not a contained media path: %q (err %v)", relative, err)
			}
			if !strings.HasPrefix(relative, "Music/Artist/Album/") {
				t.Fatalf("variant escaped the album directory: %q", relative)
			}
			// No individual path segment may be a traversal segment (the
			// literal characters ".." inside a filename segment are fine —
			// the sanitizer keeps them there).
			for _, segment := range strings.Split(relative, "/") {
				if segment == ".." || segment == "." || segment == "" {
					t.Fatalf("variant path has a traversal segment: %q", relative)
				}
			}
			if filepath.Base(relative) != tc.wantFilename {
				t.Fatalf("variant filename %q, want sanitized %q", filepath.Base(relative), tc.wantFilename)
			}
			if _, err := os.Lstat(filepath.Join(media, filepath.FromSlash(relative))); err != nil {
				t.Fatalf("variant file missing inside the album directory: %v", err)
			}

			// Nothing may exist outside the media root's album directory as a
			// consequence of the hostile label: check the traversal targets
			// the label aimed at plus the stage directory leftovers.
			for _, escaped := range []string{
				filepath.Join(media, "evil.opus"),
				filepath.Join(media, "Music", "evil.opus"),
				filepath.Join(media, "Music", "Artist", "evil.opus"),
				filepath.Join(downloads, "evil.opus"),
			} {
				if _, err := os.Lstat(escaped); !os.IsNotExist(err) {
					t.Fatalf("hostile label %q produced a file at %s (err %v)", tc.label, escaped, err)
				}
			}
			_ = variantID
		})
	}
}

// TestSecurityRegressionTamperedCacheRowFailsClosed proves the stream-cache
// containment fence: a tampered music_stream_cache.relative_path that points
// outside the cache dir can neither be opened for streaming nor deleted, and
// the targeted file survives untouched.
func TestSecurityRegressionTamperedCacheRowFailsClosed(t *testing.T) {
	service, db, mediaDir := cacheTestService(t)
	ctx := context.Background()
	secret := []byte("outside secret content")
	if err := os.WriteFile(filepath.Join(mediaDir, "secret.txt"), secret, 0o600); err != nil {
		t.Fatal(err)
	}
	sha := sha256Hex(secret)
	fileID := registerCacheFile(t, db, "Music/song.flac", sha)
	if _, err := db.Exec(`INSERT INTO music_stream_cache
		(file_id, profile_key, relative_path, size_bytes, source_sha256, codec, bitrate_kbps, created_at, last_used_at)
		VALUES (?, 'opus-128', '../../secret.txt', 5, ?, 'opus', 128, 1, 1)`, fileID, sha); err != nil {
		t.Fatal(err)
	}

	// A hit must never serve the outside file: the tampered path is
	// rejected, the stale row is dropped, and with no encode func the caller
	// gets a cache miss (never the secret bytes).
	file, info, key, err := service.OpenCachedStream(ctx, fileID, "opus", 128, sha, nil)
	if !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("tampered row served something: file=%v info=%v key=%q err=%v", file, info, key, err)
	}
	if file != nil {
		_ = file.Close()
	}
	got, err := os.ReadFile(filepath.Join(mediaDir, "secret.txt"))
	if err != nil || string(got) != string(secret) {
		t.Fatalf("secret file was read or modified: %q (err %v)", got, err)
	}

	// The cached-hit lookup must have invalidated the tampered row rather
	// than keeping a poison entry around.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_stream_cache WHERE file_id=?`, fileID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("tampered cache row survived the open attempt: %d rows", count)
	}
}

// TestSecurityRegressionCacheDeleteNeverTouchesForeignPaths proves
// deleteCacheEntry refuses to unlink a stored path that is not the
// deterministic cache name for its key, even when the row was tampered with.
func TestSecurityRegressionCacheDeleteNeverTouchesForeignPaths(t *testing.T) {
	service, db, mediaDir := cacheTestService(t)
	ctx := context.Background()
	secret := []byte("outside secret content")
	if err := os.WriteFile(filepath.Join(mediaDir, "secret.txt"), secret, 0o600); err != nil {
		t.Fatal(err)
	}
	fileID := registerCacheFile(t, db, "Music/song.flac", sha256Hex(secret))

	// The delete is row-scoped (file_id+profile_key) and path-fenced: the
	// tampered path must not be unlinked, the row must still be forgotten.
	if err := service.deleteCacheEntry(ctx, fileID, "opus-128", "../../secret.txt"); err != nil {
		t.Fatalf("deleteCacheEntry should forget the row best-effort: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mediaDir, "secret.txt")); err != nil {
		t.Fatalf("deleteCacheEntry removed a file outside the cache fence: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_stream_cache WHERE file_id=?`, fileID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("tampered row not forgotten: %d rows (err %v)", count, err)
	}
}

// TestSecurityRegressionCacheProfileKeyFailsClosed proves the cache filename
// key derivation rejects every input class that could escape the cache
// directory or smuggle junk into a filename.
func TestSecurityRegressionCacheProfileKeyFailsClosed(t *testing.T) {
	hostile := []struct {
		codec   string
		bitrate int
	}{
		{"../../evil", 128},
		{`..\evil`, 128},
		{"..", 128},
		{"opus\n128", 128},
		{"opus\x00", 128},
		{"opus/128", 128},
		{"", 128},
		{"   ", 128},
		{"opus", -1},
		{"opus", 4097},
		{"opus/../../../../../../../../evil", 128},
		{strings.Repeat("o", 65), 128},
	}
	for _, tc := range hostile {
		if key, ok := cacheProfileKey(tc.codec, tc.bitrate); ok {
			t.Fatalf("cacheProfileKey(%q, %d) accepted key %q", tc.codec, tc.bitrate, key)
		}
	}
	for _, ok := range []struct {
		codec   string
		bitrate int
	}{
		{"opus", 128},
		{"flac", 0},
		{"mp3", 320},
		{"opus", 4096},
	} {
		if key, valid := cacheProfileKey(ok.codec, ok.bitrate); !valid {
			t.Fatalf("cacheProfileKey(%q, %d) rejected a legitimate key", ok.codec, ok.bitrate)
		} else {
			if name, valid := cleanCacheRelative("42/" + key); !valid || name != "42/"+key {
				t.Fatalf("legitimate key %q is not a contained cache filename", key)
			}
		}
	}
}

// TestSecurityRegressionJobErrorHidesAbsolutePaths proves a raw OS error
// (mkdir/rename/open errors embed absolute paths) never lands verbatim on
// the conversion job row the UI displays. The stage mkdir is forced to fail
// by pointing the downloads root at a regular file.
func TestSecurityRegressionJobErrorHidesAbsolutePaths(t *testing.T) {
	helperFFmpeg(t)
	s, _, _, _, fileID := conversionFixture(t)
	s.SetProber(conversionProber())
	// A regular file where the downloads root should be: every MkdirAll
	// below it fails with an OS error that names the absolute path.
	fakeDownloads := filepath.Join(t.TempDir(), "downloads-file")
	if err := os.WriteFile(fakeDownloads, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.SetDownloadsDir(fakeDownloads)
	ctx := context.Background()

	id, err := s.EnqueueConversion(ctx, "library", fileID, "opus-high", 5)
	if err != nil {
		t.Fatal(err)
	}
	job, owner, claimed, err := s.claimConversion(ctx)
	if err != nil || !claimed {
		t.Fatalf("claim failed: claimed=%v err=%v", claimed, err)
	}
	if err := s.runConversion(ctx, job, owner); err != nil {
		t.Fatalf("runConversion returned: %v", err)
	}
	stored, err := s.loadConversion(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Error == "" {
		t.Fatal("the failed attempt stored no error at all")
	}
	// The stored message must not carry the fixture's absolute layout: no
	// drive-letter path and no raw filesystem error text.
	if matched := regexp.MustCompile(`[A-Za-z]:[\\/]`).FindString(stored.Error); matched != "" {
		t.Fatalf("job error leaks an absolute path: %q", stored.Error)
	}
	if strings.Contains(stored.Error, fakeDownloads) {
		t.Fatalf("job error leaks the downloads root: %q", stored.Error)
	}
	// The job must be back in the queue (attempt 1 of 3), not failed or
	// wedged in processing.
	if stored.State != "queued" {
		t.Fatalf("failed attempt left the job in state %q, want queued", stored.State)
	}
	if stored.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", stored.Attempts)
	}
}

// TestSecurityRegressionFailedConversionLeavesNoStageBytes proves the stage
// directory is cleaned after a failed conversion run, so no unbounded temp
// growth and no partially converted files survive.
func TestSecurityRegressionFailedConversionLeavesNoStageBytes(t *testing.T) {
	helperFFmpeg(t)
	s, db, _, downloads, fileID := conversionFixture(t)
	s.SetProber(conversionProber())
	// No ffmpeg anywhere: ConvertFile fails with the typed missing-ffmpeg
	// error after the stage directory was already created.
	brokenFFmpeg(t)
	ctx := context.Background()
	id, err := s.EnqueueConversion(ctx, "library", fileID, "opus-high", 5)
	if err != nil {
		t.Fatal(err)
	}
	if job := runConversionUntil(t, s, id, "queued"); job.State != "queued" {
		t.Fatalf("job state %q", job.State)
	}
	stage := conversionStageDir(downloads, id)
	if _, err := os.Lstat(stage); !os.IsNotExist(err) {
		t.Fatalf("stage directory survived a failed conversion: %v", err)
	}
	_ = db
}

// TestSecurityRegressionRetentionKeptWhenVariantDigestMismatch proves the
// retention fence on the variant side: when the published variant's bytes no
// longer verify against the converted digest, the original is kept and stays
// the primary. (The original-side swap fence is covered by
// TestAuditEConversionRetentionSwapBetweenFenceAndDelete.)
func TestSecurityRegressionRetentionKeptWhenVariantDigestMismatch(t *testing.T) {
	helperFFmpeg(t)
	s, db, media, _, fileID := conversionFixture(t)
	s.SetProber(conversionProber())
	ctx := context.Background()
	// Complete a conversion while retention is keep so both files survive,
	// then arm the delete policy against the recorded evidence.
	if _, err := db.Exec(`UPDATE music_preferences SET original_retention='keep' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	id, err := s.EnqueueConversion(ctx, "library", fileID, "opus-high", 5)
	if err != nil {
		t.Fatal(err)
	}
	if job := runConversionUntil(t, s, id, "completed"); job.State != "completed" {
		t.Fatalf("conversion did not complete: %+v", job)
	}
	variantID, variantRel, _, _ := variantRow(t, db, "opus-high")
	variantDigest := sha256Hex(conversionSourceBytes)
	src := auditEConversionSource(t, s, fileID)
	// Arm the delete policy only now: the completed run already applied the
	// keep policy, so both files survived the conversion itself.
	if _, err := db.Exec(`UPDATE music_preferences SET original_retention='delete' WHERE id=1`); err != nil {
		t.Fatal(err)
	}

	// Corrupt the published variant after the fact: the digest fence on the
	// variant must refuse the delete.
	if err := os.WriteFile(filepath.Join(media, filepath.FromSlash(variantRel)), []byte("tampered variant bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	retentionErr := s.applyConversionRetention(ctx, src, variantRel, variantDigest, mustProfile("opus-high"))
	if retentionErr == nil {
		t.Fatal("retention accepted a variant whose bytes no longer verify")
	}
	if !strings.Contains(retentionErr.Error(), "original file was kept") {
		t.Fatalf("unexpected retention error: %v", retentionErr)
	}
	// The original row and bytes must still be there, still primary.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE id=?`, fileID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("original row was deleted by a mismatched variant: %d rows (err %v)", count, err)
	}
	if got, err := os.ReadFile(filepath.Join(media, "Music", "Artist", "Album", "01 Song.wma")); err != nil || string(got) != string(conversionSourceBytes) {
		t.Fatalf("original bytes damaged: %q (err %v)", got, err)
	}
	var role string
	if err := db.QueryRow(`SELECT variant_role FROM music_files WHERE id=?`, variantID).Scan(&role); err != nil || role != "variant" {
		t.Fatalf("variant row role=%q (err %v), want variant", role, err)
	}
}

// TestSecurityRegressionEnqueueRejectsUnresolvableProfileKeys proves the
// queue never stores an unusable profile target: a hostile or unknown key is
// rejected before any row is written, including keys that look like paths.
func TestSecurityRegressionEnqueueRejectsUnresolvableProfileKeys(t *testing.T) {
	s, db, _, _, fileID := conversionFixture(t)
	ctx := context.Background()
	for _, key := range []string{"", "   ", "../opus-high", "opus-high/", "opus-high/../../evil", "\x01", "nonexistent-profile"} {
		if _, err := s.EnqueueConversion(ctx, "library", fileID, key, 5); err == nil {
			t.Fatalf("EnqueueConversion accepted profile key %q", key)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_conversion_jobs`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected keys still wrote job rows: %d (err %v)", count, err)
	}
}

// TestSecurityRegressionConvertFileRejectsOverwrite proves the conversion
// engine never overwrites an existing output: the caller's publish step owns
// replacement, and a staged output that already exists is a hard refusal.
func TestSecurityRegressionConvertFileRejectsOverwrite(t *testing.T) {
	helperFFmpeg(t)
	s, _, media, _, _ := conversionFixture(t)
	ctx := context.Background()
	existing := filepath.Join(media, "staged-output.opus")
	if err := os.WriteFile(existing, []byte("already here"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConvertFile(ctx, ConvertRequest{
		SourcePath: filepath.Join(media, "Music", "Artist", "Album", "01 Song.wma"),
		OutputPath: existing,
		Profile:    mustProfile("opus-high"),
		Timeout:    time.Second,
	}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("ConvertFile overwrote an existing output: %v", err)
	}
	if got, err := os.ReadFile(existing); err != nil || string(got) != "already here" {
		t.Fatalf("existing output was overwritten: %q (err %v)", got, err)
	}
}
