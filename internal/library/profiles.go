package library

import (
	"context"
	"database/sql"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/quality"
)

type ProfileContext struct {
	Default  quality.Summary   `json:"default_profile"`
	Profiles []quality.Summary `json:"profiles"`
}
type CatalogProfile struct {
	ProfileID *int64          `json:"profile_id"`
	Effective quality.Summary `json:"effective_profile"`
	// Revision is the saved item_automation revision, or 0 when the title has
	// no explicit assignment. Callers echo it back so a stale editor cannot
	// overwrite a newer assignment.
	Revision int `json:"revision"`
}

// CatalogProfiles exposes the schedule default, every saved profile and their
// summaries for callers that render or fence one title's assignment. It shares
// one coherent snapshot with the surrounding transaction.
func CatalogProfiles(ctx context.Context, tx *sql.Tx) (*ProfileContext, map[int64]quality.Summary, error) {
	return catalogProfiles(ctx, tx)
}

func catalogProfiles(ctx context.Context, tx *sql.Tx) (*ProfileContext, map[int64]quality.Summary, error) {
	profiles, e := quality.ListTx(ctx, tx)
	if e != nil {
		return nil, nil, e
	}
	out := &ProfileContext{Profiles: []quality.Summary{}}
	byID := map[int64]quality.Summary{}
	for _, p := range profiles {
		summary := p.Summary()
		byID[p.ID] = summary
		out.Profiles = append(out.Profiles, summary)
	}
	var id int64
	if e = tx.QueryRowContext(ctx, `SELECT default_profile_id FROM search_schedule WHERE id=1`).Scan(&id); e != nil {
		return nil, nil, e
	}
	var exists bool
	out.Default, exists = byID[id]
	if !exists {
		return nil, nil, errors.New("schedule profile is unavailable")
	}
	return out, byID, nil
}
