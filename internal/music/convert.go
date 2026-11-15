package music

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/faaaaq/ReelScarey/internal/filecheck"
)

// This file implements the file-to-file transcoding engine behind music
// quality profiles: one staged source file is converted (or copied) into one
// staged output file and verified before the caller publishes it. The source
// is never modified or deleted; the output is staged at OutputPath+".part"
// and renamed into place only after the produced bytes pass validation.

const (
	// defaultConvertTimeout is used when ConvertRequest.Timeout is unset.
	defaultConvertTimeout = 15 * time.Minute
	// maxConvertTimeout is the hard cap regardless of what the caller asks.
	maxConvertTimeout = 60 * time.Minute
	// maxConvertConversions matches the preferences validation cap (1..4).
	maxConvertConversions = 4
	// convertStderrMax bounds captured ffmpeg stderr kept for error text.
	convertStderrMax = 64 << 10
	// convertFailureExcerpt bounds how much captured stderr is shown.
	convertFailureExcerpt = 400
)

// ConvertRequest describes one file-to-file conversion. Both paths are
// absolute; the caller owns staging and publishing, ConvertFile only owns the
// staged output path and its ".part" sibling.
type ConvertRequest struct {
	SourcePath string // absolute, already-staged input
	OutputPath string // absolute staged output; caller owns publishing
	Profile    QualityProfile
	Timeout    time.Duration // default 15m, hard cap 60m
}

// ConvertResult reports the verified published output. SHA256 and SizeBytes
// always describe the final file; Tags is the probe of the output.
type ConvertResult struct {
	DurationMS int
	SizeBytes  int64
	SHA256     string
	Tags       AudioTags
}

// convertCaps is the package-level conversion semaphore. Service cannot grow
// a field for it (the Service struct is shared with other feature owners),
// so every ConvertFile caller in every Service shares one lazily created
// channel sized from the persisted MaxConversions preference; the first
// conversion fixes the size and it is never replaced afterwards. This bounds
// ffmpeg file conversions exactly like transcodeSlots bounds stream
// conversions, without touching service.go.
var convertCaps struct {
	once  sync.Once
	slots chan struct{}
}

// convertSlots lazily creates the shared conversion semaphore. A non-positive
// size falls back to the default and any size above the preference cap is
// clamped, so a corrupt preference row cannot remove the bound.
func convertSlots(size int) chan struct{} {
	if size < 1 {
		size = defaultMaxConversions
	}
	if size > maxConvertConversions {
		size = maxConvertConversions
	}
	convertCaps.once.Do(func() {
		convertCaps.slots = make(chan struct{}, size)
	})
	return convertCaps.slots
}

// acquireConvertSlot reserves one conversion slot, waiting for capacity as
// long as the caller's context allows (a queued conversion job may wait; it
// must not fail with a busy error while slots are merely occupied). The
// returned release func is idempotent so a double release can never
// underflow the semaphore, mirroring acquireTranscodeSlot in service.go.
func acquireConvertSlot(ctx context.Context, slots chan struct{}) (func(), error) {
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	released := false
	return func() {
		if released {
			return
		}
		released = true
		<-slots
	}, nil
}

// convertSlotSize reads MaxConversions from the persisted preferences. Any
// read error falls back to the migration default so a broken preference row
// never disables the concurrency bound.
func (s *Service) convertSlotSize(ctx context.Context) int {
	prefs, err := s.GetPreferences(ctx)
	if err != nil {
		return defaultMaxConversions
	}
	return prefs.MaxConversions
}

// clampConvertTimeout applies the default and hard cap for one conversion.
func clampConvertTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return defaultConvertTimeout
	}
	if timeout > maxConvertTimeout {
		return maxConvertTimeout
	}
	return timeout
}

// probeFormatFamily maps an ffprobe format_name to one decodable audio
// family. It distinguishes m4a (mp4-family containers) from adts, because a
// raw ADTS stream copied into a file the caller treats as .m4a would not be
// the requested container. Unknown formats return "" so they always convert.
func probeFormatFamily(format string) string {
	format = strings.ToLower(strings.TrimSpace(format))
	switch {
	case strings.Contains(format, "mov"), strings.Contains(format, "mp4"),
		strings.Contains(format, "m4a"), strings.Contains(format, "mj2"),
		strings.Contains(format, "ipod"), strings.Contains(format, "3gp"),
		strings.Contains(format, "3g2"):
		return "m4a"
	}
	for _, part := range strings.Split(format, ",") {
		switch strings.TrimSpace(part) {
		case "mp3":
			return "mp3"
		case "flac":
			return "flac"
		case "ogg", "oga", "opus":
			return "opus"
		case "wav":
			return "wav"
		case "aac", "adts":
			return "adts"
		}
	}
	return ""
}

