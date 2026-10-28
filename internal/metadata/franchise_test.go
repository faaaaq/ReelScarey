package metadata

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestWikidataFranchiseMatchesExactProviderIDsAndCaches(t *testing.T) {
	calls := 0
	c := newWikidataFranchises("https://query.wikidata.org/sparql", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		q := r.URL.Query().Get("query")
		if !strings.Contains(q, `wdt:P4947 "11"`) || !strings.Contains(q, "wdt:P8345") || !strings.Contains(q, "FILTER NOT EXISTS") || r.Header.Get("User-Agent") == "" {
			t.Fatal("bad query", q)
		}
		return response(200, `{"results":{"bindings":[{"seed":{"value":"http://www.wikidata.org/entity/Q1"},"franchise":{"value":"http://www.wikidata.org/entity/Q10"},"franchiseLabel":{"value":"Fixture saga"},"member":{"value":"http://www.wikidata.org/entity/Q2"},"memberLabel":{"value":"Fixture show"},"tv":{"value":"123"}},{"seed":{"value":"http://www.wikidata.org/entity/Q1"},"franchise":{"value":"http://www.wikidata.org/entity/Q10"},"franchiseLabel":{"value":"Fixture saga"},"member":{"value":"http://www.wikidata.org/entity/Q3"},"memberLabel":{"value":"Fixture film"},"movie":{"value":"456"}}]}}`), nil
	})})
	out, e := c.FindFranchise(context.Background(), "movie", 11)
	if e != nil || len(out.Items) != 2 || out.Items[1].Kind != "series" || out.Items[1].TMDbID != 123 {
		t.Fatal(out, e)
	}
	_, e = c.FindFranchise(context.Background(), "movie", 11)
	if e != nil || calls != 1 {
		t.Fatal("cache failed", calls, e)
	}
}
func TestWikidataRejectsAmbiguousIdentity(t *testing.T) {
	c := newWikidataFranchises("https://query.wikidata.org/sparql", &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
		return response(200, `{"results":{"bindings":[{"seed":{"value":"http://www.wikidata.org/entity/Q1"},"franchise":{"value":"http://www.wikidata.org/entity/Q2"},"franchiseLabel":{"value":"Saga"},"member":{"value":"http://www.wikidata.org/entity/Q3"},"memberLabel":{"value":"Film"},"movie":{"value":"123"}},{"seed":{"value":"http://www.wikidata.org/entity/Q4"},"franchise":{"value":"http://www.wikidata.org/entity/Q2"},"franchiseLabel":{"value":"Saga"},"member":{"value":"http://www.wikidata.org/entity/Q5"},"memberLabel":{"value":"Show"},"tv":{"value":"456"}}]}}`), nil
	})})
	if _, e := c.FindFranchise(context.Background(), "movie", 11); e == nil {
		t.Fatal("ambiguous seed accepted")
	}
}
