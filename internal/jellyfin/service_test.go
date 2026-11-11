package jellyfin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

func fixture(t *testing.T) (*Service, func()) {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := securestore.New(context.Background(), db, dir)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	return New(secrets), func() { _ = db.Close() }
}

func TestJellyfinRecentRefreshAndTokenConfidentiality(t *testing.T) {
	service, closeDB := fixture(t)
	defer closeDB()
	var token, refreshed string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token = r.Header.Get("X-Emby-Token")
		switch r.URL.Path {
		case "/Items":
			_ = json.NewEncoder(w).Encode(map[string]any{"Items": []map[string]any{{
				"Name": "Pilot", "SeriesName": "A Show", "Type": "Episode",
				"IndexNumber": 1, "ParentIndexNumber": 2, "RunTimeTicks": 100,
				"UserData": map[string]any{"PlayCount": 1, "Played": true, "PlaybackPositionTicks": 25},
			}}})
		case "/Library/Refresh":
			refreshed = "yes"
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx := context.Background()
	if err := service.Save(ctx, Config{URL: server.URL, APIKey: "private-jellyfin-key"}); err != nil {
		t.Fatal(err)
	}
	result, err := service.Recent(ctx)
	if err != nil || len(result.Items) != 1 || result.Items[0].Series != "A Show" || result.Items[0].Progress != 0.25 {
		t.Fatalf("recent: %#v %v", result, err)
	}
	if token != "private-jellyfin-key" {
		t.Fatalf("token header: %q", token)
	}
	if err := service.Refresh(ctx); err != nil || refreshed != "yes" {
		t.Fatalf("refresh: %q %v", refreshed, err)
	}
	settings, err := service.Settings(ctx)
	if err != nil || !settings.Configured || settings.URL != server.URL {
		t.Fatalf("settings: %#v %v", settings, err)
	}
	if err := service.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	settings, err = service.Settings(ctx)
	if err != nil || settings.Configured {
		t.Fatalf("delete: %#v %v", settings, err)
	}
}
