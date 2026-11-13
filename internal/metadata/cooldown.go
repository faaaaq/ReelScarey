package metadata

import (
	"context"
	"database/sql"
	"time"
)

// ProviderCooldown persists a provider rate-limit deadline so a cooldown
// earned before a restart is honored after it (RS-0025), mirroring the
// musicbrainz_provider_state pattern.
type ProviderCooldown interface {
	Load(ctx context.Context) (time.Time, error)
	Save(ctx context.Context, until time.Time) error
}

// NewSQLCooldown stores the cooldown in a single-row provider state table.
// The upsert is monotonic (NEW-D1): a shorter racing write never lowers a
// stored cooldown deadline, only extends or keeps it.
func NewSQLCooldown(db *sql.DB) ProviderCooldown { return sqlCooldown{db: db} }

type sqlCooldown struct{ db *sql.DB }

func (s sqlCooldown) Load(ctx context.Context) (time.Time, error) {
	var until int64
	err := s.db.QueryRowContext(ctx, `SELECT cooldown_until FROM tmdb_provider_state WHERE id=1`).Scan(&until)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(until, 0), nil
}

func (s sqlCooldown) Save(ctx context.Context, until time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO tmdb_provider_state(id,cooldown_until,message,updated_at) VALUES(1,?,'TMDb is rate limiting requests; they resume automatically.',strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		ON CONFLICT(id) DO UPDATE SET cooldown_until=max(cooldown_until,excluded.cooldown_until),message=excluded.message,updated_at=excluded.updated_at`, until.Unix())
	return err
}
