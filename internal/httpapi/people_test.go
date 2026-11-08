package httpapi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/metadata"
)

type creditsProvider struct {
	testProvider
	credits []metadata.PersonCredit
}

func (p *creditsProvider) PersonCredits(context.Context, string, int64) ([]metadata.PersonCredit, error) {
	return p.credits, nil
}

func TestPersonCreditsExcludeAndLibraryState(t *testing.T) {
	provider := &creditsProvider{credits: []metadata.PersonCredit{
		{Kind: "movie", TMDbID: 11, Title: "Current film", Character: "Lead", Category: "standard"},
		{Kind: "movie", TMDbID: 12, Title: "Saved film", Character: "Support", Category: "anime"},
		{Kind: "series", TMDbID: 13, Title: "Saved show", Character: "Guest", Category: "standard"},
	}}
	handler, db, accounts, code := rawFixture(t, provider)
	session, err := accounts.Setup(context.Background(), "testadmin", "test-only-password", code)
	if err != nil {
		t.Fatal(err)
	}
	if out := request(handler, "PUT", "/api/v1/providers/tmdb", `{"token":"test-only-valid-provider-token"}`, &session, true); out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	if _, err = db.Exec(`INSERT INTO media_items(kind,tmdb_id,title,monitored) VALUES('movie',12,'Saved film',1)`); err != nil {
		t.Fatal(err)
	}
	out := request(handler, "GET", "/api/v1/metadata/people/7/credits?exclude_kind=movie&exclude_tmdb_id=11", "", &session, false)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	var page struct {
		Items []struct {
			Kind      string `json:"kind"`
			TMDbID    int64  `json:"tmdb_id"`
			Category  string `json:"category"`
			InLibrary bool   `json:"in_library"`
		} `json:"items"`
	}
	if err = json.Unmarshal(out.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].TMDbID != 12 || !page.Items[0].InLibrary || page.Items[0].Category != "anime" {
		t.Fatal(page)
	}
	if page.Items[1].Kind != "series" || page.Items[1].TMDbID != 13 || page.Items[1].InLibrary {
		t.Fatal(page)
	}
	if out = request(handler, "GET", "/api/v1/metadata/people/0/credits", "", &session, false); out.Code != 400 {
		t.Fatal("invalid person accepted", out.Code, out.Body.String())
	}
}
