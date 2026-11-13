package auth

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

// ONB-P3-1: a deleted setup-token file must produce the restart-recovery
// advice, not the generic backup-restore error, while setup is incomplete.
func TestMissingSetupTokenFileReportsRestartRecovery(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, e := database.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s, e := New(ctx, db, dir)
	if e != nil {
		t.Fatal(e)
	}
	// The token file exists right after construction; deleting it simulates
	// the interrupted-setup scenario.
	if e := os.Remove(filepath.Join(dir, "setup-token")); e != nil {
		t.Fatal(e)
	}
	_, e = s.SetupWith(ctx, "owner", "test-only-password", "whatever", func(context.Context, *sql.Tx) error { return nil })
	if !errors.Is(e, ErrSetupTokenFile) {
		t.Fatalf("expected ErrSetupTokenFile, got %v", e)
	}
	if !strings.Contains(e.Error(), "Restart ReelScarey") {
		t.Fatalf("message lost the recovery advice: %v", e)
	}
	// An unrelated store failure (wrong file type) keeps its own error.
	if e := os.WriteFile(filepath.Join(dir, "setup-token"), []byte("not base64 for the required length"), 0o600); e != nil {
		t.Fatal(e)
	}
	_, e = s.SetupWith(ctx, "owner", "test-only-password", "whatever", func(context.Context, *sql.Tx) error { return nil })
	if errors.Is(e, ErrSetupTokenFile) {
		t.Fatalf("invalid contents must not be reported as a missing file: %v", e)
	}
}

// RS-0021: a setup code must not stay valid forever while an instance runs
// unconfigured. The file format is unchanged (plain secret bytes); the token
// file's modification time bounds its age, so a code older than seven days is
// rejected with the existing invalid_setup_token failure and the stale file is
// deleted. Restarting recreates a fresh code, which is the existing recovery.
func TestSetupTokenRejectsStaleFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, e := database.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s, e := New(ctx, db, dir)
	if e != nil {
		t.Fatal(e)
	}
	secret, e := securestore.SecretFile(filepath.Join(dir, "setup-token"), 32, false)
	if e != nil {
		t.Fatal(e)
	}
	code := base64.RawURLEncoding.EncodeToString(secret)

	// A fresh code still completes setup.
	if _, e = s.Setup(ctx, "owner", "test-only-password", code); e != nil {
		t.Fatalf("a fresh setup code must still work: %v", e)
	}
}

func TestSetupTokenStaleMtimeIsRejected(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, e := database.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s, e := New(ctx, db, dir)
	if e != nil {
		t.Fatal(e)
	}
	tokenPath := filepath.Join(dir, "setup-token")
	secret, e := securestore.SecretFile(tokenPath, 32, false)
	if e != nil {
		t.Fatal(e)
	}
	code := base64.RawURLEncoding.EncodeToString(secret)

	// Backdate the file's mtime past the age bound without changing contents.
	old := time.Now().Add(-8 * 24 * time.Hour)
	if e = os.Chtimes(tokenPath, old, old); e != nil {
		t.Fatal(e)
	}
	_, e = s.Setup(ctx, "owner", "test-only-password", code)
	if !errors.Is(e, ErrSetupToken) {
		t.Fatalf("a stale setup code must be rejected as invalid, got: %v", e)
	}
	if _, statErr := os.Lstat(tokenPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("the stale token file must be deleted after rejection: %v", statErr)
	}

	// Restarting recreates the file and the new code works again.
	s2, e := New(ctx, db, dir)
	if e != nil {
		t.Fatal(e)
	}
	fresh, e := securestore.SecretFile(tokenPath, 32, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s2.Setup(ctx, "owner", "test-only-password", base64.RawURLEncoding.EncodeToString(fresh)); e != nil {
		t.Fatalf("a recreated setup code must work: %v", e)
	}
}
