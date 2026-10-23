// Package securestore encrypts provider credentials with a locally held key.
// Backups must include both the SQLite database and secrets.key.
package securestore

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SecretFile creates a random, owner-only file if permitted, or reads an
// existing regular file. It never replaces a key or follows a symlink.
func SecretFile(filename string, size int, create bool) ([]byte, error) {
	if info, err := os.Lstat(filename); err == nil {
		if !info.Mode().IsRegular() || info.Size() > 1024 {
			return nil, errors.New("secret file must be a small regular file")
		}
		body, err := os.ReadFile(filename)
		if err != nil {
			return nil, errors.New("cannot read secret file")
		}
		secret, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(body)))
		if err != nil || len(secret) != size {
			return nil, errors.New("secret file has invalid contents")
		}
		return secret, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("cannot inspect secret file")
	}
	if !create {
		return nil, errors.New("required secret file is missing; restore it from your config backup")
	}
	secret := make([]byte, size)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		return SecretFile(filename, size, false)
	}
	if err != nil {
		return nil, errors.New("cannot create secret file")
	}
	_, writeErr := f.WriteString(base64.RawURLEncoding.EncodeToString(secret) + "\n")
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		return nil, errors.New("cannot persist secret file")
	}
	return secret, nil
}

type Store struct {
	db   *sql.DB
	aead cipher.AEAD
}

func New(ctx context.Context, db *sql.DB, dir string) (*Store, error) {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM provider_secrets) + (SELECT COUNT(*) FROM integration_configs) + (SELECT COUNT(*) FROM release_candidates) + (SELECT COUNT(*) FROM downloads)`).Scan(&count); err != nil {
		return nil, err
	}
	key, err := SecretFile(filepath.Join(dir, "secrets.key"), 32, count == 0)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	store := &Store{db: db, aead: aead}
	if count > 0 {
		if _, err := store.Get(ctx); err != nil {
			return nil, err
		}
		rows, err := db.QueryContext(ctx, `SELECT 'config:' || name, ciphertext FROM integration_configs UNION ALL SELECT 'release', payload FROM release_candidates UNION ALL SELECT 'download', payload FROM downloads`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var purpose string
			var sealed []byte
			if err := rows.Scan(&purpose, &sealed); err != nil {
				return nil, err
			}
			if _, err := store.Open(purpose, sealed); err != nil {
				return nil, err
			}
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return store, nil
}

// Seal and Open protect integration settings and torrent payloads as well as
// credentials: torrent URLs/metainfo can contain private tracker passkeys.
func (s *Store) Seal(purpose string, plain []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return s.aead.Seal(nonce, nonce, plain, []byte("reelscarey:"+purpose+":v1")), nil
}
func (s *Store) Open(purpose string, sealed []byte) ([]byte, error) {
	if len(sealed) < s.aead.NonceSize() {
		return nil, errors.New("invalid encrypted integration data")
	}
	plain, err := s.aead.Open(nil, sealed[:s.aead.NonceSize()], sealed[s.aead.NonceSize():], []byte("reelscarey:"+purpose+":v1"))
	if err != nil {
		return nil, errors.New("cannot decrypt integration data; restore the matching secrets.key")
	}
	return plain, nil
}
func (s *Store) GetNamed(ctx context.Context, name string) ([]byte, error) {
	var sealed []byte
	err := s.db.QueryRowContext(ctx, "SELECT ciphertext FROM integration_configs WHERE name = ?", name).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.Open("config:"+name, sealed)
}
func (s *Store) SetNamed(ctx context.Context, name string, plain []byte) error {
	sealed, err := s.Seal("config:"+name, plain)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO integration_configs(name,ciphertext) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET ciphertext=excluded.ciphertext,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')`, name, sealed)
	return err
}
func (s *Store) DeleteNamed(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM integration_configs WHERE name = ?", name)
	return err
}

func (s *Store) Get(ctx context.Context) (string, error) {
	var sealed []byte
	err := s.db.QueryRowContext(ctx, "SELECT ciphertext FROM provider_secrets WHERE provider = 'tmdb'").Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if len(sealed) < s.aead.NonceSize() {
		return "", errors.New("invalid encrypted provider credential")
	}
	plain, err := s.aead.Open(nil, sealed[:s.aead.NonceSize()], sealed[s.aead.NonceSize():], []byte("reelscarey:tmdb:v1"))
	if err != nil {
		return "", errors.New("cannot decrypt provider credential; restore the matching secrets.key")
	}
	return string(plain), nil
}

func (s *Store) Set(ctx context.Context, token string) error {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	sealed := s.aead.Seal(nonce, nonce, []byte(token), []byte("reelscarey:tmdb:v1"))
	_, err := s.db.ExecContext(ctx, `INSERT INTO provider_secrets(provider, ciphertext) VALUES ('tmdb', ?) ON CONFLICT(provider) DO UPDATE SET ciphertext = excluded.ciphertext, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`, sealed)
	return err
}

func (s *Store) Delete(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM provider_secrets WHERE provider = 'tmdb'")
	return err
}
func (s *Store) Status(ctx context.Context) (bool, string, error) {
	var updated string
	err := s.db.QueryRowContext(ctx, "SELECT updated_at FROM provider_secrets WHERE provider = 'tmdb'").Scan(&updated)
	if errors.Is(err, sql.ErrNoRows) {
		return false, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("read provider status: %w", err)
	}
	return true, updated, nil
}
