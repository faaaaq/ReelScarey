package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/securestore"
)

const (
	AccessRequests    = "requests"
	AccessMusicPlayer = "music_player"
	AccessSubsonic    = "subsonic"

	// Keep this purpose identical to the existing Subsonic credential store.
	// Ciphertext remains usable after renaming an account or moving its link.
	musicServerUserPasswordPurpose = "music-server-user-password"
)

var ErrPasswordSetupRequired = errors.New("Set a new account password before granting browser or Subsonic access.")
var ErrSubsonicProfileMissing = errors.New("The Subsonic profile is missing. Disable and re-enable Subsonic explicitly to recreate it.")

// Account is the centralized identity. RequestAccountID and MusicServerUserID
// are system-local profile IDs, retained so existing request and playback
// state never has to be renumbered.
type Account struct {
	ID                int64  `json:"id"`
	Username          string `json:"username"`
	Disabled          bool   `json:"disabled"`
	Requests          bool   `json:"requests"`
	MusicPlayer       bool   `json:"music_player"`
	Subsonic          bool   `json:"subsonic"`
	PasswordSet       bool   `json:"password_set"`
	RequestAccountID  int64  `json:"request_account_id,omitempty"`
	MusicServerUserID int64  `json:"music_server_user_id,omitempty"`
	DiscordUserID     string `json:"discord_user_id,omitempty"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
}

// AccountUpdate uses pointers so an omitted permission keeps its saved value.
type AccountUpdate struct {
	Username    string
	Disabled    *bool
	Requests    *bool
	MusicPlayer *bool
	Subsonic    *bool
}

func (s *Service) SetSecrets(store *securestore.Store) { s.secrets = store }

// loginAccount is called only after Login has acquired the shared password
// work limiter. Request-backed accounts keep using request_sessions for
// compatibility; all other central accounts use account_sessions.
func (s *Service) loginAccount(ctx context.Context, username, password string) (Session, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts a LEFT JOIN request_accounts r ON r.id=a.request_account_id WHERE a.username=? COLLATE NOCASE AND a.disabled=0 AND a.password_hash<>''`, username)
	a, encoded, err := scanAccount(row)
	if errors.Is(err, sql.ErrNoRows) {
		dummyPasswordWork(password)
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
	current, currentHash, err := accountTx(ctx, tx, a.ID)
	if err != nil || current.Disabled || currentHash != encoded {
		return Session{}, ErrUnauthorized
	}
	now := time.Now().Unix()
	token := randomToken()
	expires := time.Now().Add(SessionLifetime).Unix()
	session := Session{Username: current.Username, Token: token, CSRFToken: CSRF(token), ExpiresAt: expires, AccountID: current.ID, UserAccountID: current.ID, RequestID: current.RequestAccountID, Access: accessList(current.Requests, current.MusicPlayer, current.Subsonic)}
	if current.Requests && current.RequestAccountID > 0 {
		session.Role = "requester"
		session.AccountID = current.RequestAccountID
	} else {
		session.Role = "user"
	}
	if session.Role == "requester" {
		if _, err = tx.ExecContext(ctx, `DELETE FROM request_sessions WHERE expires_at<=? OR (account_id=? AND token_hash IN (SELECT token_hash FROM request_sessions WHERE account_id=? ORDER BY created_at DESC,rowid DESC LIMIT -1 OFFSET 9))`, now, current.RequestAccountID, current.RequestAccountID); err != nil {
			return Session{}, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO request_sessions(token_hash,account_id,expires_at,created_at) VALUES(?,?,?,?)`, digest(token), current.RequestAccountID, expires, now)
	} else {
		if _, err = tx.ExecContext(ctx, `DELETE FROM account_sessions WHERE expires_at<=? OR (account_id=? AND token_hash IN (SELECT token_hash FROM account_sessions WHERE account_id=? ORDER BY created_at DESC,rowid DESC LIMIT -1 OFFSET 9))`, now, current.ID, current.ID); err != nil {
			return Session{}, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO account_sessions(token_hash,account_id,expires_at,created_at) VALUES(?,?,?,?)`, digest(token), current.ID, expires, now)
	}
	if err != nil {
		return Session{}, err
	}
	if err = tx.Commit(); err != nil {
		return Session{}, err
	}
	return session, nil
}

func accessList(requests, player, subsonic bool) []string {
	items := make([]string, 0, 3)
	if requests {
		items = append(items, AccessRequests)
	}
	if player {
		items = append(items, AccessMusicPlayer)
	}
	if subsonic {
		items = append(items, AccessSubsonic)
	}
	return items
}

