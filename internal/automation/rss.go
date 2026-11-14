package automation

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"reflect"
	"regexp"
	"strconv"
	"time"

	"github.com/faaaaq/ReelScarey/internal/quality"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

// RSSSettings are the persisted Sonarr/Radarr-style feed-check preferences.
// The check is enabled by default (owner decision, 0.40) and every feed entry
// is grabbed for a monitored missing title without a manual step.
type RSSSettings struct {
	Enabled          bool   `json:"enabled"`
	Interval         int    `json:"interval_minutes"`
	Revision         int    `json:"revision"`
	NextRun          int64  `json:"next_run"`
	Requested        bool   `json:"requested"`
	LastChecked      int64  `json:"last_checked"`
	Checked          int    `json:"checked"`
	Queued           int    `json:"queued"`
	State            string `json:"state"`
	Message          string `json:"message"`
	IndexerConnected bool   `json:"indexer_connected"`
	ClientConnected  bool   `json:"client_connected"`
}

type RSSUpdate struct {
	Enabled  bool `json:"enabled"`
	Interval int  `json:"interval_minutes"`
	Revision int  `json:"revision"`
}

type RSSRun struct {
	ID       string `json:"id"`
	Started  int64  `json:"started_at"`
	Finished int64  `json:"finished_at"`
	State    string `json:"state"`
	Message  string `json:"message"`
	Indexers int    `json:"indexers"`
	Checked  int    `json:"checked"`
	Queued   int    `json:"queued"`
}

type RSSResult struct {
	ID           int64  `json:"id"`
	Indexer      string `json:"indexer"`
	MediaID      *int64 `json:"media_id"`
	EpisodeID    *int64 `json:"episode_id"`
	Target       string `json:"target"`
	ReleaseTitle string `json:"release_title"`
	Outcome      string `json:"outcome"`
	Message      string `json:"message"`
	DownloadID   string `json:"download_id"`
	CreatedAt    int64  `json:"created_at"`
}

type RSSStatus struct {
	RSSSettings
	LastRun *RSSRun     `json:"last_run"`
	Results []RSSResult `json:"results"`
}

const rssColumns = `enabled,interval_minutes,revision,next_run,requested,last_run,checked,queued,state,message`

func scanRSS(row interface{ Scan(...any) error }) (RSSSettings, error) {
	var v RSSSettings
	e := row.Scan(&v.Enabled, &v.Interval, &v.Revision, &v.NextRun, &v.Requested, &v.LastChecked, &v.Checked, &v.Queued, &v.State, &v.Message)
	return v, e
}

func (s *Service) GetRSS(ctx context.Context) (RSSSettings, error) {
	v, e := scanRSS(s.db.QueryRowContext(ctx, `SELECT `+rssColumns+` FROM rss_sync WHERE id=1`))
	if e != nil {
		return v, e
	}
	if s.workflow != nil {
		a, e := s.workflow.Status(ctx, "indexers")
		if e != nil {
			return v, e
		}
		b, e := s.workflow.Status(ctx, "clients")
		if e != nil {
			return v, e
		}
		v.IndexerConnected = a.Configured
		v.ClientConnected = b.Configured
	}
	if (v.Enabled || v.Requested) && !v.IndexerConnected {
		v.State = "waiting_indexer"
		v.Message = "Waiting for an enabled indexer. Add a direct tracker, RSS feed, Torznab or Newznab connection in Indexers."
	} else if (v.Enabled || v.Requested) && !v.ClientConnected {
		v.State = "waiting_client"
		v.Message = "Connect qBittorrent or SABnzbd before feed matches can be grabbed automatically."
	}
	return v, nil
}

func (s *Service) SaveRSS(ctx context.Context, in RSSUpdate) (RSSSettings, error) {
	if in.Interval < 5 || in.Interval > 1440 {
		return RSSSettings{}, invalid("Choose a release-check interval of 5–1440 minutes.")
	}
	state, message := "paused", "Release detection is paused. Existing downloads continue."
	next := int64(0)
	if in.Enabled {
		state = "scheduled"
		message = "Waiting for the next release check."
		next = s.now().Unix()
	}
	r, e := s.db.ExecContext(ctx, `UPDATE rss_sync SET enabled=?,interval_minutes=?,revision=revision+1,next_run=?,requested=0,state=?,message=? WHERE id=1 AND revision=?`, in.Enabled, in.Interval, next, state, message, in.Revision)
	if e != nil {
		return RSSSettings{}, e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return RSSSettings{}, &Error{"revision_conflict", "Release detection settings changed. Refresh before saving."}
	}
	return s.GetRSS(ctx)
}

