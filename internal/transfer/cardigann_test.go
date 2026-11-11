package transfer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const cardigannFixture = `
id: fixture
name: Fixture tracker
type: public
settings:
  - name: username
    label: Username
    type: text
    default: fixture
search:
  path: /search
  inputs:
    q: "{{ .Keywords }}"
  rows:
    selector: "tr.release"
  fields:
    title:
      selector: "a.title"
    details:
      selector: "a.title"
      attribute: href
    download:
      selector: "a.download"
      attribute: href
    size:
      selector: "td.size"
    seeders:
      selector: "td.seeders"
    leechers:
      selector: "td.leechers"
    date:
      selector: "td.date"
`

func TestCardigannSearchParsesRowsAndFilters(t *testing.T) {
	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query().Get("q")
		if r.Header.Get("Cookie") != "session=fixture" || r.Header.Get("User-Agent") != "fixture-agent" {
			t.Fatal("indexer headers were not applied")
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<table><tr class="release"><td><a class="title" href="/details/1">Example 2020 1080p</a></td><td class="size">1.5 GiB</td><td class="seeders">12</td><td class="leechers">3</td><td class="date">2026-01-02</td><td><a class="download" href="/download/1">Download</a></td></tr></table>`))
	}))
	defer server.Close()
	indexer := NewCardigann()
	items, err := indexer.Search(context.Background(), IndexerConfig{
		URL: server.URL, Kind: "cardigann", Definition: cardigannFixture,
		Cookie: "session=fixture", UserAgent: "fixture-agent",
	}, "Example")
	if err != nil || len(items) != 1 {
		t.Fatalf("search: %#v %v", items, err)
	}
	if query != "Example" || items[0].Title != "Example 2020 1080p" || items[0].Size != 1610612736 {
		t.Fatalf("parsed item: %#v (query %q)", items[0], query)
	}
	if items[0].Seeders == nil || *items[0].Seeders != 12 || items[0].Peers == nil || *items[0].Peers != 3 {
		t.Fatalf("peer counts: %#v", items[0])
	}
	if !strings.HasSuffix(items[0].URL, "/download/1") || !strings.HasSuffix(items[0].InfoURL, "/details/1") {
		t.Fatalf("links: %#v", items[0])
	}
	if items[0].Published != "2026-01-02T00:00:00Z" {
		t.Fatalf("date: %q", items[0].Published)
	}
}

func TestCardigannRejectsInvalidDefinitionAndForeignDownload(t *testing.T) {
	indexer := NewCardigann()
	if _, err := indexer.Search(context.Background(), IndexerConfig{URL: "https://example.com", Kind: "cardigann", Definition: "not: [yaml"}, "x"); err == nil {
		t.Fatal("invalid YAML accepted")
	}
	definition := strings.Replace(cardigannFixture, "/search", "https://evil.example/search", 1)
	if _, err := indexer.Search(context.Background(), IndexerConfig{URL: "https://example.com", Kind: "cardigann", Definition: definition}, "x"); err == nil {
		t.Fatal("foreign search host accepted")
	}
	if _, err := indexer.Search(context.Background(), IndexerConfig{URL: "https://example.com", Kind: "cardigann", Definition: strings.Replace(cardigannFixture, "search:", "login:\n  path: /login\nsearch:", 1)}, "x"); err == nil {
		t.Fatal("unsupported login definition accepted")
	}
	if _, err := indexer.Search(context.Background(), IndexerConfig{URL: "https://example.com", Kind: "cardigann", Definition: strings.Replace(cardigannFixture, "  rows:", "  unknown_field: true\n  rows:", 1)}, "x"); err == nil {
		t.Fatal("unknown YAML field accepted")
	}
}
