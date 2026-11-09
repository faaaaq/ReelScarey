package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

func totpFixture(t *testing.T) (*TOTPStore, *securestore.Store, string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := securestore.New(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	totp, err := NewTOTPStore(db, store)
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Unix(1_700_000_000, 0).UTC()
	totp.now = func() time.Time { return fixed }
	return totp, store, dir
}

func TestTOTPLoginRequiresCodeAndAcceptsRecoveryCodeOnce(t *testing.T) {
	ctx := context.Background()
	service, db, dir, setupCode := authFixture(t)
	if _, err := service.Setup(ctx, "admin", "test-only-password", setupCode); err != nil {
		t.Fatal(err)
	}
	store, err := securestore.New(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	totp, err := NewTOTPStore(db, store)
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Unix(1_700_000_000, 0).UTC()
	totp.now = func() time.Time { return fixed }
	service.SetTOTP(totp)

	provision, err := totp.Provision(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := totp.Confirm(ctx, mustCode(t, provision.Secret, fixed))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Login(ctx, "admin", "test-only-password"); !errors.Is(err, ErrTOTPCode) {
		t.Fatalf("password-only login with TOTP enabled: %v", err)
	}
	wrong := string(rune('0'+(mustCode(t, provision.Secret, fixed)[0]-'0'+1)%10)) + mustCode(t, provision.Secret, fixed)[1:]
	if _, err := service.LoginWithTOTP(ctx, "admin", "test-only-password", wrong); !errors.Is(err, ErrTOTPCode) {
		t.Fatalf("wrong TOTP accepted: %v", err)
	}
	session, err := service.LoginWithTOTP(ctx, "admin", "test-only-password", mustCode(t, provision.Secret, fixed))
	if err != nil || session.Role != "admin" {
		t.Fatalf("valid TOTP rejected: %v", err)
	}
	if len(recovery) == 0 {
		t.Fatal("no recovery codes returned")
	}
	if _, err := service.LoginWithTOTP(ctx, "admin", "test-only-password", recovery[0]); err != nil {
		t.Fatalf("recovery code rejected: %v", err)
	}
	if _, err := service.LoginWithTOTP(ctx, "admin", "test-only-password", recovery[0]); !errors.Is(err, ErrTOTPCode) {
		t.Fatalf("recovery code reused: %v", err)
	}
}

func TestTOTPRFC6238Vectors(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	for _, c := range []struct {
		unix int64
		code string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	} {
		got, err := totpCode(secret, time.Unix(c.unix, 0))
		if err != nil {
			t.Fatalf("time %d: %v", c.unix, err)
		}
		if got != c.code {
			t.Fatalf("time %d: got %s want %s", c.unix, got, c.code)
		}
	}
}

func TestTOTPWindowAndWrongCode(t *testing.T) {
	secret, err := generateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1_700_000_000, 0)
	current, err := totpCode(secret, at)
	if err != nil {
		t.Fatal(err)
	}
	previous, _ := totpCode(secret, at.Add(-30*time.Second))
	next, _ := totpCode(secret, at.Add(30*time.Second))
	tooOld, _ := totpCode(secret, at.Add(-60*time.Second))
	tooNew, _ := totpCode(secret, at.Add(60*time.Second))
	if !validateTOTP(secret, current, at) {
		t.Fatal("current code rejected")
	}
	if !validateTOTP(secret, previous, at) {
		t.Fatal("previous step rejected")
	}
	if !validateTOTP(secret, next, at) {
		t.Fatal("next step rejected")
	}
	if validateTOTP(secret, tooOld, at) {
		t.Fatal("code outside the window accepted")
	}
	if validateTOTP(secret, tooNew, at) {
		t.Fatal("code outside the window accepted")
	}
	wrong := string(rune('0'+(current[0]-'0'+1)%10)) + current[1:]
	if wrong == current {
		t.Fatal("test produced an unchanged code")
	}
	if validateTOTP(secret, wrong, at) {
		t.Fatal("wrong code accepted")
	}
	for _, invalid := range []string{"", "abcdef", "12345", "1234567", strings.Repeat("1", 33)} {
		if validateTOTP(secret, invalid, at) {
			t.Fatalf("invalid input %q accepted", invalid)
		}
	}
}

func TestTOTPProvisionConfirmDisable(t *testing.T) {
	ctx := context.Background()
	totp, store, _ := totpFixture(t)
	now := totp.now()

	status, err := totp.Status(ctx)
	if err != nil || status.Configured || status.Enabled || status.RecoveryCodesRemaining != 0 {
		t.Fatalf("initial status: %+v %v", status, err)
	}
	if err := totp.Verify(ctx, "123456"); !errors.Is(err, ErrTOTPNotEnabled) {
		t.Fatalf("verify without setup: %v", err)
	}
	if err := totp.VerifyRecoveryCode(ctx, "AAAA-BBBB-CCCC-DDDD"); !errors.Is(err, ErrTOTPNotEnabled) {
		t.Fatalf("recovery without setup: %v", err)
	}
	if _, err := totp.Confirm(ctx, "123456"); !errors.Is(err, ErrTOTPNotProvisioned) {
		t.Fatalf("confirm without provisioning: %v", err)
	}

	provision, err := totp.Provision(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	rawSecret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(provision.Secret)
	if err != nil || len(rawSecret) != totpSecretBytes {
		t.Fatalf("secret is not 20 base32 bytes: %q %v", provision.Secret, err)
	}
	if !strings.HasPrefix(provision.URI, "otpauth://totp/ReelScarey:admin?") {
		t.Fatalf("unexpected otpauth URI: %s", provision.URI)
	}
	for _, fragment := range []string{"issuer=ReelScarey", "algorithm=SHA1", "digits=6", "period=30", "secret=" + provision.Secret} {
		if !strings.Contains(provision.URI, fragment) {
			t.Fatalf("otpauth URI missing %q: %s", fragment, provision.URI)
		}
	}
	status, err = totp.Status(ctx)
	if err != nil || !status.Configured || status.Enabled {
		t.Fatalf("pending status: %+v %v", status, err)
	}
	// A pending secret must not authorize verification or login.
	if err := totp.Verify(ctx, mustCode(t, provision.Secret, now)); !errors.Is(err, ErrTOTPNotEnabled) {
		t.Fatalf("pending secret verified: %v", err)
	}

	code := mustCode(t, provision.Secret, now)
	wrong := string(rune('0'+(code[0]-'0'+1)%10)) + code[1:]
	if _, err := totp.Confirm(ctx, wrong); !errors.Is(err, ErrTOTPCode) {
		t.Fatalf("wrong code confirmed: %v", err)
	}
	status, _ = totp.Status(ctx)
	if status.Enabled {
		t.Fatal("wrong code enabled two-factor authentication")
	}

	var sealed []byte
	if err := totp.db.QueryRowContext(ctx, `SELECT secret_ciphertext FROM admin_totp WHERE id=1`).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if plain, err := store.Open(totpPurpose, sealed); err != nil || string(plain) != provision.Secret {
		t.Fatalf("stored secret does not decrypt: %v", err)
	}
	if string(sealed) == provision.Secret || strings.Contains(string(sealed), provision.Secret) {
		t.Fatal("plaintext secret stored in the database")
	}
	var visible string
	if err := totp.db.QueryRowContext(ctx, `SELECT CAST(secret_ciphertext AS TEXT) FROM admin_totp WHERE id=1`).Scan(&visible); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(visible, provision.Secret) {
		t.Fatal("secret is readable as text in the database")
	}

	recovery, err := totp.Confirm(ctx, code[:3]+" "+code[3:])
	if err != nil {
		t.Fatal(err)
	}
	if len(recovery) != recoveryCodeCount {
		t.Fatalf("got %d recovery codes", len(recovery))
	}
	status, err = totp.Status(ctx)
	if err != nil || !status.Configured || !status.Enabled || status.RecoveryCodesRemaining != recoveryCodeCount {
		t.Fatalf("enabled status: %+v %v", status, err)
	}
	if _, err := totp.Provision(ctx, "admin"); !errors.Is(err, ErrTOTPEnabled) {
		t.Fatalf("re-provision while enabled: %v", err)
	}

	var storedHashes string
	if err := totp.db.QueryRowContext(ctx, `SELECT recovery_hashes FROM admin_totp WHERE id=1`).Scan(&storedHashes); err != nil {
		t.Fatal(err)
	}
	for _, recoveryCode := range recovery {
		normalized, ok := normalizeRecoveryCode(recoveryCode)
		if !ok {
			t.Fatalf("generated recovery code is invalid: %q", recoveryCode)
		}
		if strings.Contains(storedHashes, normalized) || strings.Contains(storedHashes, strings.ToLower(normalized)) {
			t.Fatal("plaintext recovery code stored in the database")
		}
		sum := sha256.Sum256([]byte(normalized))
		if !strings.Contains(storedHashes, hex.EncodeToString(sum[:])) {
			t.Fatal("recovery code hash missing from the database")
		}
	}

	for _, delta := range []time.Duration{-30 * time.Second, 0, 30 * time.Second} {
		if err := totp.Verify(ctx, mustCode(t, provision.Secret, now.Add(delta))); err != nil {
			t.Fatalf("code at %v rejected: %v", delta, err)
		}
	}
	for _, delta := range []time.Duration{-60 * time.Second, 60 * time.Second} {
		if err := totp.Verify(ctx, mustCode(t, provision.Secret, now.Add(delta))); !errors.Is(err, ErrTOTPCode) {
			t.Fatalf("code at %v accepted: %v", delta, err)
		}
	}
	if err := totp.Verify(ctx, wrong); !errors.Is(err, ErrTOTPCode) {
		t.Fatalf("wrong code accepted: %v", err)
	}
	if err := totp.Verify(ctx, strings.Repeat("1", 33)); !errors.Is(err, ErrTOTPCode) {
		t.Fatalf("oversized code accepted: %v", err)
	}
	if err := totp.VerifyRecoveryCode(ctx, strings.Repeat("A", recoveryInputLength+1)); !errors.Is(err, ErrTOTPRecoveryCode) {
		t.Fatalf("oversized recovery code accepted: %v", err)
	}

	if err := totp.VerifyRecoveryCode(ctx, recovery[0]); err != nil {
		t.Fatal(err)
	}
	if err := totp.VerifyRecoveryCode(ctx, recovery[0]); !errors.Is(err, ErrTOTPRecoveryCode) {
		t.Fatalf("recovery code replay accepted: %v", err)
	}
	lowered := strings.ToLower(strings.ReplaceAll(recovery[1], "-", " "))
	if err := totp.VerifyRecoveryCode(ctx, lowered); err != nil {
		t.Fatalf("second recovery code rejected: %v", err)
	}
	if err := totp.VerifyRecoveryCode(ctx, recovery[1]); !errors.Is(err, ErrTOTPRecoveryCode) {
		t.Fatalf("recovery code replay accepted: %v", err)
	}
	status, err = totp.Status(ctx)
	if err != nil || status.RecoveryCodesRemaining != recoveryCodeCount-2 {
		t.Fatalf("recovery remaining after use: %+v %v", status, err)
	}

	if err := totp.Disable(ctx); err != nil {
		t.Fatal(err)
	}
	if err := totp.Disable(ctx); err != nil {
		t.Fatalf("disable is not idempotent: %v", err)
	}
	status, err = totp.Status(ctx)
	if err != nil || status.Configured || status.Enabled || status.RecoveryCodesRemaining != 0 {
		t.Fatalf("status after disable: %+v %v", status, err)
	}
	if err := totp.Verify(ctx, code); !errors.Is(err, ErrTOTPNotEnabled) {
		t.Fatalf("verify after disable: %v", err)
	}
	if err := totp.VerifyRecoveryCode(ctx, recovery[2]); !errors.Is(err, ErrTOTPNotEnabled) {
		t.Fatalf("recovery after disable: %v", err)
	}

	again, err := totp.Provision(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if again.Secret == provision.Secret {
		t.Fatal("provisioning after disable reused the old secret")
	}
}

func TestTOTPProvisionReplacesPendingSecret(t *testing.T) {
	ctx := context.Background()
	totp, _, _ := totpFixture(t)
	first, err := totp.Provision(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	second, err := totp.Provision(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if first.Secret == second.Secret {
		t.Fatal("pending provisioning did not rotate the secret")
	}
	if _, err := totp.Confirm(ctx, mustCode(t, first.Secret, totp.now())); !errors.Is(err, ErrTOTPCode) {
		t.Fatalf("stale pending secret confirmed: %v", err)
	}
	if _, err := totp.Confirm(ctx, mustCode(t, second.Secret, totp.now())); err != nil {
		t.Fatalf("current pending secret rejected: %v", err)
	}
}

func TestTOTPProvisionInputBounds(t *testing.T) {
	ctx := context.Background()
	totp, _, _ := totpFixture(t)
	for _, account := range []string{"", "bad label", strings.Repeat("a", 129)} {
		if _, err := totp.Provision(ctx, account); !errors.Is(err, ErrTOTPInput) {
			t.Fatalf("invalid account %q accepted: %v", account, err)
		}
	}
	if _, err := NewTOTPStore(nil, nil); err == nil {
		t.Fatal("store accepted nil dependencies")
	}
}

func mustCode(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	code, err := totpCode(secret, at)
	if err != nil {
		t.Fatal(err)
	}
	return code
}
