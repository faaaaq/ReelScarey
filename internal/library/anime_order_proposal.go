package library

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/faaaaq/ReelScarey/internal/metadata"
)

type AnimeOrderGroup struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Order        int    `json:"order"`
	EpisodeCount int    `json:"episode_count"`
}
type AnimeOrderUnmatched struct {
	TMDbID int64  `json:"tmdb_id"`
	Season int    `json:"season"`
	Number int    `json:"number"`
	Title  string `json:"title"`
}
type AnimeOrderProposal struct {
	Groups     []AnimeOrderGroup     `json:"groups"`
	Entries    []AnimeOrderEntry     `json:"entries"`
	Unmatched  []AnimeOrderUnmatched `json:"unmatched"`
	SourceName string                `json:"source_name"`
}

func (s *Store) ProposeAnimeOrder(ctx context.Context, id int64, details metadata.EpisodeGroupDetails, subgroup string, providerID int64, releaseTitle, prefix string, start int) (AnimeOrderProposal, error) {
	out := AnimeOrderProposal{Groups: []AnimeOrderGroup{}, Entries: []AnimeOrderEntry{}, Unmatched: []AnimeOrderUnmatched{}, SourceName: details.Name}
	if _, e := s.AnimeOrder(ctx, id); e != nil {
		return out, e
	}
	if providerID < 0 || providerID > 2147483647 || start < 1 || start > 100000 || (prefix != "episode" && prefix != "SP" && prefix != "OVA" && prefix != "ONA" && prefix != "OAD") || !ValidAnimeTitle(releaseTitle) || len(releaseTitle) > 180 {
		return out, ErrAnimeOrderInput
	}
	for _, g := range details.Groups {
		out.Groups = append(out.Groups, AnimeOrderGroup{ID: g.ID, Name: g.Name, Order: g.Order, EpisodeCount: len(g.Episodes)})
	}
	if subgroup == "" {
		return out, nil
	}
	var group *metadata.EpisodeSubgroup
	for n := range details.Groups {
		if details.Groups[n].ID == subgroup {
			group = &details.Groups[n]
		}
	}
	if group == nil || len(group.Episodes) == 0 || len(group.Episodes) > 5000 || start+len(group.Episodes)-1 > 100000 {
		return out, ErrAnimeOrderInput
	}
	ordered := append([]metadata.GroupEpisode{}, group.Episodes...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Order < ordered[j].Order })
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	var seriesTMDb int64
	if e = tx.QueryRowContext(ctx, `SELECT tmdb_id FROM media_items WHERE id=? AND kind='series' AND category='anime'`, id).Scan(&seriesTMDb); e != nil {
		return out, e
	}
	type ep struct {
		id             int64
		season, number int
	}
	saved := map[int64]ep{}
	rows, e := tx.QueryContext(ctx, `SELECT e.id,e.tmdb_id,s.number,e.number FROM episodes e JOIN seasons s ON s.id=e.season_id WHERE s.series_id=? AND s.loaded_at<>''`, id)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var v ep
		var tmdb int64
		if e = rows.Scan(&v.id, &tmdb, &v.season, &v.number); e != nil {
			break
		}
		if saved[tmdb].id != 0 {
			rows.Close()
			return out, ErrAnimeOrderInput
		}
		saved[tmdb] = v
	}
	re := rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if re != nil {
		return out, re
	}
	seen := map[int64]bool{}
	for n, v := range ordered {
		if v.Order != n || v.ShowID != seriesTMDb || v.ID < 1 || seen[v.ID] {
			return out, ErrAnimeOrderInput
		}
		seen[v.ID] = true
		canonical, ok := saved[v.ID]
		if !ok || canonical.season != v.Season || canonical.number != v.Number {
			out.Unmatched = append(out.Unmatched, AnimeOrderUnmatched{TMDbID: v.ID, Season: v.Season, Number: v.Number, Title: v.Name})
			continue
		}
		label := fmt.Sprint(start + n)
		if prefix != "episode" {
			label = prefix + label
		}
		out.Entries = append(out.Entries, AnimeOrderEntry{EpisodeID: canonical.id, ProviderID: providerID, ReleaseTitle: strings.TrimSpace(releaseTitle), Label: label})
	}
	return out, tx.Commit()
}
