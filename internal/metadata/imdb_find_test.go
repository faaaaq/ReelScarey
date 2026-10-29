package metadata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFindIMDbUsesExactExternalIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/find/tt0133093" || r.URL.Query().Get("external_source") != "imdb_id" {
			t.Errorf("incorrect query: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"movie_results":[{"id":603,"title":"The Matrix"}],"tv_results":[]}`))
	}))
	defer server.Close()
	client := newClient(server.URL, server.Client())
	got, err := client.FindIMDb(context.Background(), "test-provider-token-for-api-tests", "tt0133093")
	if err != nil || got.Kind != "movie" || got.TMDbID != 603 {
		t.Fatal(got, err)
	}
	if _, err = client.FindIMDb(context.Background(), "test-provider-token-for-api-tests", "../../bad"); err == nil {
		t.Fatal("invalid external id accepted")
	}
}
