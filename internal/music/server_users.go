package music

import (
	"context"
	"crypto/md5"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"modernc.org/sqlite"
)

// musicServerUserPasswordPurpose is the AES-GCM associated-data tag used when
// sealing a per-user Subsonic password. It is deliberately independent of the
// row id and username so renaming an account never invalidates its ciphertext.
const musicServerUserPasswordPurpose = "music-server-user-password"

const (
	musicServerUsernameMax = 64
	musicServerEmailMax    = 254
	musicServerPasswordMax = 256
)

var (
	// ErrMusicServerUsername rejects usernames that cannot be typed or echoed
	// safely by a Subsonic client.
	ErrMusicServerUsername = errors.New("Use a username of 1–64 characters without control characters.")
	// ErrMusicServerEmail validates the optional contact address.
	ErrMusicServerEmail = errors.New("Use an email address of 254 characters or fewer.")
	// ErrMusicServerPassword validates a person-chosen password.
	ErrMusicServerPassword = errors.New("Use a password of 1–256 characters.")
	// ErrMusicServerUserExists reports a duplicate username. Usernames are
	// unique case-insensitively, so "Scarey" and "scarey" collide.
	ErrMusicServerUserExists = errors.New("That username is already in use. Choose another.")
	// ErrMusicServerUserAbsent reports an unknown account id.
	ErrMusicServerUserAbsent = errors.New("That music server user does not exist.")
)

