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
	"time"
)

type Probe struct {
	Codec    string  `json:"codec"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Duration float64 `json:"duration_seconds"`
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
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-protocol_whitelist", "pipe", "-probesize", "16777216", "-analyzeduration", "10000000", "-show_entries", "format=duration:stream=codec_type,codec_name,width,height:stream_disposition=attached_pic", "-of", "json", "-i", "pipe:0")
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
		} `json:"format"`
		Streams []struct {
			Type        string `json:"codec_type"`
			Codec       string `json:"codec_name"`
			Width       int    `json:"width"`
			Height      int    `json:"height"`
			Disposition struct {
				Attached int `json:"attached_pic"`
			} `json:"disposition"`
		} `json:"streams"`
	}
	if json.Unmarshal(output.Bytes(), &raw) != nil {
		return Probe{}, problem("invalid_media", "FFprobe returned invalid media information.")
	}
	duration, e := strconv.ParseFloat(raw.Format.Duration, 64)
	if e != nil || math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 {
		return Probe{}, problem("invalid_media", "The movie's duration could not be verified.")
	}
	for _, stream := range raw.Streams {
		if stream.Type == "video" && stream.Codec != "" && stream.Codec != "unknown" && stream.Width > 0 && stream.Height > 0 && stream.Width <= 32768 && stream.Height <= 32768 && stream.Disposition.Attached == 0 {
			return Probe{Codec: stream.Codec, Width: stream.Width, Height: stream.Height, Duration: duration}, nil
		}
	}
	return Probe{}, problem("invalid_media", "The selected file has no usable video stream.")
}
