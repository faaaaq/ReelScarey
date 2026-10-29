package httpapi

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/discordbot"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

func TestDiscordPublicEndpointRequiresSignature(t *testing.T) {
	_, db, authService, _ := rawFixture(t, nil)
	ctx := context.Background()
	dir := t.TempDir()
	secrets, err := securestore.New(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	bot := discordbot.New(db, secrets, nil, nil)
	if err = bot.Save(ctx, discordbot.Config{ApplicationID: "111111111111111111", PublicKey: hex.EncodeToString(pub), GuildID: "222222222222222222", AdminUserID: "333333333333333333", AdminChannelID: "444444444444444444"}, strings.Repeat("b", 60)); err != nil {
		t.Fatal(err)
	}
	assets := fstest.MapFS{"index.html": {Data: []byte("ReelScarey")}}
	h := New(db, config.Config{PublicURL: "https://example.com"}, slog.New(slog.NewTextHandler(io.Discard, nil)), assets, "test", Dependencies{Auth: authService, Bot: bot, Secrets: secrets})
	body := `{"type":1}`
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	request := func(valid bool) *httptest.ResponseRecorder {
		sig := ed25519.Sign(priv, append([]byte(stamp), []byte(body)...))
		if !valid {
			sig[0] ^= 1
		}
		r := httptest.NewRequest(http.MethodPost, "https://example.com/api/v1/discord/interactions", strings.NewReader(body))
		r.Header.Set("X-Signature-Timestamp", stamp)
		r.Header.Set("X-Signature-Ed25519", hex.EncodeToString(sig))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	if rec := request(false); rec.Code != 401 {
		t.Fatal("invalid signature passed", rec.Code, rec.Body)
	}
	if rec := request(true); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"type":1`) {
		t.Fatal("valid Discord ping rejected", rec.Code, rec.Body)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "https://example.com/api/v1/discord-bot", nil))
	if rec.Code != 401 {
		t.Fatal("bot configuration is public", rec.Code)
	}
}
