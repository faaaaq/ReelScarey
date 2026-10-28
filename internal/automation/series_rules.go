package automation

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
)

type SeriesRules struct {
	Codec    string `json:"codec"`
	Group    string `json:"group"`
	Revision int    `json:"revision"`
}

var groupRule = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$`)

func (s *Service) SeriesRules(ctx context.Context, id int64) (SeriesRules, error) {
	var kind string
	var r SeriesRules
	e := s.db.QueryRowContext(ctx, `SELECT m.kind,COALESCE(r.codec,''),COALESCE(r.release_group,''),COALESCE(r.revision,0) FROM media_items m LEFT JOIN series_release_rules r ON r.media_id=m.id WHERE m.id=?`, id).Scan(&kind, &r.Codec, &r.Group, &r.Revision)
	if e != nil {
		return r, e
	}
	if kind != "series" {
		return r, invalid("Choose a TV show.")
	}
	return r, nil
}
func (s *Service) SaveSeriesRules(ctx context.Context, id int64, r SeriesRules) (SeriesRules, error) {
	r.Group = strings.TrimSpace(r.Group)
	if (r.Codec != "" && r.Codec != "h264" && r.Codec != "h265") || (r.Group != "" && !groupRule.MatchString(r.Group)) || r.Revision < 0 {
		return SeriesRules{}, invalid("Choose a supported codec and a release group using letters, numbers, dots, underscores or hyphens.")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return SeriesRules{}, e
	}
	defer tx.Rollback()
	var kind string
	var current int
	e = tx.QueryRowContext(ctx, `SELECT m.kind,COALESCE(r.revision,0) FROM media_items m LEFT JOIN series_release_rules r ON r.media_id=m.id WHERE m.id=?`, id).Scan(&kind, &current)
	if errors.Is(e, sql.ErrNoRows) {
		return SeriesRules{}, e
	}
	if e != nil {
		return SeriesRules{}, e
	}
	if kind != "series" {
		return SeriesRules{}, invalid("Choose a TV show.")
	}
	if current != r.Revision {
		return SeriesRules{}, &Error{"revision_conflict", "These release rules changed. Reload before saving."}
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO series_release_rules(media_id,codec,release_group,revision) VALUES(?,?,?,1) ON CONFLICT(media_id) DO UPDATE SET codec=excluded.codec,release_group=excluded.release_group,revision=series_release_rules.revision+1`, id, r.Codec, r.Group)
	if e != nil {
		return SeriesRules{}, e
	}
	if e = tx.Commit(); e != nil {
		return SeriesRules{}, e
	}
	r.Revision++
	return r, nil
}
