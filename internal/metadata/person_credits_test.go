package metadata

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestPersonCreditsSortsFiltersAndClassifiesAnime(t *testing.T) {
	client := newClient("https://api.themoviedb.org/3", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/3/person/7/combined_credits" || r.URL.Query().Get("language") != "en-US" {
			t.Fatal("unexpected person credits request", r.URL)
		}
		items := []string{
			`{"id":1,"media_type":"movie","title":"Recent Movie","release_date":"2026-01-02","character":"Lead","poster_path":"/a.jpg","popularity":5,"genre_ids":[16],"original_language":"ja"}`,
			`{"id":2,"media_type":"tv","name":"Western Show","first_air_date":"2025-01-01","character":"Voice","poster_path":"/b.jpg","popularity":4,"genre_ids":[16],"original_language":"en","origin_country":["US"]}`,
			`{"id":3,"media_type":"movie","title":"Older Movie","release_date":"2010-01-01","character":"Support","popularity":1}`,
			`{"id":4,"media_type":"movie","title":"Adult","release_date":"2026-02-01","adult":true}`,
			`{"id":1,"media_type":"movie","title":"Duplicate","release_date":"2026-03-01"}`,
		}
		for n := 0; n < 25; n++ {
			items = append(items, fmt.Sprintf(`{"id":%d,"media_type":"movie","title":"Filler %02d","release_date":"20%02d-01-01","popularity":1}`, 100+n, n, n%20))
		}
		return response(200, `{"cast":[`+strings.Join(items, ",")+`]}`), nil
	})})
	credits, err := client.PersonCredits(context.Background(), token, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(credits) != 24 {
		t.Fatalf("limit not applied: %d", len(credits))
	}
	if credits[0].Title != "Recent Movie" || credits[0].Category != "anime" || credits[0].Kind != "movie" || credits[0].Year == nil || *credits[0].Year != 2026 {
		t.Fatal(credits[0])
	}
	if credits[1].Title != "Western Show" || credits[1].Category != "standard" || credits[1].Kind != "series" {
		t.Fatal(credits[1])
	}
	for _, credit := range credits {
		if credit.Title == "Adult" || credit.Title == "Duplicate" {
			t.Fatal("adult or duplicate credit leaked", credit)
		}
	}
	if _, err = client.PersonCredits(context.Background(), token, 0); err == nil {
		t.Fatal("invalid person accepted")
	}
}