func (s *Service) RequestRSS(ctx context.Context) error {
	r, e := s.db.ExecContext(ctx, `UPDATE rss_sync SET requested=1,next_run=0,state='scheduled',message='A release check was requested.' WHERE id=1 AND enabled=1 AND requested=0 AND lease_until<?`, s.now().Unix())
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return &Error{"automation_busy", "Release detection is paused or a check is already requested or running."}
	}
	return nil
}

func (s *Service) RSSStatus(ctx context.Context) (RSSStatus, error) {
	v, e := s.GetRSS(ctx)
	if e != nil {
		return RSSStatus{}, e
	}
	out := RSSStatus{RSSSettings: v, Results: []RSSResult{}}
	run, e := s.latestRSSRun(ctx)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	if e == nil {
		out.LastRun = &run
	}
	rows, e := s.db.QueryContext(ctx, `SELECT id,indexer_name,media_id,episode_id,target,release_title,outcome,message,COALESCE(download_id,''),created_at FROM rss_results ORDER BY id DESC LIMIT 25`)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var r RSSResult
		if e = rows.Scan(&r.ID, &r.Indexer, &r.MediaID, &r.EpisodeID, &r.Target, &r.ReleaseTitle, &r.Outcome, &r.Message, &r.DownloadID, &r.CreatedAt); e != nil {
			return out, e
		}
		out.Results = append(out.Results, r)
	}
	return out, rows.Err()
}

func (s *Service) latestRSSRun(ctx context.Context) (RSSRun, error) {
	var v RSSRun
	e := s.db.QueryRowContext(ctx, `SELECT id,started_at,finished_at,state,message,indexers,checked,queued FROM rss_runs ORDER BY started_at DESC,rowid DESC LIMIT 1`).Scan(&v.ID, &v.Started, &v.Finished, &v.State, &v.Message, &v.Indexers, &v.Checked, &v.Queued)
	return v, e
}

type rssClaim struct {
	RSSSettings
	ID string
}

func (s *Service) claimRSS(ctx context.Context) (rssClaim, error) {
	now := s.now().Unix()
	id := transfer.ID()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return rssClaim{}, e
	}
	defer tx.Rollback()
	p, e := scanRSS(tx.QueryRowContext(ctx, `UPDATE rss_sync SET lease_owner=?,lease_until=?,state='running',message='Checking enabled indexers for new releases.' WHERE id=1 AND (enabled=1 OR requested=1) AND next_run<=? AND lease_until<? RETURNING `+rssColumns, id, now+60, now, now))
	if e != nil {
		return rssClaim{}, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE rss_runs SET state='interrupted',finished_at=?,message='An interrupted release check was reclaimed after its lease expired.' WHERE state='running'`, now); e != nil {
		return rssClaim{}, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO rss_runs(id,started_at,state) VALUES(?,?,'running')`, id, now); e != nil {
		return rssClaim{}, e
	}
	if e = tx.Commit(); e != nil {
		return rssClaim{}, e
	}
	return rssClaim{RSSSettings: p, ID: id}, nil
}

