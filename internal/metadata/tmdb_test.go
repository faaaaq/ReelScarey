package metadata

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

const token = "test-provider-token-for-unit-tests-only"

func TestTMDbSearchDetailsAndValidate(t *testing.T) {
	client := newClient("https://api.themoviedb.org/3", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer "+token || strings.Contains(r.URL.RawQuery, token) {
			t.Fatal("credential transport is unsafe")
		}
		switch r.URL.Path {
		case "/3/authentication":
			return response(200, `{"success":true}`), nil
		case "/3/search/movie":
			if r.URL.Query().Get("query") != "A & B" || r.URL.Query().Get("include_adult") != "false" || r.URL.Query().Get("page") != "2" {
				t.Fatal("incorrect search request")
			}
			return response(200, `{"page":2,"total_pages":3,"total_results":41,"results":[{"id":11,"title":"A & B","overview":"A test fixture","poster_path":"/poster.jpg","release_date":"2020-01-02"}]}`), nil
		case "/3/movie/11":
			if r.URL.Query().Get("append_to_response") != "release_dates" {
				t.Fatal("release dates were not requested")
			}
			return response(200, `{"id":11,"title":"A & B","poster_path":"/poster.jpg","release_date":"2020-01-02","runtime":101}`), nil
		default:
			t.Fatalf("unexpected endpoint: %s", r.URL.Path)
			return nil, nil
		}
	})})
	if err := client.Validate(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	results, err := client.Search(context.Background(), token, "A & B", 2)
	if err != nil || len(results.Items) != 1 || results.Items[0].PosterURL != "https://image.tmdb.org/t/p/w500/poster.jpg" {
		t.Fatalf("bad search: %+v %v", results, err)
	}
	movie, err := client.Movie(context.Background(), token, 11)
	if err != nil || movie.Year == nil || *movie.Year != 2020 || movie.RuntimeMinutes == nil || *movie.RuntimeMinutes != 101 {
		t.Fatalf("bad details: %+v %v", movie, err)
	}
}
func TestTMDbReleaseDatesParseTheatricalDigitalAndPhysical(t *testing.T) {
	client := newClient("https://api.themoviedb.org/3", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/3/movie/12" || r.URL.Query().Get("append_to_response") != "release_dates" {
			t.Fatalf("unexpected release-date request: %s", r.URL.String())
		}
		return response(200, `{"id":12,"title":"Three Dates","release_date":"2026-05-01",
			"release_dates":{"results":[
				{"iso_3166_1":"GB","release_dates":[{"type":3,"release_date":"2026-05-20T00:00:00.000Z"},{"type":4,"release_date":"2026-06-01T00:00:00.000Z"},{"type":5,"release_date":"2026-07-01T00:00:00.000Z"}]},
				{"iso_3166_1":"US","release_dates":[{"type":1,"release_date":"2026-04-20T00:00:00.000Z"},{"type":2,"release_date":"2026-05-15T00:00:00.000Z"},{"type":3,"release_date":"2026-05-10T00:00:00.000Z"},{"type":4,"release_date":"2026-06-02T00:00:00.000Z"},{"type":5,"release_date":"2026-07-03T00:00:00.000Z"}]},
				{"iso_3166_1":"FR","release_dates":[{"type":4,"release_date":"not-a-date"}]}
			]}}`), nil
	})})
	movie, e := client.Movie(context.Background(), token, 12)
	if e != nil {
		t.Fatal(e)
	}
	if movie.ReleaseDate != "2026-05-01" || movie.TheatricalReleaseDate != "2026-05-10" || movie.DigitalReleaseDate != "2026-06-02" || movie.PhysicalReleaseDate != "2026-07-03" {
		t.Fatalf("release-dates parsing: %+v", movie)
	}
}

func TestTMDbFailureSafety(t *testing.T) {
	for _, test := range []struct {
		status     int
		body, code string
	}{{401, token, "tmdb_unauthorized"}, {404, token, "movie_not_found"}, {500, token, "tmdb_unavailable"}, {200, "{", "tmdb_invalid_response"}, {200, `{"id":99,"title":"Wrong movie"}`, "tmdb_invalid_response"}, {200, strings.Repeat("x", (2<<20)+1), "tmdb_invalid_response"}} {
		client := newClient("https://api.themoviedb.org/3", &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { return response(test.status, test.body), nil })})
		_, err := client.Movie(context.Background(), token, 11)
		var provider *Error
		if !errors.As(err, &provider) || provider.Code != test.code || strings.Contains(err.Error(), token) {
			t.Fatalf("unsafe/incorrect error: %v", err)
		}
	}
	calls := 0
	client := newClient("https://api.themoviedb.org/3", &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		out := response(429, token)
		out.Header.Set("Retry-After", "30")
		return out, nil
	})})
	for range 2 {
		_, err := client.Movie(context.Background(), token, 11)
		var e *Error
		if !errors.As(err, &e) || e.Code != "tmdb_rate_limited" || e.RetryAfter < 1 {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatal("Retry-After cooldown not enforced")
	}
	client = newClient("https://api.themoviedb.org/3", &http.Client{Timeout: time.Millisecond, Transport: transportFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })})
	if _, err := client.Movie(context.Background(), token, 11); err == nil {
		t.Fatal("timeout not handled")
	}
}
func TestImageAndSearchBounds(t *testing.T) {
	for _, path := range []string{"https://evil.test/x.jpg", "//evil.test/a.jpg", "/../evil.jpg", "/x.svg", "/x.jpg?token=secret"} {
		if PosterURL(path) != "" {
			t.Fatal("unsafe poster path accepted")
		}
	}
	for _, query := range []string{"", strings.Repeat("x", 201), "hello\nworld"} {
		if ValidQuery(query, 1) {
			t.Fatal("invalid query accepted")
		}
	}
}

func TestRedirectCannotForwardCredentials(t *testing.T) {
	calls := 0
	client := newClient("https://api.themoviedb.org/3", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "api.themoviedb.org" {
			t.Fatal("credentials sent to a redirect destination")
		}
		out := response(302, "")
		out.Header.Set("Location", "https://evil.example/steal")
		return out, nil
	})})
	if _, err := client.Movie(context.Background(), token, 11); err == nil {
		t.Fatal("redirect accepted")
	}
	if calls != 1 {
		t.Fatal("redirect was followed")
	}
}
