package emby

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRecentPlaybackAndEncryptedConfiguration(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, e := database.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	secrets, e := securestore.New(ctx, db, dir)
	if e != nil {
		t.Fatal(e)
	}
	svc := New(secrets)
	const token = "test-secret-emby-key"
	if e = svc.Save(ctx, Config{URL: "http://emby.example/emby", APIKey: token}); e != nil {
		t.Fatal(e)
	}
	var sealed []byte
	if e = db.QueryRow(`SELECT ciphertext FROM integration_configs WHERE name='emby'`).Scan(&sealed); e != nil || bytes.Contains(sealed, []byte(token)) {
		t.Fatal("credential was not encrypted", e)
	}
	settings, e := svc.Settings(ctx)
	if e != nil || !settings.Configured || settings.URL != "http://emby.example/emby" {
		t.Fatal(settings, e)
	}
	if e = svc.Save(ctx, Config{URL: "http://other.example"}); e == nil {
		t.Fatal("key reused for different server")
	}
	if e = svc.Save(ctx, Config{URL: "http://emby.example/emby"}); e != nil {
		t.Fatal(e)
	}
	svc.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("X-Emby-Token") != token || r.URL.Query().Get("api_key") != "" {
			t.Fatal("missing header or key exposed in URL")
		}
		var body string
		switch r.URL.Path {
		case "/emby/Users/Query":
			body = `{"Items":[{"Id":"abc123","Name":"Alex"}],"TotalRecordCount":1}`
		case "/emby/Users/abc123/Items":
			body = `{"Items":[{"Name":"Older","Type":"Movie","UserData":{"LastPlayedDate":"2025-01-01T00:00:00Z","PlayCount":2,"Played":true}},{"Name":"Pilot","SeriesName":"Series","Type":"Episode","ParentIndexNumber":1,"IndexNumber":2,"UserData":{"LastPlayedDate":"2026-01-01T00:00:00Z","PlayedPercentage":25}},{"Name":"Never watched","UserData":{}}],"TotalRecordCount":3}`
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	got, e := svc.Recent(ctx)
	if e != nil || len(got.Items) != 2 || got.Items[0].Title != "Pilot" || got.Items[0].User != "Alex" || got.Items[0].Progress != 25 || got.Items[1].PlayCount != 2 {
		t.Fatal(got, e)
	}
	svc.client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{"Location": []string{"http://other.example"}}}, nil
	})
	if _, e = svc.Recent(ctx); e == nil {
		t.Fatal("redirect accepted")
	}
	if e = svc.Delete(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Recent(ctx); e == nil {
		t.Fatal("deleted config still used")
	}
}
