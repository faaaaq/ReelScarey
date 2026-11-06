package transfer

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strings"
)

// tagManifest retains the stream, attachment, chapter and tag facts which must
// survive a metadata-only remux. It intentionally excludes container-specific
// encoding statistics which the muxer recalculates.
type tagManifest struct {
	Streams []struct {
		Index          int               `json:"index"`
		Type           string            `json:"codec_type"`
		Codec          string            `json:"codec_name"`
		CodecTag       string            `json:"codec_tag_string"`
		Width          int               `json:"width"`
		Height         int               `json:"height"`
		SampleRate     string            `json:"sample_rate"`
		Channels       int               `json:"channels"`
		ChannelLayout  string            `json:"channel_layout"`
		PixelFormat    string            `json:"pix_fmt"`
		SampleFormat   string            `json:"sample_fmt"`
		FrameRate      string            `json:"r_frame_rate"`
		TimeBase       string            `json:"time_base"`
		ColorTransfer  string            `json:"color_transfer"`
		ColorPrimaries string            `json:"color_primaries"`
		ColorSpace     string            `json:"color_space"`
		ColorRange     string            `json:"color_range"`
		ExtraHash      string            `json:"extradata_hash"`
		Tags           map[string]string `json:"tags"`
		Disposition    map[string]int    `json:"disposition"`
	} `json:"streams"`
	Chapters []struct {
		Start string            `json:"start_time"`
		End   string            `json:"end_time"`
		Tags  map[string]string `json:"tags"`
	} `json:"chapters"`
	Format struct {
		Tags map[string]string `json:"tags"`
	} `json:"format"`
}

type retagResult struct {
	Probe      Probe
	Manifest   tagManifest
	PacketHash [32]byte
}

func retagInspect(ctx context.Context, f *os.File) (retagResult, error) {
	var out retagResult
	if _, e := f.Seek(0, 0); e != nil {
		return out, e
	}
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-protocol_whitelist", probeProtocols(), "-show_data_hash", "sha256", "-show_entries", "stream=index,codec_type,codec_name,codec_tag_string,extradata_hash,width,height,sample_rate,channels,channel_layout,pix_fmt,sample_fmt,r_frame_rate,time_base,color_transfer,color_primaries,color_space,color_range:stream_tags:stream_disposition:chapter=start_time,end_time:chapter_tags:format_tags", "-of", "json", "-i", probeInput())
	hideProbeWindow(cmd)
	cmd.Stdin = f
	cmd.Stderr = io.Discard
	var b limitedBuffer
	b.max = 1 << 20
	cmd.Stdout = &b
	if e := cmd.Run(); e != nil {
		return out, problem("invalid_media", "Unable to verify all media streams and metadata.")
	}
	if e := json.Unmarshal(b.Bytes(), &out.Manifest); e != nil {
		return out, problem("invalid_media", "Unable to read media stream information.")
	}
	if _, e := f.Seek(0, 0); e != nil {
		return out, e
	}
	// Hash packet indexes, timestamps, durations and data hashes in their original
	// order. Any remux that changes, omits or reorders packets is rejected.
	cmd = exec.CommandContext(ctx, "ffprobe", "-v", "error", "-protocol_whitelist", probeProtocols(), "-show_packets", "-show_data_hash", "sha256", "-show_entries", "packet=stream_index,pts_time,dts_time,duration_time,data_hash", "-of", "compact=p=0:nk=0", "-i", probeInput())
	hideProbeWindow(cmd)
	cmd.Stdin = f
	cmd.Stderr = io.Discard
	h := sha256.New()
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		return out, e
	}
	if e = cmd.Start(); e != nil {
		return out, problem("invalid_media", "Unable to verify media packets.")
	}
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 4096), 65536)
	packets := 0
	havePacket, haveHash := false, false
	continuations := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "stream_index=") {
			if havePacket && !haveHash {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return out, problem("invalid_media", "A media packet could not be hashed.")
			}
			havePacket = true
			haveHash = false
			continuations = 0
			packets++
		} else if !havePacket || !strings.HasPrefix(line, "|") {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return out, problem("invalid_media", "A media packet could not be verified.")
		} else {
			continuations++
			if continuations > 16 {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return out, problem("invalid_media", "Media packet metadata exceeds the verification limit.")
			}
		}
		const marker = "data_hash=SHA256:"
		pos := strings.Index(line, marker)
		if pos >= 0 {
			if haveHash || len(line) < pos+len(marker)+64 {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return out, problem("invalid_media", "A media packet hash was invalid.")
			}
			end := pos + len(marker) + 64
			if end < len(line) && line[end] != '|' {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return out, problem("invalid_media", "A media packet hash was invalid.")
			}
			if _, e = hex.DecodeString(line[pos+len(marker) : pos+len(marker)+64]); e != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return out, problem("invalid_media", "A media packet hash was invalid.")
			}
			haveHash = true
		}
		_, _ = io.WriteString(h, line+"\n")
	}
	if e = scanner.Err(); e != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return out, problem("invalid_media", "Unable to verify media packet data.")
	}
	if e = cmd.Wait(); e != nil {
		return out, problem("invalid_media", "Unable to verify media packets.")
	}
	if packets == 0 || !haveHash {
		return out, problem("invalid_media", "No media packets could be verified.")
	}
	copy(out.PacketHash[:], h.Sum(nil))
	p, e := (FFprobe{}).Probe(ctx, f)
	if e != nil {
		return out, e
	}
	out.Probe = p
	return out, nil
}

