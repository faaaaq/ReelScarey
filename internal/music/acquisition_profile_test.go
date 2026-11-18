package music

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

// formatCapturingSource records the yt-dlp format the acquisition worker
// actually passes to the downloader, so the profile-to-download-format
// mapping is observable without a real yt-dlp.
type formatCapturingSource struct {
	formats *[]string
}

func (f formatCapturingSource) SearchAudio(context.Context, string, int) ([]AudioCandidate, error) {
	return []AudioCandidate{{ID: "abcdefghijk", Title: "Song", DurationSeconds: 200}}, nil
}

func (f formatCapturingSource) DownloadAudio(_ context.Context, _ string, directory, format string) (AudioFile, error) {
	*f.formats = append(*f.formats, format)
	path := filepath.Join(directory, "track.mp3")
	if err := os.WriteFile(path, []byte("audio"), 0o600); err != nil {
		return AudioFile{}, err
	}
	return AudioFile{Path: path, Format: "mp3"}, nil
}

func (f formatCapturingSource) InspectAudio(context.Context, string) (AudioInspection, error) {
	return AudioInspection{}, errors.New("not used")
}

func TestResolveAcquisitionProfile(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := NewService(db, t.TempDir())

	if _, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "Car Opus", Codec: "opus", BitrateKbps: 96}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		format   string
		wantKey  string
		wantName string
	}{
		{"", "", "Original"},
		{"source", "original", "Original"},
		{"mp3", "mp3-maximum", "MP3 Maximum"},
		{"flac", "lossless-archive", "Lossless Archive"},
		{"m4a", "aac-high", "AAC High"},
		{"opus", "opus-maximum", "Opus Maximum"},
		{"opus-balanced", "opus-balanced", "Opus Balanced"},
		{"MP3-Compatible", "mp3-compatible", "MP3 Compatible"},
		{"Car Opus", "Car Opus", "Car Opus"},
	}
	for _, tc := range cases {
		profile, key, err := service.resolveAcquisitionProfile(ctx, tc.format)
		if err != nil {
			t.Fatalf("resolveAcquisitionProfile(%q): %v", tc.format, err)
		}
		if key != tc.wantKey || profile.Name != tc.wantName {
			t.Fatalf("resolveAcquisitionProfile(%q) = key %q name %q, want key %q name %q", tc.format, key, profile.Name, tc.wantKey, tc.wantName)
		}
	}
	for _, invalid := range []string{"wma", "aac-unknown", "source flac"} {
		if _, _, err := service.resolveAcquisitionProfile(ctx, invalid); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("resolveAcquisitionProfile(%q) = %v, want ErrInvalidInput", invalid, err)
		}
	}
	// The legacy acceptance helper keeps accepting the yt-dlp values for
	// backward compatibility.
	for _, legacy := range []string{"", "source", "mp3", "flac", "m4a", "opus"} {
		if !validAcquisitionFormat(legacy) {
			t.Fatalf("validAcquisitionFormat(%q) rejected a legacy value", legacy)
		}
	}
}

func TestAcquisitionDownloadFormat(t *testing.T) {
	cases := map[string]string{
		"original": "source",
		"flac":     "source",
		"mp3":      "source",
		"aac":      "source",
		"opus":     "source",
	}
	for codec, want := range cases {
		if got := acquisitionDownloadFormat(QualityProfile{Codec: codec}); got != want {
			t.Fatalf("acquisitionDownloadFormat(%q) = %q, want %q", codec, got, want)
		}
	}
}

