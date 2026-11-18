package music

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type chainAudioSource struct {
	name          string
	candidates    []AudioCandidate
	searchErr     error
	download      AudioFile
	downloadErr   error
	claims        func(id string) bool
	searchCalls   int
	downloadCalls int
}

func (f *chainAudioSource) SearchAudio(ctx context.Context, query string, limit int) ([]AudioCandidate, error) {
	f.searchCalls++
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return f.candidates, nil
}

func (f *chainAudioSource) DownloadAudio(ctx context.Context, id, directory, format string) (AudioFile, error) {
	f.downloadCalls++
	if f.downloadErr != nil {
		return AudioFile{}, f.downloadErr
	}
	return f.download, nil
}

func (f *chainAudioSource) InspectAudio(ctx context.Context, rawURL string) (AudioInspection, error) {
	return AudioInspection{Kind: "track", Title: f.name}, nil
}

func (f *chainAudioSource) Owns(id string) bool {
	if f.claims == nil {
		return false
	}
	return f.claims(id)
}

func TestPrioritySearchPrefersPrimary(t *testing.T) {
	primary := &chainAudioSource{name: "primary", candidates: []AudioCandidate{{ID: "https://tidal.com/browse/track/1", Title: "Song"}}}
	fallback := &chainAudioSource{name: "fallback", candidates: []AudioCandidate{{ID: "abcde123456", Title: "Song"}}}
	chain := PriorityAudioSource{Primary: primary, Fallback: fallback}
	candidates, err := chain.SearchAudio(context.Background(), "artist song", 10)
	if err != nil {
		t.Fatalf("SearchAudio() failed: %v", err)
	}
	if len(candidates) != 1 || candidates[0].ID != "https://tidal.com/browse/track/1" {
		t.Fatalf("the primary result must win: %+v", candidates)
	}
	if primary.searchCalls != 1 || fallback.searchCalls != 0 {
		t.Fatalf("the fallback must not be contacted while the primary answers: primary=%d fallback=%d", primary.searchCalls, fallback.searchCalls)
	}
}

func TestPrioritySearchFallsBackOnErrorAndEmptyResults(t *testing.T) {
	for _, test := range []struct {
		name       string
		candidates []AudioCandidate
		searchErr  error
	}{
		{"error", nil, errors.New("no TIDAL session")},
		{"empty", []AudioCandidate{}, nil},
	} {
		primary := &chainAudioSource{name: "primary", candidates: test.candidates, searchErr: test.searchErr}
		fallback := &chainAudioSource{name: "fallback", candidates: []AudioCandidate{{ID: "abcde123456"}}}
		chain := PriorityAudioSource{Primary: primary, Fallback: fallback}
		candidates, err := chain.SearchAudio(context.Background(), "artist song", 10)
		if err != nil {
			t.Fatalf("%s: SearchAudio() failed: %v", test.name, err)
		}
		if len(candidates) != 1 || candidates[0].ID != "abcde123456" {
			t.Fatalf("%s: the fallback result must be used: %+v", test.name, candidates)
		}
	}
}

func TestPrioritySearchNamesBothSourcesWhenBothFail(t *testing.T) {
	primary := &chainAudioSource{searchErr: errors.New("primary down")}
	fallback := &chainAudioSource{searchErr: errors.New("fallback down")}
	chain := PriorityAudioSource{Primary: primary, Fallback: fallback}
	_, err := chain.SearchAudio(context.Background(), "q", 10)
	if err == nil || !chainContainsAll(err.Error(), "primary down", "fallback down") {
		t.Fatalf("both failures must be visible, got %v", err)
	}
}

func chainContainsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}

func TestPriorityDownloadRoutesByClaim(t *testing.T) {
	primary := &chainAudioSource{name: "primary", download: AudioFile{Path: "primary.flac"}, claims: tidalClaim}
	fallback := &chainAudioSource{name: "fallback", download: AudioFile{Path: "fallback.flac"}, claims: youtubeClaim}
	chain := PriorityAudioSource{Primary: primary, Fallback: fallback}
	file, err := chain.DownloadAudio(context.Background(), "https://tidal.com/browse/track/1", "dir", "source")
	if err != nil || file.Path != "primary.flac" {
		t.Fatalf("a claimed primary ID must route to the primary, got %+v %v", file, err)
	}
	if primary.downloadCalls != 1 || fallback.downloadCalls != 0 {
		t.Fatalf("unexpected call counts: primary=%d fallback=%d", primary.downloadCalls, fallback.downloadCalls)
	}
	file, err = chain.DownloadAudio(context.Background(), "abcde123456", "dir", "source")
	if err != nil || file.Path != "fallback.flac" {
		t.Fatalf("an unclaimed ID must route to the fallback, got %+v %v", file, err)
	}
}

func TestPriorityDownloadNeverReroutesFailedPrimaryID(t *testing.T) {
	primary := &chainAudioSource{name: "primary", downloadErr: errors.New("TIDAL refused the stream"), claims: tidalClaim}
	fallback := &chainAudioSource{name: "fallback", download: AudioFile{Path: "fallback.flac"}, claims: youtubeClaim}
	chain := PriorityAudioSource{Primary: primary, Fallback: fallback}
	if _, err := chain.DownloadAudio(context.Background(), "https://tidal.com/browse/track/1", "dir", "source"); err == nil {
		t.Fatal("a failed primary download must surface its error, not silently reroute")
	}
	if fallback.downloadCalls != 0 {
		t.Fatal("the fallback must not download a foreign ID")
	}
}

func TestPriorityInspectRoutesByOwnership(t *testing.T) {
	primary := &chainAudioSource{name: "primary", claims: tidalClaim}
	fallback := &chainAudioSource{name: "fallback", claims: youtubeClaim}
	chain := PriorityAudioSource{Primary: primary, Fallback: fallback}
	inspection, err := chain.InspectAudio(context.Background(), "https://tidal.com/browse/album/1")
	if err != nil || inspection.Title != "primary" {
		t.Fatalf("the primary must inspect its own links, got %+v %v", inspection, err)
	}
	inspection, err = chain.InspectAudio(context.Background(), "https://www.youtube.com/watch?v=abcde123456")
	if err != nil || inspection.Title != "fallback" {
		t.Fatalf("foreign links must go to the fallback, got %+v %v", inspection, err)
	}
}

func TestPriorityWithoutSourcesIsAnError(t *testing.T) {
	chain := PriorityAudioSource{}
	if _, err := chain.SearchAudio(context.Background(), "q", 1); err == nil {
		t.Fatal("an empty chain must fail visibly")
	}
	if _, err := chain.DownloadAudio(context.Background(), "id", "dir", "source"); err == nil {
		t.Fatal("an empty chain must fail visibly")
	}
}

func tidalClaim(id string) bool   { return strings.Contains(id, "tidal.com/browse/") }
func youtubeClaim(id string) bool { return len(id) == 11 && !strings.Contains(id, "://") }
