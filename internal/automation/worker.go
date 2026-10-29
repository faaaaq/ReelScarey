package automation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/library"
	"log/slog"
	"reflect"
	"time"

	"github.com/faaaaq/ReelScarey/internal/quality"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

type searchTarget struct {
	quality.Target
	Key                string
	MediaID            int64
	EpisodeID          *int64
	Probe              string
	ProfileID          int64
	AssignmentRevision int
	AnimeRevision      int
	AnimeSearchTitle   string
	CategoryRevision   int
	NumberingRevision  int64
}

// Only released, effectively monitored items with no active reservation qualify.
// TV metadata is loaded explicitly by the owner; the scheduler does not invent it.
const targetSQL = `SELECT key,media_id,episode_id,title,year,season,episode,probe,profile_id,assignment_revision,category,anime_mode,alternate_titles,absolute_number,anime_revision,anime_search_title,category_revision,numbering_revision,rule_codec,rule_group,rule_revision FROM (
 SELECT printf('m:%020d',m.id) AS key,m.id AS media_id,NULL AS episode_id,m.title,m.year,0 AS season,0 AS episode,COALESCE(f.probe_json,'') AS probe,COALESCE(a.profile_id,ss.default_profile_id) AS profile_id,COALESCE(a.revision,0) AS assignment_revision,m.category,'' AS anime_mode,'[]' AS alternate_titles,NULL AS absolute_number,0 AS anime_revision,'' AS anime_search_title,m.category_revision,0 AS numbering_revision,'' AS rule_codec,'' AS rule_group,0 AS rule_revision
 FROM media_items m CROSS JOIN search_schedule ss LEFT JOIN item_automation a ON a.media_id=m.id LEFT JOIN media_files f ON f.movie_id=m.id
 WHERE m.kind='movie' AND m.monitored=1 AND length(m.release_date)=10 AND substr(m.release_date,1,4) BETWEEN '0001' AND '9999' AND date(m.release_date,'+0 days')=m.release_date AND m.release_date<=? AND NOT EXISTS(SELECT 1 FROM downloads d WHERE d.movie_id=m.id AND d.kind='movie' AND d.state NOT IN ('completed','cancelled'))
 UNION ALL
 SELECT printf('e:%020d',e.id),m.id,e.id,m.title,m.year,s.number,e.number,COALESCE(f.probe_json,''),COALESCE(a.profile_id,ss.default_profile_id),COALESCE(a.revision,0),m.category,COALESCE(ans.mode,'disabled'),COALESCE(ans.alternate_titles,'[]'),an.absolute_number,COALESCE(ans.revision,0),COALESCE(ans.search_title,''),m.category_revision,d.revision,COALESCE(rr.codec,''),COALESCE(rr.release_group,''),COALESCE(rr.revision,0)
 FROM episodes e JOIN seasons s ON s.id=e.season_id JOIN media_items m ON m.id=s.series_id JOIN series_details d ON d.media_id=m.id CROSS JOIN search_schedule ss LEFT JOIN item_automation a ON a.media_id=m.id LEFT JOIN episode_files ef ON ef.episode_id=e.id LEFT JOIN media_files f ON f.id=ef.media_file_id LEFT JOIN anime_search_settings ans ON ans.media_id=m.id LEFT JOIN anime_episode_numbers an ON an.episode_id=e.id LEFT JOIN series_release_rules rr ON rr.media_id=m.id
 WHERE m.monitored=1 AND (m.category='standard' OR (m.category='anime' AND ((s.number>0 AND ans.mode IN ('season','absolute')) OR (ans.mode='mapped' AND (s.number>0 OR ans.include_specials=1))))) AND s.monitored=1 AND e.monitored=1 AND s.loaded_at<>'' AND length(e.air_date)=10 AND substr(e.air_date,1,4) BETWEEN '0001' AND '9999' AND date(e.air_date,'+0 days')=e.air_date AND e.air_date<=? AND NOT EXISTS(SELECT 1 FROM episode_downloads ed JOIN downloads d ON d.id=ed.download_id WHERE ed.episode_id=e.id AND d.state NOT IN ('completed','cancelled'))
)`