func TestStartAcquisitionStoresResolvedProfileKey(t *testing.T) {
	ctx := context.Background()
	downloads := t.TempDir()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := NewService(db, t.TempDir())
	service.SetDownloadsDir(downloads)
	formats := []string{}
	service.SetAudioSource(formatCapturingSource{formats: &formats})

	input := []AcquisitionTrackInput{{
		Title: "Song", Artist: "Artist", Album: "Album", AlbumArtist: "Artist",
		TrackNumber: 1, DiscNumber: 1, DurationMS: 200000, SearchQuery: "Artist Song",
	}}

	// A builtin profile key is stored verbatim and the downloader receives the
	// codec family yt-dlp understands.
	job, err := service.StartAcquisition(ctx, "", "top_track", "Song", "Music", "opus-balanced", input)
	if err != nil {
		t.Fatal(err)
	}
	if job.Format != "opus-balanced" {
		t.Fatalf("job.Format = %q, want the resolved profile key", job.Format)
	}
	if worked, err := service.ProcessAcquisition(ctx); err != nil || !worked {
		t.Fatal("process", worked, err)
	}
	if len(formats) != 1 || formats[0] != "source" {
		t.Fatalf("downloader formats = %v, want original source", formats)
	}

	// A legacy value maps onto its builtin equivalent but still downloads the
	// same yt-dlp format as before.
	job, err = service.StartAcquisition(ctx, "", "top_track", "Other", "Music", "m4a", input)
	if err != nil {
		t.Fatal(err)
	}
	if job.Format != "aac-high" {
		t.Fatalf("legacy m4a job.Format = %q, want aac-high", job.Format)
	}
	if worked, err := service.ProcessAcquisition(ctx); err != nil || !worked {
		t.Fatal("process", worked, err)
	}
	if len(formats) != 2 || formats[1] != "source" {
		t.Fatalf("downloader formats = %v, want original source for the legacy profile", formats)
	}

	// An empty format stays open so the import step can apply the per-album
	// override or the global download preference.
	job, err = service.StartAcquisition(ctx, "", "top_track", "Third", "Music", "", input)
	if err != nil {
		t.Fatal(err)
	}
	if job.Format != "" {
		t.Fatalf("empty format job.Format = %q, want empty", job.Format)
	}
	if worked, err := service.ProcessAcquisition(ctx); err != nil || !worked {
		t.Fatal("process", worked, err)
	}
	if len(formats) != 3 || formats[2] != "source" {
		t.Fatalf("downloader formats = %v, want [.. source]", formats)
	}

	// An unknown format is refused exactly as before.
	if _, err := service.StartAcquisition(ctx, "", "top_track", "Fourth", "Music", "wma", input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unknown format = %v, want ErrInvalidInput", err)
	}
}

func TestStartAlbumAcquisitionAcceptsProfileKeys(t *testing.T) {
	ctx := context.Background()
	downloads := t.TempDir()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := NewService(db, t.TempDir())
	service.SetDownloadsDir(downloads)
	service.SetAudioSource(formatCapturingSource{formats: &[]string{}})

	// A local catalog album with one track and no registered file.
	if _, err := db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES(1,?, 'Artist',1,'missing',1,1)`, testMBID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(2,1,?, 'Album',1,'missing',1,1)`, testRGID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_tracks(id,album_id,musicbrainz_id,disc,number,title,created_at,updated_at) VALUES(3,2,?,1,1,'Song',1,1)`, testTrackID); err != nil {
		t.Fatal(err)
	}
	job, err := service.StartAlbumAcquisition(ctx, 2, "Music", "mp3-compatible")
	if err != nil {
		t.Fatal(err)
	}
	if job.Format != "mp3-compatible" {
		t.Fatalf("album acquisition job.Format = %q, want the profile key", job.Format)
	}
	if _, err := service.StartAlbumAcquisition(ctx, 2, "Music", "wma"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unknown album profile = %v, want ErrInvalidInput", err)
	}
}

func TestImportProfilePlanResolution(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := NewService(db, t.TempDir())

	// No preferences change: the default global profile is original, so the
	// plan is nil even with a legacy per-download key pointing at a codec.
	if plan := service.importProfilePlan(ctx, 1, 1); plan != nil {
		t.Fatalf("default plan = %+v, want nil", plan)
	}

	prefs, err := service.GetPreferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	prefs.DownloadProfile = "opus-high"
	if _, err := service.SavePreferences(ctx, prefs); err != nil {
		t.Fatal(err)
	}
	plan := service.importProfilePlan(ctx, 1, 1)
	if plan == nil || plan.Profile.Key != "opus-high" || plan.Retention != "keep" || plan.Fallback != "best" {
		t.Fatalf("global profile plan = %+v", plan)
	}

	// A per-download key recorded on an acquisition job wins over the global
	// preference; legacy values are mapped.
	if _, err := db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES(1,?, 'Artist',1,'missing',1,1)`, testMBID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(2,1,?, 'Album',1,'missing',1,1)`, testRGID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_acquisition_jobs(id,source_kind,root,format,state,track_count,created_at,updated_at) VALUES(7,'link','Music','mp3','queued',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_acquisition_tracks(id,job_id,position,title,artist,album,state,created_at,updated_at) VALUES(8,7,1,'Song','Artist','Album','queued',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_import_jobs(id,album_id,root,source_path,state,file_count,created_at,updated_at) VALUES(9,2,'Music','x','running',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE music_acquisition_tracks SET import_job_id=9 WHERE id=8`); err != nil {
		t.Fatal(err)
	}
	plan = service.importProfilePlan(ctx, 9, 2)
	if plan == nil || plan.Profile.Key != "mp3-maximum" {
		t.Fatalf("per-download plan = %+v, want mp3-maximum", plan)
	}

	// An explicit original choice pins the original even against a global
	// lossy profile.
	if _, err := db.Exec(`UPDATE music_acquisition_jobs SET format='original' WHERE id=7`); err != nil {
		t.Fatal(err)
	}
	if plan := service.importProfilePlan(ctx, 9, 2); plan != nil {
		t.Fatalf("explicit original plan = %+v, want nil", plan)
	}

	// Transcoding disabled imports unchanged regardless of the profile.
	prefs, err = service.GetPreferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	prefs.TranscodingEnabled = false
	if _, err := service.SavePreferences(ctx, prefs); err != nil {
		t.Fatal(err)
	}
	if plan := service.importProfilePlan(ctx, 9, 1); plan != nil {
		t.Fatalf("transcoding disabled plan = %+v, want nil", plan)
	}
}

