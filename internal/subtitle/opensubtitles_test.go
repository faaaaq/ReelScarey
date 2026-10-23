package subtitle

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

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

var testCredentials = Credentials{APIKey: "private-key", Username: "fixture", Password: "private-password", UserAgent: "ReelScarey v0.7.0", Generation: "fixture"}

func protocol(t *testing.T, fn func(*http.Request) *http.Response) *OpenSubtitles {
	t.Helper()
	p := NewOpenSubtitles()
	p.spacing = 0
	p.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) { return fn(r), nil })
	return p
}
func requireCode(t *testing.T, e error, code string) *Error {
	t.Helper()
	var p *Error
	if !errors.As(e, &p) || p.Code != code {
		t.Fatalf("expected %s, got %v", code, e)
	}
	return p
}
func TestOpenSubtitlesContractAndCredentialIsolation(t *testing.T) {
	requests := 0
	p := protocol(t, func(r *http.Request) *http.Response {
		requests++
		if r.Header.Get("User-Agent") != testCredentials.UserAgent {
			t.Fatal("missing registered user agent")
		}
		if r.URL.Host == "dl.opensubtitles.com" {
			if r.Header.Get("Api-Key") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
				t.Fatal("credentials leaked to CDN")
			}
			return response(200, validSRT)
		}
		if r.Header.Get("Api-Key") != testCredentials.APIKey {
			t.Fatal("API key missing")
		}
		switch r.URL.Path {
		case "/api/v1/login":
			if r.Method != "POST" || r.URL.Host != "api.opensubtitles.com" || r.Header.Get("Authorization") != "" {
				t.Fatal(r.Method, r.URL)
			}
			var body map[string]string
			if e := json.NewDecoder(r.Body).Decode(&body); e != nil || body["password"] != testCredentials.Password || body["username"] != "fixture" {
				t.Fatal("login contract", e)
			}
			return response(200, `{"token":"private-jwt","base_url":"vip-api.opensubtitles.com"}`)
		case "/api/v1/subtitles":
			if r.URL.Host != "vip-api.opensubtitles.com" || r.Header.Get("Authorization") != "Bearer private-jwt" {
				t.Fatal("VIP login not applied")
			}
			q := r.URL.Query()
			if q.Get("languages") != "en,ja" || q.Get("tmdb_id") != "123" || q.Get("type") != "movie" || q.Get("moviehash") != "0123456789abcdef" || q.Get("ai_translated") != "exclude" || q.Get("machine_translated") != "exclude" || r.URL.RawQuery != q.Encode() {
				t.Fatal(q)
			}
			return response(200, `{"data":[{"id":"1","attributes":{"language":"eng","release":"Signs.2002.WEB","moviehash_match":true,"feature_details":{"feature_type":"Movie","tmdb_id":"123"},"files":[{"file_id":45,"file_name":"Signs.2002.en.srt"}]}},{"id":"2","attributes":{"language":"en","release":"Signs.2002.Signs.Songs","foreign_parts_only":true,"feature_details":{"feature_type":"Movie","tmdb_id":123},"files":[{"file_id":"46","file_name":"Signs.2002.signs.srt"}]}},{"id":"3","attributes":{"language":"en","release":"Signs.2002.WEB","foreign_parts_only":true,"feature_details":{"feature_type":"Movie","tmdb_id":123},"files":[{"file_id":47,"file_name":"Signs.2002.forced.srt"}]}}]}`)
		case "/api/v1/download":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if r.Method != "POST" || body["file_id"] != float64(45) || body["sub_format"] != "srt" {
				t.Fatal(body)
			}
			return response(200, `{"link":"https://dl.opensubtitles.com/download/private-signed-link","remaining":3,"reset_time_utc":"`+time.Now().Add(time.Hour).UTC().Format(time.RFC3339)+`"}`)
		}
		t.Fatalf("unexpected protocol request %s", r.URL)
		return nil
	})
	candidates, e := p.Search(context.Background(), testCredentials, Target{Kind: "movie", TMDbID: 123, Title: "Signs", MovieHash: "0123456789abcdef"}, []string{"ja", "en"})
	if e != nil || len(candidates) != 3 {
		t.Fatal(candidates, e)
	}
	for i, variant := range []string{"full", "signs", "forced"} {
		if candidates[i].Variant != variant || !candidates[i].Identity || candidates[i].Language != "en" {
			t.Fatal(candidates[i])
		}
	}
	d, e := p.Fetch(context.Background(), testCredentials, candidates[0])
	if e != nil || string(d.Content) != validSRT || d.Remaining == nil || *d.Remaining != 3 || d.ResetAt <= time.Now().Unix() {
		t.Fatal(d, e)
	}
	if requests != 4 {
		t.Fatal("session was not reused", requests)
	}
}
func TestOpenSubtitlesTVIdentityAndExclusions(t *testing.T) {
	p := protocol(t, func(r *http.Request) *http.Response {
		if strings.HasSuffix(r.URL.Path, "/login") {
			return response(200, `{"token":"jwt","base_url":"api.opensubtitles.com"}`)
		}
		q := r.URL.Query()
		if q.Get("parent_tmdb_id") != "900" || q.Get("season_number") != "2" || q.Get("episode_number") != "3" || q.Get("type") != "episode" || q.Has("tmdb_id") {
			t.Fatal(q)
		}
		return response(200, `{"data":[{"id":"1","attributes":{"language":"en","release":"Show.S02E03","feature_details":{"feature_type":"Episode","parent_tmdb_id":900,"season_number":2,"episode_number":3},"files":[{"file_id":1,"file_name":"ep.srt"}]}},{"id":"2","attributes":{"language":"en","release":"Show.S02E04","feature_details":{"feature_type":"Episode","parent_tmdb_id":900,"season_number":2,"episode_number":4},"files":[{"file_id":2,"file_name":"ep.srt"}]}},{"id":"3","attributes":{"language":"en","release":"Show.S02E03","ai_translated":true,"nb_cd":2,"feature_details":{"feature_type":"Episode","tmdb_id":903},"files":[{"file_id":3,"file_name":"ep.srt"}]}}]}`)
	})
	out, e := p.Search(context.Background(), testCredentials, Target{Kind: "series", TMDbID: 900, Episodes: []Episode{{TMDbID: 903, Season: 2, Number: 3}}}, []string{"en"})
	if e != nil || len(out) != 3 || !out[0].Identity || out[1].Identity || !out[2].Translated || !out[2].Multipart {
		t.Fatal(out, e)
	}
}
func TestOpenSubtitlesAuthenticationFailuresDoNotLoop(t *testing.T) {
	calls := 0
	p := protocol(t, func(r *http.Request) *http.Response { calls++; return response(401, `{"message":"private-password"}`) })
	for range 3 {
		_, e := p.Search(context.Background(), testCredentials, Target{Kind: "movie"}, []string{"en"})
		requireCode(t, e, "provider_auth")
		if strings.Contains(e.Error(), "private-password") {
			t.Fatal("upstream body disclosed")
		}
	}
	if calls != 1 {
		t.Fatal("repeated rejected login", calls)
	}
	_ = p.Test(context.Background(), testCredentials)
	if calls != 2 {
		t.Fatal("explicit test did not retry")
	}
	login, api := 0, 0
	p = protocol(t, func(r *http.Request) *http.Response {
		if strings.HasSuffix(r.URL.Path, "/login") {
			login++
			return response(200, `{"token":"jwt","base_url":"api.opensubtitles.com"}`)
		}
		api++
		return response(401, `{}`)
	})
	_, e := p.Search(context.Background(), testCredentials, Target{Kind: "movie"}, []string{"en"})
	requireCode(t, e, "provider_auth")
	if login != 2 || api != 2 {
		t.Fatal("token refresh was unbounded", login, api)
	}
}
func TestOpenSubtitlesBlocksUnsafeDownloadLocationsAndRedirects(t *testing.T) {
	for _, location := range []string{"http://dl.opensubtitles.com/x", "https://127.0.0.1/x", "https://dl.opensubtitles.com.evil.invalid/x", "https://user@dl.opensubtitles.com/x", "https://dl.opensubtitles.com:444/x", "https://dl.opensubtitles.com/x#fragment"} {
		if _, e := downloadURL(location); e == nil {
			t.Fatal("unsafe location accepted", location)
		}
	}
	calls := 0
	p := protocol(t, func(r *http.Request) *http.Response {
		calls++
		switch r.URL.Path {
		case "/api/v1/login":
			return response(200, `{"token":"jwt","base_url":"api.opensubtitles.com"}`)
		case "/api/v1/download":
			return response(200, `{"link":"https://dl.opensubtitles.com/x"}`)
		default:
			v := response(302, "")
			v.Header.Set("Location", "http://127.0.0.1/private")
			return v
		}
	})
	_, e := p.Fetch(context.Background(), testCredentials, Candidate{FileID: 1})
	requireCode(t, e, "unsafe_download")
	if calls != 3 {
		t.Fatal("unsafe redirect followed")
	}
	p = protocol(t, func(*http.Request) *http.Response {
		return response(200, `{"token":"jwt","base_url":"http://127.0.0.1"}`)
	})
	requireCode(t, p.Test(context.Background(), testCredentials), "provider_response")
}
func TestOpenSubtitlesQuotaRateLimitsAndBoundedResponses(t *testing.T) {
	for _, code := range []int{406, 429, 503, 302} {
		p := protocol(t, func(*http.Request) *http.Response {
			v := response(code, `{"reset_time_utc":"`+time.Now().Add(2*time.Hour).UTC().Format(time.RFC3339)+`"}`)
			v.Header.Set("Retry-After", "120")
			v.Header.Set("Location", "https://example.invalid")
			return v
		})
		e := p.Test(context.Background(), testCredentials)
		switch code {
		case 406:
			v := requireCode(t, e, "provider_quota")
			if v.RetryAt < time.Now().Unix()+7100 {
				t.Fatal(v)
			}
		case 429:
			v := requireCode(t, e, "provider_rate_limit")
			if v.RetryAt < time.Now().Unix()+119 {
				t.Fatal(v)
			}
		case 503:
			var v *Error
			if !errors.As(e, &v) || !v.Temporary {
				t.Fatal(e)
			}
		case 302:
			requireCode(t, e, "provider_response")
		}
	}
	p := protocol(t, func(*http.Request) *http.Response { return response(200, strings.Repeat("x", (2<<20)+1)) })
	requireCode(t, p.Test(context.Background(), testCredentials), "provider_response")
	if _, e := readBounded(strings.NewReader(strings.Repeat("x", MaxSubtitle+1)), MaxSubtitle); e == nil {
		t.Fatal("unbounded subtitle accepted")
	}
}
