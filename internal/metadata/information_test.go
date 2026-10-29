package metadata

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestExtendedMovieAndTVInformation(t *testing.T) {
	for _, kind := range []string{"movie", "series"} {
		t.Run(kind, func(t *testing.T) {
			client := newClient("https://api.themoviedb.org/3", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				expected := "credits,external_ids,videos"
				path := "/3/movie/11"
				if kind == "series" {
					expected = "aggregate_credits,external_ids,videos"
					path = "/3/tv/11"
				}
				if r.URL.Path != path || r.URL.Query().Get("append_to_response") != expected || r.Header.Get("Authorization") != "Bearer "+token {
					t.Error("incorrect extended metadata request")
				}
				return response(200, `{"id":11,"tagline":"A fixture","genres":[{"name":"Drama"}],"vote_average":8.2,"vote_count":10,"external_ids":{"imdb_id":"tt1234567"},"created_by":[{"id":9,"name":"Creator"}],"credits":{"cast":[{"id":1,"name":"Actor","character":"Lead","profile_path":"/actor.jpg","order":1},{"id":1,"name":"Duplicate","order":2},{"id":-1,"name":"Invalid"}],"crew":[{"id":2,"name":"Director","job":"Director"},{"id":3,"name":"Writer","job":"Screenplay"}]},"aggregate_credits":{"cast":[{"id":4,"name":"TV actor","roles":[{"character":"TV lead"}]}],"crew":[{"id":5,"name":"TV writer","jobs":[{"job":"Writer"},{"job":"Writer"},{"job":"Unwanted"}]}]},"videos":{"results":[{"site":"YouTube","type":"Trailer","key":"abcdefghijk","official":false},{"site":"YouTube","type":"Trailer","key":"0123456789_","official":true}]}}`), nil
			})})
			info, e := client.Information(context.Background(), token, kind, 11)
			if e != nil || info.IMDbURL != "https://www.imdb.com/title/tt1234567/" || info.TrailerURL != "https://www.youtube.com/watch?v=0123456789_" || len(info.Cast) != 1 || info.Rating != 8.2 {
				t.Fatal(info, e)
			}
			if kind == "series" && (info.Cast[0].Role != "TV lead" || len(info.Crew) != 2) {
				t.Fatal("aggregate roles/creator missing or duplicate", info)
			}
			if kind == "movie" && (info.Cast[0].Role != "Lead" || len(info.Crew) != 3) {
				t.Fatal("movie credits missing", info)
			}
		})
	}
}
func TestInformationRejectsHostileLinksAndWrongIdentity(t *testing.T) {
	for _, body := range []string{
		`{"id":12}`, `{"id":11,"external_ids":{"imdb_id":"https://evil.invalid"},"videos":{"results":[{"site":"YouTube","type":"Trailer","key":"javascript:bad"}]},"credits":{"cast":[{"id":1,"name":"Actor","profile_path":"//evil.invalid/x.jpg"}]}}`,
	} {
		c := newClient("https://api.themoviedb.org/3", &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { return response(200, body), nil })})
		info, e := c.Information(context.Background(), token, "movie", 11)
		if strings.Contains(body, `"id":12`) {
			if e == nil {
				t.Fatal("wrong identity accepted")
			}
			continue
		}
		if e != nil || info.IMDbURL != "" || info.TrailerURL != "" || info.Cast[0].Image != "" {
			t.Fatal("unsafe metadata link accepted", info, e)
		}
	}
}