// HasAccess is deliberately explicit for non-administrators. The administrator
// bypass is confined to the singleton owner account and is not an assignable
// account permission.
func (s Session) HasAccess(access string) bool {
	if s.Role == "admin" {
		return true
	}
	for _, granted := range s.Access {
		if granted == access {
			return true
		}
	}
	return false
}

// RequestAccountID resolves the system-local profile from the authenticated
// session, never from requester-controlled input.
func (s Session) RequestAccountID() int64 {
	if !s.HasAccess(AccessRequests) {
		return 0
	}
	if s.RequestID > 0 {
		return s.RequestID
	}
	if s.Role == "requester" {
		return s.AccountID
	}
	return 0
}

type accountRow interface {
	Scan(dest ...any) error
}

const accountColumns = `a.id,a.username,a.password_hash,a.disabled,a.requests_access,a.music_player_access,a.subsonic_access,COALESCE(a.request_account_id,0),COALESCE(a.music_server_user_id,0),COALESCE(r.discord_user_id,''),a.created_at,a.updated_at`

func scanAccount(row accountRow) (Account, string, error) {
	var a Account
	var hash string
	var disabled, requests, player, subsonic int
	err := row.Scan(&a.ID, &a.Username, &hash, &disabled, &requests, &player, &subsonic, &a.RequestAccountID, &a.MusicServerUserID, &a.DiscordUserID, &a.CreatedAt, &a.UpdatedAt)
	a.Disabled = disabled != 0
	a.Requests = requests != 0
	a.MusicPlayer = player != 0
	a.Subsonic = subsonic != 0
	a.PasswordSet = hash != ""
	return a, hash, err
}

func scanAccountSession(row accountRow, token string, expires int64, accountSession bool) (Session, error) {
	a, _, err := scanAccount(row)
	if err != nil {
		return Session{}, err
	}
	session := Session{Username: a.Username, Token: token, CSRFToken: CSRF(token), ExpiresAt: expires, AccountID: a.ID, UserAccountID: a.ID, RequestID: a.RequestAccountID, Access: accessList(a.Requests, a.MusicPlayer, a.Subsonic)}
	if !accountSession {
		// Preserve the original requester API contract for request-backed
		// browser sessions while also exposing the central identity.
		session.Role = "requester"
		session.AccountID = a.RequestAccountID
	} else {
		session.Role = "user"
	}
	return session, nil
}

func (s *Service) AccountByID(ctx context.Context, id int64) (Account, error) {
	a, _, err := s.account(ctx, id)
	return a, err
}

func (s *Service) account(ctx context.Context, id int64) (Account, string, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts a LEFT JOIN request_accounts r ON r.id=a.request_account_id WHERE a.id=?`, id)
	a, hash, err := scanAccount(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, "", sql.ErrNoRows
	}
	return a, hash, err
}

func accountTx(ctx context.Context, tx *sql.Tx, id int64) (Account, string, error) {
	row := tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts a LEFT JOIN request_accounts r ON r.id=a.request_account_id WHERE a.id=?`, id)
	return scanAccount(row)
}

func (s *Service) Accounts(ctx context.Context) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+accountColumns+` FROM accounts a LEFT JOIN request_accounts r ON r.id=a.request_account_id ORDER BY a.username COLLATE NOCASE,a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Account{}
	for rows.Next() {
		a, _, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return items, rows.Err()
}

func validateNewAccountName(ctx context.Context, tx *sql.Tx, username string, exclude int64) error {
	if !usernamePattern.MatchString(username) {
		return ErrUsername
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM admin_account WHERE username=? COLLATE NOCASE)+(SELECT COUNT(*) FROM accounts WHERE username=? COLLATE NOCASE AND id<>?)`, username, username, exclude).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return ErrAccountExists
	}
	return nil
}

func keep(value *bool, current bool) bool {
	if value == nil {
		return current
	}
	return *value
}

