package notify

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

func fixture(t *testing.T) (*Store, *sql.DB) {
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

func TestEventMapping(t *testing.T) {
	for _, test := range []struct{ kind, state, want string }{
		{"download", "completed", "download.completed"},
		{"download", "failed", "download.failed"},
		{"download", "blocked by policy", "download.failed"},
		{"title", "file registered", "import.completed"},
		{"request", "approved", "request.approved"},
		{"request", "declined", "request.declined"},
		{"automation", "failed", "health.failed"},
		{"subtitle", "failed", "subtitle.failed"},
	} {
		got, ok := eventName(test.kind, test.state)
		if !ok || got != test.want {
			t.Fatalf("%s/%s: got %q %v, want %q", test.kind, test.state, got, ok, test.want)
		}
	}
	if _, ok := eventName("title", "monitoring enabled"); ok {
		t.Fatal("unrelated event mapped")
	}
}

func TestNotificationChannelValidation(t *testing.T) {
	store, _ := fixture(t)
	ctx := context.Background()
	for _, in := range []ChannelInput{
		{Name: "bad", Kind: "ntfy", Enabled: true, Config: map[string]string{"url": "https://example.com/topic"}, Events: []string{"unknown.event"}},
		{Name: "bad", Kind: "ntfy", Enabled: true, Config: map[string]string{"url": "file:///etc/passwd"}, Events: []string{"download.completed"}},
		{Name: "", Kind: "ntfy", Enabled: true, Config: map[string]string{"url": "https://example.com/topic"}, Events: []string{"download.completed"}},
		{Name: "bad", Kind: "unsupported", Enabled: true, Config: map[string]string{}, Events: []string{"download.completed"}},
	} {
		if _, err := store.Save(ctx, 0, in); err == nil {
			t.Fatalf("invalid channel accepted: %#v", in)
		}
	}
}

func TestNotificationOutboxPublishesAndDelivers(t *testing.T) {
	store, db := fixture(t)
	ctx := context.Background()
	var authHeader, body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	channel, err := store.Save(ctx, 0, ChannelInput{
		Name: "ntfy", Kind: "ntfy", Enabled: true,
		Config: map[string]string{"url": server.URL + "/topic"},
		Events: []string{"download.completed"}, Secret: "private-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !channel.HasSecret {
		t.Fatal("saved channel did not report its secret")
	}
	discord, err := store.Save(ctx, 0, ChannelInput{
		Name: "discord", Kind: "discord", Enabled: true,
		Config: map[string]string{"url": "https://discord.com/api/webhooks/123456789012345678/" + strings.Repeat("a", 60)},
		Events: []string{"download.failed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !discord.HasSecret || discord.Config["url"] != "" {
		t.Fatalf("discord webhook was not stored as a secret: %#v", discord)
	}
	if _, err := db.Exec(`INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'download','job-1','A Movie','completed')`); err != nil {
		t.Fatal(err)
	}
	published, err := store.PublishNewEvents(ctx)
	if err != nil || published != 1 {
		t.Fatalf("published %d: %v", published, err)
	}
	worked, err := store.ProcessOne(ctx)
	if err != nil || !worked {
		t.Fatalf("process: %v %v", worked, err)
	}
	if authHeader != "Bearer private-token" || !strings.Contains(body, "A Movie") {
		t.Fatalf("unexpected delivery: auth=%q body=%q", authHeader, body)
	}
	items, err := store.Outbox(ctx, 10)
	if err != nil || len(items) != 1 || items[0].State != "sent" {
		t.Fatalf("outbox: %#v %v", items, err)
	}
	listed, err := store.List(ctx)
	if err != nil || len(listed) != 2 || !listed[0].HasSecret {
		t.Fatalf("list: %#v %v", listed, err)
	}
	if _, err := db.Exec(`INSERT INTO notification_outbox(channel_id,event,title,body,state,created_at,updated_at) VALUES(?,?,?,?,'uncertain',unixepoch(),unixepoch())`, channel.ID, "download.completed", "Retry me", "body"); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := db.QueryRow(`SELECT MAX(id) FROM notification_outbox`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := store.Retry(ctx, id); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := db.QueryRow(`SELECT state FROM notification_outbox WHERE id=?`, id).Scan(&state); err != nil || state != "pending" {
		t.Fatalf("retry state: %q %v", state, err)
	}
}
