package tidal

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testService(t *testing.T, responses map[string]string, recorded *[][]string) *Service {
	t.Helper()
	service, err := New("python3", "/opt/tidal-provider.py", t.TempDir(), "")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	service.run = func(_ context.Context, _ time.Duration, _ []string, args []string) (string, error) {
		if recorded != nil {
			*recorded = append(*recorded, args)
		}
		if response, ok := responses[args[1]]; ok {
			return response, nil
		}
		return "", errors.New("unexpected command")
	}
	return service
}

func TestNewRejectsInvalidQuality(t *testing.T) {
	if _, err := New("python3", "script.py", t.TempDir(), "ULTRA"); err == nil {
		t.Fatal("New() accepted an unknown quality")
	}
	service, err := New("python3", "script.py", t.TempDir(), "")
	if err != nil {
		t.Fatalf("New() with default quality failed: %v", err)
	}
	if service.quality != DefaultQuality {
		t.Fatalf("default quality = %q, want %q", service.quality, DefaultQuality)
	}
}

func TestStatusReportsProviderFailure(t *testing.T) {
	service, err := New("python3", "script.py", t.TempDir(), "")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	service.run = func(context.Context, time.Duration, []string, []string) (string, error) {
		return "", errors.New("python3: no such file")
	}
	status := service.Status(context.Background())
	if status.Available || status.LoggedIn {
		t.Fatalf("failing provider must not report availability: %+v", status)
	}
	if status.Detail == "" {
		t.Fatal("failing provider must keep a visible detail")
	}
}

