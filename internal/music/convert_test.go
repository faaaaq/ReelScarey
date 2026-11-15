package music

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
)

// The fake "ffmpeg" is the test binary itself (see transcode_cap_test.go
// TestMain): a conversion child started with the -hide_banner first argument
// streams stdin to stdout. ConvertFile wires cmd.Stdin to the open source
// file, so the fake child's staged output is a deterministic copy of the
// source bytes and real ffmpeg (which ignores stdin because of -nostdin)
// behaves identically apart from encoding. Argv content is asserted directly
// against the pure convertArgs function.

// convertFixture builds a Service with a real migrated database (the seeded
// preferences row is what ConvertFile reads MaxConversions from) and returns
// a disposable working directory.
func convertFixture(t *testing.T) (*Service, string) {
	t.Helper()
	db, err := database.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	dir := t.TempDir()
	return NewService(db, dir), dir
}

// fakeConvertProber answers source probes with sourceTags and probes of any
// staged ".part" output with outputTags (or outErr), so validation can be
// steered without ffprobe.
type fakeConvertProber struct {
	source AudioTags
	output AudioTags
	outErr error
}

func (p *fakeConvertProber) ProbeAudio(_ context.Context, path string) (AudioTags, error) {
	if strings.HasSuffix(path, ".part") {
		if p.outErr != nil {
			return AudioTags{}, p.outErr
		}
		return p.output, nil
	}
	return p.source, nil
}

func sourceTags(format string, durationMS, bitrate int) AudioTags {
	return AudioTags{Version: audioTagVersion, Format: format, DurationMS: durationMS, Bitrate: bitrate}
}

func resetConvertSlotsForTest(size int) chan struct{} {
	convertCaps.once = sync.Once{}
	convertCaps.slots = nil
	return convertSlots(size)
}

// missingFFmpeg pins ffmpeg detection on a failing lookup: ffmpegPath()
// resolves to "" and ConvertFile must report music_ffmpeg_missing without
// spawning anything.
func missingFFmpeg(t *testing.T) {
	t.Helper()
	transcodeLookup = func(string) (string, error) { return "", os.ErrNotExist }
	resetTranscodeCacheForTest()
	t.Cleanup(func() {
		transcodeLookup = exec.LookPath
		resetTranscodeCacheForTest()
	})
}

// countingMissingFFmpeg pins ffmpeg detection on a failing lookup that
// counts how often it is consulted, proving the copy path never resolves
// ffmpeg at all.
func countingMissingFFmpeg(t *testing.T) *int {
	t.Helper()
	calls := 0
	transcodeLookup = func(string) (string, error) {
		calls++
		return "", os.ErrNotExist
	}
	resetTranscodeCacheForTest()
	t.Cleanup(func() {
		transcodeLookup = exec.LookPath
		resetTranscodeCacheForTest()
	})
	return &calls
}

