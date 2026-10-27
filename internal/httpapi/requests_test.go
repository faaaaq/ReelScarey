package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/requests"
)

type imdbTestProvider struct{ testProvider }

func (*imdbTestProvider) FindIMDb(_ context.Context, _ string, id string) (metadata.ExternalTitle, error) {
	if id == "tt0133093" {
		return metadata.ExternalTitle{Kind: "movie", TMDbID: 603, Title: "The Matrix"}, nil
	}
	return metadata.ExternalTitle{}, &metadata.Error{Code: "imdb_unmatched", Message: "No exact match."}
}

func TestIMDbImportRejectsWholeBatchOnUnmatchedID(t *testing.T) {
	h, _, service, code := rawFixture(t, &imdbTestProvider{})
	ctx := context.Background()
	admin, err := service.Setup(ctx, "admin", "admin-test-password", code)
	if err != nil {
		t.Fatal(err)
	}
	if out := request(h, "PUT", "/api/v1/providers/tmdb", `{"token":"test-provider-token-for-api-tests"}`, &admin, true); out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	if _, err = service.CreateRequestAccount(ctx, "viewer", "viewer-test-password"); err != nil {
		t.Fatal(err)
	}
	viewer, err := service.Login(ctx, "viewer", "viewer-test-password")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"csv": "Const,Title\ntt0133093,Matrix\ntt9999999,Unknown\n"})
	if out := request(h, "POST", "/api/v1/requester/imdb-csv", string(body), &viewer, true); out.Code != 502 {
		t.Fatal("unmatched batch accepted", out.Code, out.Body)
	}
	if out := request(h, "GET", "/api/v1/requester/requests", "", &viewer, false); strings.Contains(out.Body.String(), "The Matrix") {
		t.Fatal("partially imported failed batch", out.Body)
	}
	body, _ = json.Marshal(map[string]string{"csv": "Const,Title\ntt0133093,Matrix\n"})
	if out := request(h, "POST", "/api/v1/requester/imdb-csv", string(body), &viewer, true); out.Code != 201 || !strings.Contains(out.Body.String(), `"added":1`) {
		t.Fatal("IMDb export import failed", out.Code, out.Body)
	}
}

func TestRequestAccountsAndApprovalAreIsolated(t *testing.T) {
	h, db, service, code := rawFixture(t, &testProvider{})
	ctx := context.Background()
	admin, err := service.Setup(ctx, "admin", "admin-test-password", code)
	if err != nil {
		t.Fatal(err)
	}
	if out := request(h, "PUT", "/api/v1/providers/tmdb", `{"token":"test-provider-token-for-api-tests"}`, &admin, true); out.Code != 200 {
		t.Fatal("provider setup", out.Code, out.Body)
	}
	for _, username := range []string{"viewer", "second"} {
		out := request(h, "POST", "/api/v1/request-accounts", `{"username":"`+username+`","password":"viewer-test-password"}`, &admin, true)
		if out.Code != 201 || strings.Contains(out.Body.String(), "viewer-test-password") {
			t.Fatal("account create leaked or failed", out.Code, out.Body)
		}
	}
	viewer, err := service.Login(ctx, "viewer", "viewer-test-password")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Login(ctx, "second", "viewer-test-password")
	if err != nil {
		t.Fatal(err)
	}
	out := request(h, "POST", "/api/v1/requester/lists", `{"provider":"tmdb","owner":"","list_id":"123","client_id":""}`, &viewer, true)
	if out.Code != 201 {
		t.Fatal("list create failed", out.Code, out.Body)
	}
	if out = request(h, "DELETE", "/api/v1/requester/lists/1", "", &second, true); out.Code != 404 {
		t.Fatal("another user deleted the list", out.Code, out.Body)
	}
	hook := "https://discord.com/api/webhooks/123456789012345678/" + strings.Repeat("a", 60)
	body, _ := json.Marshal(map[string]string{"webhook": hook})
	if out = request(h, "PUT", "/api/v1/requester/discord", string(body), &viewer, true); out.Code != 200 || strings.Contains(out.Body.String(), hook) {
		t.Fatal("webhook save failed or leaked", out.Code, out.Body)
	}
	if out = request(h, "GET", "/api/v1/requester/discord", "", &second, false); out.Code != 200 || strings.Contains(out.Body.String(), "true") {
		t.Fatal("webhook leaked across users", out.Code, out.Body)
	}
	if out := request(h, "POST", "/api/v1/request-accounts", `{"username":"evil","password":"viewer-test-password"}`, &viewer, true); out.Code != 403 {
		t.Fatal("requester created an account", out.Code, out.Body)
	}
	out = request(h, "POST", "/api/v1/requester/requests", `{"kind":"movie","tmdb_id":11}`, &viewer, true)
	var item requests.Request
	if out.Code != 201 || json.Unmarshal(out.Body.Bytes(), &item) != nil || item.Title != "Canonical provider title" || item.AccountID != viewer.AccountID {
		t.Fatal("request create", out.Code, out.Body)
	}
	out = request(h, "GET", "/api/v1/requester/requests", "", &second, false)
	if out.Code != 200 || strings.Contains(out.Body.String(), "Canonical provider title") {
		t.Fatal("request leaked across users", out.Code, out.Body)
	}
	out = request(h, "POST", "/api/v1/requests/1/approve", `{}`, &viewer, true)
	if out.Code != 403 {
		t.Fatal("requester approved their own request", out.Code, out.Body)
	}
	out = request(h, "POST", "/api/v1/requests/1/approve", `{}`, &admin, true)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"state":"approved"`) {
		t.Fatal("approval failed", out.Code, out.Body)
	}
	var n int
	if err = db.QueryRow(`SELECT COUNT(*) FROM media_items WHERE kind='movie' AND tmdb_id=11`).Scan(&n); err != nil || n != 1 {
		t.Fatal("approval did not add catalog item", n, err)
	}
	out = request(h, "POST", "/api/v1/requests/1/approve", `{}`, &admin, true)
	if out.Code != 200 {
		t.Fatal("approval retry failed", out.Code, out.Body)
	}
}
