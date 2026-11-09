package library

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/quality"
)

var ErrAnimeOrderInput = errors.New("Choose up to 20 distinct entries from one Anime provider and 5000 distinct loaded episodes with a release title (at most 180 UTF-8 bytes) and an unambiguous episode label.")
var ErrAnimeOrderRevision = errors.New("The Anime identities, order, section or episode metadata changed. Reload and review again.")
var ErrAnimeOrderUnavailable = errors.New("Episode-order mappings apply to Anime shows; Anime films can link identities only.")
var ErrAnimeOrderReview = errors.New("This order review expired or was already applied. Create a fresh review.")

type AnimeIdentity = metadata.AnimeIdentity
type AnimeOrderEntry struct {
	EpisodeID    int64  `json:"episode_id"`
	ProviderID   int64  `json:"provider_id"`
	ReleaseTitle string `json:"release_title"`
	Label        string `json:"label"`
}
type AnimeOrderSettings struct {
	Provider string `json:"provider"`
	MediaID  int64  `json:"media_id"`
	// Revision is anime_orders.revision (the release-order record), while
	// NumberingRevision is series_details.revision (canonical episode numbering).
	// The JSON names follow the client contract; do not swap them without a
	// coordinated API and UI change.
	Revision          int               `json:"revision"`
	CategoryRevision  int               `json:"category_revision"`
	NumberingRevision int64             `json:"numbering_revision"`
	Identities        []AnimeIdentity   `json:"identities"`
	Entries           []AnimeOrderEntry `json:"entries"`
	Episodes          []AnimeEpisode    `json:"episodes"`
}
type AnimeOrderUpdate struct {
	Provider          string            `json:"provider,omitempty"`
	Revision          int               `json:"revision"`
	CategoryRevision  int               `json:"category_revision"`
	NumberingRevision int64             `json:"numbering_revision"`
	ProviderIDs       []int64           `json:"provider_ids"`
	Entries           []AnimeOrderEntry `json:"entries"`
}

// Validate local scope before making any external identity requests.
func ValidateAnimeOrderUpdate(old AnimeOrderSettings, in AnimeOrderUpdate) error {
	provider := in.Provider
	if provider == "" {
		provider = "anilist"
	}
	identities := []AnimeIdentity{}
	for _, id := range in.ProviderIDs {
		identities = append(identities, AnimeIdentity{Provider: provider, ID: id, Title: "Pending identity review", URL: metadata.AnimeIdentityURL(provider, id)})
	}
	_, e := normalizeAnimeOrder(old, in, identities)
	return e
}

type AnimeOrderReview struct {
	ReviewID  string             `json:"review_id"`
	Settings  AnimeOrderSettings `json:"settings"`
	ExpiresAt int64              `json:"expires_at"`
}
type AnimeOrderSnapshot struct {
	MediaID           int64             `json:"media_id"`
	Revision          int               `json:"revision"`
	CategoryRevision  int               `json:"category_revision"`
	NumberingRevision int64             `json:"numbering_revision"`
	Entries           []AnimeOrderEntry `json:"entries"`
	Identities        []AnimeIdentity   `json:"identities"`
}

var animeOrderLabel = regexp.MustCompile(`(?i)^(?:([0-9]{1,6})|(SP|OVA|ONA|OAD)([0-9]{1,6})|S([0-9]{1,5})E([0-9]{1,6}))$`)

