package music

import (
	"context"
	"fmt"
)

// SourceScope lets a priority chain route a candidate ID or pasted link back
// to the source that owns it, without music knowing any provider's URL
// shape. Ownership answers two honest questions: whose candidate is this
// when downloading, and whose link is this when inspecting. A source that
// owns an ID but cannot fulfill it fails with its own error; the chain never
// silently reroutes an owned ID to another source, because a foreign ID
// would download unrelated audio.
type SourceScope interface {
	Owns(id string) bool
}

// PriorityAudioSource tries the primary source first and only uses the
// fallback when the primary is unavailable or genuinely has nothing. A
// search failure on the primary is not an honest reason to pretend it never
// happened: when both sides fail, the returned error names both.
type PriorityAudioSource struct {
	Primary  AudioSource
	Fallback AudioSource
}

func (p PriorityAudioSource) AudioSourceName() string { return "Automatic source chain" }

// SearchAudio returns the primary's candidates when it answers with at least
// one usable row; otherwise it falls back. An empty primary result with no
// error still falls back, because catalogs differ and a track that the
// primary source does not carry may still exist elsewhere.
func (p PriorityAudioSource) SearchAudio(ctx context.Context, query string, limit int) ([]AudioCandidate, error) {
	if p.Primary == nil {
		return p.searchFallback(ctx, query, limit)
	}
	candidates, err := p.Primary.SearchAudio(ctx, query, limit)
	if err == nil && len(candidates) > 0 {
		return candidates, nil
	}
	if p.Fallback == nil {
		if err != nil {
			return nil, err
		}
		return candidates, nil
	}
	fallback, fallbackErr := p.Fallback.SearchAudio(ctx, query, limit)
	if fallbackErr == nil {
		return fallback, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s search: %v; %s search: %v", audioSourceName(p.Primary), err, audioSourceName(p.Fallback), fallbackErr)
	}
	return nil, fallbackErr
}

func (p PriorityAudioSource) searchFallback(ctx context.Context, query string, limit int) ([]AudioCandidate, error) {
	if p.Fallback == nil {
		return nil, &Error{Code: "music_acquisition_unavailable", Message: "No audio source is configured."}
	}
	return p.Fallback.SearchAudio(ctx, query, limit)
}

// DownloadAudio routes the ID to the source that claims it. A primary failure
// on its own ID returns the error: converting a failed primary download into
// a fallback download would silently hand back unrelated audio.
func (p PriorityAudioSource) DownloadAudio(ctx context.Context, id, directory, format string) (AudioFile, error) {
	if p.Primary == nil && p.Fallback == nil {
		return AudioFile{}, &Error{Code: "music_acquisition_unavailable", Message: "No audio source is configured."}
	}
	if primary, claims := claim(p.Primary, id); claims {
		return primary.DownloadAudio(ctx, id, directory, format)
	}
	if fallback, claims := claim(p.Fallback, id); claims {
		return fallback.DownloadAudio(ctx, id, directory, format)
	}
	// No source claimed the ID. Legacy queued tracks may still carry a bare
	// video ID from before sources identified themselves; the fallback keeps
	// serving them, and every fresh candidate now carries a self-describing
	// ID. With no fallback, the primary receives the call so existing
	// behavior is preserved.
	if p.Fallback != nil {
		return p.Fallback.DownloadAudio(ctx, id, directory, format)
	}
	return p.Primary.DownloadAudio(ctx, id, directory, format)
}

func claim(source AudioSource, id string) (AudioSource, bool) {
	if source == nil {
		return nil, false
	}
	scoped, ok := source.(SourceScope)
	if !ok {
		return nil, false
	}
	return source, scoped.Owns(id)
}

// InspectAudio routes a pasted link by ownership: the primary inspects the
// links it claims, everything else goes to the fallback so an existing
// link flow keeps working while the primary source is configured.
func (p PriorityAudioSource) InspectAudio(ctx context.Context, rawURL string) (AudioInspection, error) {
	if p.Primary != nil {
		if _, claims := claim(p.Primary, rawURL); claims {
			return p.Primary.InspectAudio(ctx, rawURL)
		}
	}
	if p.Fallback != nil {
		return p.Fallback.InspectAudio(ctx, rawURL)
	}
	if p.Primary != nil {
		return p.Primary.InspectAudio(ctx, rawURL)
	}
	return AudioInspection{}, &Error{Code: "music_acquisition_unavailable", Message: "No audio source is configured."}
}
