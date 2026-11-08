package automation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/quality"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

// This optional interface keeps legacy workflow implementations unchanged.
// Automatic mapped candidates have distinct authority from manual pack searches.
type mappedWorkflow interface {
	SearchAutomaticAnimeOrder(context.Context, int64, []int64, int) ([]transfer.Release, error)
}
type mappedPlan struct {
	policy   library.AnimeSearchSettings
	order    library.AnimeOrderSnapshot
	targets  []searchTarget
	identity quality.Target
	ids      []int64
}

func mappedSpecial(season int, label string) bool {
	family, _, ok := quality.MappedAnimeLabel(label)
	return !ok || season == 0 || family == "S0" || family == "SP" || family == "OVA" || family == "ONA" || family == "OAD"
}
func sameMappedTarget(a, b searchTarget) bool {
	return a.Key == b.Key && a.MediaID == b.MediaID && a.ProfileID == b.ProfileID && a.AssignmentRevision == b.AssignmentRevision && a.AnimeRevision == b.AnimeRevision && a.CategoryRevision == b.CategoryRevision && a.NumberingRevision == b.NumberingRevision && a.Probe == b.Probe && a.AnimeSearchTitle == b.AnimeSearchTitle && reflect.DeepEqual(a.Target, b.Target)
}

func (s *Service) mappedEligibility(ctx context.Context, mediaID int64) (map[int64]searchTarget, error) {
	date := s.now().UTC().Format("2006-01-02")
	rows, e := s.db.QueryContext(ctx, targetSQL+` AS mapped WHERE media_id=? AND anime_mode='mapped' AND episode_id IN (SELECT episode_id FROM anime_order_entries WHERE media_id=?) AND NOT EXISTS(SELECT 1 FROM episode_files ef WHERE ef.episode_id=mapped.episode_id) ORDER BY key LIMIT 5001`, date, 0, mediaID, mediaID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[int64]searchTarget{}
	for rows.Next() {
		t, e := scanTarget(rows)
		if e != nil {
			return nil, e
		}
		if t.EpisodeID != nil {
			out[*t.EpisodeID] = t
		}
	}
	return out, rows.Err()
}

func makeMappedPlan(policy library.AnimeSearchSettings, order library.AnimeOrderSettings, entries []library.AnimeOrderEntry, eligible map[int64]searchTarget) (mappedPlan, bool) {
	plan := mappedPlan{policy: policy, ids: []int64{}, targets: []searchTarget{}, identity: quality.Target{TV: true, Anime: true, AnimeMode: "mapped"}, order: library.AnimeOrderSnapshot{MediaID: order.MediaID, Revision: order.Revision, CategoryRevision: order.CategoryRevision, NumberingRevision: order.NumberingRevision, Identities: order.Identities, Entries: []library.AnimeOrderEntry{}}}
	if len(entries) < 1 || len(entries) > 100 || len(entries) > 1 && (!policy.AllowPacks || len(entries) > policy.MaxPackEpisodes) {
		return plan, false
	}
	sort.Slice(entries, func(i, j int) bool {
		_, a, _ := quality.MappedAnimeLabel(entries[i].Label)
		_, b, _ := quality.MappedAnimeLabel(entries[j].Label)
		return a < b
	})
	plan.identity.AnimeOrderTitle = entries[0].ReleaseTitle
	for _, entry := range entries {
		t, ok := eligible[entry.EpisodeID]
		if !ok || t.NumberingRevision != policy.NumberingRevision || t.CategoryRevision != policy.CategoryRevision || t.AnimeRevision != policy.Revision || t.Probe != "" || !policy.IncludeSpecials && mappedSpecial(t.Season, entry.Label) || quality.Normalize(entry.ReleaseTitle) != quality.Normalize(plan.identity.AnimeOrderTitle) {
			return plan, false
		}
		if len(plan.targets) == 0 {
			plan.identity.SeriesCodec = t.SeriesCodec
			plan.identity.SeriesGroup = t.SeriesGroup
			plan.identity.SeriesRulesRevision = t.SeriesRulesRevision
		} else if plan.identity.SeriesCodec != t.SeriesCodec || plan.identity.SeriesGroup != t.SeriesGroup || plan.identity.SeriesRulesRevision != t.SeriesRulesRevision {
			return plan, false
		}
		plan.ids = append(plan.ids, entry.EpisodeID)
		plan.targets = append(plan.targets, t)
		plan.order.Entries = append(plan.order.Entries, entry)
		plan.identity.AnimeOrderLabels = append(plan.identity.AnimeOrderLabels, entry.Label)
	}
	return plan, len(quality.MappedAnimeQueries(plan.identity.AnimeOrderTitle, plan.identity.AnimeOrderLabels)) > 0
}

func (s *Service) processMapped(ctx context.Context, c claim, seed searchTarget, profile quality.Profile, seen map[string]bool, queued map[int64]bool) error {
	workflow, ok := s.workflow.(mappedWorkflow)
	if !ok {
		return s.record(ctx, c, seed, "needs_attention", "This search provider does not support mapped Anime automation.", nil, nil)
	}
	policy, e := library.New(s.db).AnimeSearch(ctx, seed.MediaID)
	if e != nil {
		return e
	}
	if policy.Mode != "mapped" || policy.Revision != seed.AnimeRevision || policy.NumberingRevision != seed.NumberingRevision || policy.CategoryRevision != seed.CategoryRevision {
		return ErrLease
	}
	if policy.OrderRevision < 1 || policy.ApprovedOrderRevision != policy.OrderRevision {
		return s.record(ctx, c, seed, "needs_order_review", "The saved Anime mapping changed. Review and save this show's automatic search settings before using the updated mapping.", nil, nil)
	}
	order, e := library.New(s.db).AnimeOrder(ctx, seed.MediaID)
	if e != nil {
		return e
	}
	if order.Revision != policy.OrderRevision || order.NumberingRevision != policy.NumberingRevision || order.CategoryRevision != policy.CategoryRevision {
		return ErrLease
	}
	var anchor *library.AnimeOrderEntry
	for i := range order.Entries {
		if seed.EpisodeID != nil && order.Entries[i].EpisodeID == *seed.EpisodeID {
			anchor = &order.Entries[i]
			break
		}
	}
	if anchor == nil {
		return s.record(ctx, c, seed, "needs_mapping", "Save a release title and episode label for this episode before mapped automatic searching.", nil, nil)
	}
	if !policy.IncludeSpecials && mappedSpecial(seed.Season, anchor.Label) {
		return s.record(ctx, c, seed, "specials_disabled", "Specials and typed OVA/ONA/OAD labels are excluded by this show's automatic search settings.", nil, nil)
	}
	eligible, e := s.mappedEligibility(ctx, seed.MediaID)
	if e != nil {
		return e
	}
	single, ok := makeMappedPlan(policy, order, []library.AnimeOrderEntry{*anchor}, eligible)
	if !ok {
		return s.record(ctx, c, seed, "not_needed", "This mapped episode is no longer missing, released and monitored, or is already reserved. Existing files remain unchanged.", nil, nil)
	}
	if !sameMappedTarget(single.targets[0], seed) {
		return ErrLease
	}
	plans := []mappedPlan{}
	packNote := ""
	if policy.AllowPacks {
		family, _, _ := quality.MappedAnimeLabel(anchor.Label)
		key := fmt.Sprintf("%d/%d/%s/%s", seed.MediaID, order.Revision, quality.Normalize(anchor.ReleaseTitle), family)
		if !seen[key] {
			seen[key] = true
			entries := []library.AnimeOrderEntry{}
			for _, entry := range order.Entries {
				f, _, valid := quality.MappedAnimeLabel(entry.Label)
				if valid && f == family && quality.Normalize(entry.ReleaseTitle) == quality.Normalize(anchor.ReleaseTitle) {
					entries = append(entries, entry)
				}
			}
			if len(entries) > 1 {
				if pack, valid := makeMappedPlan(policy, order, entries, eligible); valid {
					plans = append(plans, pack)
				} else {
					packNote = " The complete saved label group cannot form an eligible pack: check gaps, the pack limit, special consent, monitoring, release dates or existing files/reservations."
				}
			}
		}
	}
	plans = append(plans, single)
	search, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	decisions := []quality.Decision{}
	byRelease := map[string]mappedPlan{}
	releases := map[string]transfer.Release{}
	for _, plan := range plans {
		found, err := workflow.SearchAutomaticAnimeOrder(search, seed.MediaID, plan.ids, policy.Revision)
		if err != nil {
			return err
		}
		if len(found) > 200 {
			found = found[:200]
		}
		for _, r := range found {
			if _, exists := byRelease[r.ID]; exists {
				continue
			}
			d := quality.Evaluate(profile, plan.identity, quality.Release{ID: r.ID, Title: r.Title, Size: r.Size, Seeders: r.Seeders, Protocol: r.Protocol, Published: r.Published, Now: s.now().Unix()})
			byRelease[r.ID] = plan
			releases[r.ID] = r
			decisions = append(decisions, d)
		}
	}
	if c.Mode == "download" {
		ready := map[string]bool{}
		for _, name := range []string{"torrent", "usenet"} {
			status, err := s.workflow.Status(ctx, name)
			if err != nil {
				return err
			}
			ready[name] = status.Configured
		}
		for i := range decisions {
			name := "torrent"
			if releases[decisions[i].ID].Protocol == "usenet" {
				name = "usenet"
			}
			if !ready[name] {
				decisions[i].Eligible = false
				decisions[i].Reasons = append(decisions[i].Reasons, "The matching download client is not connected.")
			}
		}
	}
	sort.SliceStable(decisions, func(i, j int) bool {
		a, b := decisions[i], decisions[j]
		if a.Eligible != b.Eligible {
			return a.Eligible
		}
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if len(byRelease[a.ID].ids) != len(byRelease[b.ID].ids) {
			return len(byRelease[a.ID].ids) > len(byRelease[b.ID].ids)
		}
		if a.Title != b.Title {
			return a.Title < b.Title
		}
		return a.ID < b.ID
	})
	outcome, message := "no_match", "No release exactly covers the saved mapped labels and satisfies the quality profile."+packNote
	var downloadID *string
	if len(decisions) > 0 && decisions[0].Eligible {
		best := decisions[0]
		plan := byRelease[best.ID]
		outcome = "match"
		message = fmt.Sprintf("Matching release covers %d mapped episode(s) at order revision %d. File mapping still requires review.", len(plan.ids), plan.order.Revision)
		if c.Mode == "download" {
			if e = s.fence(ctx, c); e != nil {
				return e
			}
			job, created, err := s.workflow.QueueAutomatic(ctx, best.ID, s.mappedGuard(c, plan, profile))
			if errors.Is(err, ErrLease) {
				return err
			}
			if err != nil {
				var typed *transfer.Error
				if errors.As(err, &typed) && !typed.Temporary {
					outcome = "needs_attention"
					message = typed.Message
				} else {
					return err
				}
			} else {
				downloadID = &job.ID
				if created {
					outcome = "queued"
					message = fmt.Sprintf("Queued %d mapped episode(s). Review the completed files in Activity before importing.", len(plan.ids))
					for _, id := range plan.ids {
						queued[id] = true
					}
				} else {
					outcome = "already_tracked"
					message = "This release already has a download record. Review its state in Activity."
				}
			}
		}
	}
	if len(decisions) > 10 {
		decisions = decisions[:10]
	}
	return s.record(ctx, c, seed, outcome, message, decisions, downloadID)
}

func (s *Service) mappedGuard(c claim, plan mappedPlan, profile quality.Profile) transfer.QueueGuard {
	return func(ctx context.Context, tx *sql.Tx, v transfer.Candidate) error {
		var count int
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM search_schedule WHERE id=1 AND revision=? AND lease_owner=? AND lease_until>=? AND mode='download' AND (enabled=1 OR requested=1)`, c.Revision, c.ID, s.now().Unix()).Scan(&count); e != nil {
			return e
		}
		if count != 1 {
			return ErrLease
		}
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM quality_profiles WHERE id=? AND revision=?`, profile.ID, profile.Revision).Scan(&count); e != nil {
			return e
		}
		if count != 1 {
			return ErrLease
		}
		current, e := library.AnimeSearchInTx(ctx, tx, plan.policy.MediaID)
		if e != nil {
			return e
		}
		p := plan.policy
		if current.Mode != "mapped" || current.Revision != p.Revision || current.OrderRevision != p.OrderRevision || current.ApprovedOrderRevision != p.OrderRevision || current.NumberingRevision != p.NumberingRevision || current.CategoryRevision != p.CategoryRevision || current.IncludeSpecials != p.IncludeSpecials || current.AllowPacks != p.AllowPacks || current.MaxPackEpisodes != p.MaxPackEpisodes {
			return ErrLease
		}
		if v.Kind != "series" || v.MovieID != p.MediaID || v.AnimeOrder == nil || v.AnimeSearch == nil || v.AnimeSearch.Mode != "mapped" || v.AnimeSearch.PolicyRevision != p.Revision || len(v.Targets) != len(plan.ids) {
			return ErrLease
		}
		// Compare immutable selected entries without depending on serialization order.
		if v.AnimeOrder.Revision != plan.order.Revision || v.AnimeOrder.NumberingRevision != plan.order.NumberingRevision || v.AnimeOrder.CategoryRevision != plan.order.CategoryRevision || v.AnimeOrder.MediaID != plan.order.MediaID || !reflect.DeepEqual(v.AnimeOrder.Identities, plan.order.Identities) || len(v.AnimeOrder.Entries) != len(plan.order.Entries) {
			return ErrLease
		}
		entries := map[int64]library.AnimeOrderEntry{}
		for _, entry := range plan.order.Entries {
			entries[entry.EpisodeID] = entry
		}
		seen := map[int64]bool{}
		for _, entry := range v.AnimeOrder.Entries {
			if seen[entry.EpisodeID] || entries[entry.EpisodeID] != entry {
				return ErrLease
			}
			seen[entry.EpisodeID] = true
		}
		date := s.now().UTC().Format("2006-01-02")
		targetIDs := map[int64]bool{}
		for _, target := range v.Targets {
			if targetIDs[target.EpisodeID] {
				return ErrLease
			}
			targetIDs[target.EpisodeID] = true
		}
		for _, expected := range plan.targets {
			if expected.EpisodeID == nil || !targetIDs[*expected.EpisodeID] {
				return ErrLease
			}
			fresh, err := scanTarget(tx.QueryRowContext(ctx, targetSQL+` WHERE key=?`, date, 0, expected.Key))
			if errors.Is(err, sql.ErrNoRows) {
				return ErrLease
			}
			if err != nil {
				return err
			}
			if !sameMappedTarget(fresh, expected) {
				return ErrLease
			}
			if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM episode_files WHERE episode_id=?`, *expected.EpisodeID).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				return ErrLease
			}
			if fresh.ProfileID != profile.ID || !p.IncludeSpecials && mappedSpecial(fresh.Season, entries[*expected.EpisodeID].Label) {
				return ErrLease
			}
		}
		if e = library.ValidateAnimeOrderSnapshot(ctx, tx, v.AnimeOrder); e != nil {
			return ErrLease
		}
		d := quality.Evaluate(profile, plan.identity, quality.Release{ID: v.ID, Title: v.Title, Size: v.Size, Seeders: v.Seeders, Protocol: v.Protocol, Published: v.Published, Now: s.now().Unix()})
		if !d.Eligible {
			return ErrLease
		}
		return nil
	}
}