// mustProfile returns a builtin profile by key; tests fail hard on a typo.
func mustProfile(key string) QualityProfile {
	profile, ok := ProfileByKey(key)
	if !ok {
		panic("unknown builtin profile " + key)
	}
	return profile
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// --- ConversionNeeded matrix ------------------------------------------------

func TestConversionNeededMatrix(t *testing.T) {
	cases := []struct {
		name         string
		source       AudioTags
		profile      QualityProfile
		wantNeeded   bool
		wantContains string
	}{
		{"original never converts", sourceTags("mp3", 1000, 320), mustProfile("original"), false, "original"},
		{"flac to flac copies", sourceTags("flac", 1000, 900), mustProfile("lossless-archive"), false, "already FLAC"},
		{"wav to flac re-packages losslessly", sourceTags("wav", 1000, 1411), mustProfile("lossless-archive"), true, "without quality loss"},
		{"mp3 320 to mp3 192 copies", sourceTags("mp3", 1000, 320), mustProfile("mp3-compatible"), false, "inherits the source quality"},
		{"mp3 128 to mp3 192 re-encodes", sourceTags("mp3", 1000, 128), mustProfile("mp3-compatible"), true, "Re-encoding"},
		{"mp3 unknown bitrate re-encodes", sourceTags("mp3", 1000, 0), mustProfile("mp3-compatible"), true, "unknown"},
		{"m4a 256 to aac 128 copies", sourceTags("mov,mp4,m4a,3gp,3g2,mj2", 1000, 256), mustProfile("aac-compatible"), false, "AAC/M4A"},
		{"m4a 96 to aac 128 re-encodes", sourceTags("mov,mp4,m4a,3gp,3g2,mj2", 1000, 96), mustProfile("aac-compatible"), true, "Re-encoding"},
		{"adts source to m4a profile re-encodes", sourceTags("aac", 1000, 256), mustProfile("aac-compatible"), true, "Converting"},
		{"opus 160 to opus 128 copies", sourceTags("ogg", 1000, 160), mustProfile("opus-high"), false, "Opus"},
		{"opus 96 to opus 128 re-encodes", sourceTags("ogg", 1000, 96), mustProfile("opus-high"), true, "Re-encoding"},
		{"mp3 to opus converts", sourceTags("mp3", 1000, 320), mustProfile("opus-high"), true, "bounded by the source"},
		{"flac to opus converts", sourceTags("flac", 1000, 900), mustProfile("opus-balanced"), true, "bounded by the source"},
		{"lossy to flac without requirement is honest", sourceTags("mp3", 1000, 320), mustProfile("lossless-archive"), true, "does not restore lost detail"},
		{"lossy to flac with requirement needs lossless", sourceTags("mp3", 1000, 320), QualityProfile{Codec: "flac", Container: "flac", RequireLossless: true}, true, "requires a lossless source"},
		{"flac source satisfies lossless requirement", sourceTags("flac", 1000, 900), QualityProfile{Codec: "opus", Container: "opus", BitrateKbps: 128, RequireLossless: true}, true, "bounded by the source"},
		{"unknown source format converts", sourceTags("wma", 1000, 192), mustProfile("mp3-compatible"), true, "bounded by the source"},
		{"unknown codec needs conversion", sourceTags("mp3", 1000, 320), QualityProfile{Codec: "wma"}, true, "unsupported"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			needed, reason := ConversionNeeded(tc.source, tc.profile)
			if needed != tc.wantNeeded {
				t.Fatalf("needed = %v, want %v (reason %q)", needed, tc.wantNeeded, reason)
			}
			if !strings.Contains(reason, tc.wantContains) {
				t.Fatalf("reason %q does not contain %q", reason, tc.wantContains)
			}
		})
	}
}

// --- Argument generation -----------------------------------------------------

func hasArg(args []string, flag, value string) bool {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) && args[i+1] == value {
			return true
		}
	}
	return false
}

func hasFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}

