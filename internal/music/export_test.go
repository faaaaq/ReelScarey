package music

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

// exportPathProber answers probes by exact path so variant-validation and
// conversion-verification can be steered independently: the ".part" staged
// output answer mirrors fakeConvertProber in convert_test.go.
type exportPathProber struct {
	byPath map[string]AudioTags
	part   AudioTags
}

func (p exportPathProber) ProbeAudio(_ context.Context, path string) (AudioTags, error) {
	if tags, ok := p.byPath[path]; ok {
		return tags, nil
	}
	if strings.HasSuffix(path, ".part") {
		return p.part, nil
	}
	return AudioTags{}, &Error{Code: "audio_unreadable", Message: "unknown probe path in test"}
}

type exportCatalogIDs struct{ artist, album, track int64 }

func exportFixture(t *testing.T) (*Service, *sql.DB, string, exportCatalogIDs) {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// music_files carries foreign keys, so a real artist/album/track chain
	// is seeded exactly like transcodeFixture does.
	store := NewStore(db)
	year := 2024
	saved, err := store.SaveArtist(ctx,
		Artist{MusicBrainzID: testMBID, Name: "Artist", Monitored: true, Status: "missing"},
		[]Album{{MusicBrainzID: testRGID, Title: "Album", Year: &year, Monitored: true, Status: "missing"}})
	if err != nil {
		t.Fatal(err)
	}
	albums, err := store.Albums(ctx, saved.ID)
	if err != nil || len(albums) != 1 {
		t.Fatal("albums", albums, err)
	}
	if err := store.SaveTracks(ctx, albums[0].ID, []Track{{Disc: 1, Number: 1, Title: "Song", DurationMS: 1000}}); err != nil {
		t.Fatal(err)
	}
	tracks, err := store.Tracks(ctx, albums[0].ID)
	if err != nil || len(tracks) != 1 {
		t.Fatal("tracks", tracks, err)
	}
	mediaDir := t.TempDir()
	return NewService(db, mediaDir), db, mediaDir, exportCatalogIDs{artist: saved.ID, album: albums[0].ID, track: tracks[0].ID}
}

