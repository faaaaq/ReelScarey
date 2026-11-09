package music

import (
	"context"
	"errors"
	"os"
	"path"
	"strings"
	"time"
)

// AudioStream is one registered audio file opened for HTTP streaming. The
// caller must close File. The path is never serialized or returned to a
// browser; playback addresses the catalog file ID instead.
type AudioStream struct {
	File        *os.File
	Name        string
	ContentType string
	Size        int64
	ModTime     time.Time
}

// OpenAudioFile opens a registered music file below the media root. os.Root
// prevents a stale or tampered relative path from escaping that root, and the
// stored catalog row is the only source of the path.
func (s *Service) OpenAudioFile(ctx context.Context, id int64) (AudioStream, error) {
	if id < 1 {
		return AudioStream{}, ErrInvalidInput
	}
	var relative, format string
	err := s.store.db.QueryRowContext(ctx, `SELECT relative_path,format FROM music_files WHERE id=?`, id).
		Scan(&relative, &format)
	if err != nil {
		return AudioStream{}, err
	}
	if s.mediaDir == "" || strings.TrimSpace(relative) == "" {
		return AudioStream{}, &Error{Code: "music_file_missing", Message: "This registered music file is not available on the server."}
	}
	name := path.Clean(strings.ReplaceAll(strings.TrimSpace(relative), "\\", "/"))
	if name == "." || name == ".." || strings.HasPrefix(name, "../") || strings.HasPrefix(name, "/") {
		return AudioStream{}, ErrInvalidInput
	}
	media, err := os.OpenRoot(s.mediaDir)
	if err != nil {
		return AudioStream{}, err
	}
	defer media.Close()
	file, err := media.Open(name)
	if errors.Is(err, os.ErrNotExist) {
		return AudioStream{}, &Error{Code: "music_file_missing", Message: "This registered music file is not available on the server."}
	}
	if err != nil {
		return AudioStream{}, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return AudioStream{}, &Error{Code: "music_file_missing", Message: "This registered music file is not available on the server."}
	}
	return AudioStream{
		File:        file,
		Name:        path.Base(relative),
		ContentType: audioContentType(format, relative),
		Size:        info.Size(),
		ModTime:     info.ModTime(),
	}, nil
}

func audioContentType(format, name string) string {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
	if ext == "" {
		ext = strings.ToLower(strings.TrimSpace(format))
	}
	switch ext {
	case "mp3":
		return "audio/mpeg"
	case "flac":
		return "audio/flac"
	case "m4a", "mp4", "aac":
		return "audio/mp4"
	case "ogg", "oga", "opus":
		return "audio/ogg"
	case "wav":
		return "audio/wav"
	case "aif", "aiff":
		return "audio/aiff"
	case "wma":
		return "audio/x-ms-wma"
	default:
		return "application/octet-stream"
	}
}
