package transfer

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Probe struct {
	Subtitles []SubtitleStream `json:"subtitle_streams"`
	Audio     []AudioStream    `json:"audio_streams"`
	Title     *string          `json:"embedded_title"`
	Codec     string           `json:"codec"`
	Width     int              `json:"width"`
	Height    int              `json:"height"`
	Duration  float64          `json:"duration_seconds"`
	// HDR records the video stream's transfer characteristics: true when the
	// stream declares HDR10 (SMPTE ST 2084) or HLG, false when it declares
	// anything else or nothing. Recorded for every newly captured probe.
	HDR bool `json:"hdr"`
}
type AudioStream struct {
	Codec    string `json:"codec"`
	Language string `json:"language"`
	Title    string `json:"title"`
}
type SubtitleStream struct {
	Language        string `json:"language"`
	Title           string `json:"title"`
	Forced          bool   `json:"forced"`
	HearingImpaired bool   `json:"hearing_impaired"`
}
type Prober interface {
	Probe(context.Context, *os.File) (Probe, error)
}
type FFprobe struct{}
type limitedBuffer struct {
	bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.max-b.Len() {
		return 0, io.ErrShortWrite
	}
	return b.Buffer.Write(p)
}
func (FFprobe) Probe(ctx context.Context, file *os.File) (Probe, error) {
	if _, e := file.Seek(0, 0); e != nil {
		return Probe{}, e
	}
	defer file.Seek(0, 0)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// REELSCAREY_FFPROBE_PATH names the ffprobe executable, matching the
	// media-analysis subsystem (RS-0012). Imports and System checks must use
	// the same binary, otherwise diagnostics contradict actual behavior.
	binary := os.Getenv("REELSCAREY_FFPROBE_PATH")
	if strings.TrimSpace(binary) == "" {
		binary = "ffprobe"
	}
	cmd := exec.CommandContext(ctx, binary, "-v", "error", "-protocol_whitelist", probeProtocols(), "-format_whitelist", "mov,matroska,webm,avi,mpegts", "-enable_drefs", "0", "-use_absolute_path", "0", "-probesize", "16777216", "-analyzeduration", "10000000", "-show_entries", "format=duration:format_tags=title:stream=codec_type,codec_name,width,height,color_transfer:stream_disposition=attached_pic,forced,hearing_impaired:stream_tags=language,title", "-of", "json", "-i", probeInput())
	hideProbeWindow(cmd)
	var output limitedBuffer
	output.max = 64 << 10
	cmd.Stdin = file
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if e := cmd.Run(); e != nil {
		if _, ok := e.(*exec.Error); ok {
			return Probe{}, problem("ffprobe_missing", "Install FFprobe on the ReelScarey server, or use the Docker image, then retry the import.")
		}
		return Probe{}, problem("invalid_media", "FFprobe could not verify a playable video. The source was left unchanged.")
	}
	var raw struct {
		Format struct {
			Duration string `json:"duration"`
			Tags     struct {
				Title string `json:"title"`
			} `json:"tags"`
		} `json:"format"`
		Streams []struct {
			Type          string `json:"codec_type"`
			Codec         string `json:"codec_name"`
			Width         int    `json:"width"`
			Height        int    `json:"height"`
			ColorTransfer string `json:"color_transfer"`
			Disposition   struct {
				Attached int `json:"attached_pic"`
				Forced   int `json:"forced"`
				Hearing  int `json:"hearing_impaired"`
			} `json:"disposition"`
			Tags struct {
				Language string `json:"language"`
				Title    string `json:"title"`
			} `json:"tags"`
		} `json:"streams"`
	}
	if json.Unmarshal(output.Bytes(), &raw) != nil {
		return Probe{}, problem("invalid_media", "FFprobe returned invalid media information.")
	}
	duration, e := strconv.ParseFloat(raw.Format.Duration, 64)
	if e != nil || math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 {
		return Probe{}, problem("invalid_media", "The movie's duration could not be verified.")
	}
	subtitles := []SubtitleStream{}
	audio := []AudioStream{}
	for _, stream := range raw.Streams {
		if stream.Type == "subtitle" && len(subtitles) < 100 {
			subtitles = append(subtitles, SubtitleStream{Language: stream.Tags.Language, Title: stream.Tags.Title, Forced: stream.Disposition.Forced != 0, HearingImpaired: stream.Disposition.Hearing != 0})
		}
		if stream.Type == "audio" && len(audio) < 100 {
			audio = append(audio, AudioStream{Codec: stream.Codec, Language: stream.Tags.Language, Title: stream.Tags.Title})
		}
	}
	for _, stream := range raw.Streams {
		if stream.Type == "video" && stream.Codec != "" && stream.Codec != "unknown" && stream.Width > 0 && stream.Height > 0 && stream.Width <= 32768 && stream.Height <= 32768 && stream.Disposition.Attached == 0 {
			// HDR10 and HLG are the two broadcast HDR transfer families;
			// anything else (bt709, bt2020 SDR, unspecified) is SDR. Dolby
			// Vision files carry a base layer with HDR10 transfer, so they
			// label as HDR.
			hdr := stream.ColorTransfer == "smpte2084" || stream.ColorTransfer == "arib-std-b67"
			title := raw.Format.Tags.Title
			return Probe{Codec: stream.Codec, Width: stream.Width, Height: stream.Height, Duration: duration, HDR: hdr, Subtitles: subtitles, Audio: audio, Title: &title}, nil
		}
	}
	return Probe{}, problem("no_video_stream", "The selected file has no usable video stream.")
}
