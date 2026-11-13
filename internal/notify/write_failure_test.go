package notify

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

func outboxFixture(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	secrets, err := securestore.New(context.Background(), db, dir)
	if err != nil {
		t.Fatal(err)
	}
	return New(db, secrets), db
}

func queueOneNotification(t *testing.T, store *Store, db *sql.DB, url string) {
	t.Helper()
	if _, err := store.Save(context.Background(), 0, ChannelInput{
		Name: "ntfy", Kind: "ntfy", Enabled: true,
		Config: map[string]string{"url": url + "/topic"},
		Events: []string{"download.completed"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'download','job-1','A Movie','completed')`); err != nil {
		t.Fatal(err)
	}
	if published, err := store.PublishNewEvents(context.Background()); err != nil || published != 1 {
		t.Fatalf("published %d: %v", published, err)
	}
}

// A failed terminal state write must not silently leave the item in
// 'sending': the error is propagated to the delivery loop and journaled as an
// activity event so the failure is observable (RS-0014).
func TestFailedOutboxStateWriteIsPropagatedAndJournaled(t *testing.T) {
	store, db := outboxFixture(t)
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	queueOneNotification(t, store, db, server.URL)
	if _, err := db.Exec(`CREATE TRIGGER fail_outbox_update BEFORE UPDATE ON notification_outbox WHEN NEW.state<>'sending' BEGIN SELECT RAISE(ABORT, 'injected write failure'); END;`); err != nil {
		t.Fatal(err)
	}
	worked, err := store.ProcessOne(ctx)
	if !worked {
		t.Fatal("the delivery attempt must still be reported as worked")
	}
	if err == nil {
		t.Fatal("a failed state write must be returned, not swallowed")
	}
	if !strings.Contains(err.Error(), "injected write failure") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := db.Exec(`DROP TRIGGER fail_outbox_update`); err != nil {
		t.Fatal(err)
	}
	var kind, state string
	if err := db.QueryRow(`SELECT kind,state FROM activity_events WHERE state='notification delivery state update failed'`).Scan(&kind, &state); err != nil {
		t.Fatal("failed state write was not journaled:", err)
	}
	if kind != "notification" {
		t.Fatalf("unexpected activity kind %q", kind)
	}
	// Reconciliation: with the fault removed, the stuck 'sending' item is
	// moved to 'uncertain' exactly like the startup sweep does.
	if _, err := db.Exec(`UPDATE notification_outbox SET state='uncertain',updated_at=? WHERE state='sending'`, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	var state2 string
	if err := db.QueryRow(`SELECT state FROM notification_outbox`).Scan(&state2); err != nil || state2 != "uncertain" {
		t.Fatal(state2, err)
	}
}

// The delivery loop must log failed state writes instead of failing silently.
func TestRunLogsFailedStateWrites(t *testing.T) {
	store, db := outboxFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	queueOneNotification(t, store, db, server.URL)
	if _, err := db.Exec(`CREATE TRIGGER fail_outbox_update BEFORE UPDATE ON notification_outbox WHEN NEW.state<>'sending' BEGIN SELECT RAISE(ABORT, 'injected write failure'); END;`); err != nil {
		t.Fatal(err)
	}
	var logged strings.Builder
	logger := slog.New(slog.NewTextHandler(&logged, nil))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		store.Run(ctx, logger)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(logged.String(), "notification delivery failed") {
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
	if !strings.Contains(logged.String(), "notification delivery failed") ||
		!strings.Contains(logged.String(), "injected write failure") {
		t.Fatalf("failure was not logged with its cause: %s", logged.String())
	}
}
