package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"modernc.org/sqlite"
)

// RequestAccount is the legacy management representation. New account
// management uses Account; this view keeps request history and existing API
// consumers attached to the same system-local request profile ID.
type RequestAccount struct {
	ID            int64  `json:"id"`
	Username      string `json:"username"`
	Disabled      bool   `json:"disabled"`
	CreatedAt     int64  `json:"created_at"`
	DiscordUserID string `json:"discord_user_id"`
}

func (s *Service) CreateRequestAccount(ctx context.Context, username, password string) (RequestAccount, error) {
	a, err := s.CreateAccount(ctx, username, password, true, false, false)
	if err != nil {
		return RequestAccount{}, err
	}
	return RequestAccount{ID: a.RequestAccountID, Username: a.Username, CreatedAt: a.CreatedAt}, nil
}

func (s *Service) RequestAccounts(ctx context.Context) ([]RequestAccount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.id,a.username,a.disabled OR NOT a.requests_access,r.created_at,COALESCE(r.discord_user_id,'') FROM accounts a JOIN request_accounts r ON r.id=a.request_account_id ORDER BY a.username COLLATE NOCASE,a.id`)
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

// SetRequestAccountDisabled retains the legacy endpoint's global-revocation
// semantics. The new Accounts API updates the same central disabled flag.
func (s *Service) SetRequestAccountDisabled(ctx context.Context, id int64, disabled bool) error {
	if id < 1 {
		return sql.ErrNoRows
	}
	var central int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM accounts WHERE request_account_id=?`, id).Scan(&central)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.ErrNoRows
	}
	if err != nil {
		return err
	}
	update := AccountUpdate{Disabled: &disabled}
	if !disabled {
		// The legacy enable action explicitly re-enables its request system;
		// it must never leave Discord enabled while the central grant is off.
		requests := true
		update.Requests = &requests
	}
	_, err = s.UpdateAccount(ctx, central, update)
	return err
}

func (s *Service) ChangeRequestPassword(ctx context.Context, id int64, currentPassword, newPassword string) error {
	if id < 1 {
		return ErrUnauthorized
	}
	var central int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM accounts WHERE request_account_id=?`, id).Scan(&central)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUnauthorized
	}
	if err != nil {
		return err
	}
	return s.ChangeAccountPassword(ctx, central, currentPassword, newPassword)
}

// isUsernameUniqueConflict reports whether err is the driver's UNIQUE
// constraint failure on the named column. Kept for compatibility with callers
// that translate legacy request-account write errors.
func isUsernameUniqueConflict(err error, column string) bool {
	if err == nil {
		return false
	}
	var serr *sqlite.Error
	if errors.As(err, &serr) && strings.Contains(serr.Error(), "UNIQUE constraint failed: "+column) {
		return true
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed: "+column)
}
