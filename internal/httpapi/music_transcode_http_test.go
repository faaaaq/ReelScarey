package httpapi

// Transcoding HTTP tests: quality profiles, conversion queue, optimization
// dry-run/apply, stream cache stats and the per-file export route. The
// fakeMusic stub in music_test.go implements the transcoding methods.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/music"
)

func (f *fakeMusic) ListProfiles(context.Context) ([]music.QualityProfile, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.profiles == nil {
		return music.BuiltinProfiles(), nil
	}
	return f.profiles, nil
}

func (f *fakeMusic) SaveCustomProfile(_ context.Context, in music.QualityProfile) (music.QualityProfile, error) {
	if f.err != nil {
		return music.QualityProfile{}, f.err
	}
	if strings.TrimSpace(in.Name) == "" {
		return music.QualityProfile{}, music.ErrInvalidInput
	}
	if in.ID > 0 && in.ID != f.savedProfile.ID {
		return music.QualityProfile{}, music.ErrInvalidInput
	}
	if in.ID == 0 {
		in.ID = 12 // like the service assigns on create
	}
	in.Key = ""
	in.Custom = true
	f.savedProfile = in
	return in, nil
}

func (f *fakeMusic) DeleteCustomProfile(_ context.Context, id int64) error {
	if f.err != nil {
		return f.err
	}
	if id != f.savedProfile.ID {
		return music.ErrInvalidInput
	}
	f.deletedProfileID = id
	return nil
}

func (f *fakeMusic) ListConversions(context.Context, int) (music.ConversionJobPage, error) {
	return f.convPage, f.err
}

func (f *fakeMusic) CancelConversion(_ context.Context, id int64) error {
	if f.err != nil {
		return f.err
	}
	if id < 1 {
		return music.ErrInvalidInput
	}
	f.cancelID = id
	return nil
}

func (f *fakeMusic) RetryConversion(_ context.Context, id int64) error {
	if f.err != nil {
		return f.err
	}
	if id < 1 {
		return music.ErrInvalidInput
	}
	f.retryID = id
	return nil
}

func (f *fakeMusic) EnqueueConversion(_ context.Context, kind string, fileID int64, profile string, priority int) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.exportKind, f.exportFileID, f.exportProfile, f.exportPriority = kind, fileID, profile, priority
	f.enqueued++
	return f.enqueued, nil
}

func (f *fakeMusic) resolveTestProfile(key string) (music.QualityProfile, error) {
	if profile, ok := music.ProfileByKey(key); ok {
		return profile, nil
	}
	if f.customProfileName != "" && key == f.customProfileName {
		return music.QualityProfile{Name: f.customProfileName, Codec: f.customProfileCodec, Custom: true}, nil
	}
	return music.QualityProfile{}, music.ErrInvalidInput
}

func (f *fakeMusic) PreviewOptimization(_ context.Context, profile string, fileIDs []int64) (music.OptimizationPreview, error) {
	if f.err != nil {
		return music.OptimizationPreview{}, f.err
	}
	if _, err := f.resolveTestProfile(profile); err != nil {
		return music.OptimizationPreview{}, err
	}
	f.previewProfile, f.previewFileIDs = profile, fileIDs
	return f.preview, nil
}

func (f *fakeMusic) ApplyOptimization(_ context.Context, profile string, fileIDs []int64) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	if _, err := f.resolveTestProfile(profile); err != nil {
		return 0, err
	}
	f.appliedProfile, f.appliedFileIDs = profile, fileIDs
	return int64(len(fileIDs)), nil
}

func (f *fakeMusic) StreamCacheStats(context.Context) (music.StreamCacheStats, error) {
	return f.cacheStats, f.err
}

func (f *fakeMusic) ClearStreamCache(context.Context) error {
	if f.err != nil {
		return f.err
	}
	f.cacheCleared = true
	return nil
}

func (f *fakeMusic) ResolveProfileKey(_ context.Context, key string) (music.QualityProfile, error) {
	if f.err != nil {
		return music.QualityProfile{}, f.err
	}
	return f.resolveTestProfile(key)
}

