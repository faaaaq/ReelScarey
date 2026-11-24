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
	// maxBitRate is a ceiling, not a request to raise a small value to the
	// server's preferred minimum. FFmpeg remains responsible for rejecting a
	// codec/bitrate combination it cannot encode; silently returning 32k for a
	// 16k request would violate the client's cap before FFmpeg even runs.
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
	return planTranscodeWithSource(suffix, requested, maxBitRate, 0)
}

// planTranscodeWithSource is planTranscode with optional source bitrate
// evidence in kbit/s. Unknown or invalid evidence is zero and deliberately
// takes the conservative path. A known lossy source already at or below the
// requested cap needs no same-family re-encode.
func planTranscodeWithSource(suffix, requested string, maxBitRate, sourceBitRate int) (codec string, bitRate int) {
	requested = strings.ToLower(strings.TrimSpace(requested))
	raw := requested == "raw"
	if raw {
		// Raw explicitly opts out of requested-format and bitrate conversion.
		// Preserve the existing compatibility fallback for containers clients
		// cannot decode at all; a playable native file is always passthrough.
		if clientPlayableSuffix(suffix) {
			return "", 0
		}
		requested = ""
	}
	if !clientPlayableSuffix(suffix) {
		if out := outputCodec(requested); out != "" {
			return out, bitRateFor(out, maxBitRate)
		}
		return "mp3", bitRateFor("mp3", maxBitRate)
	}
	lossySourceWithinCap := func(out string) bool {
		return maxBitRate > 0 && sourceBitRate > 0 && sourceBitRate <= maxBitRate &&
			out != "flac" && out != "wav" && audioFamily(out) == audioFamily(suffix)
	}
	if requested == "" {
		// A cap is meaningful even without a format parameter. Without source
		// bitrate evidence, re-encode a lossy native stream conservatively
		// instead of claiming the original already satisfies the cap.
		out := outputCodec(suffix)
		if maxBitRate > 0 && out != "" && out != "flac" && out != "wav" {
			if lossySourceWithinCap(out) {
				return "", 0
			}
			return out, bitRateFor(out, maxBitRate)
		}
		return "", 0
	}
	out := outputCodec(requested)
	if out == "" {
		return "", 0
	}
	if audioFamily(out) == audioFamily(suffix) {
		if maxBitRate > 0 && out != "flac" && out != "wav" && !lossySourceWithinCap(out) {
			return out, bitRateFor(out, maxBitRate)
		}
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
// stays inside the cache dir; the source still travels on stdin. -hide_banner
// stays the leading argument, matching the pipe variant, so the hermetic test
// helper that re-executes the test binary as "ffmpeg" sees the same first
// flag on both paths. -y makes ffmpeg overwrite a stale .part file left by an
// interrupted encode instead of prompting on the piped input.
func transcodeFileArgs(codec string, bitRate, timeOffset int, dest string) []string {
	return append(transcodeCodecArgs(codec, bitRate, timeOffset), "-y", dest)
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
		// Vorbis is still a capped lossy output. A quality-only argument lets
		// the encoder exceed maxBitRate, so pass the same effective ceiling as
		// the other lossy encoders instead of silently ignoring it.
		args = append(args, "-c:a", "libvorbis", "-b:a", strconv.Itoa(bitRate)+"k", "-f", "ogg")
	case "wav":
		args = append(args, "-c:a", "pcm_s16le", "-f", "wav")
	}
	return args
}

// NormalizeStreamFormat validates a client-supplied stream format and returns
// the canonical output codec, or "" when the value is not a streamable format.
// It is exported for the HTTP layer so browser capability negotiation can
// request a compatible container without reaching into ffmpeg internals.
func NormalizeStreamFormat(requested string) string {
	return outputCodec(requested)
}

// PlaybackStream is one audio response for a client. Body is set when ffmpeg is
// converting the file; File is set when the registered file is served as-is.
// Close releases both the process and the underlying file.
type PlaybackStream struct {
	Name            string
	ContentType     string
	Size            int64
	ModTime         time.Time
	File            *os.File
	Body            io.ReadCloser
	Codec           string
	BitrateKbps     int
	Mode            string
	Reason          string
	RequestedFormat string

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
	codec, bitRate := planTranscodeWithSource(suffix, requestedFormat, maxBitRate, native.Bitrate)
	if timeOffset > 0 && codec == "" {
		// A byte-range direct stream starts at byte zero; it cannot honor a
		// media-time offset. Decode same-format input through the existing
		// output-seek path instead of silently returning the whole track.
		codec = outputCodec(suffix)
		bitRate = bitRateFor(codec, maxBitRate)
	}
	ffmpeg := ffmpegPath()
	if timeOffset > 0 && ffmpeg == "" {
		_ = native.File.Close()
		return PlaybackStream{}, &Error{Code: "music_transcode_unavailable", Message: "Seeking during playback requires FFmpeg on the server."}
	}
	if codec == "" || ffmpeg == "" {
		s.playbackDirect.Add(1)
		reason := "The registered source format was accepted for direct play."
		if codec != "" && ffmpeg == "" {
			reason = "FFmpeg is unavailable; the registered source is being served directly instead of the requested conversion."
		}
		return PlaybackStream{
			Name:            native.Name,
			ContentType:     native.ContentType,
			Size:            native.Size,
			ModTime:         native.ModTime,
			File:            native.File,
			Codec:           suffix,
			BitrateKbps:     native.Bitrate,
			Mode:            "direct",
			Reason:          reason,
			RequestedFormat: requestedFormat,
		}, nil
	}

	// Full-file cache hits stay synchronous regular files so the HTTP layer
	// retains normal range support. A miss is deliberately hit-only: waiting
	// for a complete cache encode before returning used to delay the first
	// audio byte by an entire track. The live pipe below starts immediately;
	// once it is running, a detached deduplicated fill repopulates the cache.
	// Offset requests never use the whole-track cache and retain output seek.
	if timeOffset == 0 {
		cacheMode := ""
		if cached, info, _, err := s.OpenCachedStream(ctx, id, codec, bitRate, native.SHA256, nil, &cacheMode); err == nil {
			_ = native.File.Close()
			return PlaybackStream{
				Name:            transcodedName(native.Name, codec),
				ContentType:     transcodeContentType(codec),
				Size:            info.Size(),
				ModTime:         info.ModTime(),
				File:            cached,
				Codec:           codec,
				BitrateKbps:     bitRate,
				Mode:            cacheMode,
				Reason:          "Full-file conversion served from the persistent transcode cache.",
				RequestedFormat: requestedFormat,
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
	mode := "live-miss"
	reason := "The conversion was not cached; streaming it live while a detached cache fill proceeds."
	if timeOffset > 0 {
		mode = "offset-live"
		s.playbackOffset.Add(1)
		reason = "A time offset was requested; decoding the registered source from that offset."
	} else {
		s.playbackLive.Add(1)
		// Schedule only after the live encoder owns its slot and its source.
		// The fill opens fileID again and therefore never shares native.File,
		// its offset, or the request's cancellation lifecycle.
		s.startStreamCacheFill(id, codec, bitRate, native.SHA256, ffmpeg)
	}
	return PlaybackStream{
		Name:            transcodedName(native.Name, codec),
		ContentType:     transcodeContentType(codec),
		File:            native.File,
		Body:            stdout.body,
		Codec:           codec,
		BitrateKbps:     bitRate,
		Mode:            mode,
		Reason:          reason,
		RequestedFormat: requestedFormat,
		cmd:             stdout.cmd,
		release:         release,
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
		// Cache encodes are always full-file; offset requests never enter the
		// cache. ctx belongs to the fill, never to the disconnected listener.
		return runTranscodeFile(ctx, ffmpeg, input, codec, bitRate, 0, dest)
	}
}

// Detached fills are an optimization, not an unbounded work queue. Distinct
// cold keys may each have one fill; excess keys simply remain live-only until
// a later request retries. Every running/waiting fill also has a hard lifetime.
const maxDetachedStreamCacheFills = 32

var streamCacheFillTimeout = 30 * time.Minute

// startStreamCacheFill schedules one service-owned cache encode for a key.
// It returns false when the key is already running, the bounded fill set is
// full, or the service is shutting down. Live playback never depends on the
// result. The source is opened inside the goroutine with the detached context.
func (s *Service) startStreamCacheFill(fileID int64, codec string, bitRate int, sourceSHA, ffmpeg string) bool {
	profileKey, valid := cacheProfileKey(codec, bitRate)
	if fileID < 1 || !valid || ffmpeg == "" {
		return false
	}
	key := cacheRelativePath(fileID, profileKey)

	s.streamFillMu.Lock()
	if s.streamFillClosed || len(s.streamFills) >= maxDetachedStreamCacheFills {
		s.streamFillMu.Unlock()
		return false
	}
	if _, running := s.streamFills[key]; running {
		s.streamFillMu.Unlock()
		return false
	}
	if s.streamFillCtx == nil {
		s.streamFillCtx, s.streamFillCancel = context.WithCancel(context.Background())
	}
	if s.streamFills == nil {
		s.streamFills = make(map[string]struct{})
	}
	s.streamFills[key] = struct{}{}
	fillContext := s.streamFillCtx
	s.streamFillWG.Add(1)
	s.streamFillMu.Unlock()

	go func() {
		defer func() {
			s.streamFillMu.Lock()
			delete(s.streamFills, key)
			s.streamFillMu.Unlock()
			s.streamFillWG.Done()
		}()
		ctx, cancel := context.WithTimeout(fillContext, streamCacheFillTimeout)
		defer cancel()

		// This handle has an independent open file description and lifetime.
		// In particular, a live listener reading or closing its own handle can
		// neither move this encoder's offset nor EOF its stdin.
		source, err := s.OpenAudioFile(ctx, fileID)
		if err != nil {
			return
		}
		defer source.File.Close()
		if sourceSHA != "" && source.SHA256 != sourceSHA {
			// The catalog source changed after the cache-miss decision. Do not
			// publish bytes under identity evidence from a different request.
			return
		}
		cached, _, _, cacheErr := s.OpenCachedStream(ctx, fileID, codec, bitRate, source.SHA256,
			s.cacheEncodeFunc(ctx, ffmpeg, source.File, codec, bitRate))
		if cached != nil {
			_ = cached.Close()
		}
		if cacheErr != nil && ctx.Err() == nil {
			s.streamFillFailed.Add(1)
		}
	}()
	return true
}

// Close stops detached cache fills, waits for their FFmpeg processes and
// staging cleanup, and prevents new fills. It is idempotent and does not close
// the catalog database, whose lifetime belongs to the application composition
// root. Live streams remain request-owned and are closed by their callers.
func (s *Service) Close() error {
	s.streamFillMu.Lock()
	if !s.streamFillClosed {
		s.streamFillClosed = true
		if s.streamFillCancel != nil {
			s.streamFillCancel()
		}
	}
	s.streamFillMu.Unlock()
	s.streamFillWG.Wait()
	return nil
}
