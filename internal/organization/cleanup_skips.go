package organization

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"
)

// Millisecond timestamps preserve failure/retry ordering within one second.
type CleanupSkipAttempt struct {
	FileID     int64  `json:"file_id"`
	Path       string `json:"path"`
	Message    string `json:"message"`
	OccurredAt int64  `json:"occurred_at"`
	ResolvedBy string `json:"resolved_by,omitempty"`
	ResolvedAt int64  `json:"resolved_at,omitempty"`
}
type CleanupSuccess struct {
	FileID      int64  `json:"file_id"`
	Path        string `json:"path"`
	CompletedAt int64  `json:"completed_at"`
}

func hydrateCleanupSkips(p *Plan) {
	j := p.TrackCleanup
	if j == nil {
		return
	}
	if j.SkipAttempts == 0 {
		j.SkipAttempts = j.Skipped
	}
	if len(j.SkipHistory) == 0 && len(j.Examples) > 0 {
		targets := map[string]cleanupTarget{}
		for _, t := range j.Targets {
			targets[t.Path] = t
		}
		for _, issue := range j.Examples {
			name, message, ok := strings.Cut(issue, ": ")
			if !ok {
				continue
			}
			if t, ok := targets[name]; ok {
				j.SkipHistory = append(j.SkipHistory, CleanupSkipAttempt{FileID: t.ID, Path: t.Path, Message: message, OccurredAt: 0})
			}
		}
	}
	if j.SkipAttempts < len(j.SkipHistory) {
		j.SkipAttempts = len(j.SkipHistory)
	}
	countCleanupSkips(j)
}

func countCleanupSkips(j *TrackCleanup) {
	unresolved := map[string]bool{}
	resolved := map[string]bool{}
	for _, r := range j.SkipHistory {
		key := r.Path
		if r.ResolvedBy == "" {
			unresolved[key] = true
		} else {
			resolved[key] = true
		}
	}
	j.UntrackedSkips = max(0, j.SkipAttempts-len(j.SkipHistory))
	j.Skipped = len(unresolved) + j.UntrackedSkips
	j.Recovered = 0
	for key := range resolved {
		if !unresolved[key] {
			j.Recovered++
		}
	}
}

func cleanupLanguages(languages []string) []string {
	out := []string{}
	for _, language := range languages {
		normalized := trackLanguage(language)
		if normalized == "" {
			normalized = strings.ToLower(language)
		}
		if !slices.Contains(out, normalized) {
			out = append(out, normalized)
		}
	}
	slices.Sort(out)
	return out
}
func sameCleanupPolicy(a, b *TrackCleanup, x, y cleanupTarget) bool {
	action := func(s string) string {
		if s == "" {
			return "remove"
		}
		return s
	}
	if a.Request.Mode != b.Request.Mode || action(a.Request.Action) != action(b.Request.Action) || x.ID != y.ID || x.Path != y.Path {
		return false
	}
	if a.Request.Mode == "all_subtitles" {
		return true
	}
	if !slices.Equal(cleanupLanguages(x.Languages), cleanupLanguages(y.Languages)) {
		return false
	}
	return a.Request.Mode != "other_audio" || trackLanguage(x.Original) == trackLanguage(y.Original)
}

// This is a read projection. It never overwrites a running worker's snapshot.
// Successful outcomes are journaled with the retry's per-file commit, including
// a no-op where all selected tracks already have their desired state.
func reconcileCleanupSkips(ctx context.Context, q query, p *Plan) error {
	hydrateCleanupSkips(p)
	j := p.TrackCleanup
	if j == nil || len(j.SkipHistory) == 0 {
		return nil
	}
	targets := map[int64]cleanupTarget{}
	for _, t := range j.Targets {
		targets[t.ID] = t
	}
	action := j.Request.Action
	if action == "" {
		action = "remove"
	}
	rows, err := q.QueryContext(ctx, `SELECT id,state,snapshot,updated_at FROM organization_plans WHERE id<>? AND updated_at>=? AND json_extract(snapshot,'$.track_cleanup.request.mode')=? AND COALESCE(NULLIF(json_extract(snapshot,'$.track_cleanup.request.action'),''),'remove')=?`, p.ID, p.Created, j.Request.Mode, action)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, state, raw string
		var updated int64
		if err = rows.Scan(&id, &state, &raw, &updated); err != nil {
			return err
		}
		var other Plan
		if err = json.Unmarshal([]byte(raw), &other); err != nil {
			return err
		}
		retry := other.TrackCleanup
		if retry == nil {
			continue
		}
		successes := retry.Successes
		// An older completed batch with no failures proves success for all
		// processed targets. A mixed legacy batch has no per-file evidence.
		if len(successes) == 0 && state == "completed" && retry.SkipAttempts == 0 && len(retry.SkipHistory) == 0 && len(retry.Examples) == 0 && retry.Skipped == 0 && len(retry.Targets) > 0 && retry.Processed == len(retry.Targets) {
			for _, t := range retry.Targets {
				successes = append(successes, CleanupSuccess{FileID: t.ID, Path: t.Path, CompletedAt: updated * 1000})
			}
		}
		retryTargets := map[int64]cleanupTarget{}
		for _, t := range retry.Targets {
			retryTargets[t.ID] = t
		}
		for _, success := range successes {
			original, ok := targets[success.FileID]
			if !ok {
				continue
			}
			retried, ok := retryTargets[success.FileID]
			if !ok || success.Path != retried.Path || !sameCleanupPolicy(j, retry, original, retried) {
				continue
			}
			for i := range j.SkipHistory {
				failure := &j.SkipHistory[i]
				failureAt := failure.OccurredAt
				if failureAt == 0 {
					failureAt = p.Created * 1000
				}
				if failure.ResolvedBy == "" && failure.FileID == success.FileID && failure.Path == success.Path && success.CompletedAt > failureAt {
					failure.ResolvedBy = id
					failure.ResolvedAt = success.CompletedAt
				}
			}
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	countCleanupSkips(j)
	return nil
}

func recordCleanupSuccess(p *Plan) {
	j := p.TrackCleanup
	t := j.Targets[j.Processed]
	for _, failure := range j.SkipHistory {
		if failure.FileID == t.ID && failure.Path == t.Path && failure.ResolvedBy == "" {
			return
		}
	}
	j.Successes = append(j.Successes, CleanupSuccess{FileID: t.ID, Path: t.Path, CompletedAt: time.Now().UnixMilli()})
}
