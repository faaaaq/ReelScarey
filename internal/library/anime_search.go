package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/quality"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrAnimeSearchInput = errors.New("Choose disabled, season or absolute numbering, up to eight distinct alternate titles (1–200 UTF-8 bytes), and a search title from the canonical or alternate titles. Enabled search titles must fit 180 UTF-8 bytes.")
var ErrAnimeSearchUnavailable = errors.New("Automatic Anime episode settings apply only to a saved show in the Anime section.")
var ErrAnimeSearchRevision = errors.New("The Anime search settings, section or episode metadata changed. Reload and review again.")

type AnimeSearchSettings struct {
	MediaID            int64    `json:"media_id"`
	CanonicalTitle     string   `json:"canonical_title"`
	Category           string   `json:"category"`
	Revision           int      `json:"revision"`
	CategoryRevision   int      `json:"category_revision"`
	NumberingRevision  int64    `json:"numbering_revision"`
	Mode               string   `json:"mode"`
	SearchTitle        string   `json:"search_title"`
	AlternateTitles    []string `json:"alternate_titles"`
	LoadedCount        int      `json:"loaded_count"`
	ReadyCount         int      `json:"ready_count"`
	MissingNumberCount int      `json:"missing_number_count"`
}
type AnimeSearchUpdate struct {
	Revision          int      `json:"revision"`
	CategoryRevision  int      `json:"category_revision"`
	NumberingRevision int64    `json:"numbering_revision"`
	Mode              string   `json:"mode"`
	SearchTitle       string   `json:"search_title"`
	AlternateTitles   []string `json:"alternate_titles"`
}

func readAnimeSearch(ctx context.Context, tx *sql.Tx, id int64) (AnimeSearchSettings, error) {
	out := AnimeSearchSettings{MediaID: id, Mode: "disabled", AlternateTitles: []string{}}
	var raw, kind string
	e := tx.QueryRowContext(ctx, `SELECT m.title,m.kind,m.category,m.category_revision,COALESCE(d.revision,0),COALESCE(a.revision,0),COALESCE(a.mode,'disabled'),COALESCE(a.search_title,''),COALESCE(a.alternate_titles,'[]') FROM media_items m LEFT JOIN series_details d ON d.media_id=m.id LEFT JOIN anime_search_settings a ON a.media_id=m.id WHERE m.id=?`, id).Scan(&out.CanonicalTitle, &kind, &out.Category, &out.CategoryRevision, &out.NumberingRevision, &out.Revision, &out.Mode, &out.SearchTitle, &raw)
	if e != nil {
		return out, e
	}
	if kind != "series" {
		return out, ErrAnimeSearchUnavailable
	}
	if e = json.Unmarshal([]byte(raw), &out.AlternateTitles); e != nil {
		return out, e
	}
	if out.AlternateTitles == nil {
		out.AlternateTitles = []string{}
	}
	e = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(a.absolute_number IS NULL),0) FROM episodes e JOIN seasons s ON s.id=e.season_id LEFT JOIN anime_episode_numbers a ON a.episode_id=e.id WHERE s.series_id=? AND s.number>0 AND s.loaded_at<>''`, id).Scan(&out.LoadedCount, &out.MissingNumberCount)
	if out.Category == "anime" && out.Mode != "disabled" {
		out.ReadyCount = out.LoadedCount
		if out.Mode == "absolute" {
			out.ReadyCount -= out.MissingNumberCount
		}
	}
	return out, e
}
func (s *Store) AnimeSearch(ctx context.Context, id int64) (AnimeSearchSettings, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return AnimeSearchSettings{}, e
	}
	defer tx.Rollback()
	out, e := readAnimeSearch(ctx, tx, id)
	if e != nil {
		return out, e
	}
	return out, tx.Commit()
}
func ValidAnimeTitle(s string) bool {
	return len(s) > 0 && len(s) <= 200 && utf8.ValidString(s) && !strings.ContainsFunc(s, unicode.IsControl) && quality.Normalize(s) != ""
}

// Canonical titles are always accepted. Empty search_title follows the current
// canonical title; an alternate query name must be explicitly listed.
func AnimeQueryTitle(canonical, selected string, aliases []string) (string, bool) {
	if selected == "" {
		return canonical, ValidAnimeTitle(canonical)
	}
	if quality.Normalize(selected) == quality.Normalize(canonical) {
		return selected, ValidAnimeTitle(selected)
	}
	for _, v := range aliases {
		if quality.Normalize(selected) == quality.Normalize(v) {
			return selected, ValidAnimeTitle(selected)
		}
	}
	return "", false
}
func (s *Store) SaveAnimeSearch(ctx context.Context, id int64, in AnimeSearchUpdate) (AnimeSearchSettings, error) {
	if in.Revision < 0 || in.CategoryRevision < 1 || in.NumberingRevision < 1 || (in.Mode != "disabled" && in.Mode != "season" && in.Mode != "absolute") || len(in.AlternateTitles) > 8 {
		return AnimeSearchSettings{}, ErrAnimeSearchInput
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return AnimeSearchSettings{}, e
	}
	defer tx.Rollback()
	old, e := readAnimeSearch(ctx, tx, id)
	if e != nil {
		return old, e
	}
	if old.Category != "anime" {
		return old, ErrAnimeSearchUnavailable
	}
	if old.Revision != in.Revision || old.CategoryRevision != in.CategoryRevision || old.NumberingRevision != in.NumberingRevision {
		return old, ErrAnimeSearchRevision
	}
	in.SearchTitle = strings.TrimSpace(in.SearchTitle)
	seen := map[string]bool{}
	if in.Mode != "disabled" {
		seen[quality.Normalize(old.CanonicalTitle)] = true
	}
	aliases := []string{}
	for _, raw := range in.AlternateTitles {
		v := strings.TrimSpace(raw)
		n := quality.Normalize(v)
		if !ValidAnimeTitle(v) || seen[n] {
			return old, ErrAnimeSearchInput
		}
		seen[n] = true
		aliases = append(aliases, v)
	}
	if in.SearchTitle != "" && !ValidAnimeTitle(in.SearchTitle) {
		return old, ErrAnimeSearchInput
	}
	if in.Mode != "disabled" {
		if title, ok := AnimeQueryTitle(old.CanonicalTitle, in.SearchTitle, aliases); !ok || len(title) > 180 {
			return old, ErrAnimeSearchInput
		}
	}
	if old.Mode == in.Mode && old.SearchTitle == in.SearchTitle && reflect.DeepEqual(old.AlternateTitles, aliases) {
		return old, tx.Commit()
	}
	raw, e := json.Marshal(aliases)
	if e != nil {
		return old, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO anime_search_settings(media_id,mode,search_title,alternate_titles,revision) VALUES(?,?,?,?,1) ON CONFLICT(media_id) DO UPDATE SET mode=excluded.mode,search_title=excluded.search_title,alternate_titles=excluded.alternate_titles,revision=anime_search_settings.revision+1`, id, in.Mode, in.SearchTitle, string(raw))
	if e != nil {
		return old, e
	}
	if e = bumpSeries(ctx, tx, id, old.NumberingRevision); e != nil {
		return old, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO item_automation(media_id,revision) VALUES(?,1) ON CONFLICT(media_id) DO UPDATE SET revision=item_automation.revision+1`, id); e != nil {
		return old, e
	}
	out, e := readAnimeSearch(ctx, tx, id)
	if e != nil {
		return out, e
	}
	return out, tx.Commit()
}