// RewriteTitleVerified writes only to a caller-created independent staging file.
// It supports MP4, Matroska, MOV, AVI and WebM and rejects remuxes that change media packets,
// stream facts, chapters or existing non-title container tags.
func RewriteTitleVerified(ctx context.Context, source, stage *os.File, extension, title string) (Probe, error) {
	if strings.TrimSpace(title) == "" || len(title) > 1024 || strings.ContainsAny(title, "\x00\r\n") {
		return Probe{}, problem("invalid_metadata", "The saved title cannot be written safely.")
	}
	mux := ""
	switch strings.ToLower(extension) {
	case ".mkv":
		mux = "matroska"
	case ".mp4":
		mux = "mp4"
	case ".m4v":
		mux = "ipod"
	case ".mov":
		mux = "mov"
	case ".avi":
		mux = "avi"
	case ".webm":
		mux = "webm"
	default:
		return Probe{}, problem("unsupported_media", "Embedded title updates support MP4, Matroska, MOV, AVI and WebM files.")
	}
	a, e := source.Stat()
	if e != nil {
		return Probe{}, e
	}
	b, e := stage.Stat()
	if e != nil {
		return Probe{}, e
	}
	if !a.Mode().IsRegular() || !b.Mode().IsRegular() || os.SameFile(a, b) {
		return Probe{}, problem("unsafe_path", "Metadata staging must be a separate regular file.")
	}
	before, e := retagInspect(ctx, source)
	if e != nil {
		return Probe{}, e
	}
	if e = stage.Truncate(0); e != nil {
		return Probe{}, e
	}
	if _, e = source.Seek(0, 0); e != nil {
		return Probe{}, e
	}
	// The source is inherited as stdin so Linux can open a seekable descriptor
	// for MP4/MOV files whose index sits at the end. The stage is fd 3.
	args := []string{"-nostdin", "-v", "error", "-copyts", "-protocol_whitelist", probeProtocols(), "-i", probeInput(), "-map", "0", "-map_metadata", "0", "-map_chapters", "0", "-c", "copy", "-metadata", "title=" + title, "-f", mux, "-avoid_negative_ts", "disabled"}
	if mux == "mp4" || mux == "mov" || mux == "ipod" {
		if brand := before.Manifest.Format.Tags["major_brand"]; brand != "" {
			args = append(args, "-brand", brand)
		}
	}
	args = append(args, "-y", "/proc/self/fd/3")
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	hideProbeWindow(cmd)
	cmd.Stdin = source
	cmd.ExtraFiles = []*os.File{stage}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if e = cmd.Run(); e != nil {
		return Probe{}, problem("metadata_unsupported", "This file could not be safely rewritten with all streams. The original was left unchanged.")
	}
	if e = stage.Sync(); e != nil {
		return Probe{}, e
	}
	after, e := retagInspect(ctx, stage)
	if e != nil {
		return Probe{}, e
	}
	if len(before.Manifest.Streams) != len(after.Manifest.Streams) {
		return Probe{}, problem("metadata_changed_streams", "The rewritten copy did not preserve every stream.")
	}
	for i := range before.Manifest.Streams {
		a, b := before.Manifest.Streams[i], after.Manifest.Streams[i]
		for k, v := range a.Tags {
			if strings.EqualFold(k, "encoder") || strings.EqualFold(k, "duration") || strings.HasPrefix(strings.ToUpper(k), "_STATISTICS_") {
				continue
			}
			if b.Tags[k] != v {
				return Probe{}, problem("metadata_changed_tags", "The rewritten copy did not preserve stream metadata.")
			}
		}
		a.Tags = nil
		b.Tags = nil
		if !reflect.DeepEqual(a, b) {
			return Probe{}, problem("metadata_changed_streams", "The rewritten copy did not preserve stream facts.")
		}
	}
	if after.Probe.Title == nil || *after.Probe.Title != title || before.PacketHash != after.PacketHash || !reflect.DeepEqual(before.Manifest.Chapters, after.Manifest.Chapters) {
		return Probe{}, problem("metadata_changed_streams", "The rewritten copy did not preserve every stream, packet and chapter; the original was left unchanged.")
	}
	for k, v := range before.Manifest.Format.Tags {
		if strings.EqualFold(k, "title") || strings.EqualFold(k, "encoder") {
			continue
		}
		got, ok := after.Manifest.Format.Tags[k]
		if !ok || got != v {
			return Probe{}, problem("metadata_changed_tags", "The rewritten copy did not preserve existing metadata; the original was left unchanged.")
		}
	}
	return after.Probe, nil
}
