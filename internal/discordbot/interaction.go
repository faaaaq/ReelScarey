package discordbot

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/metadata"
)

type interaction struct {
	ID            string `json:"id"`
	ApplicationID string `json:"application_id"`
	GuildID       string `json:"guild_id"`
	Token         string `json:"token"`
	Type          int    `json:"type"`
	Member        struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	} `json:"member"`
	Data struct {
		Name     string `json:"name"`
		CustomID string `json:"custom_id"`
		Options  []struct {
			Name  string          `json:"name"`
			Value json.RawMessage `json:"value"`
		} `json:"options"`
	} `json:"data"`
}
type jobPayload struct {
	Kind      string `json:"kind,omitempty"`
	Query     string `json:"query,omitempty"`
	TMDbID    int64  `json:"tmdb_id,omitempty"`
	RequestID int64  `json:"request_id,omitempty"`
	Season    int    `json:"season,omitempty"`
	Episode   int    `json:"episode,omitempty"`
	ReleaseID string `json:"release_id,omitempty"`
}

// ServeInteraction is the only public bot entry point. Discord authenticates
// every payload; access is then narrowed to the configured server and linked
// account or administrator. Only durable jobs are queued before acknowledging.
func (s *Service) ServeInteraction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	c, err := s.Config(r.Context())
	if err != nil || !c.Configured {
		http.Error(w, "unavailable", 503)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 65537))
	if err != nil || len(body) > 65536 {
		http.Error(w, "invalid request", 400)
		return
	}
	if Verify(c.PublicKey, r.Header.Get("X-Signature-Ed25519"), r.Header.Get("X-Signature-Timestamp"), body, time.Now()) != nil {
		http.Error(w, "invalid signature", 401)
		return
	}
	var in interaction
	if json.Unmarshal(body, &in) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	if in.Type == 1 {
		writeInteraction(w, map[string]any{"type": 1})
		return
	}
	actor := in.Member.User.ID
	if in.ApplicationID != c.ApplicationID || in.GuildID != c.GuildID || !snowflake.MatchString(in.ID) || !snowflake.MatchString(actor) || len(in.Token) < 20 || len(in.Token) > 256 || strings.IndexFunc(in.Token, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.')
	}) >= 0 {
		http.Error(w, "unauthorized", 403)
		return
	}
	var account int64
	err = s.db.QueryRowContext(r.Context(), `SELECT id FROM request_accounts WHERE discord_user_id=? AND disabled=0`, actor).Scan(&account)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "unavailable", 503)
		return
	}
	if account == 0 && actor != c.AdminUserID {
		writeInteraction(w, ephemeral("Ask the administrator to link your Discord account."))
		return
	}
	kind, payload, choice, err := s.interactionJob(r.Context(), c, in, actor, account)
	if err != nil {
		writeInteraction(w, ephemeral("This action is unavailable or has expired."))
		return
	}
	sealed, err := s.secrets.Seal("discord-job:"+in.ID, []byte(in.Token))
	if err != nil {
		http.Error(w, "unavailable", 503)
		return
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "unavailable", 503)
		return
	}
	defer tx.Rollback()
	var current int
	if err = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM discord_bot_config WHERE id=1 AND application_id=? AND public_key=? AND guild_id=? AND admin_user_id=? AND ciphertext=?`, c.ApplicationID, c.PublicKey, c.GuildID, c.AdminUserID, c.ciphertext).Scan(&current); err != nil || current != 1 {
		http.Error(w, "unavailable", 503)
		return
	}
	var recent int
	if err = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM discord_bot_jobs WHERE actor_id=? AND created_at>=unixepoch()-60`, actor).Scan(&recent); err != nil {
		http.Error(w, "unavailable", 503)
		return
	}
	if recent >= 20 {
		writeInteraction(w, ephemeral("Too many interactions. Try again in a minute."))
		return
	}
	var accountValue any
	if account > 0 {
		accountValue = account
	}
	result, err := tx.ExecContext(r.Context(), `INSERT INTO discord_bot_jobs(id,account_id,actor_id,kind,payload,ciphertext,state,created_at,updated_at) VALUES(?,?,?,?,?,?,'queued',unixepoch(),unixepoch()) ON CONFLICT(id) DO NOTHING`, in.ID, accountValue, actor, kind, string(raw), sealed)
	if err != nil {
		http.Error(w, "unavailable", 503)
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "unavailable", 503)
		return
	}
	if count == 0 {
		writeInteraction(w, ephemeral("This interaction has already been received."))
		return
	}
	if choice != "" {
		result, err = tx.ExecContext(r.Context(), `UPDATE discord_bot_choices SET consumed_at=unixepoch() WHERE id=? AND actor_id=? AND account_id=? AND consumed_at=0 AND expires_at>unixepoch()`, choice, actor, account)
		if err != nil {
			http.Error(w, "unavailable", 503)
			return
		}
		n, e := result.RowsAffected()
		if e != nil || n != 1 {
			writeInteraction(w, ephemeral("This selection has expired or was already used."))
			return
		}
	}
	if err = tx.Commit(); err != nil {
		http.Error(w, "unavailable", 503)
		return
	}
	writeInteraction(w, map[string]any{"type": 5, "data": map[string]any{"flags": 64}})
}
func writeInteraction(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func ephemeral(message string) map[string]any {
	return map[string]any{"type": 4, "data": map[string]any{"content": message, "flags": 64, "allowed_mentions": map[string]any{"parse": []string{}}}}
}

func (s *Service) interactionJob(ctx context.Context, c Config, in interaction, actor string, account int64) (string, jobPayload, string, error) {
	var p jobPayload
	if in.Type == 2 {
		options := map[string]json.RawMessage{}
		for _, v := range in.Data.Options {
			if _, exists := options[v.Name]; exists {
				return "", p, "", ErrConfig
			}
			options[v.Name] = v.Value
		}
		switch in.Data.Name {
		case "request":
			if account == 0 || len(options) != 2 || json.Unmarshal(options["kind"], &p.Kind) != nil || json.Unmarshal(options["query"], &p.Query) != nil || (p.Kind != "movie" && p.Kind != "series") || !metadata.ValidQuery(strings.TrimSpace(p.Query), 1) {
				return "", p, "", ErrConfig
			}
			p.Query = strings.TrimSpace(p.Query)
			return "search", p, "", nil
		case "releases":
			if (account == 0 && actor != c.AdminUserID) || len(options) < 1 || len(options) > 3 || json.Unmarshal(options["request_id"], &p.RequestID) != nil || p.RequestID < 1 {
				return "", p, "", ErrConfig
			}
			for name := range options {
				if name != "request_id" && name != "season" && name != "episode" {
					return "", p, "", ErrConfig
				}
			}
			if value, ok := options["season"]; ok && json.Unmarshal(value, &p.Season) != nil {
				return "", p, "", ErrConfig
			}
			if value, ok := options["episode"]; ok && json.Unmarshal(value, &p.Episode) != nil {
				return "", p, "", ErrConfig
			}
			if p.Season < 0 || p.Season > 999 || p.Episode < 0 || p.Episode > 9999 {
				return "", p, "", ErrConfig
			}
			return "releases", p, "", nil
		}
	}
	if in.Type == 3 {
		parts := strings.SplitN(in.Data.CustomID, ":", 2)
		if len(parts) != 2 {
			return "", p, "", ErrConfig
		}
		switch parts[0] {
		case "request":
			if account == 0 || len(parts[1]) != 32 {
				return "", p, "", ErrConfig
			}
			var raw string
			err := s.db.QueryRowContext(ctx, `SELECT payload FROM discord_bot_choices WHERE id=? AND actor_id=? AND account_id=? AND kind='request' AND consumed_at=0 AND expires_at>unixepoch()`, parts[1], actor, account).Scan(&raw)
			if err != nil || json.Unmarshal([]byte(raw), &p) != nil {
				return "", p, "", ErrConfig
			}
			return "request", p, parts[1], nil
		case "grab":
			secret, err := s.secret(ctx)
			if err != nil {
				return "", p, "", ErrConfig
			}
			key, err := base64.RawURLEncoding.DecodeString(secret.SelectionKey)
			if err != nil {
				return "", p, "", ErrConfig
			}
			p.ReleaseID, err = VerifySelection(key, parts[1], actor, time.Now())
			if err != nil {
				return "", p, "", err
			}
			return "grab", p, "", nil
		case "approve":
			if actor != c.AdminUserID {
				return "", p, "", ErrConfig
			}
			var err error
			p.RequestID, err = strconv.ParseInt(parts[1], 10, 64)
			if err != nil || p.RequestID < 1 {
				return "", p, "", ErrConfig
			}
			return "approve", p, "", nil
		}
	}
	return "", p, "", fmt.Errorf("unsupported interaction")
}
