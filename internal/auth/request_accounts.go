package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type RequestAccount struct {
	ID            int64  `json:"id"`
	Username      string `json:"username"`
	Disabled      bool   `json:"disabled"`
	CreatedAt     int64  `json:"created_at"`
	DiscordUserID string `json:"discord_user_id"`
}

func (s *Service) CreateRequestAccount(ctx context.Context, username, password string) (RequestAccount, error) {
	username = strings.TrimSpace(username)
	if !usernamePattern.MatchString(username) {
		return RequestAccount{}, ErrUsername
	}
	if !s.acquire() {
		return RequestAccount{}, ErrBusy
	}
	defer func() { <-s.hashing }()
	hash, err := hashPassword(password)
	if err != nil {
		return RequestAccount{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RequestAccount{}, err
	}
	defer tx.Rollback()
	var configured int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_account`).Scan(&configured); err != nil {
		return RequestAccount{}, err
	}
	if configured != 1 {
		return RequestAccount{}, ErrUnauthorized
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM admin_account WHERE username=? COLLATE NOCASE)+(SELECT COUNT(*) FROM request_accounts WHERE username=? COLLATE NOCASE)`, username, username).Scan(&count); err != nil {
		return RequestAccount{}, err
	}
	if count != 0 {
		return RequestAccount{}, ErrAccountExists
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_accounts`).Scan(&count); err != nil {
		return RequestAccount{}, err
	}
	if count >= 100 {
		return RequestAccount{}, errors.New("The request account limit is 100.")
	}
	now := time.Now().Unix()
	result, err := tx.ExecContext(ctx, `INSERT INTO request_accounts(username,password_hash,created_at) VALUES(?,?,?)`, username, hash, now)
	if err != nil {
		return RequestAccount{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return RequestAccount{}, err
	}
	if err = tx.Commit(); err != nil {
		return RequestAccount{}, err
	}
	return RequestAccount{ID: id, Username: username, CreatedAt: now}, nil
}

func (s *Service) RequestAccounts(ctx context.Context) ([]RequestAccount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,username,disabled,created_at,COALESCE(discord_user_id,'') FROM request_accounts ORDER BY username COLLATE NOCASE,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []RequestAccount{}
	for rows.Next() {
		var a RequestAccount
		if err = rows.Scan(&a.ID, &a.Username, &a.Disabled, &a.CreatedAt, &a.DiscordUserID); err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return items, rows.Err()
}

func (s *Service) SetRequestAccountDisabled(ctx context.Context, id int64, disabled bool) error {
	if id < 1 {
		return sql.ErrNoRows
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE request_accounts SET disabled=? WHERE id=?`, disabled, id)
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
		if _, err = tx.ExecContext(ctx, `DELETE FROM request_sessions WHERE account_id=?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Service) ChangeRequestPassword(ctx context.Context, id int64, currentPassword, newPassword string) error {
	if id < 1 {
		return ErrUnauthorized
	}
	if !s.acquire() {
		return ErrBusy
	}
	defer func() { <-s.hashing }()
	var encoded string
	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM request_accounts WHERE id=? AND disabled=0`, id).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUnauthorized
	}
	if err != nil {
		return err
	}
	if !verifyPassword(encoded, currentPassword) {
		return ErrUnauthorized
	}
	next, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE request_accounts SET password_hash=? WHERE id=? AND disabled=0 AND password_hash=?`, next, id, encoded)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrUnauthorized
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM request_sessions WHERE account_id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// Called only after Login has acquired the shared password-work limiter.
func (s *Service) loginRequester(ctx context.Context, username, password string) (Session, error) {
	var id int64
	var stored, encoded string
	err := s.db.QueryRowContext(ctx, `SELECT id,username,password_hash FROM request_accounts WHERE username=? COLLATE NOCASE AND disabled=0`, username).Scan(&id, &stored, &encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrUnauthorized
	}
	if err != nil {
		return Session{}, err
	}
	if !verifyPassword(encoded, password) {
		return Session{}, ErrUnauthorized
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback()
	var current string
	if err = tx.QueryRowContext(ctx, `SELECT password_hash FROM request_accounts WHERE id=? AND disabled=0`, id).Scan(&current); errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrUnauthorized
	} else if err != nil {
		return Session{}, err
	}
	if current != encoded {
		return Session{}, ErrUnauthorized
	}
	now := time.Now().Unix()
	if _, err = tx.ExecContext(ctx, `DELETE FROM request_sessions WHERE expires_at<=? OR (account_id=? AND token_hash IN (SELECT token_hash FROM request_sessions WHERE account_id=? ORDER BY created_at DESC,rowid DESC LIMIT -1 OFFSET 9))`, now, id, id); err != nil {
		return Session{}, err
	}
	token := randomToken()
	session := Session{Username: stored, Token: token, CSRFToken: CSRF(token), ExpiresAt: time.Now().Add(SessionLifetime).Unix(), Role: "requester", AccountID: id}
	if _, err = tx.ExecContext(ctx, `INSERT INTO request_sessions(token_hash,account_id,expires_at,created_at) VALUES(?,?,?,?)`, digest(token), id, session.ExpiresAt, now); err != nil {
		return Session{}, err
	}
	if err = tx.Commit(); err != nil {
		return Session{}, err
	}
	return session, nil
}
