package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/securestore"
)

// TOTP follows RFC 6238 with the RFC 4226 defaults: HMAC-SHA-1, six digits and
// a thirty-second period. A single step of clock skew is accepted in each
// direction.
const (
	totpPurpose       = "config:admin-totp"
	totpIssuer        = "ReelScarey"
	totpSecretBytes   = 20
	totpDigits        = 6
	totpPeriodSeconds = 30
	totpSkew          = 1
	totpCodeMaxLength = 32

	recoveryCodeCount   = 10
	recoveryCodeBytes   = 10
	recoveryCodeLength  = 16 // base32 without padding
	recoveryInputLength = 64
)

var (
	ErrTOTPCode           = errors.New("The authenticator code is incorrect.")
	ErrTOTPNotEnabled     = errors.New("Two-factor authentication is not enabled.")
	ErrTOTPEnabled        = errors.New("Two-factor authentication is already enabled.")
	ErrTOTPNotProvisioned = errors.New("Start two-factor setup before confirming a code.")
	ErrTOTPRecoveryCode   = errors.New("The recovery code is incorrect or has already been used.")
	ErrTOTPInput          = errors.New("Use a valid administrator account label for two-factor setup.")
)

// TOTPStatus reports whether a pending secret exists and whether TOTP is
// enforced. RecoveryCodesRemaining is zero while setup is pending.
type TOTPStatus struct {
	Configured             bool `json:"configured"`
	Enabled                bool `json:"enabled"`
	RecoveryCodesRemaining int  `json:"recovery_codes_remaining"`
}

// TOTPProvision is returned once by Provision. The secret must be shown to the
// administrator and never persisted in plaintext by callers.
type TOTPProvision struct {
	Secret string `json:"secret"`
	URI    string `json:"otpauth_uri"`
}

// TOTPStore owns the single admin_totp row. Secrets are encrypted through
// securestore and recovery codes are stored only as SHA-256 hashes.
type TOTPStore struct {
	db    *sql.DB
	store *securestore.Store
	now   func() time.Time
}

func NewTOTPStore(db *sql.DB, store *securestore.Store) (*TOTPStore, error) {
	if db == nil || store == nil {
		return nil, errors.New("two-factor storage requires a database and secure store")
	}
	return &TOTPStore{db: db, store: store, now: time.Now}, nil
}

// Status reports the configured and enabled flags without decrypting the
// secret. A missing row means two-factor authentication has never been set up.
func (t *TOTPStore) Status(ctx context.Context) (TOTPStatus, error) {
	var enabled int
	var hashes string
	err := t.db.QueryRowContext(ctx, `SELECT enabled,recovery_hashes FROM admin_totp WHERE id=1`).Scan(&enabled, &hashes)
	if errors.Is(err, sql.ErrNoRows) {
		return TOTPStatus{}, nil
	}
	if err != nil {
		return TOTPStatus{}, err
	}
	return TOTPStatus{
		Configured:             true,
		Enabled:                enabled == 1,
		RecoveryCodesRemaining: len(splitRecoveryHashes(hashes)),
	}, nil
}

// Provision generates a fresh secret and stores it encrypted with the row left
// disabled until Confirm proves the administrator can produce a valid code.
// Calling Provision again replaces any unconfirmed pending secret.
func (t *TOTPStore) Provision(ctx context.Context, account string) (TOTPProvision, error) {
	account = strings.TrimSpace(account)
	if account == "" || len(account) > 128 || !usernamePattern.MatchString(account) {
		return TOTPProvision{}, ErrTOTPInput
	}
	var enabled int
	err := t.db.QueryRowContext(ctx, `SELECT enabled FROM admin_totp WHERE id=1`).Scan(&enabled)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return TOTPProvision{}, err
	}
	if err == nil && enabled == 1 {
		return TOTPProvision{}, ErrTOTPEnabled
	}
	secret, err := generateTOTPSecret()
	if err != nil {
		return TOTPProvision{}, err
	}
	sealed, err := t.store.Seal(totpPurpose, []byte(secret))
	if err != nil {
		return TOTPProvision{}, err
	}
	now := t.now().Unix()
	_, err = t.db.ExecContext(ctx, `INSERT INTO admin_totp(id,secret_ciphertext,recovery_hashes,enabled,created_at,updated_at,totp_last_step)
		VALUES(1,?,'',0,?,?,0)
		ON CONFLICT(id) DO UPDATE SET secret_ciphertext=excluded.secret_ciphertext,
			recovery_hashes='',enabled=0,updated_at=excluded.updated_at,totp_last_step=0`, sealed, now, now)
	if err != nil {
		return TOTPProvision{}, err
	}
	return TOTPProvision{Secret: secret, URI: buildTOTPURI(totpIssuer, account, secret)}, nil
}

