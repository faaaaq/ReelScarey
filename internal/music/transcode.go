package music

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// This file makes the Subsonic stream endpoint playable on clients that cannot
// decode every container in the catalogue (iOS/Amperfy rejects WMA and WebM,
// and Android clients ask for a fixed output format). Registered files are
// never modified: ffmpeg converts a file into an MP3/FLAC/Opus stream on the
// fly, and the Subsonic song metadata advertises the format the stream
// endpoint will actually deliver.

// transcodeLookup and the resolver cache make ffmpeg detection cheap while
// still being swappable in tests.
var (
	transcodeLookup     = exec.LookPath
	transcodeMu         sync.Mutex
	transcodeResolved   bool
	transcodeExecutable string
)

func ffmpegPath() string {
	transcodeMu.Lock()
	defer transcodeMu.Unlock()
	if !transcodeResolved {
		transcodeResolved = true
		if found, err := transcodeLookup("ffmpeg"); err == nil {
			transcodeExecutable = found
		}
	}
	return transcodeExecutable
}

// resetTranscodeCacheForTest clears the cached ffmpeg lookup. Tests in this
// package use it so the outcome never depends on the build machine.
func resetTranscodeCacheForTest() {
	transcodeMu.Lock()
	transcodeResolved = false
	transcodeExecutable = ""
	transcodeMu.Unlock()
}

// clientPlayableSuffix reports whether iOS and Android Subsonic clients can
// decode a file with this suffix without help from the server.
func clientPlayableSuffix(suffix string) bool {
	switch strings.ToLower(strings.TrimSpace(suffix)) {
	case "mp3", "flac", "m4a", "mp4", "aac", "opus", "ogg", "oga", "wav", "aif", "aiff":
		return true
	}
	return false
}

// serverSuffix is the Subsonic "suffix" attribute for a registered file. Files
// the clients cannot decode are advertised as the MP3 stream that
// OpenPlayback converts them to, so clients do not refuse to attempt playback.
func serverSuffix(format, name string) string {
	suffix := audioSuffix(format, name)
	if suffix == "" || clientPlayableSuffix(suffix) || ffmpegPath() == "" {
		return suffix
	}
	return "mp3"
}

// serverContentType is the Subsonic "contentType" attribute matching
// serverSuffix. Amperfy marks a song unplayable when this is
// application/octet-stream or audio/x-ms-wma, so it must agree with the bytes
// the stream endpoint will serve.
func serverContentType(format, name string) string {
	suffix := serverSuffix(format, name)
	if suffix == "" {
		return "application/octet-stream"
	}
	return audioContentType(suffix, "stream."+suffix)
}

// audioFamily groups suffixes that share one decodable family so a client that
// asks for AAC is not handed needless conversion for its own M4A file.
func audioFamily(suffix string) string {
	switch strings.ToLower(strings.TrimSpace(suffix)) {
	case "mp3":
		return "mp3"
	case "flac":
		return "flac"
	case "m4a", "mp4", "aac":
		return "aac"
	case "ogg", "oga", "opus":
		return "opus"
	case "wav", "aif", "aiff":
		return "wav"
	}
	return ""
}

// outputCodec normalizes a client-requested format to a supported output codec.
func outputCodec(requested string) string {
	switch strings.ToLower(strings.TrimSpace(requested)) {
	case "mp3", "flac", "aac", "opus", "ogg", "wav":
		return strings.ToLower(strings.TrimSpace(requested))
	case "m4a", "mp4", "alac":
		return "aac"
	case "oga":
		return "opus"
	case "aif", "aiff":
		return "wav"
	}
	return ""
}

func clampBitRate(bitRate int) int {
	if bitRate <= 0 {
		return 192
	}
	if bitRate < 32 {
		return 32
	}
	if bitRate > 320 {
		return 320
	}
	return bitRate
}

// DefaultStreamingQuality is the effective quality when neither the account
// nor the global preferences name one. It matches the migration default for
// music_preferences.streaming_quality.
const DefaultStreamingQuality = defaultStreamingQuality

// StreamingQualityParams maps a streaming quality name to the default
// (format, maxBitRate) the conversion planner receives. "original" returns an
// empty format so playable files are served byte-for-byte (the compatibility
// fallback still converts containers clients cannot decode). "auto" behaves
// like balanced for now: client-reported connection information (available
// bandwidth) is not reliably available server-side, so there is nothing
// honest to adapt to and no bandwidth detection is fabricated here.
func StreamingQualityParams(quality string) (format string, maxBitRate int) {
	switch strings.TrimSpace(quality) {
	case "data-saver":
		return "opus", 64
	case "high":
		return "opus", 128
	case "premium":
		return "opus", 160
	case "original":
		return "", 0
	case "auto", "balanced":
		return "opus", 96
	default:
		// Unknown or empty values fall back to the built-in default quality
		// instead of failing the stream.
		return "opus", 96
	}
}