func (s *Service) rssFence(ctx context.Context, c rssClaim) error {
	var count int
	e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM rss_sync WHERE id=1 AND revision=? AND lease_owner=? AND lease_until>=? AND (enabled=1 OR requested=1)`, c.Revision, c.ID, s.now().Unix()).Scan(&count)
	if e != nil {
		return e
	}
	if count != 1 {
		return ErrLease
	}
	return nil
}

func (s *Service) RunRSS(ctx context.Context, logger *slog.Logger) {
	for ctx.Err() == nil {
		_, e := s.ProcessRSS(ctx)
		if e != nil && ctx.Err() == nil {
			logger.Warn("release detection deferred", "error", e)
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

// errRSSWaitingClient keeps feed entries unseen while no download client can
// accept them, so connecting a client does not require re-fetching lost entries.
var errRSSWaitingClient = errors.New("release detection is waiting for a download client")

func (s *Service) ProcessRSS(ctx context.Context) (bool, error) {
	c, e := s.claimRSS(ctx)
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
				r, e := s.db.ExecContext(work, `UPDATE rss_sync SET lease_until=? WHERE id=1 AND lease_owner=? AND revision=? AND lease_until>=?`, s.now().Unix()+60, c.ID, c.Revision, s.now().Unix())
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
	checked, queued, failures, attempted, e := s.rssPass(work, c)
	cancel()
	<-done

	finish, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	state := "ready"
	message := "Release check complete. Monitored missing titles were checked against every enabled feed."
	if errors.Is(e, errRSSWaitingClient) {
		state = "waiting_client"
		message = "Waiting for a usable download client before feed matches can be grabbed."
	} else if e != nil && !errors.Is(e, ErrLease) && !errors.Is(e, context.Canceled) {
		var typed *transfer.Error
		if errors.As(e, &typed) && typed.Code == "indexer_not_configured" {
			state = "waiting_indexer"
			message = typed.Message
		} else {
			state = "attention"
			message = "The release check hit a storage error. Saved feeds and downloads were preserved."
		}
	}
	if ctx.Err() != nil || errors.Is(e, ErrLease) || errors.Is(e, context.Canceled) {
		state = "interrupted"
		message = "Release check stopped; settings changed or the application is restarting."
	} else if failures > 0 {
		state = "attention"
		if failures == attempted {
			message = "No enabled indexer answered the release check. The next check will try again."
		} else {
			message = "Some indexers did not answer the release check. The other feeds were checked."
		}
	}
	next := s.now().Unix() + int64(c.Interval)*60
	requestedOut := false
	var pending int
	_ = s.db.QueryRowContext(finish, `SELECT requested FROM rss_sync WHERE id=1`).Scan(&pending)
	if !c.Requested && pending == 1 {
		requestedOut = true
		next = s.now().Unix()
	}
	if state == "interrupted" {
		next = s.now().Unix()
	}
	tx, err := s.db.BeginTx(finish, nil)
	if err != nil {
		return true, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(finish, `UPDATE rss_runs SET finished_at=?,state=?,message=?,indexers=?,checked=?,queued=? WHERE id=? AND state='running'`, s.now().Unix(), state, message, attempted, checked, queued, c.ID); err != nil {
		return true, err
	}
	if _, err = tx.ExecContext(finish, `UPDATE rss_sync SET next_run=?,requested=CASE WHEN ? THEN 0 ELSE requested END,last_run=?,checked=?,queued=?,state=?,message=?,lease_owner='',lease_until=0 WHERE id=1 AND lease_owner=? AND revision=?`, next, boolInt(c.Requested), s.now().Unix(), checked, queued, state, message, c.ID, c.Revision); err != nil {
		return true, err
	}
	if requestedOut {
		if _, err = tx.ExecContext(finish, `UPDATE rss_sync SET requested=1,next_run=? WHERE id=1`, s.now().Unix()); err != nil {
			return true, err
		}
	}
	if _, err = tx.ExecContext(finish, `UPDATE rss_sync SET lease_owner='',lease_until=0 WHERE id=1 AND lease_owner=?`, c.ID); err != nil {
		return true, err
	}
	if _, err = tx.ExecContext(finish, `DELETE FROM rss_seen WHERE first_seen<?`, s.now().Unix()-180*24*3600); err != nil {
		return true, err
	}
	if _, err = tx.ExecContext(finish, `DELETE FROM rss_results WHERE id NOT IN (SELECT id FROM rss_results ORDER BY id DESC LIMIT 2000)`); err != nil {
		return true, err
	}
	if _, err = tx.ExecContext(finish, `DELETE FROM rss_runs WHERE id NOT IN (SELECT id FROM rss_runs ORDER BY started_at DESC,rowid DESC LIMIT 100)`); err != nil {
		return true, err
	}
	return true, tx.Commit()
}

type rssEpisodeKey struct{ season, episode int }

// futureRef builds the not-yet-aired target index at most once per feed pass.
type futureRef struct {
	service *Service
	present rssIndex
	once    bool
	idx     rssIndex
	err     error
}

func (f *futureRef) get(ctx context.Context) (rssIndex, error) {
	if !f.once {
		f.once = true
		f.idx, f.err = f.service.futureIndex(ctx, f.present, rssTargetLimit)
	}
	return f.idx, f.err
}

type rssIndex struct {
	byEpisode    map[rssEpisodeKey][]searchTarget
	byYear       map[int][]searchTarget
	movieFiles   map[int64][]int
	episodeFiles map[int64]bool
}

var rssYear = regexp.MustCompile(`(?:^| )(?:19|20)[0-9]{2}(?: |$)`)

func yearTokens(title string) []int {
	normalized := " " + quality.Normalize(title) + " "
	var out []int
	for _, token := range rssYear.FindAllString(normalized, -1) {
		n, e := strconv.Atoi(trimSpaces(token))
		if e == nil {
			out = append(out, n)
		}
	}
	return out
}

func trimSpaces(s string) string {
	for len(s) > 0 && s[0] == ' ' {
		s = s[1:]
	}
	for len(s) > 0 && s[len(s)-1] == ' ' {
		s = s[:len(s)-1]
	}
	return s
}

// match maps one feed title to saved targets without scanning the library:
// episodes are indexed by SxxEyy and movies by their four-digit year, then the
// existing exact identity rules decide.
func (idx *rssIndex) match(title string) []searchTarget {
	if quality.HasEpisodeLabel(title) {
		season, episode, _, _, ok := quality.EpisodeLabel(title)
		if !ok {
			// Packs and ranges stay manual, matching the automatic-search rule.
			return nil
		}
		candidates := idx.byEpisode[rssEpisodeKey{season, episode}]
		out := []searchTarget{}
		for _, t := range candidates {
			if quality.Identify(title, t.Target) {
				out = append(out, t)
			}
		}
		return out
	}
	out := []searchTarget{}
	for _, year := range yearTokens(title) {
		for _, t := range idx.byYear[year] {
			if quality.Identify(title, t.Target) {
				out = append(out, t)
			}
		}
	}
	return out
}

func (idx *rssIndex) hasFile(t searchTarget) bool {
	if t.TV {
		return t.EpisodeID != nil && idx.episodeFiles[*t.EpisodeID]
	}
	resolutions, exists := idx.movieFiles[t.MediaID]
	if !exists {
		return false
	}
	if t.MovieVersion == "" {
		return true
	}
	for _, res := range resolutions {
		if res == 0 || quality.MovieVersionMatches(t.MovieVersion, res) {
			return true
		}
	}
	return false
}

// rssTargetLimit bounds one feed pass. A library with more missing targets is
// still covered by the scheduled cursor search and later feed checks.
const rssTargetLimit = 20000

func (s *Service) rssTargets(ctx context.Context) (rssIndex, error) {
	targets, e := s.targets(ctx, "", rssTargetLimit, "", "", 0, "")
	if e != nil {
		return rssIndex{}, e
	}
	idx := rssIndex{
		byEpisode:    map[rssEpisodeKey][]searchTarget{},
		byYear:       map[int][]searchTarget{},
		movieFiles:   map[int64][]int{},
		episodeFiles: map[int64]bool{},
	}
	for _, t := range targets {
		if t.TV {
			idx.byEpisode[rssEpisodeKey{t.Season, t.Episode}] = append(idx.byEpisode[rssEpisodeKey{t.Season, t.Episode}], t)
		} else if t.Year != nil {
			idx.byYear[*t.Year] = append(idx.byYear[*t.Year], t)
		}
	}
	rows, e := s.db.QueryContext(ctx, `SELECT movie_id,video_resolution FROM media_files WHERE movie_id IS NOT NULL`)
	if e != nil {
		return idx, e
	}
	for rows.Next() {
		var movie int64
		var res int
		if e = rows.Scan(&movie, &res); e != nil {
			rows.Close()
			return idx, e
		}
		idx.movieFiles[movie] = append(idx.movieFiles[movie], res)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return idx, e
	}
	rows, e = s.db.QueryContext(ctx, `SELECT episode_id FROM episode_files`)
	if e != nil {
		return idx, e
	}
	for rows.Next() {
		var episode int64
		if e = rows.Scan(&episode); e != nil {
			rows.Close()
			return idx, e
		}
		idx.episodeFiles[episode] = true
	}
	return idx, rows.Close()
}

// futureIndex builds the not-yet-aired companion of rssTargets: the identical
// eligibility query with the air-date window inverted, so an unmatched feed
// entry or announce can be told apart from a genuine title mismatch when the
// only failing condition is that the episode has not aired yet. The window
// contains future-dated TV episodes only — movie targets are skipped here
// because a movie has no date gate and can never be deferred on one. The
// registered file maps are shared with the present index so hasFile stays
// consistent.
func (s *Service) futureIndex(ctx context.Context, present rssIndex, limit int) (rssIndex, error) {
	date := s.now().Format("2006-01-02")
	rows, e := s.db.QueryContext(ctx, `SELECT * FROM (`+futureTargetSQL+`) WHERE 1=1 ORDER BY key LIMIT ?`, date, 1, limit)
	if e != nil {
		return rssIndex{}, e
	}
	idx := rssIndex{
		byEpisode:    map[rssEpisodeKey][]searchTarget{},
		byYear:       map[int][]searchTarget{},
		movieFiles:   present.movieFiles,
		episodeFiles: present.episodeFiles,
	}
	for rows.Next() {
		t, e := scanTarget(rows)
		if e != nil {
			rows.Close()
			return rssIndex{}, e
		}
		if !t.TV {
			// Movie targets carry no date and are never deferred.
			continue
		}
		idx.byEpisode[rssEpisodeKey{t.Season, t.Episode}] = append(idx.byEpisode[rssEpisodeKey{t.Season, t.Episode}], t)
	}
	return idx, rows.Err()
}

func (s *Service) rssSeen(ctx context.Context, indexer, key string) (bool, error) {
	var n int
	e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM rss_seen WHERE indexer_id=? AND release_key=?`, indexer, key).Scan(&n)
	return n > 0, e
}

