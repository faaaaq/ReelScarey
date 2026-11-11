package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// APIKey is the non-secret view of a scoped integration token.
type APIKey struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Prefix     string   `json:"prefix"`
	Scopes     []string `json:"scopes"`
	CreatedAt  int64    `json:"created_at"`
	LastUsedAt int64    `json:"last_used_at"`
	ExpiresAt  int64    `json:"expires_at"`
	RevokedAt  int64    `json:"revoked_at"`
}

// CreatedAPIKey is returned exactly once, when the token is minted.
type CreatedAPIKey struct {
	APIKey
	Token string `json:"token"`
}

type APIKeyInput struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresAt int64    `json:"expires_at"`
}

var (
	ErrAPIKeyName   = errors.New("Use an API key name of 1–80 characters without control characters.")
	ErrAPIKeyScope  = errors.New("Choose at least one supported API scope.")
	ErrAPIKeyLimit  = errors.New("At most 20 API keys are supported. Revoke one first.")
	ErrAPIKeyExpiry = errors.New("Choose no expiry or a future expiry time.")
	ErrAPIKeyAbsent = errors.New("That API key does not exist or was already revoked.")
)

// SupportedAPIKeyScopes maps the compat scopes to the eight-character prefix
// used by the corresponding route group. A key may carry several scopes.
var SupportedAPIKeyScopes = []string{"compat-sonarr", "compat-radarr", "compat-prowlarr"}

func (k APIKey) HasScope(scope string) bool { return slices.Contains(k.Scopes, scope) }

func (k APIKey) HasAnyScope(scopes ...string) bool {
	for _, scope := range scopes {
		if k.HasScope(scope) {
			return true
		}
	}
	return false
}

func normalizeScopes(in []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, scope := range in {
		scope = strings.TrimSpace(scope)
		if !slices.Contains(SupportedAPIKeyScopes, scope) || seen[scope] {
			return nil, ErrAPIKeyScope
		}
		seen[scope] = true
		out = append(out, scope)
	}
	slices.Sort(out)
	if len(out) == 0 {
		return nil, ErrAPIKeyScope
	}
	return out, nil
}

func (s *Service) CreateAPIKey(ctx context.Context, in APIKeyInput) (CreatedAPIKey, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 80 || !utf8.ValidString(in.Name) || strings.IndexFunc(in.Name, unicode.IsControl) >= 0 {
		return CreatedAPIKey{}, ErrAPIKeyName
	}
	scopes, err := normalizeScopes(in.Scopes)
	if err != nil {
		return CreatedAPIKey{}, err
	}
	now := time.Now().Unix()
	if in.ExpiresAt < 0 || (in.ExpiresAt > 0 && in.ExpiresAt <= now) {
		return CreatedAPIKey{}, ErrAPIKeyExpiry
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CreatedAPIKey{}, err
	}
	defer tx.Rollback()
	var duplicate int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_keys WHERE name=? COLLATE NOCASE`, in.Name).Scan(&duplicate); err != nil {
		return CreatedAPIKey{}, err
	}
	if duplicate > 0 {
		return CreatedAPIKey{}, ErrAPIKeyName
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_keys WHERE revoked_at=0`).Scan(&count); err != nil {
		return CreatedAPIKey{}, err
	}
	if count >= 20 {
		return CreatedAPIKey{}, ErrAPIKeyLimit
	}
	encoded, err := json.Marshal(scopes)
	if err != nil {
		return CreatedAPIKey{}, err
	}
	var token string
	var id int64
	for attempt := 0; attempt < 3; attempt++ {
		token = "rs_" + base64.RawURLEncoding.EncodeToString(randomBytes(32))
		result, execErr := tx.ExecContext(ctx, `INSERT INTO api_keys(name,prefix,token_hash,scopes,created_at,expires_at) VALUES(?,?,?,?,?,?)`,
			in.Name, token[:12], digest(token), string(encoded), now, in.ExpiresAt)
		if execErr == nil {
			id, err = result.LastInsertId()
			if err != nil {
				return CreatedAPIKey{}, err
			}
			break
		}
		if !strings.Contains(execErr.Error(), "UNIQUE") {
			return CreatedAPIKey{}, execErr
		}
	}
	if id == 0 {
		return CreatedAPIKey{}, ErrAPIKeyLimit
	}
	if err := tx.Commit(); err != nil {
		return CreatedAPIKey{}, err
	}
	return CreatedAPIKey{APIKey: APIKey{ID: id, Name: in.Name, Prefix: token[:12], Scopes: scopes, CreatedAt: now, ExpiresAt: in.ExpiresAt}, Token: token}, nil
}

func (s *Service) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,prefix,scopes,created_at,last_used_at,expires_at,revoked_at FROM api_keys ORDER BY revoked_at<>0, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIKey{}
	for rows.Next() {
		var key APIKey
		var scopes string
		if err := rows.Scan(&key.ID, &key.Name, &key.Prefix, &scopes, &key.CreatedAt, &key.LastUsedAt, &key.ExpiresAt, &key.RevokedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(scopes), &key.Scopes); err != nil {
			return nil, err
		}
		if key.Scopes == nil {
			key.Scopes = []string{}
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

func (s *Service) RevokeAPIKey(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `UPDATE api_keys SET revoked_at=? WHERE id=? AND revoked_at=0`, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrAPIKeyAbsent
	}
	return nil
}

// ResolveAPIKey accepts an opaque token, checks its digest and returns the
// scoped identity. Revoked and expired keys fail closed.
func (s *Service) ResolveAPIKey(ctx context.Context, token string) (APIKey, error) {
	token = strings.TrimSpace(token)
	if len(token) != 46 || !strings.HasPrefix(token, "rs_") {
		return APIKey{}, ErrUnauthorized
	}
	var key APIKey
	var scopes, storedHash string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,prefix,token_hash,scopes,created_at,last_used_at,expires_at,revoked_at FROM api_keys WHERE prefix=?`, token[:12]).
		Scan(&key.ID, &key.Name, &key.Prefix, &storedHash, &scopes, &key.CreatedAt, &key.LastUsedAt, &key.ExpiresAt, &key.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return APIKey{}, ErrUnauthorized
	}
	if err != nil {
		return APIKey{}, err
	}
	if subtle.ConstantTimeCompare([]byte(digest(token)), []byte(storedHash)) != 1 {
		return APIKey{}, ErrUnauthorized
	}
	if key.RevokedAt != 0 || (key.ExpiresAt != 0 && key.ExpiresAt <= time.Now().Unix()) {
		return APIKey{}, ErrUnauthorized
	}
	if err := json.Unmarshal([]byte(scopes), &key.Scopes); err != nil {
		return APIKey{}, err
	}
	now := time.Now().Unix()
	if now-key.LastUsedAt >= 60 {
		_, _ = s.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at=? WHERE id=? AND revoked_at=0`, now, key.ID)
		key.LastUsedAt = now
	}
	return key, nil
}

type apiKeyContextKey struct{}

func WithAPIKey(ctx context.Context, key APIKey) context.Context {
	return context.WithValue(ctx, apiKeyContextKey{}, key)
}

func APIKeyFrom(ctx context.Context) (APIKey, bool) {
	key, ok := ctx.Value(apiKeyContextKey{}).(APIKey)
	return key, ok
}

func randomBytes(n int) []byte {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return buf
}
