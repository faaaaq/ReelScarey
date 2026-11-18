package main

import (
	"context"
	"regexp"

	"github.com/faaaaq/ReelScarey/internal/music"
	"github.com/faaaaq/ReelScarey/internal/tidal"
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

// tidalAudioSource adapts the tidaler provider to the music acquisition
// contract. Candidate IDs are the tidal.com track URLs the provider itself
// accepts back for download, so journal records and retry requests always
// name a source that identifies itself.
type tidalAudioSource struct {
	service *tidal.Service
}

func (a tidalAudioSource) SearchAudio(ctx context.Context, query string, limit int) ([]music.AudioCandidate, error) {
	candidates, err := a.service.SearchAudio(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]music.AudioCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, music.AudioCandidate{
			ID: candidate.ID, Title: candidate.Title, Channel: candidate.Artist, DurationSeconds: candidate.DurationSeconds,
		})
	}
	return out, nil
}

func (a tidalAudioSource) DownloadAudio(ctx context.Context, id, directory, format string) (music.AudioFile, error) {
	file, err := a.service.DownloadAudio(ctx, id, directory, format)
	if err != nil {
		return music.AudioFile{}, err
	}
	return music.AudioFile{Path: file.Path, Format: file.Format}, nil
}

func (a tidalAudioSource) InspectAudio(ctx context.Context, rawURL string) (music.AudioInspection, error) {
	inspection, err := a.service.Inspect(ctx, rawURL)
	if err != nil {
		return music.AudioInspection{}, err
	}
	out := music.AudioInspection{Kind: inspection.Kind, Title: inspection.Title, Channel: inspection.Channel, Entries: []music.AudioEntry{}}
	for _, entry := range inspection.Entries {
		out.Entries = append(out.Entries, music.AudioEntry{
			ID: entry.ID, Title: entry.Title, Channel: entry.Channel, DurationSeconds: entry.DurationSeconds,
		})
	}
	return out, nil
}

// Owns claims exactly the tidal.com track and album links the provider
// produces, so the priority chain routes them back to this source.
func (a tidalAudioSource) Owns(id string) bool {
	return tidal.TrackURL.MatchString(id) || tidal.AlbumURL.MatchString(id)
}

// youtubeVideoID matches the bare video IDs the yt-dlp search path returns.
// It is the routing key for the fallback source in the priority chain.
var youtubeVideoID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// Owns claims bare YouTube video IDs. Full youtube.com URLs stay with the
// youtube inspector's own link handling.
func (a youtubeAudioSource) Owns(id string) bool { return youtubeVideoID.MatchString(id) }
