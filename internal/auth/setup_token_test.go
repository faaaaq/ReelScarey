package auth

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
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
