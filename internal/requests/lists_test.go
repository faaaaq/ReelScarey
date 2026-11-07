package requests

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

func TestPublicListImportIsBoundedOwnedAndFenced(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	secrets, err := securestore.New(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = secrets.Set(ctx, "test-provider-token-for-api-tests"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO request_accounts(id,username,password_hash,created_at) VALUES(1,'first','hash',1),(2,'second','hash',1)`); err != nil {
		t.Fatal(err)
	}
	lists := NewLists(db, secrets)
	m, err := lists.Add(ctx, 1, "tmdb", "", "123", "")
	if err != nil {
		t.Fatal(err)
	}
	traktKey := strings.Repeat("k", 30)
	tr, err := lists.Add(ctx, 1, "trakt", "viewer", "my-list", traktKey)
	if err != nil {
		t.Fatal(err)
	}
	if err = lists.Delete(ctx, 2, m.ID); err == nil {
		t.Fatal("other user removed list")
	}
	var encrypted []byte
	if err = db.QueryRow(`SELECT ciphertext FROM request_lists WHERE id=?`, tr.ID).Scan(&encrypted); err != nil || strings.Contains(string(encrypted), traktKey) {
		t.Fatal("key stored in plaintext", err)
	}
	calls := 0
	lists.client = &http.Client{Timeout: time.Second, Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		headers := make(http.Header)
		data := `{"public":true,"page":1,"total_pages":1,"results":[{"id":11,"media_type":"movie","title":"One"}]}`
		if r.URL.Host == "api.trakt.tv" {
			if r.Header.Get("trakt-api-key") != traktKey {
				t.Fatal("wrong Trakt key")
			}
			headers.Set("X-Pagination-Page-Count", "1")
			data = `[{"type":"show","show":{"title":"Two","ids":{"tmdb":22}}}]`
		} else if r.URL.Host != "api.themoviedb.org" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Fatal("unexpected provider request")
		}
		return &http.Response{StatusCode: 200, Header: headers, Body: io.NopCloser(strings.NewReader(data))}, nil
	})}
	if did, err := lists.Tick(ctx); err != nil || !did {
		t.Fatal("tmdb import failed", did, err)
	}
	if did, err := lists.Tick(ctx); err != nil || !did {
		t.Fatal("trakt import failed", did, err)
	}
	items, err := New(db).List(ctx, 1)
	if err != nil || len(items) != 2 || calls != 2 {
		t.Fatal("imported requests missing", items, calls, err)
	}
	if err = lists.Refresh(ctx, 1, m.ID); err != nil {
		t.Fatal(err)
	}
	lists.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if _, e := db.Exec(`UPDATE request_lists SET revision=revision+1,enabled=0 WHERE id=?`, m.ID); e != nil {
			t.Fatal(e)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"public":true,"page":1,"total_pages":1,"results":[{"id":99,"media_type":"movie","title":"Stale"}]}`))}, nil
	})
	if did, err := lists.Tick(ctx); err != nil || !did {
		t.Fatal("fenced import failed", did, err)
	}
	items, err = New(db).List(ctx, 1)
	if err != nil || len(items) != 2 {
		t.Fatal("stale provider response created a request", items, err)
	}
}

func TestIMDbMDBListSyncRequiresOwnedIMDbSource(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	secrets, err := securestore.New(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO request_accounts(id,username,password_hash,created_at) VALUES(1,'viewer','hash',1)`); err != nil {
		t.Fatal(err)
	}
	s := NewLists(db, secrets)
	key := strings.Repeat("z", 32)
	list, err := s.Add(ctx, 1, "imdb_mdblist", "", "123", key)
	if err != nil {
		t.Fatal(err)
	}
	var sealed []byte
	if err = db.QueryRow(`SELECT ciphertext FROM request_lists WHERE id=?`, list.ID).Scan(&sealed); err != nil || strings.Contains(string(sealed), key) {
		t.Fatal("MDBList key was not encrypted", err)
	}
	s.client = &http.Client{Timeout: time.Second, Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.mdblist.com" || r.URL.Query().Get("apikey") != key {
			t.Fatal("wrong MDBList request")
		}
		h := make(http.Header)
		body := `[{"id":123,"source":"IMDb"}]`
		if strings.HasSuffix(r.URL.Path, "/items") {
			h.Set("X-Has-More", "false")
			body = `{"movies":[{"id":603,"title":"The Matrix"}],"shows":[{"id":1396,"title":"Breaking Bad"}],"pagination":{}}`
		}
		return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	if did, err := s.Tick(ctx); err != nil || !did {
		t.Fatal(did, err)
	}
	items, err := New(db).List(ctx, 1)
	if err != nil || len(items) != 2 {
		t.Fatal(items, err)
	}
	if err = s.Refresh(ctx, 1, list.ID); err != nil {
		t.Fatal(err)
	}
	s.client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`[{"id":123,"source":"Trakt"}]`))}, nil
	})
	if did, err := s.Tick(ctx); err != nil || !did {
		t.Fatal(did, err)
	}
	saved, err := s.All(ctx, 1)
	if err != nil || len(saved) != 1 || saved[0].LastError == "" {
		t.Fatal("non-IMDb list accepted", saved, err)
	}
}

func TestIMDbMDBListPaginationMustBeComplete(t *testing.T) {
	s := &Lists{}
	key := strings.Repeat("z", 32)
	s.client = &http.Client{Timeout: time.Second, Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		h := make(http.Header)
		body := `[{"id":123,"source":"IMDb"}]`
		if strings.HasSuffix(r.URL.Path, "/items") {
			if r.URL.Query().Get("cursor") == "" {
				h.Set("X-Has-More", "true")
				body = `{"movies":[{"id":603,"title":"First"}],"shows":[],"pagination":{"next_cursor":"next"}}`
			} else {
				h.Set("X-Has-More", "false")
				body = `{"movies":[],"shows":[{"id":1396,"title":"Second"}],"pagination":{}}`
			}
		}
		return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	items, err := s.fetchIMDbMDBList(context.Background(), "123", key)
	if err != nil || len(items) != 2 {
		t.Fatal(items, err)
	}
	s.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		h := make(http.Header)
		body := `[{"id":123,"source":"IMDb"}]`
		if strings.HasSuffix(r.URL.Path, "/items") {
			h.Set("X-Has-More", "true")
			body = `{"movies":[{"id":603,"title":"First"}],"shows":[],"pagination":{}}`
		}
		return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	if _, err = s.fetchIMDbMDBList(context.Background(), "123", key); err == nil {
		t.Fatal("incomplete pagination accepted")
	}
}