// profileTargetFamily is the ffprobe format family a profile's output must
// report. AAC defaults to the m4a container; only an explicit adts container
// targets raw ADTS.
func profileTargetFamily(profile QualityProfile) string {
	switch profile.Codec {
	case "mp3":
		return "mp3"
	case "flac":
		return "flac"
	case "opus":
		return "opus"
	case "aac":
		if profile.Container == "adts" || profile.Container == "aac" {
			return "adts"
		}
		return "m4a"
	}
	return ""
}

// sourceIsLossless reports whether the probed source family is lossless.
// ALAC inside an m4a container cannot be told apart from lossy AAC by
// format_name alone, so only FLAC and WAV are treated as lossless.
func sourceIsLossless(family string) bool {
	return family == "flac" || family == "wav"
}

// ConversionNeeded is the exported dry-run decision helper. A "not needed"
// verdict always means a plain file copy is a valid result: the source format
// family already equals the profile's target family and its quality is at or
// above the target (lossless source for a lossless profile, or a source
// bitrate at or above the requested bitrate for a lossy profile). In those
// cases re-encoding would only lose quality, so the output intentionally
// inherits the source quality.
func ConversionNeeded(source AudioTags, profile QualityProfile) (bool, string) {
	family := probeFormatFamily(source.Format)
	switch profile.Codec {
	case "original":
		return false, "This profile keeps the original file."
	case "flac":
		if family == "flac" {
			return false, "The source is already FLAC and is copied without re-encoding."
		}
		if profile.RequireLossless && !sourceIsLossless(family) {
			return true, "This profile requires a lossless source, but the file is lossy."
		}
		if sourceIsLossless(family) {
			return true, "The lossless source is re-packaged as FLAC without quality loss."
		}
		// Honest framing: transcoding lossy audio to FLAC never restores the
		// data the lossy encode already discarded; the output simply wraps
		// the source quality in a lossless container.
		return true, "The source is lossy: the FLAC output will inherit the source quality and does not restore lost detail."
	case "mp3", "aac", "opus":
		target := profileTargetFamily(profile)
		if family == target {
			if source.Bitrate > 0 && source.Bitrate >= profile.BitrateKbps {
				return false, fmt.Sprintf("The source is already %s at %d kbps, at or above the %d kbps target, so it is copied without re-encoding and the output inherits the source quality.", targetFamilyName(target), source.Bitrate, profile.BitrateKbps)
			}
			if source.Bitrate <= 0 {
				return true, fmt.Sprintf("The source bitrate is unknown, so the file is re-encoded to the %d kbps %s profile.", profile.BitrateKbps, profile.Codec)
			}
			return true, fmt.Sprintf("Re-encoding %d kbps %s audio to the %d kbps %s profile.", source.Bitrate, targetFamilyName(target), profile.BitrateKbps, profile.Codec)
		}
		if profile.RequireLossless && !sourceIsLossless(family) {
			return true, "This profile requires a lossless source, but the file is lossy."
		}
		if family == "" {
			return true, fmt.Sprintf("Converting this file to the %s profile; the output quality is bounded by the source.", profile.Name)
		}
		return true, fmt.Sprintf("Converting %s audio to the %s profile; the output quality is bounded by the source.", targetFamilyName(family), profile.Name)
	default:
		return true, "This profile uses an unsupported codec, so the file is left for review."
	}
}

// targetFamilyName turns a probe family into user-facing wording.
func targetFamilyName(family string) string {
	switch family {
	case "mp3":
		return "MP3"
	case "flac":
		return "FLAC"
	case "opus":
		return "Opus"
	case "m4a":
		return "AAC/M4A"
	case "adts":
		return "AAC"
	case "wav":
		return "WAV"
	}
	return "audio"
}

// mp3VBRQuality maps a kbps target onto libmp3lame's VBR -q:a scale
// (0 best .. 9 worst). Unmapped bitrates pick the nearest known setting.
func mp3VBRQuality(kbps int) int {
	table := []struct{ kbps, q int }{{320, 0}, {256, 1}, {192, 2}, {160, 3}, {128, 4}, {96, 5}, {64, 7}}
	best, bestDistance := table[len(table)-1].q, 1<<30
	for _, entry := range table {
		distance := entry.kbps - kbps
		if distance < 0 {
			distance = -distance
		}
		if distance < bestDistance {
			best, bestDistance = entry.q, distance
		}
	}
	return best
}

