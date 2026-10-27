package requests

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrInvalid = errors.New("Choose a valid movie or show.")
var ErrState = errors.New("The request changed; refresh it before continuing.")

type Request struct {
	ID           int64  `json:"id"`
	AccountID    int64  `json:"account_id"`
	Username     string `json:"username"`
	Kind         string `json:"kind"`
	TMDbID       int64  `json:"tmdb_id"`
	Title        string `json:"title"`
	State        string `json:"state"`
	Available    bool   `json:"available"`
	Notification string `json:"notification"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}
type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

const selectRequest = `SELECT r.id,r.account_id,a.username,r.kind,r.tmdb_id,r.title,r.state,r.created_at,r.updated_at,EXISTS(SELECT 1 FROM media_items m JOIN media_files f ON COALESCE(f.movie_id,f.series_id)=m.id WHERE m.kind=r.kind AND m.tmdb_id=r.tmdb_id),COALESCE(n.state,'') FROM media_requests r JOIN request_accounts a ON a.id=r.account_id LEFT JOIN request_notifications n ON n.request_id=r.id`

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Request, error) {
	var v Request
	err := row.Scan(&v.ID, &v.AccountID, &v.Username, &v.Kind, &v.TMDbID, &v.Title, &v.State, &v.CreatedAt, &v.UpdatedAt, &v.Available, &v.Notification)
	return v, err
}

func (s *Store) Create(ctx context.Context, account int64, kind string, id int64, title string) (Request, error) {
	title = strings.TrimSpace(title)
	if account < 1 || (kind != "movie" && kind != "series") || id < 1 || !utf8.ValidString(title) || len(title) == 0 || len(title) > 400 {
		return Request{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Request{}, err
	}
	defer tx.Rollback()
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_accounts WHERE id=? AND disabled=0`, account).Scan(&active); err != nil {
		return Request{}, err
	}
	if active != 1 {
		return Request{}, sql.ErrNoRows
	}
	existing, err := scan(tx.QueryRowContext(ctx, selectRequest+` WHERE r.account_id=? AND r.kind=? AND r.tmdb_id=?`, account, kind, id))
	if err == nil {
		return existing, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Request{}, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_requests WHERE account_id=?`, account).Scan(&count); err != nil {
		return Request{}, err
	}
	if count >= 1000 {
		return Request{}, errors.New("This account has reached its request limit.")
	}
	now := time.Now().Unix()
	if _, err = tx.ExecContext(ctx, `INSERT INTO media_requests(account_id,kind,tmdb_id,title,created_at,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(account_id,kind,tmdb_id) DO NOTHING`, account, kind, id, title, now, now); err != nil {
		return Request{}, err
	}
	v, err := scan(tx.QueryRowContext(ctx, selectRequest+` WHERE r.account_id=? AND r.kind=? AND r.tmdb_id=?`, account, kind, id))
	if err != nil {
		return Request{}, err
	}
	return v, tx.Commit()
}
func (s *Store) List(ctx context.Context, account int64) ([]Request, error) {
	query := selectRequest
	args := []any{}
	if account > 0 {
		query += ` WHERE r.account_id=?`
		args = append(args, account)
	}
	query += ` ORDER BY r.created_at DESC,r.id DESC LIMIT 200`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Request{}
	for rows.Next() {
		v, e := scan(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) Get(ctx context.Context, id int64) (Request, error) {
	return scan(s.db.QueryRowContext(ctx, selectRequest+` WHERE r.id=?`, id))
}

// Claim makes provider/catalog work retryable after restart and prevents a
// concurrent decline from racing an approved catalog addition.
func (s *Store) Claim(ctx context.Context, id int64) (Request, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE media_requests SET state='approving',updated_at=? WHERE id=? AND state='pending'`, time.Now().Unix(), id)
	if err != nil {
		return Request{}, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return Request{}, err
	}
	v, err := s.Get(ctx, id)
	if err != nil {
		return Request{}, err
	}
	if n == 0 && v.State != "approving" && v.State != "approved" {
		return Request{}, ErrState
	}
	return v, nil
}
func (s *Store) Finish(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `UPDATE media_requests SET state='approved',updated_at=? WHERE id=? AND state='approving'`, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		current, e := s.Get(ctx, id)
		if e != nil {
			return e
		}
		if current.State != "approved" {
			return ErrState
		}
	}
	return nil
}
func (s *Store) Decline(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `UPDATE media_requests SET state='declined',updated_at=? WHERE id=? AND state='pending'`, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrState
	}
	return nil
}
