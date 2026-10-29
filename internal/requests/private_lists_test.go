package requests

import (
	"context"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPrivateListsEncryptTokensAndFenceOwnership(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, e := database.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	secrets, e := securestore.New(ctx, db, dir)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO request_accounts(id,username,password_hash,created_at) VALUES(1,'owner','hash',1),(2,'other','hash',1)`); e != nil {
		t.Fatal(e)
	}
	s := NewLists(db, secrets)
	token := strings.Repeat("private", 8)
	client := strings.Repeat("c", 30)
	for _, provider := range []string{"tmdb_private", "trakt_private"} {
		owner, key := "", ""
		if provider == "trakt_private" {
			owner, key = "viewer", client
		}
		v, e := s.Add(ctx, 1, provider, owner, "123", key, token)
		if e != nil {
			t.Fatal(e)
		}
		var sealed []byte
		if e = db.QueryRow(`SELECT ciphertext FROM request_lists WHERE id=?`, v.ID).Scan(&sealed); e != nil || strings.Contains(string(sealed), token) {
			t.Fatal("unencrypted token", e)
		}
		if e = s.Delete(ctx, 2, v.ID); e == nil {
			t.Fatal("cross-account deletion accepted")
		}
	}
	s.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Fatal("wrong user token")
		}
		body := `{"public":false,"page":1,"total_pages":1,"results":[{"id":11,"media_type":"movie","title":"Private movie"}]}`
		h := http.Header{}
		if r.URL.Host == "api.trakt.tv" {
			if r.Header.Get("trakt-api-key") != client {
				t.Fatal("wrong client")
			}
			h.Set("X-Pagination-Page-Count", "1")
			body = `[{"type":"show","show":{"title":"Private show","ids":{"tmdb":22}}}]`
		} else if r.URL.Host != "api.themoviedb.org" {
			t.Fatal("unexpected host")
		}
		return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	for range 2 {
		if did, e := s.Tick(ctx); !did || e != nil {
			t.Fatal(did, e)
		}
	}
	items, e := New(db).List(ctx, 1)
	if e != nil || len(items) != 2 {
		t.Fatal(items, e)
	}
	other, e := New(db).List(ctx, 2)
	if e != nil || len(other) != 0 {
		t.Fatal("private requests leaked", other, e)
	}
}