// convertArgs builds the ffmpeg argv for one file-to-file conversion. The
// output is a real file path (never a pipe) so the caller can rename it into
// place atomically; the source is a file path so ffmpeg can seek in it (an
// m4a input cannot be decoded from a non-seekable stdin stream).
//
// Tag policy: -map_metadata 0 copies the source global metadata (the stream
// endpoint pipeline strips tags with -map_metadata -1; a file conversion must
// preserve them instead).
//
// Artwork policy: MP3 and FLAC containers carry embedded attached pictures,
// so the (usually attached_pic) video stream is mapped through untouched
// with -c:v copy. Ogg/Opus (and ADTS) has no attached-picture support that
// survives real clients, so those outputs drop the artwork stream with -vn
// rather than producing a broken file; keep the original file (KeepOriginal)
// when the artwork matters.
//
// Bitrate policy: MP3 uses CBR -b:a when the profile is not VBR, otherwise
// libmp3lame VBR via -q:a (0 best .. 9 worst). The native AAC encoder has no
// stable VBR mode across ffmpeg versions, so aac profiles always use -b:a
// and the VBR flag is ignored. Opus is inherently VBR: -b:a is a ceiling per
// channel that libopus adapts around, and there is no CBR mode to select.
func convertArgs(profile QualityProfile, source, output string) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-i", source, "-sn", "-dn", "-map_metadata", "0"}
	switch profile.Codec {
	case "mp3":
		args = append(args, "-map", "0:a", "-map", "0:v?", "-c:v", "copy",
			"-disposition:v", "attached_pic", "-c:a", "libmp3lame")
		if profile.VBR {
			args = append(args, "-q:a", strconv.Itoa(mp3VBRQuality(profile.BitrateKbps)))
		} else {
			args = append(args, "-b:a", strconv.Itoa(profile.BitrateKbps)+"k")
		}
		args = append(args, "-f", "mp3", output)
	case "flac":
		args = append(args, "-map", "0:a", "-map", "0:v?", "-c:v", "copy",
			"-disposition:v", "attached_pic", "-c:a", "flac")
		if profile.CompressionLevel > 0 {
			args = append(args, "-compression_level", strconv.Itoa(profile.CompressionLevel))
		}
		args = append(args, "-f", "flac", output)
	case "aac":
		args = append(args, "-vn", "-c:a", "aac", "-b:a", strconv.Itoa(profile.BitrateKbps)+"k")
		if profile.Container == "adts" || profile.Container == "aac" {
			args = append(args, "-f", "adts", output)
		} else {
			// The ipod muxer writes the .m4a MP4-family container; faststart
			// moves the index to the front so players can start immediately.
			args = append(args, "-f", "ipod", "-movflags", "+faststart", output)
		}
	case "opus":
		args = append(args, "-vn", "-c:a", "libopus",
			"-b:a", strconv.Itoa(profile.BitrateKbps)+"k", "-f", "ogg", output)
	}
	return args
}

// convertStderrCapture captures bounded ffmpeg stderr for error text. Writes
// never fail: overflow is truncated instead of failing the exec copy
// goroutine, so a verbose ffmpeg run that still exits 0 cannot be reported
// as a conversion failure.
type convertStderrCapture struct {
	bytes.Buffer
	max int
}

func (c *convertStderrCapture) Write(p []byte) (int, error) {
	if room := c.max - c.Buffer.Len(); room > 0 {
		if len(p) > room {
			p = p[:room]
		}
		c.Buffer.Write(p)
	}
	return len(p), nil
}

// absolutePathPattern scrubs anything that still looks like an absolute path
// (Windows drive paths and Unix rooted paths) out of user-facing error text.
var absolutePathPattern = regexp.MustCompile(`(?:[A-Za-z]:)?(?:[\\/][^\s"']*)+`)

// redactPaths removes the known staging paths and any remaining absolute
// path pattern from ffmpeg stderr so no server filesystem layout leaks into
// user-facing messages.
func redactPaths(message string, paths ...string) string {
	for _, path := range paths {
		if path != "" {
			message = strings.ReplaceAll(message, path, "[path]")
		}
	}
	return absolutePathPattern.ReplaceAllString(message, "[path]")
}