func ensureRequestProfile(ctx context.Context, tx *sql.Tx, a *Account, hash string) error {
	disabled := a.Disabled || !a.Requests
	if a.RequestAccountID > 0 {
		_, err := tx.ExecContext(ctx, `UPDATE request_accounts SET username=?,password_hash=?,disabled=? WHERE id=?`, a.Username, hash, disabled, a.RequestAccountID)
		return err
	}
	if !a.Requests {
		return nil
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO request_accounts(username,password_hash,disabled,created_at) VALUES(?,?,?,?)`, a.Username, hash, disabled, a.CreatedAt)
	if err != nil {
		return err
	}
	a.RequestAccountID, err = result.LastInsertId()
	return err
}

func ensureMusicProfile(ctx context.Context, tx *sql.Tx, a *Account, sealed []byte) error {
	if a.MusicServerUserID > 0 {
		if len(sealed) > 0 {
			_, err := tx.ExecContext(ctx, `UPDATE music_server_users SET username=?,password_sealed=?,password_generated=0,updated_at=unixepoch() WHERE id=?`, a.Username, sealed, a.MusicServerUserID)
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE music_server_users SET username=?,updated_at=unixepoch() WHERE id=?`, a.Username, a.MusicServerUserID)
		return err
	}
	if !a.Subsonic {
		return nil
	}
	if len(sealed) == 0 {
		return ErrPasswordSetupRequired
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO music_server_users(username,password_sealed,created_at,updated_at) VALUES(?,?,?,?)`, a.Username, sealed, a.CreatedAt, a.UpdatedAt)
	if err != nil {
		return err
	}
	a.MusicServerUserID, err = result.LastInsertId()
	return err
}

func accountUniqueError(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	return strings.Contains(text, "UNIQUE constraint failed") && (strings.Contains(text, "accounts.username") || strings.Contains(text, "request_accounts.username") || strings.Contains(text, "music_server_users.username"))
}

func (s *Service) CreateAccount(ctx context.Context, username, password string, requests, player, subsonic bool) (Account, error) {
	username = strings.TrimSpace(username)
	if !usernamePattern.MatchString(username) {
		return Account{}, ErrUsername
	}
	if !s.acquire() {
		return Account{}, ErrBusy
	}
	defer func() { <-s.hashing }()
	hash, err := hashPassword(password)
	if err != nil {
		return Account{}, err
	}
	var sealed []byte
	if s.secrets != nil {
		sealed, err = s.secrets.Seal(musicServerUserPasswordPurpose, []byte(password))
		if err != nil {
			return Account{}, err
		}
	} else if subsonic {
		return Account{}, ErrPasswordSetupRequired
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback()
	var configured int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_account`).Scan(&configured); err != nil {
		return Account{}, err
	}
	if configured != 1 {
		return Account{}, ErrUnauthorized
	}
	if err = validateNewAccountName(ctx, tx, username, 0); err != nil {
		return Account{}, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts`).Scan(&count); err != nil {
		return Account{}, err
	}
	if count >= 100 {
		return Account{}, errors.New("The account limit is 100.")
	}
	now := time.Now().Unix()
	a := Account{Username: username, Requests: requests, MusicPlayer: player, Subsonic: subsonic, CreatedAt: now, UpdatedAt: now}
	result, err := tx.ExecContext(ctx, `INSERT INTO accounts(username,password_hash,password_sealed,requests_access,music_player_access,subsonic_access,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, username, hash, sealed, requests, player, subsonic, now, now)
	if err != nil {
		if accountUniqueError(err) {
			return Account{}, ErrAccountExists
		}
		return Account{}, err
	}
	a.ID, err = result.LastInsertId()
	if err != nil {
		return Account{}, err
	}
	if err = ensureRequestProfile(ctx, tx, &a, hash); err != nil {
		if accountUniqueError(err) {
			return Account{}, ErrAccountExists
		}
		return Account{}, err
	}
	if err = ensureMusicProfile(ctx, tx, &a, sealed); err != nil {
		if accountUniqueError(err) {
			return Account{}, ErrAccountExists
		}
		return Account{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE accounts SET request_account_id=?,music_server_user_id=? WHERE id=?`, nullableID(a.RequestAccountID), nullableID(a.MusicServerUserID), a.ID); err != nil {
		return Account{}, err
	}
	if err = tx.Commit(); err != nil {
		return Account{}, err
	}
	a.PasswordSet = true
	return a, nil
}

func nullableID(id int64) any {
	if id < 1 {
		return nil
	}
	return id
}

func (s *Service) UpdateAccount(ctx context.Context, id int64, in AccountUpdate) (Account, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback()
	current, hash, err := accountTx(ctx, tx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, sql.ErrNoRows
	}
	if err != nil {
		return Account{}, err
	}
	next := current
	if username := strings.TrimSpace(in.Username); username != "" && username != current.Username {
		if err = validateNewAccountName(ctx, tx, username, id); err != nil {
			return Account{}, err
		}
		next.Username = username
	}
	next.Disabled = keep(in.Disabled, current.Disabled)
	next.Requests = keep(in.Requests, current.Requests)
	next.MusicPlayer = keep(in.MusicPlayer, current.MusicPlayer)
	next.Subsonic = keep(in.Subsonic, current.Subsonic)
	if (next.Requests || next.MusicPlayer) && hash == "" {
		return Account{}, ErrPasswordSetupRequired
	}
	var sealed []byte
	if err = tx.QueryRowContext(ctx, `SELECT password_sealed FROM accounts WHERE id=?`, id).Scan(&sealed); err != nil {
		return Account{}, err
	}
	if next.Subsonic && current.Subsonic && current.MusicServerUserID == 0 {
		return Account{}, ErrSubsonicProfileMissing
	}
	if next.Subsonic && len(sealed) == 0 && next.MusicServerUserID == 0 {
		return Account{}, ErrPasswordSetupRequired
	}
	next.UpdatedAt = time.Now().Unix()
	if err = ensureRequestProfile(ctx, tx, &next, hash); err != nil {
		if accountUniqueError(err) {
			return Account{}, ErrAccountExists
		}
		return Account{}, err
	}
	if err = ensureMusicProfile(ctx, tx, &next, sealed); err != nil {
		if accountUniqueError(err) {
			return Account{}, ErrAccountExists
		}
		return Account{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET username=?,disabled=?,requests_access=?,music_player_access=?,subsonic_access=?,request_account_id=?,music_server_user_id=?,updated_at=? WHERE id=?`, next.Username, next.Disabled, next.Requests, next.MusicPlayer, next.Subsonic, nullableID(next.RequestAccountID), nullableID(next.MusicServerUserID), next.UpdatedAt, id)
	if err != nil {
		if accountUniqueError(err) {
			return Account{}, ErrAccountExists
		}
		return Account{}, err
	}
	if next.Disabled {
		if err = revokeAccountSessions(ctx, tx, id, next.RequestAccountID); err != nil {
			return Account{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Account{}, err
	}
	next.PasswordSet = hash != ""
	return next, nil
}

func revokeAccountSessions(ctx context.Context, tx *sql.Tx, id, requestID int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM account_sessions WHERE account_id=?`, id); err != nil {
		return err
	}
	if requestID > 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM request_sessions WHERE account_id=?`, requestID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ResetAccountPassword(ctx context.Context, id int64, password string) error {
	return s.changeAccountPassword(ctx, id, "", password, false)
}

func (s *Service) ChangeAccountPassword(ctx context.Context, id int64, currentPassword, newPassword string) error {
	return s.changeAccountPassword(ctx, id, currentPassword, newPassword, true)
}

func (s *Service) changeAccountPassword(ctx context.Context, id int64, currentPassword, newPassword string, requireCurrent bool) error {
	if id < 1 {
		return ErrUnauthorized
	}
	if !s.acquire() {
		return ErrBusy
	}
	defer func() { <-s.hashing }()
	a, encoded, err := s.account(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if requireCurrent {
				return ErrUnauthorized
			}
			return sql.ErrNoRows
		}
		return err
	}
	if requireCurrent && (a.Disabled || encoded == "" || !verifyPassword(encoded, currentPassword)) {
		return ErrUnauthorized
	}
	if a.Subsonic && s.secrets == nil {
		return ErrPasswordSetupRequired
	}
	next, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	var sealed []byte
	if s.secrets != nil {
		sealed, err = s.secrets.Seal(musicServerUserPasswordPurpose, []byte(newPassword))
		if err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// A legacy Subsonic-only row may share its name with a request identity.
	// Setting a browser password must not make login names ambiguous.
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts WHERE username=(SELECT username FROM accounts WHERE id=?) AND password_hash<>'' AND id<>?`, id, id).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return ErrAccountExists
	}
	var current string
	if err = tx.QueryRowContext(ctx, `SELECT password_hash FROM accounts WHERE id=?`, id).Scan(&current); err != nil {
		return err
	}
	if current != encoded {
		return ErrUnauthorized
	}
	if _, err = tx.ExecContext(ctx, `UPDATE accounts SET password_hash=?,password_sealed=?,updated_at=unixepoch() WHERE id=?`, next, sealed, id); err != nil {
		if accountUniqueError(err) {
			return ErrAccountExists
		}
		return err
	}
	if a.RequestAccountID > 0 {
		if _, err = tx.ExecContext(ctx, `UPDATE request_accounts SET password_hash=? WHERE id=?`, next, a.RequestAccountID); err != nil {
			return err
		}
	}
	if a.MusicServerUserID > 0 {
		if len(sealed) == 0 {
			return ErrPasswordSetupRequired
		}
		if _, err = tx.ExecContext(ctx, `UPDATE music_server_users SET password_sealed=?,password_generated=0,updated_at=unixepoch() WHERE id=?`, sealed, a.MusicServerUserID); err != nil {
			return err
		}
	}
	if err = revokeAccountSessions(ctx, tx, id, a.RequestAccountID); err != nil {
		return err
	}
	return tx.Commit()
}