// Confirm verifies a code against the pending secret, enables TOTP and returns
// freshly generated recovery codes. The codes are returned in plaintext only
// here; the database keeps only their SHA-256 hashes.
func (t *TOTPStore) Confirm(ctx context.Context, code string) ([]string, error) {
	normalized, ok := normalizeTOTPCode(code)
	if !ok {
		return nil, ErrTOTPCode
	}
	var sealed []byte
	var enabled int
	err := t.db.QueryRowContext(ctx, `SELECT secret_ciphertext,enabled FROM admin_totp WHERE id=1`).Scan(&sealed, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTOTPNotProvisioned
	}
	if err != nil {
		return nil, err
	}
	if enabled == 1 {
		return nil, ErrTOTPEnabled
	}
	secret, err := t.store.Open(totpPurpose, sealed)
	if err != nil {
		return nil, err
	}
	if _, ok := validateTOTPStep(string(secret), normalized, t.now()); !ok {
		return nil, ErrTOTPCode
	}
	codes, hashes, err := generateRecoveryCodes()
	if err != nil {
		return nil, err
	}
	tx, err := t.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Fence concurrent provisioning or confirmation: only the exact pending
	// ciphertext we verified may be enabled. The confirmed step is not
	// consumed here — Verify fences replay once TOTP is enabled.
	result, err := tx.ExecContext(ctx, `UPDATE admin_totp SET recovery_hashes=?,enabled=1,updated_at=?
		WHERE id=1 AND enabled=0 AND secret_ciphertext=?`, strings.Join(hashes, "\n"), t.now().Unix(), sealed)
	if err != nil {
		return nil, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, ErrTOTPCode
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return codes, nil
}

// Disable removes the administrator's TOTP secret and recovery codes. Callers
// should revoke existing sessions after disabling.
func (t *TOTPStore) Disable(ctx context.Context) error {
	_, err := t.db.ExecContext(ctx, `DELETE FROM admin_totp WHERE id=1`)
	return err
}

// Verify checks a current authenticator code. It never validates a pending
// secret: TOTP must be enabled with Confirm first.
func (t *TOTPStore) Verify(ctx context.Context, code string) error {
	normalized, ok := normalizeTOTPCode(code)
	if !ok {
		return ErrTOTPCode
	}
	var sealed []byte
	var enabled int
	var lastStep int64
	err := t.db.QueryRowContext(ctx, `SELECT secret_ciphertext,enabled,totp_last_step FROM admin_totp WHERE id=1`).Scan(&sealed, &enabled, &lastStep)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && enabled != 1) {
		return ErrTOTPNotEnabled
	}
	if err != nil {
		return err
	}
	secret, err := t.store.Open(totpPurpose, sealed)
	if err != nil {
		return err
	}
	step, ok := validateTOTPStep(string(secret), normalized, t.now())
	if !ok || step <= lastStep {
		return ErrTOTPCode
	}
	// Persist the consumed step with the same compare-gated fencing the
	// recovery-code consumption uses, so a concurrent replay of the same code
	// cannot slip past the read above.
	result, err := t.db.ExecContext(ctx, `UPDATE admin_totp SET totp_last_step=?,updated_at=?
		WHERE id=1 AND enabled=1 AND secret_ciphertext=? AND totp_last_step<?`, step, t.now().Unix(), sealed, step)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrTOTPCode
	}
	return nil
}

// VerifyRecoveryCode consumes a single recovery code. A code that matches is
// removed atomically, so a replay fails even across concurrent attempts.
func (t *TOTPStore) VerifyRecoveryCode(ctx context.Context, code string) error {
	normalized, ok := normalizeRecoveryCode(code)
	if !ok {
		return ErrTOTPRecoveryCode
	}
	target := recoveryHash(normalized)
	for attempt := 0; attempt < 3; attempt++ {
		var text string
		var enabled int
		err := t.db.QueryRowContext(ctx, `SELECT recovery_hashes,enabled FROM admin_totp WHERE id=1`).Scan(&text, &enabled)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && enabled != 1) {
			return ErrTOTPNotEnabled
		}
		if err != nil {
			return err
		}
		hashes := splitRecoveryHashes(text)
		if !matchesAnyHash(hashes, target) {
			return ErrTOTPRecoveryCode
		}
		remaining := removeRecoveryHash(hashes, target)
		result, err := t.db.ExecContext(ctx, `UPDATE admin_totp SET recovery_hashes=?,updated_at=?
			WHERE id=1 AND enabled=1 AND recovery_hashes=?`, strings.Join(remaining, "\n"), t.now().Unix(), text)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count == 1 {
			return nil
		}
		// The row changed between read and write (another code was consumed).
		// Re-read once more; our code is still valid unless it was the change.
	}
	return ErrTOTPRecoveryCode
}

