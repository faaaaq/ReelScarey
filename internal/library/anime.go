package library

import (
	"context"
	"database/sql"
	"errors"
)

var ErrAnimeInput = errors.New("Choose Standard or Anime and distinct loaded episodes with unique absolute numbers between 1 and 100000 (at most 5000 episodes).")
var ErrAnimeRevision = errors.New("The library section or episode metadata changed. Reload and review again before saving.")

func addCategory(values []string) (string, error) {
	if len(values) > 1 {
		return "", ErrAnimeInput
	}
	value := "standard"
	if len(values) == 1 && values[0] != "" {
		value = values[0]
	}
	if value != "standard" && value != "anime" {
		return "", ErrAnimeInput
	}
	return value, nil
}

type AnimeEpisode struct {
	EpisodeID      int64  `json:"episode_id"`
	Season         int    `json:"season"`
	Number         int    `json:"number"`
	Title          string `json:"title"`
	AbsoluteNumber *int   `json:"absolute_number"`
}
type AnimeSettings struct {
	ID                int64          `json:"id"`
	Kind              string         `json:"kind"`
	Category          string         `json:"category"`
	Revision          int            `json:"revision"`
	NumberingRevision int64          `json:"numbering_revision"`
	Episodes          []AnimeEpisode `json:"episodes"`
}
type AnimeAlias struct {
	EpisodeID      int64 `json:"episode_id"`
	AbsoluteNumber *int  `json:"absolute_number"`
}
type AnimeUpdate struct {
	Category          string       `json:"category"`
	Revision          int          `json:"revision"`
	NumberingRevision int64        `json:"numbering_revision"`
	Episodes          []AnimeAlias `json:"episodes"`
}

func readAnime(ctx context.Context, tx *sql.Tx, id int64) (AnimeSettings, error) {
	out := AnimeSettings{Episodes: []AnimeEpisode{}}
	if e := tx.QueryRowContext(ctx, `SELECT id,kind,category,category_revision,COALESCE((SELECT revision FROM series_details WHERE media_id=media_items.id),0) FROM media_items WHERE id=?`, id).Scan(&out.ID, &out.Kind, &out.Category, &out.Revision, &out.NumberingRevision); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT e.id,s.number,e.number,e.title,a.absolute_number FROM episodes e JOIN seasons s ON s.id=e.season_id LEFT JOIN anime_episode_numbers a ON a.episode_id=e.id WHERE s.series_id=? ORDER BY s.number,e.number LIMIT 5001`, id)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var ep AnimeEpisode
		if e = rows.Scan(&ep.EpisodeID, &ep.Season, &ep.Number, &ep.Title, &ep.AbsoluteNumber); e != nil {
			return out, e
		}
		out.Episodes = append(out.Episodes, ep)
	}
	if len(out.Episodes) > 5000 {
		return out, ErrAnimeInput
	}
	return out, rows.Err()
}
func (s *Store) Anime(ctx context.Context, id int64) (AnimeSettings, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return AnimeSettings{}, e
	}
	defer tx.Rollback()
	out, e := readAnime(ctx, tx, id)
	if e != nil {
		return out, e
	}
	return out, tx.Commit()
}

// SaveAnime retains canonical provider, file and episode identities. Aliases are
// explicit owner choices and never inferred from language, genres or filenames.
func (s *Store) SaveAnime(ctx context.Context, id int64, in AnimeUpdate) (AnimeSettings, error) {
	if (in.Category != "standard" && in.Category != "anime") || in.Revision < 1 || len(in.Episodes) > 5000 {
		return AnimeSettings{}, ErrAnimeInput
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return AnimeSettings{}, e
	}
	defer tx.Rollback()
	prior, e := readAnime(ctx, tx, id)
	if e != nil {
		return AnimeSettings{}, e
	}
	if prior.Revision != in.Revision || prior.NumberingRevision != in.NumberingRevision {
		return AnimeSettings{}, ErrAnimeRevision
	}
	valid := map[int64]bool{}
	old := map[int64]int{}
	for _, ep := range prior.Episodes {
		valid[ep.EpisodeID] = true
		if ep.AbsoluteNumber != nil {
			old[ep.EpisodeID] = *ep.AbsoluteNumber
		}
	}
	seen := map[int64]bool{}
	numbers := map[int]bool{}
	aliases := map[int64]int{}
	for _, ep := range in.Episodes {
		if !valid[ep.EpisodeID] || seen[ep.EpisodeID] {
			return AnimeSettings{}, ErrAnimeInput
		}
		seen[ep.EpisodeID] = true
		if ep.AbsoluteNumber == nil {
			continue
		}
		n := *ep.AbsoluteNumber
		if n < 1 || n > 100000 || numbers[n] {
			return AnimeSettings{}, ErrAnimeInput
		}
		numbers[n] = true
		aliases[ep.EpisodeID] = n
	}
	changed := prior.Category != in.Category || len(aliases) != len(old)
	for id, n := range aliases {
		if old[id] != n {
			changed = true
		}
	}
	if !changed {
		return prior, tx.Commit()
	}
	if _, e = tx.ExecContext(ctx, `UPDATE media_items SET category=?,category_revision=category_revision+1 WHERE id=?`, in.Category, id); e != nil {
		return AnimeSettings{}, e
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM anime_episode_numbers WHERE series_id=?`, id); e != nil {
		return AnimeSettings{}, e
	}
	for ep, n := range aliases {
		if _, e = tx.ExecContext(ctx, `INSERT INTO anime_episode_numbers(episode_id,series_id,absolute_number) VALUES(?,?,?)`, ep, id, n); e != nil {
			return AnimeSettings{}, e
		}
	}
	if prior.Kind == "series" {
		if e = bumpSeries(ctx, tx, id, prior.NumberingRevision); e != nil {
			return AnimeSettings{}, e
		}
	}
	// Invalidate older profile/monitoring reviews and automatic queue decisions,
	// including a section changed away and back while release bytes are fetched.
	if _, e = tx.ExecContext(ctx, `INSERT INTO item_automation(media_id,revision) VALUES(?,1) ON CONFLICT(media_id) DO UPDATE SET revision=item_automation.revision+1`, id); e != nil {
		return AnimeSettings{}, e
	}
	out, e := readAnime(ctx, tx, id)
	if e != nil {
		return out, e
	}
	return out, tx.Commit()
}
