package music

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// insertOptimizeFile registers one file row directly, with a chosen variant
// role and profile, so variant and optimization tests do not depend on the
// scan pipeline. The file itself is written under the media root so the
// prober can find it.
func insertOptimizeFile(t *testing.T, db *sql.DB, media string, id, trackID int64, relative, format string, durationMS int, sizeBytes int64, role, profile string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO music_files(id,artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,duration_ms,tag_json,imported_at,origin,source_name,variant_role,variant_profile)
		VALUES(?,1,2,?,?,?,?,?, ?,?, '{}',1,'existing','fixture',?,?)`,
		id, trackID, "Music", relative, sizeBytes, "sha", format, durationMS, role, profile); err != nil {
		t.Fatal(err)
	}
	absolute := filepath.Join(media, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte("fixture audio"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// optimizeFixture registers one primary FLAC file and one primary MP3 file for
// track 3, plus a variant opus copy of the same track, and returns the
// service, database and media root.
func optimizeFixture(t *testing.T) (*Service, *sql.DB, string) {
	t.Helper()
	s, db, media := editFixture(t)
	s.SetProber(fakeProber{
		"01 - Song.flac": {Format: "flac", DurationMS: 240000, Bitrate: 900, Title: "Song"},
		"01 - Song.mp3":  {Format: "mp3", DurationMS: 240000, Bitrate: 128, Title: "Song"},
		"01 - Song.opus": {Format: "ogg", DurationMS: 240000, Bitrate: 96, Title: "Song"},
		"02 - Other.wav": {Format: "wav", DurationMS: 240000, Bitrate: 1411, Title: "Other"},
		"03 - Third.wma": {Format: "wma", DurationMS: 240000, Bitrate: 128, Title: "Third"},
	})
	insertOptimizeFile(t, db, media, 10, 3, "Music/Artist/Album/01 - Song.flac", "flac", 240000, 30_000_000, "primary", "")
	insertOptimizeFile(t, db, media, 11, 3, "Music/Artist/Album/01 - Song.mp3", "mp3", 240000, 8_000_000, "primary", "")
	insertOptimizeFile(t, db, media, 12, 3, "Music/Artist/Album/01 - Song.opus", "ogg", 240000, 4_000_000, "variant", "opus-balanced")
	insertOptimizeFile(t, db, media, 13, 3, "Music/Artist/Album/02 - Other.wav", "wav", 240000, 40_000_000, "primary", "")
	insertOptimizeFile(t, db, media, 14, 3, "Music/Artist/Album/03 - Third.wma", "wma", 240000, 9_000_000, "primary", "")
	return s, db, media
}

func TestOptimizationEligibilityMatrix(t *testing.T) {
	ctx := context.Background()
	s, db, _ := optimizeFixture(t)
	opus := mustProfile("opus-high")            // 128 kbps lossy
	lossless := mustProfile("lossless-archive") // FLAC lossless
	mp3 := mustProfile("mp3-compatible")        // 192 kbps CBR
	original := mustProfile("original")
	requireLossless := QualityProfile{Codec: "flac", Container: "flac", RequireLossless: true}
	requireLosslessOpus := QualityProfile{Codec: "opus", Container: "opus", BitrateKbps: 128, RequireLossless: true}

	cases := []struct {
		name         string
		fileID       int64
		profile      QualityProfile
		wantEligible bool
		wantReason   string
	}{
		{"lossless flac to opus converts", 10, opus, true, ""},
		{"lossy mp3 to opus converts", 11, opus, true, ""},
		{"wav to opus converts", 13, opus, true, ""},
		{"wma to opus converts", 14, opus, true, ""},
		{"flac to flac is already satisfied", 10, lossless, false, "already FLAC"},
		{"wav to flac re-packages", 13, lossless, true, ""},
		{"mp3 to flac without requirement is honest", 11, lossless, true, "does not restore lost detail"},
		{"mp3 128 to mp3 192 re-encodes", 11, mp3, true, "Re-encoding"},
		{"original never converts", 10, original, false, "profile does not convert"},
		{"lossy source fails the lossless requirement", 11, requireLossless, false, "requires lossless source"},
		{"flac source satisfies the lossless requirement", 10, requireLosslessOpus, true, ""},
		{"variant copy is never re-converted", 12, opus, false, "variant copy"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var file MusicFile
			if err := db.QueryRow(`SELECT f.id,f.relative_path,f.format,f.duration_ms,f.size_bytes,f.variant_role FROM music_files f WHERE f.id=?`, tc.fileID).
				Scan(&file.ID, &file.RelativePath, &file.Format, &file.DurationMS, &file.SizeBytes, &file.VariantRole); err != nil {
				t.Fatal(err)
			}
			item := s.optimizeItem(ctx, file, tc.profile)
			if item.Eligible != tc.wantEligible {
				t.Fatalf("eligible = %v, want %v (reason %q)", item.Eligible, tc.wantEligible, item.NotEligibleReason)
			}
			if !tc.wantEligible && !strings.Contains(item.NotEligibleReason, tc.wantReason) {
				t.Fatalf("reason %q does not contain %q", item.NotEligibleReason, tc.wantReason)
			}
			if tc.wantEligible && item.NotEligibleReason != "" {
				t.Fatalf("eligible item must not carry a reason, got %q", item.NotEligibleReason)
			}
			if item.FileID != tc.fileID || item.RelativePath != file.RelativePath || item.SizeBytes != file.SizeBytes {
				t.Fatalf("item identity mismatch: %+v", item)
			}
		})
	}
}

func TestOptimizationProbeFailureIsHonest(t *testing.T) {
	ctx := context.Background()
	s, db, media := optimizeFixture(t)
	insertOptimizeFile(t, db, media, 15, 3, "Music/Artist/Album/04 - Gone.flac", "flac", 240000, 1_000_000, "primary", "")
	// The row exists but no prober entry answers it, so the probe fails.
	profile := mustProfile("opus-high")
	var file MusicFile
	if err := db.QueryRow(`SELECT f.id,f.relative_path,f.format,f.duration_ms,f.size_bytes,f.variant_role FROM music_files f WHERE f.id=15`).
		Scan(&file.ID, &file.RelativePath, &file.Format, &file.DurationMS, &file.SizeBytes, &file.VariantRole); err != nil {
		t.Fatal(err)
	}
	item := s.optimizeItem(ctx, file, profile)
	if item.Eligible {
		t.Fatal("unprobeable file must not be eligible")
	}
	if item.NotEligibleReason != "source could not be inspected" {
		t.Fatalf("reason = %q", item.NotEligibleReason)
	}
	// A missing file on disk must fail the same way, not fail the preview.
	if err := os.Remove(filepath.Join(media, filepath.FromSlash(file.RelativePath))); err != nil {
		t.Fatal(err)
	}
	if item := s.optimizeItem(ctx, file, profile); item.Eligible || item.NotEligibleReason != "source could not be inspected" {
		t.Fatalf("missing file: %+v", item)
	}
}

func TestPreviewOptimizationMath(t *testing.T) {
	ctx := context.Background()
	s, _, media := optimizeFixture(t)
	preview, err := s.PreviewOptimization(ctx, "opus-high", nil)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Profile != "opus-high" || preview.TotalCount != 5 || preview.EligibleCount != 4 {
		t.Fatalf("counts: %+v", preview)
	}
	// opus-high is 128 kbps; 128 * 240000 / 8 = 3,840,000 bytes of audio, plus
	// the 2% container overhead factor.
	want := int64(3_916_800)
	for _, item := range preview.Items {
		if !item.Eligible {
			if item.EstimatedBytes != item.SizeBytes {
				t.Fatalf("ineligible item must keep its size: %+v", item)
			}
			continue
		}
		if item.EstimatedBytes != want {
			t.Fatalf("file %d estimate = %d, want %d", item.FileID, item.EstimatedBytes, want)
		}
	}
	// The lossless-archive profile claims no savings for lossless targets.
	preview, err = s.PreviewOptimization(ctx, "lossless-archive", nil)
	if err != nil {
		t.Fatal(err)
	}
	// The flac file is already satisfied and the variant copy is never
	// re-converted; the mp3, wav and wma files are eligible with conservative
	// source-size estimates.
	if preview.EligibleCount != 3 {
		t.Fatalf("eligible count = %d, want 3: %+v", preview.EligibleCount, preview)
	}
	if preview.EstimatedSavings != 0 {
		t.Fatalf("lossless target must claim no savings, got %d", preview.EstimatedSavings)
	}
	// The original profile converts nothing.
	preview, err = s.PreviewOptimization(ctx, "original", nil)
	if err != nil {
		t.Fatal(err)
	}
	if preview.EligibleCount != 0 || preview.EstimatedSavings != 0 || preview.TotalCount != 5 {
		t.Fatalf("original preview: %+v", preview)
	}
	_ = media
}

func TestPreviewOptimizationBoundedIDs(t *testing.T) {
	ctx := context.Background()
	s, _, _ := optimizeFixture(t)
	tooMany := make([]int64, maxOptimizeFiles+1)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	if _, err := s.PreviewOptimization(ctx, "opus-high", tooMany); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("over-bound selection err = %v, want ErrInvalidInput", err)
	}
	if _, err := s.ApplyOptimization(ctx, "opus-high", tooMany); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("over-bound apply err = %v, want ErrInvalidInput", err)
	}
	if _, err := s.PreviewOptimization(ctx, "no-such-profile", nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unknown profile err = %v, want ErrInvalidInput", err)
	}
}

func TestOptimizationDryRunWritesNothing(t *testing.T) {
	ctx := context.Background()
	s, db, media := optimizeFixture(t)
	before := snapshotTree(t, media)
	preview, err := s.PreviewOptimization(ctx, "opus-high", nil)
	if err != nil {
		t.Fatal(err)
	}
	if preview.EligibleCount == 0 {
		t.Fatal("fixture must have eligible files")
	}
	if after := snapshotTree(t, media); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatal("dry run changed the media tree")
	}
	var jobs int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_conversion_jobs`).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 0 {
		t.Fatalf("dry run enqueued %d conversion jobs", jobs)
	}
}

