package requests

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
)

var imdbTitleID = regexp.MustCompile(`^tt[0-9]{5,12}$`)
var ErrIMDbCSV = errors.New("Upload an IMDb CSV export with a Const column and 1–25 title IDs.")

func IMDbIDs(body string) ([]string, error) {
	if len(body) > 512<<10 {
		return nil, ErrIMDbCSV
	}
	r := csv.NewReader(strings.NewReader(body))
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		return nil, ErrIMDbCSV
	}
	column := -1
	for i, v := range header {
		if strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(v, "\ufeff")), "Const") {
			if column != -1 {
				return nil, ErrIMDbCSV
			}
			column = i
		}
	}
	if column < 0 {
		return nil, ErrIMDbCSV
	}
	ids := []string{}
	seen := map[string]bool{}
	for {
		row, e := r.Read()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil || column >= len(row) {
			return nil, ErrIMDbCSV
		}
		id := strings.TrimSpace(row[column])
		if !imdbTitleID.MatchString(id) {
			return nil, ErrIMDbCSV
		}
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
		if len(ids) > 25 {
			return nil, ErrIMDbCSV
		}
	}
	if len(ids) == 0 {
		return nil, ErrIMDbCSV
	}
	return ids, nil
}

// ImportResolved inserts only a completely resolved batch, preserving each
// user's existing requests and refusing disabled accounts or excessive totals.
func (s *Store) ImportResolved(ctx context.Context, account int64, items []Request) (int, error) {
	if account < 1 || len(items) < 1 || len(items) > 25 {
		return 0, ErrIMDbCSV
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var active, count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_accounts WHERE id=? AND disabled=0`, account).Scan(&active); err != nil {
		return 0, err
	}
	if active != 1 {
		return 0, sql.ErrNoRows
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_requests WHERE account_id=?`, account).Scan(&count); err != nil {
		return 0, err
	}
	seen := map[string]bool{}
	added := 0
	for _, v := range items {
		if (v.Kind != "movie" && v.Kind != "series") || v.TMDbID < 1 || len(strings.TrimSpace(v.Title)) < 1 || len(v.Title) > 400 {
			return 0, ErrIMDbCSV
		}
		key := v.Kind + ":" + strconv.FormatInt(v.TMDbID, 10)
		if seen[key] {
			continue
		}
		seen[key] = true
		var exists int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_requests WHERE account_id=? AND kind=? AND tmdb_id=?`, account, v.Kind, v.TMDbID).Scan(&exists); err != nil {
			return 0, err
		}
		if exists == 0 {
			added++
		}
	}
	if count+added > 1000 {
		return 0, ErrIMDbCSV
	}
	for _, v := range items {
		if _, err = tx.ExecContext(ctx, `INSERT INTO media_requests(account_id,kind,tmdb_id,title,created_at,updated_at) VALUES(?,?,?,?,unixepoch(),unixepoch()) ON CONFLICT(account_id,kind,tmdb_id) DO NOTHING`, account, v.Kind, v.TMDbID, strings.TrimSpace(v.Title)); err != nil {
			return 0, err
		}
	}
	return added, tx.Commit()
}
