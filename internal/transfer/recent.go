package transfer

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// RecentFeed is one indexer's newest releases. Err is a safe provider message;
// one failing feed does not prevent the others from being checked.
type RecentFeed struct {
	IndexerID   string
	IndexerName string
	Items       []RecentItem
	Err         error
}

// RecentItem is the sanitized part of a feed entry. Token is an opaque,
// short-lived handle used to queue this exact release; download URLs and
// credentials never cross this boundary.
type RecentItem struct {
	Key       string
	Token     string
	Title     string
	Size      int64
	Seeders   *int
	Peers     *int
	Protocol  string
	Published string
}

const recentTTL = 30 * time.Minute
const recentLimit = 4000

type recentOffer struct {
	candidate Candidate
	expires   time.Time
}

func (s *Service) enabledIndexerConfigs(ctx context.Context) ([]IndexerConfig, error) {
	statuses, e := s.Indexers(ctx)
	if e != nil {
		return nil, e
	}
	configs := []IndexerConfig{}
	for _, r := range statuses {
		if !r.Enabled {
			continue
		}
		var c IndexerConfig
		if r.Legacy {
			c, e = s.indexerConfig(ctx)
			c.Name = r.Name
		} else {
			var rec indexerRecord
			rec, e = s.managedIndexer(ctx, r.ID)
			c = rec.config
		}
		if e != nil {
			return nil, e
		}
		configs = append(configs, c)
	}
	return configs, nil
}

func recentKey(c IndexerConfig, v Candidate) string {
	identity := v.Guid
	if identity == "" {
		identity = v.Hash
	}
	if identity == "" {
		identity = v.Magnet
	}
	if identity == "" {
		identity = v.URL
	}
	sum := sha256.Sum256([]byte(c.ID + "\x00" + identity + "\x00" + v.Title))
	return hex.EncodeToString(sum[:])
}

func (s *Service) rememberRecent(v Candidate) string {
	token := ID()
	s.recentMu.Lock()
	if s.recent == nil {
		s.recent = map[string]recentOffer{}
	}
	now := time.Now()
	for key, offer := range s.recent {
		if offer.expires.Before(now) {
			delete(s.recent, key)
		}
	}
	if len(s.recent) >= recentLimit {
		// Feed checks are periodic; dropping the oldest handles is safer than
		// growing an unbounded in-memory map when a provider returns a huge feed.
		for key := range s.recent {
			delete(s.recent, key)
			if len(s.recent) < recentLimit/2 {
				break
			}
		}
	}
	s.recent[token] = recentOffer{candidate: v, expires: now.Add(recentTTL)}
	s.recentMu.Unlock()
	return token
}

func (s *Service) takeRecent(token string) (Candidate, bool) {
	s.recentMu.Lock()
	defer s.recentMu.Unlock()
	offer, ok := s.recent[token]
	if !ok || offer.expires.Before(time.Now()) {
		delete(s.recent, token)
		return Candidate{}, false
	}
	delete(s.recent, token)
	return offer.candidate, true
}

// Recent fetches every enabled indexer's newest releases. The result keeps
// provider errors per indexer so the caller can report partial failures.
func (s *Service) Recent(ctx context.Context) ([]RecentFeed, error) {
	configs, e := s.enabledIndexerConfigs(ctx)
	if e != nil {
		return nil, e
	}
	if len(configs) == 0 {
		return nil, problem("indexer_not_configured", "Add and enable an indexer in Indexers first.")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	feeds := make([]RecentFeed, len(configs))
	seen := make([]map[string]bool, len(configs))
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	for i, c := range configs {
		feeds[i].IndexerID = c.ID
		feeds[i].IndexerName = c.Name
		seen[i] = map[string]bool{}
		wg.Add(1)
		go func(i int, c IndexerConfig) {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				feeds[i].Err = unavailable("The release check timed out.")
				return
			}
			a := s.adapter(c)
			if a == nil {
				feeds[i].Err = problem("invalid_settings", "This adapter is unavailable.")
				return
			}
			items, err := a.Recent(ctx, c)
			if err != nil {
				feeds[i].Err = err
				return
			}
			protocol := "torrent"
			if c.Kind == "newznab" {
				protocol = "usenet"
			}
			for _, v := range items {
				key := recentKey(c, v)
				if seen[i][key] {
					continue
				}
				seen[i][key] = true
				feeds[i].Items = append(feeds[i].Items, RecentItem{
					Key:       key,
					Token:     s.rememberRecent(v),
					Title:     v.Title,
					Size:      v.Size,
					Seeders:   v.Seeders,
					Peers:     v.Peers,
					Protocol:  protocol,
					Published: v.Published,
				})
				if len(feeds[i].Items) == 200 {
					break
				}
			}
		}(i, c)
	}
	wg.Wait()
	return feeds, nil
}