func TestStatusParsesProviderState(t *testing.T) {
	service := testService(t, map[string]string{"status": `{"available":true,"logged_in":false,"quality":"HI_RES_LOSSLESS"}`}, nil)
	status := service.Status(context.Background())
	if !status.Available || status.LoggedIn || status.Quality != "HI_RES_LOSSLESS" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestSearchAudioValidatesAndParses(t *testing.T) {
	var calls [][]string
	service := testService(t, map[string]string{
		"search": `{"candidates":[{"id":"https://tidal.com/browse/track/123","title":"Song","artist":"Artist","duration_seconds":200}]}`,
	}, &calls)
	candidates, err := service.SearchAudio(context.Background(), "Artist Song", 10)
	if err != nil {
		t.Fatalf("SearchAudio() failed: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Title != "Song" {
		t.Fatalf("unexpected candidates: %+v", candidates)
	}
	if len(calls) != 1 || calls[0][1] != "search" || calls[0][2] != "Artist Song" || calls[0][3] != "10" {
		t.Fatalf("unexpected call arguments: %v", calls)
	}
	for _, bad := range []string{"", "line\nbreak", strings.Repeat("a", 301)} {
		if _, err := service.SearchAudio(context.Background(), bad, 10); err == nil {
			t.Fatalf("SearchAudio(%q) accepted an invalid query", bad)
		}
	}
}

func TestSearchAudioSurfacesProviderError(t *testing.T) {
	service := testService(t, map[string]string{"search": `{"candidates":[],"error":"No valid TIDAL session."}`}, nil)
	if _, err := service.SearchAudio(context.Background(), "query", 5); err == nil || !strings.Contains(err.Error(), "TIDAL session") {
		t.Fatalf("provider error must be surfaced, got %v", err)
	}
}

func TestDownloadAudioRejectsForeignIDs(t *testing.T) {
	var calls [][]string
	service := testService(t, map[string]string{}, &calls)
	if _, err := service.DownloadAudio(context.Background(), "dQw4w9WgXcQ", t.TempDir(), ""); err == nil {
		t.Fatal("DownloadAudio() accepted a bare video ID")
	}
	if len(calls) != 0 {
		t.Fatalf("a rejected ID must never start a process, got %v", calls)
	}
}

func TestDownloadAudioValidatesOutputLocation(t *testing.T) {
	dir := t.TempDir()
	service := testService(t, map[string]string{
		"download": `{"ok":true,"path":"/somewhere/else/song.flac","format":"flac"}`,
	}, nil)
	if _, err := service.DownloadAudio(context.Background(), "https://tidal.com/browse/track/123", dir, ""); err == nil {
		t.Fatal("a file outside the dedicated folder must be rejected")
	}
	service2 := testService(t, map[string]string{
		"download": `{"ok":true,"path":"` + strings.ReplaceAll(filepath.Join(dir, "song.flac"), `\`, `\\`) + `","format":"flac"}`,
	}, nil)
	file, err := service2.DownloadAudio(context.Background(), "https://tidal.com/browse/track/123", dir, "")
	if err != nil {
		t.Fatalf("DownloadAudio() failed: %v", err)
	}
	if file.Format != "flac" {
		t.Fatalf("unexpected format %q", file.Format)
	}
}

func TestDownloadAudioSurfacesProviderFailure(t *testing.T) {
	service := testService(t, map[string]string{"download": `{"ok":false,"error":"TIDAL refused the stream."}`}, nil)
	if _, err := service.DownloadAudio(context.Background(), "https://tidal.com/browse/track/123", t.TempDir(), ""); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("provider failure must be surfaced, got %v", err)
	}
}

func TestOwnsClaimsProviderLinks(t *testing.T) {
	service, err := New("python3", "script.py", t.TempDir(), "")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	claims := []struct {
		id    string
		claim bool
	}{
		{"https://tidal.com/browse/track/123", true},
		{"https://www.tidal.com/browse/track/123", true},
		{"https://tidal.com/browse/album/123", true},
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", false},
		{"dQw4w9WgXcQ", false},
		{"https://tidal.com/browse/track/notanumber", false},
		{"https://evil.com/browse/track/123", false},
	}
	for _, test := range claims {
		if got := service.Owns(test.id); got != test.claim {
			t.Fatalf("Owns(%q) = %v, want %v", test.id, got, test.claim)
		}
	}
}

func TestInspectRejectsUnknownLinks(t *testing.T) {
	var calls [][]string
	service := testService(t, map[string]string{}, &calls)
	if _, err := service.Inspect(context.Background(), "https://example.com/track/1"); err == nil {
		t.Fatal("Inspect() accepted a foreign link")
	}
	if len(calls) != 0 {
		t.Fatal("a foreign link must never start a process")
	}
}

func TestLoginStatusBoundsLines(t *testing.T) {
	lines := make([]string, 60)
	for i := range lines {
		lines[i] = strings.Repeat("x", 500)
	}
	raw, err := json.Marshal(map[string]any{"phase": "done", "lines": lines})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	service := testService(t, map[string]string{"login-status": string(raw)}, nil)
	state, err := service.LoginStatus(context.Background())
	if err != nil {
		t.Fatalf("LoginStatus() failed: %v", err)
	}
	if len(state.Lines) != 50 {
		t.Fatalf("lines were not bounded: %d", len(state.Lines))
	}
	for _, line := range state.Lines {
		if len(line) > 400 {
			t.Fatalf("a line exceeded its bound: %d", len(line))
		}
	}
}

func TestEnvIsolatesProvider(t *testing.T) {
	service, err := New("python3", "script.py", t.TempDir(), "LOSSLESS")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	env := service.env()
	found := map[string]string{}
	for _, entry := range env {
		parts := strings.SplitN(entry, "=", 2)
		found[parts[0]] = parts[1]
	}
	if found["XDG_CONFIG_HOME"] != service.configDir {
		t.Fatalf("the provider config root must live inside the configured directory, got %q", found["XDG_CONFIG_HOME"])
	}
	if found["REELSCAREY_TIDAL_QUALITY"] != "LOSSLESS" {
		t.Fatalf("quality was not handed to the provider: %q", found["REELSCAREY_TIDAL_QUALITY"])
	}
	if _, ok := found["PATH"]; !ok {
		t.Fatal("the provider needs a minimal PATH")
	}
	// The parent environment must not leak: no secret-bearing entries.
	for _, entry := range env {
		if strings.HasPrefix(entry, "REELSCAREY_TMDB_TOKEN=") {
			t.Fatal("the parent environment leaked into the provider")
		}
	}
	if _, err := os.Stat(service.configDir); !os.IsNotExist(err) {
		t.Log("config dir pre-exists; fine")
	}
}