func NormalizeAnimeOrderLabel(label string) (string, bool) {
	m := animeOrderLabel.FindStringSubmatch(strings.TrimSpace(label))
	if m == nil {
		return "", false
	}
	if m[1] != "" {
		n, _ := strconv.Atoi(m[1])
		return strconv.Itoa(n), n >= 1 && n <= 100000
	}
	if m[2] != "" {
		n, _ := strconv.Atoi(m[3])
		return strings.ToUpper(m[2]) + strconv.Itoa(n), n >= 1 && n <= 100000
	}
	s, _ := strconv.Atoi(m[4])
	e, _ := strconv.Atoi(m[5])
	return fmt.Sprintf("S%02dE%02d", s, e), s >= 0 && s <= 10000 && e >= 1 && e <= 100000
}
func readAnimeOrder(ctx context.Context, tx *sql.Tx, id int64) (AnimeOrderSettings, error) {
	out := AnimeOrderSettings{MediaID: id, Identities: []AnimeIdentity{}, Entries: []AnimeOrderEntry{}, Episodes: []AnimeEpisode{}}
	var category string
	if e := tx.QueryRowContext(ctx, `SELECT m.category,m.category_revision,COALESCE(d.revision,0),COALESCE(o.revision,0) FROM media_items m LEFT JOIN series_details d ON d.media_id=m.id LEFT JOIN anime_orders o ON o.media_id=m.id WHERE m.id=?`, id).Scan(&category, &out.CategoryRevision, &out.NumberingRevision, &out.Revision); e != nil {
		return out, e
	}
	if category != "anime" {
		return out, ErrAnimeOrderUnavailable
	}
	rows, e := tx.QueryContext(ctx, `SELECT identity_json FROM anime_provider_links WHERE media_id=? ORDER BY provider_id`, id)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var raw string
		if e = rows.Scan(&raw); e != nil {
			break
		}
		var v AnimeIdentity
		if e = json.Unmarshal([]byte(raw), &v); e != nil {
			break
		}
		out.Identities = append(out.Identities, v)
	}
	re := rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if re != nil {
		return out, re
	}
	out.Provider = "anilist"
	if len(out.Identities) > 0 {
		out.Provider = out.Identities[0].Provider
	}
	rows, e = tx.QueryContext(ctx, `SELECT episode_id,provider_id,release_title,label FROM anime_order_entries WHERE media_id=? ORDER BY episode_id`, id)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var v AnimeOrderEntry
		if e = rows.Scan(&v.EpisodeID, &v.ProviderID, &v.ReleaseTitle, &v.Label); e != nil {
			break
		}
		out.Entries = append(out.Entries, v)
	}
	re = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if re != nil {
		return out, re
	}
	rows, e = tx.QueryContext(ctx, `SELECT e.id,s.number,e.number,e.title,a.absolute_number FROM episodes e JOIN seasons s ON s.id=e.season_id LEFT JOIN anime_episode_numbers a ON a.episode_id=e.id WHERE s.series_id=? AND s.loaded_at<>'' ORDER BY s.number,e.number LIMIT 5001`, id)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var ep AnimeEpisode
		if e = rows.Scan(&ep.EpisodeID, &ep.Season, &ep.Number, &ep.Title, &ep.AbsoluteNumber); e != nil {
			break
		}
		out.Episodes = append(out.Episodes, ep)
	}
	re = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if re != nil {
		return out, re
	}
	if len(out.Episodes) > 5000 {
		return out, ErrAnimeOrderInput
	}
	return out, nil
}
func (s *Store) AnimeOrder(ctx context.Context, id int64) (AnimeOrderSettings, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return AnimeOrderSettings{}, e
	}
	defer tx.Rollback()
	out, e := readAnimeOrder(ctx, tx, id)
	if e != nil {
		return out, e
	}
	return out, tx.Commit()
}
func normalizeAnimeOrder(old AnimeOrderSettings, in AnimeOrderUpdate, identities []AnimeIdentity) (AnimeOrderSettings, error) {
	if in.Provider == "" {
		in.Provider = "anilist"
	}
	if in.Provider != "anilist" && in.Provider != "kitsu" {
		return old, ErrAnimeOrderInput
	}
	if in.Revision != old.Revision || in.CategoryRevision != old.CategoryRevision || in.NumberingRevision != old.NumberingRevision {
		return old, ErrAnimeOrderRevision
	}
	if len(in.ProviderIDs) > 20 || len(in.Entries) > 5000 || len(identities) != len(in.ProviderIDs) || (old.NumberingRevision == 0 && len(in.Entries) > 0) {
		return old, ErrAnimeOrderInput
	}
	links := map[int64]AnimeIdentity{}
	for _, v := range identities {
		if v.Provider != in.Provider || v.ID < 1 || v.ID > 2147483647 || v.Title == "" || v.URL != metadata.AnimeIdentityURL(v.Provider, v.ID) || links[v.ID].ID != 0 {
			return old, ErrAnimeOrderInput
		}
		links[v.ID] = v
	}
	seenIDs := map[int64]bool{}
	for _, id := range in.ProviderIDs {
		if links[id].ID != id || seenIDs[id] {
			return old, ErrAnimeOrderInput
		}
		seenIDs[id] = true
	}
	available := map[int64]bool{}
	for _, ep := range old.Episodes {
		available[ep.EpisodeID] = true
	}
	episodeIDs := map[int64]bool{}
	labels := map[string]bool{}
	entries := []AnimeOrderEntry{}
	for _, v := range in.Entries {
		v.ReleaseTitle = strings.TrimSpace(v.ReleaseTitle)
		label, valid := NormalizeAnimeOrderLabel(v.Label)
		key := quality.Normalize(v.ReleaseTitle) + "\x00" + label
		if !available[v.EpisodeID] || episodeIDs[v.EpisodeID] || v.ProviderID < 0 || (v.ProviderID != 0 && links[v.ProviderID].ID == 0) || !ValidAnimeTitle(v.ReleaseTitle) || len(v.ReleaseTitle) > 180 || !valid || labels[key] {
			return old, ErrAnimeOrderInput
		}
		v.Label = label
		episodeIDs[v.EpisodeID] = true
		labels[key] = true
		entries = append(entries, v)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].EpisodeID < entries[j].EpisodeID })
	sort.Slice(identities, func(i, j int) bool { return identities[i].ID < identities[j].ID })
	out := old
	out.Provider = in.Provider
	out.Entries = entries
	out.Identities = append([]AnimeIdentity{}, identities...)
	return out, nil
}
func (s *Store) ReviewAnimeOrder(ctx context.Context, id int64, in AnimeOrderUpdate, identities []AnimeIdentity) (AnimeOrderReview, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return AnimeOrderReview{}, e
	}
	defer tx.Rollback()
	old, e := readAnimeOrder(ctx, tx, id)
	if e != nil {
		return AnimeOrderReview{}, e
	}
	out, e := normalizeAnimeOrder(old, in, identities)
	if e != nil {
		return AnimeOrderReview{}, e
	}
	raw, e := json.Marshal(out)
	if e != nil {
		return AnimeOrderReview{}, e
	}
	buf := make([]byte, 16)
	if _, e = rand.Read(buf); e != nil {
		return AnimeOrderReview{}, e
	}
	review := AnimeOrderReview{ReviewID: hex.EncodeToString(buf), Settings: out, ExpiresAt: time.Now().Add(30 * time.Minute).Unix()}
	if _, e = tx.ExecContext(ctx, `DELETE FROM anime_order_reviews WHERE expires_at<unixepoch() OR id IN (SELECT id FROM anime_order_reviews ORDER BY expires_at DESC,id DESC LIMIT -1 OFFSET 99)`); e != nil {
		return review, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO anime_order_reviews(id,media_id,payload,expires_at) VALUES(?,?,?,?)`, review.ReviewID, id, string(raw), review.ExpiresAt); e != nil {
		return review, e
	}
	return review, tx.Commit()
}
func (s *Store) ApplyAnimeOrder(ctx context.Context, id int64, reviewID string) (AnimeOrderSettings, error) {
	if len(reviewID) != 32 {
		return AnimeOrderSettings{}, ErrAnimeOrderReview
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return AnimeOrderSettings{}, e
	}
	defer tx.Rollback()
	var raw string
	e = tx.QueryRowContext(ctx, `SELECT payload FROM anime_order_reviews WHERE id=? AND media_id=? AND applied=0 AND expires_at>=unixepoch()`, reviewID, id).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return AnimeOrderSettings{}, ErrAnimeOrderReview
	}
	if e != nil {
		return AnimeOrderSettings{}, e
	}
	var proposed AnimeOrderSettings
	if e = json.Unmarshal([]byte(raw), &proposed); e != nil {
		return proposed, e
	}
	old, e := readAnimeOrder(ctx, tx, id)
	if e != nil {
		return old, e
	}
	ids := []int64{}
	for _, v := range proposed.Identities {
		ids = append(ids, v.ID)
	}
	proposed, e = normalizeAnimeOrder(old, AnimeOrderUpdate{Provider: proposed.Provider, Revision: proposed.Revision, CategoryRevision: proposed.CategoryRevision, NumberingRevision: proposed.NumberingRevision, ProviderIDs: ids, Entries: proposed.Entries}, proposed.Identities)
	if e != nil {
		return old, e
	}
	if !reflect.DeepEqual(old.Entries, proposed.Entries) || !reflect.DeepEqual(old.Identities, proposed.Identities) {
		if _, e = tx.ExecContext(ctx, `INSERT INTO anime_orders(media_id,revision) VALUES(?,1) ON CONFLICT(media_id) DO UPDATE SET revision=anime_orders.revision+1`, id); e != nil {
			return old, e
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM anime_order_entries WHERE media_id=?`, id); e != nil {
			return old, e
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM anime_provider_links WHERE media_id=?`, id); e != nil {
			return old, e
		}
		for _, v := range proposed.Identities {
			payload, e := json.Marshal(v)
			if e != nil {
				return old, e
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO anime_provider_links(media_id,provider_id,identity_json) VALUES(?,?,?)`, id, v.ID, string(payload)); e != nil {
				return old, e
			}
		}
		for _, v := range proposed.Entries {
			if _, e = tx.ExecContext(ctx, `INSERT INTO anime_order_entries(media_id,episode_id,provider_id,release_title,label) VALUES(?,?,?,?,?)`, id, v.EpisodeID, v.ProviderID, v.ReleaseTitle, v.Label); e != nil {
				return old, e
			}
		}
		if old.NumberingRevision > 0 {
			if e = bumpSeries(ctx, tx, id, old.NumberingRevision); e != nil {
				return old, e
			}
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO item_automation(media_id,revision) VALUES(?,1) ON CONFLICT(media_id) DO UPDATE SET revision=item_automation.revision+1`, id); e != nil {
			return old, e
		}
	}
	if _, e = tx.ExecContext(ctx, `UPDATE anime_order_reviews SET applied=1 WHERE id=?`, reviewID); e != nil {
		return old, e
	}
	out, e := readAnimeOrder(ctx, tx, id)
	if e != nil {
		return out, e
	}
	return out, tx.Commit()
}
func (s *Store) AnimeOrderSnapshot(ctx context.Context, id int64, episodeIDs []int64) (AnimeOrderSnapshot, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return AnimeOrderSnapshot{}, e
	}
	defer tx.Rollback()
	state, e := readAnimeOrder(ctx, tx, id)
	if e != nil {
		return AnimeOrderSnapshot{}, e
	}
	out := AnimeOrderSnapshot{MediaID: id, Revision: state.Revision, CategoryRevision: state.CategoryRevision, NumberingRevision: state.NumberingRevision, Entries: []AnimeOrderEntry{}, Identities: state.Identities}
	if len(episodeIDs) < 1 || len(episodeIDs) > 100 || state.Revision < 1 {
		return out, ErrAnimeOrderInput
	}
	wanted := map[int64]bool{}
	for _, ep := range episodeIDs {
		if ep < 1 || wanted[ep] {
			return out, ErrAnimeOrderInput
		}
		wanted[ep] = true
	}
	for _, entry := range state.Entries {
		if wanted[entry.EpisodeID] {
			out.Entries = append(out.Entries, entry)
		}
	}
	if len(out.Entries) != len(wanted) {
		return out, ErrAnimeOrderInput
	}
	return out, tx.Commit()
}
func ValidateAnimeOrderSnapshot(ctx context.Context, tx *sql.Tx, snapshot *AnimeOrderSnapshot) error {
	if snapshot == nil {
		return ErrAnimeOrderInput
	}
	state, e := readAnimeOrder(ctx, tx, snapshot.MediaID)
	if e != nil {
		return e
	}
	if state.Revision != snapshot.Revision || state.CategoryRevision != snapshot.CategoryRevision || state.NumberingRevision != snapshot.NumberingRevision || !reflect.DeepEqual(state.Identities, snapshot.Identities) {
		return ErrAnimeOrderRevision
	}
	entries := map[int64]AnimeOrderEntry{}
	valid := map[int64]bool{}
	for _, v := range state.Entries {
		entries[v.EpisodeID] = v
	}
	for _, v := range state.Episodes {
		valid[v.EpisodeID] = true
	}
	if len(snapshot.Entries) < 1 || len(snapshot.Entries) > 100 {
		return ErrAnimeOrderInput
	}
	seen := map[int64]bool{}
	for _, v := range snapshot.Entries {
		if seen[v.EpisodeID] || !valid[v.EpisodeID] || entries[v.EpisodeID] != v {
			return ErrAnimeOrderRevision
		}
		seen[v.EpisodeID] = true
	}
	return nil
}
