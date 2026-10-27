package auth

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func authFixture(t *testing.T) (*Service, *sql.DB, string, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	service, err := New(context.Background(), db, dir)
	if err != nil {
		t.Fatal(err)
	}
	code, err := securestore.SecretFile(filepath.Join(dir, "setup-token"), 32, false)
	if err != nil {
		t.Fatal(err)
	}
	return service, db, dir, base64.RawURLEncoding.EncodeToString(code)
}
func TestSetupLoginSessionRestartLogout(t *testing.T) {
	ctx := context.Background()
	s, db, dir, code := authFixture(t)
	if _, err := s.Setup(ctx, "admin", "test-only-password", "wrong"); !errors.Is(err, ErrSetupToken) {
		t.Fatalf("bad setup code: %v", err)
	}
	session, err := s.Setup(ctx, "admin", "test-only-password", code)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "setup-token")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("bootstrap code retained")
	}
	if _, err := s.Setup(ctx, "other", "test-only-password", code); !errors.Is(err, ErrSetupClosed) {
		t.Fatal("setup reopened")
	}
	if _, err := s.Login(ctx, "admin", "wrong"); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("incorrect password accepted")
	}
	if _, err := s.Login(ctx, "other", "test-only-password"); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("incorrect username accepted")
	}
	var hash, stored string
	if err := db.QueryRow("SELECT password_hash FROM admin_account").Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash == "test-only-password" || !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatal("password not hashed")
	}
	if err := db.QueryRow("SELECT token_hash FROM sessions").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == session.Token || stored == "" {
		t.Fatal("raw session stored")
	}
	db.Close()
	db, err = database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err = New(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := s.Resolve(ctx, session.Token)
	if err != nil || restored.CSRFToken != session.CSRFToken {
		t.Fatalf("session not persistent: %v", err)
	}
	if err := s.Logout(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(ctx, session.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("logout did not revoke")
	}
	session, err = s.Login(ctx, "admin", "test-only-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE sessions SET expires_at=?", time.Now().Unix()-1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(ctx, session.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("expired session accepted")
	}
}
func TestPasswordResetAndBounds(t *testing.T) {
	ctx := context.Background()
	s, _, _, code := authFixture(t)
	for _, password := range []string{"short", strings.Repeat("a", 257)} {
		if _, err := s.Setup(ctx, "admin", password, code); !errors.Is(err, ErrPassword) {
			t.Fatal("invalid password accepted")
		}
	}
	if _, err := s.Setup(ctx, "a b", "test-only-password", code); !errors.Is(err, ErrUsername) {
		t.Fatal("invalid username accepted")
	}
	session, err := s.Setup(ctx, "admin", "test-only-password", code)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ResetPassword(ctx, "a-new-test-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(ctx, session.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("reset left old session alive")
	}
	if _, err := s.Login(ctx, "admin", "test-only-password"); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("old password still accepted")
	}
	if _, err := s.Login(ctx, "admin", "a-new-test-password"); err != nil {
		t.Fatal(err)
	}
}

func TestRequestAccountsHaveIsolatedRevocableSessions(t *testing.T) {
	ctx := context.Background()
	s, db, _, code := authFixture(t)
	admin, err := s.Setup(ctx, "admin", "admin-test-password", code)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateRequestAccount(ctx, "viewer", "viewer-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateRequestAccount(ctx, "ADMIN", "viewer-test-password"); !errors.Is(err, ErrAccountExists) {
		t.Fatal("admin name collision accepted", err)
	}
	session, err := s.Login(ctx, "viewer", "viewer-test-password")
	if err != nil || session.Role != "requester" || session.AccountID != a.ID {
		t.Fatal("requester login failed", session, err)
	}
	var hash string
	if err = db.QueryRow(`SELECT token_hash FROM request_sessions WHERE account_id=?`, a.ID).Scan(&hash); err != nil || hash == session.Token {
		t.Fatal("requester session token stored in plaintext", err)
	}
	resolved, err := s.Resolve(ctx, session.Token)
	if err != nil || resolved.Role != "requester" || resolved.AccountID != a.ID {
		t.Fatal("requester session changed identity", resolved, err)
	}
	if err = s.ChangeRequestPassword(ctx, a.ID, "wrong-current-password", "new-viewer-password"); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("incorrect current password accepted", err)
	}
	if err = s.ChangeRequestPassword(ctx, a.ID, "viewer-test-password", "new-viewer-password"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Resolve(ctx, session.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("password change retained session", err)
	}
	session, err = s.Login(ctx, "viewer", "new-viewer-password")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetRequestAccountDisabled(ctx, a.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Resolve(ctx, session.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("disabled requester session survived", err)
	}
	if _, err = s.Login(ctx, "viewer", "new-viewer-password"); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("disabled requester could log in", err)
	}
	if current, err := s.Resolve(ctx, admin.Token); err != nil || current.Role != "admin" {
		t.Fatal("requester disable affected admin", err)
	}
}
func TestLimiterAndHashWorkBound(t *testing.T) {
	s, _, _, _ := authFixture(t)
	for range 10 {
		if !s.Limit("127.0.0.1") {
			t.Fatal("early limit")
		}
	}
	if s.Limit("127.0.0.1") {
		t.Fatal("IP limit missing")
	}
	for i := 0; i < 50; i++ {
		if !s.Limit("other-" + string(rune(65+i))) {
			t.Fatal("early global limit")
		}
	}
	if s.Limit("fresh") {
		t.Fatal("global limit missing")
	}
	s.hashing <- struct{}{}
	s.hashing <- struct{}{}
	if s.acquire() {
		t.Fatal("password work was not bounded")
	}
}
