package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
)

var ErrProfileDefaultInput = errors.New("Choose the Movies, TV and Anime libraries exactly once with valid quality profiles.")

type ProfileDefault struct {
	LibraryKey string `json:"library_key"`
	ProfileID  *int64 `json:"profile_id"`
	Revision   int    `json:"revision"`
}

var profileDefaultKeys = []string{"movies", "tv", "anime"}

func (s *Store) ProfileDefaults(ctx context.Context) ([]ProfileDefault, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT library_key,profile_id,revision FROM library_profile_defaults ORDER BY CASE library_key WHEN 'movies' THEN 0 WHEN 'tv' THEN 1 ELSE 2 END`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProfileDefault{}
	for rows.Next() {
		var item ProfileDefault
		if err := rows.Scan(&item.LibraryKey, &item.ProfileID, &item.Revision); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) DefaultProfileID(ctx context.Context, libraryKey string) (int64, error) {
	var profile sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT profile_id FROM library_profile_defaults WHERE library_key=?`, libraryKey).Scan(&profile)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil || !profile.Valid {
		return 0, err
	}
	return profile.Int64, nil
}

func (s *Store) SaveProfileDefaults(ctx context.Context, in []ProfileDefault) ([]ProfileDefault, error) {
	if len(in) != len(profileDefaultKeys) {
		return nil, ErrProfileDefaultInput
	}
	seen := map[string]bool{}
	for _, item := range in {
		if !slices.Contains(profileDefaultKeys, item.LibraryKey) || seen[item.LibraryKey] {
			return nil, ErrProfileDefaultInput
		}
		seen[item.LibraryKey] = true
		if item.ProfileID != nil && *item.ProfileID < 1 {
			return nil, ErrProfileDefaultInput
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, item := range in {
		var revision int
		if err := tx.QueryRowContext(ctx, `SELECT revision FROM library_profile_defaults WHERE library_key=?`, item.LibraryKey).Scan(&revision); err != nil {
			return nil, err
		}
		if revision != item.Revision {
			return nil, fmt.Errorf("%w: a library default changed; reload and try again", ErrProfileDefaultInput)
		}
		if item.ProfileID != nil {
			var count int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM quality_profiles WHERE id=?`, *item.ProfileID).Scan(&count); err != nil {
				return nil, err
			}
			if count != 1 {
				return nil, ErrProfileDefaultInput
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE library_profile_defaults SET profile_id=?,revision=revision+1,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE library_key=?`, item.ProfileID, item.LibraryKey); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.ProfileDefaults(ctx)
}