func exportInsertFile(t *testing.T, db *sql.DB, artistID, albumID, trackID int64, relative, format string, bitrate int, role, profile string) int64 {
	t.Helper()
	result, err := db.Exec(`INSERT INTO music_files(artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,sample_rate,channels,duration_ms,tag_json,imported_at,origin,source_name,variant_role,variant_profile)
		VALUES(?,?,?,'Music',?,10,'sha',?,?,44100,2,1000,'{}',1,'existing',?,?,?)`,
		artistID, albumID, trackID, relative, format, bitrate, filepath.Base(relative), role, profile)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func exportWrite(t *testing.T, mediaDir, relative string, content []byte) {
	t.Helper()
	abs := filepath.Join(mediaDir, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

// --- Variant reuse (hermetic: no ffmpeg, no ffprobe) ------------------------

func TestExportServesSatisfyingVariant(t *testing.T) {
	service, db, mediaDir, ids := exportFixture(t)
	primary := "Music/Artist/Album/01 Song.flac"
	variant := "Music/Artist/Album/01 Song [opus-high].opus"
	exportWrite(t, mediaDir, primary, []byte("primary-bytes"))
	exportWrite(t, mediaDir, variant, []byte("variant-bytes-for-export"))
	primaryID := exportInsertFile(t, db, ids.artist, ids.album, ids.track, primary, "flac", 900000, "primary", "")
	exportInsertFile(t, db, ids.artist, ids.album, ids.track, variant, "ogg", 130, "variant", "opus-high")
	service.SetProber(exportPathProber{byPath: map[string]AudioTags{
		filepath.Join(mediaDir, filepath.FromSlash(variant)): {Format: "ogg", Bitrate: 130000},
	}})

	export, err := service.OpenExportStream(context.Background(), primaryID, mustProfile("opus-high"))
	if err != nil {
		t.Fatal(err)
	}
	defer export.File.Close()
	content, err := io.ReadAll(export.File)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "variant-bytes-for-export" {
		t.Fatalf("export served %q, not the satisfying variant bytes", content)
	}
	if export.Name != "01 Song.opus" {
		t.Fatalf("download filename %q", export.Name)
	}
	if export.ContentType != "audio/ogg" {
		t.Fatalf("content type %q", export.ContentType)
	}
	if export.Size != int64(len("variant-bytes-for-export")) {
		t.Fatalf("size %d", export.Size)
	}
	// Cleanup is a no-op for a reused library variant and must stay
	// idempotent: the registered file is never removed by an export.
	export.Cleanup()
	export.Cleanup()
	if _, err := os.Stat(filepath.Join(mediaDir, filepath.FromSlash(variant))); err != nil {
		t.Fatalf("reused variant must survive export cleanup: %v", err)
	}
}

func TestExportSkipsLowQualityVariantAndConvertsFresh(t *testing.T) {
	service, db, mediaDir, ids := exportFixture(t)
	helperFFmpeg(t)
	primary := "Music/Artist/Album/01 Song.flac"
	variant := "Music/Artist/Album/01 Song [opus-high].opus"
	sourceBytes := []byte("fresh-source-bytes")
	exportWrite(t, mediaDir, primary, sourceBytes)
	exportWrite(t, mediaDir, variant, []byte("low-quality-variant-bytes"))
	primaryID := exportInsertFile(t, db, ids.artist, ids.album, ids.track, primary, "flac", 900000, "primary", "")
	exportInsertFile(t, db, ids.artist, ids.album, ids.track, variant, "ogg", 64, "variant", "opus-high")
	service.SetProber(exportPathProber{
		byPath: map[string]AudioTags{
			filepath.Join(mediaDir, filepath.FromSlash(variant)): {Format: "ogg", Bitrate: 64000},
			filepath.Join(mediaDir, filepath.FromSlash(primary)): {Format: "flac", DurationMS: 1000},
		},
		part: AudioTags{Format: "ogg", Bitrate: 128000, DurationMS: 1000},
	})

	export, err := service.OpenExportStream(context.Background(), primaryID, mustProfile("opus-high"))
	if err != nil {
		t.Fatal(err)
	}
	defer export.File.Close()
	content, err := io.ReadAll(export.File)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(sourceBytes) {
		// The fake ffmpeg streams stdin to stdout, so a fresh conversion
		// produces exactly the source bytes; the low-quality variant was
		// never acceptable and must not have been served.
		t.Fatalf("export served %q instead of a fresh conversion", content)
	}
	dir := filepath.Dir(export.File.Name())
	if !strings.HasPrefix(filepath.Base(dir), "reelscarey-export-") {
		t.Fatalf("conversion output %q is not in an isolated export temp directory", export.File.Name())
	}
	if strings.HasPrefix(export.File.Name(), mediaDir) {
		t.Fatal("export must never write inside the media root")
	}
	if export.Name != "01 Song.opus" || export.ContentType != "audio/ogg" {
		t.Fatalf("name/content type %q/%q", export.Name, export.ContentType)
	}
	if export.Size <= 0 {
		t.Fatalf("size %d", export.Size)
	}
	if err := export.File.Close(); err != nil {
		t.Fatal(err)
	}
	export.Cleanup()
	export.Cleanup()
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup must remove the export temp directory, got %v", err)
	}
}

func TestExportRejectsOriginalAndUnknownFiles(t *testing.T) {
	service, db, mediaDir, ids := exportFixture(t)
	primary := "Music/Artist/Album/01 Song.flac"
	exportWrite(t, mediaDir, primary, []byte("primary-bytes"))
	primaryID := exportInsertFile(t, db, ids.artist, ids.album, ids.track, primary, "flac", 900000, "primary", "")
	if _, err := service.OpenExportStream(context.Background(), primaryID, mustProfile("original")); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("original profile must be refused, got %v", err)
	}
	if _, err := service.OpenExportStream(context.Background(), 0, mustProfile("opus-high")); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid id must be refused, got %v", err)
	}
	if _, err := service.OpenExportStream(context.Background(), primaryID+99, mustProfile("opus-high")); err == nil {
		t.Fatal("unknown file must be refused")
	}
}

// --- Fresh conversions with real ffmpeg/ffprobe -----------------------------

// exportLiveFFmpeg runs the conversion and probe assertions against the real
// ffmpeg/ffprobe when they are installed; without them the test skips
// honestly instead of faking a pass.
func exportLiveFFmpeg(t *testing.T) {
	t.Helper()
	resetTranscodeCacheForTest()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed on this machine")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is not installed on this machine")
	}
	t.Cleanup(resetTranscodeCacheForTest)
}

type exportProbeResult struct {
	Format   string
	BitRate  string
	Tags     map[string]string
	Attached bool
	Streams  []string
}