func TestConvertArgs(t *testing.T) {
	source := "C:\\stage\\src.ogg"
	output := "C:\\stage\\out.mp3"

	t.Run("common flags", func(t *testing.T) {
		args := convertArgs(mustProfile("mp3-compatible"), source, output)
		if args[0] != "-hide_banner" {
			t.Fatalf("first arg = %q, want -hide_banner", args[0])
		}
		for _, flag := range []string{"-nostdin", "-y", "-sn", "-dn"} {
			if !hasFlag(args, flag) {
				t.Fatalf("missing flag %s in %v", flag, args)
			}
		}
		if !hasArg(args, "-i", source) {
			t.Fatalf("missing -i %s in %v", source, args)
		}
		if !hasArg(args, "-map_metadata", "0") {
			t.Fatalf("missing -map_metadata 0 (source tags must be preserved) in %v", args)
		}
		if args[len(args)-1] != output {
			t.Fatalf("output path must be the final argument, got %v", args)
		}
	})

	t.Run("mp3 CBR", func(t *testing.T) {
		args := convertArgs(mustProfile("mp3-compatible"), source, output)
		if !hasArg(args, "-c:a", "libmp3lame") || !hasArg(args, "-b:a", "192k") {
			t.Fatalf("mp3 CBR flags missing: %v", args)
		}
		if hasFlag(args, "-q:a") {
			t.Fatalf("CBR mp3 must not use -q:a: %v", args)
		}
		if !hasArg(args, "-map", "0:v?") || !hasArg(args, "-c:v", "copy") {
			t.Fatalf("mp3 must copy embedded artwork: %v", args)
		}
		if !hasArg(args, "-f", "mp3") {
			t.Fatalf("missing mp3 muxer: %v", args)
		}
	})

	t.Run("mp3 VBR scale", func(t *testing.T) {
		cases := map[int]string{320: "0", 256: "1", 192: "2", 160: "3", 128: "4", 96: "5", 64: "7", 100: "5"}
		for kbps, q := range cases {
			args := convertArgs(QualityProfile{Codec: "mp3", Container: "mp3", BitrateKbps: kbps, VBR: true}, source, output)
			if !hasArg(args, "-q:a", q) {
				t.Fatalf("%d kbps VBR: want -q:a %s, got %v", kbps, q, args)
			}
			if hasFlag(args, "-b:a") {
				t.Fatalf("%d kbps VBR mp3 must not set -b:a: %v", kbps, args)
			}
		}
	})

	t.Run("flac", func(t *testing.T) {
		args := convertArgs(mustProfile("lossless-archive"), source, "out.flac")
		if !hasArg(args, "-c:a", "flac") || !hasArg(args, "-compression_level", "8") {
			t.Fatalf("flac flags missing: %v", args)
		}
		if !hasArg(args, "-f", "flac") || !hasArg(args, "-map", "0:v?") {
			t.Fatalf("flac artwork/muxer flags missing: %v", args)
		}
		if hasArg(args, "-b:a", "0k") {
			t.Fatalf("lossless flac must not set a bitrate: %v", args)
		}
	})

	t.Run("aac m4a faststart, bitrate always -b:a", func(t *testing.T) {
		vbr := mustProfile("aac-high")
		vbr.VBR = true
		args := convertArgs(vbr, source, "out.m4a")
		if !hasArg(args, "-c:a", "aac") || !hasArg(args, "-b:a", "256k") {
			t.Fatalf("aac flags missing: %v", args)
		}
		// The native AAC encoder has no stable VBR mode, so even a VBR
		// profile must produce a plain -b:a command line.
		if hasFlag(args, "-q:a") {
			t.Fatalf("aac must ignore the VBR flag: %v", args)
		}
		if !hasArg(args, "-f", "ipod") || !hasArg(args, "-movflags", "+faststart") {
			t.Fatalf("m4a container flags missing: %v", args)
		}
		if !hasFlag(args, "-vn") || hasArg(args, "-map", "0:v?") {
			t.Fatalf("aac output must drop artwork with -vn: %v", args)
		}
	})

	t.Run("aac adts container", func(t *testing.T) {
		args := convertArgs(QualityProfile{Codec: "aac", Container: "adts", BitrateKbps: 128}, source, "out.adts")
		if !hasArg(args, "-f", "adts") || hasArg(args, "-movflags", "+faststart") {
			t.Fatalf("adts container flags wrong: %v", args)
		}
	})

	t.Run("opus is VBR via -b:a in ogg", func(t *testing.T) {
		args := convertArgs(mustProfile("opus-high"), source, "out.ogg")
		if !hasArg(args, "-c:a", "libopus") || !hasArg(args, "-b:a", "128k") {
			t.Fatalf("opus flags missing: %v", args)
		}
		if !hasArg(args, "-f", "ogg") {
			t.Fatalf("opus must use the ogg container: %v", args)
		}
		// Ogg has no attached-picture support that survives clients, so the
		// artwork stream is dropped instead of copied.
		if !hasFlag(args, "-vn") || hasArg(args, "-map", "0:v?") {
			t.Fatalf("opus output must drop artwork with -vn: %v", args)
		}
	})
}

// --- ConvertFile -------------------------------------------------------------

