package music

import (
	"os/exec"
	"strings"
	"testing"
)

// withoutFFmpeg pins ffmpeg detection off so a test result never depends on
// whether the build machine has ffmpeg installed.
func withoutFFmpeg(t *testing.T) {
	t.Helper()
	transcodeLookup = func(string) (string, error) { return "", exec.ErrNotFound }
	resetTranscodeCacheForTest()
	t.Cleanup(func() {
		transcodeLookup = exec.LookPath
		resetTranscodeCacheForTest()
	})
}

// withFFmpeg pins ffmpeg detection on so conversion planning is exercised the
// way it runs in the supported Docker image.
func withFFmpeg(t *testing.T) {
	t.Helper()
	transcodeLookup = func(string) (string, error) { return "/usr/bin/ffmpeg", nil }
	resetTranscodeCacheForTest()
	t.Cleanup(func() {
		transcodeLookup = exec.LookPath
		resetTranscodeCacheForTest()
	})
}

// planTranscode decides between serving a registered file untouched and
// converting it. Getting this wrong is exactly what made Amperfy refuse to
// play: WMA and WebM cannot be decoded by iOS, and a client that asks for MP3
// must not be handed the original FLAC.
func TestPlanTranscode(t *testing.T) {
	cases := []struct {
		name       string
		suffix     string
		requested  string
		maxBitRate int
		wantCodec  string
		wantRate   int
	}{
		{name: "native mp3 untouched", suffix: "mp3", wantCodec: ""},
		{name: "native flac untouched", suffix: "flac", wantCodec: ""},
		{name: "flac to mp3", suffix: "flac", requested: "mp3", maxBitRate: 320, wantCodec: "mp3", wantRate: 320},
		{name: "flac to mp3 default bitrate", suffix: "flac", requested: "mp3", wantCodec: "mp3", wantRate: 192},
		{name: "flac to mp3 clamps bitrate", suffix: "flac", requested: "mp3", maxBitRate: 9999, wantCodec: "mp3", wantRate: 320},
		{name: "raw leaves flac alone", suffix: "flac", requested: "raw"},
		{name: "m4a request on m4a file", suffix: "m4a", requested: "m4a"},
		{name: "aac request on m4a file", suffix: "m4a", requested: "aac"},
		{name: "mp3 request on m4a file", suffix: "m4a", requested: "mp3", maxBitRate: 128, wantCodec: "mp3", wantRate: 128},
		{name: "wma always converts", suffix: "wma", wantCodec: "mp3", wantRate: 192},
		{name: "wma honours mp3 request", suffix: "wma", requested: "mp3", maxBitRate: 128, wantCodec: "mp3", wantRate: 128},
		{name: "wma honours flac request", suffix: "wma", requested: "flac", wantCodec: "flac"},
		{name: "webm always converts", suffix: "webm", wantCodec: "mp3", wantRate: 192},
		{name: "unknown container converts", suffix: "", requested: "mp3", wantCodec: "mp3", wantRate: 192},
		{name: "unsupported request keeps the native file", suffix: "flac", requested: "wavpack"},
		{name: "opus native untouched", suffix: "opus", requested: "raw"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			codec, rate := planTranscode(tc.suffix, tc.requested, tc.maxBitRate)
			if codec != tc.wantCodec {
				t.Fatalf("planTranscode(%q, %q, %d) codec = %q, want %q", tc.suffix, tc.requested, tc.maxBitRate, codec, tc.wantCodec)
			}
			if codec != "" && rate != tc.wantRate {
				t.Fatalf("planTranscode(%q, %q, %d) bit rate = %d, want %d", tc.suffix, tc.requested, tc.maxBitRate, rate, tc.wantRate)
			}
		})
	}
}

// The Subsonic song metadata is what clients use to decide whether a song can
// be played at all, so a file the server converts must be advertised as the
// format that will actually be delivered, and an unplayable container must
// never reach a client as audio/x-ms-wma or application/octet-stream.
func TestServerSuffixAdvertisesConversionTarget(t *testing.T) {
	withFFmpeg(t)
	cases := []struct {
		format, path, wantSuffix, wantType string
	}{
		{"asf", "Music/XG/DEPENDS/01 DEPENDS.wma", "mp3", "audio/mpeg"},
		{"matroska,webm", "Music/XG/DEPENDS/01 DEPENDS.webm", "mp3", "audio/mpeg"},
		{"flac", "Music/XG/DEPENDS/01 DEPENDS.flac", "flac", "audio/flac"},
		{"mp3", "Music/XG/DEPENDS/01 DEPENDS.mp3", "mp3", "audio/mpeg"},
		{"mov,mp4,m4a,3gp,3g2,mj2", "Music/XG/DEPENDS/01 DEPENDS.m4a", "m4a", "audio/mp4"},
	}
	for _, tc := range cases {
		if got := serverSuffix(tc.format, tc.path); got != tc.wantSuffix {
			t.Fatalf("serverSuffix(%q, %q) = %q, want %q", tc.format, tc.path, got, tc.wantSuffix)
		}
		if got := serverContentType(tc.format, tc.path); got != tc.wantType {
			t.Fatalf("serverContentType(%q, %q) = %q, want %q", tc.format, tc.path, got, tc.wantType)
		}
	}
}

// Without a working ffmpeg the metadata must stay honest about the real file
// instead of promising an MP3 stream the server cannot produce.
func TestServerSuffixWithoutFFmpegKeepsRealFormat(t *testing.T) {
	withoutFFmpeg(t)
	if got := serverSuffix("asf", "Music/XG/DEPENDS/01 DEPENDS.wma"); got != "wma" {
		t.Fatalf("serverSuffix without ffmpeg = %q, want wma", got)
	}
	if got := serverContentType("asf", "Music/XG/DEPENDS/01 DEPENDS.wma"); got != "audio/x-ms-wma" {
		t.Fatalf("serverContentType without ffmpeg = %q, want audio/x-ms-wma", got)
	}
}

// transcodeArgs must always strip video/subtitle streams (covers and music
// videos in a WebM container) and must never pass a local path to ffmpeg.
func TestTranscodeArgs(t *testing.T) {
	args := strings.Join(transcodeArgs("mp3", 128, 30), " ")
	for _, want := range []string{"-i pipe:0", "-ss 30", "-vn", "-sn", "-dn", "-c:a libmp3lame", "-b:a 128k", "-f mp3", "pipe:1"} {
		if !strings.Contains(args, want) {
			t.Fatalf("mp3 transcode args missing %q: %s", want, args)
		}
	}
	if strings.Contains(args, ".flac") || strings.Contains(args, "Music/") {
		t.Fatalf("transcode args must not contain a local path: %s", args)
	}
	flacArgs := strings.Join(transcodeArgs("flac", 320, 0), " ")
	if !strings.Contains(flacArgs, "-c:a flac") || strings.Contains(flacArgs, "-ss") {
		t.Fatalf("flac transcode args wrong: %s", flacArgs)
	}
}