func TestMusicProfileRoutes(t *testing.T) {
	handler, session := musicFixture(t, &fakeMusic{})
	if out := request(handler, "GET", "/api/v1/library/music/profiles", "", nil, false); out.Code != 401 {
		t.Fatal("unauthenticated profiles read", out.Code)
	}
	out := request(handler, "GET", "/api/v1/library/music/profiles", "", session, false)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"profiles"`) || !strings.Contains(out.Body.String(), `"key":"opus-high"`) {
		t.Fatal("profiles list", out.Code, out.Body.String())
	}
	if out := request(handler, "POST", "/api/v1/library/music/profiles", `{"name":"Nope","codec":"opus","bitrate_kbps":128}`, session, false); out.Code != 403 {
		t.Fatal("create without CSRF", out.Code)
	}
	out = request(handler, "POST", "/api/v1/library/music/profiles", `{"name":"My Mix","codec":"opus","bitrate_kbps":128}`, session, true)
	if out.Code != 201 || !strings.Contains(out.Body.String(), `"name":"My Mix"`) {
		t.Fatal("create custom profile", out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/music/profiles", `{}`, session, true)
	if out.Code != 400 {
		t.Fatal("empty profile rejected", out.Code, out.Body.String())
	}
	out = request(handler, "PUT", "/api/v1/library/music/profiles/12", `{"name":"Renamed","codec":"mp3","bitrate_kbps":192}`, session, true)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"name":"Renamed"`) {
		t.Fatal("update custom profile", out.Code, out.Body.String())
	}
	if out := request(handler, "DELETE", "/api/v1/library/music/profiles", "", session, true); out.Code != 405 {
		t.Fatal("method", out.Code)
	}
	out = request(handler, "DELETE", "/api/v1/library/music/profiles/12", "", session, true)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"deleted":true`) {
		t.Fatal("delete custom profile", out.Code, out.Body.String())
	}
	out = request(handler, "DELETE", "/api/v1/library/music/profiles/99", "", session, true)
	if out.Code != 400 {
		t.Fatal("unknown profile delete", out.Code, out.Body.String())
	}
}

func TestMusicConversionRoutes(t *testing.T) {
	service := &fakeMusic{convPage: music.ConversionJobPage{
		Items: []music.ConversionJob{{ID: 5, Kind: "export", FileID: 7, Profile: "opus-128", State: "queued"}},
		Page:  2, Total: 1, Limit: 50,
	}}
	handler, session := musicFixture(t, service)
	out := request(handler, "GET", "/api/v1/library/music/conversions?page=2", "", session, false)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"page":2`) || !strings.Contains(out.Body.String(), `"kind":"export"`) || !strings.Contains(out.Body.String(), `"profile":"opus-128"`) {
		t.Fatal("conversion list", out.Code, out.Body.String())
	}
	if out := request(handler, "GET", "/api/v1/library/music/conversions?page=0", "", session, false); out.Code != 400 {
		t.Fatal("invalid page", out.Code)
	}
	if out := request(handler, "POST", "/api/v1/library/music/conversions", "{}", session, true); out.Code != 405 {
		t.Fatal("method", out.Code)
	}
	if out := request(handler, "POST", "/api/v1/library/music/conversions/5/cancel", "{}", session, false); out.Code != 403 {
		t.Fatal("cancel without CSRF", out.Code)
	}
	out = request(handler, "POST", "/api/v1/library/music/conversions/5/cancel", "{}", session, true)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"cancelled":true`) || service.cancelID != 5 {
		t.Fatal("cancel", out.Code, out.Body.String(), service.cancelID)
	}
	out = request(handler, "POST", "/api/v1/library/music/conversions/5/retry", "{}", session, true)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"retried":true`) || service.retryID != 5 {
		t.Fatal("retry", out.Code, out.Body.String(), service.retryID)
	}
	if out := request(handler, "POST", "/api/v1/library/music/conversions/5/pause", "{}", session, true); out.Code != 404 {
		t.Fatal("unknown action", out.Code)
	}
}

