package music

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func writeLibraryTags(ctx context.Context, source, stage string, tags EditTags, ext string) error {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	observed, err := (FFprobeTagger{}).ProbeAudio(ctx, source)
	if err != nil {
		return err
	}
	args := []string{"-y", "-v", "error", "-i", source, "-map", "0", "-c", "copy", "-map_metadata", "0", "-fflags", "+bitexact"}
	for field, value := range tags.LibraryEdit.Fields {
		name := field
		switch field {
		case "year":
			name = "date"
			args = append(args, "-metadata", "year=")
			if value == "" {
				args = append(args, "-metadata", "originaldate=")
			}
		case "number":
			name = "track"
			value = editedTrackValue(value, observed.TrackTotal)
			args = append(args, "-metadata", "tracknumber=")
		case "disc":
			value = editedTrackValue(value, observed.DiscTotal)
			args = append(args, "-metadata", "discnumber=")
		case "album_artist":
			args = append(args, "-metadata", "albumartist=")
		case "comment":
			args = append(args, "-metadata", "description=")
		}
		args = append(args, "-metadata", name+"="+value)
	}
	if strings.EqualFold(ext, ".mp3") {
		args = append(args, "-id3v2_version", "3")
	}
	if strings.EqualFold(ext, ".m4a") || strings.EqualFold(ext, ".mp4") {
		args = append(args, "-movflags", "use_metadata_tags")
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", append(args, stage)...)
	if err := cmd.Run(); err != nil {
		return &Error{Code: "music_tag_failed", Message: "FFmpeg could not write the selected fields. The original was retained."}
	}
	return nil
}

func editedTrackValue(value string, total *int) string {
	if total == nil {
		return value
	}
	return value + "/" + strconv.Itoa(*total)
}

func verifyLibraryTags(actual AudioTags, tags EditTags) error {
	number := func(value *int) string {
		if value == nil {
			return ""
		}
		return strconv.Itoa(*value)
	}
	values := map[string]string{
		"title": actual.Title, "artist": actual.Artist, "album_artist": actual.AlbumArtist, "album": actual.Album,
		"year": number(actual.Year), "number": number(actual.Track), "disc": number(actual.Disc), "genre": actual.Genre, "comment": actual.Comment,
	}
	for field, wanted := range tags.LibraryEdit.Fields {
		if values[field] != wanted {
			return &Error{Code: "music_tag_verify", Message: "The file did not retain the requested " + strings.ReplaceAll(field, "_", " ") + " value. The original was retained."}
		}
	}
	return nil
}
