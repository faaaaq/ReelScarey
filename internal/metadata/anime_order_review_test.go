package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const reviewAnimeMedia = `{"id":41,"type":"ANIME","format":"OVA","episodes":3,"startDate":{"year":2021},"title":{"romaji":"A & B","english":"English title","native":"作品"}}`

func TestReviewAniListUsesLiteralVariablesAndScopedIdentity(t *testing.T) {
	for _, search := range []bool{true, false} {
		calls := 0
		c := newAniList("https://provider.invalid/graphql", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Authorization") != "" {
				t.Fatal("unexpected provider request", r.Method, r.Header)
			}
			var in struct {
				Query     string
				Variables map[string]any
			}
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Fatal(err)
			}
			if search {
				if in.Variables["search"] != `A & B" { id }` || strings.Contains(in.Query, `A & B`) {
					t.Fatal("query text interpolated", in)
				}
				return response(200, `{"data":{"Page":{"media":[`+reviewAnimeMedia+`]}}}`), nil
			}
			if in.Variables["id"] != float64(41) {
				t.Fatal("incorrect identity", in)
			}
			return response(200, `{"data":{"Media":`+reviewAnimeMedia+`}}`), nil
		})})
		var got AnimeIdentity
		if search {
			items, err := c.SearchAnime(context.Background(), `A & B" { id }`)
			if err != nil || len(items) != 1 {
				t.Fatal(items, err)
			}
			got = items[0]
		} else {
			var err error
			got, err = c.Anime(context.Background(), 41)
			if err != nil {
				t.Fatal(err)
			}
		}
		if calls != 1 || got.Provider != "anilist" || got.ID != 41 || got.URL != "https://anilist.co/anime/41" || got.Format != "OVA" || got.Episodes == nil || *got.Episodes != 3 {
			t.Fatal(got, calls)
		}
	}
}

func TestReviewAniListRejectsUntrustedIdentitiesAndMalformedEnvelopes(t *testing.T) {
	for _, body := range []string{
		`{"data":{"Media":` + strings.Replace(reviewAnimeMedia, `"id":41`, `"id":42`, 1) + `}}`,
		`{"data":{"Media":` + strings.Replace(reviewAnimeMedia, `"ANIME"`, `"MANGA"`, 1) + `}}`,
		`{"data":{"Media":` + strings.Replace(reviewAnimeMedia, `"OVA"`, `"OVA\nsecret"`, 1) + `}}`,
		`{"data":{"Media":null}}`, `{"data":null}`, `{"errors":[{"message":"sensitive remote text"}],"data":{}}`,
		`{"data":{"Media":` + strings.Replace(reviewAnimeMedia, `"episodes":3`, `"episodes":-1`, 1) + `}}`,
		strings.Repeat("x", (1<<20)+1),
	} {
		c := newAniList("https://provider.invalid/graphql", &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { return response(200, body), nil })})
		_, err := c.Anime(context.Background(), 41)
		var p *Error
		if !errors.As(err, &p) || p.Code != "anilist_invalid_response" || strings.Contains(err.Error(), "sensitive") {
			t.Fatalf("unsafe provider acceptance: %v", err)
		}
	}
	for _, body := range []string{`{"data":{"Page":{"media":[` + reviewAnimeMedia + `,` + reviewAnimeMedia + `]}}}`, `{"data":{"Page":{"media":[` + strings.Replace(reviewAnimeMedia, `"ANIME"`, `"MANGA"`, 1) + `]}}}`} {
		c := newAniList("https://provider.invalid/graphql", &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { return response(200, body), nil })})
		if _, err := c.SearchAnime(context.Background(), "A"); err == nil {
			t.Fatal("accepted duplicate or non-Anime search result")
		}
	}
}

func TestReviewAniListInvalidInputCooldownAndCancellationDoNotContactProvider(t *testing.T) {
	calls := 0
	c := newAniList("https://provider.invalid/graphql", &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		out := response(429, "remote-sensitive-body")
		out.Header.Set("Retry-After", "120")
		return out, nil
	})})
	for _, q := range []string{"", "\n", "bad\x00title", strings.Repeat("a", 201)} {
		if _, err := c.SearchAnime(context.Background(), q); err == nil {
			t.Fatal("accepted query", q)
		}
	}
	for _, id := range []int64{0, -1, 2147483648} {
		if _, err := c.Anime(context.Background(), id); err == nil {
			t.Fatal("accepted ID", id)
		}
	}
	if calls != 0 {
		t.Fatal("invalid input contacted provider")
	}
	for range 2 {
		_, err := c.Anime(context.Background(), 41)
		var p *Error
		if !errors.As(err, &p) || p.Code != "anilist_rate_limited" || p.RetryAfter < 60 || strings.Contains(err.Error(), "sensitive") {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatal("remote cooldown ignored", calls)
	}
	c.next = time.Now().Add(time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Anime(ctx, 41); !errors.Is(err, context.Canceled) {
		t.Fatal("spacing wait ignored cancellation", err)
	}
	if calls != 1 {
		t.Fatal("canceled spacing wait contacted provider")
	}
}

func TestReviewAniListDoesNotFollowRedirects(t *testing.T) {
	calls := 0
	c := newAniList("https://provider.invalid/graphql", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		out := response(302, "")
		out.Header.Set("Location", "https://different.invalid/sensitive")
		return out, nil
	})})
	if _, err := c.Anime(context.Background(), 41); err == nil || calls != 1 {
		t.Fatal("redirect followed", calls, err)
	}
}

func TestReviewEpisodeGroupVerifiesMembershipBeforeDetailsAndForeignEpisodeIDs(t *testing.T) {
	for _, tc := range []struct {
		name, list, details string
		wantCalls           int
		wantOK              bool
	}{
		{"foreign group", `{"id":100,"results":[]}`, ``, 1, false},
		{"wrong parent", `{"id":101,"results":[]}`, ``, 1, false},
		{"foreign episode", `{"id":100,"results":[{"id":"group","name":"Alternative","type":2,"episode_count":1,"group_count":1}]}`, `{"id":"group","name":"Alternative","groups":[{"id":"sub","order":0,"episodes":[{"id":10,"show_id":101,"season_number":0,"episode_number":1,"order":0}]}]}`, 2, false},
		{"valid special", `{"id":100,"results":[{"id":"group","name":"Alternative","type":2,"episode_count":1,"group_count":1}]}`, `{"id":"group","name":"Alternative","groups":[{"id":"sub","order":0,"episodes":[{"id":10,"show_id":100,"season_number":0,"episode_number":1,"order":0}]}]}`, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := newClient("https://provider.invalid/3", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Authorization") != "Bearer "+token {
					t.Fatal("missing token")
				}
				if r.URL.Path == "/3/tv/100/episode_groups" {
					return response(200, tc.list), nil
				}
				if r.URL.Path == "/3/tv/episode_group/group" {
					return response(200, tc.details), nil
				}
				t.Fatal("unexpected endpoint", r.URL.Path)
				return nil, io.EOF
			})})
			got, err := c.EpisodeGroup(context.Background(), token, 100, "group")
			if (err == nil) != tc.wantOK || calls != tc.wantCalls {
				t.Fatal(got, err, calls)
			}
		})
	}
}