func TestMusicOptimizeRoutes(t *testing.T) {
	service := &fakeMusic{}
	handler, session := musicFixture(t, service)
	if out := request(handler, "GET", "/api/v1/library/music/optimize/preview", "", session, false); out.Code != 405 {
		t.Fatal("method", out.Code)
	}
	out := request(handler, "POST", "/api/v1/library/music/optimize/preview", `{"profile":""}`, session, true)
	if out.Code != 400 {
		t.Fatal("empty profile", out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/music/optimize/preview", `{"profile":"opus-999"}`, session, true)
	if out.Code != 400 || !strings.Contains(out.Body.String(), "invalid_music") {
		t.Fatal("unknown profile", out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/music/optimize/preview", `{"profile":"opus-high","file_ids":[3]}`, session, true)
	if out.Code != 200 || service.previewProfile != "opus-high" || len(service.previewFileIDs) != 1 || service.previewFileIDs[0] != 3 {
		t.Fatal("preview", out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/music/optimize/preview", `{"profile":"opus-high","file_ids":[0]}`, session, true)
	if out.Code != 400 {
		t.Fatal("invalid file id", out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/music/optimize/apply", `{"profile":"opus-high"}`, session, true)
	if out.Code != 400 {
		t.Fatal("apply without files", out.Code, out.Body.String())
	}
	out = request(handler, "POST", "/api/v1/library/music/optimize/apply", `{"profile":"opus-high","file_ids":[3,4]}`, session, true)
	if out.Code != 202 || !strings.Contains(out.Body.String(), `"enqueued":2`) || service.appliedProfile != "opus-high" || len(service.appliedFileIDs) != 2 {
		t.Fatal("apply", out.Code, out.Body.String(), service.appliedProfile)
	}
}

func TestMusicCacheRoutes(t *testing.T) {
	service := &fakeMusic{cacheStats: music.StreamCacheStats{Entries: 3, SizeBytes: 100, LimitBytes: 200, Hits: 7, Misses: 9}}
	handler, session := musicFixture(t, service)
	out := request(handler, "GET", "/api/v1/library/music/cache", "", session, false)
	body := out.Body.String()
	if out.Code != 200 || !strings.Contains(body, `"entries":3`) || !strings.Contains(body, `"size_bytes":100`) ||
		!strings.Contains(body, `"limit_bytes":200`) || !strings.Contains(body, `"hits":7`) || !strings.Contains(body, `"misses":9`) {
		t.Fatal("cache stats", out.Code, body)
	}
	if out := request(handler, "PUT", "/api/v1/library/music/cache", "", session, true); out.Code != 405 {
		t.Fatal("method", out.Code)
	}
	out = request(handler, "DELETE", "/api/v1/library/music/cache", "", session, true)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"cleared":true`) || !service.cacheCleared {
		t.Fatal("cache clear", out.Code, out.Body.String())
	}
}

func TestMusicFileExportRoutes(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "track.mp3")
	if err := os.WriteFile(audio, []byte("fake audio bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &fakeMusic{audioPath: audio, customProfileName: "Custom Mix", customProfileCodec: "mp3"}
	handler, session := musicFixture(t, service)
	if out := request(handler, "GET", "/api/v1/library/music/files/7/export", "", session, false); out.Code != 400 {
		t.Fatal("missing profile", out.Code, out.Body.String())
	}
	if out := request(handler, "GET", "/api/v1/library/music/files/7/export?profile=opus-999", "", session, false); out.Code != 400 {
		t.Fatal("unknown profile", out.Code, out.Body.String())
	}
	out := request(handler, "GET", "/api/v1/library/music/files/7/export?profile=opus-high", "", session, false)
	if out.Code != 409 || !strings.Contains(out.Body.String(), "music_export_pending") {
		t.Fatal("pending export", out.Code, out.Body.String())
	}
	if service.exportKind != "export" || service.exportFileID != 7 || service.exportProfile != "opus-high" || service.exportPriority != 3 {
		t.Fatal("enqueue shape", service.exportKind, service.exportFileID, service.exportProfile, service.exportPriority)
	}
	out = request(handler, "GET", "/api/v1/library/music/files/7/export?profile=Custom%20Mix", "", session, false)
	if out.Code != 409 || service.exportProfile != "Custom Mix" {
		t.Fatal("custom profile export", out.Code, service.exportProfile)
	}
	// Original codec streams the registered file directly with range support.
	out = request(handler, "GET", "/api/v1/library/music/files/7/export?profile=original", "", session, false)
	if out.Code != 200 || out.Body.String() != "fake audio bytes" || out.Header().Get("Content-Type") != "audio/mpeg" {
		t.Fatal("original export", out.Code, out.Body.String())
	}
	if out.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatal("range support", out.Header().Get("Accept-Ranges"))
	}
}