func TestImportProfilePlanAlbumOverride(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := NewService(db, t.TempDir())
	if _, err := db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES(1,?, 'Artist',1,'missing',1,1)`, testMBID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(2,1,?, 'Album',1,'missing',1,1)`, testRGID); err != nil {
		t.Fatal(err)
	}
	prefs, err := service.GetPreferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	prefs.DownloadProfile = "mp3-compatible"
	if _, err := service.SavePreferences(ctx, prefs); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE music_albums SET download_profile='opus-economy' WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	plan := service.importProfilePlan(ctx, 1, 2)
	if plan == nil || plan.Profile.Key != "opus-economy" {
		t.Fatalf("album override plan = %+v, want opus-economy", plan)
	}
	// Another album follows the global preference.
	plan = service.importProfilePlan(ctx, 1, 9999)
	if plan == nil || plan.Profile.Key != "mp3-compatible" {
		t.Fatalf("global fallback plan = %+v, want mp3-compatible", plan)
	}
}

// profileImportFixture builds a one-track album import fixture with a fake
// prober so conversion decisions are steerable without ffprobe.
func profileImportFixture(t *testing.T, prober AudioProber) (*Service, *sql.DB, string, string) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	media := t.TempDir()
	downloads := t.TempDir()
	if _, err = db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES(1,?, 'Artist',1,'missing',1,1)`, testMBID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(2,1,?, 'Album',1,'missing',1,1)`, testRGID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO music_tracks(id,album_id,musicbrainz_id,recording_id,disc,number,title,created_at,updated_at) VALUES(3,2,?,?,1,1,'Song',1,1)`, testTrackID, testRecordingID); err != nil {
		t.Fatal(err)
	}
	service := NewService(db, media)
	service.SetDownloadsDir(downloads)
	service.SetProber(prober)
	return service, db, media, downloads
}

// generateMP3 writes one real MP3 fixture so writeTags (which shells out to
// the real ffmpeg for the staged tag copy) has decodable input.
func generateMP3(t *testing.T, path, title string) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-c:a", "libmp3lame", "-b:a", "128k",
		"-metadata", "artist=Artist", "-metadata", "album=Album", "-metadata", "title="+title,
		"-metadata", "track=1", "-metadata", "disc=1", "-y", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skip("could not generate an MP3 fixture", err, string(out))
	}
}

// profileImportTags is the identity the fake prober reports for the staged
// source; it must match the catalog so verifyEditTags passes.
func profileImportTags(format string, bitrate int) AudioTags {
	return AudioTags{
		Version: audioTagVersion, Format: format, DurationMS: 1000, Bitrate: bitrate,
		SampleRate: 44100, Channels: 2,
		Artist: "Artist", AlbumArtist: "Artist", Album: "Album", Title: "Song",
		Track: intPtr(1), TrackTotal: intPtr(1), Disc: intPtr(1), DiscTotal: intPtr(1),
		MusicBrainzArtistID: testMBID, MusicBrainzReleaseGroupID: testRGID,
		MusicBrainzTrackID: testTrackID, MusicBrainzRecordingID: testRecordingID,
	}
}

func setImportPreferences(t *testing.T, service *Service, mutate func(*MusicPreferences)) {
	t.Helper()
	ctx := context.Background()
	prefs, err := service.GetPreferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mutate(&prefs)
	if _, err := service.SavePreferences(ctx, prefs); err != nil {
		t.Fatal(err)
	}
}

func TestImportConvertsStagedFileToProfile(t *testing.T) {
	helperFFmpeg(t)
	ctx := context.Background()
	prober := &fakeConvertProber{
		source: profileImportTags("mp3", 128),
		output: sourceTags("ogg", 1000, 128),
	}
	service, db, media, downloads := profileImportFixture(t, prober)
	setImportPreferences(t, service, func(p *MusicPreferences) { p.DownloadProfile = "opus-high" })

	source := filepath.Join(downloads, "Album")
	generateMP3(t, filepath.Join(source, "01.mp3"), "Song")
	job, err := service.StartDownloadImport(ctx, "fixture-download", 2, "Album", "Music", map[int]int64{0: 3})
	if err != nil {
		t.Fatal(err)
	}
	job = waitForImport(t, ctx, service, job.ID)
	if job.State != "completed" || job.ImportedCount != 1 {
		t.Fatal("converted import job", job)
	}

	// The converted file is the registered primary, described by the probed
	// output tags and its own digest.
	converted := filepath.Join(media, "Music", "Artist", "Album", "01 Song.opus")
	content, err := os.ReadFile(converted)
	if err != nil {
		t.Fatal("converted file missing", err)
	}
	var format, variantRole string
	var sha string
	if err := db.QueryRow(`SELECT format,sha256,variant_role FROM music_files WHERE track_id=3 AND variant_role='primary'`).Scan(&format, &sha, &variantRole); err != nil {
		t.Fatal(err)
	}
	if format != "ogg" {
		t.Fatalf("registered format = %q, want the converted opus family", format)
	}
	if sha != sha256Hex(content) {
		t.Fatal("the published row does not describe the converted file")
	}
	// The default retention keeps the original alongside as an archive
	// variant.
	var archives int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE track_id=3 AND variant_role='archive'`).Scan(&archives); err != nil || archives != 1 {
		t.Fatal("retained original variant", archives, err)
	}
	if _, err := os.Stat(filepath.Join(media, "Music", "Artist", "Album", "01 Song.mp3")); err != nil {
		t.Fatal("retained original missing", err)
	}
	// The download source is untouched.
	if _, err := os.Stat(filepath.Join(source, "01.mp3")); err != nil {
		t.Fatal("download source was modified", err)
	}
	// No staged files are left behind.
	entries, err := os.ReadDir(filepath.Join(media, "Music", "Artist", "Album"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if len(entry.Name()) > 0 && entry.Name()[0] == '.' {
			t.Fatal("staged file was left behind", entry.Name())
		}
	}
}

