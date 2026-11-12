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
	return append(args, "pipe:1")
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

	cmd *exec.Cmd
}

// Close stops any conversion process and closes every open handle.
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

	stdout, err := newTranscodeStream(ctx, ffmpeg, native.File, codec, bitRate, timeOffset)
	if err != nil {
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
