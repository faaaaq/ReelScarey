package subtitle

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

const subdlFixture = `{"status":true,"results":[{"tmdb_id":27205,"type":"movie"}],"subtitles":[{"unpack_files":[{"file_n_id":"owned","name":"Owned.srt","release_name":"Owned release","language":"EN","hi":false,"format":"srt","size":100,"url":"/subtitle/parent/owned"}]}]}`

func TestSubDLCanonicalSearchSafeDownloadAndReview(t *testing.T) {
	p := NewSubDL()
	calls := 0
	p.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host == "api.subdl.com" {
			if r.URL.Query().Get("api_key") != "private-key" || r.URL.Query().Get("tmdb_id") != "27205" || r.URL.Query().Get("unpack") != "1" || r.URL.Query().Has("file_name") {
				t.Error("search boundary")
			}
			return response(200, subdlFixture), nil
		}
		if r.URL.Host != "dl.subdl.com" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "" || r.Header.Get("x-api-key") != "" {
			t.Error("download leaked credentials")
		}
		return response(200, validSRT), nil
	})
	ctx := context.Background()
	target := Target{Kind: "movie", TMDbID: 27205, Release: "Owned release"}
	items, e := p.Search(ctx, testCredentials, target, []string{"en"})
	if e != nil || len(items) != 1 {
		t.Fatal(items, e)
	}
	ranked := rank(Preferences{Tracks: []Track{{"en", "full"}}, MatchPolicy: "hash_or_release", DownloadFormat: "srt"}, target, items)
	if len(ranked) != 1 || !ranked[0].CanDownload || ranked[0].Automatic {
		t.Fatal("SubDL must require review", ranked)
	}
	d, e := p.Fetch(ctx, testCredentials, ranked[0])
	if e != nil || string(d.Content) != validSRT {
		t.Fatal(e)
	}
	for _, raw := range []string{"https://other.invalid/subtitle/a/b", "//dl.subdl.com/subtitle/a/b", "https://dl.subdl.com/subtitle/a/b?api_key=secret", "/subtitle/../bad", "/subtitle/a/b.zip", "/subtitle/a/b#x", "/subtitle/a%2fb/c"} {
		v := ranked[0]
		v.DownloadURL = raw
		if _, e = p.Fetch(ctx, testCredentials, v); e == nil {
			t.Fatal("unsafe URL", raw)
		}
	}
	if calls != 2 {
		t.Fatal("rejected URLs made network requests", calls)
	}
	p.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		return response(200, strings.ReplaceAll(subdlFixture, "27205", "999")), nil
	})
	if items, e = p.Search(ctx, testCredentials, target, []string{"en"}); e != nil || len(items) != 0 {
		t.Fatal("fallback identity accepted", e)
	}
}
func TestSubDLSwitchKeepsSecretsScopedAndInvalidatesOldChoices(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.s.subdl.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) { return response(200, subdlFixture), nil })
	if e := f.s.Configure(ctx, CredentialUpdate{Provider: "subdl"}); e == nil {
		t.Fatal("old provider key reused")
	}
	key := "subdl-private-key"
	if e := f.s.Configure(ctx, CredentialUpdate{Provider: "subdl", APIKey: &key}); e != nil {
		t.Fatal(e)
	}
	c, e := f.s.credentials(ctx)
	if e != nil || c.Provider != "subdl" || c.APIKey != key || c.Password != "" || c.Username != "" {
		t.Fatal("wrong credential scope", e)
	}
	status, e := f.s.Status(ctx)
	if e != nil || status.Provider != "subdl" {
		t.Fatal(status, e)
	}
	if e = f.s.Configure(ctx, CredentialUpdate{Username: testCredentials.Username, UserAgent: testCredentials.UserAgent}); e == nil {
		t.Fatal("SubDL key reused for OpenSubtitles")
	}
	var raw []byte
	if e = f.db.QueryRow(`SELECT ciphertext FROM integration_configs WHERE name='opensubtitles'`).Scan(&raw); e != nil || strings.Contains(string(raw), key) {
		t.Fatal("plaintext key", e)
	}
}

func TestSubDLRejectsWrongEpisodesAndHonorsCooldown(t *testing.T) {
	p := NewSubDL()
	episode := 1
	p.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("type") != "tv" || r.URL.Query().Get("season_number") != "2" || r.URL.Query().Get("episode_number") != "1" {
			t.Error("canonical episode query")
		}
		body := strings.ReplaceAll(subdlFixture, `"type":"movie"`, `"type":"tv"`)
		body = strings.ReplaceAll(body, `"language":"EN"`, fmt.Sprintf(`"season":2,"episode":%d,"language":"EN"`, episode))
		return response(200, body), nil
	})
	target := Target{Kind: "series", TMDbID: 27205, Episodes: []Episode{{Season: 2, Number: 1}}}
	items, e := p.Search(context.Background(), testCredentials, target, []string{"en"})
	if e != nil || len(items) != 1 {
		t.Fatal(items, e)
	}
	episode = 2
	items, e = p.Search(context.Background(), testCredentials, target, []string{"en"})
	if e != nil || len(items) != 0 {
		t.Fatal("wrong episode accepted", e)
	}
	p.http.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		r := response(429, `private-key`)
		r.Header.Set("Retry-After", "120")
		return r, nil
	})
	_, e = p.Search(context.Background(), testCredentials, target, []string{"en"})
	var limited *Error
	if !errors.As(e, &limited) || limited.Code != "provider_rate_limit" || limited.RetryAt < time.Now().Add(110*time.Second).Unix() || strings.Contains(e.Error(), "private-key") {
		t.Fatal("invalid cooldown", e)
	}
}

func TestProviderSwitchRetainsPendingBytesAndOriginalProvenance(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	enable(t, f, true, Track{"en", "full"})
	m, e := f.s.media(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	candidate := f.p.matches[0]
	candidate.Provider = "subdl"
	if _, e = f.s.plan(ctx, m, candidate, []byte(validSRT)); e != nil {
		t.Fatal(e)
	}
	// The current connection is OpenSubtitles; a colliding provider file number
	// must not make a new choice appear to replace the pending SubDL import.
	if e = f.s.Search(ctx, 1); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	rows, e := f.s.candidates(ctx, 1)
	if e != nil || len(rows) == 0 {
		t.Fatal(e)
	}
	if e = f.s.Select(ctx, rows[0].ID, true); e == nil {
		t.Fatal("cross-provider pending selection accepted")
	}
	if e = f.s.request(ctx, 1, "automatic", ""); e != nil {
		t.Fatal(e)
	}
	reopen(t, &f)
	if _, e = f.s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	var provider, state string
	if e = f.db.QueryRow(`SELECT provider,state FROM subtitles WHERE media_file_id=1 AND language='en' AND variant='full'`).Scan(&provider, &state); e != nil || provider != "subdl" || state != "available" {
		t.Fatal("lost original provider", provider, state, e)
	}
	if f.p.downloads != 0 {
		t.Fatal("journaled bytes downloaded again")
	}
}
