package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// AudioCandidate is a bounded YouTube search hit used by the music acquisition
// pipeline. It is not a provider identity for the MusicBrainz catalog.
type AudioCandidate struct {
	ID              string
	Title           string
	Channel         string
	DurationSeconds int64
}

type AudioFile struct {
	Path   string
	Format string
}

var ErrAudioDownload = errors.New("audio download failed or produced an unexpected output")

func validAudioQuery(query string, limit int) bool {
	query = strings.TrimSpace(query)
	return utf8.ValidString(query) && utf8.RuneCountInString(query) >= 1 &&
		utf8.RuneCountInString(query) <= 300 && !strings.ContainsFunc(query, unicode.IsControl) &&
		limit >= 1 && limit <= 25
}

// SearchAudio searches YouTube for an audio candidate. The caller chooses a
// result only when its duration is close enough to the catalog metadata.
func (i *Inspector) SearchAudio(ctx context.Context, query string, limit int) ([]AudioCandidate, error) {
	if i == nil || i.command == nil || !validAudioQuery(query, limit) {
		return nil, ErrInspection
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	args := []string{"--ignore-config", "--no-cache-dir", "--no-cookies", "--no-cookies-from-browser", "--no-warnings", "--no-progress", "--skip-download", "--flat-playlist", "--dump-single-json", "--playlist-end", strconv.Itoa(limit), "--", "ytsearch" + strconv.Itoa(limit) + ":" + strings.TrimSpace(query)}
	cmd := i.command(ctx, i.executable, args...)
	cmd.Env = append(os.Environ(), "YTDLP_NO_PLUGINS=1")
	stdout := &boundedBuffer{max: maxInspectionBytes}
	stderr := &boundedBuffer{max: 64 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil || stdout.exceeded || stderr.exceeded || ctx.Err() != nil {
		return nil, ErrInspection
	}
	var body rawEntry
	if err := json.Unmarshal(stdout.Bytes(), &body); err != nil {
		return nil, ErrInspection
	}
	out := []AudioCandidate{}
	for _, item := range body.Entries {
		var entry rawEntry
		if json.Unmarshal(item, &entry) != nil || !videoID.MatchString(entry.ID) {
			continue
		}
		title := strings.TrimSpace(entry.Title)
		if title == "" {
			continue
		}
		out = append(out, AudioCandidate{
			ID:              entry.ID,
			Title:           title,
			Channel:         strings.TrimSpace(first(entry.Channel, entry.Uploader)),
			DurationSeconds: int64(entry.Duration),
		})
	}
	return out, nil
}

// DownloadAudio downloads one exact video ID as audio into a dedicated folder.
// Callers own the folder and must verify the returned file before import.
func (i *Inspector) DownloadAudio(ctx context.Context, id, directory, format string) (AudioFile, error) {
	if i == nil || i.command == nil || !videoID.MatchString(id) {
		return AudioFile{}, ErrAudioDownload
	}
	format = strings.ToLower(strings.TrimSpace(format))
	switch format {
	case "", "source":
		format = "source"
	case "mp3", "flac", "m4a", "opus":
	default:
		return AudioFile{}, ErrAudioDownload
	}
	dir, err := filepath.Abs(directory)
	if err != nil {
		return AudioFile{}, ErrAudioDownload
	}
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return AudioFile{}, ErrAudioDownload
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	args := []string{"--ignore-config", "--no-cache-dir", "--no-cookies", "--no-cookies-from-browser", "--no-warnings", "--no-progress", "--no-playlist", "--no-overwrites", "--restrict-filenames"}
	if format == "source" {
		args = append(args, "-f", "bestaudio/best")
	} else {
		args = append(args, "-x", "--audio-format", format, "--audio-quality", "0")
	}
	args = append(args, "-o", filepath.Join(dir, "%(id)s.%(ext)s"), "--", "https://www.youtube.com/watch?v="+id)
	cmd := i.command(ctx, i.executable, args...)
	cmd.Env = append(os.Environ(), "YTDLP_NO_PLUGINS=1")
	stdout := &boundedBuffer{max: 1 << 20}
	stderr := &boundedBuffer{max: 128 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil || stdout.exceeded || stderr.exceeded || ctx.Err() != nil {
		return AudioFile{}, ErrAudioDownload
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return AudioFile{}, ErrAudioDownload
	}
	var found []AudioFile
	for _, entry := range entries {
		if entry.IsDir() || strings.HasSuffix(strings.ToLower(entry.Name()), ".part") {
			continue
		}
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(entry.Name()), "."))
		switch ext {
		case "mp3", "flac", "m4a", "opus", "ogg", "oga", "webm", "wav", "aac":
			found = append(found, AudioFile{Path: filepath.Join(dir, entry.Name()), Format: ext})
		}
	}
	if len(found) != 1 {
		return AudioFile{}, ErrAudioDownload
	}
	return found[0], nil
}
