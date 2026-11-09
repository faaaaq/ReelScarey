package main

import (
	"context"

	"github.com/faaaaq/ReelScarey/internal/music"
	"github.com/faaaaq/ReelScarey/internal/youtube"
)

// youtubeAudioSource adapts the existing yt-dlp inspector to the narrow music
// acquisition contract without making the music package depend on YouTube.
type youtubeAudioSource struct {
	inspector *youtube.Inspector
}

func (a youtubeAudioSource) SearchAudio(ctx context.Context, query string, limit int) ([]music.AudioCandidate, error) {
	items, err := a.inspector.SearchAudio(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]music.AudioCandidate, 0, len(items))
	for _, item := range items {
		out = append(out, music.AudioCandidate{
			ID: item.ID, Title: item.Title, Channel: item.Channel, DurationSeconds: item.DurationSeconds,
		})
	}
	return out, nil
}

func (a youtubeAudioSource) DownloadAudio(ctx context.Context, id, directory, format string) (music.AudioFile, error) {
	file, err := a.inspector.DownloadAudio(ctx, id, directory, format)
	if err != nil {
		return music.AudioFile{}, err
	}
	return music.AudioFile{Path: file.Path, Format: file.Format}, nil
}

func (a youtubeAudioSource) InspectAudio(ctx context.Context, rawURL string) (music.AudioInspection, error) {
	inspected, err := a.inspector.Inspect(ctx, rawURL)
	if err != nil {
		return music.AudioInspection{}, err
	}
	out := music.AudioInspection{Kind: inspected.Source.Kind, Title: inspected.Title, Channel: inspected.Channel, Entries: []music.AudioEntry{}}
	for _, entry := range inspected.Entries {
		out.Entries = append(out.Entries, music.AudioEntry{
			ID: entry.ID, Title: entry.Title, Channel: entry.Channel, DurationSeconds: entry.DurationSeconds,
		})
	}
	return out, nil
}
