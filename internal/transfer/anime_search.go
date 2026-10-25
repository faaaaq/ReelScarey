package transfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/quality"
	"reflect"
	"time"
)

// AnimeSearchSnapshot is audit evidence captured with a searched candidate and
// queued job. Import identity remains the immutable canonical download_targets.
type AnimeSearchSnapshot struct {
	Mode              string   `json:"mode"`
	SearchTitle       string   `json:"search_title"`
	CanonicalTitle    string   `json:"canonical_title"`
	AlternateTitles   []string `json:"alternate_titles"`
	PolicyRevision    int      `json:"policy_revision"`
	CategoryRevision  int      `json:"category_revision"`
	NumberingRevision int64    `json:"numbering_revision"`
	AbsoluteNumber    *int     `json:"absolute_number,omitempty"`
	Queries           []string `json:"queries"`
}

func (s *Service) SearchAnimeAutomatic(ctx context.Context, seriesID int64, season, episode int, includeAvailable bool) ([]Release, error) {
	p, e := library.New(s.db).AnimeSearch(ctx, seriesID)
	if e != nil {
		return nil, e
	}
	if p.Category != "anime" || p.Mode == "disabled" {
		return nil, problem("anime_search_disabled", "Automatic Anime searching is disabled for this show.")
	}
	title, ok := library.AnimeQueryTitle(p.CanonicalTitle, p.SearchTitle, p.AlternateTitles)
	if !ok {
		return nil, problem("anime_search_changed", "Review this show's selected Anime search title.")
	}
	snap := &AnimeSearchSnapshot{Mode: p.Mode, SearchTitle: title, CanonicalTitle: p.CanonicalTitle, AlternateTitles: p.AlternateTitles, PolicyRevision: p.Revision, CategoryRevision: p.CategoryRevision, NumberingRevision: p.NumberingRevision}
	return s.searchTV(ctx, seriesID, season, episode, "", includeAvailable, snap)
}
func (s *Service) animeCandidates(ctx context.Context, snap *AnimeSearchSnapshot, season, episode int, targets []TVTarget) ([]Candidate, error) {
	if len(targets) != 1 {
		return nil, problem("invalid_anime_search", "Automatic Anime searches require one exact canonical episode.")
	}
	snap.AbsoluteNumber = targets[0].AbsoluteNumber
	t := quality.Target{Title: snap.CanonicalTitle, Season: season, Episode: episode, TV: true, Anime: true, AnimeMode: snap.Mode, AbsoluteNumber: snap.AbsoluteNumber, AlternateTitles: snap.AlternateTitles}
	snap.Queries = quality.AnimeQueries(t, snap.SearchTitle)
	if len(snap.Queries) == 0 {
		return nil, problem("anime_numbering_required", "Save an explicit absolute number before searching this episode automatically.")
	}
	search, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	groups := [][]Candidate{}
	for _, query := range snap.Queries {
		if !textOK(query, 200) {
			return nil, problem("invalid_anime_search", "Choose a shorter Anime search title so the episode query fits 200 characters.")
		}
		items, e := s.searchIndexers(search, query, "5000")
		if e != nil {
			return nil, e
		}
		groups = append(groups, items)
	}
	// Interleave bounded responses so padded forms are not crowded out by the
	// first query. Dedupe source selections without exposing private URLs.
	out := []Candidate{}
	seen := map[string]bool{}
	for n := 0; n < 200 && len(out) < 200; n++ {
		for _, g := range groups {
			if n >= len(g) {
				continue
			}
			v := g[n]
			key := v.IndexerID + "\x00" + v.URL + "\x00" + v.Magnet + "\x00" + v.Hash
			if v.URL == "" && v.Magnet == "" && v.Hash == "" {
				key += "\x00" + v.Title
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, v)
			if len(out) == 200 {
				break
			}
		}
	}
	return out, nil
}
func validateAnimeSnapshot(ctx context.Context, tx *sql.Tx, v Candidate) error {
	if v.AnimeSearch == nil {
		return nil
	}
	a := v.AnimeSearch
	var category, title, mode, selected, raw string
	var catRev, revision int
	var numberRev int64
	e := tx.QueryRowContext(ctx, `SELECT m.category,m.title,m.category_revision,d.revision,COALESCE(a.mode,'disabled'),COALESCE(a.search_title,''),COALESCE(a.alternate_titles,'[]'),COALESCE(a.revision,0) FROM media_items m JOIN series_details d ON d.media_id=m.id LEFT JOIN anime_search_settings a ON a.media_id=m.id WHERE m.id=?`, v.MovieID).Scan(&category, &title, &catRev, &numberRev, &mode, &selected, &raw, &revision)
	if e != nil {
		return e
	}
	aliases := []string{}
	if e = json.Unmarshal([]byte(raw), &aliases); e != nil {
		return e
	}
	queryTitle, ok := library.AnimeQueryTitle(title, selected, aliases)
	if !ok || category != "anime" || mode == "disabled" || mode != a.Mode || title != a.CanonicalTitle || catRev != a.CategoryRevision || numberRev != a.NumberingRevision || revision != a.PolicyRevision || queryTitle != a.SearchTitle || !reflect.DeepEqual(aliases, a.AlternateTitles) || len(v.Targets) != 1 {
		return problem("anime_search_changed", "Anime search settings or numbering changed. Search again before downloading.")
	}
	return nil
}
