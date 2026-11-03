package subtitle

import (
	"context"
	"database/sql"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/organization"
	"log/slog"
	"slices"
	"time"
)

func (s *Service) Run(ctx context.Context, logger *slog.Logger) {
	for ctx.Err() == nil {
		processed, e := s.ProcessNext(ctx)
		if e != nil && ctx.Err() == nil {
			logger.Warn("subtitle job deferred")
		}
		if processed && e == nil {
			continue
		}
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (s *Service) ProcessNext(ctx context.Context) (bool, error) {
	if processed, e := s.ProcessEdit(ctx); processed || e != nil {
		return processed, e
	}
	defer organization.ReadMedia()()
	if e := organization.EnsureMediaAvailable(ctx, s.db); e != nil {
		return false, e
	}
	p, e := s.Preferences(ctx)
	if e != nil {
		return false, e
	}
	if e = s.discover(ctx, p); e != nil {
		return false, e
	}
	j, e := s.claim(ctx)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	p, e = s.filePreferences(ctx, j.MediaFileID)
	if e != nil {
		return true, e
	}
	work, cancel := context.WithTimeout(ctx, 8*time.Minute)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-work.Done():
				return
			case <-ticker.C:
				r, e := s.db.ExecContext(work, `UPDATE subtitle_jobs SET lease_until=? WHERE id=? AND lease_owner=? AND lease_until>=?`, s.now().Unix()+60, j.ID, j.owner, s.now().Unix())
				if e != nil {
					cancel()
					return
				}
				n, _ := r.RowsAffected()
				if n != 1 {
					cancel()
					return
				}
			}
		}
	}()
	state, message, err := s.process(work, j, p)
	cancel()
	<-done
	finish, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	next := s.now().Unix() + int64(p.Interval)*60
	requested := false
	attempts := 0
	if ctx.Err() != nil || errors.Is(err, ErrLease) || errors.Is(err, context.Canceled) {
		state = "queued"
		message = "Subtitle work interrupted; current preferences will be checked before resuming."
		next = s.now().Unix()
		requested = j.Requested
	} else if err != nil {
		state = "blocked"
		message = "Subtitle work needs attention. Check provider and media access, then retry."
		var typed *Error
		if errors.As(err, &typed) {
			message = typed.Message
			if typed.Code == "provider_not_configured" || typed.Code == "provider_auth" || typed.Code == "provider_access" || typed.Code == "provider_quota" || typed.Code == "provider_rate_limit" {
				state = "waiting_provider"
				next = max(next, typed.RetryAt)
				requested = j.Requested
			} else if typed.Temporary {
				attempts = j.Attempts + 1
				if attempts < 5 {
					state = "retrying"
					next = max(s.now().Unix()+int64(60)*(1<<min(attempts-1, 6)), typed.RetryAt)
					requested = j.Requested
				} else {
					state = "blocked"
					message = "Subtitle retries exhausted. Check the connection or storage, then retry."
				}
			}
		}
	}
	r, e := s.db.ExecContext(finish, `UPDATE subtitle_jobs SET state=?,message=?,attempts=?,next_run=?,requested=?,lease_owner='',lease_until=0,updated_at=? WHERE id=? AND lease_owner=? AND revision=? AND (SELECT revision FROM subtitle_preferences WHERE id=1)=?`, state, message, attempts, next, requested, s.now().Unix(), j.ID, j.owner, j.Revision, j.Revision)
	if e != nil {
		return true, e
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		_, e = s.db.ExecContext(finish, `UPDATE subtitle_jobs SET lease_owner='',lease_until=0 WHERE id=? AND lease_owner=?`, j.ID, j.owner)
	}
	return true, e
}
func (s *Service) process(ctx context.Context, j Job, p Preferences) (string, string, error) {
	m, e := s.media(ctx, j.MediaFileID)
	if e != nil {
		return "", "", e
	}
	o, e := s.openMedia(ctx, m)
	if e != nil {
		return "", "", e
	}
	defer o.Close()
	if e = s.cleanupCommitted(ctx, m, o); e != nil {
		return "", "", e
	}
	m.MovieHash = o.moviehash
	detected, e := detectExisting(ctx, m, o, p)
	if e != nil {
		return "", "", e
	}
	if e = s.saveDetected(ctx, j, m, detected); e != nil {
		return "", "", e
	}
	missing := []Track{}
	unverified := false
	for _, t := range p.Tracks {
		if v, ok := detected.tracks[t]; ok {
			if v.State != "available" {
				unverified = true
			}
			continue
		}
		missing = append(missing, t)
	}
	pendingTracks := map[Track]*importPlan{}
	if j.Mode != "review" {
		for _, track := range p.Tracks {
			pending, pe := s.pending(ctx, m.ID, track)
			if pe != nil {
				return "", "", pe
			}
			if pending != nil {
				pendingTracks[track] = pending
			}
		}
	}
	if len(missing) == 0 && len(pendingTracks) == 0 {
		if unverified {
			return "needs_review", "Existing subtitle files need review and were preserved.", nil
		}
		return "completed", "Every requested subtitle track is already available.", nil
	}
	if j.Mode == "automatic" && detected.unknown {
		return "needs_review", "An existing sidecar has an unknown language. Review it before automatic subtitle downloads.", nil
	}
	c, e := s.credentials(ctx)
	if e != nil {
		return "", "", e
	}
	if e = s.fence(ctx, nil, j, c.Generation, true); e != nil {
		return "", "", e
	}
	// Recover journaled bytes before making any provider request or consuming quota.
	for _, track := range p.Tracks {
		if pending := pendingTracks[track]; pending != nil {
			if e = s.publish(ctx, j, m, o, c.Generation, *pending); e != nil {
				return "", "", e
			}
			detected.tracks[track] = SavedTrack{Track: track, Source: "downloaded", State: "available"}
		}
	}
	missing = slices.DeleteFunc(missing, func(t Track) bool { v, ok := detected.tracks[t]; return ok && v.State == "available" })
	if len(missing) == 0 {
		return "completed", "Requested subtitles are available; interrupted imports recovered.", nil
	}
	if e = s.providerReady(ctx, c.Generation); e != nil {
		return "", "", e
	}
	var candidates []Candidate
	if j.Mode == "download" {
		candidate, e := s.selected(ctx, j, p, c, m)
		if e != nil {
			return "", "", e
		}
		candidates = []Candidate{candidate}
	} else {
		langs := []string{}
		for _, track := range missing {
			if !slices.Contains(langs, track.Language) {
				langs = append(langs, track.Language)
			}
		}
		raw, e := s.selectedProvider(c).Search(ctx, c, m.Target, langs)
		if e != nil {
			_ = s.providerError(ctx, c.Generation, e)
			return "", "", e
		}
		if e = s.fence(ctx, nil, j, c.Generation, true); e != nil {
			return "", "", e
		}
		candidates = rank(p, m.Target, raw)
		candidates, e = s.storeCandidates(ctx, m, p, c, candidates)
		if e != nil {
			return "", "", e
		}
	}
	if j.Mode == "review" {
		if len(candidates) == 0 {
			return "no_match", "No subtitle matches were returned. Existing files were preserved.", nil
		}
		return "needs_review", "Review subtitle language, track type and timing before choosing a download.", nil
	}
	count := 0
	review := false
	for _, track := range missing {
		var chosen *Candidate
		for i := range candidates {
			v := &candidates[i]
			if v.Track == track && v.CanDownload && (j.Mode == "download" || v.Automatic) {
				chosen = v
				break
			}
			if v.Track == track && v.CanDownload {
				review = true
			}
		}
		if chosen == nil {
			continue
		}
		if e = s.fence(ctx, nil, j, c.Generation, true); e != nil {
			return "", "", e
		}
		if e = s.providerReady(ctx, c.Generation); e != nil {
			return "", "", e
		}
		download, e := s.selectedProvider(c).Fetch(ctx, c, *chosen)
		if e != nil {
			_ = s.providerError(ctx, c.Generation, e)
			return "", "", e
		}
		if e = s.quota(ctx, c, download); e != nil {
			return "", "", e
		}
		content, e := normalizeProviderSubtitle(download.Content, chosen.Format, o.probe.Duration)
		if e != nil {
			return "", "", e
		}
		if e = o.unchanged(ctx, m); e != nil {
			return "", "", e
		}
		if e = s.fence(ctx, nil, j, c.Generation, true); e != nil {
			return "", "", e
		}
		plan, e := s.plan(ctx, m, *chosen, content)
		if e != nil {
			return "", "", e
		}
		if e = s.publish(ctx, j, m, o, c.Generation, plan); e != nil {
			return "", "", e
		}
		count++
	}
	if count == len(missing) && !unverified {
		return "completed", "Requested subtitles were downloaded and saved without replacing existing files.", nil
	}
	if count > 0 {
		return "needs_review", "Matching subtitles were saved. Some requested tracks are still missing or need review.", nil
	}
	if review || unverified {
		return "needs_review", "Only uncertain timing matches or unverified existing tracks were found. Review before downloading.", nil
	}
	return "no_match", "No safe automatic match was found for the requested language and track types.", nil
}
func (s *Service) saveDetected(ctx context.Context, j Job, m media, d detection) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = s.fence(ctx, tx, j, "", false); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE subtitles SET state='missing',message='Not found during the latest check.',updated_at=? WHERE media_file_id=?`, s.now().Unix(), m.ID); e != nil {
		return e
	}
	for t, v := range d.tracks {
		if _, e = tx.ExecContext(ctx, `INSERT INTO subtitles(media_file_id,language,variant,source,relative_path,state,message,updated_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(media_file_id,language,variant) DO UPDATE SET provider=CASE WHEN subtitles.source='downloaded' AND subtitles.relative_path=excluded.relative_path THEN subtitles.provider ELSE '' END,source=CASE WHEN subtitles.source='downloaded' AND subtitles.relative_path=excluded.relative_path THEN 'downloaded' ELSE excluded.source END,relative_path=excluded.relative_path,state=excluded.state,message=excluded.message,updated_at=excluded.updated_at`, m.ID, t.Language, t.Variant, v.Source, v.Path, v.State, v.Message, s.now().Unix()); e != nil {
			return e
		}
	}
	return tx.Commit()
}
