package discordbot

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

type rateLimitSetup struct {
	s        *Service
	db       *sql.DB
	private  ed25519.PrivateKey
	config   Config
	viewerID string
}

func setupRateLimitService(t *testing.T, ctx context.Context) *rateLimitSetup {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	secrets, err := securestore.New(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	config := Config{ApplicationID: "111111111111111111", PublicKey: hex.EncodeToString(pub), GuildID: "222222222222222222", AdminUserID: "333333333333333333", AdminChannelID: "444444444444444444"}
	s := New(db, secrets, metadata.New(secrets, testProvider{}), nil)
	if err = s.Save(ctx, config, strings.Repeat("b", 60)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO request_accounts(id,username,password_hash,created_at) VALUES(1,'viewer','hash',1)`); err != nil {
		t.Fatal(err)
	}
	if err = s.Link(ctx, 1, "555555555555555555"); err != nil {
		t.Fatal(err)
	}
	return &rateLimitSetup{s: s, db: db, private: private, config: config, viewerID: "555555555555555555"}
}

func (setup *rateLimitSetup) queueSearchJob(t *testing.T, ctx context.Context, id string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"id": id, "application_id": setup.config.ApplicationID, "guild_id": setup.config.GuildID, "token": strings.Repeat("t", 40), "type": 2, "member": map[string]any{"user": map[string]string{"id": setup.viewerID}}, "data": map[string]any{"name": "request", "options": []any{map[string]any{"name": "kind", "value": "movie"}, map[string]any{"name": "query", "value": "test"}}}})
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	sig := ed25519.Sign(setup.private, append([]byte(stamp), body...))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
	req.Header.Set("X-Signature-Timestamp", stamp)
	req.Header.Set("X-Signature-Ed25519", hex.EncodeToString(sig))
	rec := httptest.NewRecorder()
	setup.s.ServeInteraction(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"type":5`) {
		t.Fatalf("search interaction was not deferred: %d %s", rec.Code, rec.Body.String())
	}
}

func rateLimitResponse() *http.Response {
	header := make(http.Header)
	header.Set("Retry-After", "1")
	return &http.Response{StatusCode: 429, Header: header, Body: io.NopCloser(strings.NewReader("{}"))}
}

func okResponse() *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}
}

// A 429 on the interaction reply is temporary: the worker must wait out the
// Retry-After hint, retry the reply within the interaction window, and finish
// the job sent instead of marking it uncertain on the first 429 (NEW-C1).
func TestBotReply429RetriesWithinInteractionWindow(t *testing.T) {
	ctx := context.Background()
	setup := setupRateLimitService(t, ctx)
	attempts := 0
	setup.s.client = &http.Client{Timeout: time.Second, Transport: testTransport(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "@original") {
			attempts++
			if attempts == 1 {
				return rateLimitResponse(), nil
			}
		}
		return okResponse(), nil
	})}
	setup.queueSearchJob(t, ctx, "777777777777777781")
	did, err := setup.s.ProcessOne(ctx)
	if !did || err != nil {
		t.Fatalf("429 reply was not retried: did=%v err=%v", did, err)
	}
	if attempts != 2 {
		t.Fatalf("expected the 429 reply to be retried once, saw %d attempts", attempts)
	}
	var state string
	if err = setup.s.db.QueryRow(`SELECT state FROM discord_bot_jobs`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "sent" {
		t.Fatalf("retried job ended in state %q", state)
	}
}

// A persistent 429 must still be reported as a rate limit failure and leave
// the job uncertain — never a silent success or a generic error (NEW-C1).
func TestBotReplyPersistent429StaysUncertainWithRateLimitFailure(t *testing.T) {
	ctx := context.Background()
	setup := setupRateLimitService(t, ctx)
	attempts := 0
	setup.s.client = &http.Client{Timeout: time.Second, Transport: testTransport(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "@original") {
			attempts++
			return rateLimitResponse(), nil
		}
		return okResponse(), nil
	})}
	setup.queueSearchJob(t, ctx, "777777777777777782")
	did, err := setup.s.ProcessOne(ctx)
	if !did {
		t.Fatal("job was not processed")
	}
	var rateLimited *RateLimitError
	if err == nil || !errors.As(err, &rateLimited) {
		t.Fatalf("persistent 429 was not reported as rate limited: %v", err)
	}
	if rateLimited.RetryAfter < 1 || rateLimited.RetryAfter > 300 {
		t.Fatalf("Retry-After hint was not clamped: %d", rateLimited.RetryAfter)
	}
	if attempts != 2 {
		t.Fatalf("expected one bounded retry, saw %d attempts", attempts)
	}
	var state string
	if err = setup.s.db.QueryRow(`SELECT state FROM discord_bot_jobs`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "uncertain" {
		t.Fatalf("persistent 429 left the job in state %q", state)
	}
}

// A 429 on the administrator notice was never delivered, so the notice must
// be requeued for the next worker tick instead of being parked in the
// terminal uncertain state (NEW-C1).
func TestBotNotice429RequeuesForDelivery(t *testing.T) {
	ctx := context.Background()
	setup := setupRateLimitService(t, ctx)
	if _, err := setup.s.db.Exec(`INSERT INTO media_requests(id,account_id,kind,tmdb_id,title,state,created_at,updated_at) VALUES(1,1,'movie',11,'Test movie','pending',unixepoch(),unixepoch())`); err != nil {
		t.Fatal(err)
	}
	notices := 0
	setup.s.client = &http.Client{Timeout: time.Second, Transport: testTransport(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/channels/") {
			notices++
			return rateLimitResponse(), nil
		}
		return okResponse(), nil
	})}
	did, err := setup.s.ProcessNotice(ctx)
	if !did || err == nil {
		t.Fatalf("the rate limit must still be reported honestly: did=%v err=%v", did, err)
	}
	var asRateLimitErr *RateLimitError
	if !errors.As(err, &asRateLimitErr) {
		t.Fatalf("429 was not reported as rate limited: %v", err)
	}
	var state string
	if err = setup.s.db.QueryRow(`SELECT state FROM discord_bot_notices WHERE request_id=1`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "queued" {
		t.Fatalf("429 parked the notice in state %q instead of requeueing it", state)
	}
}