func (s *Service) markRSSSeen(ctx context.Context, indexer, key string) error {
	_, e := s.db.ExecContext(ctx, `INSERT INTO rss_seen(indexer_id,release_key,first_seen) VALUES(?,?,?) ON CONFLICT(indexer_id,release_key) DO NOTHING`, indexer, key, s.now().Unix())
	return e
}

func (s *Service) recordRSS(ctx context.Context, c rssClaim, src transfer.RecentFeed, item transfer.RecentItem, t searchTarget, outcome, message, downloadID string) error {
	var media, episode any
	target := item.Title
	if t.MediaID > 0 {
		media = t.MediaID
		if t.EpisodeID != nil {
			episode = *t.EpisodeID
		}
		target = targetName(t)
	}
	var download any
	if downloadID != "" {
		download = downloadID
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO rss_results(run_id,indexer_id,indexer_name,media_id,episode_id,target,release_title,outcome,message,download_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, c.ID, src.IndexerID, src.IndexerName, media, episode, target, item.Title, outcome, message, download, s.now().Unix()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE rss_runs SET checked=checked+1,queued=queued+? WHERE id=?`, boolInt(outcome == "queued"), c.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) rssPass(ctx context.Context, c rssClaim) (checked, queued, failures, attempted int, err error) {
	if s.workflow == nil {
		return 0, 0, 0, 0, nil
	}
	client, e := s.workflow.Status(ctx, "clients")
	if e != nil {
		return 0, 0, 0, 0, e
	}
	if !client.Configured {
		return 0, 0, 0, 0, errRSSWaitingClient
	}
	feeds, e := s.workflow.Recent(ctx)
	if e != nil {
		return 0, 0, 0, 0, e
	}
	idx, e := s.rssTargets(ctx)
	if e != nil {
		return 0, 0, 0, 0, e
	}
	// The not-yet-aired companion index is built lazily: only a pass that
	// meets an unmatched entry pays for the extra query, and one build serves
	// the whole pass.
	future := futureRef{service: s, present: idx}
	profiles := map[int64]quality.Profile{}
	store := quality.New(s.db)
	for _, src := range feeds {
		attempted++
		if src.Err != nil {
			failures++
			continue
		}
		for _, item := range src.Items {
			if e = s.rssFence(ctx, c); e != nil {
				return checked, queued, failures, attempted, e
			}
			seen, e := s.rssSeen(ctx, src.IndexerID, item.Key)
			if e != nil {
				return checked, queued, failures, attempted, e
			}
			if seen {
				continue
			}
			checked++
			q, retry, e := s.handleRSS(ctx, c, src, item, idx, &future, store, profiles)
			if errors.Is(e, ErrLease) {
				if fence := s.rssFence(ctx, c); fence != nil {
					return checked, queued, failures, attempted, fence
				}
				// The target changed but the check is still current: skip it
				// rather than aborting every other feed.
				retry, e = false, nil
			}
			if e != nil {
				return checked, queued, failures, attempted, e
			}
			if q {
				queued++
			}
			if !retry {
				if e = s.markRSSSeen(ctx, src.IndexerID, item.Key); e != nil {
					return checked, queued, failures, attempted, e
				}
			}
		}
	}
	return checked, queued, failures, attempted, nil
}

// handleRSS returns (queued, retry, error). retry keeps the feed entry unseen
// so a lost lease or a temporary client failure can be tried again.
func (s *Service) handleRSS(ctx context.Context, c rssClaim, src transfer.RecentFeed, item transfer.RecentItem, idx rssIndex, future *futureRef, store *quality.Store, profiles map[int64]quality.Profile) (bool, bool, error) {
	matches := idx.match(item.Title)
	if len(matches) == 0 {
		// An entry can fail to match today because its monitored episode has
		// not aired yet. Record that explicitly instead of silently skipping
		// it: feed entries are seen once, so this row is the owner's only
		// trace of why an early release was not grabbed automatically.
		if f, e := future.get(ctx); e != nil {
			return false, false, e
		} else if fm := f.match(item.Title); len(fm) > 0 {
			distinct := map[int64]bool{}
			notAired := []searchTarget{}
			for _, t := range fm {
				if t.Anime || t.Season <= 0 || idx.hasFile(t) {
					continue
				}
				distinct[t.MediaID] = true
				notAired = append(notAired, t)
			}
			if len(distinct) > 1 {
				if e := s.recordRSS(ctx, c, src, item, searchTarget{}, "ambiguous", "This release matches more than one monitored future episode, so it was not grabbed automatically.", ""); e != nil {
					return false, false, e
				}
			} else if len(notAired) == 1 {
				// The same honesty rule as the announce path: record the
				// deferral only when the release would actually satisfy the
				// target's saved profile; otherwise the profile rejection is
				// the owner-visible outcome.
				t := notAired[0]
				p, ok := profiles[t.ProfileID]
				if !ok {
					p, e = store.Get(ctx, t.ProfileID)
					if e != nil {
						return false, false, e
					}
					profiles[t.ProfileID] = p
				}
				d := quality.Evaluate(p, t.Target, quality.Release{ID: item.Key, Title: item.Title, Size: item.Size, Seeders: item.Seeders, Protocol: item.Protocol, Published: item.Published, Now: s.now().Unix()})
				if !d.Eligible {
					if e := s.recordRSS(ctx, c, src, item, searchTarget{}, "rejected", "No feed candidate satisfies this title's quality profile, seed, size or identity rules.", ""); e != nil {
						return false, false, e
					}
				} else if e := s.recordRSS(ctx, c, src, item, searchTarget{}, "not_aired", "The release matches a monitored episode whose listed air date has not arrived. Automatic grabbing for that episode begins on its air date.", ""); e != nil {
					return false, false, e
				}
			}
		}
		return false, false, nil
	}
	var best *searchTarget
	var bestProfile quality.Profile
	bestScore, bestQuality := -1, 0
	evaluated := false
	for i := range matches {
		t := matches[i]
		if idx.hasFile(t) {
			// Feed checking never replaces registered media. The title remains
			// eligible for a reviewed manual replacement instead.
			continue
		}
		evaluated = true
		p, ok := profiles[t.ProfileID]
		if !ok {
			var e error
			p, e = store.Get(ctx, t.ProfileID)
			if e != nil {
				return false, false, e
			}
			profiles[t.ProfileID] = p
		}
		d := quality.Evaluate(p, t.Target, quality.Release{ID: item.Key, Title: item.Title, Size: item.Size, Seeders: item.Seeders, Protocol: item.Protocol, Published: item.Published, Now: s.now().Unix()})
		if !d.Eligible {
			continue
		}
		if best == nil || d.Score > bestScore || (d.Score == bestScore && d.Quality > bestQuality) {
			copy := t
			best = &copy
			bestProfile = p
			bestScore, bestQuality = d.Score, d.Quality
		}
	}
	if best == nil {
		if !evaluated {
			// Every matching target already has a registered file. Upgrades are
			// suggestions, not automatic replacements.
			return false, false, nil
		}
		// The title matched but no candidate passed the saved profile. Keep one
		// honest row so the owner can see why a feed entry was not grabbed.
		if e := s.recordRSS(ctx, c, src, item, searchTarget{}, "rejected", "No feed candidate satisfies this title's quality profile, seed, size or identity rules.", ""); e != nil {
			return false, false, e
		}
		return false, false, nil
	}
	job, created, e := s.workflow.QueueCandidateAfter(ctx, item.Token, best.MediaID, best.EpisodeID, best.MovieVersion, bestProfile.GrabDelay(), s.rssGuard(c, *best, bestProfile))
	if e != nil {
		var typed *transfer.Error
		if errors.As(e, &typed) && typed.Temporary {
			if recErr := s.recordRSS(ctx, c, src, item, *best, "attention", typed.Message, ""); recErr != nil {
				return false, true, recErr
			}
			return false, true, nil
		}
		if errors.Is(e, ErrLease) {
			return false, true, e
		}
		if recErr := s.recordRSS(ctx, c, src, item, *best, "needs_attention", transferMessage(e), ""); recErr != nil {
			return false, true, recErr
		}
		return false, false, nil
	}
	outcome := "queued"
	message := "Matching release queued automatically. Review TV episode files in Activity when needed."
	if !created {
		outcome = "already_tracked"
		message = "This release already has a download record. Review its state in Activity."
	}
	if e = s.recordRSS(ctx, c, src, item, *best, outcome, message, job.ID); e != nil {
		return false, false, e
	}
	return created, false, nil
}

func transferMessage(e error) string {
	var typed *transfer.Error
	if errors.As(e, &typed) && typed.Message != "" {
		return typed.Message
	}
	return "The release could not be queued. Its original source was not removed."
}

func (s *Service) rssGuard(c rssClaim, t searchTarget, p quality.Profile) transfer.QueueGuard {
	return func(ctx context.Context, tx *sql.Tx, v transfer.Candidate) error {
		now := s.now().Unix()
		var count int
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM rss_sync WHERE id=1 AND revision=? AND lease_owner=? AND lease_until>=? AND (enabled=1 OR requested=1)`, c.Revision, c.ID, now).Scan(&count); e != nil {
			return e
		}
		if count != 1 {
			return ErrLease
		}
		date := s.now().Format("2006-01-02")
		current, e := scanTarget(tx.QueryRowContext(ctx, targetSQL+` WHERE key=?`, date, 1, t.Key))
		if errors.Is(e, sql.ErrNoRows) {
			return ErrLease
		}
		if e != nil {
			return e
		}
		if current.VersionRevision != t.VersionRevision || current.ProfileID != t.ProfileID || current.AssignmentRevision != t.AssignmentRevision || current.AnimeRevision != t.AnimeRevision || current.CategoryRevision != t.CategoryRevision || current.NumberingRevision != t.NumberingRevision || current.AnimeSearchTitle != t.AnimeSearchTitle || !reflect.DeepEqual(current.Target, t.Target) {
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
		// Feed checking never replaces registered media. Recheck immediately
		// inside the reservation transaction, after metainfo retrieval.
		if t.TV {
			if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM episode_files WHERE episode_id=?`, *t.EpisodeID).Scan(&count); e != nil {
				return e
			}
		} else {
			if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_files WHERE movie_id=? AND (?='' OR video_resolution=0 OR (?='hd' AND video_resolution BETWEEN 1 AND 1080) OR (?='uhd' AND video_resolution=2160))`, t.MediaID, t.MovieVersion, t.MovieVersion, t.MovieVersion).Scan(&count); e != nil {
				return e
			}
		}
		if count != 0 {
			return ErrLease
		}
		d := quality.Evaluate(p, current.Target, quality.Release{ID: v.ID, Title: v.Title, Size: v.Size, Seeders: v.Seeders, Protocol: v.Protocol, Published: v.Published, Now: s.now().Unix()})
		if !d.Eligible {
			return ErrLease
		}
		return nil
	}
}
