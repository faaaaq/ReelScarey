package transfer

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sort"
	"time"

	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/quality"
)

// SearchAutomaticAnimeOrder is deliberately separate from permissive manual pack
// searching. The scheduler supplies an exact canonical cohort before any release
// is seen; saved typed order labels define its only eligible release identities.
func (s *Service) SearchAutomaticAnimeOrder(ctx context.Context, seriesID int64, episodeIDs []int64, policyRevision int) ([]Release, error) {
	if seriesID < 1 || policyRevision < 1 || len(episodeIDs) < 1 || len(episodeIDs) > 100 {
		return nil, problem("invalid_anime_selection", "Choose an exact mapped Anime cohort of 1–100 canonical episodes.")
	}
	ids := append([]int64{}, episodeIDs...)
	seen := map[int64]bool{}
	for _, id := range ids {
		if id < 1 || seen[id] {
			return nil, problem("invalid_anime_selection", "Mapped automatic cohorts require distinct canonical episodes.")
		}
		seen[id] = true
	}
	policy, e := library.New(s.db).AnimeSearch(ctx, seriesID)
	if e != nil {
		return nil, e
	}
	if policy.Mode != "mapped" || policy.Revision != policyRevision || policy.Category != "anime" {
		return nil, problem("anime_search_changed", "Review the current mapped Anime search policy before searching.")
	}
	order, e := library.New(s.db).AnimeOrderSnapshot(ctx, seriesID, ids)
	if e != nil {
		return nil, orderError(e)
	}
	a := &AnimeSearchSnapshot{Mode: "mapped", PolicyRevision: policy.Revision, CategoryRevision: policy.CategoryRevision, NumberingRevision: policy.NumberingRevision, CanonicalTitle: policy.CanonicalTitle, AlternateTitles: append([]string{}, policy.AlternateTitles...), OrderRevision: order.Revision, ApprovedOrderRevision: policy.ApprovedOrderRevision, IncludeSpecials: policy.IncludeSpecials, AllowPacks: policy.AllowPacks, MaxPackEpisodes: policy.MaxPackEpisodes}
	targets := []TVTarget{}
	for _, id := range ids {
		var target TVTarget
		e = s.db.QueryRowContext(ctx, `SELECT e.id,e.tmdb_id,s.number,e.number,e.title,a.absolute_number FROM episodes e JOIN seasons s ON s.id=e.season_id LEFT JOIN anime_episode_numbers a ON a.episode_id=e.id WHERE e.id=? AND s.series_id=?`, id, seriesID).Scan(&target.EpisodeID, &target.TMDbID, &target.Season, &target.Number, &target.Title, &target.AbsoluteNumber)
		if e != nil {
			return nil, problem("invalid_anime_selection", "Every mapped cohort member must be a loaded episode of the selected Anime show.")
		}
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Season != targets[j].Season {
			return targets[i].Season < targets[j].Season
		}
		return targets[i].Number < targets[j].Number
	})
	title, labels, e := mappedCohort(&order, targets, a)
	if e != nil {
		return nil, e
	}
	a.SearchTitle = title
	a.Queries = quality.MappedAnimeQueries(title, labels)
	if len(a.Queries) < 1 || len(a.Queries) > 3 {
		return nil, problem("invalid_anime_selection", "This mapped cohort cannot produce an unambiguous bounded release query.")
	}
	for _, query := range a.Queries {
		if !textOK(query, 200) {
			return nil, problem("invalid_anime_search", "Choose a shorter mapped release title so the query fits 200 characters.")
		}
	}
	v := Candidate{Release: Release{MovieID: seriesID}, Kind: "series", Targets: targets, AnimeSearch: a, AnimeOrder: &order}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	if e = validateMappedAnimeSnapshot(ctx, tx, v); e != nil {
		tx.Rollback()
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	search, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	groups := [][]Candidate{}
	for _, query := range a.Queries {
		items, e := s.searchIndexers(search, query, "5000")
		if e != nil {
			return nil, e
		}
		groups = append(groups, items)
	}
	candidates := []Candidate{}
	sources := map[string]bool{}
	for index := 0; index < 200 && len(candidates) < 200; index++ {
		for _, group := range groups {
			if index >= len(group) {
				continue
			}
			item := group[index]
			key := item.IndexerID + "\x00" + item.URL + "\x00" + item.Magnet + "\x00" + item.Hash
			if item.URL == "" && item.Magnet == "" && item.Hash == "" {
				key += "\x00" + item.Title
			}
			if sources[key] {
				continue
			}
			sources[key] = true
			candidates = append(candidates, item)
			if len(candidates) == 200 {
				break
			}
		}
	}
	tx, e = s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = validateMappedAnimeSnapshot(ctx, tx, v); e != nil {
		return nil, e
	}
	keyIDs := append([]int64{}, ids...)
	sort.Slice(keyIDs, func(i, j int) bool { return keyIDs[i] < keyIDs[j] })
	key, _ := json.Marshal(struct {
		IDs           []int64
		Policy, Order int
	}{keyIDs, policyRevision, order.Revision})
	digest := sha256.Sum256(key)
	scope := "animeautoorder:" + hex.EncodeToString(digest[:16])
	now := time.Now().Unix()
	if _, e = tx.ExecContext(ctx, `DELETE FROM release_candidates WHERE id NOT IN (SELECT release_id FROM downloads) AND ((movie_id=? AND scope=?) OR expires_at<?)`, seriesID, scope, now); e != nil {
		return nil, e
	}
	out := []Release{}
	for _, item := range candidates {
		item.ID = ID()
		item.MovieID = seriesID
		item.Kind = "series"
		item.Targets = targets
		item.AnimeOrder = &order
		item.AnimeSearch = a
		raw, e := json.Marshal(item)
		if e != nil {
			return nil, e
		}
		sealed, e := s.secrets.Seal("release", raw)
		if e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO release_candidates(id,movie_id,title,size_bytes,seeders,peers,published_at,payload,expires_at,scope) VALUES(?,?,?,?,?,?,?,?,?,?)`, item.ID, seriesID, item.Title, item.Size, item.Seeders, item.Peers, item.Published, sealed, now+1800, scope); e != nil {
			return nil, e
		}
		out = append(out, item.Release)
	}
	return out, tx.Commit()
}

func mappedSpecialLabel(label string) bool {
	family, _, ok := quality.MappedAnimeLabel(label)
	return ok && (family == "S0" || family == "SP" || family == "OVA" || family == "ONA" || family == "OAD")
}
func mappedCohort(order *library.AnimeOrderSnapshot, targets []TVTarget, a *AnimeSearchSnapshot) (string, []string, error) {
	if order == nil || a == nil || !orderMatchesTargets(order, order.MediaID, targets) || len(targets) < 1 || len(targets) > 100 || a.MaxPackEpisodes < 2 || a.MaxPackEpisodes > 100 || len(targets) > 1 && (!a.AllowPacks || len(targets) > a.MaxPackEpisodes) {
		return "", nil, problem("invalid_anime_selection", "The mapped automatic cohort exceeds the reviewed pack policy.")
	}
	selected := map[int64]TVTarget{}
	for _, target := range targets {
		selected[target.EpisodeID] = target
	}
	title, family := "", ""
	numbers := map[int]bool{}
	labels := []string{}
	ordered := []struct {
		number int
		label  string
	}{}
	for _, entry := range order.Entries {
		target, ok := selected[entry.EpisodeID]
		if !ok {
			return "", nil, problem("invalid_anime_selection", "The mapped order does not cover the exact selected cohort.")
		}
		if !a.IncludeSpecials && (target.Season == 0 || mappedSpecialLabel(entry.Label)) {
			return "", nil, problem("anime_specials_disabled", "Enable mapped specials explicitly before searching this episode automatically.")
		}
		kind, number, ok := quality.MappedAnimeLabel(entry.Label)
		if !ok || numbers[number] {
			return "", nil, problem("invalid_anime_selection", "Mapped automatic cohorts require distinct unambiguous typed release labels.")
		}
		if title == "" {
			title = entry.ReleaseTitle
			family = kind
		} else if quality.Normalize(title) != quality.Normalize(entry.ReleaseTitle) || family != kind {
			return "", nil, problem("invalid_anime_selection", "A mapped automatic cohort must use one release title and typed numbering family.")
		}
		numbers[number] = true
		ordered = append(ordered, struct {
			number int
			label  string
		}{number, entry.Label})
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].number < ordered[j].number })
	for n, entry := range ordered {
		if n > 0 && entry.number != ordered[n-1].number+1 {
			return "", nil, problem("invalid_anime_selection", "Mapped automatic pack labels must form an exact consecutive range.")
		}
		labels = append(labels, entry.label)
	}
	return title, labels, nil
}

func validateMappedAnimeSnapshot(ctx context.Context, tx *sql.Tx, v Candidate) error {
	a := v.AnimeSearch
	if a == nil || a.Mode != "mapped" || v.Kind != "series" || v.AnimeOrder == nil || !orderMatchesTargets(v.AnimeOrder, v.MovieID, v.Targets) {
		return problem("anime_search_changed", "Mapped automatic searching requires both reviewed policy and exact canonical order evidence.")
	}
	policy, e := library.AnimeSearchInTx(ctx, tx, v.MovieID)
	if e != nil {
		return e
	}
	if policy.Category != "anime" || policy.Mode != "mapped" || policy.Revision != a.PolicyRevision || policy.CategoryRevision != a.CategoryRevision || policy.NumberingRevision != a.NumberingRevision || policy.CanonicalTitle != a.CanonicalTitle || !reflect.DeepEqual(policy.AlternateTitles, a.AlternateTitles) || policy.IncludeSpecials != a.IncludeSpecials || policy.AllowPacks != a.AllowPacks || policy.MaxPackEpisodes != a.MaxPackEpisodes || policy.OrderRevision < 1 || policy.ApprovedOrderRevision != policy.OrderRevision || policy.ApprovedOrderRevision != a.ApprovedOrderRevision || policy.OrderRevision != a.OrderRevision || v.AnimeOrder.Revision != a.OrderRevision {
		return problem("anime_search_changed", "Mapped Anime consent, order, flags or canonical metadata changed. Review again before downloading.")
	}
	if e = library.ValidateAnimeOrderSnapshot(ctx, tx, v.AnimeOrder); e != nil {
		return orderError(e)
	}
	title, labels, e := mappedCohort(v.AnimeOrder, v.Targets, a)
	if e != nil {
		return e
	}
	if title != a.SearchTitle || !reflect.DeepEqual(quality.MappedAnimeQueries(title, labels), a.Queries) {
		return problem("anime_search_changed", "The exact mapped Anime query cohort changed. Search again.")
	}
	date := time.Now().UTC().Format("2006-01-02")
	for _, target := range v.Targets {
		var valid int
		e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM episodes e JOIN seasons s ON s.id=e.season_id JOIN media_items m ON m.id=s.series_id WHERE e.id=? AND e.tmdb_id=? AND e.number=? AND s.number=? AND m.id=? AND m.kind='series' AND m.category='anime' AND m.monitored=1 AND s.monitored=1 AND e.monitored=1 AND s.loaded_at<>'' AND length(e.air_date)=10 AND substr(e.air_date,1,4) BETWEEN '0001' AND '9999' AND date(e.air_date,'+0 days')=e.air_date AND e.air_date<=? AND NOT EXISTS(SELECT 1 FROM episode_files f WHERE f.episode_id=e.id) AND NOT EXISTS(SELECT 1 FROM episode_downloads ed JOIN downloads d ON d.id=ed.download_id WHERE ed.episode_id=e.id AND d.state NOT IN ('completed','cancelled'))`, target.EpisodeID, target.TMDbID, target.Number, target.Season, v.MovieID, date).Scan(&valid)
		if e != nil {
			return e
		}
		if valid != 1 {
			return problem("anime_cohort_changed", "Every mapped cohort member must remain loaded, monitored, released, missing and unreserved. No targets were dropped from the reviewed cohort.")
		}
	}
	return nil
}

func validateMappedAnimeRelease(v Candidate) error {
	if v.AnimeSearch == nil || v.AnimeSearch.Mode != "mapped" {
		return nil
	}
	title, labels, e := mappedCohort(v.AnimeOrder, v.Targets, v.AnimeSearch)
	if e != nil {
		return e
	}
	if !quality.Identify(v.Title, quality.Target{TV: true, Anime: true, AnimeMode: "mapped", AnimeOrderTitle: title, AnimeOrderLabels: labels}) {
		return problem("invalid_anime_release", "The release does not identify the exact reviewed mapped episode cohort. Partial, larger or ambiguous packs were not queued.")
	}
	return nil
}