func scanTarget(row interface{ Scan(...any) error }) (searchTarget, error) {
	var t searchTarget
	var category, aliases string
	e := row.Scan(&t.Key, &t.MediaID, &t.EpisodeID, &t.Title, &t.Year, &t.Season, &t.Episode, &t.Probe, &t.ProfileID, &t.AssignmentRevision, &category, &t.AnimeMode, &aliases, &t.AbsoluteNumber, &t.AnimeRevision, &t.AnimeSearchTitle, &t.CategoryRevision, &t.NumberingRevision, &t.SeriesCodec, &t.SeriesGroup, &t.SeriesRulesRevision)
	t.TV = t.EpisodeID != nil
	t.Anime = t.TV && category == "anime"
	if e == nil {
		e = json.Unmarshal([]byte(aliases), &t.AlternateTitles)
	}
	return t, e
}
func (s *Service) targets(ctx context.Context, cursor string, limit int) ([]searchTarget, error) {
	date := s.now().UTC().Format("2006-01-02")
	out := []searchTarget{}
	for pass := 0; pass < 2 && len(out) < limit; pass++ {
		suffix := ` WHERE key>? ORDER BY key LIMIT ?`
		bound := cursor
		if pass == 1 {
			if cursor == "" {
				break
			}
			suffix = ` WHERE key<=? ORDER BY key LIMIT ?`
		}
		rows, e := s.db.QueryContext(ctx, targetSQL+suffix, date, date, bound, limit-len(out))
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			t, e := scanTarget(rows)
			if e != nil {
				rows.Close()
				return nil, e
			}
			out = append(out, t)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
	}
	return out, nil
}

type claim struct {
	Schedule
	ID, Cursor string
}

