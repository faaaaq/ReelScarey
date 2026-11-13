package discordbot

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/requests"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

var snowflake = regexp.MustCompile(`^[0-9]{16,22}$`)
var botToken = regexp.MustCompile(`^[A-Za-z0-9._-]{40,300}$`)
var ErrConfig = errors.New("Enter valid Discord application, server, administrator and channel IDs, public key and bot token.")

type Config struct {
	ApplicationID  string `json:"application_id"`
	PublicKey      string `json:"public_key"`
	GuildID        string `json:"guild_id"`
	AdminUserID    string `json:"admin_user_id"`
	AdminChannelID string `json:"admin_channel_id"`
	Configured     bool   `json:"configured"`
	ciphertext     []byte
}
type botSecret struct {
	Token        string `json:"token"`
	SelectionKey string `json:"selection_key"`
}
type transferServiceInterface interface {
	Search(context.Context, int64, string) ([]transfer.Release, error)
	SearchTV(context.Context, int64, int, int, string) ([]transfer.Release, error)
	Queue(context.Context, string) (transfer.Job, bool, error)
}
type Service struct {
	db       *sql.DB
	secrets  *securestore.Store
	metadata *metadata.Service
	transfer transferServiceInterface
	library  *library.Store
	requests *requests.Store
	client   *http.Client
	mu       sync.Mutex
	// noticeAttempts counts 429 requeues per notice id in memory. It caps
	// the requeue loop; restarts reset it, which costs at most one more
	// bounded batch of retries per notice (no schema change required).
	noticeAttempts map[int64]int
}

func New(db *sql.DB, secrets *securestore.Store, meta *metadata.Service, transferService *transfer.Service) *Service {
	var transferAdapter transferServiceInterface
	if transferService != nil {
		transferAdapter = transferService
	}
	return &Service{db: db, secrets: secrets, metadata: meta, transfer: transferAdapter, library: library.New(db), requests: requests.New(db), client: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirects disabled") }}, noticeAttempts: map[int64]int{}}
}
func (s *Service) Config(ctx context.Context) (Config, error) {
	var c Config
	err := s.db.QueryRowContext(ctx, `SELECT application_id,public_key,guild_id,admin_user_id,admin_channel_id,ciphertext FROM discord_bot_config WHERE id=1`).Scan(&c.ApplicationID, &c.PublicKey, &c.GuildID, &c.AdminUserID, &c.AdminChannelID, &c.ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	c.Configured = true
	return c, nil
}
func (s *Service) Save(ctx context.Context, c Config, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, err := hex.DecodeString(c.PublicKey)
	if !snowflake.MatchString(c.ApplicationID) || !snowflake.MatchString(c.GuildID) || !snowflake.MatchString(c.AdminUserID) || !snowflake.MatchString(c.AdminChannelID) || err != nil || len(key) != 32 || !botToken.MatchString(token) {
		return ErrConfig
	}
	var random [32]byte
	if _, err = rand.Read(random[:]); err != nil {
		return err
	}
	raw, err := json.Marshal(botSecret{Token: token, SelectionKey: base64.RawURLEncoding.EncodeToString(random[:])})
	if err != nil {
		return err
	}
	sealed, err := s.secrets.Seal("discord-bot", raw)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE discord_bot_jobs SET state='uncertain',updated_at=unixepoch() WHERE state IN ('queued','running')`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE discord_bot_notices SET state='uncertain',updated_at=unixepoch() WHERE state IN ('queued','sending')`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM discord_bot_choices`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO discord_bot_config(id,application_id,public_key,guild_id,admin_user_id,admin_channel_id,ciphertext,updated_at) VALUES(1,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET application_id=excluded.application_id,public_key=excluded.public_key,guild_id=excluded.guild_id,admin_user_id=excluded.admin_user_id,admin_channel_id=excluded.admin_channel_id,ciphertext=excluded.ciphertext,updated_at=excluded.updated_at`, c.ApplicationID, strings.ToLower(c.PublicKey), c.GuildID, c.AdminUserID, c.AdminChannelID, sealed, time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) secret(ctx context.Context) (botSecret, error) {
	var sealed []byte
	if err := s.db.QueryRowContext(ctx, `SELECT ciphertext FROM discord_bot_config WHERE id=1`).Scan(&sealed); err != nil {
		return botSecret{}, err
	}
	plain, err := s.secrets.Open("discord-bot", sealed)
	if err != nil {
		return botSecret{}, err
	}
	var result botSecret
	if json.Unmarshal(plain, &result) != nil || !botToken.MatchString(result.Token) {
		return botSecret{}, ErrConfig
	}
	return result, nil
}
func (s *Service) Remove(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE discord_bot_jobs SET state='uncertain',updated_at=unixepoch() WHERE state IN ('queued','running')`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE discord_bot_notices SET state='uncertain',updated_at=unixepoch() WHERE state IN ('queued','sending')`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM discord_bot_choices`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM discord_bot_config WHERE id=1`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) Link(ctx context.Context, account int64, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if account < 1 || userID != "" && !snowflake.MatchString(userID) {
		return ErrConfig
	}
	var value any
	if userID != "" {
		value = userID
	}
	result, err := s.db.ExecContext(ctx, `UPDATE request_accounts SET discord_user_id=? WHERE id=?`, value, account)
	if err != nil {
		return ErrConfig
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	return nil
}

// DisableAccount serializes revocation with bot actions so a queued button
// cannot start a download after an administrator has disabled the account.
func (s *Service) DisableAccount(ctx context.Context, account int64, disabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if account < 1 {
		return sql.ErrNoRows
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE request_accounts SET disabled=? WHERE id=?`, disabled, account)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	if disabled {
		if _, err = tx.ExecContext(ctx, `DELETE FROM request_sessions WHERE account_id=?`, account); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Service) RetryNotice(ctx context.Context, requestID int64) error {
	result, err := s.db.ExecContext(ctx, `UPDATE discord_bot_notices SET state='queued',updated_at=unixepoch() WHERE request_id=? AND state='uncertain' AND EXISTS(SELECT 1 FROM media_requests WHERE id=? AND state='pending') AND EXISTS(SELECT 1 FROM discord_bot_config WHERE id=1)`, requestID, requestID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	return nil
}
