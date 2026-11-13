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

// NEW-C2: a provider 429 with a Retry-After header must defer the next check
// past the provider's window instead of the generic 3600 s delay.
func TestListSyncHonorsRetryAfterOn429(t *testing.T) {
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
	list, err := s.Add(ctx, 1, "trakt", "owner", "mylist", strings.Repeat("y", 32))
	if err != nil {
		t.Fatal(err)
	}
	s.client = &http.Client{Timeout: time.Second, Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.trakt.tv" {
			t.Fatal("wrong provider request", r.URL)
		}
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"7200"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	if did, err := s.Tick(ctx); err != nil || !did {
		t.Fatal(did, err)
	}
	var lastError string
	var nextCheck int64
	if err = db.QueryRow(`SELECT last_error,next_check FROM request_lists WHERE id=?`, list.ID).Scan(&lastError, &nextCheck); err != nil {
		t.Fatal(err)
	}
	if lastError == "" {
		t.Fatal("429 must record the failed sync")
	}
	now := time.Now().Unix()
	if nextCheck < now+6900 || nextCheck > now+7600 {
		t.Fatalf("Retry-After of 7200 s must defer the next check past the generic 3600 s delay: %d (now %d)", nextCheck, now)
	}
}

// NEW-C4: the 429 terminal state write must survive request cancellation, like
// the network-error path; the notification stays pending with a deferred retry.
func TestDiscordRateLimited429PersistsStateAfterCancellation(t *testing.T) {
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
	url := "https://discord.com/api/webhooks/123456789012345678/" + strings.Repeat("a", 60)
	if err = d.Save(ctx, 1, url); err != nil {
		t.Fatal(err)
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
	if _, err = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,status) VALUES(1,'movie',11,'Example','available'); INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name) VALUES(1,1,'Movies/example.mkv',1,'x','{}',1,'existing','example')`); err != nil {
		t.Fatal(err)
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	d.client = &http.Client{Timeout: time.Second, Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		cancel()
		return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}
	if did, err := d.Tick(cancelCtx); err != nil || !did {
		t.Fatal("429 tick must return a handled notification", did, err)
	}
	var state string
	var next int64
	if err = db.QueryRow(`SELECT state,next_attempt FROM request_notifications WHERE request_id=?`, request.ID).Scan(&state, &next); err != nil {
		t.Fatal(err)
	}
	if state != "pending" {
		t.Fatalf("429 must keep the notification pending, not stuck in sending: %s", state)
	}
	if next <= time.Now().Unix() {
		t.Fatalf("429 must defer the next attempt past the current time: %d", next)
	}
}

// The terminal sent write must survive request cancellation like the 429 and
// network-error paths: a lost terminal write leaves the notification stuck
// in sending and causes a duplicate post on the next manual retry.
func TestDiscordSentStatePersistsAfterCancellation(t *testing.T) {
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
	url := "https://discord.com/api/webhooks/123456789012345678/" + strings.Repeat("a", 60)
	if err = d.Save(ctx, 1, url); err != nil {
		t.Fatal(err)
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
	if _, err = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,status) VALUES(1,'movie',11,'Example','available'); INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name) VALUES(1,1,'Movies/example.mkv',1,'x','{}',1,'existing','example')`); err != nil {
		t.Fatal(err)
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	d.client = &http.Client{Timeout: time.Second, Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		cancel()
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}
	did, err := d.Tick(cancelCtx)
	if err != nil || !did {
		t.Fatalf("a sent tick must report the notification handled: did=%v err=%v", did, err)
	}
	var state string
	if err = db.QueryRow(`SELECT state FROM request_notifications WHERE request_id=?`, request.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "sent" {
		t.Fatalf("the sent state was lost when the context was cancelled: %s", state)
	}
}
