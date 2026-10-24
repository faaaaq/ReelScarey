package securestore

import (
	"bytes"
	"context"
	"github.com/faaaaq/ReelScarey/internal/database"
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptionAndKeyLifecycle(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := New(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	token := "a-test-token-that-is-not-a-real-credential"
	if err := store.Set(ctx, token); err != nil {
		t.Fatal(err)
	}
	var ciphertext []byte
	if err := db.QueryRow("SELECT ciphertext FROM provider_secrets").Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, []byte(token)) {
		t.Fatal("provider token stored in plaintext")
	}
	store, err = New(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	value, err := store.Get(ctx)
	if err != nil || value != token {
		t.Fatal("credential did not survive reopen")
	}
	keyPath := filepath.Join(dir, "secrets.key")
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	if _, err := New(ctx, db, dir); err == nil {
		t.Fatal("missing key silently regenerated")
	}
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatal("missing key was replaced")
	}
	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		t.Fatal(err)
	}
	ciphertext[len(ciphertext)-1] ^= 1
	if _, err := db.Exec("UPDATE provider_secrets SET ciphertext=?", ciphertext); err != nil {
		t.Fatal(err)
	}
	if _, err := New(ctx, db, dir); err == nil {
		t.Fatal("tampered secret accepted")
	}
	if err := store.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	configured, _, err := store.Status(ctx)
	if err != nil || configured {
		t.Fatal("credential removal failed")
	}
}

func TestIndexerOnlyKeyLifecycle(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, e := database.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	store, e := New(ctx, db, dir)
	if e != nil {
		t.Fatal(e)
	}
	cipher, e := store.Seal("indexer:fixture", []byte("private fixture cookie"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO indexers(id,name,kind,enabled,generation,ciphertext,updated_at) VALUES('fixture','Fixture','rss',1,'one',?,1)`, cipher); e != nil {
		t.Fatal(e)
	}
	if _, e = New(ctx, db, dir); e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(filepath.Join(dir, "secrets.key")); e != nil {
		t.Fatal(e)
	}
	if _, e = New(ctx, db, dir); e == nil {
		t.Fatal("indexer-only config silently generated a replacement key")
	}
}
