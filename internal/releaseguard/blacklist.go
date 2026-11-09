package releaseguard

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Entry is one durable blacklist decision. ExpiresAt is a Unix timestamp in
// seconds; zero means the entry is permanent. Indexer may be empty to describe
// a release identity that is not tied to a named indexer.
type Entry struct {
	ID         int64  `json:"id"`
	Protocol   string `json:"protocol"`
	ReleaseKey string `json:"release_key"`
	Indexer    string `json:"indexer"`
	Title      string `json:"title"`
	Reason     string `json:"reason"`
	CreatedAt  int64  `json:"created_at"`
	ExpiresAt  int64  `json:"expires_at"`
}

// Store persists blacklist decisions in the release_blacklist table.
type Store struct{ db *sql.DB }

// New returns a blacklist store over the application database.
func New(db *sql.DB) *Store { return &Store{db: db} }

// Block inserts or replaces the blacklist decision for one release identity and
// indexer. Re-blocking keeps the existing row ID but refreshes the protocol,
// title, reason and expiry. A zero CreatedAt is replaced with the current time;
// a zero ExpiresAt means permanent.
//
// The table's unique key is (release_key, indexer), not protocol, so a decision
// stored for the same release key and indexer under another protocol is
// replaced rather than duplicated.
func (s *Store) Block(ctx context.Context, entry Entry) error {
	protocol, err := validProtocol(entry.Protocol)
	if err != nil {
		return err
	}
	releaseKey := strings.TrimSpace(entry.ReleaseKey)
	if releaseKey == "" {
		return invalid("release key is required")
	}
	if len(releaseKey) > maxReleaseKeyBytes {
		return invalid("release key is too long")
	}
	if entry.ExpiresAt < 0 {
		return invalid("blacklist expiry must be zero or a positive Unix timestamp")
	}
	createdAt := entry.CreatedAt
	if createdAt <= 0 {
		createdAt = time.Now().Unix()
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO release_blacklist
		(protocol, release_key, indexer, title, reason, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(release_key, indexer) DO UPDATE SET
			protocol = excluded.protocol,
			title = excluded.title,
			reason = excluded.reason,
			created_at = excluded.created_at,
			expires_at = excluded.expires_at`,
		protocol, releaseKey, strings.TrimSpace(entry.Indexer), strings.TrimSpace(entry.Title),
		strings.TrimSpace(entry.Reason), createdAt, entry.ExpiresAt)
	return err
}

// Blocked reports whether the exact release identity is currently blacklisted
// for the protocol and indexer. Permanent entries never expire; expired entries
// are ignored even if they have not been cleaned up yet.
func (s *Store) Blocked(ctx context.Context, protocol, releaseKey, indexer string) (bool, error) {
	protocol, err := validProtocol(protocol)
	if err != nil {
		return false, err
	}
	releaseKey = strings.TrimSpace(releaseKey)
	if releaseKey == "" {
		return false, invalid("release key is required")
	}
	var blocked bool
	err = s.db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM release_blacklist
		WHERE release_key = ? AND indexer = ? AND protocol = ?
			AND (expires_at = 0 OR expires_at > ?))`,
		releaseKey, strings.TrimSpace(indexer), protocol, time.Now().Unix()).Scan(&blocked)
	return blocked, err
}

// List returns blacklist entries newest first, including entries whose expiry
// has passed but which have not been removed. A non-positive limit uses a
// conservative default; very large limits are capped.
func (s *Store) List(ctx context.Context, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, protocol, release_key, indexer, title, reason, created_at, expires_at
		FROM release_blacklist
		ORDER BY created_at DESC, id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Entry, 0)
	for rows.Next() {
		var entry Entry
		if err := rows.Scan(&entry.ID, &entry.Protocol, &entry.ReleaseKey, &entry.Indexer,
			&entry.Title, &entry.Reason, &entry.CreatedAt, &entry.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

// Remove deletes one blacklist entry by ID. Removing a missing entry is not an
// error.
func (s *Store) Remove(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM release_blacklist WHERE id = ?`, id)
	return err
}

// CleanupExpired deletes entries whose expiry has passed and returns the number
// of rows removed. Permanent entries (expires_at = 0) are preserved.
func (s *Store) CleanupExpired(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM release_blacklist WHERE expires_at > 0 AND expires_at <= ?`,
		time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func validProtocol(protocol string) (string, error) {
	protocol = strings.TrimSpace(protocol)
	if protocol != ProtocolTorrent && protocol != ProtocolUsenet {
		return "", invalid("protocol must be torrent or usenet")
	}
	return protocol, nil
}
