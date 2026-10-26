package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
)

func (s *Server) animeOrderError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, library.ErrAnimeOrderInput):
		fail(w, 400, "invalid_anime_order", e.Error())
	case errors.Is(e, library.ErrAnimeOrderRevision):
		fail(w, 409, "revision_conflict", e.Error())
	case errors.Is(e, library.ErrAnimeOrderUnavailable):
		fail(w, 409, "anime_order_unavailable", e.Error())
	case errors.Is(e, library.ErrAnimeOrderReview):
		fail(w, 409, "review_expired", e.Error())
	case errors.Is(e, sql.ErrNoRows):
		fail(w, 404, "not_found", "Choose a saved Anime title.")
	default:
		var p *metadata.Error
		if errors.As(e, &p) {
			s.providerError(w, e)
		} else {
			s.internal(w, "anime_order", e)
		}
	}
}
func (s *Server) searchAnimeIdentities(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 1 || len(r.URL.Query()["query"]) != 1 {
		fail(w, 400, "invalid_anime_query", "Use one Anime title query.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	items, e := s.animeProvider.SearchAnime(ctx, strings.TrimSpace(r.URL.Query().Get("query")))
	if e != nil {
		s.providerError(w, e)
		return
	}
	send(w, 200, map[string]any{"items": items})
}
func (s *Server) animeOrderSettings(w http.ResponseWriter, r *http.Request) {
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var out library.AnimeOrderSettings
	var e error
	switch r.Method {
	case http.MethodGet:
		out, e = s.library.AnimeOrder(ctx, id)
	case http.MethodPut:
		var in struct {
			ReviewID string `json:"review_id"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		out, e = s.library.ApplyAnimeOrder(ctx, id, in.ReviewID)
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, 405, "method_not_allowed", "Use GET or PUT.")
		return
	}
	if e != nil {
		s.animeOrderError(w, e)
		return
	}
	send(w, 200, out)
}
func decodeAnimeOrder(w http.ResponseWriter, r *http.Request) (*library.AnimeOrderUpdate, bool) {
	mt, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || mt != "application/json" {
		fail(w, 415, "unsupported_media_type", "Use application/json.")
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 512<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	var in *library.AnimeOrderUpdate
	e = d.Decode(&in)
	if e == nil {
		var tail any
		if next := d.Decode(&tail); next != io.EOF {
			if next == nil {
				e = errors.New("trailing JSON")
			} else {
				e = next
			}
		}
	}
	if e != nil || in == nil {
		var large *http.MaxBytesError
		if errors.As(e, &large) {
			fail(w, 413, "body_too_large", "Anime order reviews must fit 512 KiB.")
		} else {
			fail(w, 400, "invalid_json", "Send one documented Anime order object.")
		}
		return nil, false
	}
	return in, true
}
func (s *Server) reviewAnimeOrder(w http.ResponseWriter, r *http.Request) {
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	in, ok := decodeAnimeOrder(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	old, e := s.library.AnimeOrder(ctx, id)
	if e != nil {
		s.animeOrderError(w, e)
		return
	}
	if e = library.ValidateAnimeOrderUpdate(old, *in); e != nil {
		s.animeOrderError(w, e)
		return
	}
	known := map[int64]metadata.AnimeIdentity{}
	for _, v := range old.Identities {
		known[v.ID] = v
	}
	newCount := 0
	for _, providerID := range in.ProviderIDs {
		if _, exists := known[providerID]; !exists {
			newCount++
		}
	}
	if newCount > 8 {
		fail(w, 400, "too_many_anime_identities", "Link at most eight new AniList entries per review; a show can retain up to twenty.")
		return
	}
	identities := []metadata.AnimeIdentity{}
	for _, providerID := range in.ProviderIDs {
		v, exists := known[providerID]
		if !exists {
			v, e = s.animeProvider.Anime(ctx, providerID)
			if e != nil {
				s.providerError(w, e)
				return
			}
		}
		identities = append(identities, v)
	}
	out, e := s.library.ReviewAnimeOrder(ctx, id, *in, identities)
	if e != nil {
		s.animeOrderError(w, e)
		return
	}
	send(w, 200, out)
}
func (s *Server) animeOrderSources(w http.ResponseWriter, r *http.Request) {
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if _, e := s.library.AnimeOrder(ctx, id); e != nil {
		s.animeOrderError(w, e)
		return
	}
	show, e := s.library.TVShow(ctx, id)
	if e != nil {
		s.animeOrderError(w, e)
		return
	}
	if s.metadata == nil {
		fail(w, 503, "metadata_unavailable", "Episode order lookup is unavailable.")
		return
	}
	items, e := s.metadata.EpisodeGroups(ctx, show.TMDbID)
	if e != nil {
		s.providerError(w, e)
		return
	}
	send(w, 200, map[string]any{"items": items})
}
func (s *Server) proposeAnimeOrder(w http.ResponseWriter, r *http.Request) {
	id, ok := tvID(w, r)
	if !ok {
		return
	}
	var in struct {
		GroupID      string `json:"group_id"`
		SubgroupID   string `json:"subgroup_id"`
		ProviderID   int64  `json:"provider_id"`
		ReleaseTitle string `json:"release_title"`
		LabelPrefix  string `json:"label_prefix"`
		StartNumber  int    `json:"start_number"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.GroupID == "" || len(in.GroupID) > 80 || len(in.SubgroupID) > 80 || !library.ValidAnimeTitle(strings.TrimSpace(in.ReleaseTitle)) || len(in.ReleaseTitle) > 180 || in.StartNumber < 1 || in.StartNumber > 100000 || in.ProviderID < 0 || in.ProviderID > 2147483647 || (in.LabelPrefix != "episode" && in.LabelPrefix != "SP" && in.LabelPrefix != "OVA" && in.LabelPrefix != "ONA" && in.LabelPrefix != "OAD") {
		s.animeOrderError(w, library.ErrAnimeOrderInput)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if _, e := s.library.AnimeOrder(ctx, id); e != nil {
		s.animeOrderError(w, e)
		return
	}
	show, e := s.library.TVShow(ctx, id)
	if e != nil {
		s.animeOrderError(w, e)
		return
	}
	if s.metadata == nil {
		fail(w, 503, "metadata_unavailable", "Episode order lookup is unavailable.")
		return
	}
	details, e := s.metadata.EpisodeGroup(ctx, show.TMDbID, in.GroupID)
	if e != nil {
		s.providerError(w, e)
		return
	}
	out, e := s.library.ProposeAnimeOrder(ctx, id, details, in.SubgroupID, in.ProviderID, in.ReleaseTitle, in.LabelPrefix, in.StartNumber)
	if e != nil {
		s.animeOrderError(w, e)
		return
	}
	send(w, 200, out)
}
func (s *Server) searchAnimePack(w http.ResponseWriter, r *http.Request) {
	if !s.transferReady(w) {
		return
	}
	var in struct {
		SeriesID      int64   `json:"series_id"`
		EpisodeIDs    []int64 `json:"episode_ids"`
		Query         string  `json:"query"`
		OrderRevision int     `json:"order_revision"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	items, e := s.transfer.SearchAnimePack(ctx, in.SeriesID, in.EpisodeIDs, in.Query, in.OrderRevision)
	if e != nil {
		if errors.Is(e, library.ErrAnimeOrderInput) || errors.Is(e, library.ErrAnimeOrderRevision) || errors.Is(e, library.ErrAnimeOrderUnavailable) {
			s.animeOrderError(w, e)
		} else {
			s.transferError(w, e)
		}
		return
	}
	send(w, 200, map[string]any{"items": items, "warnings": s.transfer.IndexerWarnings(ctx)})
}
