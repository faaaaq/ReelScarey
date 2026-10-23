package quality

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

var ErrConflict = errors.New("The profile changed. Refresh before saving.")

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

const columns = `id,name,qualities,cutoff,language,min_seeders,max_size_gib,required_terms,rejected_terms,revision`

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Profile, error) {
	var p Profile
	var q string
	e := row.Scan(&p.ID, &p.Name, &q, &p.Cutoff, &p.Language, &p.MinSeeders, &p.MaxSizeGiB, &p.RequiredTerms, &p.RejectedTerms, &p.Revision)
	if e == nil {
		e = json.Unmarshal([]byte(q), &p.Qualities)
	}
	return p, e
}
func (s *Store) Get(ctx context.Context, id int64) (Profile, error) {
	return scan(s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM quality_profiles WHERE id=?`, id))
}
func (s *Store) List(ctx context.Context) ([]Profile, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT `+columns+` FROM quality_profiles ORDER BY id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Profile{}
	for rows.Next() {
		p, e := scan(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) Save(ctx context.Context, p Profile) (Profile, error) {
	p.Name = strings.TrimSpace(p.Name)
	if e := Validate(p); e != nil {
		return Profile{}, e
	}
	q, _ := json.Marshal(p.Qualities)
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return Profile{}, e
	}
	defer tx.Rollback()
	var duplicate int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM quality_profiles WHERE name=? COLLATE NOCASE AND id<>?`, p.Name, p.ID).Scan(&duplicate); e != nil {
		return Profile{}, e
	}
	if duplicate > 0 {
		return Profile{}, &ValidationError{"Choose a unique profile name."}
	}
	if p.ID == 0 {
		var count int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM quality_profiles`).Scan(&count); e != nil {
			return Profile{}, e
		}
		if count >= 20 {
			return Profile{}, &ValidationError{"At most 20 profiles are supported."}
		}
		r, e := tx.ExecContext(ctx, `INSERT INTO quality_profiles(name,qualities,cutoff,language,min_seeders,max_size_gib,required_terms,rejected_terms) VALUES(?,?,?,?,?,?,?,?)`, p.Name, string(q), p.Cutoff, p.Language, p.MinSeeders, p.MaxSizeGiB, p.RequiredTerms, p.RejectedTerms)
		if e != nil {
			return Profile{}, e
		}
		p.ID, _ = r.LastInsertId()
		p.Revision = 1
	} else {
		r, e := tx.ExecContext(ctx, `UPDATE quality_profiles SET name=?,qualities=?,cutoff=?,language=?,min_seeders=?,max_size_gib=?,required_terms=?,rejected_terms=?,revision=revision+1 WHERE id=? AND revision=?`, p.Name, string(q), p.Cutoff, p.Language, p.MinSeeders, p.MaxSizeGiB, p.RequiredTerms, p.RejectedTerms, p.ID, p.Revision)
		if e != nil {
			return Profile{}, e
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return Profile{}, ErrConflict
		}
		p.Revision++
	}
	if e = tx.Commit(); e != nil {
		return Profile{}, e
	}
	return p, nil
}
