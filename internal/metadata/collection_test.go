package metadata

import (
	"context"
	"net/http"
	"testing"
)

func TestMovieCollection(t *testing.T) {
	client := newClient("https://tmdb.invalid/3", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/3/movie/11":
			return response(200, `{"id":11,"belongs_to_collection":{"id":5}}`), nil
		case "/3/collection/5":
			return response(200, `{"id":5,"name":"Fixture Collection","parts":[{"id":11,"title":"First","release_date":"2020-01-01"},{"id":12,"title":"Second"}]}`), nil
		default:
			t.Fatal("unexpected provider URL", r.URL.Path)
			return nil, nil
		}
	})})
	got, e := client.MovieCollection(context.Background(), token, 11)
	if e != nil || got.Name != "Fixture Collection" || len(got.Items) != 2 || got.Items[1].TMDbID != 12 {
		t.Fatal(got, e)
	}
}