// ResolveStreamQuality picks the effective streaming quality name:
// an explicit per-server-account choice wins, then the global preference.
// An empty per-user value means "follow global"; an empty global value falls
// back to the built-in default.
func ResolveStreamQuality(user, global string) string {
	user = strings.TrimSpace(user)
	if user != "" {
		return user
	}
	global = strings.TrimSpace(global)
	if global != "" {
		return global
	}
	return DefaultStreamingQuality
}

// planTranscode decides whether a stream request needs conversion and into
// which codec. The codec is empty when the registered file can be served
// unchanged.
func planTranscode(suffix, requested string, maxBitRate int) (codec string, bitRate int) {
	requested = strings.ToLower(strings.TrimSpace(requested))
	if requested == "raw" {
		requested = ""
	}
	if !clientPlayableSuffix(suffix) {
		if out := outputCodec(requested); out != "" {
			return out, bitRateFor(out, maxBitRate)
		}
		return "mp3", bitRateFor("mp3", maxBitRate)
	}
	if requested == "" {
		return "", 0
	}
	out := outputCodec(requested)
	if out == "" || audioFamily(out) == audioFamily(suffix) {
		return "", 0
	}
	return out, bitRateFor(out, maxBitRate)
}

// bitRateFor reports the output bit rate; lossless output ignores it.
func bitRateFor(codec string, maxBitRate int) int {
	if codec == "flac" || codec == "wav" {
		return 0
	}
	return clampBitRate(maxBitRate)
}

func transcodeContentType(codec string) string {
	switch codec {
	case "mp3":
		return "audio/mpeg"
	case "flac":
		return "audio/flac"
	case "aac":
		return "audio/aac"
	case "opus", "ogg":
		return "audio/ogg"
	case "wav":
		return "audio/wav"
	}
	return "application/octet-stream"
}

// transcodeArgs builds the ffmpeg command line. Audio is read from stdin and
// written to stdout so a registered path never leaves the server process.
func transcodeArgs(codec string, bitRate, timeOffset int) []string {
	return append(transcodeCodecArgs(codec, bitRate, timeOffset), "pipe:1")
}

// transcodeFileArgs is the file-output variant used by the stream cache: the
// encoded audio is written to dest (the .part staging path inside the cache
// dir) instead of stdout. The dest path is the only path in the arguments and
// stays inside the cache dir; the source still travels on stdin. -y makes
// ffmpeg overwrite a stale .part file left by an interrupted encode instead
// of prompting on the piped input.
func transcodeFileArgs(codec string, bitRate, timeOffset int, dest string) []string {
	return append(append([]string{"-y"}, transcodeCodecArgs(codec, bitRate, timeOffset)...), dest)
}

// transcodeCodecArgs builds the shared conversion options: input on stdin,
// one audio stream, no metadata, the requested codec and an unbound output.
func transcodeCodecArgs(codec string, bitRate, timeOffset int) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-i", "pipe:0"}
	if timeOffset > 0 {
		// Output seeking keeps working for non-seekable pipe input; ffmpeg
		// decodes and discards up to the requested offset.
		args = append(args, "-ss", strconv.Itoa(timeOffset))
	}
	args = append(args, "-vn", "-sn", "-dn", "-map_metadata", "-1")
	switch codec {
	case "mp3":
		args = append(args, "-c:a", "libmp3lame", "-b:a", strconv.Itoa(bitRate)+"k", "-f", "mp3")
	case "flac":
		args = append(args, "-c:a", "flac", "-f", "flac")
	case "aac":
		args = append(args, "-c:a", "aac", "-b:a", strconv.Itoa(bitRate)+"k", "-f", "adts")
	case "opus":
		args = append(args, "-c:a", "libopus", "-b:a", strconv.Itoa(bitRate)+"k", "-f", "ogg")
	case "ogg":
		args = append(args, "-c:a", "libvorbis", "-q:a", "5", "-f", "ogg")
	case "wav":
		args = append(args, "-c:a", "pcm_s16le", "-f", "wav")
	}
	return args
}

// PlaybackStream is one audio response for a client. Body is set when ffmpeg is
// converting the file; File is set when the registered file is served as-is.
// Close releases both the process and the underlying file.
type PlaybackStream struct {
	Name        string
	ContentType string
	Size        int64
	ModTime     time.Time
	File        *os.File
	Body        io.ReadCloser
	Codec       string

	cmd     *exec.Cmd
	release func()
}

// Close stops any conversion process, closes every open handle and releases
// the bounded transcode slot exactly once.
func (p *PlaybackStream) Close() {
	if p.Body != nil {
		_ = p.Body.Close()
	}
	if p.cmd != nil {
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		_ = p.cmd.Wait()
	}
	if p.File != nil {
		_ = p.File.Close()
	}
	if p.release != nil {
		p.release()
	}
}