func TestApplyOptimizationEnqueuesOnlyEligible(t *testing.T) {
	ctx := context.Background()
	s, db, _ := optimizeFixture(t)
	enqueued, err := s.ApplyOptimization(ctx, "opus-high", nil)
	if err != nil {
		t.Fatal(err)
	}
	if enqueued != 4 {
		t.Fatalf("enqueued = %d, want 4 (primary files only)", enqueued)
	}
	rows, err := db.Query(`SELECT kind,file_id,profile,state,priority FROM music_conversion_jobs ORDER BY file_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var kind, profile, state string
		var fileID, priority int64
		if err := rows.Scan(&kind, &fileID, &profile, &state, &priority); err != nil {
			t.Fatal(err)
		}
		if kind != "library" || profile != "opus-high" || state != "queued" || priority != 8 {
			t.Fatalf("job row kind=%s profile=%s state=%s priority=%d", kind, profile, state, priority)
		}
		ids = append(ids, fileID)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(ids) != fmt.Sprint([]int64{10, 11, 13, 14}) {
		t.Fatalf("enqueued file ids = %v, want the four primary files", ids)
	}
}

func TestListTrackVariantsOrdersPrimaryFirst(t *testing.T) {
	ctx := context.Background()
	s, _, _ := optimizeFixture(t)
	files, err := s.store.ListTrackVariants(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 5 {
		t.Fatalf("variant count = %d, want 5", len(files))
	}
	ids := []int64{}
	for _, file := range files {
		ids = append(ids, file.ID)
	}
	if fmt.Sprint(ids) != fmt.Sprint([]int64{10, 11, 13, 14, 12}) {
		t.Fatalf("order = %v, want primary files first then variants by id", ids)
	}
	for _, file := range files {
		if file.VariantRole == "" {
			t.Fatalf("file %d did not surface variant_role", file.ID)
		}
	}
	if files[4].VariantRole != "variant" || files[4].VariantProfile != "opus-balanced" {
		t.Fatalf("variant fields: %+v", files[4])
	}
	files, err = s.store.ListTrackVariants(ctx, 999999)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("unknown track must return an empty list, got %d files", len(files))
	}
}

func TestPreferredFileSelectionIgnoresVariantRows(t *testing.T) {
	ctx := context.Background()
	s, db, media := optimizeFixture(t)
	// The variant row has the lowest id (12) but must never hijack the
	// preferred-file selection.
	insertOptimizeFile(t, db, media, 9, 3, "Music/Artist/Album/00 - Song.opus", "ogg", 240000, 3_000_000, "variant", "opus-economy")
	insertOptimizeFile(t, db, media, 20, 3, "Music/Artist/Album/01 - Song.archive.flac", "flac", 240000, 30_000_000, "archive", "lossless-archive")

	tracks, err := s.store.Tracks(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].FileID == nil || *tracks[0].FileID != 10 {
		t.Fatalf("Tracks preferred file = %v, want 10", tracks[0].FileID)
	}
	page, err := s.store.ListTracks(ctx, "", 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].FileID == nil || *page.Items[0].FileID != 10 {
		t.Fatalf("ListTracks preferred file = %v, want 10", page.Items[0].FileID)
	}
	artistPage, err := s.store.ArtistTracks(ctx, 1, 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(artistPage.Items) != 1 || artistPage.Items[0].FileID == nil || *artistPage.Items[0].FileID != 10 {
		t.Fatalf("ArtistTracks preferred file = %v, want 10", artistPage.Items[0].FileID)
	}
}

// snapshotTree returns a sorted list of every path below root, so tests can
// prove that a dry run touched nothing.
func snapshotTree(t *testing.T, root string) []string {
	t.Helper()
	out := []string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
