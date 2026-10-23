package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/faaaaq/ReelScarey/internal/securestore"
)

var (
	ErrUnauthorized = errors.New("The username or password is incorrect.")
	ErrSetupClosed  = errors.New("The administrator account has already been created.")
	ErrSetupToken   = errors.New("The setup code is incorrect. Read setup-token from your config folder.")
	ErrPassword     = errors.New("Use a password of at least 12 characters and at most 256 bytes.")
	ErrUsername     = errors.New("Use a username of 3–40 letters, numbers, underscores or hyphens.")
	ErrBusy         = errors.New("Too many authentication attempts. Wait a minute and try again.")
)

const SessionLifetime = 24 * time.Hour

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{3,40}$`)

type Session struct {
	Username  string `json:"username"`
	Token     string `json:"-"`
	CSRFToken string `json:"csrf_token"`
	ExpiresAt int64  `json:"expires_at"`
}
type attempt struct {
	count int
	until time.Time
}
type Service struct {
	db        *sql.DB
	setupPath string
	mu        sync.Mutex
	attempts  map[string]attempt
	hashing   chan struct{}
	global    attempt
}

func New(ctx context.Context, db *sql.DB, dir string) (*Service, error) {
	s := &Service{db: db, setupPath: filepath.Join(dir, "setup-token"), attempts: make(map[string]attempt), hashing: make(chan struct{}, 2)}
	configured, err := s.Configured(ctx)
	if err != nil {
		return nil, err
	}
	if !configured {
		_, err = securestore.SecretFile(s.setupPath, 32, true)
	} else {
		err = s.removeSetupToken()
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) Configured(ctx context.Context) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM admin_account").Scan(&count)
	return count > 0, err
}

// Limit is bounded by both remote IP and process-wide work. Forwarded headers
// are deliberately ignored. No credential values enter the limiter or logs.
func (s *Service) Limit(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for key, entry := range s.attempts {
		if now.After(entry.until) {
			delete(s.attempts, key)
		}
	}
	if now.After(s.global.until) {
		s.global = attempt{until: now.Add(time.Minute)}
	}
	entry := s.attempts[ip]
	if entry.until.IsZero() {
		entry.until = now.Add(time.Minute)
	}
	if entry.count >= 10 || s.global.count >= 60 || (len(s.attempts) >= 2048 && entry.count == 0) {
		return false
	}
	entry.count++
	s.global.count++
	s.attempts[ip] = entry
	return true
}

func (s *Service) acquire() bool {
	select {
	case s.hashing <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Service) Setup(ctx context.Context, username, password, code string) (Session, error) {
	configured, err := s.Configured(ctx)
	if err != nil {
		return Session{}, err
	}
	if configured {
		return Session{}, ErrSetupClosed
	}
	secret, err := securestore.SecretFile(s.setupPath, 32, false)
	if err != nil {
		return Session{}, err
	}
	expected := base64.RawURLEncoding.EncodeToString(secret)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) != 1 {
		return Session{}, ErrSetupToken
	}
	username = strings.TrimSpace(username)
	if !usernamePattern.MatchString(username) {
		return Session{}, ErrUsername
	}
	if !s.acquire() {
		return Session{}, ErrBusy
	}
	defer func() { <-s.hashing }()
	hash, err := hashPassword(password)
	if err != nil {
		return Session{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "INSERT INTO admin_account(id, username, password_hash, created_at) VALUES(1, ?, ?, ?) ON CONFLICT(id) DO NOTHING", username, hash, time.Now().Unix())
	if err != nil {
		return Session{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return Session{}, err
	}
	if count == 0 {
		return Session{}, ErrSetupClosed
	}
	session, err := issue(ctx, tx, username)
	if err != nil {
		return Session{}, err
	}
	if err := tx.Commit(); err != nil {
		return Session{}, err
	}
	// A leftover file cannot reopen setup; the singleton database row gates it.
	_ = s.removeSetupToken()
	return session, nil
}

func (s *Service) Login(ctx context.Context, username, password string) (Session, error) {
	if !s.acquire() {
		return Session{}, ErrBusy
	}
	defer func() { <-s.hashing }()
	var storedUser, encoded string
	err := s.db.QueryRowContext(ctx, "SELECT username, password_hash FROM admin_account WHERE id = 1").Scan(&storedUser, &encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrUnauthorized
	}
	if err != nil {
		return Session{}, err
	}
	valid := verifyPassword(encoded, password)
	if !valid || subtle.ConstantTimeCompare([]byte(storedUser), []byte(username)) != 1 {
		return Session{}, ErrUnauthorized
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback()
	// Verify that a concurrent password reset did not invalidate this login.
	var current string
	if err := tx.QueryRowContext(ctx, "SELECT password_hash FROM admin_account WHERE id = 1").Scan(&current); err != nil {
		return Session{}, err
	}
	if current != encoded {
		return Session{}, ErrUnauthorized
	}
	session, err := issue(ctx, tx, storedUser)
	if err != nil {
		return Session{}, err
	}
	if err := tx.Commit(); err != nil {
		return Session{}, err
	}
	return session, nil
}

func digest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func CSRF(token string) string { return digest("reelscarey:csrf:" + token) }
func randomToken() string {
	var secret [32]byte
	_, _ = rand.Read(secret[:])
	return base64.RawURLEncoding.EncodeToString(secret[:])
}

func issue(ctx context.Context, tx *sql.Tx, username string) (Session, error) {
	token := randomToken()
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ? OR token_hash IN (SELECT token_hash FROM sessions ORDER BY created_at DESC, rowid DESC LIMIT -1 OFFSET 9)", now); err != nil {
		return Session{}, err
	}
	session := Session{Username: username, Token: token, CSRFToken: CSRF(token), ExpiresAt: time.Now().Add(SessionLifetime).Unix()}
	_, err := tx.ExecContext(ctx, "INSERT INTO sessions(token_hash, expires_at, created_at) VALUES (?, ?, ?)", digest(token), session.ExpiresAt, now)
	return session, err
}

func (s *Service) Resolve(ctx context.Context, token string) (Session, error) {
	if len(token) != 43 {
		return Session{}, ErrUnauthorized
	}
	var session Session
	err := s.db.QueryRowContext(ctx, "SELECT a.username, s.expires_at FROM sessions s CROSS JOIN admin_account a WHERE s.token_hash = ? AND s.expires_at > ?", digest(token), time.Now().Unix()).Scan(&session.Username, &session.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrUnauthorized
	}
	if err != nil {
		return Session{}, err
	}
	session.Token = token
	session.CSRFToken = CSRF(token)
	return session, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", digest(token))
	return err
}

// ResetPassword is used by the owner's local CLI. It revokes every session.
func (s *Service) ResetPassword(ctx context.Context, password string) error {
	if !s.acquire() {
		return ErrBusy
	}
	defer func() { <-s.hashing }()
	encoded, err := hashPassword(password)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE admin_account SET password_hash = ? WHERE id = 1", encoded)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return errors.New("create the administrator account before resetting its password")
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) removeSetupToken() error {
	err := os.Remove(s.setupPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