// convertFailureMessage builds the user-facing message for a failed ffmpeg
// run: a bounded, path-free excerpt of its stderr.
func convertFailureMessage(stderr string, paths ...string) string {
	excerpt := strings.TrimSpace(redactPaths(stderr, paths...))
	if len(excerpt) > convertFailureExcerpt {
		excerpt = excerpt[:convertFailureExcerpt]
	}
	if excerpt == "" {
		return "The server could not convert this audio file."
	}
	return "The server could not convert this audio file: " + excerpt
}

// stageDigest digests a staged file at an absolute path outside the media
// root. It uses the filecheck digest with a stat-before/stat-after stability
// check (same discipline as musicDigest in rollback.go, which is bound to an
// os.Root inside mediaDir and therefore cannot be reused here).
func stageDigest(ctx context.Context, path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return "", 0, err
	}
	if !before.Mode().IsRegular() || before.Size() <= 0 {
		return "", 0, filecheck.ErrChanged
	}
	sha, err := filecheck.Digest(ctx, f)
	if err != nil {
		return "", 0, err
	}
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return "", 0, filecheck.ErrChanged
	}
	return sha, after.Size(), nil
}

// ConvertFile converts (or copies) one staged source file into the staged
// output path and verifies the result before renaming it into place. The
// source is never modified or deleted; on any failure the staged ".part"
// file is removed. The output path must not exist yet: the caller owns
// publishing and no-overwrite semantics.
func (s *Service) ConvertFile(ctx context.Context, req ConvertRequest) (ConvertResult, error) {
	if strings.TrimSpace(req.SourcePath) == "" || strings.TrimSpace(req.OutputPath) == "" {
		return ConvertResult{}, ErrInvalidInput
	}
	profile := req.Profile
	switch profile.Codec {
	case "original", "flac", "mp3", "aac", "opus":
	default:
		return ConvertResult{}, &Error{Code: "music_output_invalid", Message: "This quality profile uses an unsupported audio codec."}
	}
	if _, err := os.Stat(req.OutputPath); err == nil {
		return ConvertResult{}, fmt.Errorf("the conversion output already exists; refusing to overwrite it")
	}

	ctx, cancel := context.WithTimeout(ctx, clampConvertTimeout(req.Timeout))
	defer cancel()

	source, err := s.prober.ProbeAudio(ctx, req.SourcePath)
	if err != nil {
		return ConvertResult{}, err
	}
	sourceFamily := probeFormatFamily(source.Format)
	// A profile that demands lossless audio fails closed on a lossy source
	// before any work is done or any slot is taken.
	if profile.Codec != "original" && profile.RequireLossless && !sourceIsLossless(sourceFamily) {
		return ConvertResult{}, &Error{Code: "music_source_not_lossless", Message: "This profile requires a lossless source, but the file is lossy."}
	}
	needed, _ := ConversionNeeded(source, profile)
	if needed && ffmpegPath() == "" {
		return ConvertResult{}, &Error{Code: "music_ffmpeg_missing", Message: "FFmpeg is not installed on the server, so this file cannot be converted."}
	}

	release, err := acquireConvertSlot(ctx, convertSlots(s.convertSlotSize(ctx)))
	if err != nil {
		return ConvertResult{}, err
	}
	defer release()

	if err := os.MkdirAll(filepath.Dir(req.OutputPath), 0o755); err != nil {
		return ConvertResult{}, err
	}
	partPath := req.OutputPath + ".part"
	_ = os.Remove(partPath)

	// The source file handle is passed as cmd.Stdin: real ffmpeg never reads
	// it (-nostdin), but the hermetic fake ffmpeg used in tests (the test
	// binary itself, see transcode_cap_test.go TestMain) replays stdin to
	// stdout so the staged output holds deterministic non-empty bytes.
	srcFile, err := os.Open(req.SourcePath)
	if err != nil {
		return ConvertResult{}, &Error{Code: "music_transcode_failed", Message: "The source audio file could not be opened."}
	}
	defer srcFile.Close()

	var result ConvertResult
	if needed {
		result, err = s.runFFmpegConvert(ctx, profile, req.SourcePath, partPath, source, srcFile)
	} else {
		result, err = s.copyVerified(ctx, srcFile, partPath)
	}
	if err != nil {
		_ = os.Remove(partPath)
		return ConvertResult{}, err
	}
	if err := os.Rename(partPath, req.OutputPath); err != nil {
		_ = os.Remove(partPath)
		return ConvertResult{}, err
	}
	return result, nil
}