// QueueCandidate queues one feed entry for an already matched target. It
// persists the same encrypted release selection used by the interactive flow so
// reservations, client submission and imports stay identical.
func (s *Service) QueueCandidate(ctx context.Context, token string, mediaID int64, episodeID *int64, version string, guard QueueGuard) (Job, bool, error) {
	return s.queueCandidate(ctx, token, mediaID, episodeID, version, 0, guard)
}

// QueueCandidateAfter applies the profile's grab delay to a feed entry. The
// candidate is still persisted immediately so policy fencing and reservations
// remain identical; only the first submission is postponed, inside the same
// reservation transaction as the queued row.
func (s *Service) QueueCandidateAfter(ctx context.Context, token string, mediaID int64, episodeID *int64, version string, delay time.Duration, guard QueueGuard) (Job, bool, error) {
	return s.queueCandidate(ctx, token, mediaID, episodeID, version, delay, guard)
}

func (s *Service) queueCandidate(ctx context.Context, token string, mediaID int64, episodeID *int64, version string, delay time.Duration, guard QueueGuard) (Job, bool, error) {
	if guard == nil {
		return Job{}, false, problem("invalid_release", "Automatic downloads require a current release-check policy.")
	}
	if mediaID < 1 || (version != "" && version != "hd" && version != "uhd") {
		return Job{}, false, problem("invalid_release", "This feed entry has no valid target.")
	}
	v, ok := s.takeRecent(token)
	if !ok {
		return Job{}, false, problem("release_expired", "This feed entry expired. Wait for the next release check.")
	}
	v.ID = ID()
	v.MovieID = mediaID
	v.Kind = ""
	v.Targets = nil
	if episodeID != nil {
		if version != "" {
			return Job{}, false, problem("invalid_version", "Movie versions cannot target TV episodes.")
		}
		target, e := s.tvTargetForRSS(ctx, mediaID, *episodeID)
		if e != nil {
			return Job{}, false, e
		}
		v.Kind = "series"
		v.Targets = []TVTarget{target}
	}
	b, e := json.Marshal(v)
	if e != nil {
		return Job{}, false, e
	}
	sealed, e := s.secrets.Seal("release", b)
	if e != nil {
		return Job{}, false, e
	}
	now := time.Now().Unix()
	if _, e = s.db.ExecContext(ctx, `INSERT INTO release_candidates(id,movie_id,title,size_bytes,seeders,peers,published_at,info_url,payload,expires_at,scope) VALUES(?,?,?,?,?,?,?,?,?,?,'rss')`, v.ID, mediaID, v.Title, v.Size, v.Seeders, v.Peers, v.Published, v.InfoURL, sealed, now+1800); e != nil {
		return Job{}, false, e
	}
	return s.queueVersion(ctx, v.ID, guard, 0, version, delay)
}

func (s *Service) tvTargetForRSS(ctx context.Context, seriesID, episodeID int64) (TVTarget, error) {
	var t TVTarget
	e := s.db.QueryRowContext(ctx, `SELECT e.id,e.tmdb_id,s.number,e.number,e.title,a.absolute_number FROM episodes e JOIN seasons s ON s.id=e.season_id LEFT JOIN anime_episode_numbers a ON a.episode_id=e.id WHERE e.id=? AND s.series_id=?`, episodeID, seriesID).Scan(&t.EpisodeID, &t.TMDbID, &t.Season, &t.Number, &t.Title, &t.AbsoluteNumber)
	if errors.Is(e, sql.ErrNoRows) {
		return t, problem("not_found", "This episode is no longer in the saved show.")
	}
	return t, e
}
