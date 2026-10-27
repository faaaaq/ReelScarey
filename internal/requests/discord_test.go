package requests

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDiscordFulfillmentAndUncertainDelivery(t *testing.T) {
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
	d := NewDiscord(db, secrets)
	if _, err = db.Exec(`INSERT INTO request_accounts(id,username,password_hash,created_at) VALUES(1,'viewer','hash',1),(2,'other','hash',1)`); err != nil {
		t.Fatal(err)
	}
	url := "https://discord.com/api/webhooks/123456789012345678/" + strings.Repeat("a", 60)
	for _, bad := range []string{"http://discord.com/api/webhooks/123456789012345678/" + strings.Repeat("a", 60), "https://discord.com.evil.test/api/webhooks/123456789012345678/" + strings.Repeat("a", 60), url + "?x=1"} {
		if err = d.Save(ctx, 1, bad); err == nil {
			t.Fatal("unsafe webhook accepted", bad)
		}
	}
	if err = d.Save(ctx, 1, url); err != nil {
		t.Fatal(err)
	}
	var encrypted []byte
	if err = db.QueryRow(`SELECT ciphertext FROM request_webhooks WHERE account_id=1`).Scan(&encrypted); err != nil || strings.Contains(string(encrypted), url) {
		t.Fatal("webhook not encrypted", err)
	}
	store := New(db)
	request, err := store.Create(ctx, 1, "movie", 11, "Example @everyone")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Claim(ctx, request.ID); err != nil {
		t.Fatal(err)
	}
	if err = store.Finish(ctx, request.ID); err != nil {
		t.Fatal(err)
	}
	calls := 0
	status := 204
	d.client = &http.Client{Timeout: time.Second, Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if r.URL.String() != url || !strings.Contains(string(body), `"parse":[]`) {
			t.Fatal("incorrect notification", string(body))
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}
	if did, err := d.Tick(ctx); err != nil || did || calls != 0 {
		t.Fatal("notified before media was available", did, err)
	}
	if _, err = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,status) VALUES(1,'movie',11,'Example','available'); INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name) VALUES(1,1,'Movies/example.mkv',1,'x','{}',1,'existing','example')`); err != nil {
		t.Fatal(err)
	}
	if did, err := d.Tick(ctx); err != nil || !did || calls != 1 {
		t.Fatal("notification missing", did, calls, err)
	}
	if did, err := d.Tick(ctx); err != nil || did || calls != 1 {
		t.Fatal("duplicate notification", did, calls, err)
	}
	request2, err := store.Create(ctx, 1, "movie", 12, "Second")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Claim(ctx, request2.ID); err != nil {
		t.Fatal(err)
	}
	if err = store.Finish(ctx, request2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,status) VALUES(2,'movie',12,'Second','available'); INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name) VALUES(2,2,'Movies/second.mkv',1,'x','{}',1,'existing','second')`); err != nil {
		t.Fatal(err)
	}
	status = 500
	if did, err := d.Tick(ctx); err != nil || !did {
		t.Fatal("failure state not recorded", did, err)
	}
	if err = d.Retry(ctx, 2, request2.ID); !errors.Is(err, ErrState) {
		t.Fatal("other account retried notification", err)
	}
	if err = d.Retry(ctx, 1, request2.ID); err != nil {
		t.Fatal(err)
	}
	status = 204
	if did, err := d.Tick(ctx); err != nil || !did || calls != 3 {
		t.Fatal("explicit retry failed", calls, err)
	}
	if err = securestore.Validate(ctx, db, dir); err != nil {
		t.Fatal("valid webhook ciphertext failed backup validation", err)
	}
	if _, err = db.Exec(`UPDATE request_webhooks SET ciphertext=x'00' WHERE account_id=1`); err != nil {
		t.Fatal(err)
	}
	if err = securestore.Validate(ctx, db, dir); err == nil {
		t.Fatal("corrupt webhook ciphertext was accepted")
	}
}

func TestDiscordWaitsForAllMonitoredEpisodes(t *testing.T) {
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
	d := NewDiscord(db, secrets)
	if _, err = db.Exec(`INSERT INTO request_accounts(id,username,password_hash,created_at) VALUES(1,'viewer','hash',1)`); err != nil {
		t.Fatal(err)
	}
	if err = d.Save(ctx, 1, "https://discord.com/api/webhooks/123456789012345678/"+strings.Repeat("a", 60)); err != nil {
		t.Fatal(err)
	}
	s := New(db)
	item, err := s.Create(ctx, 1, "series", 22, "A show")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Claim(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Finish(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title) VALUES(1,'series',22,'A show'); INSERT INTO series_details(media_id) VALUES(1); INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,loaded_at) VALUES(1,1,1,1,'Season','','',2,'loaded'); INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date) VALUES(1,1,1,1,'One','',''),(2,1,2,2,'Two','',''); INSERT INTO media_files(id,series_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name) VALUES(1,1,'TV/one.mkv',1,'x','{}',1,'existing','one'); INSERT INTO episode_files(episode_id,media_file_id) VALUES(1,1)`); err != nil {
		t.Fatal(err)
	}
	calls := 0
	d.client = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 204, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	if did, err := d.Tick(ctx); err != nil || did || calls != 0 {
		t.Fatal("partial series triggered notification", did, err)
	}
	if _, err = db.Exec(`INSERT INTO episode_files(episode_id,media_file_id) VALUES(2,1)`); err != nil {
		t.Fatal(err)
	}
	if did, err := d.Tick(ctx); err != nil || !did || calls != 1 {
		t.Fatal("completed series not notified", did, err)
	}
}