func (s *Service) claim(ctx context.Context) (claim, error) {
	now := s.now().Unix()
	id := transfer.ID()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return claim{}, e
	}
	defer tx.Rollback()
	var cursor string
	e = tx.QueryRowContext(ctx, `UPDATE search_schedule SET lease_owner=?,lease_until=?,state='running',message='Searching monitored library items.' WHERE id=1 AND (enabled=1 OR requested=1) AND next_run<=? AND lease_until<? RETURNING cursor`, id, now+60, now, now).Scan(&cursor)
	if e != nil {
		return claim{}, e
	}
	v, e := scanSchedule(tx.QueryRowContext(ctx, `SELECT `+scheduleColumns+` FROM search_schedule WHERE id=1`))
	if e != nil {
		return claim{}, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE automation_runs SET state='interrupted',finished_at=?,message='An interrupted search was reclaimed after its lease expired.' WHERE state='running'`, now); e != nil {
		return claim{}, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO automation_runs(id,started_at,state,mode) VALUES(?,?,'running',?)`, id, now, v.Mode); e != nil {
		return claim{}, e
	}
	if e = tx.Commit(); e != nil {
		return claim{}, e
	}
	return claim{v, id, cursor}, nil
}
func (s *Service) fence(ctx context.Context, c claim) error {
	var count int
	e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM search_schedule WHERE id=1 AND revision=? AND lease_owner=? AND lease_until>=?`, c.Revision, c.ID, s.now().Unix()).Scan(&count)
	if e != nil {
		return e
	}
	if count != 1 {
		return ErrLease
	}
	return nil
}
func (s *Service) guard(c claim, t searchTarget, p quality.Profile) transfer.QueueGuard {
	return func(ctx context.Context, tx *sql.Tx, v transfer.Candidate) error {
		now := s.now().Unix()
		var count int
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM search_schedule WHERE id=1 AND revision=? AND lease_owner=? AND lease_until>=? AND mode='download' AND (enabled=1 OR requested=1)`, c.Revision, c.ID, now).Scan(&count); e != nil {
			return e
		}
		if count != 1 {
			return ErrLease
		}
		date := s.now().UTC().Format("2006-01-02")
		current, e := scanTarget(tx.QueryRowContext(ctx, targetSQL+` WHERE key=?`, date, date, t.Key))
		if errors.Is(e, sql.ErrNoRows) {
			return ErrLease
		}
		if e != nil {
			return e
		}
		if current.ProfileID != t.ProfileID || current.AssignmentRevision != t.AssignmentRevision || current.AnimeRevision != t.AnimeRevision || current.CategoryRevision != t.CategoryRevision || current.NumberingRevision != t.NumberingRevision || !reflect.DeepEqual(current.Target, t.Target) || current.AnimeSearchTitle != t.AnimeSearchTitle {
			return ErrLease
		}
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM quality_profiles WHERE id=? AND revision=?`, p.ID, p.Revision).Scan(&count); e != nil {
			return e
		}
		if count != 1 {
			return ErrLease
		}
		if v.MovieID != t.MediaID || (t.TV && (v.Kind != "series" || len(v.Targets) != 1 || v.Targets[0].EpisodeID != *t.EpisodeID)) || (!t.TV && v.Kind == "series") {
			return ErrLease
		}
		if t.Anime && (v.AnimeSearch == nil || v.AnimeSearch.PolicyRevision != t.AnimeRevision) {
			return ErrLease
		}
		d := quality.Evaluate(p, current.Target, quality.Release{ID: v.ID, Title: v.Title, Size: v.Size, Seeders: v.Seeders, Protocol: v.Protocol, Published: v.Published, Now: s.now().Unix()})
		if !d.Eligible {
			return ErrLease
		}
		return nil
	}
}
func (s *Service) Run(ctx context.Context, logger *slog.Logger) {
	for ctx.Err() == nil {
		_, e := s.ProcessNext(ctx)
		if e != nil && ctx.Err() == nil {
			logger.Warn("scheduled search deferred")
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
	c, e := s.claim(ctx)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
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
				r, e := s.db.ExecContext(work, `UPDATE search_schedule SET lease_until=? WHERE id=1 AND lease_owner=? AND revision=? AND lease_until>=?`, s.now().Unix()+60, c.ID, c.Revision, s.now().Unix())
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
	state, message := "completed", "Search complete. No matching monitored items need attention."
	state, message, e = s.process(work, c)
	cancel()
	<-done
	finish, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if ctx.Err() != nil || errors.Is(e, ErrLease) || errors.Is(e, context.Canceled) {
		state = "interrupted"
		message = "Search stopped; settings changed or the application is restarting."
	}
	next := s.now().Unix() + int64(c.Interval)*60
	if state == "interrupted" {
		next = s.now().Unix()
	}
	attempts := 0
	if e != nil && state != "interrupted" {
		state = "retrying"
		message = "A provider or storage request failed. The search will retry with backoff."
		var old int
		_ = s.db.QueryRowContext(finish, `SELECT attempts FROM search_schedule WHERE id=1`).Scan(&old)
		attempts = old + 1
		if attempts < 5 {
			delay := int64(60) * (1 << min(attempts-1, 6))
			next = s.now().Unix() + delay
		} else {
			state = "failed"
			message = "Search retries exhausted. Check integrations or request a new search."
			attempts = 0
		}
	}
	tx, err := s.db.BeginTx(finish, nil)
	if err != nil {
		return true, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(finish, `UPDATE automation_runs SET finished_at=?,state=?,message=? WHERE id=? AND state='running'`, s.now().Unix(), state, message, c.ID); err != nil {
		return true, err
	}
	requested := state == "retrying" || (state == "interrupted" && c.Requested)
	if _, err = tx.ExecContext(finish, `UPDATE search_schedule SET next_run=?,requested=?,attempts=?,state=?,message=?,lease_owner='',lease_until=0 WHERE id=1 AND lease_owner=? AND revision=?`, next, requested, attempts, state, message, c.ID, c.Revision); err != nil {
		return true, err
	}
	// A changed schedule keeps its new intent, but releases this worker's lease.
	if _, err = tx.ExecContext(finish, `UPDATE search_schedule SET lease_owner='',lease_until=0 WHERE id=1 AND lease_owner=?`, c.ID); err != nil {
		return true, err
	}
	if _, err = tx.ExecContext(finish, `DELETE FROM automation_runs WHERE id NOT IN (SELECT id FROM automation_runs ORDER BY started_at DESC,rowid DESC LIMIT 100) AND state<>'running'`); err != nil {
		return true, err
	}
	return true, tx.Commit()
}
func (s *Service) process(ctx context.Context, c claim) (string, string, error) {
	if s.workflow == nil {
		return "waiting_indexer", "Waiting for a search provider.", nil
	}
	indexer, e := s.workflow.Status(ctx, "indexers")
	if e != nil {
		return "", "", e
	}
	if !indexer.Configured {
		return "waiting_indexer", "Waiting for an enabled indexer. Add a direct tracker, Torznab or Newznab connection in Indexers.", nil
	}
	client, e := s.workflow.Status(ctx, "clients")
	if e != nil {
		return "", "", e
	}
	if c.Mode == "download" && !client.Configured {
		return "waiting_client", "Connect qBittorrent or SABnzbd before automatic downloads can start.", nil
	}
	targets, e := s.targets(ctx, c.Cursor, c.Batch)
	if e != nil {
		return "", "", e
	}
	mappedSeen := map[string]bool{}
	mappedQueued := map[int64]bool{}
	for _, t := range targets {
		if e = s.fence(ctx, c); e != nil {
			return "", "", e
		}
		p, e := quality.New(s.db).Get(ctx, t.ProfileID)
		if e != nil {
			return "", "", e
		}
		if t.Anime && t.AnimeMode == "mapped" {
			if t.EpisodeID != nil && mappedQueued[*t.EpisodeID] {
				if e = s.record(ctx, c, t, "already_tracked", "This episode was included in a mapped pack queued earlier in this run.", nil, nil); e != nil {
					return "", "", e
				}
				continue
			}
			if e = s.processMapped(ctx, c, t, p, mappedSeen, mappedQueued); e != nil {
				return "", "", e
			}
			continue
		}
		if t.Anime && t.AnimeMode == "absolute" && t.AbsoluteNumber == nil {
			if e = s.record(ctx, c, t, "needs_numbering", "Save an explicit absolute episode number in this show's Library section before automatic searching.", nil, nil); e != nil {
				return "", "", e
			}
			continue
		}
		if t.Anime {
			if _, ok := library.AnimeQueryTitle(t.Title, t.AnimeSearchTitle, t.AlternateTitles); !ok {
				if e = s.record(ctx, c, t, "needs_attention", "The selected Anime query title changed. Review this show's automatic search settings.", nil, nil); e != nil {
					return "", "", e
				}
				continue
			}
		}
		var inspected struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		}
		current := 0
		if t.Probe != "" {
			_ = json.Unmarshal([]byte(t.Probe), &inspected)
			current = quality.VerifiedQuality(inspected.Width, inspected.Height)
			if current == 0 || current >= p.Cutoff {
				outcome := "cutoff_met"
				reason := "Verified file meets the resolution cutoff."
				if current == 0 {
					outcome = "quality_unknown"
					reason = "Existing file quality is unknown; automatic upgrade selection is skipped."
				}
				if e = s.record(ctx, c, t, outcome, reason, nil, nil); e != nil {
					return "", "", e
				}
				continue
			}
		}
		var releases []transfer.Release
		if t.Anime {
			releases, e = s.workflow.SearchAnimeAutomatic(ctx, t.MediaID, t.Season, t.Episode, current > 0)
		} else if t.TV && current > 0 {
			releases, e = s.workflow.SearchTVUpgrade(ctx, t.MediaID, t.Season, t.Episode)
		} else if t.TV {
			releases, e = s.workflow.SearchTV(ctx, t.MediaID, t.Season, t.Episode, "")
		} else {
			releases, e = s.workflow.Search(ctx, t.MediaID, "")
		}
		if e != nil {
			return "", "", e
		}
		candidates := make([]quality.Release, 0, len(releases))
		for _, r := range releases {
			candidates = append(candidates, quality.Release{ID: r.ID, Title: r.Title, Size: r.Size, Seeders: r.Seeders, Protocol: r.Protocol, Published: r.Published, Now: s.now().Unix()})
		}
		decisions := quality.Rank(p, t.Target, candidates)
		if c.Mode == "download" && current == 0 {
			ready := map[string]bool{}
			for _, name := range []string{"qbittorrent", "sabnzbd"} {
				status, err := s.workflow.Status(ctx, name)
				if err != nil {
					return "", "", err
				}
				ready[name] = status.Configured
			}
			for i := range decisions {
				for _, r := range releases {
					if r.ID != decisions[i].ID {
						continue
					}
					name := "qbittorrent"
					if r.Protocol == "usenet" {
						name = "sabnzbd"
					}
					if !ready[name] {
						decisions[i].Eligible = false
						decisions[i].Reasons = append(decisions[i].Reasons, "The matching download client is not connected.")
					}
					break
				}
			}
		}
		outcome, message := "no_match", "No release satisfies the profile and exact identity rules."
		var downloadID *string
		var best *quality.Decision
		for i := range decisions {
			if decisions[i].Eligible && (current == 0 || decisions[i].Quality > current) {
				best = &decisions[i]
				break
			}
		}
		if best != nil {
			outcome = "match"
			message = "Matching release found. Open release search to review a fresh selection."
			if current > 0 {
				outcome = "upgrade_available"
				message = "A higher resolution is available. Your existing file is preserved; replacement imports are not available yet."
			} else if c.Mode == "download" {
				if e = s.fence(ctx, c); e != nil {
					return "", "", e
				}
				job, created, qe := s.workflow.QueueAutomatic(ctx, best.ID, s.guard(c, t, p))
				if errors.Is(qe, ErrLease) {
					return "", "", qe
				}
				if qe != nil {
					var typed *transfer.Error
					if errors.As(qe, &typed) && !typed.Temporary {
						outcome = "needs_attention"
						message = typed.Message
					} else {
						return "", "", qe
					}
				} else {
					downloadID = &job.ID
					if created {
						outcome = "queued"
						message = "Download queued. Review TV episode files in Activity before importing."
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
		if e = s.record(ctx, c, t, outcome, message, decisions, downloadID); e != nil {
			return "", "", e
		}
	}
	return "completed", "Search complete. Results include matching releases and any items needing attention.", nil
}
func (s *Service) record(ctx context.Context, c claim, t searchTarget, outcome, message string, decisions []quality.Decision, downloadID *string) error {
	if decisions == nil {
		decisions = []quality.Decision{}
	}
	b, e := json.Marshal(decisions)
	if e != nil {
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var count int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM search_schedule WHERE id=1 AND lease_owner=? AND revision=? AND lease_until>=?`, c.ID, c.Revision, s.now().Unix()).Scan(&count); e != nil {
		return e
	}
	if count != 1 {
		return ErrLease
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO automation_results(run_id,media_id,episode_id,target,outcome,message,decisions,download_id) VALUES(?,?,?,?,?,?,?,?)`, c.ID, t.MediaID, t.EpisodeID, targetName(t), outcome, message, string(b), downloadID); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE automation_runs SET checked=checked+1,queued=queued+? WHERE id=?`, boolInt(outcome == "queued"), c.ID); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE search_schedule SET cursor=? WHERE id=1 AND lease_owner=?`, t.Key, c.ID); e != nil {
		return e
	}
	return tx.Commit()
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func (s *Service) Results(ctx context.Context, id string) ([]Result, error) {
	var exists string
	if e := s.db.QueryRowContext(ctx, `SELECT id FROM automation_runs WHERE id=?`, id).Scan(&exists); e != nil {
		return nil, e
	}
	out := []Result{}
	rows, e := s.db.QueryContext(ctx, `SELECT r.id,r.media_id,r.episode_id,r.target,r.outcome,r.message,r.decisions,r.download_id,m.kind,COALESCE(s.number,0),COALESCE(e.number,0) FROM automation_results r JOIN media_items m ON m.id=r.media_id LEFT JOIN episodes e ON e.id=r.episode_id LEFT JOIN seasons s ON s.id=e.season_id WHERE r.run_id=? ORDER BY r.id LIMIT 25`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var v Result
		var raw string
		if e = rows.Scan(&v.ID, &v.MediaID, &v.EpisodeID, &v.Target, &v.Outcome, &v.Message, &raw, &v.DownloadID, &v.Kind, &v.Season, &v.Episode); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(raw), &v.Decisions); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