func generateTOTPSecret() (string, error) {
	raw := make([]byte, totpSecretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

func decodeTOTPSecret(secret string) ([]byte, error) {
	trimmed := strings.ToUpper(strings.TrimSpace(secret))
	trimmed = strings.ReplaceAll(trimmed, " ", "")
	trimmed = strings.TrimRight(trimmed, "=")
	if len(trimmed) == 0 || len(trimmed) > 128 {
		return nil, ErrTOTPInput
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(trimmed)
	if err != nil || len(key) == 0 {
		return nil, ErrTOTPInput
	}
	return key, nil
}

// totpCode derives the six-digit code for the exact time step containing at.
func totpCode(secret string, at time.Time) (string, error) {
	key, err := decodeTOTPSecret(secret)
	if err != nil {
		return "", err
	}
	if at.Unix() < 0 {
		return "", ErrTOTPInput
	}
	return hotpCode(key, uint64(at.Unix()/totpPeriodSeconds)), nil
}

// validateTOTP accepts the current step plus one step of clock skew in each
// direction, comparing every candidate in constant time.
func validateTOTP(secret, code string, at time.Time) bool {
	_, ok := validateTOTPStep(secret, code, at)
	return ok
}

// validateTOTPStep returns the exact time step that matched, so callers can
// reject a replay of an already-consumed code. The skew window is unchanged:
// the current step plus one step in each direction.
func validateTOTPStep(secret, code string, at time.Time) (int64, bool) {
	normalized, ok := normalizeTOTPCode(code)
	if !ok {
		return 0, false
	}
	key, err := decodeTOTPSecret(secret)
	if err != nil || at.Unix() < 0 {
		return 0, false
	}
	step := at.Unix() / totpPeriodSeconds
	matched := int64(-1)
	for offset := int64(-totpSkew); offset <= totpSkew; offset++ {
		candidate := step + offset
		if candidate < 0 {
			continue
		}
		expected := hotpCode(key, uint64(candidate))
		if subtle.ConstantTimeCompare([]byte(expected), []byte(normalized)) == 1 {
			matched = candidate
		}
	}
	return matched, matched >= 0
}

// hotpCode implements RFC 4226 dynamic truncation.
func hotpCode(key []byte, counter uint64) string {
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], counter)
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(message[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])
	return fmt.Sprintf("%06d", value%1_000_000)
}

func buildTOTPURI(issuer, account, secret string) string {
	u := url.URL{
		Scheme: "otpauth",
		Host:   "totp",
		Path:   "/" + issuer + ":" + account,
	}
	query := u.Query()
	query.Set("secret", secret)
	query.Set("issuer", issuer)
	query.Set("algorithm", "SHA1")
	query.Set("digits", "6")
	query.Set("period", "30")
	u.RawQuery = query.Encode()
	return u.String()
}

func normalizeTOTPCode(code string) (string, bool) {
	if len(code) == 0 || len(code) > totpCodeMaxLength {
		return "", false
	}
	digits := make([]byte, 0, totpDigits)
	for i := 0; i < len(code); i++ {
		switch c := code[i]; c {
		case ' ', '-', '\t':
			continue
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			digits = append(digits, c)
		default:
			return "", false
		}
	}
	if len(digits) != totpDigits {
		return "", false
	}
	return string(digits), true
}

func generateRecoveryCodes() ([]string, []string, error) {
	encoding := base32.StdEncoding.WithPadding(base32.NoPadding)
	codes := make([]string, 0, recoveryCodeCount)
	hashes := make([]string, 0, recoveryCodeCount)
	for i := 0; i < recoveryCodeCount; i++ {
		raw := make([]byte, recoveryCodeBytes)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, err
		}
		code := encoding.EncodeToString(raw)
		codes = append(codes, code[0:4]+"-"+code[4:8]+"-"+code[8:12]+"-"+code[12:16])
		hashes = append(hashes, recoveryHash(code))
	}
	return codes, hashes, nil
}

func normalizeRecoveryCode(code string) (string, bool) {
	if len(code) == 0 || len(code) > recoveryInputLength {
		return "", false
	}
	normalized := make([]byte, 0, recoveryCodeLength)
	for i := 0; i < len(code); i++ {
		c := code[i]
		switch {
		case c == ' ' || c == '-' || c == '\t' || c == '\r' || c == '\n':
			continue
		case c >= 'a' && c <= 'z':
			c -= 'a' - 'A'
		}
		if !(c >= 'A' && c <= 'Z') && !(c >= '2' && c <= '7') {
			return "", false
		}
		normalized = append(normalized, c)
	}
	if len(normalized) != recoveryCodeLength {
		return "", false
	}
	return string(normalized), true
}

func recoveryHash(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

func splitRecoveryHashes(text string) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ' ' || r == '\t' || r == ','
	})
	hashes := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) == sha256.Size*2 {
			hashes = append(hashes, field)
		}
	}
	return hashes
}

func matchesAnyHash(hashes []string, target string) bool {
	matched := 0
	for _, hash := range hashes {
		if len(hash) != len(target) {
			continue
		}
		matched |= subtle.ConstantTimeCompare([]byte(hash), []byte(target))
	}
	return matched == 1
}

func removeRecoveryHash(hashes []string, target string) []string {
	remaining := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		if hash != target {
			remaining = append(remaining, hash)
		}
	}
	return remaining
}