func TestImportRetentionDeleteRemovesStagedOriginal(t *testing.T) {
	helperFFmpeg(t)
	ctx := context.Background()
	prober := &fakeConvertProber{
		source: profileImportTags("mp3", 128),
		output: sourceTags("ogg", 1000, 128),
	}
	service, db, media, downloads := profileImportFixture(t, prober)
	setImportPreferences(t, service, func(p *MusicPreferences) {
		p.DownloadProfile = "opus-high"
		p.OriginalRetention = "delete"
	})

	generateMP3(t, filepath.Join(downloads, "Album", "01.mp3"), "Song")
	job, err := service.StartDownloadImport(ctx, "fixture-download", 2, "Album", "Music", map[int]int64{0: 3})
	if err != nil {
		t.Fatal(err)
	}
	job = waitForImport(t, ctx, service, job.ID)
	if job.State != "completed" || job.ImportedCount != 1 || job.Error != "" {
		t.Fatal("delete-retention import job", job)
	}
	if _, err := os.Stat(filepath.Join(media, "Music", "Artist", "Album", "01 Song.opus")); err != nil {
		t.Fatal("converted file missing", err)
	}
	if _, err := os.Stat(filepath.Join(media, "Music", "Artist", "Album", "01 Song.mp3")); !os.IsNotExist(err) {
		t.Fatal("the staged original survived the delete retention", err)
	}
	var files int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE track_id=3`).Scan(&files); err != nil || files != 1 {
		t.Fatal("registered rows", files, err)
	}
	if _, err := os.Stat(filepath.Join(downloads, "Album", "01.mp3")); err != nil {
		t.Fatal("download source was modified", err)
	}
}

func TestImportRetentionAskKeepsOriginalAndRecordsNote(t *testing.T) {
	helperFFmpeg(t)
	ctx := context.Background()
	prober := &fakeConvertProber{
		source: profileImportTags("mp3", 128),
		output: sourceTags("ogg", 1000, 128),
	}
	service, db, media, downloads := profileImportFixture(t, prober)
	setImportPreferences(t, service, func(p *MusicPreferences) {
		p.DownloadProfile = "opus-high"
		p.OriginalRetention = "ask"
	})

	generateMP3(t, filepath.Join(downloads, "Album", "01.mp3"), "Song")
	job, err := service.StartDownloadImport(ctx, "fixture-download", 2, "Album", "Music", map[int]int64{0: 3})
	if err != nil {
		t.Fatal(err)
	}
	job = waitForImport(t, ctx, service, job.ID)
	if job.State != "completed" || job.ImportedCount != 1 {
		t.Fatal("ask-retention import job", job)
	}
	if job.Error == "" || !containsAll(job.Error, "retained", "pending review") {
		t.Fatalf("ask retention did not record an honest note: %q", job.Error)
	}
	if _, err := os.Stat(filepath.Join(media, "Music", "Artist", "Album", "01 Song.mp3")); err != nil {
		t.Fatal("retained original missing", err)
	}
	var archives int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE track_id=3 AND variant_role='archive'`).Scan(&archives); err != nil || archives != 1 {
		t.Fatal("retained original variant", archives, err)
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !substringContains(value, part) {
			return false
		}
	}
	return true
}

