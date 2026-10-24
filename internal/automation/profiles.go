package automation

import (
	"context"
	"database/sql"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/quality"
)

var ErrProfileReview = &Error{"revision_conflict", "The selection, profile rules or schedule changed. Review again before applying. No titles were changed."}

type ProfileChoice struct {
	UseDefault bool   `json:"use_default"`
	ProfileID  *int64 `json:"profile_id"`
}
type ProfileReviewItem struct {
	ID        int64           `json:"id"`
	Title     string          `json:"title"`
	Year      *int            `json:"year"`
	ProfileID *int64          `json:"profile_id"`
	Revision  int             `json:"revision"`
	Effective quality.Summary `json:"effective_profile"`
}
type ProfileReview struct {
	Target           ProfileChoice       `json:"target"`
	TargetProfile    quality.Profile     `json:"target_profile"`
	ScheduleRevision int                 `json:"schedule_revision"`
	Default          quality.Summary     `json:"default_profile"`
	Profiles         []quality.Summary   `json:"profiles"`
	Items            []ProfileReviewItem `json:"items"`
}
type ProfileExpectation struct {
	ID                int64 `json:"id"`
	Revision          int   `json:"revision"`
	EffectiveID       int64 `json:"effective_id"`
	EffectiveRevision int   `json:"effective_revision"`
}
type ProfileApply struct {
	Kind             string               `json:"kind"`
	Target           ProfileChoice        `json:"target"`
	TargetID         int64                `json:"target_id"`
	TargetRevision   int                  `json:"target_revision"`
	ScheduleRevision int                  `json:"schedule_revision"`
	Items            []ProfileExpectation `json:"items"`
}
type ProfileApplyResult struct {
	Total      int             `json:"total"`
	Changed    int             `json:"changed"`
	Unchanged  int             `json:"unchanged"`
	Profile    quality.Summary `json:"profile"`
	UseDefault bool            `json:"use_default"`
}

func validProfileSelection(kind string, ids []int64, target ProfileChoice) bool {
	if (kind != "movie" && kind != "series") || len(ids) < 1 || len(ids) > 100 {
		return false
	}
	if target.UseDefault {
		if target.ProfileID != nil {
			return false
		}
	} else if target.ProfileID == nil || *target.ProfileID < 1 {
		return false
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if id < 1 || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}
func profileReview(ctx context.Context, tx *sql.Tx, kind string, ids []int64, target ProfileChoice) (ProfileReview, error) {
	out := ProfileReview{Target: target, Items: []ProfileReviewItem{}, Profiles: []quality.Summary{}}
	var defaultID int64
	if e := tx.QueryRowContext(ctx, `SELECT default_profile_id,revision FROM search_schedule WHERE id=1`).Scan(&defaultID, &out.ScheduleRevision); e != nil {
		return out, e
	}
	profiles, e := quality.ListTx(ctx, tx)
	if e != nil {
		return out, e
	}
	byID := map[int64]quality.Profile{}
	for _, p := range profiles {
		byID[p.ID] = p
		out.Profiles = append(out.Profiles, p.Summary())
	}
	defaultProfile, ok := byID[defaultID]
	if !ok {
		return out, sql.ErrNoRows
	}
	out.Default = defaultProfile.Summary()
	targetID := defaultID
	if !target.UseDefault {
		targetID = *target.ProfileID
	}
	out.TargetProfile, ok = byID[targetID]
	if !ok {
		return out, invalid("Choose an existing quality profile or the schedule default.")
	}
	for _, id := range ids {
		var item ProfileReviewItem
		if e = tx.QueryRowContext(ctx, `SELECT m.id,m.title,m.year,a.profile_id,COALESCE(a.revision,0) FROM media_items m LEFT JOIN item_automation a ON a.media_id=m.id WHERE m.id=? AND m.kind=?`, id, kind).Scan(&item.ID, &item.Title, &item.Year, &item.ProfileID, &item.Revision); e != nil {
			return out, e
		}
		effectiveID := defaultID
		if item.ProfileID != nil {
			effectiveID = *item.ProfileID
		}
		profile, exists := byID[effectiveID]
		if !exists {
			return out, sql.ErrNoRows
		}
		item.Effective = profile.Summary()
		out.Items = append(out.Items, item)
	}
	return out, nil
}
func (s *Service) ReviewProfiles(ctx context.Context, kind string, ids []int64, target ProfileChoice) (ProfileReview, error) {
	if !validProfileSelection(kind, ids, target) {
		return ProfileReview{}, invalid("Choose 1–100 distinct saved titles of one media type and a profile assignment.")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return ProfileReview{}, e
	}
	defer tx.Rollback()
	out, e := profileReview(ctx, tx, kind, ids, target)
	if e != nil {
		return out, e
	}
	return out, tx.Commit()
}

// ApplyProfiles changes assignment only. Current and target profile rules, the
// schedule and every item revision are checked before any write. No-op assignments
// stay untouched; changed rows fence existing editors and pending queue decisions.
func (s *Service) ApplyProfiles(ctx context.Context, in ProfileApply) (ProfileApplyResult, error) {
	ids := make([]int64, 0, len(in.Items))
	for _, v := range in.Items {
		if v.Revision < 0 || v.EffectiveID < 1 || v.EffectiveRevision < 1 {
			return ProfileApplyResult{}, invalid("Review the current profile assignments before applying.")
		}
		ids = append(ids, v.ID)
	}
	if !validProfileSelection(in.Kind, ids, in.Target) || in.TargetID < 1 || in.TargetRevision < 1 || in.ScheduleRevision < 1 {
		return ProfileApplyResult{}, invalid("Choose valid titles, a target profile and current review revisions.")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return ProfileApplyResult{}, e
	}
	defer tx.Rollback()
	current, e := profileReview(ctx, tx, in.Kind, ids, in.Target)
	if e != nil {
		var input *Error
		if errors.Is(e, sql.ErrNoRows) || errors.As(e, &input) {
			return ProfileApplyResult{}, ErrProfileReview
		}
		return ProfileApplyResult{}, e
	}
	if current.ScheduleRevision != in.ScheduleRevision || current.TargetProfile.ID != in.TargetID || current.TargetProfile.Revision != in.TargetRevision {
		return ProfileApplyResult{}, ErrProfileReview
	}
	for i, item := range current.Items {
		expected := in.Items[i]
		if item.Revision != expected.Revision || item.Effective.ID != expected.EffectiveID || item.Effective.Revision != expected.EffectiveRevision {
			return ProfileApplyResult{}, ErrProfileReview
		}
	}
	out := ProfileApplyResult{Total: len(ids), Profile: current.TargetProfile.Summary(), UseDefault: in.Target.UseDefault}
	for _, item := range current.Items {
		unchanged := in.Target.UseDefault && item.ProfileID == nil || !in.Target.UseDefault && item.ProfileID != nil && *item.ProfileID == *in.Target.ProfileID
		if unchanged {
			out.Unchanged++
			continue
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO item_automation(media_id,profile_id,revision) VALUES(?,?,1) ON CONFLICT(media_id) DO UPDATE SET profile_id=excluded.profile_id,revision=item_automation.revision+1`, item.ID, in.Target.ProfileID); e != nil {
			return ProfileApplyResult{}, e
		}
		out.Changed++
	}
	return out, tx.Commit()
}
