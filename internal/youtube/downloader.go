package youtube

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var ErrDownload = errors.New("YouTube download failed or produced an unexpected output")
var subtitleLanguage = regexp.MustCompile(`^[A-Za-z]{2,3}(?:-[A-Za-z0-9]{2,8}){0,3}$`)

type DownloadOptions struct {
	Languages []string `json:"languages"`
	Automatic bool     `json:"automatic"`
}
type SubtitleFile struct {
	Language string `json:"language"`
	Name     string `json:"name"`
}
type DownloadResult struct {
	Video            string         `json:"video"`
	Subtitles        []SubtitleFile `json:"subtitles"`
	MissingLanguages []string       `json:"missing_languages"`
}

func validSubtitleOptions(options DownloadOptions) bool {
	if len(options.Languages) > 8 || options.Automatic && len(options.Languages) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, language := range options.Languages {
		key := strings.ToLower(language)
		if !subtitleLanguage.MatchString(language) || seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

// Download writes one exact video into an existing, empty, dedicated staging
// directory. The caller owns that directory and must independently probe and
// review the returned file before importing it into a media library.
func (i *Inspector) Download(ctx context.Context, id, directory string) (string, error) {
	result, err := i.DownloadWithSubtitles(ctx, id, directory, DownloadOptions{})
	return result.Video, err
}

// DownloadWithSubtitles accepts only explicit language tags. Captions are
// stored as separate VTT files and are never treated as an imported sidecar
// until the caller has reviewed and journaled them with the video.
func (i *Inspector) DownloadWithSubtitles(ctx context.Context, id, directory string, options DownloadOptions) (DownloadResult, error) {
	result := DownloadResult{Subtitles: []SubtitleFile{}, MissingLanguages: []string{}}
	seen := map[string]bool{}
	if !validSubtitleOptions(options) {
		return result, ErrDownload
	}
	for _, language := range options.Languages {
		seen[strings.ToLower(language)] = true
	}
	if i == nil || i.command == nil || !videoID.MatchString(id) {
		return result, ErrDownload
	}
	dir, err := filepath.Abs(directory)
	if err != nil {
		return result, ErrDownload
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return result, ErrDownload
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		return result, ErrDownload
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()
	args := []string{"--ignore-config", "--no-cache-dir", "--no-cookies", "--no-cookies-from-browser", "--no-warnings", "--no-progress", "--no-playlist", "--no-overwrites", "--no-write-info-json", "--no-write-thumbnail", "--no-write-playlist-metafiles", "--paths", dir, "-o", id + ".%(ext)s"}
	if len(options.Languages) == 0 {
		args = append(args, "--no-write-subs", "--no-write-auto-subs")
	} else {
		if options.Automatic {
			args = append(args, "--no-write-subs", "--write-auto-subs")
		} else {
			args = append(args, "--write-subs", "--no-write-auto-subs")
		}
		patterns := make([]string, 0, len(options.Languages))
		for _, language := range options.Languages {
			patterns = append(patterns, "^"+regexp.QuoteMeta(language)+"$")
		}
		args = append(args, "--sub-langs", strings.Join(patterns, ","), "--sub-format", "vtt")
	}
	args = append(args, "--", source("video", id).URL)
	cmd := i.command(ctx, i.executable, args...)
	cmd.Env = append(os.Environ(), "YTDLP_NO_PLUGINS=1")
	stdout, stderr := &boundedBuffer{max: 64 << 10}, &boundedBuffer{max: 64 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil || stdout.exceeded || stderr.exceeded || ctx.Err() != nil {
		return result, ErrDownload
	}
	files, err = os.ReadDir(dir)
	if err != nil || len(files) < 1 || len(files) > 1+len(options.Languages) {
		return result, ErrDownload
	}
	found := map[string]bool{}
	for _, file := range files {
		name := file.Name()
		ext := strings.ToLower(filepath.Ext(name))
		info, err = os.Lstat(filepath.Join(dir, name))
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
			return result, ErrDownload
		}
		if name == id+ext && (ext == ".mkv" || ext == ".mp4" || ext == ".webm" || ext == ".mov" || ext == ".m4v") {
			if result.Video != "" {
				return result, ErrDownload
			}
			result.Video = name
			continue
		}
		if !strings.HasPrefix(name, id+".") || ext != ".vtt" || info.Size() > 4<<20 {
			return result, ErrDownload
		}
		lang := strings.TrimSuffix(strings.TrimPrefix(name, id+"."), ext)
		key := strings.ToLower(lang)
		if !seen[key] || found[key] {
			return result, ErrDownload
		}
		found[key] = true
		result.Subtitles = append(result.Subtitles, SubtitleFile{Language: lang, Name: name})
	}
	if result.Video == "" {
		return result, ErrDownload
	}
	for _, lang := range options.Languages {
		if !found[strings.ToLower(lang)] {
			result.MissingLanguages = append(result.MissingLanguages, lang)
		}
	}
	return result, nil
}
