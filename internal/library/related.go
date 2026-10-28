package library

import (
	"context"
	"database/sql"
	"errors"
)

var ErrRelatedLimit = errors.New("Each title can have at most 200 related titles.")

func (s *Store) Related(ctx context.Context, id int64) ([]Item, error) {
	if _, e := s.Item(ctx, id); e != nil {
		return nil, e
	}
	rows, e := s.db.QueryContext(ctx, `SELECT CASE WHEN left_id=? THEN right_id ELSE left_id END FROM related_titles WHERE left_id=? OR right_id=? ORDER BY created_at DESC,left_id,right_id LIMIT 200`, id, id, id)
	if e != nil {
		return nil, e
	}
	ids := []int64{}
	for rows.Next() {
		var v int64
		if e = rows.Scan(&v); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []Item{}
	for _, v := range ids {
		item, e := s.Item(ctx, v)
		if e != nil {
			return nil, e
		}
		out = append(out, item)
	}
	return out, nil
}
func (s *Store) LinkRelated(ctx context.Context, a, b int64) error {
	if a < 1 || b < 1 || a == b {
		return errors.New("Choose two different saved titles.")
	}
	if a > b {
		a, b = b, a
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var count int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_items WHERE id IN (?,?)`, a, b).Scan(&count); e != nil {
		return e
	}
	if count != 2 {
		return sql.ErrNoRows
	}
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM related_titles WHERE left_id=? AND right_id=?`, a, b).Scan(&count); e != nil {
		return e
	}
	if count == 1 {
		return tx.Commit()
	}
	for _, id := range []int64{a, b} {
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM related_titles WHERE left_id=? OR right_id=?`, id, id).Scan(&count); e != nil {
			return e
		}
		if count >= 200 {
			return ErrRelatedLimit
		}
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO related_titles(left_id,right_id) VALUES(?,?) ON CONFLICT DO NOTHING`, a, b); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) UnlinkRelated(ctx context.Context, a, b int64) error {
	if a < 1 || b < 1 || a == b {
		return errors.New("Choose two different saved titles.")
	}
	if a > b {
		a, b = b, a
	}
	_, e := s.db.ExecContext(ctx, `DELETE FROM related_titles WHERE left_id=? AND right_id=?`, a, b)
	return e
}