func TestConvertFileEncodeSuccess(t *testing.T) {
	helperFFmpeg(t)
	s, dir := convertFixture(t)
	source := []byte("ogg-audio-bytes-for-convert-test")
	srcPath := filepath.Join(dir, "song.ogg")
	outPath := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(srcPath, source, 0o600); err != nil {
		t.Fatal(err)
	}
	s.SetProber(&fakeConvertProber{
		source: sourceTags("ogg", 1000, 160),
		output: sourceTags("mp3", 1000, 192),
	})

	result, err := s.ConvertFile(context.Background(), ConvertRequest{
		SourcePath: srcPath,
		OutputPath: outPath,
		Profile:    mustProfile("mp3-compatible"),
	})
	if err != nil {
		t.Fatalf("ConvertFile: %v", err)
	}
	// The fake ffmpeg replays stdin (the source bytes) to stdout, so the
	// published output must equal the source byte-for-byte and the reported
	// digest matches those bytes.
	if result.SHA256 != sha256Hex(source) {
		t.Fatalf("result.SHA256 = %s, want digest of source bytes", result.SHA256)
	}
	if result.SizeBytes != int64(len(source)) {
		t.Fatalf("result.SizeBytes = %d, want %d", result.SizeBytes, len(source))
	}
	if result.DurationMS != 1000 || result.Tags.Format != "mp3" {
		t.Fatalf("unexpected result tags: %+v", result.Tags)
	}
	out, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("output missing: %v", err)
	}
	if !bytes.Equal(out, source) {
		t.Fatal("published output does not match the fake ffmpeg bytes")
	}
	if _, err := os.Stat(outPath + ".part"); !os.IsNotExist(err) {
		t.Fatal("staged .part file was left behind")
	}
	after, err := os.ReadFile(srcPath)
	if err != nil || !bytes.Equal(after, source) {
		t.Fatal("source bytes were modified")
	}
	// The shared semaphore is sized from the seeded max_conversions default.
	if cap(convertCaps.slots) != defaultMaxConversions {
		t.Fatalf("conversion semaphore cap = %d, want %d", cap(convertCaps.slots), defaultMaxConversions)
	}
}

func TestConvertFileInvalidOutput(t *testing.T) {
	helperFFmpeg(t)
	s, dir := convertFixture(t)
	source := []byte("source-bytes")
	srcPath := filepath.Join(dir, "song.ogg")
	outPath := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(srcPath, source, 0o600); err != nil {
		t.Fatal(err)
	}
	s.SetProber(&fakeConvertProber{
		source: sourceTags("ogg", 1000, 160),
		outErr: &Error{Code: "audio_unreadable", Message: "probe failed"},
	})

	_, err := s.ConvertFile(context.Background(), ConvertRequest{
		SourcePath: srcPath,
		OutputPath: outPath,
		Profile:    mustProfile("mp3-compatible"),
	})
	var musicErr *Error
	if !errors.As(err, &musicErr) || musicErr.Code != "music_output_invalid" {
		t.Fatalf("expected music_output_invalid, got %v", err)
	}
	if _, statErr := os.Stat(outPath); !os.IsNotExist(statErr) {
		t.Fatal("invalid output was published")
	}
	if _, statErr := os.Stat(outPath + ".part"); !os.IsNotExist(statErr) {
		t.Fatal("staged .part file was left behind")
	}
	after, _ := os.ReadFile(srcPath)
	if !bytes.Equal(after, source) {
		t.Fatal("source bytes were modified")
	}
}

