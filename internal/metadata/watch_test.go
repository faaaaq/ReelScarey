package metadata

import (
	"context"
	"net/http"
	"testing"
)

func TestWatchProvidersRegionAndUnsafeLinks(t *testing.T) {
	client := newClient("https://api.themoviedb.org/3", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/3/movie/11/watch/providers" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		return response(200, `{"results":{"US":{"link":"javascript:alert(1)","flatrate":[{"provider_name":"Example","logo_path":"/logo.jpg"}],"rent":[],"buy":[]},"GB":{"link":"https://www.themoviedb.org/movie/11","flatrate":[],"rent":[{"provider_name":"Rent","logo_path":"/r.png"}],"buy":[]}}}`), nil
	})})
	out, err := client.WatchProviders(context.Background(), token, 11, "US")
	if err != nil || out.Link != "" || len(out.Flatrate) != 1 || out.Flatrate[0].Name != "Example" {
		t.Fatalf("US providers: %#v %v", out, err)
	}
	if out.Flatrate[0].LogoURL != "https://image.tmdb.org/t/p/w92/logo.jpg" {
		t.Fatalf("logo URL: %q", out.Flatrate[0].LogoURL)
	}
	gb, err := client.WatchProviders(context.Background(), token, 11, "GB")
	if err != nil || gb.Link == "" || len(gb.Rent) != 1 {
		t.Fatalf("GB providers: %#v %v", gb, err)
	}
	missing, err := client.WatchProviders(context.Background(), token, 11, "JP")
	if err != nil || len(missing.Flatrate) != 0 || missing.Link != "" {
		t.Fatalf("missing region: %#v %v", missing, err)
	}
}
