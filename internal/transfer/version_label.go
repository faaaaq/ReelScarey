package transfer

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode"

	"github.com/faaaaq/ReelScarey/internal/organization"
)

// maxVersionLabel bounds the owner-written edition label. The empty string
// keeps the derived resolution label.
const maxVersionLabel = 80

// SetFileVersionLabel records an owner-corrected version label for one
// registered movie file. Inspection derives a resolution label only; which
// edition or cut a file actually is (for example the 2019 version of a movie
// registered under a 1994 catalog entry) can never be verified from the
// stream, so the label is an explicit owner correction stored per file. An
// empty label clears the correction and returns to the derived label. Series
// files are rejected: their rows already carry the episode mapping.
func (s *Service) SetFileVersionLabel(ctx context.Context, fileID int64, label string) error {
	label = strings.TrimSpace(label)
	if len(label) > maxVersionLabel {
		return problem("invalid_label", "The version label is limited to 80 characters.")
	}
	for _, r := range label {
		if unicode.IsControl(r) {
			return problem("invalid_label", "The version label cannot contain control characters.")
		}
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = organization.EnsureMediaAvailableTx(ctx, tx); e != nil {
		return e
	}
	var movieID int64
	var title string
	e = tx.QueryRowContext(ctx, `SELECT m.id,m.title FROM media_files f JOIN media_items m ON m.id=f.movie_id WHERE f.id=?`, fileID).Scan(&movieID, &title)
	if errors.Is(e, sql.ErrNoRows) {
		return problem("invalid_file", "Choose a registered file belonging to a movie. Series files do not carry a version label.")
	}
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE media_files SET version_label=? WHERE id=?`, label, fileID); e != nil {
		return e
	}
	state := "file version label cleared"
	if label != "" {
		state = "file version label corrected"
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'title',?,?,?)`, movieID, title, state); e != nil {
		return e
	}
	return tx.Commit()
}
