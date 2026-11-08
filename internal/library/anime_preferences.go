package library

import (
	"context"
	"database/sql"
	"errors"

	"github.com/faaaaq/ReelScarey/internal/quality"
)

var ErrAnimePreferences = errors.New("Enter a valid English-dub rule with at most 100 regex or phrase lines.")
var ErrAnimePreferencesRevision = errors.New("The English-dub rule changed. Reload and try again.")

type AnimePreferences struct {
	DubRule     string `json:"dub_rule"`
	Revision    int    `json:"revision"`
	DefaultRule string `json:"default_rule"`
}

func (s *Store) AnimePreferences(ctx context.Context) (AnimePreferences, error) {
	var out AnimePreferences
	if err := s.db.QueryRowContext(ctx, `SELECT dub_rule,revision FROM anime_preferences WHERE id=1`).Scan(&out.DubRule, &out.Revision); err != nil {
		return AnimePreferences{}, err
	}
	out.DefaultRule = quality.DefaultDubRule
	return out, nil
}

func (s *Store) SaveAnimePreferences(ctx context.Context, in AnimePreferences) (AnimePreferences, error) {
	if _, err := quality.ParseDubRule(in.DubRule); err != nil {
		return AnimePreferences{}, ErrAnimePreferences
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AnimePreferences{}, err
	}
	defer tx.Rollback()
	var current int
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM anime_preferences WHERE id=1`).Scan(&current); err != nil {
		return AnimePreferences{}, err
	}
	if current != in.Revision {
		return AnimePreferences{}, ErrAnimePreferencesRevision
	}
	if _, err := tx.ExecContext(ctx, `UPDATE anime_preferences SET dub_rule=?,revision=revision+1,updated_at=unixepoch() WHERE id=1 AND revision=?`, in.DubRule, in.Revision); err != nil {
		return AnimePreferences{}, err
	}
	if err := tx.Commit(); err != nil {
		return AnimePreferences{}, err
	}
	return s.AnimePreferences(ctx)
}

// SetAllAnimeEnglishDub toggles the per-show rule for every Anime show and
// bumps each policy revision so an older search cannot be queued.
func (s *Store) SetAllAnimeEnglishDub(ctx context.Context, enabled bool) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	scope := `SELECT id FROM media_items WHERE kind='series' AND category='anime'`
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM (`+scope+`)`).Scan(&count); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO anime_search_settings(media_id,english_dub_only,revision) SELECT id,?,1 FROM media_items WHERE kind='series' AND category='anime' ON CONFLICT(media_id) DO UPDATE SET english_dub_only=excluded.english_dub_only,revision=anime_search_settings.revision+1`, enabled); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE series_details SET revision=revision+1,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE media_id IN (`+scope+`)`); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

// AnimePreferencesInTx reads the current rule inside a queue/reservation
// transaction so a rule edit fences an older selection.
func AnimePreferencesInTx(ctx context.Context, tx *sql.Tx) (string, error) {
	var rule string
	if err := tx.QueryRowContext(ctx, `SELECT dub_rule FROM anime_preferences WHERE id=1`).Scan(&rule); err != nil {
		return "", err
	}
	return rule, nil
}
