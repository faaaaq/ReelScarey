package plex

import (
	"context"
	"fmt"
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

func TestPlexRecentSectionsAndRefresh(t *testing.T) {
	service, closeDB := fixture(t)
	defer closeDB()
	var token, refreshed string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token = r.Header.Get("X-Plex-Token")
		switch r.URL.Path {
		case "/library/recentlyAdded":
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><MediaContainer size="1"><Video title="Pilot" type="episode" grandparentTitle="A Show" parentIndex="2" index="1" addedAt="1700000000" lastViewedAt="1700000100" viewCount="1" viewOffset="50" duration="100"/></MediaContainer>`)
		case "/library/sections/1/refresh":
			refreshed = "yes"
			w.WriteHeader(http.StatusOK)
		case "/library/sections":
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><MediaContainer><Directory key="1" title="TV" type="show"/></MediaContainer>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx := context.Background()
	if err := service.Save(ctx, Config{URL: server.URL, Token: "private-plex-token", SectionID: "1"}); err != nil {
		t.Fatal(err)
	}
	result, err := service.Recent(ctx)
	if err != nil || len(result.Items) != 1 || result.Items[0].Series != "A Show" || result.Items[0].Progress != 0.5 {
		t.Fatalf("recent: %#v %v", result, err)
	}
	if result.Items[0].Season == nil || *result.Items[0].Season != 2 || result.Items[0].Episode == nil || *result.Items[0].Episode != 1 {
		t.Fatalf("episode numbering: %#v", result.Items[0])
	}
	if token != "private-plex-token" {
		t.Fatalf("token header: %q", token)
	}
	if err := service.Refresh(ctx); err != nil || refreshed != "yes" {
		t.Fatalf("refresh: %q %v", refreshed, err)
	}
	sections, err := service.Sections(ctx)
	if err != nil || len(sections) != 1 {
		t.Fatalf("sections: %#v %v", sections, err)
	}
	settings, err := service.Settings(ctx)
	if err != nil || !settings.Configured || settings.SectionID != "1" {
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
