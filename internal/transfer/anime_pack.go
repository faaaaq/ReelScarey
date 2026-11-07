package transfer

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/library"
)

// SearchAnimePack searches only an explicitly reviewed canonical selection.
// Provider labels are evidence retained with the selection, never import authority.
func (s *Service) SearchAnimePack(ctx context.Context, seriesID int64, episodeIDs []int64, query string, orderRevision int) ([]Release, error) {
	if seriesID < 1 || orderRevision < 1 || len(episodeIDs) < 1 || len(episodeIDs) > 100 {
		return nil, problem("invalid_anime_selection", "Choose 1–100 distinct mapped Anime episodes and the current order revision.")
	}
	seen := map[int64]bool{}
	ids := append([]int64{}, episodeIDs...)
	for _, id := range ids {
		if id < 1 || seen[id] {
			return nil, problem("invalid_anime_selection", "Choose distinct saved canonical episodes.")
		}
		seen[id] = true
	}
	show, e := library.New(s.db).TVShow(ctx, seriesID)
	if e != nil {
		return nil, e
	}
	if show.Category != "anime" {
		return nil, problem("invalid_anime_selection", "Reviewed order selections apply only to shows in the Anime section.")
	}
	snapshot, e := library.New(s.db).AnimeOrderSnapshot(ctx, seriesID, ids)
	if e != nil {
		return nil, orderError(e)
	}
	if snapshot.Revision != orderRevision {
		return nil, problem("anime_order_changed", "The reviewed Anime order changed. Reload and review the selection again.")
	}
	targets := []TVTarget{}
	for _, id := range ids {
		var target TVTarget
		var loaded string
		var available, busy int
		e = s.db.QueryRowContext(ctx, `SELECT e.id,e.tmdb_id,s.number,e.number,e.title,a.absolute_number,s.loaded_at,EXISTS(SELECT 1 FROM episode_files f WHERE f.episode_id=e.id),EXISTS(SELECT 1 FROM episode_downloads ed JOIN downloads d ON d.id=ed.download_id WHERE ed.episode_id=e.id AND d.state NOT IN ('completed','cancelled')) FROM episodes e JOIN seasons s ON s.id=e.season_id LEFT JOIN anime_episode_numbers a ON a.episode_id=e.id WHERE e.id=? AND s.series_id=?`, id, seriesID).Scan(&target.EpisodeID, &target.TMDbID, &target.Season, &target.Number, &target.Title, &target.AbsoluteNumber, &loaded, &available, &busy)
		if e == sql.ErrNoRows {
			return nil, problem("invalid_anime_selection", "Every selected episode must belong to this saved Anime show.")
		}
		if e != nil {
			return nil, e
		}
		if loaded == "" {
			return nil, problem("episodes_not_loaded", "Load each selected season before searching its reviewed episode order.")
		}
		if available != 0 || busy != 0 {
			return nil, problem("episode_busy", "A selected episode already has a file or active download. Select only missing, unreserved episodes.")
		}
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Season != targets[j].Season {
			return targets[i].Season < targets[j].Season
		}
		return targets[i].Number < targets[j].Number
	})
	if !orderMatchesTargets(&snapshot, seriesID, targets) {
		return nil, problem("invalid_anime_selection", "Save a reviewed order entry for every selected canonical episode.")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		for _, entry := range snapshot.Entries {
			if entry.EpisodeID == ids[0] {
				query = entry.ReleaseTitle
				break
			}
		}
	}
	if !textOK(query, 200) {
		return nil, problem("invalid_search", "Enter a release query with 1–200 characters.")
	}
	candidates, e := s.searchIndexers(ctx, query, "5000")
	if e != nil {
		return nil, e
	}
	if len(candidates) > 200 {
		candidates = candidates[:200]
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = library.ValidateAnimeOrderSnapshot(ctx, tx, &snapshot); e != nil {
		return nil, orderError(e)
	}
	keyIDs := append([]int64{}, ids...)
	sort.Slice(keyIDs, func(i, j int) bool { return keyIDs[i] < keyIDs[j] })
	key, e := json.Marshal(struct {
		IDs      []int64
		Revision int
	}{keyIDs, orderRevision})
	if e != nil {
		return nil, e
	}
	sum := sha256.Sum256(key)
	scope := "animepack:" + hex.EncodeToString(sum[:16])
	now := time.Now().Unix()
	if _, e = tx.ExecContext(ctx, `DELETE FROM release_candidates WHERE id NOT IN (SELECT release_id FROM downloads) AND ((movie_id=? AND scope=?) OR expires_at<?)`, seriesID, scope, now); e != nil {
		return nil, e
	}
	out := []Release{}
	for _, v := range candidates {
		v.ID = ID()
		v.MovieID = seriesID
		v.Kind = "series"
		v.Targets = targets
		v.AnimeSearch = nil
		v.AnimeOrder = &snapshot
		raw, e := json.Marshal(v)
		if e != nil {
			return nil, e
		}
		sealed, e := s.secrets.Seal("release", raw)
		if e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO release_candidates(id,movie_id,title,size_bytes,seeders,peers,published_at,info_url,payload,expires_at,scope) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, v.ID, seriesID, v.Title, v.Size, v.Seeders, v.Peers, v.Published, v.InfoURL, sealed, now+1800, scope); e != nil {
			return nil, e
		}
		out = append(out, v.Release)
	}
	return out, tx.Commit()
}
func orderError(e error) error {
	switch {
	case errors.Is(e, library.ErrAnimeOrderRevision):
		return problem("anime_order_changed", "The reviewed Anime identities, episode order or canonical metadata changed. Reload and review again.")
	case errors.Is(e, library.ErrAnimeOrderInput), errors.Is(e, library.ErrAnimeOrderUnavailable):
		return problem("invalid_anime_selection", e.Error())
	default:
		return e
	}
}

func orderMatchesTargets(snapshot *library.AnimeOrderSnapshot, mediaID int64, targets []TVTarget) bool {
	if snapshot == nil || snapshot.MediaID != mediaID || len(snapshot.Entries) != len(targets) {
		return false
	}
	seen := map[int64]bool{}
	for _, entry := range snapshot.Entries {
		if seen[entry.EpisodeID] {
			return false
		}
		seen[entry.EpisodeID] = true
	}
	for _, target := range targets {
		if !seen[target.EpisodeID] {
			return false
		}
	}
	return true
}
