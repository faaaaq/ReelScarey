package transfer

import (
	"context"
	"encoding/json"
	"time"
)

// Durable feed deferral. The feed check sees each entry once, and a release
// for a monitored episode whose listed air date is still in the future must
// not be lost when the feed rotates the entry out before that date. The
// deferral keeps the sealed release payload captured from the live feed entry;
// at the air date the same reservation, guard fencing and client-submission
// path runs as for a live entry. The sealed payload carries the release
// identity (provider link, size, seeders) and no credential, and only the
// "release" securestore key can open it.

// PersistDeferredFeed seals one feed entry's release payload for a durable
// deferral and consumes its recent-entry token. Target selection is never
// frozen into the payload: the episode target is re-resolved and re-fenced
// from the saved show at wake time.
func (s *Service) PersistDeferredFeed(ctx context.Context, token string) ([]byte, error) {
	v, ok := s.takeRecent(token)
	if !ok {
		return nil, problem("release_expired", "This feed entry expired. Wait for the next release check.")
	}
	v.ID = ""
	v.MovieID = 0
	v.Kind = ""
	v.Targets = nil
	v.AnimeOrder = nil
	v.AnimeSearch = nil
	v.Manual = false
	v.Torrent = nil
	b, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	return s.secrets.Seal("release", b)
}

// QueueDeferredFeed wakes one deferred feed release: the payload captured at
// deferral time is unsealed, the episode target is re-resolved from the saved
// show, and the ordinary reservation, guard fencing and delayed-submission
// path runs exactly as for a live feed entry. The guard still decides: it is
// revalidated inside the reservation transaction after the metainfo fetch.
func (s *Service) QueueDeferredFeed(ctx context.Context, sealed []byte, mediaID, episodeID int64, delay time.Duration, guard QueueGuard) (Job, bool, error) {
	if guard == nil {
		return Job{}, false, problem("invalid_release", "Automatic downloads require a current release-check policy.")
	}
	if mediaID < 1 || episodeID < 1 {
		return Job{}, false, problem("invalid_release", "This deferred release has no valid target.")
	}
	b, e := s.secrets.Open("release", sealed)
	if e != nil {
		return Job{}, false, problem("invalid_release", "The deferred release selection could not be opened.")
	}
	var v Candidate
	if e = json.Unmarshal(b, &v); e != nil {
		return Job{}, false, problem("invalid_release", "The deferred release selection is invalid.")
	}
	// A deferred payload is a plain feed release identity. Anything carrying
	// its own metainfo, an announce source or a frozen target never comes from
	// the deferral path and is refused.
	if v.Announce != nil || v.Manual || v.Kind != "" || len(v.Targets) != 0 || v.MovieID != 0 || v.AnimeOrder != nil || v.AnimeSearch != nil || len(v.Torrent) != 0 {
		return Job{}, false, problem("invalid_release", "The deferred release selection is invalid.")
	}
	v.ID = ID()
	v.MovieID = mediaID
	v.Kind = "series"
	target, e := s.tvTargetForRSS(ctx, mediaID, episodeID)
	if e != nil {
		return Job{}, false, e
	}
	v.Targets = []TVTarget{target}
	b, e = json.Marshal(v)
	if e != nil {
		return Job{}, false, e
	}
	sealedFresh, e := s.secrets.Seal("release", b)
	if e != nil {
		return Job{}, false, e
	}
	now := time.Now().Unix()
	if _, e = s.db.ExecContext(ctx, `INSERT INTO release_candidates(id,movie_id,title,size_bytes,seeders,peers,published_at,info_url,payload,expires_at,scope) VALUES(?,?,?,?,?,?,?,?,?,?,'rss:deferral')`, v.ID, mediaID, v.Title, v.Size, v.Seeders, v.Peers, v.Published, v.InfoURL, sealedFresh, now+1800); e != nil {
		return Job{}, false, e
	}
	return s.queueVersion(ctx, v.ID, guard, 0, "", delay)
}
