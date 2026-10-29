package discordbot

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

type testProvider struct{}

func (testProvider) Validate(context.Context, string) error { return nil }
func (testProvider) Search(context.Context, string, string, int) (metadata.Results, error) {
	return metadata.Results{Items: []metadata.Movie{{TMDbID: 11, Title: "Test movie"}}}, nil
}
func (testProvider) Movie(context.Context, string, int64) (metadata.Movie, error) {
	return metadata.Movie{TMDbID: 11, Title: "Test movie"}, nil
}

type testTransfer struct{ queued int }

func (*testTransfer) Search(context.Context, int64, string) ([]transfer.Release, error) {
	return []transfer.Release{{ID: "0123456789abcdef0123456789abcdef", Title: "Owned test release"}}, nil
}
func (*testTransfer) SearchTV(context.Context, int64, int, int, string) ([]transfer.Release, error) {
	return nil, nil
}
func (t *testTransfer) Queue(_ context.Context, id string) (transfer.Job, bool, error) {
	if id != "0123456789abcdef0123456789abcdef" {
		return transfer.Job{}, false, ErrConfig
	}
	t.queued++
	return transfer.Job{}, true, nil
}

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSignedBotRequestApprovalAndReleaseButtons(t *testing.T) {
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
	if _, err = db.Exec(`INSERT INTO request_accounts(id,username,password_hash,created_at) VALUES(1,'viewer','hash',1),(2,'other','hash',1)`); err != nil {
		t.Fatal(err)
	}
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	c := Config{ApplicationID: "111111111111111111", PublicKey: hex.EncodeToString(pub), GuildID: "222222222222222222", AdminUserID: "333333333333333333", AdminChannelID: "444444444444444444"}
	s := New(db, secrets, metadata.New(secrets, testProvider{}), nil)
	adapter := &testTransfer{}
	s.transfer = adapter
	if err = s.Save(ctx, c, strings.Repeat("b", 60)); err != nil {
		t.Fatal(err)
	}
	viewer := "555555555555555555"
	other := "666666666666666666"
	if err = s.Link(ctx, 1, viewer); err != nil {
		t.Fatal(err)
	}
	if err = s.Link(ctx, 2, other); err != nil {
		t.Fatal(err)
	}
	var last map[string]any
	noticeCount := 0
	registered := 0
	s.client = &http.Client{Timeout: time.Second, Transport: testTransport(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		last = map[string]any{}
		if err := json.Unmarshal(body, &last); err != nil {
			t.Fatal(err)
		}
		if r.URL.Host != "discord.com" {
			t.Fatal("unexpected Discord host")
		}
		if strings.Contains(r.URL.Path, "/channels/") {
			noticeCount++
		}
		if strings.Contains(r.URL.Path, "/commands") {
			if r.Header.Get("Authorization") != "Bot "+strings.Repeat("b", 60) {
				t.Fatal("bot token missing during registration")
			}
			registered++
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	if err = s.Register(ctx); err != nil || registered != 2 {
		t.Fatal("command registration failed", registered, err)
	}
	sign := func(id, actor string, typ int, data map[string]any) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"id": id, "application_id": c.ApplicationID, "guild_id": c.GuildID, "token": strings.Repeat("t", 40), "type": typ, "member": map[string]any{"user": map[string]string{"id": actor}}, "data": data})
		stamp := strconv.FormatInt(time.Now().Unix(), 10)
		sig := ed25519.Sign(private, append([]byte(stamp), body...))
		req := httptest.NewRequest("POST", "/", strings.NewReader(string(body)))
		req.Header.Set("X-Signature-Timestamp", stamp)
		req.Header.Set("X-Signature-Ed25519", hex.EncodeToString(sig))
		rec := httptest.NewRecorder()
		s.ServeInteraction(rec, req)
		return rec
	}
	buttonID := func() string {
		rows := last["components"].([]any)
		row := rows[0].(map[string]any)
		return row["components"].([]any)[0].(map[string]any)["custom_id"].(string)
	}
	rec := sign("777777777777777771", viewer, 2, map[string]any{"name": "request", "options": []any{map[string]any{"name": "kind", "value": "movie"}, map[string]any{"name": "query", "value": "test"}}})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"type":5`) {
		t.Fatal(rec.Code, rec.Body)
	}
	if did, err := s.ProcessOne(ctx); err != nil || !did {
		t.Fatal("search failed", did, err)
	}
	requestButton := buttonID()
	rec = sign("777777777777777772", viewer, 3, map[string]any{"custom_id": requestButton})
	if !strings.Contains(rec.Body.String(), `"type":5`) {
		t.Fatal(rec.Body)
	}
	if did, err := s.ProcessOne(ctx); err != nil || !did {
		t.Fatal("request failed", did, err)
	}
	var requestID int64
	if err = db.QueryRow(`SELECT id FROM media_requests WHERE account_id=1 AND tmdb_id=11`).Scan(&requestID); err != nil {
		t.Fatal(err)
	}
	if did, err := s.ProcessNotice(ctx); err != nil || !did || noticeCount != 1 {
		t.Fatal("administrator notice failed", did, err)
	}
	approve := buttonID()
	rec = sign("777777777777777773", other, 3, map[string]any{"custom_id": approve})
	if strings.Contains(rec.Body.String(), `"type":5`) {
		t.Fatal("non-admin approved request")
	}
	rec = sign("777777777777777774", c.AdminUserID, 3, map[string]any{"custom_id": approve})
	if !strings.Contains(rec.Body.String(), `"type":5`) {
		t.Fatal(rec.Body)
	}
	if did, err := s.ProcessOne(ctx); err != nil || !did {
		t.Fatal("approval failed", did, err)
	}
	rec = sign("777777777777777775", viewer, 2, map[string]any{"name": "releases", "options": []any{map[string]any{"name": "request_id", "value": requestID}}})
	if !strings.Contains(rec.Body.String(), `"type":5`) {
		t.Fatal(rec.Body)
	}
	if did, err := s.ProcessOne(ctx); err != nil || !did {
		t.Fatal("release search failed", did, err)
	}
	grab := buttonID()
	rec = sign("777777777777777776", other, 3, map[string]any{"custom_id": grab})
	if strings.Contains(rec.Body.String(), `"type":5`) {
		t.Fatal("another user could use release button")
	}
	rec = sign("777777777777777777", viewer, 3, map[string]any{"custom_id": grab})
	if !strings.Contains(rec.Body.String(), `"type":5`) {
		t.Fatal(rec.Body)
	}
	if did, err := s.ProcessOne(ctx); err != nil || !did || adapter.queued != 1 {
		t.Fatal("release not queued", did, adapter.queued, err)
	}
	rec = sign("777777777777777777", viewer, 3, map[string]any{"custom_id": grab})
	if strings.Contains(rec.Body.String(), `"type":5`) {
		t.Fatal("replayed interaction queued twice")
	}
	if err = securestore.Validate(ctx, db, dir); err != nil {
		t.Fatal("bot secrets failed validation", err)
	}
}

func TestBotInteractionSignatureAndDisabledAccount(t *testing.T) {
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
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	c := Config{ApplicationID: "111111111111111111", PublicKey: hex.EncodeToString(pub), GuildID: "222222222222222222", AdminUserID: "333333333333333333", AdminChannelID: "444444444444444444"}
	s := New(db, secrets, nil, nil)
	if err = s.Save(ctx, c, strings.Repeat("b", 60)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO request_accounts(id,username,password_hash,created_at) VALUES(1,'viewer','hash',1)`); err != nil {
		t.Fatal(err)
	}
	viewer := "555555555555555555"
	if err = s.Link(ctx, 1, viewer); err != nil {
		t.Fatal(err)
	}
	signed := func(body string, valid bool) *httptest.ResponseRecorder {
		stamp := strconv.FormatInt(time.Now().Unix(), 10)
		sig := ed25519.Sign(priv, append([]byte(stamp), []byte(body)...))
		if !valid {
			sig[0] ^= 1
		}
		req := httptest.NewRequest("POST", "/", strings.NewReader(body))
		req.Header.Set("X-Signature-Timestamp", stamp)
		req.Header.Set("X-Signature-Ed25519", hex.EncodeToString(sig))
		rec := httptest.NewRecorder()
		s.ServeInteraction(rec, req)
		return rec
	}
	if out := signed(`{"type":1}`, false); out.Code != 401 {
		t.Fatal("forged interaction accepted", out.Code)
	}
	if out := signed(`{"type":1}`, true); out.Code != 200 || !strings.Contains(out.Body.String(), `"type":1`) {
		t.Fatal("signed ping failed", out.Code, out.Body)
	}
	if err = s.DisableAccount(ctx, 1, true); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"id": "777777777777777779", "application_id": c.ApplicationID, "guild_id": c.GuildID, "token": strings.Repeat("t", 40), "type": 2, "member": map[string]any{"user": map[string]string{"id": viewer}}, "data": map[string]any{"name": "request", "options": []any{map[string]any{"name": "kind", "value": "movie"}, map[string]any{"name": "query", "value": "test"}}}})
	if out := signed(string(body), true); out.Code != 200 || strings.Contains(out.Body.String(), `"type":5`) {
		t.Fatal("disabled account could queue an interaction", out.Code, out.Body)
	}
	var n int
	if err = db.QueryRow(`SELECT COUNT(*) FROM discord_bot_jobs`).Scan(&n); err != nil || n != 0 {
		t.Fatal("disabled account created job", n, err)
	}
}
