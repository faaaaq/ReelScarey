package music

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// AudioTags is the bounded tag and stream information read from one audio
// file. It is evidence for matching, not a verified identity.
type AudioTags struct {
	Format                    string `json:"format"`
	DurationMS                int    `json:"duration_ms"`
	Bitrate                   int    `json:"bitrate"`
	SampleRate                int    `json:"sample_rate"`
	Channels                  int    `json:"channels"`
	Artist                    string `json:"artist"`
	AlbumArtist               string `json:"album_artist"`
	Album                     string `json:"album"`
	Title                     string `json:"title"`
	Track                     *int   `json:"track"`
	Disc                      *int   `json:"disc"`
	Year                      *int   `json:"year"`
	MusicBrainzArtistID       string `json:"musicbrainz_artist_id"`
	MusicBrainzAlbumID        string `json:"musicbrainz_album_id"`
	MusicBrainzReleaseGroupID string `json:"musicbrainz_release_group_id"`
	MusicBrainzTrackID        string `json:"musicbrainz_track_id"`
	MusicBrainzRecordingID    string `json:"musicbrainz_recording_id"`
}

type AudioProber interface {
	ProbeAudio(context.Context, string) (AudioTags, error)
}

type FFprobeTagger struct{}

func (FFprobeTagger) ProbeAudio(ctx context.Context, path string) (AudioTags, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error",
		"-show_entries", "format=format_name,duration,bit_rate:format_tags:stream=codec_type,sample_rate,channels:stream_tags",
		"-of", "json", path)
	var output boundedBuffer
	output.max = 256 << 10
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return AudioTags{}, &Error{Code: "audio_unreadable", Message: "FFprobe could not read this audio file."}
	}
	return parseAudioProbe(output.Bytes())
}

type boundedBuffer struct {
	bytes.Buffer
	max int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.max-b.Len() {
		return 0, io.ErrShortWrite
	}
	return b.Buffer.Write(p)
}

func parseAudioProbe(body []byte) (AudioTags, error) {
	var raw struct {
		Format struct {
			Name     string            `json:"format_name"`
			Duration string            `json:"duration"`
			BitRate  string            `json:"bit_rate"`
			Tags     map[string]string `json:"tags"`
		} `json:"format"`
		Streams []struct {
			Type       string            `json:"codec_type"`
			SampleRate string            `json:"sample_rate"`
			Channels   int               `json:"channels"`
			Tags       map[string]string `json:"tags"`
		} `json:"streams"`
	}
	if json.Unmarshal(body, &raw) != nil {
		return AudioTags{}, &Error{Code: "audio_unreadable", Message: "FFprobe returned invalid audio information."}
	}
	tags := map[string]string{}
	merge := func(source map[string]string, overwrite bool) {
		for key, value := range source {
			key = strings.ToLower(strings.TrimSpace(key))
			value = strings.TrimSpace(value)
			if key == "" || value == "" {
				continue
			}
			if _, exists := tags[key]; !exists || overwrite {
				tags[key] = value
			}
		}
	}
	merge(raw.Format.Tags, true)
	for _, stream := range raw.Streams {
		if stream.Type != "audio" {
			continue
		}
		// OGG keeps Vorbis comments on the stream; format tags win when both
		// exist.
		merge(stream.Tags, false)
	}
	out := AudioTags{
		Format:                    strings.TrimSpace(raw.Format.Name),
		Artist:                    tags["artist"],
		AlbumArtist:               firstNonEmpty(tags["album_artist"], tags["albumartist"], tags["artist"]),
		Album:                     tags["album"],
		Title:                     tags["title"],
		Track:                     leadingInt(tags["track"], tags["tracknumber"]),
		Disc:                      leadingInt(tags["disc"], tags["discnumber"]),
		Year:                      yearValue(tags["date"], tags["year"], tags["originaldate"]),
		MusicBrainzArtistID:       mbidValue(tags["musicbrainz_artistid"], tags["musicbrainz artist id"]),
		MusicBrainzAlbumID:        mbidValue(tags["musicbrainz_albumid"], tags["musicbrainz album id"]),
		MusicBrainzReleaseGroupID: mbidValue(tags["musicbrainz_releasegroupid"], tags["musicbrainz release group id"]),
		MusicBrainzTrackID:        mbidValue(tags["musicbrainz_trackid"], tags["musicbrainz track id"]),
		MusicBrainzRecordingID:    mbidValue(tags["musicbrainz_recordingid"], tags["musicbrainz recording id"]),
	}
	out.DurationMS = secondsToMillis(raw.Format.Duration)
	out.Bitrate = positiveInt(raw.Format.BitRate)
	for _, stream := range raw.Streams {
		if stream.Type == "audio" {
			out.SampleRate = positiveInt(stream.SampleRate)
			out.Channels = stream.Channels
			break
		}
	}
	return out, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func positiveInt(value string) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func leadingInt(values ...string) *int {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if slash := strings.IndexByte(value, '/'); slash >= 0 {
			value = value[:slash]
		}
		if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && n > 0 && n <= 100000 {
			return &n
		}
	}
	return nil
}

func yearValue(values ...string) *int {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if len(value) < 4 {
			continue
		}
		if n, err := strconv.Atoi(value[:4]); err == nil && n >= 1800 && n <= 9999 {
			return &n
		}
	}
	return nil
}

func mbidValue(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if ValidMBID(value) {
			return strings.ToLower(value)
		}
	}
	return ""
}

func secondsToMillis(value string) int {
	seconds, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || seconds <= 0 || seconds > 24*60*60 {
		return 0
	}
	return int(seconds * 1000)
}