// MusicServerUser is a named Subsonic account. Every field maps to a Subsonic
// user role so a client sees exactly the permissions the administrator granted.
type MusicServerUser struct {
	ID                int64  `json:"id"`
	Username          string `json:"username"`
	Email             string `json:"email"`
	PasswordGenerated bool   `json:"password_generated"`
	ScrobblingEnabled bool   `json:"scrobbling_enabled"`
	AdminRole         bool   `json:"admin_role"`
	SettingsRole      bool   `json:"settings_role"`
	DownloadRole      bool   `json:"download_role"`
	UploadRole        bool   `json:"upload_role"`
	PlaylistRole      bool   `json:"playlist_role"`
	CoverArtRole      bool   `json:"cover_art_role"`
	ShareRole         bool   `json:"share_role"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
	LastUsedAt        int64  `json:"last_used_at"`
}

// MusicServerUserInput carries a create or update request. The role fields are
// optional pointers: on create an omitted role falls back to the household
// defaults, and on update an omitted role keeps its current value, so a partial
// edit can never silently strip a permission.
type MusicServerUserInput struct {
	Username          string
	Email             string
	Password          string
	ScrobblingEnabled *bool
	AdminRole         *bool
	SettingsRole      *bool
	DownloadRole      *bool
	UploadRole        *bool
	PlaylistRole      *bool
	CoverArtRole      *bool
	ShareRole         *bool
}

// Anonymous marks an account that is not a named user: the legacy shared token
// path or an unauthenticated request. The zero id never matches a real row,
// because the table's primary key starts at 1.
func (u MusicServerUser) Anonymous() bool { return u.ID == 0 }

// has reports whether a role is granted. The legacy shared token (zero value)
// and administrators are treated as fully privileged so an existing client that
// has not been moved to a named account keeps working exactly as before.
func (u MusicServerUser) has(role bool) bool {
	return u.Anonymous() || u.AdminRole || role
}

func (u MusicServerUser) CanDownload() bool { return u.has(u.DownloadRole) }
func (u MusicServerUser) CanUpload() bool   { return u.has(u.UploadRole) }
func (u MusicServerUser) CanPlaylist() bool { return u.has(u.PlaylistRole) }
func (u MusicServerUser) CanCoverArt() bool { return u.has(u.CoverArtRole) }
func (u MusicServerUser) CanShare() bool    { return u.has(u.ShareRole) }
func (u MusicServerUser) CanSettings() bool { return u.has(u.SettingsRole) }

const musicServerUserColumns = `id,username,email,password_generated,scrobbling_enabled,admin_role,settings_role,download_role,upload_role,playlist_role,cover_art_role,share_role,created_at,updated_at,last_used_at`

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanMusicServerUser(row rowScanner) (MusicServerUser, error) {
	var out MusicServerUser
	var generated, scrobbling, admin, settings, download, upload, playlist, coverArt, share int
	if err := row.Scan(&out.ID, &out.Username, &out.Email, &generated, &scrobbling, &admin, &settings,
		&download, &upload, &playlist, &coverArt, &share, &out.CreatedAt, &out.UpdatedAt, &out.LastUsedAt); err != nil {
		return MusicServerUser{}, err
	}
	out.PasswordGenerated = generated == 1
	out.ScrobblingEnabled = scrobbling == 1
	out.AdminRole = admin == 1
	out.SettingsRole = settings == 1
	out.DownloadRole = download == 1
	out.UploadRole = upload == 1
	out.PlaylistRole = playlist == 1
	out.CoverArtRole = coverArt == 1
	out.ShareRole = share == 1
	return out, nil
}

func (s *Service) musicServerUser(ctx context.Context, id int64) (MusicServerUser, error) {
	row := s.store.db.QueryRowContext(ctx, `SELECT `+musicServerUserColumns+` FROM music_server_users WHERE id=?`, id)
	user, err := scanMusicServerUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return MusicServerUser{}, ErrMusicServerUserAbsent
	}
	return user, err
}

// musicServerUserByUsername resolves a login name case-insensitively, matching
// the COLLATE NOCASE uniqueness of the column. A missing account is not an
// error: it falls through to the legacy shared-token check.
func (s *Service) musicServerUserByUsername(ctx context.Context, username string) (MusicServerUser, bool, error) {
	row := s.store.db.QueryRowContext(ctx, `SELECT `+musicServerUserColumns+` FROM music_server_users WHERE username=?`, username)
	user, err := scanMusicServerUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return MusicServerUser{}, false, nil
	}
	if err != nil {
		return MusicServerUser{}, false, err
	}
	return user, true, nil
}

func (s *Service) musicServerUsernameTaken(ctx context.Context, username string, excludeID int64) (bool, error) {
	var id int64
	err := s.store.db.QueryRowContext(ctx, `SELECT id FROM music_server_users WHERE username=? AND id<>?`, username, excludeID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ListMusicServerUsers returns every named account, ordered the way clients
// expect to browse them.
func (s *Service) ListMusicServerUsers(ctx context.Context) ([]MusicServerUser, error) {
	rows, err := s.store.db.QueryContext(ctx, `SELECT `+musicServerUserColumns+` FROM music_server_users ORDER BY username COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []MusicServerUser{}
	for rows.Next() {
		user, err := scanMusicServerUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

// CreateMusicServerUser adds a named account. When the caller omits a password
// the server generates a Crockford password and returns it once; a later read
// can never recover it.
func (s *Service) CreateMusicServerUser(ctx context.Context, input MusicServerUserInput) (MusicServerUser, string, error) {
	if s.secrets == nil {
		return MusicServerUser{}, "", &Error{Code: "music_server_unavailable", Message: "Encrypted credential storage is unavailable, so a music server user cannot be created."}
	}
	username, err := validateMusicServerUsername(input.Username)
	if err != nil {
		return MusicServerUser{}, "", err
	}
	email, err := validateMusicServerEmail(input.Email)
	if err != nil {
		return MusicServerUser{}, "", err
	}
	password := input.Password
	generated := false
	if password == "" {
		password, err = newMusicServerToken()
		if err != nil {
			return MusicServerUser{}, "", err
		}
		generated = true
	} else if err := validateMusicServerPassword(password); err != nil {
		return MusicServerUser{}, "", err
	}
	taken, err := s.musicServerUsernameTaken(ctx, username, 0)
	if err != nil {
		return MusicServerUser{}, "", err
	}
	if taken {
		return MusicServerUser{}, "", ErrMusicServerUserExists
	}
	sealed, err := s.secrets.Seal(musicServerUserPasswordPurpose, []byte(password))
	if err != nil {
		return MusicServerUser{}, "", err
	}
	now := time.Now().Unix()
	result, err := s.store.db.ExecContext(ctx, `INSERT INTO music_server_users
		(username,email,password_sealed,password_generated,scrobbling_enabled,admin_role,settings_role,
		 download_role,upload_role,playlist_role,cover_art_role,share_role,created_at,updated_at,last_used_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,0)`,
		username, email, sealed, boolInt(generated),
		boolInt(roleDefault(input.ScrobblingEnabled, true)),
		boolInt(roleDefault(input.AdminRole, false)),
		boolInt(roleDefault(input.SettingsRole, false)),
		boolInt(roleDefault(input.DownloadRole, true)),
		boolInt(roleDefault(input.UploadRole, false)),
		boolInt(roleDefault(input.PlaylistRole, true)),
		boolInt(roleDefault(input.CoverArtRole, true)),
		boolInt(roleDefault(input.ShareRole, false)),
		now, now)
	if err != nil {
		// A concurrent creation can win the COLLATE NOCASE UNIQUE(username)
		// race after the taken check; report the typed duplicate error.
		if isMusicUsernameUniqueConflict(err) {
			return MusicServerUser{}, "", ErrMusicServerUserExists
		}
		return MusicServerUser{}, "", err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return MusicServerUser{}, "", err
	}
	user, err := s.musicServerUser(ctx, id)
	if err != nil {
		return MusicServerUser{}, "", err
	}
	if generated {
		return user, password, nil
	}
	return user, "", nil
}

// isMusicUsernameUniqueConflict reports whether err is the driver's UNIQUE
// constraint failure on music_server_users.username. The taken check usually
// catches duplicates, but a concurrent insert can win the race; callers map
// this to ErrMusicServerUserExists instead of leaking a raw driver error.
func isMusicUsernameUniqueConflict(err error) bool {
	if err == nil {
		return false
	}
	const column = "UNIQUE constraint failed: music_server_users.username"
	var serr *sqlite.Error
	if errors.As(err, &serr) && strings.Contains(serr.Error(), column) {
		return true
	}
	return strings.Contains(err.Error(), column)
}

// UpdateMusicServerUser edits an existing account. Omitted role fields keep
// their stored value, and a supplied password replaces the stored one (and
// flips password_generated off, because a person chose it).
func (s *Service) UpdateMusicServerUser(ctx context.Context, id int64, input MusicServerUserInput) (MusicServerUser, error) {
	if id < 1 {
		return MusicServerUser{}, ErrInvalidInput
	}
	if s.secrets == nil {
		return MusicServerUser{}, &Error{Code: "music_server_unavailable", Message: "Encrypted credential storage is unavailable, so a music server user cannot be saved."}
	}
	current, err := s.musicServerUser(ctx, id)
	if err != nil {
		return MusicServerUser{}, err
	}
	username, err := validateMusicServerUsername(input.Username)
	if err != nil {
		return MusicServerUser{}, err
	}
	email, err := validateMusicServerEmail(input.Email)
	if err != nil {
		return MusicServerUser{}, err
	}
	taken, err := s.musicServerUsernameTaken(ctx, username, id)
	if err != nil {
		return MusicServerUser{}, err
	}
	if taken {
		return MusicServerUser{}, ErrMusicServerUserExists
	}
	var sealed []byte
	if err := s.store.db.QueryRowContext(ctx, `SELECT password_sealed FROM music_server_users WHERE id=?`, id).Scan(&sealed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MusicServerUser{}, ErrMusicServerUserAbsent
		}
		return MusicServerUser{}, err
	}
	generated := current.PasswordGenerated
	if input.Password != "" {
		if err := validateMusicServerPassword(input.Password); err != nil {
			return MusicServerUser{}, err
		}
		sealed, err = s.secrets.Seal(musicServerUserPasswordPurpose, []byte(input.Password))
		if err != nil {
			return MusicServerUser{}, err
		}
		generated = false
	}
	_, err = s.store.db.ExecContext(ctx, `UPDATE music_server_users SET
		username=?,email=?,password_sealed=?,password_generated=?,scrobbling_enabled=?,admin_role=?,settings_role=?,
		download_role=?,upload_role=?,playlist_role=?,cover_art_role=?,share_role=?,updated_at=unixepoch()
		WHERE id=?`,
		username, email, sealed, boolInt(generated),
		boolInt(roleKeep(input.ScrobblingEnabled, current.ScrobblingEnabled)),
		boolInt(roleKeep(input.AdminRole, current.AdminRole)),
		boolInt(roleKeep(input.SettingsRole, current.SettingsRole)),
		boolInt(roleKeep(input.DownloadRole, current.DownloadRole)),
		boolInt(roleKeep(input.UploadRole, current.UploadRole)),
		boolInt(roleKeep(input.PlaylistRole, current.PlaylistRole)),
		boolInt(roleKeep(input.CoverArtRole, current.CoverArtRole)),
		boolInt(roleKeep(input.ShareRole, current.ShareRole)),
		id)
	if err != nil {
		return MusicServerUser{}, err
	}
	return s.musicServerUser(ctx, id)
}

// ResetMusicServerUserPassword issues a fresh generated password and returns it
// once. Rotating is the safe response to a leaked credential.
func (s *Service) ResetMusicServerUserPassword(ctx context.Context, id int64) (MusicServerUser, string, error) {
	if id < 1 {
		return MusicServerUser{}, "", ErrInvalidInput
	}
	if s.secrets == nil {
		return MusicServerUser{}, "", &Error{Code: "music_server_unavailable", Message: "Encrypted credential storage is unavailable, so a music server password cannot be created."}
	}
	password, err := newMusicServerToken()
	if err != nil {
		return MusicServerUser{}, "", err
	}
	sealed, err := s.secrets.Seal(musicServerUserPasswordPurpose, []byte(password))
	if err != nil {
		return MusicServerUser{}, "", err
	}
	result, err := s.store.db.ExecContext(ctx, `UPDATE music_server_users SET password_sealed=?,password_generated=1,updated_at=unixepoch() WHERE id=?`, sealed, id)
	if err != nil {
		return MusicServerUser{}, "", err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return MusicServerUser{}, "", err
	}
	if changed != 1 {
		return MusicServerUser{}, "", ErrMusicServerUserAbsent
	}
	user, err := s.musicServerUser(ctx, id)
	if err != nil {
		return MusicServerUser{}, "", err
	}
	return user, password, nil
}

// DeleteMusicServerUser removes an account. Any client still configured with it
// is rejected on its next request.
func (s *Service) DeleteMusicServerUser(ctx context.Context, id int64) error {
	if id < 1 {
		return ErrInvalidInput
	}
	result, err := s.store.db.ExecContext(ctx, `DELETE FROM music_server_users WHERE id=?`, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrMusicServerUserAbsent
	}
	return nil
}

// AuthenticateMusicServer resolves a Subsonic request to a named account. ok
// reports whether the credentials are valid; a zero-id user with ok true means
// the legacy shared token authenticated (an unnamed account with full rights).
// Both the plaintext p= form and the salted md5 t=/s= form are accepted.
func (s *Service) AuthenticateMusicServer(ctx context.Context, username, password, token, salt string) (MusicServerUser, bool, error) {
	username = strings.TrimSpace(username)
	if username == "" || len(username) > 200 || s.secrets == nil {
		return MusicServerUser{}, false, nil
	}
	status, err := s.MusicServer(ctx)
	if err != nil {
		return MusicServerUser{}, false, err
	}
	if !status.Enabled {
		return MusicServerUser{}, false, nil
	}
	user, found, err := s.musicServerUserByUsername(ctx, username)
	if err != nil {
		return MusicServerUser{}, false, err
	}
	if found {
		var sealed []byte
		if err := s.store.db.QueryRowContext(ctx, `SELECT password_sealed FROM music_server_users WHERE id=?`, user.ID).Scan(&sealed); err != nil {
			return MusicServerUser{}, false, err
		}
		expected, err := s.secrets.Open(musicServerUserPasswordPurpose, sealed)
		if err != nil {
			return MusicServerUser{}, false, err
		}
		if verifyMusicServerSecret(string(expected), user.PasswordGenerated, password, token, salt) {
			s.touchMusicServerUser(ctx, user)
			return user, true, nil
		}
		return MusicServerUser{}, false, nil
	}
	// No named account matched. Fall back to the shared token so a client that
	// was configured before named accounts existed keeps working until the
	// administrator removes it.
	if !status.Configured {
		return MusicServerUser{}, false, nil
	}
	secret, err := s.secrets.GetNamed(ctx, musicServerTokenName)
	if err != nil || len(secret) == 0 {
		return MusicServerUser{}, false, err
	}
	if verifyMusicServerSecret(string(secret), true, password, token, salt) {
		return MusicServerUser{}, true, nil
	}
	return MusicServerUser{}, false, nil
}

// touchMusicServerUser records activity without writing on every request; a
// sync-heavy client would otherwise hammer the row.
func (s *Service) touchMusicServerUser(ctx context.Context, user MusicServerUser) {
	now := time.Now().Unix()
	if now-user.LastUsedAt < 300 {
		return
	}
	_, _ = s.store.db.ExecContext(ctx, `UPDATE music_server_users SET last_used_at=? WHERE id=?`, now, user.ID)
}

// verifyMusicServerSecret compares a supplied Subsonic credential against a
// known plaintext password. forgiving enables the case- and separator-
// insensitive comparison used for Crockford passwords the server generated, so
// a password retyped from a phone screen still matches.
func verifyMusicServerSecret(expected string, forgiving bool, password, token, salt string) bool {
	if password != "" {
		value := password
		if strings.HasPrefix(strings.ToLower(value), "enc:") {
			decoded, err := hex.DecodeString(value[4:])
			if err != nil {
				return false
			}
			value = string(decoded)
		}
		if subtle.ConstantTimeCompare([]byte(value), []byte(expected)) == 1 {
			return true
		}
		if !forgiving {
			return false
		}
		return subtle.ConstantTimeCompare([]byte(normalizeMusicServerToken(value)), []byte(normalizeMusicServerToken(expected))) == 1
	}
	if token == "" || salt == "" {
		return false
	}
	sum := md5.Sum([]byte(expected + salt))
	return subtle.ConstantTimeCompare([]byte(strings.ToLower(token)), []byte(hex.EncodeToString(sum[:]))) == 1
}

func validateMusicServerUsername(raw string) (string, error) {
	username := strings.TrimSpace(raw)
	if username == "" || len([]rune(username)) > musicServerUsernameMax {
		return "", ErrMusicServerUsername
	}
	for _, r := range username {
		if r < 0x20 || r == 0x7f {
			return "", ErrMusicServerUsername
		}
	}
	return username, nil
}

func validateMusicServerEmail(raw string) (string, error) {
	email := strings.TrimSpace(raw)
	if len([]rune(email)) > musicServerEmailMax {
		return "", ErrMusicServerEmail
	}
	for _, r := range email {
		if r < 0x20 || r == 0x7f {
			return "", ErrMusicServerEmail
		}
	}
	return email, nil
}

func validateMusicServerPassword(password string) error {
	if len(password) == 0 || len(password) > musicServerPasswordMax {
		return ErrMusicServerPassword
	}
	return nil
}

func roleDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func roleKeep(value *bool, current bool) bool {
	if value == nil {
		return current
	}
	return *value
}
