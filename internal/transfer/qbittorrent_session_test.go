package transfer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestQbittorrentLoginsDoNotSerializeAcrossClients(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v2/auth/login") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		time.Sleep(1200 * time.Millisecond)
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: "slow", Path: "/"})
		fmt.Fprint(w, "Ok.")
	}))
	defer slow.Close()
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: "fast", Path: "/"})
		fmt.Fprint(w, "Ok.")
	}))
	defer fast.Close()

	q := NewQBittorrent()
	slowDone := make(chan error, 1)
	go func() {
		_, err := q.session(context.Background(), ClientConfig{URL: slow.URL, Username: "user", Password: "password"})
		slowDone <- err
	}()
	time.Sleep(100 * time.Millisecond)

	// The fast client must not wait behind the slow client's login round trip.
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	if _, err := q.session(ctx, ClientConfig{URL: fast.URL, Username: "user", Password: "password"}); err != nil {
		t.Fatalf("a second client waited for an unrelated login: %v", err)
	}
	if err := <-slowDone; err != nil {
		t.Fatal(err)
	}
}
