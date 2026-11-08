package music

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	testMBID        = "a74b1b7f-71a5-4011-9441-d0b5e4122711"
	testRGID        = "b1392450-e666-3926-a536-22c65f834433"
	testReleaseID   = "1834eae1-741b-3c03-9ca5-0df3decb43ea"
	testTrackID     = "65806a55-e874-3cde-92ba-8a5dc0dce42e"
	testRecordingID = "5229c9b3-8523-4b91-9a5f-c33e0c4eeb95"
)

func TestClientNormalizesMusicBrainz(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing user agent")
		}
		if r.URL.Query().Get("fmt") != "json" {
			t.Error("missing fmt=json")
		}
		switch {
		case r.URL.Path == "/ws/2/artist" && r.URL.Query().Get("query") != "":
			io.WriteString(w, `{"count":1,"offset":0,"artists":[{"id":"`+testMBID+`","name":"Radiohead","sort-name":"Radiohead","country":"GB","type":"Group","life-span":{"begin":"1991","ended":false}}]}`)
		case r.URL.Path == "/ws/2/artist/"+testMBID:
			io.WriteString(w, `{"id":"`+testMBID+`","name":"Radiohead","sort-name":"Radiohead","country":"GB","type":"Group","life-span":{"begin":"1991","ended":false}}`)
		case r.URL.Path == "/ws/2/release-group":
			io.WriteString(w, `{"release-group-count":1,"release-group-offset":0,"release-groups":[{"id":"`+testRGID+`","title":"OK Computer","primary-type":"Album","secondary-types":[],"first-release-date":"1997-05-21"}]}`)
		case r.URL.Path == "/ws/2/release" && r.URL.Query().Get("release-group") == testRGID:
			io.WriteString(w, `{"release-count":1,"release-offset":0,"releases":[{"id":"`+testReleaseID+`","title":"OK Computer","status":"Official","date":"1997-05-21"}]}`)
		case r.URL.Path == "/ws/2/release/"+testReleaseID:
			io.WriteString(w, `{"id":"`+testReleaseID+`","title":"OK Computer","media":[{"position":1,"tracks":[{"id":"`+testTrackID+`","position":1,"number":"1","title":"Airbag","length":284000,"recording":{"id":"`+testRecordingID+`","length":284000}}]}]}`)
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newClient(server.URL+"/ws/2", server.Client(), 0)
	ctx := context.Background()

	results, err := client.SearchArtists(ctx, "radiohead", 1)
	if err != nil || len(results.Items) != 1 || results.Items[0].Name != "Radiohead" || results.TotalResults != 1 {
		t.Fatal("search", results, err)
	}
	if results.Items[0].Overview != "Group · GB · 1991–" {
		t.Fatal("artist overview", results.Items[0].Overview)
	}
	artist, err := client.Artist(ctx, testMBID)
	if err != nil || artist.Name != "Radiohead" {
		t.Fatal("artist", artist, err)
	}
	albums, err := client.ArtistAlbums(ctx, testMBID)
	if err != nil || len(albums) != 1 || albums[0].Title != "OK Computer" || albums[0].Year == nil || *albums[0].Year != 1997 {
		t.Fatal("albums", albums, err)
	}
	if albums[0].CoverURL != "https://coverartarchive.org/release-group/"+testRGID+"/front-500" {
		t.Fatal("cover url", albums[0].CoverURL)
	}
	releases, err := client.ReleaseGroupReleases(ctx, testRGID)
	if err != nil || len(releases) != 1 || releases[0].ID != testReleaseID {
		t.Fatal("releases", releases, err)
	}
	tracks, err := client.ReleaseTracks(ctx, testReleaseID)
	if err != nil || len(tracks) != 1 || tracks[0].Disc != 1 || tracks[0].Number != 1 || tracks[0].DurationMS != 284000 || tracks[0].RecordingID != testRecordingID {
		t.Fatal("tracks", tracks, err)
	}
}

func TestClientSearchesAndLooksUpReleaseGroups(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/2/release-group" && r.URL.Query().Get("query") != "":
			query := r.URL.Query().Get("query")
			if !strings.Contains(query, "arid:"+testMBID) || !strings.Contains(query, `releasegroup:"OK Computer"`) {
				t.Errorf("release-group query %q", query)
			}
			io.WriteString(w, `{"count":1,"offset":0,"release-groups":[{"id":"`+testRGID+`","title":"OK Computer","primary-type":"Album","secondary-types":[],"first-release-date":"1997-05-21"}]}`)
		case r.URL.Path == "/ws/2/release-group/"+testRGID:
			if r.URL.Query().Get("inc") != "artist-credits" {
				t.Errorf("release-group lookup include %q", r.URL.Query().Get("inc"))
			}
			io.WriteString(w, `{"id":"`+testRGID+`","title":"OK Computer","primary-type":"Album","secondary-types":[],"first-release-date":"1997-05-21","artist-credit":[{"name":"Radiohead","artist":{"id":"`+testMBID+`","name":"Radiohead"}}]}`)
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newClient(server.URL+"/ws/2", server.Client(), 0)
	ctx := context.Background()

	albums, complete, err := client.SearchReleaseGroups(ctx, testMBID, "OK Computer")
	if err != nil || !complete || len(albums) != 1 || albums[0].Title != "OK Computer" || albums[0].Year == nil || *albums[0].Year != 1997 {
		t.Fatal("release-group search", albums, complete, err)
	}
	album, artistID, err := client.ReleaseGroup(ctx, testRGID)
	if err != nil || album.Title != "OK Computer" || artistID != testMBID {
		t.Fatal("release-group lookup", album, artistID, err)
	}
}

func TestClientRejectsRateLimitAndOversizedBodies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws/2/artist/"+testMBID {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, strings.Repeat("x", 2<<20+10))
	}))
	defer server.Close()
	client := newClient(server.URL+"/ws/2", server.Client(), 0)
	ctx := context.Background()

	var provider *Error
	if _, err := client.Artist(ctx, testMBID); !errors.As(err, &provider) || provider.Code != "music_rate_limited" || provider.RetryAfter != 2 {
		t.Fatal("rate limit", err)
	}
	provider = nil
	if _, err := client.SearchArtists(ctx, "radiohead", 1); !errors.As(err, &provider) || provider.Code != "music_invalid_response" {
		t.Fatal("oversized body", err)
	}
}

func TestClientRefusesProviderRedirects(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{}`)
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer server.Close()
	client := newClient(server.URL+"/ws/2", server.Client(), 0)
	var provider *Error
	if _, err := client.Artist(context.Background(), testMBID); !errors.As(err, &provider) || provider.Code != "music_unavailable" {
		t.Fatal("redirect accepted", err)
	}
}

func TestValidMBIDAndQuery(t *testing.T) {
	if !ValidMBID(testMBID) || ValidMBID("not-a-uuid") {
		t.Fatal("mbid validation")
	}
	if !ValidQuery("radiohead", 1) || ValidQuery("", 1) || ValidQuery(strings.Repeat("x", 201), 1) || ValidQuery("x", 0) {
		t.Fatal("query validation")
	}
}