// runFFmpegConvert runs one bounded ffmpeg conversion into the staged part
// file and verifies the produced bytes. The context is bounded by
// CommandContext: when the caller's timeout expires, the child is killed
// explicitly and reaped by Wait, and the partial stage is removed by the
// caller.
func (s *Service) runFFmpegConvert(ctx context.Context, profile QualityProfile, sourcePath, partPath string, source AudioTags, srcFile *os.File) (ConvertResult, error) {
	ffmpeg := ffmpegPath()
	args := convertArgs(profile, sourcePath, partPath)
	part, err := os.OpenFile(partPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return ConvertResult{}, err
	}
	defer part.Close()

	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	cmd.Stdin = srcFile
	// ffmpeg writes the output file at the path argument itself; stdout stays
	// idle for real ffmpeg and carries the fake ffmpeg's bytes in tests. The
	// child is always started with an argv list, never through a shell.
	cmd.Stdout = part
	stderr := &convertStderrCapture{max: convertStderrMax}
	cmd.Stderr = stderr
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Start(); err != nil {
		return ConvertResult{}, &Error{Code: "music_ffmpeg_missing", Message: "FFmpeg could not be started on the server, so this file cannot be converted."}
	}
	waitErr := cmd.Wait()
	if waitErr != nil {
		// CommandContext has already killed the child when the context
		// expired; surface the context error so callers can distinguish
		// cancellation and timeout from a conversion failure.
		if ctx.Err() != nil {
			return ConvertResult{}, ctx.Err()
		}
		return ConvertResult{}, &Error{Code: "music_transcode_failed", Message: convertFailureMessage(stderr.String(), sourcePath, partPath)}
	}
	return s.verifyConvertOutput(ctx, partPath, source, profile)
}

// verifyConvertOutput validates the staged output: the probed codec family
// must match the profile, the duration must stay within 2% of a known source
// duration, and the file must be non-empty and stable while it is digested.
func (s *Service) verifyConvertOutput(ctx context.Context, partPath string, source AudioTags, profile QualityProfile) (ConvertResult, error) {
	tags, err := s.prober.ProbeAudio(ctx, partPath)
	if err != nil {
		return ConvertResult{}, &Error{Code: "music_output_invalid", Message: "The converted audio file is not decodable, so it was discarded."}
	}
	if probeFormatFamily(tags.Format) != profileTargetFamily(profile) {
		return ConvertResult{}, &Error{Code: "music_output_invalid", Message: "The converted audio file does not use the codec this profile requested, so it was discarded."}
	}
	if source.DurationMS > 0 {
		diff := tags.DurationMS - source.DurationMS
		if diff < 0 {
			diff = -diff
		}
		if tags.DurationMS <= 0 || diff*50 > source.DurationMS {
			return ConvertResult{}, &Error{Code: "music_output_invalid", Message: "The converted audio file duration does not match the source, so it was discarded."}
		}
	}
	sha, size, err := stageDigest(ctx, partPath)
	if err != nil {
		return ConvertResult{}, &Error{Code: "music_output_invalid", Message: "The converted audio file could not be verified, so it was discarded."}
	}
	return ConvertResult{DurationMS: tags.DurationMS, SizeBytes: size, SHA256: sha, Tags: tags}, nil
}

// copyVerified publishes a bit-exact copy of the source as the conversion
// result. ConversionNeeded decided the source already satisfies the profile
// (same codec family, quality at or above the target), so re-encoding would
// only lose quality: the output inherits the source quality by design.
func (s *Service) copyVerified(ctx context.Context, srcFile *os.File, partPath string) (ConvertResult, error) {
	part, err := os.OpenFile(partPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return ConvertResult{}, err
	}
	_, copyErr := io.Copy(part, musicReader{ctx, srcFile})
	closeErr := errors.Join(part.Sync(), part.Close())
	if err := errors.Join(copyErr, closeErr); err != nil {
		return ConvertResult{}, err
	}
	tags, err := s.prober.ProbeAudio(ctx, partPath)
	if err != nil {
		return ConvertResult{}, &Error{Code: "music_output_invalid", Message: "The copied audio file is not decodable, so it was discarded."}
	}
	sha, size, err := stageDigest(ctx, partPath)
	if err != nil {
		return ConvertResult{}, &Error{Code: "music_output_invalid", Message: "The copied audio file could not be verified, so it was discarded."}
	}
	return ConvertResult{DurationMS: tags.DurationMS, SizeBytes: size, SHA256: sha, Tags: tags}, nil
}