func exportProbeOutput(t *testing.T, path string) exportProbeResult {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error",
		"-show_entries", "format=format_name,bit_rate:format_tags:stream=codec_type,codec_name:stream_disposition=attached_pic:stream_tags",
		"-of", "json", path).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	var raw struct {
		Format struct {
			Name    string            `json:"format_name"`
			BitRate string            `json:"bit_rate"`
			Tags    map[string]string `json:"tags"`
		} `json:"format"`
		Streams []struct {
			Type        string            `json:"codec_type"`
			Name        string            `json:"codec_name"`
			Tags        map[string]string `json:"tags"`
			Disposition struct {
				AttachedPic int `json:"attached_pic"`
			} `json:"disposition"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		t.Fatalf("ffprobe output: %v", err)
	}
	result := exportProbeResult{Format: raw.Format.Name, BitRate: raw.Format.BitRate, Tags: map[string]string{}, Streams: []string{}}
	for key, value := range raw.Format.Tags {
		result.Tags[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	for _, stream := range raw.Streams {
		result.Streams = append(result.Streams, stream.Type+":"+stream.Name)
		if stream.Type == "video" && stream.Disposition.AttachedPic == 1 {
			result.Attached = true
		}
		// Ogg/Opus keeps Vorbis comments on the stream (same rule as
		// parseAudioProbe), so stream tags join the merged tag view without
		// overriding format-level values.
		if stream.Type == "audio" {
			for key, value := range stream.Tags {
				key = strings.ToLower(strings.TrimSpace(key))
				if key == "encoder" {
					continue
				}
				if _, exists := result.Tags[key]; !exists {
					result.Tags[key] = strings.TrimSpace(value)
				}
			}
		}
	}
	return result
}

// exportTaggedSourceFixture generates a one-second MP3 with a sine wave,
// full text metadata and an embedded JPEG cover, registers it in the catalog
// and returns the catalog row id.
func exportTaggedSourceFixture(t *testing.T, db *sql.DB, mediaDir string, ids exportCatalogIDs) int64 {
	t.Helper()
	relative := "Music/Artist/Album/01 Song.mp3"
	abs := filepath.Join(mediaDir, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	art := filepath.Join(workDir, "art.jpg")
	if out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=red:s=8x8", "-frames:v", "1", art).CombinedOutput(); err != nil {
		t.Fatalf("generate cover art: %v: %s", err, out)
	}
	// The sine source is 30 seconds: a 1-second fixture makes the MP3
	// encoder's ~45ms padding dominate the conversion engine's 2% duration
	// tolerance on older ffmpeg builds (5.x/6.x report the padded source
	// duration while the decoded length is 1s), failing otherwise-correct
	// conversions. Thirty seconds keeps the truncation guard meaningful while
	// absorbing encoder padding on every ffmpeg the CI matrix can produce.
	if out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=30", "-i", art,
		"-map", "0:a", "-map", "1", "-c:a", "libmp3lame", "-b:a", "64k",
		"-c:v", "copy", "-disposition:v", "attached_pic",
		"-metadata", "title=Export Song", "-metadata", "artist=Export Artist",
		"-metadata", "album_artist=Export Album Artist", "-metadata", "album=Export Album",
		"-metadata", "track=7", "-metadata", "disc=2", "-metadata", "date=1999",
		"-metadata", "genre=Test Genre",
		"-metadata", "MusicBrainz Album Id=9f1b2f6a-0000-4000-8000-000000000001",
		abs).CombinedOutput(); err != nil {
		t.Fatalf("generate tagged source fixture: %v: %s", err, out)
	}
	return exportInsertFile(t, db, ids.artist, ids.album, ids.track, relative, "mp3", 64, "primary", "")
}

func exportCheckCommonTags(t *testing.T, probe exportProbeResult) {
	t.Helper()
	expect := map[string]string{
		"title": "Export Song", "artist": "Export Artist", "album": "Export Album",
		"album_artist": "Export Album Artist", "genre": "Test Genre",
	}
	for key, want := range expect {
		if got := probe.Tags[key]; got != want {
			t.Fatalf("tag %s = %q, want %q (all tags: %v)", key, got, want, probe.Tags)
		}
	}
	if track := probe.Tags["track"]; track != "7" && track != "7/0" {
		t.Fatalf("track tag = %q", track)
	}
	if date := probe.Tags["date"]; date != "1999" && date != "1999-01-01" {
		t.Fatalf("date tag = %q", date)
	}
}

func exportCheckNameTypeCleanup(t *testing.T, export ExportStream, mediaDir, name, contentType string) {
	t.Helper()
	if export.Name != name {
		t.Fatalf("download filename %q, want %q", export.Name, name)
	}
	if export.ContentType != contentType {
		t.Fatalf("content type %q, want %q", export.ContentType, contentType)
	}
	if export.Size <= 0 || export.ModTime.IsZero() {
		t.Fatalf("size/mod time %d/%v", export.Size, export.ModTime)
	}
	dir := filepath.Dir(export.File.Name())
	if !strings.HasPrefix(filepath.Base(dir), "reelscarey-export-") {
		t.Fatalf("conversion output %q is not in an isolated export temp directory", export.File.Name())
	}
	if strings.HasPrefix(export.File.Name(), mediaDir) {
		t.Fatal("export must never write inside the media root")
	}
	if err := export.File.Close(); err != nil {
		t.Fatal(err)
	}
	export.Cleanup()
	export.Cleanup()
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup must remove the export temp directory, got %v", err)
	}
}

func TestExportConvertsFreshMP3WithMetadataAndArtwork(t *testing.T) {
	exportLiveFFmpeg(t)
	service, db, mediaDir, ids := exportFixture(t)
	fileID := exportTaggedSourceFixture(t, db, mediaDir, ids)
	export, err := service.OpenExportStream(context.Background(), fileID, mustProfile("mp3-compatible"))
	if err != nil {
		t.Fatal(err)
	}
	probe := exportProbeOutput(t, export.File.Name())
	if !strings.Contains(probe.Format, "mp3") {
		t.Fatalf("output format %q", probe.Format)
	}
	if bitrate, err := parseProbeBitrate(probe.BitRate); err != nil || bitrate < 150000 {
		t.Fatalf("output bitrate %q (%v), want at least 150000 for the 192 kbps profile", probe.BitRate, err)
	}
	if !probe.Attached {
		t.Fatalf("mp3 export must keep the embedded cover art; streams %v", probe.Streams)
	}
	exportCheckCommonTags(t, probe)
	exportCheckNameTypeCleanup(t, export, mediaDir, "01 Song.mp3", "audio/mpeg")
}

func TestExportConvertsFreshFLACWithMetadataAndArtwork(t *testing.T) {
	exportLiveFFmpeg(t)
	service, db, mediaDir, ids := exportFixture(t)
	fileID := exportTaggedSourceFixture(t, db, mediaDir, ids)
	export, err := service.OpenExportStream(context.Background(), fileID, mustProfile("lossless-archive"))
	if err != nil {
		t.Fatal(err)
	}
	probe := exportProbeOutput(t, export.File.Name())
	if !strings.Contains(probe.Format, "flac") {
		t.Fatalf("output format %q", probe.Format)
	}
	if !probe.Attached {
		t.Fatalf("flac export must keep the embedded cover art; streams %v", probe.Streams)
	}
	exportCheckCommonTags(t, probe)
	exportCheckNameTypeCleanup(t, export, mediaDir, "01 Song.flac", "audio/flac")
}

func TestExportConvertsFreshM4AWithCoverArt(t *testing.T) {
	exportLiveFFmpeg(t)
	service, db, mediaDir, ids := exportFixture(t)
	fileID := exportTaggedSourceFixture(t, db, mediaDir, ids)
	export, err := service.OpenExportStream(context.Background(), fileID, mustProfile("aac-compatible"))
	if err != nil {
		t.Fatal(err)
	}
	probe := exportProbeOutput(t, export.File.Name())
	if !strings.Contains(probe.Format, "mp4") && !strings.Contains(probe.Format, "m4a") {
		t.Fatalf("output format %q", probe.Format)
	}
	if !probe.Attached {
		t.Fatalf("m4a export must embed the cover art in the covr atom; streams %v", probe.Streams)
	}
	exportCheckCommonTags(t, probe)
	exportCheckNameTypeCleanup(t, export, mediaDir, "01 Song.m4a", "audio/mp4")
}

func TestExportOpusKeepsTagsButDocumentsNoArtwork(t *testing.T) {
	exportLiveFFmpeg(t)
	service, db, mediaDir, ids := exportFixture(t)
	fileID := exportTaggedSourceFixture(t, db, mediaDir, ids)
	export, err := service.OpenExportStream(context.Background(), fileID, mustProfile("opus-high"))
	if err != nil {
		t.Fatal(err)
	}
	probe := exportProbeOutput(t, export.File.Name())
	// The ffmpeg ogg muxer cannot attach pictures reliably, so an Opus
	// export intentionally drops artwork while keeping audio and text
	// metadata; the documented limitation must hold exactly.
	for _, stream := range probe.Streams {
		if strings.HasPrefix(stream, "video") {
			t.Fatalf("opus export must not carry an artwork stream, got %v", probe.Streams)
		}
	}
	exportCheckCommonTags(t, probe)
	exportCheckNameTypeCleanup(t, export, mediaDir, "01 Song.opus", "audio/ogg")
}

func parseProbeBitrate(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("ffprobe reported no bit rate")
	}
	return strconv.ParseInt(value, 10, 64)
}