func TestConvertFileOutputCodecMismatch(t *testing.T) {
	helperFFmpeg(t)
	s, dir := convertFixture(t)
	srcPath := filepath.Join(dir, "song.ogg")
	outPath := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(srcPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.SetProber(&fakeConvertProber{
		source: sourceTags("ogg", 1000, 160),
		output: sourceTags("ogg", 1000, 192), // wrong codec for mp3 profile
	})
	_, err := s.ConvertFile(context.Background(), ConvertRequest{
		SourcePath: srcPath,
		OutputPath: outPath,
		Profile:    mustProfile("mp3-compatible"),
	})
	var musicErr *Error
	if !errors.As(err, &musicErr) || musicErr.Code != "music_output_invalid" {
		t.Fatalf("expected music_output_invalid for codec mismatch, got %v", err)
	}
}

func TestConvertFileDurationMismatch(t *testing.T) {
	helperFFmpeg(t)
	s, dir := convertFixture(t)
	srcPath := filepath.Join(dir, "song.ogg")
	outPath := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(srcPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.SetProber(&fakeConvertProber{
		source: sourceTags("ogg", 1000, 160),
		output: sourceTags("mp3", 1200, 192), // 20% off, far beyond 2%
	})
	_, err := s.ConvertFile(context.Background(), ConvertRequest{
		SourcePath: srcPath,
		OutputPath: outPath,
		Profile:    mustProfile("mp3-compatible"),
	})
	var musicErr *Error
	if !errors.As(err, &musicErr) || musicErr.Code != "music_output_invalid" {
		t.Fatalf("expected music_output_invalid for duration mismatch, got %v", err)
	}
}

func TestConvertFileRequireLosslessWithLossySource(t *testing.T) {
	// A broken ffmpeg lookup proves the failure happens before any spawn.
	brokenFFmpeg(t)
	s, dir := convertFixture(t)
	srcPath := filepath.Join(dir, "song.mp3")
	outPath := filepath.Join(dir, "song.flac")
	if err := os.WriteFile(srcPath, []byte("lossy-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.SetProber(&fakeConvertProber{source: sourceTags("mp3", 1000, 320)})

	_, err := s.ConvertFile(context.Background(), ConvertRequest{
		SourcePath: srcPath,
		OutputPath: outPath,
		Profile:    QualityProfile{Codec: "flac", Container: "flac", RequireLossless: true},
	})
	var musicErr *Error
	if !errors.As(err, &musicErr) || musicErr.Code != "music_source_not_lossless" {
		t.Fatalf("expected music_source_not_lossless, got %v", err)
	}
	if _, statErr := os.Stat(outPath); !os.IsNotExist(statErr) {
		t.Fatal("no output may be produced for a lossless-only profile fed a lossy source")
	}
}

func TestConvertFileCopySkipsFFmpeg(t *testing.T) {
	calls := countingMissingFFmpeg(t)
	s, dir := convertFixture(t)
	source := []byte("mp3-320-bytes")
	srcPath := filepath.Join(dir, "song.mp3")
	outPath := filepath.Join(dir, "song-copy.mp3")
	if err := os.WriteFile(srcPath, source, 0o600); err != nil {
		t.Fatal(err)
	}
	s.SetProber(&fakeConvertProber{
		source: sourceTags("mp3", 1000, 320),
		output: sourceTags("mp3", 1000, 320),
	})

	result, err := s.ConvertFile(context.Background(), ConvertRequest{
		SourcePath: srcPath,
		OutputPath: outPath,
		Profile:    mustProfile("mp3-compatible"), // 320 >= 192: copy, no re-encode
	})
	if err != nil {
		t.Fatalf("copy path must succeed without ffmpeg: %v", err)
	}
	if *calls != 0 {
		t.Fatalf("copy path consulted ffmpeg detection %d times, want 0", *calls)
	}
	if result.SHA256 != sha256Hex(source) || result.SizeBytes != int64(len(source)) {
		t.Fatalf("copy result does not describe the copied bytes: %+v", result)
	}
	out, err := os.ReadFile(outPath)
	if err != nil || !bytes.Equal(out, source) {
		t.Fatal("published copy is not bit-exact with the source")
	}
	if _, statErr := os.Stat(outPath + ".part"); !os.IsNotExist(statErr) {
		t.Fatal("staged .part file was left behind")
	}
}

func TestConvertFileOriginalProfileCopies(t *testing.T) {
	helperFFmpeg(t)
	s, dir := convertFixture(t)
	source := []byte("original-bytes")
	srcPath := filepath.Join(dir, "song.flac")
	outPath := filepath.Join(dir, "song-out.flac")
	if err := os.WriteFile(srcPath, source, 0o600); err != nil {
		t.Fatal(err)
	}
	s.SetProber(&fakeConvertProber{
		source: sourceTags("flac", 1000, 900),
		output: sourceTags("flac", 1000, 900),
	})
	result, err := s.ConvertFile(context.Background(), ConvertRequest{
		SourcePath: srcPath,
		OutputPath: outPath,
		Profile:    mustProfile("original"),
	})
	if err != nil {
		t.Fatalf("original profile: %v", err)
	}
	if result.SHA256 != sha256Hex(source) {
		t.Fatal("original profile must publish a bit-exact copy")
	}
}

func TestConvertFileFFmpegMissing(t *testing.T) {
	missingFFmpeg(t)
	s, dir := convertFixture(t)
	srcPath := filepath.Join(dir, "song.ogg")
	outPath := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(srcPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.SetProber(&fakeConvertProber{source: sourceTags("ogg", 1000, 160)})

	_, err := s.ConvertFile(context.Background(), ConvertRequest{
		SourcePath: srcPath,
		OutputPath: outPath,
		Profile:    mustProfile("mp3-compatible"),
	})
	var musicErr *Error
	if !errors.As(err, &musicErr) || musicErr.Code != "music_ffmpeg_missing" {
		t.Fatalf("expected music_ffmpeg_missing, got %v", err)
	}
	if _, statErr := os.Stat(outPath + ".part"); !os.IsNotExist(statErr) {
		t.Fatal("no stage file may be created when ffmpeg is missing")
	}
}

func TestConvertFileSpawnFailureCleansStage(t *testing.T) {
	brokenFFmpeg(t)
	s, dir := convertFixture(t)
	srcPath := filepath.Join(dir, "song.ogg")
	outPath := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(srcPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.SetProber(&fakeConvertProber{source: sourceTags("ogg", 1000, 160)})

	_, err := s.ConvertFile(context.Background(), ConvertRequest{
		SourcePath: srcPath,
		OutputPath: outPath,
		Profile:    mustProfile("mp3-compatible"),
	})
	var musicErr *Error
	if !errors.As(err, &musicErr) || musicErr.Code != "music_ffmpeg_missing" {
		t.Fatalf("expected music_ffmpeg_missing for a spawn failure, got %v", err)
	}
	if _, statErr := os.Stat(outPath + ".part"); !os.IsNotExist(statErr) {
		t.Fatal("failed spawn left a staged .part file behind")
	}
}

func TestConvertFileRefusesOverwrite(t *testing.T) {
	helperFFmpeg(t)
	s, dir := convertFixture(t)
	srcPath := filepath.Join(dir, "song.ogg")
	outPath := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(srcPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outPath, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.SetProber(&fakeConvertProber{source: sourceTags("ogg", 1000, 160)})
	if _, err := s.ConvertFile(context.Background(), ConvertRequest{
		SourcePath: srcPath,
		OutputPath: outPath,
		Profile:    mustProfile("mp3-compatible"),
	}); err == nil {
		t.Fatal("ConvertFile must refuse to overwrite an existing output")
	}
}

func TestConvertFileCancelledBeforeStart(t *testing.T) {
	helperFFmpeg(t)
	s, dir := convertFixture(t)
	srcPath := filepath.Join(dir, "song.ogg")
	outPath := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(srcPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.SetProber(&fakeConvertProber{source: sourceTags("ogg", 1000, 160)})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.ConvertFile(ctx, ConvertRequest{
		SourcePath: srcPath,
		OutputPath: outPath,
		Profile:    mustProfile("mp3-compatible"),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if _, statErr := os.Stat(outPath); !os.IsNotExist(statErr) {
		t.Fatal("cancelled request produced an output")
	}
	if _, statErr := os.Stat(outPath + ".part"); !os.IsNotExist(statErr) {
		t.Fatal("cancelled request left a staged .part file")
	}
}

// A cancelled in-flight conversion must kill the child process and leave no
// staged output behind. The fake ffmpeg replays the source bytes in 512-byte
// reads, so a multi-megabyte source keeps the child alive for the whole
// cancellation window.
func TestConvertFileKillsProcessOnCancel(t *testing.T) {
	helperFFmpeg(t)
	s, dir := convertFixture(t)
	chunk := bytes.Repeat([]byte("0123456789abcdef"), 64<<10) // 1 MiB
	var source bytes.Buffer
	for i := 0; i < 64; i++ {
		source.Write(chunk)
	}
	srcPath := filepath.Join(dir, "song.ogg")
	outPath := filepath.Join(dir, "song.mp3")
	partPath := outPath + ".part"
	if err := os.WriteFile(srcPath, source.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	s.SetProber(&fakeConvertProber{source: sourceTags("ogg", 0, 160)})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		result ConvertResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := s.ConvertFile(ctx, ConvertRequest{
			SourcePath: srcPath,
			OutputPath: outPath,
			Profile:    mustProfile("mp3-compatible"),
		})
		done <- outcome{result, err}
	}()

	// Wait until the child is demonstrably writing the stage, then cancel.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("staged .part file never appeared")
		}
		if info, err := os.Stat(partPath); err == nil && info.Size() > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel() // the 64 MiB replay is still running
	cancel()

	select {
	case o := <-done:
		if !errors.Is(o.err, context.Canceled) {
			t.Fatalf("expected context.Canceled after cancellation, got %v", o.err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("ConvertFile did not return after cancellation")
	}
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Fatal("cancelled conversion left a staged .part file")
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatal("cancelled conversion published an output")
	}
	after, err := os.ReadFile(srcPath)
	if err != nil || !bytes.Equal(after, source.Bytes()) {
		t.Fatal("source bytes were modified by the killed conversion")
	}
}

func TestConvertFileEmptyPathsRejected(t *testing.T) {
	s, dir := convertFixture(t)
	if _, err := s.ConvertFile(context.Background(), ConvertRequest{OutputPath: filepath.Join(dir, "a.mp3")}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for missing source, got %v", err)
	}
	if _, err := s.ConvertFile(context.Background(), ConvertRequest{SourcePath: filepath.Join(dir, "a.ogg")}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for missing output, got %v", err)
	}
}

func TestConvertFileUnsupportedCodec(t *testing.T) {
	s, dir := convertFixture(t)
	srcPath := filepath.Join(dir, "song.ogg")
	if err := os.WriteFile(srcPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.SetProber(&fakeConvertProber{source: sourceTags("ogg", 1000, 160)})
	_, err := s.ConvertFile(context.Background(), ConvertRequest{
		SourcePath: srcPath,
		OutputPath: filepath.Join(dir, "song.wma"),
		Profile:    QualityProfile{Codec: "wma"},
	})
	var musicErr *Error
	if !errors.As(err, &musicErr) || musicErr.Code != "music_output_invalid" {
		t.Fatalf("expected music_output_invalid for unsupported codec, got %v", err)
	}
}

// --- Semaphore and timeout ---------------------------------------------------

func TestAcquireConvertSlotIdempotentRelease(t *testing.T) {
	slots := make(chan struct{}, 1)
	release, err := acquireConvertSlot(context.Background(), slots)
	if err != nil {
		t.Fatal(err)
	}
	// No free slot: a bounded wait must expire with the context error.
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer waitCancel()
	if _, err := acquireConvertSlot(waitCtx, slots); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded while saturated, got %v", err)
	}
	// Double release must be a single release: the slot is usable afterwards
	// and the channel is not underflowed.
	release()
	release()
	release2, err := acquireConvertSlot(context.Background(), slots)
	if err != nil {
		t.Fatalf("slot not reusable after double release: %v", err)
	}
	release2()
}

func TestConvertSlotsClampSize(t *testing.T) {
	if cap(resetConvertSlotsForTest(0)) != defaultMaxConversions {
		t.Fatal("non-positive size must fall back to the default")
	}
	if cap(resetConvertSlotsForTest(9)) != maxConvertConversions {
		t.Fatal("oversized size must be clamped to the preference cap")
	}
	if cap(resetConvertSlotsForTest(defaultMaxConversions)) != defaultMaxConversions {
		t.Fatal("default size must be honored")
	}
	resetConvertSlotsForTest(defaultMaxConversions)
}

func TestClampConvertTimeout(t *testing.T) {
	if clampConvertTimeout(0) != defaultConvertTimeout {
		t.Fatal("unset timeout must default to 15m")
	}
	if clampConvertTimeout(-time.Minute) != defaultConvertTimeout {
		t.Fatal("negative timeout must default to 15m")
	}
	if clampConvertTimeout(90*time.Minute) != maxConvertTimeout {
		t.Fatal("timeout must be capped at 60m")
	}
	if clampConvertTimeout(5*time.Minute) != 5*time.Minute {
		t.Fatal("a reasonable timeout must be kept")
	}
}

func TestRedactPaths(t *testing.T) {
	message := redactPaths("failed on C:\\stage\\song.mp3.part and /tmp/out.ogg", "C:\\stage\\song.mp3.part", "/tmp/out.ogg")
	if strings.Contains(message, "C:\\stage") || strings.Contains(message, "/tmp") {
		t.Fatalf("paths leaked into error text: %q", message)
	}
	if !strings.Contains(message, "[path]") {
		t.Fatalf("expected [path] markers: %q", message)
	}
}

// --- Opt-in live validation --------------------------------------------------

// The live test generates a real sine-wave WAV with the installed ffmpeg,
// converts it with the real engine and probes the results with ffprobe.
// Opt in with REELSCAREY_LIVE_MUSIC_TRANSCODE=1.
func TestLiveConvertFileSineWave(t *testing.T) {
	if os.Getenv("REELSCAREY_LIVE_MUSIC_TRANSCODE") == "" {
		t.Skip("REELSCAREY_LIVE_MUSIC_TRANSCODE not set")
	}
	resetTranscodeCacheForTest()
	if ffmpegPath() == "" {
		t.Skip("ffmpeg is not installed")
	}
	s, dir := convertFixture(t)
	srcPath := filepath.Join(dir, "sine.wav")
	generate := exec.Command(ffmpegPath(), "-y", "-f", "lavfi", "-i",
		"sine=frequency=440:duration=2", "-sample_fmt", "s16", "-ar", "44100",
		"-ac", "2", srcPath)
	if out, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("generating sine WAV: %v: %s", err, out)
	}

	cases := []struct {
		name    string
		profile QualityProfile
		family  string
	}{
		{"opus", mustProfile("opus-high"), "opus"},
		{"mp3", mustProfile("mp3-compatible"), "mp3"},
		{"flac", mustProfile("lossless-archive"), "flac"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outPath := filepath.Join(dir, "sine-"+tc.name+".out")
			result, err := s.ConvertFile(context.Background(), ConvertRequest{
				SourcePath: srcPath,
				OutputPath: outPath,
				Profile:    tc.profile,
			})
			if err != nil {
				t.Fatalf("ConvertFile: %v", err)
			}
			if result.SizeBytes <= 0 || result.SHA256 == "" {
				t.Fatalf("unverified result: %+v", result)
			}
			tags, err := FFprobeTagger{}.ProbeAudio(context.Background(), outPath)
			if err != nil {
				t.Fatalf("published output is not decodable: %v", err)
			}
			if probeFormatFamily(tags.Format) != tc.family {
				t.Fatalf("output format %q, want family %s", tags.Format, tc.family)
			}
			if tags.DurationMS < 1960 || tags.DurationMS > 2040 {
				t.Fatalf("output duration %d ms, want ~2000 ms", tags.DurationMS)
			}
			if info, err := os.Stat(outPath); err != nil || info.Size() != result.SizeBytes {
				t.Fatalf("published size mismatch: %v", err)
			}
		})
	}
}