func substringContains(value, part string) bool {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return true
		}
	}
	return false
}

func TestImportFailedConversionImportsOriginal(t *testing.T) {
	// The ffmpeg lookup resolves to a path that can never start, so the
	// conversion spawn fails after staging; the import must still complete
	// with the original file and an honest note.
	brokenFFmpeg(t)
	ctx := context.Background()
	prober := &fakeConvertProber{
		source: profileImportTags("mp3", 128),
		output: sourceTags("ogg", 1000, 128),
	}
	service, db, media, downloads := profileImportFixture(t, prober)
	setImportPreferences(t, service, func(p *MusicPreferences) { p.DownloadProfile = "opus-high" })

	generateMP3(t, filepath.Join(downloads, "Album", "01.mp3"), "Song")
	job, err := service.StartDownloadImport(ctx, "fixture-download", 2, "Album", "Music", map[int]int64{0: 3})
	if err != nil {
		t.Fatal(err)
	}
	job = waitForImport(t, ctx, service, job.ID)
	if job.State != "completed" || job.ImportedCount != 1 {
		t.Fatal("fallback import job", job)
	}
	if job.Error == "" || !containsAll(job.Error, "conversion", "failed", "original") {
		t.Fatalf("conversion failure was not recorded honestly: %q", job.Error)
	}
	var format string
	if err := db.QueryRow(`SELECT format FROM music_files WHERE track_id=3`).Scan(&format); err != nil || format != "mp3" {
		t.Fatal("the original was not imported", format, err)
	}
	if _, err := os.Stat(filepath.Join(media, "Music", "Artist", "Album", "01 Song.mp3")); err != nil {
		t.Fatal("original file missing", err)
	}
}