// OpenPlayback opens a registered music file for a Subsonic stream request,
// converting it when the client asked for another format or when the native
// container is one iOS/Android clients cannot decode. Conversion runs for as
// long as the client keeps reading; the caller must Close the result.
func (s *Service) OpenPlayback(ctx context.Context, id int64, requestedFormat string, maxBitRate, timeOffset int) (PlaybackStream, error) {
	native, err := s.OpenAudioFile(ctx, id)
	if err != nil {
		return PlaybackStream{}, err
	}
	suffix := audioSuffix("", native.Name)
	codec, bitRate := planTranscode(suffix, requestedFormat, maxBitRate)
	ffmpeg := ffmpegPath()
	if codec == "" || ffmpeg == "" {
		return PlaybackStream{
			Name:        native.Name,
			ContentType: native.ContentType,
			Size:        native.Size,
			ModTime:     native.ModTime,
			File:        native.File,
			Codec:       suffix,
		}, nil
	}

	// Stream cache: a full-file conversion (no offset seek) is served from
	// the persistent cache when possible, encoding into it on a miss. The
	// cache is strictly an optimization: any cache failure (miss with a
	// failed encode, unreadable cache dir, database error) falls through to
	// the on-the-fly pipe below so the stream endpoint never fails because
	// the cache failed. Offset requests stay on the pipe with output
	// seeking: a cached encode covers only the whole track, and seeking into
	// a re-encoded ogg/opus container would need timestamp-aware range math
	// the existing decode-and-discard path already gets right, so a seek is
	// deliberately never answered from the cache.
	if timeOffset == 0 {
		if cached, info, _, err := s.OpenCachedStream(ctx, id, codec, bitRate, native.SHA256,
			s.cacheEncodeFunc(ctx, ffmpeg, native.File, codec, bitRate)); err == nil {
			// The registered source file is no longer needed once the cache
			// owns the bytes the client will read.
			_ = native.File.Close()
			return PlaybackStream{
				Name:        transcodedName(native.Name, codec),
				ContentType: transcodeContentType(codec),
				Size:        info.Size(),
				ModTime:     info.ModTime(),
				File:        cached,
				Codec:       codec,
			}, nil
		}
	}

	// NEW-A1: bound simultaneous ffmpeg conversions. The slot is taken only
	// when a conversion will actually start; a failed spawn or a closed
	// stream always releases it.
	release, err := s.acquireTranscodeSlot(ctx)
	if err != nil {
		_ = native.File.Close()
		return PlaybackStream{}, err
	}
	stdout, err := newTranscodeStream(ctx, ffmpeg, native.File, codec, bitRate, timeOffset)
	if err != nil {
		release()
		_ = native.File.Close()
		return PlaybackStream{}, &Error{Code: "music_transcode_failed", Message: "The server could not convert this file for playback."}
	}
	return PlaybackStream{
		Name:        transcodedName(native.Name, codec),
		ContentType: transcodeContentType(codec),
		File:        native.File,
		Body:        stdout.body,
		Codec:       codec,
		cmd:         stdout.cmd,
		release:     release,
	}, nil
}

// transcodedName renames the served file so clients see the output format. mp3
// and aac streams get an explicit extension; lossless outputs keep the source
// name because their container still matches the client's expectation.
func transcodedName(name, codec string) string {
	base := name
	if i := strings.LastIndexAny(base, "./\\"); i >= 0 && strings.ContainsAny(base[i:], ".") {
		base = strings.TrimSuffix(base, base[i:])
	}
	switch codec {
	case "mp3":
		return base + ".mp3"
	case "aac":
		return base + ".aac"
	case "opus", "ogg":
		return base + ".ogg"
	case "flac":
		return base + ".flac"
	case "wav":
		return base + ".wav"
	}
	return name
}

// cacheEncodeFunc wraps the pipe-based ffmpeg conversion into the encode func
// the stream cache expects: the registered file stays on stdin, ffmpeg writes
// the encoded audio to the dest staging path with file-output arguments, and
// the process is awaited before the cache publishes the file. The conversion
// takes one of the same bounded transcode slots as on-the-fly piping so
// cache encodes can never escape the server-wide concurrency limit; a busy
// slot fails the encode, which the caller falls back to on-the-fly piping.
func (s *Service) cacheEncodeFunc(ctx context.Context, ffmpeg string, input *os.File, codec string, bitRate int) func(context.Context, string) error {
	return func(ctx context.Context, dest string) error {
		release, err := s.acquireTranscodeSlot(ctx)
		if err != nil {
			return err
		}
		defer release()
		// Cache encodes are always full-file: OpenPlayback only routes
		// timeOffset==0 requests into the cache, so no output seek applies.
		return runTranscodeFile(ctx, ffmpeg, input, codec, bitRate, 0, dest)
	}
}