func TestImportLosslessFallbackBest(t *testing.T) {
	helperFFmpeg(t)
	ctx := context.Background()
	prober := &fakeConvertProber{source: profileImportTags("mp3", 128)}
	service, db, media, downloads := profileImportFixture(t, prober)
	if _, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "Lossless Only", Codec: "flac", Container: "flac", RequireLossless: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE music_albums SET download_profile='Lossless Only' WHERE id=2`); err != nil {
		t.Fatal(err)
	}

	generateMP3(t, filepath.Join(downloads, "Album", "01.mp3"), "Song")
	job, err := service.StartDownloadImport(ctx, "fixture-download", 2, "Album", "Music", map[int]int64{0: 3})
	if err != nil {
		t.Fatal(err)
	}
	job = waitForImport(t, ctx, service, job.ID)
	if job.State != "completed" || job.ImportedCount != 1 {
		t.Fatal("lossless-best import job", job)
	}
	if job.Error == "" || !containsAll(job.Error, "lossless quality was not available") {
		t.Fatalf("lossless fallback was not recorded honestly: %q", job.Error)
	}
	// The best available source is stored accurately as probed.
	var format string
	var bitrate int
	if err := db.QueryRow(`SELECT format,bitrate FROM music_files WHERE track_id=3`).Scan(&format, &bitrate); err != nil || format != "mp3" || bitrate != 128 {
		t.Fatal("the lossy source was not stored accurately", format, bitrate, err)
	}
	if _, err := os.Stat(filepath.Join(media, "Music", "Artist", "Album", "01 Song.mp3")); err != nil {
		t.Fatal("original file missing", err)
	}
}

func TestImportLosslessFallbackSkip(t *testing.T) {
	helperFFmpeg(t)
	ctx := context.Background()
	prober := &fakeConvertProber{source: profileImportTags("mp3", 128)}
	service, db, _, downloads := profileImportFixture(t, prober)
	if _, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "Lossless Only", Codec: "flac", Container: "flac", RequireLossless: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE music_albums SET download_profile='Lossless Only' WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	setImportPreferences(t, service, func(p *MusicPreferences) { p.FallbackPolicy = "skip" })

	generateMP3(t, filepath.Join(downloads, "Album", "01.mp3"), "Song")
	job, err := service.StartDownloadImport(ctx, "fixture-download", 2, "Album", "Music", map[int]int64{0: 3})
	if err != nil {
		t.Fatal(err)
	}
	job = waitForImport(t, ctx, service, job.ID)
	if job.State != "failed" {
		t.Fatal("lossless-skip import job", job)
	}
	if job.Error == "" || !containsAll(job.Error, "lossy", "lossless") {
		t.Fatalf("lossless-skip error was not clear: %q", job.Error)
	}
	var files int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE track_id=3`).Scan(&files); err != nil || files != 0 {
		t.Fatal("a lossy source was imported under a lossless-skip policy", files, err)
	}
	for _, file := range job.Files {
		if file.State != "failed" || file.Error == "" {
			t.Fatal("track was not marked failed", file)
		}
	}
}

func TestImportLosslessFallbackApproveKeepsOriginalForReview(t *testing.T) {
	helperFFmpeg(t)
	ctx := context.Background()
	prober := &fakeConvertProber{source: profileImportTags("mp3", 128)}
	service, db, _, downloads := profileImportFixture(t, prober)
	if _, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "Lossless Only", Codec: "flac", Container: "flac", RequireLossless: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE music_albums SET download_profile='Lossless Only' WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	setImportPreferences(t, service, func(p *MusicPreferences) { p.FallbackPolicy = "approve" })

	generateMP3(t, filepath.Join(downloads, "Album", "01.mp3"), "Song")
	job, err := service.StartDownloadImport(ctx, "fixture-download", 2, "Album", "Music", map[int]int64{0: 3})
	if err != nil {
		t.Fatal(err)
	}
	job = waitForImport(t, ctx, service, job.ID)
	if job.State != "failed" {
		t.Fatal("lossless-approve import job", job)
	}
	if job.Error == "" || !containsAll(job.Error, "retained for review") {
		t.Fatalf("lossless-approve error was not honest: %q", job.Error)
	}
	var files int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE track_id=3`).Scan(&files); err != nil || files != 0 {
		t.Fatal("nothing should be imported before the owner approves", files, err)
	}
	// The original download is retained for review.
	if _, err := os.Stat(filepath.Join(downloads, "Album", "01.mp3")); err != nil {
		t.Fatal("the retained original is missing", err)
	}
}

func TestImportTranscodingDisabledImportsUnchanged(t *testing.T) {
	ctx := context.Background()
	prober := &fakeConvertProber{source: profileImportTags("mp3", 128)}
	service, db, media, downloads := profileImportFixture(t, prober)
	setImportPreferences(t, service, func(p *MusicPreferences) {
		p.DownloadProfile = "opus-high"
		p.TranscodingEnabled = false
	})

	generateMP3(t, filepath.Join(downloads, "Album", "01.mp3"), "Song")
	job, err := service.StartDownloadImport(ctx, "fixture-download", 2, "Album", "Music", map[int]int64{0: 3})
	if err != nil {
		t.Fatal(err)
	}
	job = waitForImport(t, ctx, service, job.ID)
	if job.State != "completed" || job.ImportedCount != 1 || job.Error != "" {
		t.Fatal("disabled-transcoding import job", job)
	}
	if _, err := os.Stat(filepath.Join(media, "Music", "Artist", "Album", "01 Song.mp3")); err != nil {
		t.Fatal("original file missing", err)
	}
	if _, err := os.Stat(filepath.Join(media, "Music", "Artist", "Album", "01 Song.opus")); !os.IsNotExist(err) {
		t.Fatal("a conversion happened while transcoding was disabled")
	}
	var files int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE track_id=3`).Scan(&files); err != nil || files != 1 {
		t.Fatal("registered rows", files, err)
	}
}
